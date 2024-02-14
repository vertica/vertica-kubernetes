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

package vrpq

import (
	"context"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type PrecheckQueryReconciler struct {
	VRec *VerticaRestorePointsQueryReconciler
	Vrpq *vapi.VerticaRestorePointsQuery
	Log  logr.Logger
}

func MakePreCheckQueryReconciler(r *VerticaRestorePointsQueryReconciler, vrpq *vapi.VerticaRestorePointsQuery,
	log logr.Logger) controllers.ReconcileActor {
	return &PrecheckQueryReconciler{
		VRec: r,
		Vrpq: vrpq,
		Log:  log.WithName("PrecheckQueryReconciler"),
	}
}

func (q *PrecheckQueryReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	vdb := &v1.VerticaDB{}
	nm := names.GenNamespacedName(q.Vrpq, q.Vrpq.Spec.VerticaDBName)
	if res, err := vk8s.FetchVDB(ctx, q.VRec, q.Vrpq, nm, vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// check version for Vdb, the minimim version should be 24.2.0
	vinf, err := vdb.MakeVersionInfoCheck()
	if err != nil {
		return ctrl.Result{}, nil
	}
	if !vinf.IsEqualOrNewer(v1.RestoreSupportedMinVersion) {
		q.VRec.Event(q.Vrpq, corev1.EventTypeWarning, events.RestoreNotSupported, "Incompatibility with the database")
		err = vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
			[]*metav1.Condition{v1.MakeCondition(vapi.QueryReady, metav1.ConditionFalse, "IncompatibleDB")}, stateFailedQuery, nil)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Should be deployed with vclusterops, not supported for admintools deployments
	if !vmeta.UseVClusterOps(vdb.Annotations) {
		q.VRec.Event(q.Vrpq, corev1.EventTypeWarning, events.AdmintoolsNotSupported,
			"ShowRestorePoints is not supported for admintools deployments")
		err = vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
			[]*metav1.Condition{v1.MakeCondition(vapi.QueryReady, metav1.ConditionFalse, "AdmintoolsNotSupported")}, stateFailedQuery, nil)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}
