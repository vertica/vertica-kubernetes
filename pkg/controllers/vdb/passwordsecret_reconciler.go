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
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PasswordSecretReconciler will update admin password in the database
type PasswordSecretReconciler struct {
	VRec       *VerticaDBReconciler
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts     *podfacts.PodFacts
	PRunner    cmds.PodRunner
	Dispatcher vadmin.Dispatcher
}

// MakePasswordSecretReconciler will build an PasswordSecretReconciler object
func MakePasswordSecretReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &PasswordSecretReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("PasswordSecretReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		PRunner:    prunner,
		Dispatcher: dispatcher,
	}
}

func (a *PasswordSecretReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// No-op if no up nodes found
	if a.Vdb.Status.UpNodeCount == 0 {
		return ctrl.Result{}, nil
	}

	// We put the current using password secret in the status
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
	return a.Vdb.Spec.PasswordSecret == a.Vdb.Status.PasswordSecret
}

// updatePasswordSecret will update the password secret in the database
func (a *PasswordSecretReconciler) updatePasswordSecret(ctx context.Context) (ctrl.Result, error) {
	if err := a.PFacts.Collect(ctx, a.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	pf, found := a.PFacts.FindFirstUpPod(false, a.Vdb.GetFirstPrimarySubcluster().Name)
	if !found {
		return ctrl.Result{Requeue: true}, nil
	}

	// The password to be updated to
	newPasswd, err := vk8s.GetCustomSuperuserPassword(ctx, a.VRec.Client, a.Log, a.VRec, a.Vdb,
		a.Vdb.Spec.PasswordSecret, names.SuperuserPasswordKey)
	if err != nil {
		return ctrl.Result{}, err
	}

	// No-op if password is the same
	if *a.PFacts.VerticaSUPassword == *newPasswd {
		a.Log.Info("WARNING: password in secret is the same as current password", "current password secret",
			a.Vdb.Status.PasswordSecret, "new password secret", a.Vdb.Spec.PasswordSecret)
		return ctrl.Result{}, nil
	}

	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf(`ALTER USER %s IDENTIFIED BY '%s';`, a.Vdb.GetVerticaUser(), *newPasswd))
	cmd := []string{"-tAc", sb.String()}
	stdout, stderr, err := a.PRunner.ExecVSQL(ctx, pf.GetName(), names.ServerContainer, cmd...)
	if err != nil {
		a.VRec.Log.Error(err, "failed to update the password", "stderr", stderr)
		return ctrl.Result{}, err
	}

	a.VRec.Eventf(a.Vdb, corev1.EventTypeNormal, events.SuperuserPasswordSecretUpdated,
		"Superuser password updated")
	a.VRec.Log.Info("Updating password secret", "stdout", stdout, "new secret", a.Vdb.Spec.PasswordSecret)

	// reset password used in vdb
	a.resetVDBPassword(*newPasswd)
	return ctrl.Result{}, nil
}

// updatePasswordSecretStatus will update password secret status in vdb
func (a *PasswordSecretReconciler) updatePasswordSecretStatus(ctx context.Context) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// simply make a copy of the password secret in the status
		vdbChg.Status.PasswordSecret = a.Vdb.Spec.PasswordSecret
		return nil
	}

	return vdbstatus.Update(ctx, a.VRec.GetClient(), a.Vdb, updateStatus)
}

// resetVDBPassword will reset the password used in prunner, pfacts, and dispatcher
func (a *PasswordSecretReconciler) resetVDBPassword(newPasswd string) {
	// prunner, podfacts and dispatcher share the pointer
	// reset one of them will also reset the password in the others
	a.PFacts.SetSUPassword(newPasswd)
}
