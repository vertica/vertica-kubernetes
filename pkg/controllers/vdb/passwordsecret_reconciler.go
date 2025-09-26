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
	"github.com/google/uuid"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cache"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PasswordSecretReconciler will update admin password in the database
type PasswordSecretReconciler struct {
	Rec          vdbconfig.ReconcilerInterface
	Log          logr.Logger
	Vdb          *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts       *podfacts.PodFacts
	PRunner      cmds.PodRunner
	Dispatcher   vadmin.Dispatcher
	CacheManager cache.CacheManager
	ConfigMap    *corev1.ConfigMap
}

// MakePasswordSecretReconciler will build an PasswordSecretReconciler object
func MakePasswordSecretReconciler(recon vdbconfig.ReconcilerInterface, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher, cacheManager cache.CacheManager,
	configMap *corev1.ConfigMap) controllers.ReconcileActor {
	return &PasswordSecretReconciler{
		Rec:          recon,
		Log:          log.WithName("PasswordSecretReconciler"),
		Vdb:          vdb,
		PFacts:       pfacts,
		PRunner:      prunner,
		Dispatcher:   dispatcher,
		CacheManager: cacheManager,
		ConfigMap:    configMap,
	}
}

func (a *PasswordSecretReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !a.Vdb.IsDBInitialized() {
		return ctrl.Result{}, nil
	}

	sbName := a.PFacts.GetSandboxName()

	// Ensure status initialized for main cluster or sandbox
	if err := a.ensureStatusInitialized(ctx, sbName); err != nil {
		return ctrl.Result{}, err
	}

	// If we are reconciling a sandbox and it matches spec → nothing to do
	if sbName != vapi.MainCluster && a.statusMatchesSpec(sbName) {
		return ctrl.Result{}, nil
	}

	// If everything is up-to-date, no-op
	outdatedSandboxes := a.getSandboxesNeedingUpdating()
	if a.statusMatchesSpec(vapi.MainCluster) && len(outdatedSandboxes) == 0 {
		return ctrl.Result{}, nil
	}

	// Perform password update (main cluster or sandbox)
	res, err := a.updatePasswordSecret(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// For main cluster, ensure status and trigger follow-ups
	if sbName == vapi.MainCluster {
		if err := a.updatePasswordSecretStatus(ctx); err != nil {
			return ctrl.Result{}, err
		}
		// Always trigger any out-of-date sandboxes
		return a.triggerOutOfDateSandboxes(ctx, outdatedSandboxes)
	}

	// Unset sandbox annotation
	if _, ok := a.ConfigMap.Annotations[vmeta.SandboxControllerPasswordChangeTriggerID]; ok {
		delete(a.ConfigMap.Annotations, vmeta.SandboxControllerPasswordChangeTriggerID)
		if err := a.Rec.GetClient().Update(ctx, a.ConfigMap); err != nil {
			a.Log.Error(err, "failed to remove password change trigger annotation")
			return ctrl.Result{}, err
		}
		a.Log.Info("Removed password change trigger annotation from sandbox configmap")
	}

	return ctrl.Result{}, a.updatePasswordSecretStatusForSandbox(ctx, sbName)
}

// ensureStatusInitialized sets up initial status if not yet populated for main cluster or sandbox.
func (a *PasswordSecretReconciler) ensureStatusInitialized(ctx context.Context, sbName string) error {
	if sbName == vapi.MainCluster && a.Vdb.Status.PasswordSecret == nil {
		return a.updatePasswordSecretStatus(ctx)
	}
	if sbName != vapi.MainCluster {
		if ok, _ := a.Vdb.GetPasswordSecretForSandbox(sbName); !ok {
			return a.updatePasswordSecretStatusForSandbox(ctx, sbName)
		}
	}
	return nil
}

// statusMatchesSpec checks if the password secret status is the same as spec for either main cluster
// or a specific sandbox.
func (a *PasswordSecretReconciler) statusMatchesSpec(sbName string) bool {
	if sbName == vapi.MainCluster {
		if a.Vdb.Status.PasswordSecret == nil {
			return false
		}
		return a.Vdb.Spec.PasswordSecret == *a.Vdb.Status.PasswordSecret
	}

	if ok, secret := a.Vdb.GetPasswordSecretForSandbox(sbName); ok {
		return a.Vdb.Spec.PasswordSecret == secret
	}
	return false
}

// getSandboxesNeedingUpdating returns sandboxes where secret in status does not match secret in spec
func (a *PasswordSecretReconciler) getSandboxesNeedingUpdating() []vapi.Sandbox {
	sandboxes := []vapi.Sandbox{}
	for _, sb := range a.Vdb.Spec.Sandboxes {
		ok, secret := a.Vdb.GetPasswordSecretForSandbox(sb.Name)
		if !ok || secret != a.Vdb.Spec.PasswordSecret {
			sandboxes = append(sandboxes, sb)
		}
	}

	return sandboxes
}

// updatePasswordSecret will update the password secret in the database.
func (a *PasswordSecretReconciler) updatePasswordSecret(ctx context.Context) (ctrl.Result, error) {
	// Get new password; don't look in cache because it has old secret
	newPasswd, err := vk8s.GetCustomSuperuserPassword(ctx, a.Rec.GetClient(), a.Log, a.Rec, a.Vdb,
		a.Vdb.Spec.PasswordSecret, names.SuperuserPasswordKey, a.CacheManager, true)
	if err != nil {
		return ctrl.Result{}, err
	}

	sbName := a.PFacts.GetSandboxName()
	if a.statusMatchesSpec(sbName) {
		return ctrl.Result{}, nil
	}

	res, err := a.updateOnePasswordSecret(ctx, a.PFacts, newPasswd, sbName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// reset password used in vdb and CacheManager
	return a.resetVDBPassword(*newPasswd, sbName)
}

// triggerOutOfDateSandboxes will trigger password change for any sandboxes that don't match spec.
func (a *PasswordSecretReconciler) triggerOutOfDateSandboxes(ctx context.Context, sandboxes []vapi.Sandbox) (ctrl.Result, error) {
	for _, sb := range sandboxes {
		triggerUUID := uuid.NewString()
		sbMan := MakeSandboxConfigMapManager(a.Rec, a.Vdb, sb.Name, triggerUUID)
		triggered, err := sbMan.triggerSandboxController(ctx, PasswordChange)
		if triggered {
			a.Log.Info("Sandbox ConfigMap updated. The sandbox controller will drive the password change",
				"trigger-uuid", triggerUUID, "Sandbox", sb.Name)
		}
		if err != nil {
			a.Log.Error(err, "Failed to trigger sandbox password change", "sandbox", sb.Name)
		}
	}
	return ctrl.Result{}, nil
}

// updateOnePasswordSecret will update the password for the main cluster or for one sandbox.
func (a *PasswordSecretReconciler) updateOnePasswordSecret(ctx context.Context,
	pfacts *podfacts.PodFacts, newPasswd *string, sandbox string) (ctrl.Result, error) {
	// No-op if password is the same
	found, passSecret := a.Vdb.GetPasswordSecretForSandbox(sandbox)
	if !found {
		return ctrl.Result{}, fmt.Errorf("could not find password secret for sandbox %s", sandbox)
	}
	if pass, ok := a.CacheManager.GetPassword(a.Vdb.Namespace, a.Vdb.Name, passSecret); ok && pass == *newPasswd {
		a.Log.Info("WARNING: password in secret is the same as current password", "current password secret",
			a.Vdb.Status.PasswordSecret, "new password secret", a.Vdb.Spec.PasswordSecret, "sandbox", sandbox)
		return ctrl.Result{}, nil
	}

	if pErr := pfacts.Collect(ctx, a.Vdb); pErr != nil {
		return ctrl.Result{}, pErr
	}
	pf, found := pfacts.FindInitiatorInSB(sandbox, "")
	if !found {
		a.Log.Info("No Up nodes found. Requeue dbadmin password secret reconciliation.", "sandbox", sandbox)
		return ctrl.Result{Requeue: true}, nil
	}

	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf(`ALTER USER %s IDENTIFIED BY '%s';`, a.Vdb.GetVerticaUser(), *newPasswd))
	cmd := []string{"-tAc", sb.String()}
	stdout, stderr, err := pfacts.PRunner.ExecVSQL(ctx, pf.GetName(), names.ServerContainer, cmd...)
	if err != nil {
		if sandbox == vapi.MainCluster {
			a.Rec.Eventf(a.Vdb, corev1.EventTypeNormal, events.SuperuserPasswordSecretUpdateFailed,
				"Superuser password update failed")
		} else {
			a.Rec.Eventf(a.Vdb, corev1.EventTypeNormal, events.SuperuserPasswordSecretUpdateFailed,
				"Superuser password update failed in sandbox %q", sandbox)
		}
		a.Log.Error(err, "failed to update superuser password secret", "stderr", stderr, "sandbox", sandbox)
		return ctrl.Result{}, err
	}

	if sandbox == vapi.MainCluster {
		a.Rec.Eventf(a.Vdb, corev1.EventTypeNormal, events.SuperuserPasswordSecretUpdateSucceeded,
			"Superuser password update succeeded")
	} else {
		a.Rec.Eventf(a.Vdb, corev1.EventTypeNormal, events.SuperuserPasswordSecretUpdateSucceeded,
			"Superuser password update succeeded in sandbox %q", sandbox)
	}
	a.Log.Info("Successfully updated superuser password secret",
		"stdout", stdout, "new secret", a.Vdb.Spec.PasswordSecret, "sandbox", sandbox)
	return ctrl.Result{}, nil
}

// updatePasswordSecretStatus will update password secret status in vdb for the main cluster.
func (a *PasswordSecretReconciler) updatePasswordSecretStatus(ctx context.Context) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// make a copy of the password secret in the status
		statusSecret := a.Vdb.Spec.PasswordSecret
		vdbChg.Status.PasswordSecret = &statusSecret
		return nil
	}
	return vdbstatus.Update(ctx, a.Rec.GetClient(), a.Vdb, updateStatus)
}

