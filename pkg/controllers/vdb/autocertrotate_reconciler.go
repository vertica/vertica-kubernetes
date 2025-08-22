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
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	ctrl "sigs.k8s.io/controller-runtime"
)

type AutoCertRotateReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
	Init bool
}

func MakeAutoCertRotateReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, init bool) controllers.ReconcileActor {
	return &AutoCertRotateReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("AutoCertRotateReconciler"),
		Init: init,
	}
}

func (r *AutoCertRotateReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// No-op if auto-rotate is not enabled at all
	if !r.Vdb.IsAutoCertRotationEnabled(vapi.ClientServerTLSConfigName) && !r.Vdb.IsAutoCertRotationEnabled(vapi.HTTPSNMATLSConfigName) {
		return ctrl.Result{}, nil
	}

	// Check HTTPS/NMA auto-rotate
	httpsRes, err := r.autoRotateByTLSConfig(ctx, vapi.HTTPSNMATLSConfigName)
	if err != nil {
		return httpsRes, err
	}

	// Check Client-Server auto-rotate
	clientServerRes, err := r.autoRotateByTLSConfig(ctx, vapi.ClientServerTLSConfigName)
	if err != nil {
		return clientServerRes, err
	}

	// Compare results; requeue for shortest time
	return r.mergeResults(httpsRes, clientServerRes), nil
}

// autoRotateByTLSConfig will check if a certain TLS config (httpsNMA or clientServer) requires auto-rotation.
// If it is set for auto-rotate and the time to rotate has not passed, it will requeue for that time. If it
// is set for auto-rotate and the time to rotate has passed, it will trigger rotate and (if relevant) requeue
// for next rotation.
func (r *AutoCertRotateReconciler) autoRotateByTLSConfig(ctx context.Context, tlsConfig string) (ctrl.Result, error) {
	// no-op if auto-rotate disabled
	if !r.Vdb.IsAutoCertRotationEnabled(tlsConfig) {
		return ctrl.Result{}, nil
	}

	// If next update is not set, no auto-rotate is scheduled. This is likely right after auto-rotate has been
	// first set up. So, set first secret and configure status.
	nextUpdate := r.Vdb.GetTLSNextUpdate(tlsConfig)
	if nextUpdate == nil || len(r.Vdb.GetAutoRotateSecrets(tlsConfig)) == 0 {
		r.Log.Info("Initializing TLS auto-rotation", "tlsConfig", tlsConfig)
		return r.initializeAutoRotate(ctx, tlsConfig)
	}

	// exit if we are here for initialization
	if r.Init {
		return ctrl.Result{}, nil
	}

	// If spec secret list does not match status secret list, user must have updated the spec.
	// There are two scenarios here:
	//   1) If current secret is still in list, just continue rotating from its new position
	//   2) If current secret is not in list, restart auto-rotate from scratch
	specSecrets := r.Vdb.GetTLSConfigAutoRotate(tlsConfig).Secrets
	if !r.Vdb.EqualStringSlices(specSecrets, r.Vdb.GetTLSConfigByName(tlsConfig).AutoRotateSecrets) {
		current := r.Vdb.GetSecretInUse(tlsConfig)
		if r.findSecretInList(current, specSecrets) == -1 {
			r.Log.Info("Spec secret list changed and current secret is missing. Restarting auto-rotate.")
			return r.initializeAutoRotate(ctx, tlsConfig)
		} else {
			r.Log.Info("Spec secret list changed, preserving current rotation position", "currentSecret", current)
			return r.patchAutoRotateSecretsInStatus(ctx, tlsConfig)
		}
	}

	// Check if we are after nextUpdate.
	// Since this can take a long time, for testing purposes, we have added an annotation to
	// automatically trigger the auto-rotation now.
	if time.Until(nextUpdate.Time) <= 0 || r.Vdb.Annotations[vmeta.TriggerAutoTLSRotateAnnotation] != "" {
		return r.rotateToNextTLSSecret(ctx, tlsConfig)
	}

	// Otherwise, requeue to next update
	requeueAfter := time.Until(nextUpdate.Time)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// initializeAutoRotate will handle when auto-rotation is first set.
// We will get the secret list from the spec, copy to the status, then rotate to the first secret.
func (r *AutoCertRotateReconciler) initializeAutoRotate(ctx context.Context, tlsConfig string) (ctrl.Result, error) {
	secrets := r.Vdb.GetTLSConfigAutoRotate(tlsConfig).Secrets
	return r.rotateToSecret(ctx, tlsConfig, secrets, secrets[0])
}

// rotateToNextTLSSecret will handle rotation within an existing list of secrets, found in the status.
// It will find the current secret in the list then rotate the secret after it.
// If the current secret is last, we will produce an error, unless restartAtEnd is true.
func (r *AutoCertRotateReconciler) rotateToNextTLSSecret(ctx context.Context, tlsConfig string) (ctrl.Result, error) {
	secrets := r.Vdb.GetTLSConfigByName(tlsConfig).AutoRotateSecrets
	current := r.Vdb.GetSecretInUse(tlsConfig)

	// Status list should never be empty here; if it is, something went wrong.
	if len(secrets) == 0 {
		r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSAutoRotateFailed,
			"No auto-rotation secret list found for TLS Config %s", tlsConfig)
		return ctrl.Result{}, nil
	}

	// Find current secret in status secret list
	secretIndex := r.findSecretInList(current, secrets)

	// If current secret is not in list, the secret was somehow changed outside the auto-rotation.
	// This should never happen but, if it does, produce an error.
	if secretIndex == -1 {
		r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSAutoRotateFailed,
			"Current secret %s not found in auto-rotation secrets for TLS Config %s", current, tlsConfig)
		return ctrl.Result{}, nil
	}

	// If current secret is last and restartAtEnd is true, begin at the start again;
	// if it is false, error.
	if secretIndex+1 >= len(secrets) {
		if r.Vdb.GetTLSConfigAutoRotate(tlsConfig).RestartAtEnd {
			r.Log.Info("Restarting TLS auto-rotation", "tlsConfig", tlsConfig)
			return r.rotateToSecret(ctx, tlsConfig, secrets, secrets[0])
		} else {
			r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSAutoRotateFailed,
				"No remaining auto-rotation secrets for TLS Config %s", tlsConfig)
			return ctrl.Result{}, nil
		}
	}

	// Rotate to next secret in list
	r.Log.Info("Auto-rotating to next TLS secret", "tlsConfig", tlsConfig)
	return r.rotateToSecret(ctx, tlsConfig, secrets, secrets[secretIndex+1])
}

