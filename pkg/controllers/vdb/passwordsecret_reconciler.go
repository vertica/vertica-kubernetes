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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PasswordSecretReconciler will update admin password in the database
type PasswordSecretReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts  *podfacts.PodFacts
	PRunner cmds.PodRunner
}

// MakePasswordSecretReconciler will build an PasswordSecretReconciler object
func MakePasswordSecretReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &PasswordSecretReconciler{
		VRec:    vdbrecon,
		Log:     log.WithName("PasswordSecretReconciler"),
		Vdb:     vdb,
		PFacts:  pfacts,
		PRunner: prunner,
	}
}

func (a *PasswordSecretReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
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

// updatePasswordSecretStatus will update password secret status in vdb
func (a *PasswordSecretReconciler) updatePasswordSecretStatus(ctx context.Context) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// simply make a copy of the password secret in the status
		vdbChg.Status.PasswordSecret = a.Vdb.Spec.PasswordSecret
		return nil
	}

	return vdbstatus.Update(ctx, a.VRec.GetClient(), a.Vdb, updateStatus)
}

// updatePasswordSecret will update the password secret in the database
func (a *PasswordSecretReconciler) updatePasswordSecret(ctx context.Context) (ctrl.Result, error) {
	pf, found := a.PFacts.FindFirstUpPod(true, a.Vdb.GetFirstPrimarySubcluster().Name)
	if !found {
		return ctrl.Result{Requeue: true}, nil
	}

	sb := strings.Builder{}

	dbUser := "dbadmin"
	if a.Vdb.Annotations[vmeta.SuperuserNameAnnotation] != "" {
		dbUser = a.Vdb.Annotations[vmeta.SuperuserNameAnnotation]
	}
	sb.WriteString(fmt.Sprintf(
		`ALTER USER %s IDENTIFIED BY '%s';`, dbUser, a.Vdb.Spec.PasswordSecret))

	cmd := []string{"-tAc", sb.String()}
	stdout, stderr, err := a.PRunner.ExecVSQL(ctx, pf.GetName(), names.ServerContainer, cmd...)
	if err != nil {
		a.VRec.Log.Error(err, "failed to retrieve active sessions", "stderr", stderr)
		return ctrl.Result{}, err
	}

	a.VRec.Eventf(a.Vdb, corev1.EventTypeNormal, events.SuperuserPasswordSecretUpdated,
		"password secret updated")
	a.VRec.Log.Info("Updating password secret", "stdout", stdout)

	return ctrl.Result{}, nil
}
