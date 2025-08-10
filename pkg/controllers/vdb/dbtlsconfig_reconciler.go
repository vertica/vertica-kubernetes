package vdb

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/getconfigparameter"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/pollhttps"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/setconfigparameter"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	tlsVersionParam = "MinTLSVersion"
)

// TLSReconciler will turn on the tls config when users request it
type DBTLSConfigReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	PRunner    cmds.PodRunner
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeDBTLSConfigReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &DBTLSConfigReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("DBTLSConfigReconciler"),
		Dispatcher: dispatcher,
		PRunner:    prunner,
		Pfacts:     pfacts,
	}
}

func (t *DBTLSConfigReconciler) shouldSkipReconcile() bool {
	return t.Vdb.IsStatusConditionTrue(vapi.TLSConfigUpdateInProgress) ||
		t.Vdb.IsTLSCertRollbackNeeded() || !t.Vdb.IsStatusConditionTrue(vapi.DBInitialized) ||
		t.Vdb.IsStatusConditionTrue(vapi.UpgradeInProgress)
}

// Reconcile will create a TLS secret for the http server if one is missing
func (t *DBTLSConfigReconciler) Reconcile(ctx context.Context, request *ctrl.Request) (ctrl.Result, error) {
	if t.shouldSkipReconcile() {
		return ctrl.Result{}, nil
	}
	var updateTLSVersion, updateCipherSuites bool
	if t.Vdb.Status.DBTLSConfig != nil {
		updateTLSVersion, updateCipherSuites = t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)
		if !updateTLSVersion && !updateCipherSuites {
			return ctrl.Result{}, nil
		}
	}
	err := t.Pfacts.Collect(ctx, t.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	initiatorPod, ok := t.Pfacts.FindRunningPod()
	if !ok {
		t.Log.Info("No pod found to update tls config. restart next.")
		restartReconciler := MakeRestartReconciler(t.VRec, t.Log, t.Vdb, t.PRunner, t.Pfacts, true, t.Dispatcher)
		res, err2 := restartReconciler.Reconcile(ctx, request)
		return res, err2
	}
	if t.Vdb.Status.DBTLSConfig == nil {
		err = t.updateDBTLSConfigInStatus(ctx, initiatorPod)
		if err != nil {
			t.Log.Error(err, "failed to update db tls config. skip current loop")
			return ctrl.Result{}, nil
		}
		updateTLSVersion, updateCipherSuites = t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)
		if !updateTLSVersion && !updateCipherSuites {
			return ctrl.Result{}, nil
		}
	}
	t.Log.Info("tls config update required, ", "updateVersion", strconv.FormatBool(updateTLSVersion), "updateCipherSuites",
		strconv.FormatBool(updateCipherSuites))
	if updateTLSVersion {
		err := t.updateTLSVersion(ctx, initiatorPod)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	if updateTLSVersion {
		_, updateCipherSuites = t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)
	}
	if updateCipherSuites {
		err := t.updateCipherSuites(ctx, initiatorPod)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (t *DBTLSConfigReconciler) updateTLSVersion(ctx context.Context, initiatorPod *podfacts.PodFact) error {
	newTLSVersion := t.Vdb.Spec.DBTLSConfig.TLSVersion
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateStarted,
		"Started to update tls version to %d", newTLSVersion)
	// update TLS version
	err := t.setConfigParameter(ctx, initiatorPod, tlsVersionParam, strconv.FormatInt(int64(newTLSVersion), 10))
	if err != nil {
		t.Log.Info("failed to set TLS version", "newTLSVersion", newTLSVersion)
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	hosts, mainClusterHosts := t.getHostGroups()
	err = t.pollHTTPS(ctx, hosts, mainClusterHosts)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	newCipherSuites, err := t.getCipherSuitesFromDB(ctx, initiatorPod, newTLSVersion)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	err = t.saveTLSConfigInStatus(ctx, newCipherSuites, newTLSVersion)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateSucceeded,
		"Successfully updated tls version to %d", newTLSVersion)
	return nil
}

func (t *DBTLSConfigReconciler) getHostGroups() (upHosts,
	mainClusterHosts []string) {
	for _, detail := range t.Pfacts.Detail {
		upHosts = append(upHosts, detail.GetPodIP())
		if detail.GetSandbox() == "" {
			mainClusterHosts = append(mainClusterHosts, detail.GetPodIP())
		}
	}
	return upHosts, mainClusterHosts
}

func (t *DBTLSConfigReconciler) updateCipherSuites(ctx context.Context, initiatorPod *podfacts.PodFact) error {
	newCipherSuites := t.Vdb.Spec.DBTLSConfig.CipherSuites
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateStarted,
		"Started to update tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
	err := t.setCipherSuites(ctx, initiatorPod, t.Vdb.Spec.DBTLSConfig.TLSVersion, t.Vdb.Spec.DBTLSConfig.CipherSuites)
	if err != nil {
		t.Log.Info("failed to update cipher suites", "TLSVersion", t.Vdb.Spec.DBTLSConfig.TLSVersion, "cipherSuites",
			newCipherSuites)
		t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateFailed,
			"failed to update tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
		return err
	}
	hosts, mainClusterHosts := t.getHostGroups()
	err = t.pollHTTPS(ctx, hosts, mainClusterHosts)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
		return err
	}
	err = t.saveCipherSuitesInStatus(ctx, newCipherSuites)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
		return err
	}
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateSucceeded,
		"Successfully updated tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
	return nil
}

func (t *DBTLSConfigReconciler) pollHTTPS(ctx context.Context, upHosts, mainClusterHosts []string) error {
	opts := []pollhttps.Option{
		pollhttps.WithInitiators(upHosts),
		pollhttps.WithMainClusterHosts(strings.Join(mainClusterHosts, ",")),
	}
	return t.Dispatcher.PollHTTPS(ctx, opts...)
}

