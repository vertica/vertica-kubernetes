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
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	nmaTLSSecret          = "NMATLSSecret"
	httpsNMATLSSecret     = "HTTPSNMATLSSecret" //nolint:gosec
	clientServerTLSSecret = "ClientServerTLSSecret"
	interNodeTLSSecret    = "InterNodeTLSSecret" //nolint:gosec
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
	// Verify that at least one secret need generation
	// If not, skip this reconciler
	if !h.ShouldGenerateCert() || h.Vdb.IsTLSCertRollbackNeeded() || h.Vdb.GetHTTPSPollingCurrentRetries() > 0 {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, h.reconcileSecrets(ctx)
}

// reconcileSecrets will check three secrets: NMA secret, https secret, and client server secret
func (h *TLSServerCertGenReconciler) reconcileSecrets(ctx context.Context) error {
	secretStruct := []struct {
		Field string
		Name  string
	}{
		{clientServerTLSSecret, h.Vdb.GetClientServerTLSSecret()},
		{nmaTLSSecret, h.Vdb.Spec.NMATLSSecret},
		{httpsNMATLSSecret, h.Vdb.GetHTTPSNMATLSSecret()},
		{interNodeTLSSecret, h.Vdb.GetInterNodeTLSSecret()},
	}

	h.Log.Info("Starting TLS secret reconciliation", "secrets", secretStruct)

	var err error
	for _, s := range secretStruct {
		secretFieldName := s.Field
		secretName := s.Name
		if h.ShouldSkipThisConfig(secretFieldName) {
			continue
		}
		h.Log.Info("Reconciling TLS secret", "TLS secret", secretFieldName, "secretName", secretName)

		// when nma secret is not empty, we can assign it to https and/or clientserver TLS
		skipToNext, errInit := h.tryInitializeFromNMATLSSecret(ctx, secretFieldName, secretName)
		if errInit != nil {
			return err
		}
		if skipToNext {
			continue
		}

		// Initialize nmaTLSSecret for legacy compatibility
		skipToNext, errHandle := h.handleLegacyNMATLSSecret(ctx, secretFieldName)
		if errHandle != nil {
			return err
		}
		if skipToNext {
			continue
		}
		err = h.reconcileOneSecret(secretFieldName, secretName, ctx)
		if err != nil {
			h.Log.Error(err, fmt.Sprintf("failed to reconcile secret for %s", secretFieldName))
			return err
		}
	}
	return nil
}

// tryInitializeFromNMATLSSecret tries to initialize https/clientserver TLS secret from nmaTLSSecret
func (h *TLSServerCertGenReconciler) tryInitializeFromNMATLSSecret(ctx context.Context, secretFieldName, secretName string) (bool, error) {
	if h.Vdb.Spec.NMATLSSecret == "" || secretFieldName == nmaTLSSecret || secretFieldName == interNodeTLSSecret || secretName != "" {
		return false, nil
	}

	nm := names.GenNamespacedName(h.Vdb, h.Vdb.Spec.NMATLSSecret)
	var secret corev1.Secret
	err := h.VRec.Client.Get(ctx, nm, &secret)
	// Validate if nma secret can be used as TLS secret first
	if err != nil || h.ValidateSecretCertificate(ctx, &secret, vapi.NMATLSConfigName, h.Vdb.Spec.NMATLSSecret) != nil {
		return false, nil
	}

	h.Log.Info("TLS secret initialized from nmaTLSSecret", "TLS secret", secretFieldName)
	if err := h.setSecretNameInVDB(ctx, secretFieldName, h.Vdb.Spec.NMATLSSecret); err != nil {
		h.Log.Error(err, "Failed to initialize TLS secret from nmaTLSSecret", "TLS secret", secretFieldName)
		return false, err
	}
	return true, nil
}

