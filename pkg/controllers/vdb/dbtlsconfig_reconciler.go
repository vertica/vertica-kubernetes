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
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
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

// DBTLSConfigReconciler set tls version and cipher suites according to the spec
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
	return !t.Vdb.IsSetForTLSVersionAndCipher() || t.Vdb.IsStatusConditionTrue(vapi.TLSConfigUpdateInProgress) ||
		t.Vdb.IsTLSCertRollbackNeeded() || !t.Vdb.IsStatusConditionTrue(vapi.DBInitialized)
}

// Reconcile will compare TLS version and cipher suites in Spec with those in Status.
// If they are different, it will call vcluster API to update them.
func (t *DBTLSConfigReconciler) Reconcile(ctx context.Context, request *ctrl.Request) (ctrl.Result, error) {
	if t.shouldSkipReconcile() || t.Vdb.Spec.DBTLSConfig == nil {
		return ctrl.Result{}, nil
	}
	var updateTLSVersion, updateCipherSuites bool
	if t.Vdb.Spec.DBTLSConfig != nil && t.Vdb.Status.DBTLSConfig != nil {
		updateTLSVersion, updateCipherSuites = t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)
		if !updateTLSVersion && !updateCipherSuites {
			return ctrl.Result{}, nil
		}
	}
	initiatorPodIP, res, err := t.getInitiatorPodIP(ctx, request)
	if initiatorPodIP == "" {
		return res, err
	}
	if t.Vdb.Status.DBTLSConfig == nil {
		err = t.initializeDBTLSConfigInStatus(ctx, initiatorPodIP)
		if err != nil {
			t.Log.Error(err, "failed to initialize db tls config in status. skip current loop")
			return ctrl.Result{}, err
		}
		updateTLSVersion, updateCipherSuites = t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)
		if !updateTLSVersion && !updateCipherSuites {
			return ctrl.Result{}, nil
		}
	}
	t.Log.Info("tls config update required, ", "updateVersion", strconv.FormatBool(updateTLSVersion), "updateCipherSuites",
		strconv.FormatBool(updateCipherSuites))
	err = t.updateTLSVersionAndCipherSuites(ctx, initiatorPodIP, updateTLSVersion, updateCipherSuites)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (t *DBTLSConfigReconciler) updateTLSVersionAndCipherSuites(ctx context.Context, initiatorPodIP string,
	updateTLSVersion, updateCipherSuites bool) error {
	if updateTLSVersion {
		err := t.updateTLSVersionAndReadCipherSuites(ctx, initiatorPodIP)
		if err != nil {
			return err
		}
		_, updateCipherSuites = t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)
	}
	if updateCipherSuites {
		err := t.updateCipherSuites(ctx, initiatorPodIP)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *DBTLSConfigReconciler) getInitiatorPodIP(ctx context.Context, request *ctrl.Request) (string, ctrl.Result, error) {
	initiatorPodIP, res, err := t.getInitiatorPodIPImpl(ctx, request, true)
	if initiatorPodIP == "" {
		if verrors.IsReconcileAborted(res, err) {
			return initiatorPodIP, res, err
		} else {
			// restart successful, retry without restart
			initiatorPodIP, res, err = t.getInitiatorPodIPImpl(ctx, request, false)
			if initiatorPodIP == "" {
				return initiatorPodIP, res, err
			}
		}
	}
	return initiatorPodIP, ctrl.Result{}, nil
}

// getInitiatorPodIPImpl will find an initiator pod from primary cluster. If that fails, it can optionally do a restart
func (t *DBTLSConfigReconciler) getInitiatorPodIPImpl(ctx context.Context, request *ctrl.Request,
	restartOnFailure bool) (string, ctrl.Result, error) {
	err := t.Pfacts.Collect(ctx, t.Vdb)
	if err != nil {
		return "", ctrl.Result{}, err
	}
	initiatorPodIP, ok := t.Pfacts.FindFirstPrimaryUpPodIP()
	if !ok {
		if restartOnFailure {
			t.Log.Info("No pod found to update tls config. restart next.")
			restartReconciler := MakeRestartReconciler(t.VRec, t.Log, t.Vdb, t.PRunner, t.Pfacts, true, t.Dispatcher)
			res, err2 := restartReconciler.Reconcile(ctx, request)
			return "", res, err2
		} else {
			return "", ctrl.Result{}, fmt.Errorf("failed to find an initiator pod to reconcile tls version and cipher suites")
		}
	}
	return initiatorPodIP, ctrl.Result{}, err
}

