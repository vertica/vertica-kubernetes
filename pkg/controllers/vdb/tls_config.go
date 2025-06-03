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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
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
	Key     string
	Cert    string
	CACert  string
	TLSMode string
}

type TLSConfigManager struct {
	Rec           config.ReconcilerInterface
	Vdb           *vapi.VerticaDB
	Log           logr.Logger
	TLSConfig     string
	Dispatcher    vadmin.Dispatcher
	TLSData       *TLSUpdateData
	TLSUpdateType int
}

func MakeTLSConfigManager(recon config.ReconcilerInterface, log logr.Logger, vdb *vapi.VerticaDB,
	tlsConfig string, dispatcher vadmin.Dispatcher) *TLSConfigManager {
	return &TLSConfigManager{
		Rec:        recon,
		Vdb:        vdb,
		Log:        log,
		TLSConfig:  tlsConfig,
		Dispatcher: dispatcher,
	}
}

// buildHTTPSTLSUpdateData constructs the data needed for tls update
func (t *TLSConfigManager) setHTTPSTLSUpdateData(ctx context.Context) (ctrl.Result, error) {
	t.setTLSUpdateType()
	var currentSecretData map[string][]byte
	var newSecretData map[string][]byte
	var res ctrl.Result
	var err error
	t.TLSData = &TLSUpdateData{}

	_, t.TLSData.TLSMode = t.getTLSModes()

	currentSecretName, newSecretName := t.getSecrets()
	currentSecretData, res, err = readSecret(t.Vdb, t.Rec, t.Rec.GetClient(), t.Log, ctx, currentSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	if t.TLSUpdateType != tlsModeChangeOnly {
		newSecretData, res, err = readSecret(t.Vdb, t.Rec, t.Rec.GetClient(), t.Log, ctx, newSecretName)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		t.TLSData.Key = string(newSecretData[corev1.TLSPrivateKeyKey])
		t.TLSData.Cert = string(newSecretData[corev1.TLSCertKey])
		t.TLSData.CACert = string(newSecretData[corev1.ServiceAccountRootCAKey])
		return res, err
	}

	t.TLSData.Key = string(currentSecretData[corev1.TLSPrivateKeyKey])
	t.TLSData.Cert = string(currentSecretData[corev1.TLSCertKey])
	t.TLSData.CACert = string(currentSecretData[corev1.ServiceAccountRootCAKey])

	return res, err
}

func (t *TLSConfigManager) updateTLSConfig(ctx context.Context, initiatorIP string) error {
	currentSecret, newSecret := t.getSecrets()
	currentMode, newMode := t.getTLSModes()
	if t.TLSUpdateType == tlsModeAndCertChange || t.TLSUpdateType == httpsCertChangeOnly {
		t.Log.Info(fmt.Sprintf("ready to rotate %s cert from %s to %s", t.TLSConfig, currentSecret, newSecret))
	}
	if t.TLSUpdateType == tlsModeAndCertChange || t.TLSUpdateType == tlsModeChangeOnly {
		t.Log.Info(fmt.Sprintf("ready to change %s TLS mode from %s to %s", t.TLSConfig, currentMode, newMode))
	}

	var keyConfig, certConfig, caCertConfig, secretName string

	switch {
	case secrets.IsAWSSecretsManagerSecret(newSecret):
		keyConfig, certConfig, caCertConfig = t.getAWSCertsConfig()
		secretName = secrets.RemovePathReference(newSecret)
	default:
		keyConfig, certConfig, caCertConfig = t.getK8sCertsConfig()
		secretName = newSecret
	}
	opts := []rotatehttpscerts.Option{
		rotatehttpscerts.WithPollingKey(t.TLSData.Key),
		rotatehttpscerts.WithPollingCert(t.TLSData.Cert),
		rotatehttpscerts.WithPollingCaCert(t.TLSData.CACert),
		rotatehttpscerts.WithKey(secretName, keyConfig),
		rotatehttpscerts.WithCert(secretName, certConfig),
		rotatehttpscerts.WithCaCert(secretName, caCertConfig),
		rotatehttpscerts.WithTLSMode(t.TLSData.TLSMode),
		rotatehttpscerts.WithInitiator(initiatorIP),
		rotatehttpscerts.WithTLSConfig(t.TLSConfig),
	}
	started, failed, succeeded := t.getEvents()
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, started,
		"Starting https cert rotation with secret name %s and mode %s",
		newSecret, t.TLSData.TLSMode)
	err := t.Dispatcher.RotateHTTPSCerts(ctx, opts...)
	if err != nil {
		t.Rec.Eventf(t.Vdb, corev1.EventTypeWarning, failed,
			"Failed to rotate https cert with secret name %s and mode %s", newSecret, t.TLSData.TLSMode)
		return err
	}
	t.Rec.Eventf(t.Vdb, corev1.EventTypeNormal, succeeded,
		"Successfully rotated https cert with secret name %s and mode %s", newSecret, t.TLSData.TLSMode)

	return err
}

