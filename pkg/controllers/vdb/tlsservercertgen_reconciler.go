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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"github.com/vertica/vertica-kubernetes/pkg/security"
	corev1 "k8s.io/api/core/v1"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	httpsNMATLSSecret     = "HTTPSNMATLSSecret" //nolint:gosec
	clientServerTLSSecret = "ClientServerTLSSecret"
	TLSCertName           = "tls.crt"
	TLSKeyName            = "tls.key"
)

// TLSServerCertGenReconciler will create a secret that has TLS credentials.  This
// secret will be used to authenticate with the https server.
type TLSServerCertGenReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

func MakeTLSServerCertGenReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &TLSServerCertGenReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("TLSServerCertGenReconciler"),
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSServerCertGenReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// Verify that at least one secret has changed
	// If not, skip this reconciler
	if !h.ShouldGenerateCert() {
		return ctrl.Result{}, nil
	}

	if h.Vdb.Spec.NMATLSSecret != "" && h.Vdb.GetHTTPSNMATLSSecret() == "" {
		h.Log.Info("httpsNMATLS.secret is initialized from nmaTLSSecret")
		err := h.setSecretNameInVDB(ctx, httpsNMATLSSecret, h.Vdb.Spec.NMATLSSecret)
		if err != nil {
			h.Log.Error(err, "failed to initialize httpsNMATLS.secret from nmaTLSSecret")
			return ctrl.Result{}, err
		}
	}
	secretFieldNameMap := map[string]string{
		httpsNMATLSSecret:     h.Vdb.GetHTTPSNMATLSSecret(),
		clientServerTLSSecret: h.Vdb.GetClientServerTLSSecret(),
	}
	err := error(nil)
	for secretFieldName, secretName := range secretFieldNameMap {
		err = h.reconcileOneSecret(secretFieldName, secretName, ctx)
		if err != nil {
			h.Log.Error(err, fmt.Sprintf("failed to reconcile secret for %s", secretFieldName))
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, err
}

// reconcileOneSecret will create a TLS secret for the http server if one is missing
func (h *TLSServerCertGenReconciler) reconcileOneSecret(secretFieldName, secretName string,
	ctx context.Context) error {
	tlsConfigName := vapi.HTTPSNMATLSConfigName
	if secretFieldName == clientServerTLSSecret {
		tlsConfigName = vapi.ClientServerTLSConfigName
	}
	// If the secret name is set, check that it exists.
	if secretName != "" {
		// As a convenience we will regenerate the secret using the same name. But
		// only do this if it is a k8s secret. We skip if there is a path reference
		// for a different secret store.
		if !secrets.IsK8sSecret(secretName) {
			h.Log.Info(secretName + " is set but uses a path reference that isn't for k8s.")
			return nil
		}
		nm := names.GenNamespacedName(h.Vdb, secretName)
		secret := corev1.Secret{}
		err := h.VRec.Client.Get(ctx, nm, &secret)
		// Secret defined but not found
		if kerrors.IsNotFound(err) {
			tlsStatus := h.Vdb.GetTLSConfigByName(tlsConfigName)
			if tlsStatus != nil {
				// we do not recreate the secret as there is already
				// a secret of this type in the status.
				return nil
			}
			h.Log.Info(secretName+" is set but doesn't exist. Will recreate the secret.", "name", nm)
			// Secret found but could not be read
		} else if err != nil {
			h.Log.Error(err, "failed to read tls secret", "secretName", secretName)
			return err
			// Successfully read secret
		} else {
			// Validate secret certificate
			err = h.ValidateSecretCertificate(ctx, &secret, tlsConfigName, secretName)
			if err != nil {
				return err
			}
			// Secret is filled in, exists, and is valid. We can exit.
			return err
		}
	}
	caCert, err := security.NewSelfSignedCACertificate()
	if err != nil {
		return err
	}
	cert, err := security.NewCertificate(caCert, h.Vdb.GetVerticaUser(), h.getDNSNames())
	if err != nil {
		return err
	}
	secret, err := h.createSecret(secretFieldName, secretName, ctx, cert, caCert)
	if err != nil {
		return err
	}
	if err := h.ValidateSecretCertificate(ctx, secret, tlsConfigName, secretName); err != nil {
		return err
	}

	h.Log.Info(fmt.Sprintf("created certificate and secret %s for %s", secret.Name, secretFieldName))
	return h.setSecretNameInVDB(ctx, secretFieldName, secret.ObjectMeta.Name)
}

// getDNSNames returns the DNS names to include in the certificate that we generate
func (h *TLSServerCertGenReconciler) getDNSNames() []string {
	return []string{
		fmt.Sprintf("*.%s.svc", h.Vdb.Namespace),
		fmt.Sprintf("*.%s.svc.cluster.local", h.Vdb.Namespace),
	}
}

// createSecret returns a secret that store TLS certificate information
func (h *TLSServerCertGenReconciler) createSecret(secretFieldName, secretName string, ctx context.Context, cert,
	caCert security.Certificate) (*corev1.Secret, error) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       h.Vdb.Namespace,
			Annotations:     builder.MakeAnnotationsForObject(h.Vdb),
			Labels:          builder.MakeCommonLabels(h.Vdb, nil, false, false),
			OwnerReferences: []metav1.OwnerReference{h.Vdb.GenerateOwnerReference()},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey:   cert.TLSKey(),
			corev1.TLSCertKey:         cert.TLSCrt(),
			paths.HTTPServerCACrtName: caCert.TLSCrt(),
		},
	}
	// Either generate a name or use the one already present in the vdb. Using
	// the name already present is the case where the name was filled in but the
	// secret didn't exist.
	if secretName == "" {
		if secretFieldName == httpsNMATLSSecret {
			secret.GenerateName = fmt.Sprintf("%s-https-tls-", h.Vdb.Name)
		} else if secretFieldName == clientServerTLSSecret {
			secret.GenerateName = fmt.Sprintf("%s-clientserver-tls-", h.Vdb.Name)
		}
	} else {
		secret.Name = secretName
	}
	err := h.VRec.Client.Create(ctx, &secret)
	return &secret, err
}