func (t *DBTLSConfigReconciler) updateTLSVersionAndReadCipherSuites(ctx context.Context, initiatorPodIP string) error {
	newTLSVersion := t.Vdb.Spec.DBTLSConfig.TLSVersion
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.DBTLSUpdateStarted,
		"Started to update tls version to %d", newTLSVersion)
	// update TLS version
	err := t.setConfigParameter(ctx, initiatorPodIP, tlsVersionParam, strconv.FormatInt(int64(newTLSVersion), 10))
	if err != nil {
		t.Log.Info("failed to set TLS version", "newTLSVersion", newTLSVersion)
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.DBTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	hosts, mainClusterHosts := podfacts.GetHostGroups(t.Pfacts)
	err = t.pollHTTPS(ctx, hosts, mainClusterHosts)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.DBTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	newCipherSuites, err := t.getCipherSuitesFromDB(ctx, initiatorPodIP, newTLSVersion)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.DBTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	err = t.saveTLSConfigInStatus(ctx, newCipherSuites, newTLSVersion)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.DBTLSUpdateFailed,
			"Failed to update tls version to %d", newTLSVersion)
		return err
	}
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.DBTLSUpdateSucceeded,
		"Successfully updated tls version to %d", newTLSVersion)
	return nil
}

func (t *DBTLSConfigReconciler) updateCipherSuites(ctx context.Context, initiatorPodIP string) error {
	newCipherSuites := t.Vdb.Spec.DBTLSConfig.CipherSuites
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.DBTLSUpdateStarted,
		"Started to update tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
	err := t.setCipherSuites(ctx, initiatorPodIP, t.Vdb.Spec.DBTLSConfig.TLSVersion, strings.ToUpper(t.Vdb.Spec.DBTLSConfig.CipherSuites))
	if err != nil {
		t.Log.Info("failed to update cipher suites", "TLSVersion", t.Vdb.Spec.DBTLSConfig.TLSVersion, "cipherSuites",
			newCipherSuites)
		t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.DBTLSUpdateFailed,
			"failed to update tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
		return err
	}
	hosts, mainClusterHost := podfacts.GetHostGroups(t.Pfacts)
	err = t.pollHTTPS(ctx, hosts, mainClusterHost)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.DBTLSUpdateFailed,
			"Failed to update tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
		return err
	}
	err = t.saveCipherSuitesInStatus(ctx, newCipherSuites)
	if err != nil {
		t.VRec.Eventf(t.Vdb, corev1.EventTypeWarning, events.DBTLSUpdateFailed,
			"Failed to update tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
		return err
	}
	t.VRec.Eventf(t.Vdb, corev1.EventTypeNormal, events.DBTLSUpdateSucceeded,
		"Successfully updated tls cipher suites to %s", t.placeholderForAll(newCipherSuites))
	return nil
}

