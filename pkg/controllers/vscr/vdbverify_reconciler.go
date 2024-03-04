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

package vscr

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	"github.com/vertica/vertica-kubernetes/pkg/vscrstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const verticaDBSetForVclusterOpsScrutinize = "VerticaDBSetForVclusterOpsScrutinize"

// VDBVerifyReconciler will verify the VerticaDB in the Vscr CR exists
type VDBVerifyReconciler struct {
	VRec *VerticaScrutinizeReconciler
	Vscr *v1beta1.VerticaScrutinize
	Vdb  *v1.VerticaDB
	Log  logr.Logger
}

func MakeVDBVerifyReconciler(r *VerticaScrutinizeReconciler, vscr *v1beta1.VerticaScrutinize,
	log logr.Logger) controllers.ReconcileActor {
	return &VDBVerifyReconciler{
		VRec: r,
		Vscr: vscr,
		Log:  log.WithName("VDBVerifyReconciler"),
		Vdb:  &v1.VerticaDB{},
	}
}

// Reconcile will verify the VerticaDB in the Vscr CR exists
func (s *VDBVerifyReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if the check has already been done once
	isSet := s.Vscr.IsStatusConditionTrue(v1beta1.ScrutinizeReady)
	if isSet {
		return ctrl.Result{}, nil
	}
	// This reconciler is intended to be the first thing we run.  We want early
	// feedback if the VerticaDB that is referenced in the vscr doesn't exist.
	// This will print out an event if the VerticaDB cannot be found.
	nm := names.GenNamespacedName(s.Vscr, s.Vscr.Spec.VerticaDBName)
	if res, err := vk8s.FetchVDB(ctx, s.VRec, s.Vscr, nm, s.Vdb); verrors.IsReconcileAborted(res, err) {
		return ctrl.Result{}, s.updateScrutinizeReadyCondition(ctx, metav1.ConditionFalse, events.VerticaDBNotFound,
			fmt.Sprintf("NotReady:%s", events.VerticaDBNotFound))
	}
	return ctrl.Result{}, s.checkVersionAndDeploymentType(ctx)
}

// checkVersion verifies that vclusterops is enabled and the server version supports
// vclusterops deployment
func (s *VDBVerifyReconciler) checkVersionAndDeploymentType(ctx context.Context) error {
	if !vmeta.UseVClusterOps(s.Vdb.Annotations) {
		s.VRec.Eventf(s.Vscr, corev1.EventTypeWarning, events.VclusterOpsDisabled,
			"The VerticaDB named '%s' has vclusterops disabled", s.Vdb.Name)
		return s.updateScrutinizeReadyCondition(ctx, metav1.ConditionFalse, events.VclusterOpsDisabled,
			"NotReady:AdmintoolsNotSupported")
	}
	vinf, ok := s.Vdb.MakeVersionInfo()
	if !ok {
		// if we don't find the server version in the vdb, we assume the vdb
		// is not ready for scrutinize and exit
		s.VRec.Eventf(s.Vscr, corev1.EventTypeWarning, events.VerticaVersionNotFound,
			"The VerticaDB named '%s' does not have the version annotation set", s.Vdb.Name)
		return s.updateScrutinizeReadyCondition(ctx, metav1.ConditionFalse, events.VerticaVersionNotFound,
			fmt.Sprintf("NotReady:%s", events.VerticaVersionNotFound))
	}

	if vinf.IsOlder(v1.VcluseropsAsDefaultDeploymentMethodMinVersion) {
		ver, _ := s.Vdb.GetVerticaVersionStr()
		s.VRec.Eventf(s.Vscr, corev1.EventTypeWarning, events.VclusterOpsScrutinizeNotSupported,
			"The server version %s does not have scrutinize support through vclusterOps", ver)
		return s.updateScrutinizeReadyCondition(ctx, metav1.ConditionFalse, events.VclusterOpsScrutinizeNotSupported,
			"NotReady:IncompatibleDB")
	}

	if vinf.IsOlder(v1.ScrutinizeDBPasswdInSecretMinVersion) {
		ver, _ := s.Vdb.GetVerticaVersionStr()
		s.VRec.Eventf(s.Vscr, corev1.EventTypeWarning, events.VclusterOpsScrutinizeNotSupported,
			"The server version %s is not supported with VerticaScrutinize. The minimum server version it supports is %s.",
			ver, v1.ScrutinizeDBPasswdInSecretMinVersion)
		return s.updateScrutinizeReadyCondition(ctx, metav1.ConditionFalse,
			events.VclusterOpsScrutinizeNotSupported, "NotReady:IncompatibleDB")
	}

	s.Log.Info(fmt.Sprintf("The VerticaDB named '%s' is configured for scrutinize through vclusterops", s.Vdb.Name))
	return s.updateScrutinizeReadyCondition(ctx, metav1.ConditionTrue, verticaDBSetForVclusterOpsScrutinize,
		"Ready")
}

// updateScrutinizeReadyCondition updates ScrutinizeReady status condition
func (s *VDBVerifyReconciler) updateScrutinizeReadyCondition(ctx context.Context,
	status metav1.ConditionStatus, reason, state string) error {
	cond := v1.MakeCondition(v1beta1.ScrutinizeReady, status, reason)
	stat := &v1beta1.VerticaScrutinizeStatus{}
	stat.State = state
	stat.Conditions = []metav1.Condition{*cond}
	return vscrstatus.UpdateStatus(ctx, s.VRec.Client, s.Vscr, stat)
}
