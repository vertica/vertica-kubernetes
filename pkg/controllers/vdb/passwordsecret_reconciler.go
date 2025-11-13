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
	sbName := a.PFacts.GetSandboxName()

	if !a.Vdb.IsDBInitialized() || a.Vdb.IsMainClusterStopped() {
		return ctrl.Result{}, nil
	}

	// If everything is up-to-date, no-op
	if !a.Vdb.IsPasswordChangeInProgress() {
		return ctrl.Result{}, a.unsetSandboxAnnotion(ctx, sbName)
	}

	// For main cluster, always trigger any out-of-date sandboxes
	if sbName == vapi.MainCluster {
		if err := a.triggerOutOfDateSandboxes(ctx, a.Vdb.GetSandboxesWithPasswordChange()); err != nil {
			return ctrl.Result{}, err
		}
	}

	// passwordSecret in status will only be nil during DB/sandbox init.
	// In this case, password is already set in DB so we just need to update the status.
	err, initRan := a.ensureStatusInitialized(ctx, sbName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if initRan {
		a.Log.Info("Initialized password secret in status", "sandbox", sbName)
		return ctrl.Result{}, a.unsetSandboxAnnotion(ctx, sbName)
	}

	// no-op if this password is not changed
	if !a.Vdb.IsPasswordSecretChanged(sbName) {
		return ctrl.Result{}, a.unsetSandboxAnnotion(ctx, sbName)
	}

	// Perform password update (main cluster or sandbox)
	if res, err := a.updatePasswordSecret(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Update main cluster secret in status
	if sbName == vapi.MainCluster {
		return ctrl.Result{}, a.updatePasswordSecretStatus(ctx)
	}

	// Update sandbox secret in status
	if err := a.updatePasswordSecretStatusForSandbox(ctx, sbName); err != nil {
		return ctrl.Result{}, err
	}

	// Unset sandbox annotation
	return ctrl.Result{}, a.unsetSandboxAnnotion(ctx, sbName)
}

// ensureStatusInitialized sets up initial status if not yet populated for main cluster or sandbox.
func (a *PasswordSecretReconciler) ensureStatusInitialized(ctx context.Context, sbName string) (err error, initRan bool) {
	if sbName == vapi.MainCluster && a.Vdb.Status.PasswordSecret == nil {
		return a.updatePasswordSecretStatus(ctx), true
	}
	if sbName != vapi.MainCluster {
		sandboxStatus := a.Vdb.GetSandboxStatus(sbName)
		if sandboxStatus == nil {
			return fmt.Errorf("could not find sandbox %q in status", sbName), false
		}
		if sandboxStatus.PasswordSecret == nil {
			return a.updatePasswordSecretStatusForSandbox(ctx, sbName), true
		}
	}
	return nil, false
}

// updatePasswordSecret will update the password secret in the database.
func (a *PasswordSecretReconciler) updatePasswordSecret(ctx context.Context) (ctrl.Result, error) {
	sbName := a.PFacts.GetSandboxName()
	if a.Vdb.IsPasswordSecretChanged(sbName) {
		if res, err := a.updateOnePasswordSecret(ctx, a.PFacts, sbName); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	// reset password used in vdb and CacheManager
	return a.resetVDBPassword(ctx)
}

// triggerOutOfDateSandboxes will trigger password change for any sandboxes that don't match spec.
func (a *PasswordSecretReconciler) triggerOutOfDateSandboxes(ctx context.Context, sandboxes []vapi.SandboxStatus) error {
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
			return err
		}
	}
	return nil
}

// updateOnePasswordSecret will update the password for the main cluster or for one sandbox.
func (a *PasswordSecretReconciler) updateOnePasswordSecret(ctx context.Context,
	pfacts *podfacts.PodFacts, sandbox string) (ctrl.Result, error) {
	newPasswd, err := vk8s.GetCustomSuperuserPassword(ctx, a.Rec.GetClient(), a.Log, a.Rec, a.Vdb,
		a.Vdb.Spec.PasswordSecret, names.SuperuserPasswordKey, a.CacheManager)
	if err != nil {
		return ctrl.Result{}, err
	}

	a.Log.Info("Updating password secret", "sandbox", sandbox, "newSecret", a.Vdb.Spec.PasswordSecret,
		"oldSecret", a.Vdb.GetPasswordSecretForSandbox(sandbox))

	// No-op if password is the same
	pass, ok := a.CacheManager.GetPassword(a.Vdb.Namespace, a.Vdb.Name, a.Vdb.GetPasswordSecretForSandbox(sandbox))
	if ok && pass == *newPasswd {
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
				vdbChg.Status.Sandboxes[i].PasswordSecret = &a.Vdb.Spec.PasswordSecret
				return nil
			}
		}
		vdbChg.Status.Sandboxes = append(vdbChg.Status.Sandboxes, vapi.SandboxStatus{
			Name:           sbName,
			PasswordSecret: &a.Vdb.Spec.PasswordSecret,
		})
		return nil
	}
	return vdbstatus.Update(ctx, a.Rec.GetClient(), a.Vdb, updateStatus)
}

// resetVDBPassword will reset the password used in prunner, pfacts, dispatcher, and CacheManager.
func (a *PasswordSecretReconciler) resetVDBPassword(ctx context.Context) (ctrl.Result, error) {
	// Getting the password will automatically update the cache
	newPasswd, err := vk8s.GetCustomSuperuserPassword(ctx, a.Rec.GetClient(), a.Log, a.Rec, a.Vdb,
		a.Vdb.Spec.PasswordSecret, names.SuperuserPasswordKey, a.CacheManager)
	if err != nil {
		return ctrl.Result{}, err
	}

	// prunner, podfacts and dispatcher share the pointer
	// reset one of them will also reset the password in the others
	a.PFacts.SetSUPassword(*newPasswd)
	return ctrl.Result{}, nil
}

// unsetSandboxAnnotion will unset the annotation to trigger sandbox
func (a *PasswordSecretReconciler) unsetSandboxAnnotion(ctx context.Context, sbName string) error {
	if sbName == vapi.MainCluster {
		return nil
	}
	if _, ok := a.ConfigMap.Annotations[vmeta.SandboxControllerPasswordChangeTriggerID]; ok {
		delete(a.ConfigMap.Annotations, vmeta.SandboxControllerPasswordChangeTriggerID)
		if err := a.Rec.GetClient().Update(ctx, a.ConfigMap); err != nil {
			a.Log.Error(err, "failed to remove password change trigger annotation")
			return err
		}
		a.Log.Info("Removed password change trigger annotation from sandbox configmap")
	}
	return nil
}