func (t *DBTLSConfigReconciler) pollHTTPS(ctx context.Context, upHosts []string, mainClusterHost string) error {
	certCache := t.VRec.CacheManager.GetCertCacheForVdb(t.Vdb.Namespace, t.Vdb.Name)
	httpsCert, err := certCache.ReadCertFromSecret(ctx, t.Vdb.GetHTTPSNMATLSSecretInUse())
	if err != nil {
		return err
	}
	opts := []pollhttps.Option{
		pollhttps.WithInitiators(upHosts),
		pollhttps.WithMainClusterHosts(mainClusterHost),
		pollhttps.WithSyncCatalogRequired(true),
		pollhttps.WithNewHTTPSCerts(httpsCert),
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

func (t *DBTLSConfigReconciler) getCipherSuitesFromDB(ctx context.Context, initiatorPodIP string,
	tlsVersion int) (string, error) {
	paramName := "enabledciphersuites"
	if tlsVersion == 3 {
		paramName = "tlsciphersuites"
	}
	t.Log.Info("Getting tls ciphers from db", "TLSVersion", tlsVersion)
	cipherSuites, err := t.getConfigParameter(ctx, initiatorPodIP, paramName)
	if err != nil {
		return cipherSuites, err
	}
	if tlsVersion == 2 {
		cipherSuites = strings.ReplaceAll(cipherSuites, ",", ":")
	}
	return cipherSuites, err
}

func (t *DBTLSConfigReconciler) setCipherSuites(ctx context.Context, initiatorPodIP string,
	tlsVersion int, cipherSuites string) error {
	paramName := "tlsciphersuites"
	if tlsVersion == 2 {
		paramName = "enabledciphersuites"
		cipherSuites = strings.ReplaceAll(cipherSuites, ":", ",")
	}
	return t.setConfigParameter(ctx, initiatorPodIP, paramName, strings.ToUpper(cipherSuites))
}

func (t *DBTLSConfigReconciler) placeholderForAll(cipherSuites string) string {
	if cipherSuites == "" {
		return "all supported cipher suites"
	}
	return cipherSuites
}

func (t *DBTLSConfigReconciler) getConfigParameter(ctx context.Context, initiatorPodIP string,
	paramName string) (string, error) {
	opts := []getconfigparameter.Option{
		getconfigparameter.WithUserName(t.Vdb.GetVerticaUser()),
		getconfigparameter.WithInitiatorIP(initiatorPodIP),
		getconfigparameter.WithConfigParameter(paramName),
	}
	return t.Dispatcher.GetConfigurationParameter(ctx, opts...)
}

func (t *DBTLSConfigReconciler) setConfigParameter(ctx context.Context, initiatorPodIP string,
	paramName, value string) error {
	opts := []setconfigparameter.Option{
		setconfigparameter.WithUserName(t.Vdb.GetVerticaUser()),
		setconfigparameter.WithInitiatorIP(initiatorPodIP),
		setconfigparameter.WithConfigParameter(paramName),
		setconfigparameter.WithValue(value),
	}
	return t.Dispatcher.SetConfigurationParameter(ctx, opts...)
}

func (t *DBTLSConfigReconciler) equal(cipherSuitesInUse, cipherSuitesInSpec string) bool {
	listOfCipherSuitesInUse := strings.Split(strings.ToUpper(cipherSuitesInUse), ":")
	listOfCipherSuitesInSpec := strings.Split(strings.ToUpper(cipherSuitesInSpec), ":")
	slices.Sort(listOfCipherSuitesInUse)
	slices.Sort(listOfCipherSuitesInSpec)
	return slices.Equal(listOfCipherSuitesInUse, listOfCipherSuitesInSpec)
}

func (t *DBTLSConfigReconciler) getTLSVersionFromDB(ctx context.Context, initiatorPodIP string) (int, error) {
	strValue, err := t.getConfigParameter(ctx, initiatorPodIP, tlsVersionParam)
	if err != nil {
		t.Log.Error(err, "failed to retrieve TLS version from db")
		return 0, err
	}
	return strconv.Atoi(strValue)
}

func (t *DBTLSConfigReconciler) initializeDBTLSConfigInStatus(ctx context.Context, initiatorPodIP string) error {
	tlsVersion, err := t.getTLSVersionFromDB(ctx, initiatorPodIP)
	if err != nil {
		t.Log.Error(err, "failed to get tls version from db")
		return err
	}
	cipherSuites, err := t.getCipherSuitesFromDB(ctx, initiatorPodIP, tlsVersion)
	if err != nil {
		t.Log.Error(err, "failed to get tls cipher suites from db")
		return err
	}
	t.Vdb.Status.DBTLSConfig = &vapi.DBTLSConfig{
		TLSVersion:   tlsVersion,
		CipherSuites: cipherSuites,
	}
	t.Log.Info("initialize DBTLSConfig in status", "tls version", tlsVersion, "cipher suites", cipherSuites)
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
