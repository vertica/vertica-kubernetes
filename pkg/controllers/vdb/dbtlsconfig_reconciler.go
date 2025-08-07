package vdb

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
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
	t.Log.Info("libo: 0")
	if t.shouldSkipReconcile() {
		return ctrl.Result{}, nil
	}

	t.Log.Info("libo: 1")
	var updateTLSVersion, updateCipherSuites bool
	if t.Vdb.Status.DBTLSConfig != nil {
		updateTLSVersion, updateCipherSuites = t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)
		if !updateTLSVersion && !updateCipherSuites {
			return ctrl.Result{}, nil
		}
	}
	t.Log.Info("libo: 2")
	err := t.Pfacts.Collect(ctx, t.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	initiatorPod, ok := t.Pfacts.FindRunningPod() // FindFirstUpPod(false, "")
	if !ok {
		t.Log.Info("No pod found to update tls config. restart next.")
		restartReconciler := MakeRestartReconciler(t.VRec, t.Log, t.Vdb, t.PRunner, t.Pfacts, true, t.Dispatcher)
		res, err := restartReconciler.Reconcile(ctx, request)
		return res, err
	}
	if t.Vdb.Status.DBTLSConfig == nil {
		t.Log.Info("libo db tls config is empty")
		err = t.updateDbTLSConfigInStatus(ctx, initiatorPod)
		if err != nil {
			t.Log.Error(err, "failed to update db tls config. skip current loop")
			return ctrl.Result{}, nil
		}
		updateTLSVersion, updateCipherSuites = t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)
		if !updateTLSVersion && !updateCipherSuites {
			return ctrl.Result{}, nil
		}
	}
	t.Log.Info("libo: tls config update required, " + strconv.FormatBool(updateTLSVersion) + ", " + strconv.FormatBool(updateCipherSuites))
	if err != nil {
		t.Log.Error(err, "failed to collect pfacts to set up tls. skip current loop")
		return ctrl.Result{}, nil
	}
	if updateTLSVersion {
		err := t.updateTLSVersion(ctx, initiatorPod)
		if err != nil {
			return ctrl.Result{}, err
		}
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
	digest, err := t.getTLSConfigDigestFromDB(ctx, initiatorPod)
	if err != nil {
		t.Log.Info("failed to get tls config digrest")
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	// update TLS version
	err = t.setConfigParameter(ctx, initiatorPod, tlsVersionParam, strconv.FormatInt(int64(newTLSVersion), 10))
	if err != nil {
		t.Log.Info("failed to set TLS version", "newTLSVersion", newTLSVersion)
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	hosts, mainClusterHosts := t.getHostGroups()
	t.Log.Info("libo: b4 poll https for tls version")
	err = t.pollHTTPS(ctx, hosts, mainClusterHosts, digest, newTLSVersion)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	t.Log.Info("libo: polling https for tls version is done")
	t.saveTLSVersionInStatus(ctx, newTLSVersion)
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateSucceeded,
		"Successfully updated tls version to %d", newTLSVersion)
	return nil
}

func (t *DBTLSConfigReconciler) getHostGroups() (upHosts,
	mainClusterHosts []string) {
	for _, detail := range t.Pfacts.Detail {
		upHosts = append(upHosts, detail.GetPodIP())
		if detail.GetSandbox() == "" {
			t.Log.Info("libo: selected host ip - " + detail.GetPodIP())
			mainClusterHosts = append(mainClusterHosts, detail.GetPodIP())
		}
	}
	return upHosts, mainClusterHosts
}

func (t *DBTLSConfigReconciler) updateCipherSuites(ctx context.Context, initiatorPod *podfacts.PodFact) error {
	newCipherSuites := t.Vdb.Spec.DBTLSConfig.CipherSuites
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateStarted,
		"Started to update tls cipher suites to %s", newCipherSuites)
	digest, err := t.getTLSConfigDigestFromDB(ctx, initiatorPod)
	if err != nil {
		t.Log.Info("failed to get tls config digrest")
		t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateFailed,
			"failed to update tls cipher suites to %s", newCipherSuites)
		return err
	}
	err = t.setCipherSuites(ctx, initiatorPod, t.Vdb.Spec.DBTLSConfig.TLSVersion, t.Vdb.Spec.DBTLSConfig.CipherSuites)
	if err != nil {
		t.Log.Info("failed to update cipher suites", "TLSVersion", t.Vdb.Spec.DBTLSConfig.TLSVersion, "cipherSuites",
			t.Vdb.Spec.DBTLSConfig.CipherSuites)
		t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateFailed,
			"failed to update tls cipher suites to %s", newCipherSuites)
		return err
	}
	hosts, mainClusterHosts := t.getHostGroups()
	t.Log.Info("libo: b4 poll https for tls cipher suites")
	err = t.pollHTTPS(ctx, hosts, mainClusterHosts, digest, t.Vdb.Spec.DBTLSConfig.TLSVersion)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.HTTPSTLSUpdateFailed,
			"Failed to update tls cipher suites to %s", newCipherSuites)
		return err
	}
	t.Log.Info("libo: after poll https for tls cipher suites")
	t.saveCipherSuitesInStatus(ctx, newCipherSuites)
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.HTTPSTLSUpdateFailed,
		"Successfully updated tls cipher suites to %s", newCipherSuites)
	return nil
}