// updatePasswordSecretStatusForSandbox updates the password secret in the status for a given sandbox.
func (a *PasswordSecretReconciler) updatePasswordSecretStatusForSandbox(ctx context.Context, sbName string) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		for i := range vdbChg.Status.Sandboxes {
			if vdbChg.Status.Sandboxes[i].Name == sbName {
				vdbChg.Status.Sandboxes[i].PasswordSecret = a.Vdb.Spec.PasswordSecret
				return nil
			}
		}
		vdbChg.Status.Sandboxes = append(vdbChg.Status.Sandboxes, vapi.SandboxStatus{
			Name:           sbName,
			PasswordSecret: a.Vdb.Spec.PasswordSecret,
		})
		return nil
	}
	return vdbstatus.Update(ctx, a.Rec.GetClient(), a.Vdb, updateStatus)
}

// resetVDBPassword will reset the password used in prunner, pfacts, dispatcher, and CacheManager.
func (a *PasswordSecretReconciler) resetVDBPassword(newPasswd, sandbox string) (ctrl.Result, error) {
	// prunner, podfacts and dispatcher share the pointer
	// reset one of them will also reset the password in the others
	a.PFacts.SetSUPassword(newPasswd)
	found, passSecret := a.Vdb.GetPasswordSecretForSandbox(sandbox)
	if !found {
		return ctrl.Result{}, fmt.Errorf("could not find password secret for sandbox %s", sandbox)
	}
	a.CacheManager.SetPassword(a.Vdb.Namespace, a.Vdb.Name, passSecret, newPasswd)
	return ctrl.Result{}, nil
}
