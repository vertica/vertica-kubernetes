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
}

// MakePasswordSecretReconciler will build an PasswordSecretReconciler object
func MakePasswordSecretReconciler(recon vdbconfig.ReconcilerInterface, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher, cacheManager cache.CacheManager) controllers.ReconcileActor {
	return &PasswordSecretReconciler{
		Rec:          recon,
		Log:          log.WithName("PasswordSecretReconciler"),
		Vdb:          vdb,
		PFacts:       pfacts,
		PRunner:      prunner,
		Dispatcher:   dispatcher,
		CacheManager: cacheManager,
	}
}

func (a *PasswordSecretReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !a.Vdb.IsDBInitialized() {
		return ctrl.Result{}, nil
	}

	// set status to the current using secret when db is initialized
	if a.Vdb.Status.PasswordSecret == nil {
		return ctrl.Result{}, a.updatePasswordSecretStatus(ctx)
	}

	// No actions needed if status content is the same to spec
	if a.statusMatchesSpec() {
		return ctrl.Result{}, nil
	}

	res, err := a.updatePasswordSecret(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{}, a.updatePasswordSecretStatus(ctx)
}

// statusMatchesSpec checks if the password secret status is the same to spec or not
func (a *PasswordSecretReconciler) statusMatchesSpec() bool {
	return a.Vdb.Spec.PasswordSecret == *a.Vdb.Status.PasswordSecret
}

// updatePasswordSecret will update the password secret in the database. It starts with the main cluster,
// then cycles through the sandboxes.
func (a *PasswordSecretReconciler) updatePasswordSecret(ctx context.Context) (ctrl.Result, error) {
	// Get new password; don't look in cache because it has old secret
	newPasswd, err := vk8s.GetCustomSuperuserPassword(ctx, a.Rec.GetClient(), a.Log, a.Rec, a.Vdb,
		a.Vdb.Spec.PasswordSecret, names.SuperuserPasswordKey, a.CacheManager, true)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update password of current cluster
	sbName := a.PFacts.GetSandboxName()
	res, err := a.updatePasswordSecretInSandbox(ctx, a.PFacts, newPasswd, sbName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// If updating sandbox, we are done
	if sbName != "" {
		return ctrl.Result{}, err
	}

	// When updating main cluster, trigger sandbox controller for password change in each sandbox
	for _, sb := range a.Vdb.Spec.Sandboxes {
		triggerUUID := uuid.NewString()
		sbMan := MakeSandboxConfigMapManager(a.Rec, a.Vdb, sb.Name, triggerUUID)
		triggered, err := sbMan.triggerSandboxController(ctx, PasswordChange)
		if triggered {
			a.Log.Info("Sandbox ConfigMap updated. The sandbox controller will drive the password change",
				"trigger-uuid", triggerUUID, "Sandbox", sb.Name)
		}
		if err != nil {
			a.Log.Error(err, "Failed to trigger sandbox password change", "sandbox", sb.Name)
			// Continue to next sandbox, don't abort all
		}
	}

	// reset password used in vdb and CacheManager
	a.resetVDBPassword(*newPasswd)
	return ctrl.Result{}, nil
}

// updateOnePasswordSecret will update the password for the main cluster or for one sandbox.
func (a *PasswordSecretReconciler) updatePasswordSecretInSandbox(ctx context.Context,
	pfacts *podfacts.PodFacts, newPasswd *string, sandbox string) (ctrl.Result, error) {
	// No-op if password is the same
	if pass, ok := a.CacheManager.GetPassword(a.Vdb.Namespace, a.Vdb.Name); ok && pass == *newPasswd {
		a.Log.Info("WARNING: password in secret is the same as current password", "current password secret",
			a.Vdb.Status.PasswordSecret, "new password secret", a.Vdb.Spec.PasswordSecret)
		return ctrl.Result{}, nil
	}

	if pErr := pfacts.Collect(ctx, a.Vdb); pErr != nil {
		return ctrl.Result{}, pErr
	}
	pf, found := pfacts.FindInitiatorInSB(sandbox, "")
	if !found {
		a.Log.Info("No Up nodes found. Requeue dbadmin password secret reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf(`ALTER USER %s IDENTIFIED BY '%s';`, a.Vdb.GetVerticaUser(), *newPasswd))
	cmd := []string{"-tAc", sb.String()}
	stdout, stderr, err := pfacts.PRunner.ExecVSQL(ctx, pf.GetName(), names.ServerContainer, cmd...)
	if err != nil {
		a.Rec.Eventf(a.Vdb, corev1.EventTypeNormal, events.SuperuserPasswordSecretUpdateFailed,
			"Superuser password update failed")
		a.Log.Error(err, "failed to update superuser password secret", "stderr", stderr)
		return ctrl.Result{}, err
	}

	if sandbox == "" {
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

// updatePasswordSecretStatus will update password secret status in vdb
func (a *PasswordSecretReconciler) updatePasswordSecretStatus(ctx context.Context) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// make a copy of the password secret in the status
		statusSecret := a.Vdb.Spec.PasswordSecret
		vdbChg.Status.PasswordSecret = &statusSecret
		return nil
	}

	return vdbstatus.Update(ctx, a.Rec.GetClient(), a.Vdb, updateStatus)
}

// resetVDBPassword will reset the password used in prunner, pfacts, dispatcher, and CacheManager.
func (a *PasswordSecretReconciler) resetVDBPassword(newPasswd string) {
	// prunner, podfacts and dispatcher share the pointer
	// reset one of them will also reset the password in the others
	a.PFacts.SetSUPassword(newPasswd)
	a.CacheManager.SetPassword(a.Vdb.Namespace, a.Vdb.Name, newPasswd)
}
