/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("clientroutinglabel_reconcile", func() {
	ctx := context.Background()

	It("should add label to pods that have at least one shard subscription", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1},
			{Name: "sc2", Size: 1},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pfn1 := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pfacts.Detail[pfn1].upNode = true
		pfacts.Detail[pfn1].shardSubscriptions = 0
		pfn2 := names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0)
		pfacts.Detail[pfn2].shardSubscriptions = 3
		pfacts.Detail[pfn2].upNode = true
		r := MakeClientRoutingLabelReconciler(vdbRec, vdb, &pfacts, PodRescheduleApplyMethod, "")
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, pfn1, pod)).Should(Succeed())
		_, ok := pod.Labels[builder.ClientRoutingLabel]
		Expect(ok).Should(BeFalse())
		Expect(k8sClient.Get(ctx, pfn2, pod)).Should(Succeed())
		v, ok := pod.Labels[builder.ClientRoutingLabel]
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(builder.ClientRoutingVal))
	})

	It("should ignore second subcluster when sc filter is used", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1},
			{Name: "sc2", Size: 1},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pfn1 := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pfacts.Detail[pfn1].upNode = true
		pfacts.Detail[pfn1].shardSubscriptions = 5
		pfn2 := names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0)
		pfacts.Detail[pfn2].shardSubscriptions = 3
		pfacts.Detail[pfn2].upNode = true
		r := MakeClientRoutingLabelReconciler(vdbRec, vdb, &pfacts, PodRescheduleApplyMethod, vdb.Spec.Subclusters[0].Name)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, pfn1, pod)).Should(Succeed())
		v, ok := pod.Labels[builder.ClientRoutingLabel]
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(builder.ClientRoutingVal))
		Expect(k8sClient.Get(ctx, pfn2, pod)).Should(Succeed())
		_, ok = pod.Labels[builder.ClientRoutingLabel]
		Expect(ok).Should(BeFalse())
	})

	It("should requeue if at least one pod does not have subscriptions but other pods should have label", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 5},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		sc := &vdb.Spec.Subclusters[0]
		for i := int32(0); i < sc.Size; i++ {
			pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], i)
			pfacts.Detail[pn].upNode = true
			pfacts.Detail[pn].shardSubscriptions = int(i) // Ensures that only one pod will not have subscriptions
		}
		r := MakeClientRoutingLabelReconciler(vdbRec, vdb, &pfacts, AddNodeApplyMethod, "")
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

		pod := &corev1.Pod{}
		for i := int32(0); i < sc.Size; i++ {
			pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], i)
			Expect(k8sClient.Get(ctx, pn, pod)).Should(Succeed())
			v, ok := pod.Labels[builder.ClientRoutingLabel]
			if i == 0 {
				Expect(ok).Should(BeFalse())
			} else {
				Expect(ok).Should(BeTrue())
				Expect(v).Should(Equal(builder.ClientRoutingVal))
			}
		}
	})

	It("should remove label when pod is pending delete", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pfacts.Detail[pn].upNode = true
		pfacts.Detail[pn].shardSubscriptions = 10
		act := MakeClientRoutingLabelReconciler(vdbRec, vdb, &pfacts, AddNodeApplyMethod, "")
		r := act.(*ClientRoutingLabelReconciler)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, pn, pod)).Should(Succeed())
		v, ok := pod.Labels[builder.ClientRoutingLabel]
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(builder.ClientRoutingVal))

		pfacts.Detail[pn].pendingDelete = true
		r.ApplyMethod = DelNodeApplyMethod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		Expect(k8sClient.Get(ctx, pn, pod)).Should(Succeed())
		_, ok = pod.Labels[builder.ClientRoutingLabel]
		Expect(ok).Should(BeFalse())
	})
})