func (t *TLSConfigManager) updateTLSModeInStatus(ctx context.Context) error {
	currentMode, newMode := t.getTLSModes()
	if currentMode != newMode {
		err := vdbstatus.UpdateTLSModes(ctx, t.Rec.GetClient(), t.Vdb, []*vapi.TLSMode{t.genTLSMode()})
		if err != nil {
			t.Log.Error(err, "failed to update tls mode in status after tls mode update in db")
			return err
		}
	}

	return nil
}

func (t *TLSConfigManager) setTLSConfig() error {
	// TO DO
	return nil
}

func (t *TLSConfigManager) genTLSMode() *vapi.TLSMode {
	if t.TLSConfig == tlsConfigHTTPS {
		return vapi.MakeHTTPSTLSMode(t.Vdb.Spec.HTTPSTLSMode)
	} else if t.TLSConfig == tlsConfigServer {
		return vapi.MakeClientServerTLSMode(t.Vdb.Spec.ClientServerTLSMode)
	}

	return nil
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

func (t *TLSConfigManager) getTLSModes() (currentMode, newMode string) {
	if t.TLSConfig == tlsConfigHTTPS {
		currentMode = t.Vdb.GetHTTPSTLSModeInUse()
		newMode = t.Vdb.Spec.HTTPSTLSMode
	} else if t.TLSConfig == tlsConfigServer {
		currentMode = t.Vdb.GetClientServerTLSModeInUse()
		newMode = t.Vdb.Spec.ClientServerTLSMode
	}

	return
}

func (t *TLSConfigManager) getSecrets() (currentSecret, newSecret string) {
	if t.TLSConfig == tlsConfigHTTPS {
		currentSecret = t.Vdb.GetHTTPSTLSSecretNameInUse()
		newSecret = t.Vdb.Spec.HTTPSNMATLSSecret
	} else if t.TLSConfig == tlsConfigServer {
		currentSecret = t.Vdb.GetClientServerTLSModeInUse()
		newSecret = t.Vdb.Spec.ClientServerTLSSecret
	}

	return
}

func (t *TLSConfigManager) setTLSUpdateType() {
	currentSecretName, newSecretName := t.getSecrets()
	t.Log.Info("Determining TLS update type", "currentSecretName", currentSecretName, "newSecretName", newSecretName)

	certChanged := currentSecretName != "" && newSecretName != currentSecretName
	currentMode, newMode := t.getTLSModes()
	modeChanged := newMode != currentMode

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

func (t *TLSConfigManager) getK8sCertsConfig() (keyConfig, certConfig, caCertConfig string) {
	keyConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSPrivateKeyKey, t.Vdb.Namespace)
	certConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSCertKey, t.Vdb.Namespace)
	caCertConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", paths.HTTPServerCACrtName, t.Vdb.Namespace)
	return
}

func (t *TLSConfigManager) getAWSCertsConfig() (keyConfig, certConfig, caCertConfig string) {
	_, secret := t.getSecrets()
	region, _ := secrets.GetAWSRegion(secret)

	keyConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", corev1.TLSPrivateKeyKey, region)
	certConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", corev1.TLSCertKey, region)
	caCertConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", paths.HTTPServerCACrtName, region)
	return
}