// rotateToSecret will update the secret in the VDB spec, to trigger a rotation on the next iteration.
// It will also update the auto-rotate fields in the status, such as nextRotate and autoRotateSecrets.
func (r *AutoCertRotateReconciler) rotateToSecret(
	ctx context.Context, tlsConfig string, secrets []string, secretToRotateTo string,
) (ctrl.Result, error) {
	now := time.Now()

	// Update spec to trigger cert rotation
	switch tlsConfig {
	case vapi.ClientServerTLSConfigName:
		r.Vdb.Spec.ClientServerTLS.Secret = secretToRotateTo
	case vapi.HTTPSNMATLSConfigName:
		r.Vdb.Spec.HTTPSNMATLS.Secret = secretToRotateTo
	default:
		r.Log.Info("Unknown TLS config name", "tlsConfig", tlsConfig)
		return ctrl.Result{}, nil
	}

	patch := r.Vdb.DeepCopy()
	// Update status
	if patch.GetTLSConfigByName(tlsConfig) == nil {
		patch.Status.TLSConfigs = append(patch.Status.TLSConfigs,
			vapi.TLSConfigStatus{
				Name: tlsConfig,
			})
	}
	status := patch.GetTLSConfigByName(tlsConfig)
	status.AutoRotateSecrets = secrets
	status.LastUpdate = v1.NewTime(now)

	if err := r.VRec.Client.Update(ctx, patch); err != nil {
		r.Log.Error(err, "Failed to patch VerticaDB spec during rotate", "tlsConfig", tlsConfig)
		return ctrl.Result{}, err
	}

	if err := vdbstatus.UpdateTLSConfigs(ctx, r.VRec.Client, patch, []*vapi.TLSConfigStatus{status}); err != nil {
		r.Log.Error(err, "Failed to patch TLSConfigStatus during rotate", "tlsConfig", tlsConfig)
		return ctrl.Result{}, err
	}

	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSAutoRotateSucceeded,
		"Auto-rotate triggered for TLS config %s with secret %s", tlsConfig, secretToRotateTo)

	return ctrl.Result{}, nil
}

// mergeResults will merge two results:
//  1. if both are requeueAfter, pick the soonest one
//  2. if one is requeueAfter and the other is no-requeue, pick requeueAfter one
//  3. if either is requeue, pick that
//  4. otherwise, ctrl.Result{}
func (r *AutoCertRotateReconciler) mergeResults(res1, res2 ctrl.Result) ctrl.Result {
	switch {
	case res1.RequeueAfter > 0 && res2.RequeueAfter > 0:
		// Both want requeue, pick the sooner one
		if res1.RequeueAfter < res2.RequeueAfter {
			return res1
		}
		return res2

	case res1.RequeueAfter > 0 && !res2.Requeue:
		return res1

	case res2.RequeueAfter > 0 && !res1.Requeue:
		return res2

	case res1.Requeue:
		return res1

	case res2.Requeue:
		return res2

	default:
		// Neither has requeue nor RequeueAfter
		return ctrl.Result{}
	}
}

// findSecretInList searches for current secret in status secret list; returns -1 if not found
func (r *AutoCertRotateReconciler) findSecretInList(current string, secrets []string) int {
	idx := -1
	for i, s := range secrets {
		if s == current {
			idx = i
			break
		}
	}
	return idx
}

// updateAutoRotateSecrets will update just the autoRotateSecrets in the status
func (r *AutoCertRotateReconciler) patchAutoRotateSecretsInStatus(ctx context.Context, tlsConfig string) (ctrl.Result, error) {
	// Prepare patch
	patch := r.Vdb.DeepCopy()
	patchStatus := patch.GetTLSConfigByName(tlsConfig)
	patchStatus.AutoRotateSecrets = r.Vdb.GetTLSConfigAutoRotate(tlsConfig).Secrets

	// Patch status explicitly
	if err := vdbstatus.UpdateTLSConfigs(ctx, r.VRec.Client, patch, []*vapi.TLSConfigStatus{patchStatus}); err != nil {
		r.Log.Error(err, "Failed to patch TLSConfigStatus with updated secrets", "tlsConfig", tlsConfig)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
