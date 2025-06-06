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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/settlsconfig"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
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
	secretType     string
	tlsModeType    string
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
	currentSecretName := t.Vdb.GetHTTPSTLSSecretNameInUse()
	newSecretName := t.Vdb.Spec.HTTPSNMATLSSecret
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
	if t.TLSUpdateType == tlsModeAndCertChange || t.TLSUpdateType == httpsCertChangeOnly {
		t.Log.Info(fmt.Sprintf("ready to rotate %s cert from %s to %s", t.TLSConfig, t.CurrentSecret, t.NewSecret))
	}
	if t.TLSUpdateType == tlsModeAndCertChange || t.TLSUpdateType == tlsModeChangeOnly {
		t.Log.Info(fmt.Sprintf("ready to change %s TLS mode from %s to %s", t.TLSConfig, t.CurrentTLSMode, t.NewTLSMode))
	}

	var keyConfig, certConfig, caCertConfig, secretName string

	switch {
	case secrets.IsAWSSecretsManagerSecret(t.NewSecret):
		keyConfig, certConfig, caCertConfig = t.getAWSCertsConfig()
		secretName = secrets.RemovePathReference(t.NewSecret)
	default:
		keyConfig, certConfig, caCertConfig = t.getK8sCertsConfig()
		secretName = t.NewSecret
	}
	opts := []rotatehttpscerts.Option{
		rotatehttpscerts.WithPollingKey(t.Key),
		rotatehttpscerts.WithPollingCert(t.Cert),
		rotatehttpscerts.WithPollingCaCert(t.CACert),
		rotatehttpscerts.WithKey(secretName, keyConfig),
		rotatehttpscerts.WithCert(secretName, certConfig),
		rotatehttpscerts.WithCaCert(secretName, caCertConfig),
		rotatehttpscerts.WithTLSMode(t.NewTLSMode),
		rotatehttpscerts.WithInitiator(initiatorIP),
		rotatehttpscerts.WithTLSConfig(t.TLSConfig),
	}
	started, failed, succeeded := t.getEvents()
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, started,
		"Starting tls cert rotation for %s with secret name %s and mode %s",
		t.TLSConfig, t.NewSecret, t.CurrentTLSMode)
	err := t.Dispatcher.RotateTLSCerts(ctx, opts...)
	if err != nil {
		t.Rec.Eventf(t.Vdb, corev1.EventTypeWarning, failed,
			"Failed to rotate %s tls cert with secret name %s and mode %s", t.TLSConfig, t.NewSecret, t.NewTLSMode)
		return err
	}
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, succeeded,
		"Successfully rotated %s tls cert with secret name %s and mode %s", t.TLSConfig, t.NewSecret, t.NewTLSMode)

	return err
}

// updateTLSModeInStatus updates the tls mode in the status after it was updated
// in the database
func (t *TLSConfigManager) updateTLSModeInStatus(ctx context.Context) error {
	if t.CurrentTLSMode != t.NewTLSMode {
		t.Log.Info("Starting tls mode update in status", "old", t.CurrentTLSMode, "new", t.NewTLSMode)
		mode := vapi.MakeTLSMode(t.tlsModeType, t.NewTLSMode)
		err := vdbstatus.UpdateTLSModes(ctx, t.Rec.GetClient(), t.Vdb, []*vapi.TLSMode{mode})
		if err != nil {
			t.Log.Error(err, "failed to update tls mode in status after tls mode update in db")
			return err
		}
	}

	return nil
}