func (t *DBTLSConfigReconciler) pollHTTPS(ctx context.Context, upHosts, mainClusterHosts []string, digest string, tlsVersion int) error {
	opts := []pollhttps.Option{
		pollhttps.WithInitiators(upHosts),
		pollhttps.WithMainClusterHosts(mainClusterHosts),
		pollhttps.WithTLSConfigDigest(digest),
		pollhttps.WithTLSVersion(tlsVersion),
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

func (t *DBTLSConfigReconciler) setCipherSuites(ctx context.Context, initiatorPod *podfacts.PodFact,
	tlsVersion int, cipherSuites string) error {
	paramName := "enabledciphersuites"
	if tlsVersion == 3 {
		paramName = "tlsciphersuites"
	}
	return t.setConfigParameter(ctx, initiatorPod, paramName, cipherSuites)
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

func (t *DBTLSConfigReconciler) getTLSConfigDigestFromDB(ctx context.Context, initiatorPod *podfacts.PodFact) (string, error) {
	sql := "select get_tls_config_digest('https');"
	cmd := []string{"-tAc", sql}
	stdout, stderr, err := t.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	if err != nil || strings.Contains(stderr, "Error") {
		t.Log.Error(err, fmt.Sprintf("failed to get https tls config digrest, stderr - %s", stderr))
		return "", err
	}
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return res, nil
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

func (t *DBTLSConfigReconciler) getCipherSuitesFromDB(ctx context.Context, initiatorPod *podfacts.PodFact,
	tlsVersion int) (string, error) {
	paramName := "enabledciphersuites"
	if tlsVersion == 3 {
		paramName = "tlsciphersuites"
	}
	t.Log.Info("Getting tls ciphers from db", "TLSVersion", tlsVersion)
	return t.getConfigParameter(ctx, initiatorPod, paramName)
}

func (t *DBTLSConfigReconciler) getTLSVersionFromDB(ctx context.Context, initiatorPod *podfacts.PodFact) (int, error) {
	strValue, err := t.getConfigParameter(ctx, initiatorPod, tlsVersionParam)
	if err != nil {
		t.Log.Error(err, "failed to retrieve TLS version from db")
		return 0, err
	}
	return strconv.Atoi(strValue)
}

func (t *DBTLSConfigReconciler) updateDbTLSConfigInStatus(ctx context.Context, initiatorPod *podfacts.PodFact) error {
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

func (s *DBTLSConfigReconciler) saveTLSVersionInStatus(ctx context.Context, tlsVersion int) error {
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		vdb.Status.DBTLSConfig.TLSVersion = tlsVersion
		return nil
	}
	// Clear the condition and add a status after restore point creation.
	return vdbstatus.Update(ctx, s.VRec.Client, s.Vdb, refreshStatusInPlace)
}

func (s *DBTLSConfigReconciler) saveCipherSuitesInStatus(ctx context.Context, cipherSuites string) error {
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		vdb.Status.DBTLSConfig.CipherSuites = cipherSuites
		return nil
	}
	// Clear the condition and add a status after restore point creation.
	return vdbstatus.Update(ctx, s.VRec.Client, s.Vdb, refreshStatusInPlace)
}

func (s *DBTLSConfigReconciler) saveTLSConfigInStatus(ctx context.Context, cipherSuites string, tlsVersion int) error {
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		if vdb.Status.DBTLSConfig == nil {
			vdb.Status.DBTLSConfig = &vapi.DBTLSConfig{}
		}
		vdb.Status.DBTLSConfig.CipherSuites = cipherSuites
		vdb.Status.DBTLSConfig.TLSVersion = tlsVersion
		return nil
	}
	// Clear the condition and add a status after restore point creation.
	return vdbstatus.Update(ctx, s.VRec.Client, s.Vdb, refreshStatusInPlace)
}