// handleLegacyNMATLSSecret handles nmaTLSSecret initialization for legacy compatibility
func (h *TLSServerCertGenReconciler) handleLegacyNMATLSSecret(ctx context.Context, secretFieldName string) (bool, error) {
	if secretFieldName != nmaTLSSecret || h.Vdb.IsHTTPSNMATLSAuthEnabled() || !h.Vdb.IsDBInitialized() {
		return false, nil
	}

	if h.Vdb.Spec.HTTPSNMATLS != nil &&
		h.Vdb.Spec.HTTPSNMATLS.Secret != "" &&
		h.Vdb.Spec.NMATLSSecret == "" {
		// In older operator versions like v25.3.0, spec.nmaTLSSecret was deprecated.
		// We would always set spec.httpsNMATLS.secret even when tls is disabled.
		// If you upgrade to 25.4.0 or later with tls disabled, spec.nmaTLSSecret is back and is autogenerated
		// if empty.
		// The issue is when tls is disabled, nma in v25.3.0 got its secret from spec.httpsNMATLS.secret while
		// in v25.4.0+, it assumes the secret is in spec.nmaTLSSecret. To honore that,
		// instead of generating a new secret, we will copy spec.httpsNMATLS.secret to spec.nmaTLSSecret.
		h.Log.Info("Initializing nmaTLSSecret from httpsNMATLS.Secret for legacy compatibility")
		return true, h.setSecretNameInVDB(ctx, secretFieldName, h.Vdb.Spec.HTTPSNMATLS.Secret)
	}
	return false, nil
}

// reconcileOneSecret will create a TLS secret for the http server if one is missing
func (h *TLSServerCertGenReconciler) reconcileOneSecret(secretFieldName, secretName string,
	ctx context.Context) error {
	tlsConfigName := vapi.HTTPSNMATLSConfigName
	if secretFieldName == clientServerTLSSecret {
		tlsConfigName = vapi.ClientServerTLSConfigName
	}
	if secretFieldName == interNodeTLSSecret {
		tlsConfigName = vapi.InterNodeTLSConfigName
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
			if secretFieldName != nmaTLSSecret {
				tlsStatus := h.Vdb.GetTLSConfigByName(tlsConfigName)
				if tlsStatus != nil {
					// we do not recreate the secret as there is already
					// a secret of this type in the status.
					return h.InvalidCertRollback(ctx, "Validation of TLS Certificate %q failed; secret %q does not exist",
						tlsConfigName, secretName, err)
				}
			}
			h.Log.Error(err, secretName+" does not exist", "name", nm)
			return err
			// Secret found but could not be read
		} else if err != nil {
			h.Log.Error(err, "failed to read tls secret", "secretName", secretName)
			return err
			// Successfully read secret
		} else {
			// we do not need to verify nma tls secret
			if secretFieldName != nmaTLSSecret {
				// Validate secret certificate
				err = h.ValidateSecretCertificate(ctx, &secret, tlsConfigName, secretName)
				if err != nil {
					return err
				}
			}
			// Secret is filled in, exists, and is valid. We can exit.
			return err
		}
	}
	secret, err := h.createNewSecret(ctx, secretFieldName, secretName)
	if err != nil {
		return err
	}

	h.Log.Info(fmt.Sprintf("created certificate and secret %s for %s", secret.Name, secretFieldName))
	return h.setSecretNameInVDB(ctx, secretFieldName, secret.ObjectMeta.Name)
}