// setSecretNameInVDB will set the secretName in the vdb to indicate we have created that secret
func (h *TLSServerCertGenReconciler) setSecretNameInVDB(ctx context.Context, secretFieldName, secretName string) error {
	nm := h.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest in case we are in the retry loop
		if err := h.VRec.Client.Get(ctx, nm, h.Vdb); err != nil {
			return err
		}
		if secretFieldName == clientServerTLSSecret {
			h.Vdb.Spec.ClientServerTLS = &vapi.TLSConfigSpec{Secret: secretName, Mode: h.Vdb.GetSpecClientServerTLSMode()}
		} else if secretFieldName == httpsNMATLSSecret {
			h.Vdb.Spec.HTTPSNMATLS = &vapi.TLSConfigSpec{Secret: secretName, Mode: h.Vdb.GetSpecHTTPSNMATLSMode()}
		}
		return h.VRec.Client.Update(ctx, h.Vdb)
	})
}

// Validate that Secret contains a valid certificate
// If certificate is expiring soon, alert user
func (h *TLSServerCertGenReconciler) ValidateSecretCertificate(
	ctx context.Context,
	secret *corev1.Secret,
	tlsConfigName string,
	secretName string,
) error {
	h.Log.Info("validating TLS certificate for existing secret", "secretName", secretName)
	certPEM := secret.Data[TLSCertName]
	if certPEM == nil {
		return errors.New("failed to decode PEM block containing certificate")
	}
	keyPEM := secret.Data[TLSKeyName]
	if keyPEM == nil {
		return errors.New("failed to decode PEM block containing key")
	}

	err := security.ValidateTLSSecret(certPEM, keyPEM)
	if err != nil {
		h.VRec.Eventf(h.Vdb, corev1.EventTypeWarning, events.TLSCertValidationFailed,
			"Validation of TLS Certificate %q failed with secret %q", tlsConfigName, secretName)
		return err
	}

	err = security.ValidateCertificateCommonName(certPEM, h.Vdb.GetExpectedCertCommonName(tlsConfigName))
	if err != nil {
		h.VRec.Eventf(h.Vdb, corev1.EventTypeWarning, events.TLSCertValidationFailed,
			"Validation of common name for TLS Certificate %q failed with secret %q", tlsConfigName, secretName)
		return err
	}

	expiringSoon, expireTime, err := security.CheckCertificateExpiringSoon(certPEM)

	if err != nil {
		return err
	}

	if expiringSoon {
		h.Log.Info("certificate is nearing expiration, consider regenerating", "expiresAt", expireTime.UTC().Format(time.RFC3339)+" UTC")
	}

	return nil
}

// ShouldGenerateCert determines whether TLS server certificates should be generated.
// Returns true if either TLS config is missing in status or the expected secret differs from what's currently recorded.
func (h *TLSServerCertGenReconciler) ShouldGenerateCert() bool {
	return h.Vdb.GetTLSConfigByName(vapi.HTTPSNMATLSConfigName) == nil ||
		h.Vdb.GetTLSConfigByName(vapi.ClientServerTLSConfigName) == nil ||
		h.Vdb.GetHTTPSNMATLSSecretInUse() != h.Vdb.GetHTTPSNMATLSSecret() ||
		h.Vdb.GetClientServerTLSSecretInUse() != h.Vdb.GetClientServerTLSSecret()
}
