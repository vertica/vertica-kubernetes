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

package vas

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VDBVerifyReconciler will verify the VerticaDB in the VAS CR exists
type VDBVerifyReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *v1beta1.VerticaAutoscaler
	Vdb  *vapi.VerticaDB
}

func MakeVDBVerifyReconciler(r *VerticaAutoscalerReconciler, vas *v1beta1.VerticaAutoscaler) controllers.ReconcileActor {
	return &VDBVerifyReconciler{VRec: r, Vas: vas, Vdb: &vapi.VerticaDB{}}
}

// Reconcile will verify the VerticaDB in the VAS CR exists
func (s *VDBVerifyReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// This reconciler is intended to be the first thing we run.  We want early
	// feedback if the VerticaDB that is referenced in the vas doesn't exist.
	// This will print out an event if the VerticaDB cannot be found.
	res, err := fetchVDB(ctx, s.VRec, s.Vas, s.Vdb)
	if !s.Vas.IsCustomAutoScalerSet() || verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	vinf, vErr := s.Vdb.MakeVersionInfoCheck()
	if vErr != nil {
		return ctrl.Result{}, err
	}
	if !vinf.IsEqualOrNewer(vapi.PrometheusMetricsMinVersion) {
		ver, _ := s.Vdb.GetVerticaVersionStr()
		s.VRec.Eventf(s.Vas, corev1.EventTypeWarning, events.PrometheusMetricsNotSupported,
			"The server version %s does not support prometheus metrics", ver)
	}
	return res, err
}