func (h *TLSServerCertGenReconciler) createNewSecret(ctx context.Context, secretFieldName, secretName string) (*corev1.Secret, error) {
	caCert, err := security.NewSelfSignedCACertificate()
	if err != nil {
		return nil, err
	}
	cert, err := security.NewCertificate(caCert, h.Vdb.GetVerticaUser(), h.getDNSNames())
	if err != nil {
		return nil, err
	}
	secret, err := h.createSecret(secretFieldName, secretName, ctx, cert, caCert)
	if err != nil {
		return nil, err
	}
	return secret, nil
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
		switch secretFieldName {
		case httpsNMATLSSecret:
			secret.GenerateName = fmt.Sprintf("%s-https-tls-", h.Vdb.Name)
		case clientServerTLSSecret:
			secret.GenerateName = fmt.Sprintf("%s-clientserver-tls-", h.Vdb.Name)
		case nmaTLSSecret:
			secret.GenerateName = fmt.Sprintf("%s-nma-tls-", h.Vdb.Name)
		case interNodeTLSSecret:
			secret.GenerateName = fmt.Sprintf("%s-internode-tls-", h.Vdb.Name)
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
		switch secretFieldName {
		case clientServerTLSSecret:
			if h.Vdb.Spec.ClientServerTLS == nil {
				return errors.New("ClientServerTLS is not enabled but trying to set secret")
			}
			h.Vdb.Spec.ClientServerTLS.Secret = secretName
		case interNodeTLSSecret:
			if h.Vdb.Spec.InterNodeTLS == nil {
				return errors.New("InterNodeTLS is not enabled but trying to set secret")
			}
			h.Vdb.Spec.InterNodeTLS.Secret = secretName
		case httpsNMATLSSecret:
			if h.Vdb.Spec.HTTPSNMATLS == nil {
				return errors.New("HTTPSNMATLS is not enabled but trying to set secret")
			}
			h.Vdb.Spec.HTTPSNMATLS.Secret = secretName
		case nmaTLSSecret:
			h.Vdb.Spec.NMATLSSecret = secretName
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

	// Check if secret exists
	nm := names.GenNamespacedName(h.Vdb, secretName)
	err := h.VRec.Client.Get(ctx, nm, secret)
	if kerrors.IsNotFound(err) {
		err1 := h.InvalidCertRollback(ctx, "Validation of TLS Certificate %q failed; secret %q does not exist", tlsConfigName, secretName, err)
		if err1 != nil || h.Vdb.IsTLSCertRollbackNeeded() {
			return err1
		}
		return err
	}

	certPEM := secret.Data[TLSCertName]
	if certPEM == nil {
		return errors.New("failed to decode PEM block containing certificate")
	}
	keyPEM := secret.Data[TLSKeyName]
	if keyPEM == nil {
		return errors.New("failed to decode PEM block containing key")
	}

	err = security.ValidateTLSSecret(certPEM, keyPEM)
	if err != nil {
		err1 := h.InvalidCertRollback(ctx, "Validation of TLS Certificate %q failed with secret %q", tlsConfigName, secretName, err)
		if err1 != nil || h.Vdb.IsTLSCertRollbackNeeded() {
			return err1
		}
		return err
	}

	err = security.ValidateCertificateCommonName(certPEM, h.Vdb.GetExpectedCertCommonName(tlsConfigName))
	if err != nil {
		err1 := h.InvalidCertRollback(ctx, "Validation of common name for TLS Certificate %q failed with secret %q",
			tlsConfigName, secretName, err)
		if err1 != nil || h.Vdb.IsTLSCertRollbackNeeded() {
			return err1
		}
		return err
	}

	expiringSoon, expireTime, err := security.CheckCertificateExpiringSoon(certPEM)

	if err != nil {
		return err
	}

	if expiringSoon {
		h.Log.Info("certificate is nearing expiration, consider regenerating", "expiresAt", expireTime.UTC().Format(time.RFC3339)+" UTC")
	}

	h.Log.Info("successfully completed validating TLS certificate for existing secret", "secretName", secretName)
	return nil
}

// ShouldGenerateCert determines whether generating TLS server certificates should run at all.
// Returns true if any secret (NMA, HTTPS, or Server) needs to be generated.
func (h *TLSServerCertGenReconciler) ShouldGenerateCert() bool {
	httpsNMACertNeeded := h.Vdb.ShouldGenCertForTLSConfig(vapi.HTTPSNMATLSConfigName)
	clientServerCertNeeded := h.Vdb.ShouldGenCertForTLSConfig(vapi.ClientServerTLSConfigName)
	interNodeCertNeeded := h.Vdb.ShouldGenCertForTLSConfig(vapi.InterNodeTLSConfigName)
	nmaCertNeeded := !h.Vdb.IsHTTPSNMATLSAuthEnabled() && h.Vdb.Spec.NMATLSSecret == ""
	return httpsNMACertNeeded || clientServerCertNeeded || interNodeCertNeeded || nmaCertNeeded
}

// ShouldSkipThisConfig determines whether an individual config (NMA, HTTPS, or Server) should skipped.
func (h *TLSServerCertGenReconciler) ShouldSkipThisConfig(secretFieldName string) bool {
	if secretFieldName == nmaTLSSecret && h.Vdb.IsHTTPSNMATLSAuthEnabled() {
		h.Log.Info("TLS auth is enabled. Skipping NMA secret validation and generation")
		return true
	}
	if secretFieldName == httpsNMATLSSecret && !h.Vdb.IsHTTPSNMATLSAuthEnabled() {
		h.Log.Info("HTTPS TLS config disabled. Skipping secret validation and generation")
		return true
	}
	if secretFieldName == clientServerTLSSecret && !h.Vdb.IsClientServerTLSAuthEnabled() {
		h.Log.Info("Client-server TLS config disabled. Skipping secret validation and generation")
		return true
	}
	if secretFieldName == interNodeTLSSecret && !h.Vdb.IsInterNodeTLSAuthEnabled() {
		h.Log.Info("InterNode TLS config disabled. Skipping secret validation and generation")
		return true
	}
	return false
}

// InvalidCertRollback handles failures in cert validation, by producing an event and (if relevant) trigerring rollback
func (h *TLSServerCertGenReconciler) InvalidCertRollback(ctx context.Context, message, tlsConfigName, secretName string,
	originalErr error) error {
	h.VRec.Eventf(h.Vdb, corev1.EventTypeWarning, events.TLSCertValidationFailed, message, tlsConfigName, secretName)

	if h.Vdb.GetTLSConfigByName(tlsConfigName) == nil || h.Vdb.GetTLSConfigByName(tlsConfigName).Secret == "" ||
		h.Vdb.GetTLSConfigSpecByName(tlsConfigName) == nil || h.Vdb.GetTLSConfigSpecByName(tlsConfigName).Secret == "" {
		// No cert was configured, so no need to do rollback
		return originalErr
	}

	if h.Vdb.IsTLSCertRollbackEnabled() {
		reason := vapi.FailureBeforeHTTPSCertHealthPollingReason
		switch tlsConfigName {
		case vapi.ClientServerTLSConfigName:
			reason = vapi.RollbackAfterServerCertRotationReason
		case vapi.InterNodeTLSConfigName:
			reason = vapi.RollbackAfterInterNodeCertRotationReason
		}
		cond := vapi.MakeCondition(vapi.TLSCertRollbackNeeded, metav1.ConditionTrue, reason)
		if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
			return err
		}
	}

	// Run rollback now; otherwise, we will run all subsequent reconcilers with invalid cert.
	// Podfacts and dispatcher can be nil, since they are only required when re-running cert rotation
	h.Log.Info("Running rollback due to invalid cert", "tlsConfigName", tlsConfigName, "secretName",
		h.Vdb.GetTLSConfigSpecByName(tlsConfigName).Secret)
	rollbackRecon := MakeRollbackAfterCertRotationReconciler(h.VRec, h.Log, h.Vdb, nil, nil)
	_, err := rollbackRecon.Reconcile(ctx, nil)
	h.Log.Info("Finished running rollback", "tlsConfigName", tlsConfigName, "secretName",
		h.Vdb.GetTLSConfigSpecByName(tlsConfigName).Secret)
	return err
}
