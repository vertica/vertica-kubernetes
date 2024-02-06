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
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VDBVerifyReconciler will verify the VerticaDB in the Vscr CR exists
type VDBVerifyReconciler struct {
	VRec *VerticaScrutinizeReconciler
	Vscr *vapi.VerticaScrutinize
	Vdb  *v1.VerticaDB
	Log  logr.Logger
}

func MakeVDBVerifyReconciler(r *VerticaScrutinizeReconciler, vscr *vapi.VerticaScrutinize,
	log logr.Logger) controllers.ReconcileActor {
	return &VDBVerifyReconciler{VRec: r, Vscr: vscr, Log: log, Vdb: &v1.VerticaDB{}}
}

// Reconcile will verify the VerticaDB in the Vscr CR exists
func (s *VDBVerifyReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// This reconciler is intended to be the first thing we run.  We want early
	// feedback if the VerticaDB that is referenced in the vscr doesn't exist.
	// This will print out an event if the VerticaDB cannot be found.
	if res, err := fetchVDB(ctx, s.VRec, s.Vscr, s.Vdb); verrors.IsReconcileAborted(res, err) {
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
	if vinf.IsOlder(v1.VcluseropsAsDefaultDeploymentMethodMinVersion) {
		ver, _ := s.Vdb.GetVerticaVersionStr()
		return ctrl.Result{}, fmt.Errorf("the server version %s does not support vclusterops",
			ver)
	}
	return ctrl.Result{}, nil
}
