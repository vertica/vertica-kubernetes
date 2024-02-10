/*
Copyright [2021-2023] Open Text.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
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
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	"github.com/vertica/vertica-kubernetes/pkg/vscrstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ScrutinizePodReconciler will reconcile for the scrutinize
// pod creation
type ScrutinizePodReconciler struct {
	VRec *VerticaScrutinizeReconciler
	Vscr *v1beta1.VerticaScrutinize
	Log  logr.Logger
}

// MakeScrutinizePodReconciler will build a ScrutinizePodReconciler object
func MakeScrutinizePodReconciler(r *VerticaScrutinizeReconciler, vscr *v1beta1.VerticaScrutinize,
	log logr.Logger) controllers.ReconcileActor {
	return &ScrutinizePodReconciler{
		VRec: r,
		Vscr: vscr,
		Log:  log.WithName("ScrutinizePodReconciler"),
	}
}

func (s *ScrutinizePodReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if ScrutinizeReady is false. This means that VerticaDB
	// was not found or is not configured for vclusterops
	if s.Vscr.IsStatusConditionFalse(v1beta1.ScrutinizeReady) {
		return ctrl.Result{}, nil
	}

	isSet := s.Vscr.IsStatusConditionTrue(v1beta1.ScrutinizePodCreated)
	if isSet {
		return ctrl.Result{}, nil
	}

	// collect information from a VerticaDB.
	if res, err := s.collectInfoFromVdb(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, s.createPod(ctx)
}

// collectInfoFromVdb pull data from the VerticaDB so that we can provide all of the parameters
// to the vcluster scrutinize CLI
// the logic to collect those data will be added after VER-91241
func (s *ScrutinizePodReconciler) collectInfoFromVdb(ctx context.Context) (ctrl.Result, error) {
	vdb := &v1.VerticaDB{}

	if res, err := vk8s.FetchVDB(ctx, s.VRec, s.Vscr, s.Vscr.ExtractVDBNamespacedName(), vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, nil
}

// createPod creates the scrutinize pod
func (s *ScrutinizePodReconciler) createPod(ctx context.Context) error {
	pod := builder.BuildScrutinizePod(s.Vscr)
	s.Log.Info("Creating scrutinize pod", "Name", s.Vscr.ExtractNamespacedName())
	err := ctrl.SetControllerReference(s.Vscr, pod, s.VRec.Scheme)
	if err != nil {
		return err
	}
	err = s.VRec.Client.Create(ctx, pod)
	if err != nil {
		return err
	}
	s.Log.Info("Scrutinize pod created successfully")
	stat := &v1beta1.VerticaScrutinizeStatus{}
	stat.PodName = pod.Name
	stat.PodUID = pod.UID
	stat.Conditions = []metav1.Condition{*v1.MakeCondition(v1beta1.ScrutinizePodCreated, metav1.ConditionTrue, "PodCreated")}
	return vscrstatus.UpdateStatus(ctx, s.VRec.Client, s.Vscr, stat)
}
