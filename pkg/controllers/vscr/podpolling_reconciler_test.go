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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("podpolling_reconciler", func() {
	ctx := context.Background()

	It("should reconcile based on the scrutinize pod status", func() {
		vscr := v1beta1.MakeVscr()
		v1beta1_test.CreateVSCR(ctx, k8sClient, vscr)
		defer v1beta1_test.DeleteVSCR(ctx, k8sClient, vscr)
		cond := v1.MakeCondition(v1beta1.ScrutinizeReady, metav1.ConditionTrue, "")
		meta.SetStatusCondition(&vscr.Status.Conditions, *cond)
		cond = v1.MakeCondition(v1beta1.ScrutinizePodCreated, metav1.ConditionTrue, "")
		meta.SetStatusCondition(&vscr.Status.Conditions, *cond)
		v1beta1_test.CreateScrutinizePod(ctx, k8sClient, vscr)
		defer v1beta1_test.DeleteScrutinizePod(ctx, k8sClient, vscr)

		pn := vscr.ExtractNamespacedName()
		pod := corev1.Pod{}
		Expect(k8sClient.Get(ctx, pn, &pod)).Should(Succeed())
		// scrutinize init container completed successfully
		pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
			{
				Name:  names.ScrutinizeInitContainer,
				Ready: true,
			},
		}
		Expect(k8sClient.Status().Update(ctx, &pod)).Should(Succeed())

		runPodPollingReconcile(ctx, vscr, false)
		checkStatusConditionAfterReconcile(ctx, vscr, v1beta1.ScrutinizeCollectionFinished,
			metav1.ConditionTrue, events.VclusterOpsScrutinizeSucceeded)

		// scrutinize init container is still busy running scrutinize command
		pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
			{
				Name:  names.ScrutinizeInitContainer,
				Ready: false,
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{},
				},
			},
		}
		meta.RemoveStatusCondition(&vscr.Status.Conditions, v1beta1.ScrutinizeCollectionFinished)
		Expect(k8sClient.Status().Update(ctx, &pod)).Should(Succeed())
		runPodPollingReconcile(ctx, vscr, true)

		// scrutinize init container in waiting state
		pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
			{
				Name:  names.ScrutinizeInitContainer,
				Ready: false,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{},
				},
			},
		}
		meta.RemoveStatusCondition(&vscr.Status.Conditions, v1beta1.ScrutinizeCollectionFinished)
		Expect(k8sClient.Status().Update(ctx, &pod)).Should(Succeed())
		runPodPollingReconcile(ctx, vscr, true)

		// scrutinize init container failed
		pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
			{
				Name:  names.ScrutinizeInitContainer,
				Ready: false,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{},
				},
			},
		}
		meta.RemoveStatusCondition(&vscr.Status.Conditions, v1beta1.ScrutinizeCollectionFinished)
		Expect(k8sClient.Status().Update(ctx, &pod)).Should(Succeed())
		runPodPollingReconcile(ctx, vscr, false)
		checkStatusConditionAfterReconcile(ctx, vscr, v1beta1.ScrutinizeCollectionFinished,
			metav1.ConditionTrue, events.VclusterOpsScrutinizeFailed)

	})

	It("should exit early based on some status conditions", func() {
		vscr := v1beta1.MakeVscr()
		v1beta1_test.CreateVSCR(ctx, k8sClient, vscr)
		defer v1beta1_test.DeleteVSCR(ctx, k8sClient, vscr)
		cond := v1.MakeCondition(v1beta1.ScrutinizeReady, metav1.ConditionFalse, "")
		meta.SetStatusCondition(&vscr.Status.Conditions, *cond)

		runPodPollingReconcile(ctx, vscr, false)
		Expect(vscr.IsStatusConditionPresent(v1beta1.ScrutinizeCollectionFinished)).Should(BeFalse())

		cond = v1.MakeCondition(v1beta1.ScrutinizeReady, metav1.ConditionTrue, "")
		meta.SetStatusCondition(&vscr.Status.Conditions, *cond)
		cond = v1.MakeCondition(v1beta1.ScrutinizePodCreated, metav1.ConditionFalse, "")
		meta.SetStatusCondition(&vscr.Status.Conditions, *cond)
		runPodPollingReconcile(ctx, vscr, false)
		Expect(vscr.IsStatusConditionPresent(v1beta1.ScrutinizeCollectionFinished)).Should(BeFalse())

		cond = v1.MakeCondition(v1beta1.ScrutinizePodCreated, metav1.ConditionTrue, "")
		meta.SetStatusCondition(&vscr.Status.Conditions, *cond)
		cond = v1.MakeCondition(v1beta1.ScrutinizeCollectionFinished, metav1.ConditionTrue, "")
		meta.SetStatusCondition(&vscr.Status.Conditions, *cond)
		runPodPollingReconcile(ctx, vscr, false)
	})
})

func runPodPollingReconcile(ctx context.Context, vscr *v1beta1.VerticaScrutinize, requeue bool) {
	r := MakePodPollingReconciler(vscrRec, vscr, logger)
	res, err := r.Reconcile(ctx, &ctrl.Request{})
	Expect(err).Should(Succeed())
	if requeue {
		Expect(res).Should(Equal(ctrl.Result{Requeue: true}))
	} else {
		Expect(res).Should(Equal(ctrl.Result{}))
	}
}
