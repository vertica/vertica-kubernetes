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

package vdb

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("crashloop_reconciler", func() {
	ctx := context.Background()

	It("should write an event if server container fails to start", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		pod := &corev1.Pod{}
		podnm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Ω(k8sClient.Get(ctx, podnm, pod)).Should(Succeed())

		// Mock in a status showing the server pod not starting.
		started := false
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: names.ServerContainer,
				Ready:   false,
				Started: &started,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason:  "CreateContainerError",
						Message: `failed to generate container "abcd" spec: failed to apply OCI options: no command specified'`,
					},
				},
			},
		}
		Ω(k8sClient.Status().Update(ctx, pod)).Should(Succeed())

		r := MakeCrashLoopReconciler(vdbRec, logger, vdb)
		Ω(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		eventList := test.GetEventForObj(ctx, k8sClient, vdb)
		Ω(len(eventList.Items)).Should(Equal(1))
		Ω(eventList.Items[0].Reason).Should(Equal(events.WrongImage))
	})

	It("should write an event if nma container is in a crash loop", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		pod := &corev1.Pod{}
		podnm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Ω(k8sClient.Get(ctx, podnm, pod)).Should(Succeed())

		// Mock in a status showing the nma pod is in a crash loop
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: names.NMAContainer,
				Ready:        false,
				RestartCount: 5,
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason: "StartError",
					},
				},
			},
		}
		Ω(k8sClient.Status().Update(ctx, pod)).Should(Succeed())

		r := MakeCrashLoopReconciler(vdbRec, logger, vdb)
		Ω(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		eventList := test.GetEventForObj(ctx, k8sClient, vdb)
		Ω(len(eventList.Items)).Should(Equal(1))
		Ω(eventList.Items[0].Reason).Should(Equal(events.WrongImage))
	})
})