// setTLSConfig creates a tls config in the db , if it does not exist yet,
// and updates the status
func (t *TLSConfigManager) setTLSConfig(ctx context.Context, pfacts *podfacts.PodFacts) (ctrl.Result, error) {
	initiatorPod, ok := pfacts.FindFirstUpPod(false, "")
	if !ok {
		t.Log.Info("No pod found to run sql to set tls config. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	certificate, tlsMode, err := t.getTLSConfigFromDB(ctx, pfacts, initiatorPod)
	if err != nil {
		return ctrl.Result{}, err
	}
	// if tls config already exists in the db, we just
	// need to update the status
	if strings.Contains(certificate, t.getCertificatePrefix()) {
		err = t.setTLSConfigInStatus(ctx, tlsMode)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, t.setTLSConfigInDB(ctx, initiatorPod)
}

// setTLSConfigInDB creates a tls config in the database and update the status
func (t *TLSConfigManager) setTLSConfigInDB(ctx context.Context, initiatorPod *podfacts.PodFact) error {
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, events.SetTLSConfigStarted,
		"Starting set tls for %s with secret name %s and mode %s",
		t.TLSConfig, t.NewSecret, t.NewTLSMode)
	opts := []settlsconfig.Option{
		settlsconfig.WithTLSMode(t.NewTLSMode),
		settlsconfig.WithTLSSecretName(t.NewSecret),
		settlsconfig.WithInitiatorIP(initiatorPod.GetPodIP()),
		settlsconfig.WithNamespace(t.Vdb.GetNamespace()),
		settlsconfig.WithHTTPSTLSConfig(t.isHTTPSTLSConfig()),
	}
	err := t.Dispatcher.SetTLSConfig(ctx, opts...)
	if err != nil {
		t.Rec.Eventf(t.Vdb, corev1.EventTypeWarning, events.SetTLSConfigFailed,
			"Failed to set %s tls config with secret name %s and mode %s", t.TLSConfig, t.NewSecret, t.NewTLSMode)
		return err
	}
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, events.SetTLSConfigSucceeded,
		"Successfully set %s tls configwith secret name %s and mode %s", t.TLSConfig, t.NewSecret, t.NewTLSMode)

	return t.setTLSConfigInStatus(ctx, t.NewTLSMode)
}

// setTLSConfigInStatus updates the status with the current tls config in the db
func (t *TLSConfigManager) setTLSConfigInStatus(ctx context.Context, tlsMode string) error {
	mode := vapi.MakeTLSMode(t.tlsModeType, tlsMode)
	// TO DO: remove this once tls mode and secret are in the same struct
	if err := vdbstatus.UpdateTLSModes(ctx, t.Rec.GetClient(), t.Vdb, []*vapi.TLSMode{mode}); err != nil {
		return err
	}
	sRef := vapi.MakeSecretRef(t.secretType, t.NewSecret)
	if err := vdbstatus.UpdateSecretRef(ctx, t.Rec.GetClient(), t.Vdb, sRef); err != nil {
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
		started = events.HTTPSCertRotationStarted
		failed = events.HTTPSCertRotationFailed
		succeeded = events.HTTPSCertRotationSucceeded
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

func (t *TLSConfigManager) getK8sCertsConfig() (keyConfig, certConfig, caCertConfig string) {
	keyConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSPrivateKeyKey, t.Vdb.Namespace)
	certConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSCertKey, t.Vdb.Namespace)
	caCertConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", paths.HTTPServerCACrtName, t.Vdb.Namespace)
	return
}

func (t *TLSConfigManager) getAWSCertsConfig() (keyConfig, certConfig, caCertConfig string) {
	region, _ := secrets.GetAWSRegion(t.NewSecret)

	keyConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", corev1.TLSPrivateKeyKey, region)
	certConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", corev1.TLSCertKey, region)
	caCertConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", paths.HTTPServerCACrtName, region)
	return
}

func (t *TLSConfigManager) setTLSUpdatedata() {
	if t.TLSConfig == tlsConfigHTTPS {
		t.CurrentSecret = t.Vdb.GetHTTPSTLSSecretNameInUse()
		t.NewSecret = t.Vdb.Spec.HTTPSNMATLSSecret
		t.CurrentTLSMode = t.Vdb.GetHTTPSTLSModeInUse()
		t.NewTLSMode = t.Vdb.Spec.HTTPSTLSMode
		t.secretType = vapi.HTTPSTLSSecretType
		t.tlsModeType = vapi.HTTPSTLSModeType
	} else if t.TLSConfig == tlsConfigServer {
		t.CurrentSecret = t.Vdb.GetClientServerTLSSecretNameInUse()
		t.NewSecret = t.Vdb.Spec.ClientServerTLSSecret
		t.CurrentTLSMode = t.Vdb.GetClientServerTLSModeInUse()
		t.NewTLSMode = t.Vdb.Spec.ClientServerTLSMode
		t.secretType = vapi.ClientServerTLSSecretType
		t.tlsModeType = vapi.ClientServerTLSModeType
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
