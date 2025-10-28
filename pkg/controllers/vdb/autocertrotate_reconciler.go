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
	"k8s.io/client-go/util/retry"

	"github.com/vertica/vertica-kubernetes/pkg/controllers"
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
	if !r.Vdb.IsDBInitialized() || !r.Vdb.IsAutoCertRotationEnabled(vapi.ClientServerTLSConfigName) &&
		!r.Vdb.IsAutoCertRotationEnabled(vapi.HTTPSNMATLSConfigName) &&
		!r.Vdb.IsAutoCertRotationEnabled(vapi.InterNodeTLSConfigName) {
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
	// Check Inter Node auto-rotate
	interNodeRes, err := r.autoRotateByTLSConfig(ctx, vapi.InterNodeTLSConfigName)
	if err != nil {
		return interNodeRes, err
	}
	// Compare results; requeue for shortest time
	mergedRes := r.mergeResults(httpsRes, clientServerRes)

	return r.mergeResults(mergedRes, interNodeRes), nil
}

// autoRotateByTLSConfig will check if a certain TLS config (httpsNMA or clientServer) requires auto-rotation.
// If it is set for auto-rotate and the time to rotate has not passed, it will requeue for that time. If it
// is set for auto-rotate and the time to rotate has passed, it will trigger rotate and (if relevant) requeue
// for next rotation.
func (r *AutoCertRotateReconciler) autoRotateByTLSConfig(ctx context.Context, tlsConfig string) (ctrl.Result, error) {
	// If user has cleared autoRotate from spec, remove status fields
	if r.Vdb.IsTLSAuthEnabledForConfig(tlsConfig) && r.Vdb.GetTLSConfigSpecByName(tlsConfig) != nil &&
		r.Vdb.GetTLSConfigSpecByName(tlsConfig).AutoRotate == nil && len(r.Vdb.GetAutoRotateSecrets(tlsConfig)) > 0 {
		r.Log.Info("autoRotate has been removed from spec; clearing status fields", "tlsConfig", tlsConfig)
		return ctrl.Result{}, r.updateTLSStatus(ctx, tlsConfig, func(status *vapi.TLSConfigStatus) {
			status.AutoRotateSecrets = nil
			status.LastUpdate = v1.NewTime(time.Time{})
		})
	}
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
	// Get current secret in use and the failed secret (if any)
	current := r.Vdb.GetSecretInUse(tlsConfig)
	failedSecret := r.Vdb.GetTLSConfigByName(tlsConfig).AutoRotateFailedSecret

	// If spec secret list does not match status secret list, user must have updated the spec.
	// There are two scenarios here:
	//   1) If current secret is still in list, just continue rotating from its new position
	//   2) If current secret is not in list, restart auto-rotate from scratch
	specSecrets := r.Vdb.GetTLSConfigAutoRotate(tlsConfig).Secrets
	if !r.Vdb.EqualStringSlices(specSecrets, r.Vdb.GetTLSConfigByName(tlsConfig).AutoRotateSecrets) {
		if r.findSecretInList(current, specSecrets) == -1 {
			r.Log.Info("Spec secret list changed and current secret is missing. Restarting auto-rotate.")
			return r.initializeAutoRotate(ctx, tlsConfig)
		} else {
			r.Log.Info("Spec secret list changed, preserving current rotation position", "currentSecret", current)
			return ctrl.Result{}, r.updateTLSStatus(ctx, tlsConfig, func(status *vapi.TLSConfigStatus) {
				status.AutoRotateSecrets = r.Vdb.GetTLSConfigAutoRotate(tlsConfig).Secrets
			})
		}
	}
	// If last auto-rotate failed, we will immediately rotate to the next secret in the list.
	if failedSecret != "" {
		r.Log.Info("Previous TLS rotation with secret failed; triggering retry with next secret",
			"failedSecret", failedSecret, "tlsConfig", tlsConfig)
		return r.rotateToNextTLSSecret(ctx, tlsConfig, failedSecret)
	}
	// Check if we are after nextUpdate.
	// Since this can take a long time, for testing purposes, we have added an annotation to
	// automatically trigger the auto-rotation now.
	if r.Vdb.Annotations[vmeta.TriggerAutoTLSRotateAnnotation] != "" || time.Until(nextUpdate.Time) <= 0 {
		r.Log.Info("Next update time for auto cert rotation has passed; triggering auto-rotation", "tlsConfig", tlsConfig)
		return r.rotateToNextTLSSecret(ctx, tlsConfig, current)
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
// If the previous auto-rotate failed, we will rotate to the next secret in the list.
func (r *AutoCertRotateReconciler) rotateToNextTLSSecret(ctx context.Context, tlsConfig, current string) (ctrl.Result, error) {
	secrets := r.Vdb.GetTLSConfigByName(tlsConfig).AutoRotateSecrets

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

	targetIndex := secretIndex + 1

	// If current secret is last and restartAtEnd is true, begin at the start again;
	// if it is false, error.
	if targetIndex >= len(secrets) {
		if r.Vdb.GetTLSConfigAutoRotate(tlsConfig).RestartAtEnd {
			r.Log.Info("Restarting TLS auto-rotation at first secret", "tlsConfig", tlsConfig)
			return r.rotateToSecret(ctx, tlsConfig, secrets, secrets[0])
		}
		r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSAutoRotateFailed,
			"Completed all secrets for TLS Config %s. Auto-rotation is stopped; update the secrets list to resume.", tlsConfig)
		return ctrl.Result{}, nil
	}

	// Rotate to next secret
	r.Log.Info("Auto-rotating to next TLS secret",
		"tlsConfig", tlsConfig, "nextSecret", secrets[targetIndex])
	return r.rotateToSecret(ctx, tlsConfig, secrets, secrets[targetIndex])
}

// rotateToSecret will update the secret in the VDB spec, to trigger a rotation on the next iteration.
// It will also update the auto-rotate fields in the status, such as nextRotate and autoRotateSecrets.
func (r *AutoCertRotateReconciler) rotateToSecret(
	ctx context.Context, tlsConfig string, secrets []string, secretToRotateTo string,
) (ctrl.Result, error) {
	now := time.Now()

	// Update spec to trigger cert rotation
	if err := r.updateTLSSecretSpec(ctx, tlsConfig, secretToRotateTo); err != nil {
		r.Log.Error(err, "Failed to update VerticaDB spec during rotate", "tlsConfig", tlsConfig)
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateTLSStatus(ctx, tlsConfig, func(status *vapi.TLSConfigStatus) {
		status.AutoRotateSecrets = secrets
		status.LastUpdate = v1.NewTime(now)
	}); err != nil {
		r.Log.Error(err, "Failed to update VerticaDB status during rotate", "tlsConfig", tlsConfig)
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

func (r *AutoCertRotateReconciler) updateTLSSecretSpec(
	ctx context.Context, tlsConfig, secretToRotateTo string,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		switch tlsConfig {
		case vapi.ClientServerTLSConfigName:
			r.Vdb.Spec.ClientServerTLS.Secret = secretToRotateTo
		case vapi.HTTPSNMATLSConfigName:
			r.Vdb.Spec.HTTPSNMATLS.Secret = secretToRotateTo
		case vapi.InterNodeTLSConfigName:
			r.Vdb.Spec.InterNodeTLS.Secret = secretToRotateTo
		default:
			r.Log.Info("Unknown TLS config name", "tlsConfig", tlsConfig)
			return nil
		}

		return r.VRec.Client.Update(ctx, r.Vdb)
	})
}

// updateTLSStatus updates the TLSConfigStatus for the given tlsConfig.
// The updateFn callback is applied to the status object before persisting.
func (r *AutoCertRotateReconciler) updateTLSStatus(
	ctx context.Context, tlsConfig string, updateFn func(status *vapi.TLSConfigStatus),
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if r.Vdb.GetTLSConfigByName(tlsConfig) == nil {
			r.Vdb.Status.TLSConfigs = append(r.Vdb.Status.TLSConfigs,
				vapi.TLSConfigStatus{Name: tlsConfig})
		}

		status := r.Vdb.GetTLSConfigByName(tlsConfig)
		updateFn(status)

		// Always clear failures if status is updated
		status.AutoRotateFailedSecret = ""

		return r.VRec.Client.Status().Update(ctx, r.Vdb)
	})
}
