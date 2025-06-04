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
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	tlsConfigServer = "Server"
	tlsConfigHTTPS  = "HTTP"
)

type TLSUpdateData struct {
	NewTLSMode     string
	CurrentTLSMode string
	NewSecret      string
	CurrentSecret  string
	secretType     string
	tlsModeType    string
}

type TLSPollingCertMetadata struct {
	Key    string
	Cert   string
	CACert string
}

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

// buildHTTPSTLSUpdateData constructs the data needed for tls update
func (t *TLSConfigManager) setPollingCertMetadata(ctx context.Context) (ctrl.Result, error) {
	var currentSecretData map[string][]byte
	var newSecretData map[string][]byte
	var res ctrl.Result
	var err error

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
		"Starting https cert rotation with secret name %s and mode %s",
		t.NewSecret, t.CurrentTLSMode)
	err := t.Dispatcher.RotateHTTPSCerts(ctx, opts...)
	if err != nil {
		t.Rec.Eventf(t.Vdb, corev1.EventTypeWarning, failed,
			"Failed to rotate https cert with secret name %s and mode %s", t.NewSecret, t.NewTLSMode)
		return err
	}
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, succeeded,
		"Successfully rotated https cert with secret name %s and mode %s", t.NewSecret, t.NewTLSMode)

	return err
}

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

func (t *TLSConfigManager) setTLSConfig(ctx context.Context, pfacts *podfacts.PodFacts) (ctrl.Result, error) {
	tlsMode, res, err := t.getTLSConfigFromDB(ctx, pfacts)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	if tlsMode != "" {
		err = t.setTLSConfigInStatus(ctx, tlsMode)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, t.setTLSConfigInDB()
}

func (t *TLSConfigManager) setTLSConfigInDB() error {
	// To Do
	return nil
}

func (t *TLSConfigManager) setTLSConfigInStatus(ctx context.Context, tlsMode string) error {
	mode := vapi.MakeTLSMode(t.tlsModeType, tlsMode)
	// TO DO: remove this once tls mode and secret are i the same struct
	if err := vdbstatus.UpdateTLSModes(ctx, t.Rec.GetClient(), t.Vdb, []*vapi.TLSMode{mode}); err != nil {
		return err
	}
	sRef := vapi.MakeSecretRef(t.secretType, t.NewSecret)
	if err := vdbstatus.UpdateSecretRef(ctx, t.Rec.GetClient(), t.Vdb, sRef); err != nil {
		return err
	}

	return nil
}

func (t *TLSConfigManager) getTLSConfigFromDB(ctx context.Context, pfacts *podfacts.PodFacts) (string, ctrl.Result, error) {
	initiatorPod, ok := pfacts.FindFirstUpPod(false, "")
	if !ok {
		t.Log.Info("No pod found to run sql to get tls config. Requeue reconciliation.")
		return "", ctrl.Result{Requeue: true}, nil
	}
	sql := fmt.Sprintf("select mode from tls_configurations where name='%s';", t.TLSConfig)
	cmd := []string{"-tAc", sql}
	t.Log.Info("Getting tls config from db", "tlsConfig", t.TLSConfig)
	stdout, stderr, err := pfacts.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	if err != nil || strings.Contains(stderr, "Error") {
		t.Log.Error(err, fmt.Sprintf("failed to retrieve %s TLS config from db, stderr - %s", t.TLSConfig, stderr))
		return "", ctrl.Result{}, err
	}

	return t.parseTLSMode(stdout), ctrl.Result{}, nil
}

func (t *TLSConfigManager) isClientServerTLSConfig() bool {
	return t.TLSConfig == tlsConfigServer
}

func (t *TLSConfigManager) parseTLSMode(stdout string) string {
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return res
}

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