func (t *DBTLSConfigReconciler) compareTLSConfig(dbTLSConfigInSpec, dbTLSConfigInStatus *vapi.DBTLSConfig) (updateVersion,
	updateCipherSuites bool) {
	if *dbTLSConfigInSpec == *dbTLSConfigInStatus {
		return updateVersion, updateCipherSuites
	}
	if dbTLSConfigInSpec.TLSVersion != dbTLSConfigInStatus.TLSVersion {
		updateVersion = true
	}
	if !t.equal(dbTLSConfigInSpec.CipherSuites, dbTLSConfigInStatus.CipherSuites) {
		updateCipherSuites = true
	}

	return updateVersion, updateCipherSuites
}

func (t *DBTLSConfigReconciler) getCipherSuitesFromDB(ctx context.Context, initiatorPod *podfacts.PodFact,
	tlsVersion int) (string, error) {
	paramName := "enabledciphersuites"
	if tlsVersion == 3 {
		paramName = "tlsciphersuites"
	}
	t.Log.Info("Getting tls ciphers from db", "TLSVersion", tlsVersion)
	return t.getConfigParameter(ctx, initiatorPod, paramName)
}

func (t *DBTLSConfigReconciler) setCipherSuites(ctx context.Context, initiatorPod *podfacts.PodFact,
	tlsVersion int, cipherSuites string) error {
	paramName := "enabledciphersuites"
	if tlsVersion == 3 {
		paramName = "tlsciphersuites"
	}
	return t.setConfigParameter(ctx, initiatorPod, paramName, cipherSuites)
}

func (t *DBTLSConfigReconciler) placeholderForAll(cipherSuites string) string {
	if cipherSuites == "" {
		return "all supported cipher suites"
	}
	return cipherSuites
}

func (t *DBTLSConfigReconciler) getConfigParameter(ctx context.Context, initiatorPod *podfacts.PodFact,
	paramName string) (string, error) {
	opts := []getconfigparameter.Option{
		getconfigparameter.WithUserName(t.Vdb.GetVerticaUser()),
		getconfigparameter.WithInitiatorIP(initiatorPod.GetPodIP()),
		getconfigparameter.WithConfigParameter(paramName),
	}
	return t.Dispatcher.GetConfigurationParameter(ctx, opts...)
}

func (t *DBTLSConfigReconciler) setConfigParameter(ctx context.Context, initiatorPod *podfacts.PodFact,
	paramName, value string) error {
	opts := []setconfigparameter.Option{
		setconfigparameter.WithUserName(t.Vdb.GetVerticaUser()),
		setconfigparameter.WithInitiatorIP(initiatorPod.GetPodIP()),
		setconfigparameter.WithConfigParameter(paramName),
		setconfigparameter.WithValue(value),
	}
	return t.Dispatcher.SetConfigurationParameter(ctx, opts...)
}

func (t *DBTLSConfigReconciler) equal(cipherSuitesInUse, cipherSuitesInSpec string) bool {
	listOfCipherSuitesInUse := strings.Split(cipherSuitesInUse, ",")
	listOfCipherSuitesInSpec := strings.Split(strings.ToUpper(cipherSuitesInSpec), ",")
	slices.Sort(listOfCipherSuitesInUse)
	slices.Sort(listOfCipherSuitesInSpec)
	return slices.Equal(listOfCipherSuitesInUse, listOfCipherSuitesInSpec)
}

func (t *DBTLSConfigReconciler) getTLSVersionFromDB(ctx context.Context, initiatorPod *podfacts.PodFact) (int, error) {
	strValue, err := t.getConfigParameter(ctx, initiatorPod, tlsVersionParam)
	if err != nil {
		t.Log.Error(err, "failed to retrieve TLS version from db")
		return 0, err
	}
	return strconv.Atoi(strValue)
}

func (t *DBTLSConfigReconciler) updateDBTLSConfigInStatus(ctx context.Context, initiatorPod *podfacts.PodFact) error {
	tlsVersion, err := t.getTLSVersionFromDB(ctx, initiatorPod)
	if err != nil {
		t.Log.Error(err, "failed to get tls version from db")
		return err
	}
	cipherSuites, err := t.getCipherSuitesFromDB(ctx, initiatorPod, tlsVersion)
	if err != nil {
		t.Log.Error(err, "failed to get tls cipher suites from db")
		return err
	}
	t.Vdb.Status.DBTLSConfig = &vapi.DBTLSConfig{
		TLSVersion:   tlsVersion,
		CipherSuites: cipherSuites,
	}
	return t.saveTLSConfigInStatus(ctx, cipherSuites, tlsVersion)
}

func (t *DBTLSConfigReconciler) saveCipherSuitesInStatus(ctx context.Context, cipherSuites string) error {
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		vdb.Status.DBTLSConfig.CipherSuites = cipherSuites
		return nil
	}
	// Clear the condition and add a status after restore point creation.
	return vdbstatus.Update(ctx, t.VRec.Client, t.Vdb, refreshStatusInPlace)
}

func (t *DBTLSConfigReconciler) saveTLSConfigInStatus(ctx context.Context, cipherSuites string, tlsVersion int) error {
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		if vdb.Status.DBTLSConfig == nil {
			vdb.Status.DBTLSConfig = &vapi.DBTLSConfig{}
		}
		vdb.Status.DBTLSConfig.CipherSuites = cipherSuites
		vdb.Status.DBTLSConfig.TLSVersion = tlsVersion
		return nil
	}
	// Clear the condition and add a status after restore point creation.
	return vdbstatus.Update(ctx, t.VRec.Client, t.Vdb, refreshStatusInPlace)
}
