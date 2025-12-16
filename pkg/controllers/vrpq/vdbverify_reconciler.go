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

package vrpq

import (
	"context"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const stateIncompatibleDB = "Incompatible"

type VdbVerifyReconciler struct {
	VRec *VerticaRestorePointsQueryReconciler
	Vrpq *v1beta1.VerticaRestorePointsQuery
	Vdb  *vapi.VerticaDB
	Log  logr.Logger
}

func MakeVdbVerifyReconciler(r *VerticaRestorePointsQueryReconciler, vrpq *v1beta1.VerticaRestorePointsQuery,
	log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &VdbVerifyReconciler{
		VRec: r,
		Vrpq: vrpq,
		Vdb:  vdb,
		Log:  log.WithName("VdbVerifyReconciler"),
	}
}

// Reconcile will verify the VerticaDB in the Vrpq CR exists, vclusterops is enabled and
// the vertica version supports vclusterops deployment
func (q *VdbVerifyReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if QueryReady is present (either true or false)
	isPresent := q.Vrpq.IsStatusConditionPresent(v1beta1.QueryReady)
	if isPresent {
		return ctrl.Result{}, nil
	}

	// check version for vdb, the minimim version should be 24.2.0
	vinf, err := q.Vdb.MakeVersionInfoCheck()
	if err != nil {
		return ctrl.Result{}, err
	}
	if !vinf.IsEqualOrNewer(vapi.RestoreSupportedMinVersion) {
		q.VRec.Eventf(q.Vrpq, corev1.EventTypeWarning, events.RestoreNotSupported,
			"The Vertica version %q doesn't support in-database restore points", vinf.VdbVer)
		err = vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.QueryReady, metav1.ConditionFalse, "IncompatibleDB")}, stateIncompatibleDB, nil)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Should be deployed with vclusterops, not supported for admintools deployments
	if !q.Vdb.UseVClusterOpsDeployment() {
		q.VRec.Event(q.Vrpq, corev1.EventTypeWarning, events.VrpqAdmintoolsNotSupported,
			"ShowRestorePoints is not supported for admintools deployments")
		err = vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.QueryReady, metav1.ConditionFalse, "AdmintoolsNotSupported")}, stateIncompatibleDB, nil)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		[]*metav1.Condition{vapi.MakeCondition(v1beta1.QueryReady, metav1.ConditionTrue, "Completed")}, stateQuerying, nil)
}
