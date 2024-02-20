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
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vscrstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PodPollingReconciler waits for the scrutinize collection to finish
type PodPollingReconciler struct {
	VRec *VerticaScrutinizeReconciler
	Vscr *v1beta1.VerticaScrutinize
	Log  logr.Logger
}

func MakePodPollingReconciler(r *VerticaScrutinizeReconciler, vscr *v1beta1.VerticaScrutinize,
	log logr.Logger) controllers.ReconcileActor {
	return &PodPollingReconciler{
		VRec: r,
		Vscr: vscr,
		Log:  log.WithName("PodPollingReconciler"),
	}
}

func (p *PodPollingReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if ScrutinizeReady is false, scrutinize collection
	// is done or scrutinize pod has not been created
	if p.Vscr.IsStatusConditionFalse(v1beta1.ScrutinizeReady) ||
		p.Vscr.IsStatusConditionTrue(v1beta1.ScrutinizeCollectionFinished) ||
		p.Vscr.IsStatusConditionFalse(v1beta1.ScrutinizePodCreated) {
		return ctrl.Result{}, nil
	}

	pod := &corev1.Pod{}
	ok, err := p.fetchScrutinizePod(ctx, pod)
	if !ok {
		return ctrl.Result{}, err
	}

	return p.checkScrutinizeContainerStatus(ctx, pod)
}

// checkScrutinizeContainerStatus checks the status of the scrutinize pod
// and update status conditions based on what is found
func (p *PodPollingReconciler) checkScrutinizeContainerStatus(ctx context.Context, pod *corev1.Pod) (ctrl.Result, error) {
	cntStatus := getNamedContainerStatus(pod.Status.InitContainerStatuses, names.ScrutinizeInitContainer)
	if cntStatus == nil {
		return ctrl.Result{}, fmt.Errorf("could not find scrutinize container status")
	}
	if !cntStatus.Ready {
		if cntStatus.State.Terminated != nil {
			p.VRec.Eventf(p.Vscr, corev1.EventTypeWarning, events.VclusterOpsScrutinizeFailed,
				"Vcluster scrutinize run failed")
			cond := v1.MakeCondition(v1beta1.ScrutinizeCollectionFinished, metav1.ConditionTrue, events.VclusterOpsScrutinizeFailed)
			return ctrl.Result{}, vscrstatus.UpdateCondition(ctx, p.VRec.Client, p.Vscr, cond)
		}
		if cntStatus.State.Running != nil {
			p.Log.Info("Vcluster scrutinize run in progress")
			return ctrl.Result{Requeue: true}, nil
		}
		// if the scrutinize init container is neither running nor terminated then
		// it is in waiting state. We requeue
		p.Log.Info("Waiting for the scrutinize container to start running")
		return ctrl.Result{Requeue: true}, nil
	}
	p.VRec.Eventf(p.Vscr, corev1.EventTypeNormal, events.VclusterOpsScrutinizeSucceeded,
		"Successfully completed scrutinize run for the VerticaDB named '%s'", p.Vscr.Spec.VerticaDBName)
	cond := v1.MakeCondition(v1beta1.ScrutinizeCollectionFinished, metav1.ConditionTrue, events.VclusterOpsScrutinizeSucceeded)
	return ctrl.Result{}, vscrstatus.UpdateCondition(ctx, p.VRec.Client, p.Vscr, cond)
}

func getNamedContainerStatus(cntStatuses []corev1.ContainerStatus, cntName string) *corev1.ContainerStatus {
	for i := range cntStatuses {
		if cntStatuses[i].Name == cntName {
			return &cntStatuses[i]
		}
	}
	return nil
}

func (p *PodPollingReconciler) fetchScrutinizePod(ctx context.Context, pod *corev1.Pod) (bool, error) {
	err := p.VRec.Client.Get(ctx, p.Vscr.ExtractNamespacedName(), pod)
	if err != nil {
		if errors.IsNotFound(err) {
			p.Log.Info("Scrutinize pod not found.")
			return false, nil
		}
		p.Log.Error(err, "failed to get Scrutinize pod")
		return false, err
	}
	return true, nil
}
