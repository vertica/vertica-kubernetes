package vdb

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/getconfigparameter"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/setconfigparameter"
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

// Reconcile will create a TLS secret for the http server if one is missing
func (t *DBTLSConfigReconciler) Reconcile(ctx context.Context, request *ctrl.Request) (ctrl.Result, error) {
	if (!t.Vdb.IsStatusConditionTrue(vapi.HTTPSTLSConfigUpdateFinished) &&
		!t.Vdb.IsStatusConditionTrue(vapi.ClientServerTLSConfigUpdateFinished)) ||
		!t.Vdb.IsStatusConditionTrue(vapi.TLSConfigUpdateInProgress) ||
		t.Vdb.IsTLSCertRollbackNeeded() || !t.Vdb.IsStatusConditionTrue(vapi.DBInitialized) ||
		t.Vdb.IsStatusConditionTrue(vapi.UpgradeInProgress) {
		return ctrl.Result{}, nil
	}

	if t.Vdb.Spec.DBTLSConfig == t.Vdb.Status.DBTLSConfig {
		return ctrl.Result{}, nil
	}

	err := t.Pfacts.Collect(ctx, t.Vdb)
	if err != nil {
		t.Log.Error(err, "failed to collect pfacts to set up tls. skip current loop")
		return ctrl.Result{}, nil
	}

	initiatorPod, ok := t.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		t.Log.Info("No pod found to set up tls config. restart next.")
		restartReconciler := MakeRestartReconciler(t.VRec, t.Log, t.Vdb, t.PRunner, t.Pfacts, true, t.Dispatcher)
		res, err := restartReconciler.Reconcile(ctx, request)
		return res, err
	}
	updateTLSVersion, updateCipherSuites := t.compareTLSConfig(t.Vdb.Spec.DBTLSConfig, t.Vdb.Status.DBTLSConfig)

	if updateTLSVersion {
		err := t.setConfigParameter(ctx, initiatorPod, "MinTLSVersion", strconv.FormatInt(int64(t.Vdb.Spec.DBTLSConfig.TLSVersion), 10))
		if err != nil {
			t.Log.Info("failed to set TLS version", "newTLSVersion", t.Vdb.Spec.DBTLSConfig.TLSVersion)
			return ctrl.Result{}, err
		}
	}
	if updateCipherSuites {
		err := t.setCipherSuites(ctx, initiatorPod, t.Vdb.Spec.DBTLSConfig.TLSVersion, t.Vdb.Spec.DBTLSConfig.CipherSuites)
		if err != nil {
			t.Log.Info("failed to update cipher suites", "TLSVersion", t.Vdb.Spec.DBTLSConfig.TLSVersion, "cipherSuites", t.Vdb.Spec.DBTLSConfig.CipherSuites)
			return ctrl.Result{}, err
		}
	}

	/* dbTLSVersion, err := t.getTLSVersionFromDB(ctx, t.Pfacts, initiatorPod)

	cipherSuitesInUse := t.getCipherSuitesFromDB() */
	return ctrl.Result{}, nil
}

func (t *DBTLSConfigReconciler) compareTLSConfig(dbTLSConfigInSpec, dbTLSConfigInStatus *v1.DBTLSConfig) (bool, bool) {
	if *dbTLSConfigInSpec == *dbTLSConfigInStatus {
		return false, false
	}
	updateVersion := false
	updateCipherSuites := false
	if dbTLSConfigInSpec.TLSVersion != dbTLSConfigInStatus.TLSVersion {
		updateVersion = true
	}
	if !t.equal(dbTLSConfigInSpec.CipherSuites, dbTLSConfigInStatus.CipherSuites) {
		updateCipherSuites = true
	}

	return updateVersion, updateCipherSuites
}

func (t *DBTLSConfigReconciler) getTLSVersionFromDB(ctx context.Context, initiatorPod *podfacts.PodFact) (int, error) {
	strValue, err := t.getConfigParameter(ctx, initiatorPod, tlsVersionParam)
	if err != nil {
		t.Log.Error(err, "failed to retrieve TLS version from db")
		return 0, err
	}
	return strconv.Atoi(strValue)
}

func (t *DBTLSConfigReconciler) setCipherSuites(ctx context.Context, initiatorPod *podfacts.PodFact,
	tlsVersion int, cipherSuites string) error {
	paramName := ""
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

func (t *DBTLSConfigReconciler) parseTLSVersion(stdout string) (int, error) {
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return strconv.Atoi(res)
}

func (t *DBTLSConfigReconciler) equal(cipherSuitesInUse, cipherSuitesInSpec string) bool {
	listOfCipherSuitesInUse := strings.Split(cipherSuitesInUse, ",")
	listOfCipherSuitesInSpec := strings.Split(strings.ToUpper(cipherSuitesInSpec), ",")
	slices.Sort(listOfCipherSuitesInUse)
	slices.Sort(listOfCipherSuitesInSpec)
	return slices.Equal(listOfCipherSuitesInUse, listOfCipherSuitesInSpec)
}

/* func (t *DBTLSConfigReconciler) getTLSVersionInUse() int {
	return t.Vdb.Status.DBTLSConfig.TLSVersion
} */
