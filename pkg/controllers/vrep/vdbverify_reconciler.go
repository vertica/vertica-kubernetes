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

package vrep

import (
	"context"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	vrepstatus "github.com/vertica/vertica-kubernetes/pkg/vrepstatus"
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const stateIncompatibleDB = "Incompatible"

type VdbVerifyReconciler struct {
	VRec *VerticaReplicatorReconciler
	Vrep *v1beta1.VerticaReplicator
	Log  logr.Logger
}

func MakeVdbVerifyReconciler(r *VerticaReplicatorReconciler, vrep *v1beta1.VerticaReplicator,
	log logr.Logger) controllers.ReconcileActor {
	return &VdbVerifyReconciler{
		VRec: r,
		Vrep: vrep,
		Log:  log.WithName("VdbVerifyReconciler"),
	}
}

// Reconcile will verify the verticaDBs in the vrep CR source and target exist and
// both vertica versions support the replication feature
func (r *VdbVerifyReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if the check has already been done once
	isSet := r.Vrep.IsStatusConditionTrue(v1beta1.ReplicationReady)
	if isSet {
		return ctrl.Result{}, nil
	}

	vdbSource := &vapi.VerticaDB{}
	vdbTarget := &vapi.VerticaDB{}
	nmSource := names.GenNamespacedName(r.Vrep, r.Vrep.Spec.Source.VerticaDB)
	nmTarget := names.GenNamespacedName(r.Vrep, r.Vrep.Spec.Target.VerticaDB)
	if res, err := vk8s.FetchVDB(ctx, r.VRec, r.Vrep, nmSource, vdbSource); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	if res, err := vk8s.FetchVDB(ctx, r.VRec, r.Vrep, nmTarget, vdbTarget); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// check version for both vdbs, the minimim source version should be 24.3.0
	// and the minimum target version should be 23.3.0
	vinfSource, err := vdbSource.MakeVersionInfoCheck()
	if err != nil {
		return ctrl.Result{}, err
	}
	vinfTarget, err := vdbTarget.MakeVersionInfoCheck()
	if err != nil {
		return ctrl.Result{}, err
	}

	if !vinfSource.IsEqualOrNewer(vapi.ReplicationViaVclusteropsSupportedMinVersion) {
		r.VRec.Eventf(r.Vrep, corev1.EventTypeWarning, events.ReplicationNotSupported,
			"The source Vertica version %q doesn't support replication with the vcluster library", vinfSource.VdbVer)
		err = vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationReady, metav1.ConditionFalse, "IncompatibleSourceDB")}, stateIncompatibleDB)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if !vinfTarget.IsEqualOrNewer(vapi.ReplicationSupportedMinVersion) {
		r.VRec.Eventf(r.Vrep, corev1.EventTypeWarning, events.ReplicationNotSupported,
			"The target Vertica version %q doesn't support replication", vinfTarget.VdbVer)
		err = vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationReady, metav1.ConditionFalse, "IncompatibleTargetDB")}, stateIncompatibleDB)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// source vdb should be deployed with vclusterops, not supported for admintools deployments
	if !vmeta.UseVClusterOps(vdbSource.Annotations) {
		r.VRec.Event(r.Vrep, corev1.EventTypeWarning, events.VrepAdmintoolsNotSupported,
			"replication is not supported for admintools deployments in in the source")
		err = vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationReady, metav1.ConditionFalse, "AdmintoolsNotSupported")}, stateIncompatibleDB)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
		[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationReady, metav1.ConditionTrue, "Ready")}, "Ready")
}
