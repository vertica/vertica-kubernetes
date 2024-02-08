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

package vscr

import (
	"context"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VDBVerifyReconciler will verify the VerticaDB in the Vscr CR exists
type VDBVerifyReconciler struct {
	VRec *VerticaScrutinizeReconciler
	Vscr *vapi.VerticaScrutinize
	Vdb  *v1.VerticaDB
	Log  logr.Logger
	VInf *version.Info
}

func MakeVDBVerifyReconciler(r *VerticaScrutinizeReconciler, vscr *vapi.VerticaScrutinize,
	log logr.Logger, vinf *version.Info) controllers.ReconcileActor {
	return &VDBVerifyReconciler{
		VRec: r,
		Vscr: vscr,
		Log:  log.WithName("VDBVerifyReconciler"),
		Vdb:  &v1.VerticaDB{},
		VInf: vinf,
	}
}

// Reconcile will verify the VerticaDB in the Vscr CR exists
func (s *VDBVerifyReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// This reconciler is intended to be the first thing we run.  We want early
	// feedback if the VerticaDB that is referenced in the vscr doesn't exist.
	// This will print out an event if the VerticaDB cannot be found.
	if res, err := vk8s.FetchVDB(ctx, s.VRec, s.Vscr, s.Vscr.ExtractVDBNamespacedName(), s.Vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return s.checkVersion()
}

// checkVersion verifies that a vertica version supporting vcluster is running
func (s *VDBVerifyReconciler) checkVersion() (ctrl.Result, error) {
	vinf, ok := s.Vdb.MakeVersionInfo()
	if !ok {
		// the vertica version is not stored yet in the vdb
		// so let's requeue
		s.Log.Info("Vertica version is not available yet in the vdb, requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	s.VInf.Copy(vinf)
	if vinf.IsOlder(v1.VcluseropsAsDefaultDeploymentMethodMinVersion) {
		ver, _ := s.Vdb.GetVerticaVersionStr()
		s.VRec.Eventf(s.Vscr, corev1.EventTypeWarning, events.UnsupportedVerticaVersion,
			"the server version %s does not support vclusterops", ver)
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}
