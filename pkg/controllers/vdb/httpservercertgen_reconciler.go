/*
 (c) Copyright [2021-2023] Open Text.
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
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/security"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// HTTPServerCertGenReconciler will create a secret that has TLS credentials.  This
// secret will be used to authenticate with the http server.
type HTTPServerCertGenReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

func MakeHTTPServerCertGenReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &HTTPServerCertGenReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("HTTPServerCertGenReconciler"),
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *HTTPServerCertGenReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	const PKKeySize = 2048
	// Early out if the NMA isn't going to be used.
	if !vmeta.UseVClusterOps(h.Vdb.Annotations) {
		return ctrl.Result{}, nil
	}
	// If the secret name is set, check that it exists. As a convenience we will
	// regenerate the secret using the same name.
	if h.Vdb.Spec.NmaTLSSecret != "" {
		nm := names.GenNamespacedName(h.Vdb, h.Vdb.Spec.NmaTLSSecret)
		secret := corev1.Secret{}
		err := h.VRec.Client.Get(ctx, nm, &secret)
		if errors.IsNotFound(err) {
			h.Log.Info("httpServerTLSSecret is set but doesn't exist. Will recreate the secret.", "name", nm)
		} else if err != nil {
			return ctrl.Result{},
				fmt.Errorf("failed while attempting to reade the tls secret %s: %w", h.Vdb.Spec.NmaTLSSecret, err)
		} else {
			// Secret is filled in and exists. We can exit.
			return ctrl.Result{}, nil
		}
	}
	caCert, err := security.NewSelfSignedCACertificate(PKKeySize)
	if err != nil {
		return ctrl.Result{}, err
	}
	cert, err := security.NewCertificate(caCert, PKKeySize, "dbadmin", h.getDNSNames())
	if err != nil {
		return ctrl.Result{}, err
	}
	secret, err := h.createSecret(ctx, cert, caCert)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, h.setSecretNameInVDB(ctx, secret.ObjectMeta.Name)
}

// getDNSNames returns the DNS names to include in the certificate that we generate
func (h *HTTPServerCertGenReconciler) getDNSNames() []string {
	return []string{
		fmt.Sprintf("*.%s.svc", h.Vdb.Namespace),
		fmt.Sprintf("*.%s.svc.cluster.local", h.Vdb.Namespace),
	}
}

func (h *HTTPServerCertGenReconciler) createSecret(ctx context.Context, cert, caCert security.Certificate) (*corev1.Secret, error) {
	isController := true
	blockOwnerDeletion := false
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   h.Vdb.Namespace,
			Annotations: builder.MakeAnnotationsForObject(h.Vdb),
			Labels:      builder.MakeCommonLabels(h.Vdb, nil, false),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         vapi.GroupVersion.String(),
					Kind:               vapi.VerticaDBKind,
					Name:               h.Vdb.Name,
					UID:                h.Vdb.GetUID(),
					Controller:         &isController,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
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
	if h.Vdb.Spec.NmaTLSSecret == "" {
		secret.GenerateName = fmt.Sprintf("%s-http-server-tls-", h.Vdb.Name)
	} else {
		secret.Name = h.Vdb.Spec.NmaTLSSecret
	}
	err := h.VRec.Client.Create(ctx, &secret)
	return &secret, err
}

// setSecretNameInVDB will set the secretName in the vdb to indicate we have created that secret
func (h *HTTPServerCertGenReconciler) setSecretNameInVDB(ctx context.Context, secretName string) error {
	nm := h.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest in case we are in the retry loop
		if err := h.VRec.Client.Get(ctx, nm, h.Vdb); err != nil {
			return err
		}
		h.Vdb.Spec.NmaTLSSecret = secretName
		return h.VRec.Client.Update(ctx, h.Vdb)
	})
}
