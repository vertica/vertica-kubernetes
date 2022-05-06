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

var _ = Describe("podannotation_reconcile", func() {
	ctx := context.Background()

	It("should fetch node information and include it in the pod as annotations", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		// The pod we created will already have the annotatons we want to add.
		// We remove them to test having the reconciler add them.
		pod := &corev1.Pod{}
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Expect(k8sClient.Get(ctx, pn, pod)).Should(Succeed())
		pod.SetAnnotations(map[string]string{})

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		act := MakePodAnnotationReconciler(vdbRec, vdb, &pfacts)
		Expect(act.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		Expect(k8sClient.Get(ctx, pn, pod)).Should(Succeed())
		Expect(pod.Annotations[builder.KubernetesBuildDateAnnotation]).ShouldNot(Equal(""))
		Expect(pod.Annotations[builder.KubernetesGitCommitAnnotation]).ShouldNot(Equal(""))
		Expect(pod.Annotations[builder.KubernetesVersionAnnotation]).ShouldNot(Equal(""))
	})
})
