/*
 (c) Copyright [2021-2024] Open Text.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package vdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	vops "github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatetlscerts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/settlsconfig"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	tlsConfigServer      = "Server"
	tlsConfigHTTPS       = "HTTP"
	certicatePrefixHTTPS = ""
)

type TLSUpdateData struct {
	NewTLSMode     string
	CurrentTLSMode string
	NewSecret      string
	CurrentSecret  string
	tlsConfigName  string
}

// Struct that contains the cert metadata
// for polling cert after cert rotation
type TLSPollingCertMetadata struct {
	Key    string
	Cert   string
	CACert string
}

// Structs that owns the logic of different tls operations.
// It can set a tls config, rotate a tls cert
type TLSConfigManager struct {
	Rec        config.ReconcilerInterface
	Vdb        *vapi.VerticaDB
	Log        logr.Logger
	TLSConfig  string
	Dispatcher vadmin.Dispatcher
	TLSUpdateData
	TLSUpdateType int
	TLSPollingCertMetadata
}

func MakeTLSConfigManager(recon config.ReconcilerInterface, log logr.Logger, vdb *vapi.VerticaDB,
	tlsConfig string, dispatcher vadmin.Dispatcher) *TLSConfigManager {
	t := &TLSConfigManager{
		Rec:        recon,
		Vdb:        vdb,
		Log:        log,
		TLSConfig:  tlsConfig,
		Dispatcher: dispatcher,
	}
	return t
}

// setPollingCertMetadata sets the metadata needed for polling cert
func (t *TLSConfigManager) setPollingCertMetadata(ctx context.Context) (ctrl.Result, error) {
	var currentSecretData map[string][]byte
	var newSecretData map[string][]byte
	var res ctrl.Result
	var err error

	// The polling is done using an https endpoint, so we always need
	// httpsNMATLSSecret
	currentSecretName := t.Vdb.GetHTTPSNMATLSSecretInUse()
	newSecretName := t.Vdb.GetHTTPSNMATLSSecret()
	currentSecretData, res, err = readSecret(t.Vdb, t.Rec, t.Rec.GetClient(), t.Log, ctx, currentSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	if t.TLSUpdateType != tlsModeChangeOnly || t.isClientServerTLSConfig() {
		newSecretData, res, err = readSecret(t.Vdb, t.Rec, t.Rec.GetClient(), t.Log, ctx, newSecretName)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		t.Key = string(newSecretData[corev1.TLSPrivateKeyKey])
		t.Cert = string(newSecretData[corev1.TLSCertKey])
		t.CACert = string(newSecretData[corev1.ServiceAccountRootCAKey])
		return res, err
	}

	t.Key = string(currentSecretData[corev1.TLSPrivateKeyKey])
	t.Cert = string(currentSecretData[corev1.TLSCertKey])
	t.CACert = string(currentSecretData[corev1.ServiceAccountRootCAKey])

	return res, err
}

// updateTLSConfig calls the vclusterops api that will update the tls config by cert
// rotation and/or tls mode update
func (t *TLSConfigManager) updateTLSConfig(ctx context.Context, initiatorIP string) error {
	switch t.TLSUpdateType {
	case tlsModeAndCertChange:
		// This type implies both a cert change and a TLS mode change
		t.Log.Info(fmt.Sprintf("Ready to rotate %s cert from %s to %s", t.TLSConfig, t.CurrentSecret, t.NewSecret))
		t.Log.Info(fmt.Sprintf("Ready to change %s TLS mode from %s to %s", t.TLSConfig, t.CurrentTLSMode, t.NewTLSMode))
	case httpsCertChangeOnly:
		// Only a cert change
		t.Log.Info(fmt.Sprintf("Ready to rotate %s cert from %s to %s", t.TLSConfig, t.CurrentSecret, t.NewSecret))
	case tlsModeChangeOnly:
		// Only a TLS mode change
		t.Log.Info(fmt.Sprintf("Ready to change %s TLS mode from %s to %s", t.TLSConfig, t.CurrentTLSMode, t.NewTLSMode))
	}

	var keyConfig, certConfig, caCertConfig, secretName, secretManager string
	var cacheDuration string
	if t.Vdb.GetTLSCacheDuration() > 0 {
		cacheDuration = fmt.Sprintf(",\"cache-duration\":%d", t.Vdb.GetTLSCacheDuration())
	}

	switch {
	case secrets.IsAWSSecretsManagerSecret(t.NewSecret):
		secretARN, versionID := secrets.GetAWSSecretARN(t.NewSecret)
		keyConfig, certConfig, caCertConfig = t.getAWSCertsConfig(versionID, cacheDuration)
		secretName = secretARN
		secretManager = vops.AWSSecretManagerType
	default:
		keyConfig, certConfig, caCertConfig = t.getK8sCertsConfig(cacheDuration)
		secretName = t.NewSecret
		secretManager = vops.K8sSecretManagerType
	}
	opts := []rotatetlscerts.Option{
		rotatetlscerts.WithPollingKey(t.Key),
		rotatetlscerts.WithPollingCert(t.Cert),
		rotatetlscerts.WithPollingCaCert(t.CACert),
		rotatetlscerts.WithKey(secretName, keyConfig),
		rotatetlscerts.WithCert(secretName, certConfig),
		rotatetlscerts.WithCaCert(secretName, caCertConfig),
		rotatetlscerts.WithTLSMode(t.NewTLSMode),
		rotatetlscerts.WithInitiator(initiatorIP),
		rotatetlscerts.WithTLSConfig(t.TLSConfig),
		rotatetlscerts.WithNewSecretManager(secretManager),
	}
	started, failed, succeeded := t.getEvents()
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, started,
		"Starting tls cert rotation for %s with secret name %s and mode %s",
		t.TLSConfig, t.NewSecret, t.CurrentTLSMode)
	err := t.Dispatcher.RotateTLSCerts(ctx, opts...)
	if err != nil {
		t.Rec.Eventf(t.Vdb, corev1.EventTypeWarning, failed,
			"Failed to rotate %s tls cert with secret name %s and mode %s", t.TLSConfig, t.NewSecret, t.NewTLSMode)
		return t.triggerRollback(ctx, err)
	}
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, succeeded,
		"Successfully rotated %s tls cert with secret name %s and mode %s", t.TLSConfig, t.NewSecret, t.NewTLSMode)

	// update the tls mode in the status
	return t.updateTLSModeInStatus(ctx)
}

// updateTLSModeInStatus updates the tls mode in the status after it was updated
// in the database
func (t *TLSConfigManager) updateTLSModeInStatus(ctx context.Context) error {
	if t.CurrentTLSMode != t.NewTLSMode {
		t.Log.Info("Starting tls mode update in status", "old", t.CurrentTLSMode, "new", t.NewTLSMode)
		httpsTLSConfig := vapi.MakeTLSConfig(t.tlsConfigName, t.Vdb.GetSecretInUse(t.tlsConfigName), t.NewTLSMode)
		err := vdbstatus.UpdateTLSConfigs(ctx, t.Rec.GetClient(), t.Vdb, []*vapi.TLSConfigStatus{httpsTLSConfig})
		if err != nil {
			t.Log.Error(err, "failed to update tls mode in status after tls mode update in db")
			return err
		}
	}

	return nil
}

// setTLSConfigInDB creates a tls config in the database and update the status
func (t *TLSConfigManager) setTLSConfigInDB(ctx context.Context, initiatorPod *podfacts.PodFact, grantAuth bool) error {
	opts := []settlsconfig.Option{
		settlsconfig.WithTLSMode(t.NewTLSMode),
		settlsconfig.WithTLSSecretName(t.NewSecret),
		settlsconfig.WithInitiatorIP(initiatorPod.GetPodIP()),
		settlsconfig.WithNamespace(t.Vdb.GetNamespace()),
		settlsconfig.WithHTTPSTLSConfig(t.isHTTPSTLSConfig()),
		settlsconfig.WithGrantAuth(grantAuth),
	}

	return t.Dispatcher.SetTLSConfig(ctx, opts...)
}

// setTLSConfigInStatus updates the status with the current tls config in the db
func (t *TLSConfigManager) setTLSConfigInStatus(ctx context.Context, tlsMode string) error {
	tlsConfig := vapi.MakeTLSConfig(t.tlsConfigName, t.NewSecret, tlsMode)

	t.Log.Info("Updating TLS config in status", "TLSConfig", tlsConfig)
	if err := vdbstatus.UpdateTLSConfigs(ctx, t.Rec.GetClient(), t.Vdb, []*vapi.TLSConfigStatus{tlsConfig}); err != nil {
		t.Log.Error(err, "failed to update TLS config in status")
		return err
	}

	return nil
}

// getTLSConfigFromDB will check if the given tls config is already in the db, and return
// a proof
func (t *TLSConfigManager) getTLSConfigFromDB(ctx context.Context, pfacts *podfacts.PodFacts,
	initiatorPod *podfacts.PodFact) (certificate, mode string, err error) {
	sql := fmt.Sprintf("select certificate, mode from tls_configurations where name='%s';", t.getTLSConfigName())
	cmd := []string{"-tAc", sql}
	t.Log.Info("Getting tls config from db", "tlsConfig", t.TLSConfig)
	stdout, stderr, errVsql := pfacts.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	if errVsql != nil || strings.Contains(stderr, "Error") {
		t.Log.Error(err, fmt.Sprintf("failed to retrieve %s TLS config from db, stderr - %s", t.TLSConfig, stderr))
		err = errVsql
		return
	}

	certificate, mode, err = t.parseConfig(stdout)
	return
}

// checkNMATLSConfigMap checks if nma tls config map exists and has the
// latest values
func (t *TLSConfigManager) checkNMATLSConfigMap(ctx context.Context) (ctrl.Result, error) {
	configMapName := names.GenNMACertConfigMap(t.Vdb)
	configMap := &corev1.ConfigMap{}
	err := t.Rec.GetClient().Get(ctx, configMapName, configMap)
	if err != nil {
		if kerrors.IsNotFound(err) {
			t.Log.Info("TLS config map does not exist yet. Requeueing")
			return ctrl.Result{Requeue: true}, nil
		}
		t.Log.Error(err, "failed to retrieve TLS cert secret configmap")
		return ctrl.Result{}, err
	}

	var isUpToDate bool
	if t.isHTTPSTLSConfig() {
		isUpToDate = configMap.Data[builder.NMASecretNameEnv] == t.Vdb.GetHTTPSNMATLSSecret()
	} else {
		isUpToDate = configMap.Data[builder.NMAClientSecretNameEnv] == t.Vdb.GetClientServerTLSSecret() &&
			configMap.Data[builder.NMAClientSecretTLSModeEnv] == t.Vdb.GetNMAClientServerTLSMode()
	}

	if !isUpToDate {
		t.Log.Info("TLS config map is not up to date. Requeueing")
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (t *TLSConfigManager) isClientServerTLSConfig() bool {
	return t.TLSConfig == tlsConfigServer
}

func (t *TLSConfigManager) isHTTPSTLSConfig() bool {
	return t.TLSConfig == tlsConfigHTTPS
}

// parseConfig parses the query output and returns the certificate and tls mode
func (t *TLSConfigManager) parseConfig(stdout string) (certificate, mode string, err error) {
	lines := strings.Split(stdout, "\n")
	cols := strings.Split(lines[0], "|")
	const ExpectedCols = 2
	if len(cols) != ExpectedCols {
		err = fmt.Errorf("expected %d columns from tls_configurations query but only got %d", ExpectedCols, len(cols))
		return
	}

	certificate = cols[0]
	mode = cols[1]
	return
}

// getEvents returns the correct set of events based on the tls config
func (t *TLSConfigManager) getEvents() (started, failed, succeeded string) {
	if t.TLSConfig == tlsConfigHTTPS {
		started = events.HTTPSTLSUpdateStarted
		failed = events.HTTPSTLSUpdateFailed
		succeeded = events.HTTPSTLSUpdateSucceeded
	} else if t.TLSConfig == tlsConfigServer {
		started = events.ClientServerTLSUpdateStarted
		failed = events.ClientServerTLSUpdateFailed
		succeeded = events.ClientServerTLSUpdateSucceeded
	}

	return
}

// setTLSUpdateType sets the field used to determine what kind of tls update
// is needed. Possible options are: no tls change, only tls mode update,
// only cert rotation or both cert rotation and tls mode update
func (t *TLSConfigManager) setTLSUpdateType() {
	certChanged := t.CurrentSecret != "" && t.NewSecret != t.CurrentSecret
	modeChanged := t.NewTLSMode != t.CurrentTLSMode

	switch {
	case modeChanged && certChanged:
		t.TLSUpdateType = tlsModeAndCertChange
	case modeChanged:
		t.TLSUpdateType = tlsModeChangeOnly
	case certChanged:
		t.TLSUpdateType = httpsCertChangeOnly
	default:
		t.TLSUpdateType = noTLSChange
	}
}

func (t *TLSConfigManager) needTLSConfigChange() bool {
	return t.TLSUpdateType != noTLSChange
}

func (t *TLSConfigManager) getK8sCertsConfig(cacheDuration string) (keyConfig, certConfig, caCertConfig string) {
	keyConfig = fmt.Sprintf("{\"data-key\":%q,\"namespace\":%q%s}", corev1.TLSPrivateKeyKey, t.Vdb.Namespace, cacheDuration)
	certConfig = fmt.Sprintf("{\"data-key\":%q,\"namespace\":%q%s}", corev1.TLSCertKey, t.Vdb.Namespace, cacheDuration)
	caCertConfig = fmt.Sprintf("{\"data-key\":%q,\"namespace\":%q%s}", paths.HTTPServerCACrtName, t.Vdb.Namespace, cacheDuration)
	return
}

func (t *TLSConfigManager) getAWSCertsConfig(versionID, cacheDuration string) (keyConfig, certConfig, caCertConfig string) {
	region, _ := secrets.GetAWSRegion(t.NewSecret)

	keyConfig = fmt.Sprintf("{\"json-key\":%q,\"region\":%q,\"version-id\":%q%s}",
		corev1.TLSPrivateKeyKey, region, versionID, cacheDuration)
	certConfig = fmt.Sprintf("{\"json-key\":%q,\"region\":%q,\"version-id\":%q%s}",
		corev1.TLSCertKey, region, versionID, cacheDuration)
	caCertConfig = fmt.Sprintf("{\"json-key\":%q,\"region\":%q,\"version-id\":%q%s}",
		paths.HTTPServerCACrtName, region, versionID, cacheDuration)
	return
}

func (t *TLSConfigManager) setTLSUpdatedata() {
	if t.TLSConfig == tlsConfigHTTPS {
		t.CurrentSecret = t.Vdb.GetHTTPSNMATLSSecretInUse()
		t.NewSecret = t.Vdb.GetHTTPSNMATLSSecret()
		t.CurrentTLSMode = t.Vdb.GetHTTPSTLSModeInUse()
		t.NewTLSMode = t.Vdb.GetHTTPSNMATLSMode()
		t.tlsConfigName = vapi.HTTPSNMATLSConfigName
	} else if t.TLSConfig == tlsConfigServer {
		t.CurrentSecret = t.Vdb.GetClientServerTLSSecretInUse()
		t.NewSecret = t.Vdb.GetClientServerTLSSecret()
		t.CurrentTLSMode = t.Vdb.GetClientServerTLSModeInUse()
		t.NewTLSMode = t.Vdb.GetClientServerTLSMode()
		t.tlsConfigName = vapi.ClientServerTLSConfigName
	}
}

func (t *TLSConfigManager) getTLSConfigName() string {
	if t.isHTTPSTLSConfig() {
		return "https"
	}

	return "server"
}

func (t *TLSConfigManager) getCertificatePrefix() string {
	return fmt.Sprintf("%s_cert_", t.getTLSConfigName())
}

// triggerRollback sets a condition that lets the operator know that cert rotation
// has failed and a rollback is needed
func (t *TLSConfigManager) triggerRollback(ctx context.Context, err error) error {
	if err == nil || t.Vdb.IsTLSCertRollbackDisabled() || t.Vdb.IsTLSCertRollbackInProgress() {
		return err
	}
	reason := t.getRollbackReason(err)
	cond := vapi.MakeCondition(vapi.TLSCertRollbackNeeded, metav1.ConditionTrue, reason)
	err1 := vdbstatus.UpdateCondition(ctx, t.Rec.GetClient(), t.Vdb, cond)

	if err1 != nil {
		return err1
	}

	return err
}

func (t *TLSConfigManager) getRollbackReason(err error) string {
	errMsg := err.Error()
	if t.isClientServerTLSConfig() {
		return vapi.RollbackAfterServerCertRotationReason
	}
	if strings.Contains(errMsg, "HTTPSPollCertificateHealthOp") {
		return vapi.RollbackAfterHTTPSCertRotationReason
	}
	return vapi.FailureBeforeHTTPSCertHealthPollingReason
}
