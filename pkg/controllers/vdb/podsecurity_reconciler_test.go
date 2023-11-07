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

package vdb

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("podsecurity_reconcile", func() {
	ctx := context.Background()

	It("should set PodSecurityContext if not already set", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Spec.NMATLSSecret = "psc-nma-secret"
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakePodSecurityReconciler(vdbRec, logger, vdb)
		Ω(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Ω(vdb.Spec.PodSecurityContext).ShouldNot(BeNil())
		Ω(vdb.Spec.PodSecurityContext.FSGroup).ShouldNot(BeNil())
		Ω(*vdb.Spec.PodSecurityContext.FSGroup).Should(Equal(int64(DefaultFSGroupID)))
		Ω(vdb.Spec.PodSecurityContext.RunAsUser).ShouldNot(BeNil())
		Ω(*vdb.Spec.PodSecurityContext.RunAsUser).Should(Equal(int64(DefaultFSGroupID)))
	})

	It("should allow IDs to be overridden", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		var fsGroup int64 = 9999
		var runAsUser int64 = 9998
		vdb.Spec.PodSecurityContext = &corev1.PodSecurityContext{
			FSGroup:   &fsGroup,
			RunAsUser: &runAsUser,
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakePodSecurityReconciler(vdbRec, logger, vdb)
		Ω(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Ω(vdb.Spec.PodSecurityContext).ShouldNot(BeNil())
		Ω(vdb.Spec.PodSecurityContext.FSGroup).ShouldNot(BeNil())
		Ω(*vdb.Spec.PodSecurityContext.FSGroup).Should(Equal(fsGroup))
		Ω(*vdb.Spec.PodSecurityContext.FSGroup).ShouldNot(Equal(int64(DefaultFSGroupID)))
		Ω(vdb.Spec.PodSecurityContext.RunAsUser).ShouldNot(BeNil())
		Ω(*vdb.Spec.PodSecurityContext.RunAsUser).Should(Equal(runAsUser))
		Ω(*vdb.Spec.PodSecurityContext.RunAsUser).ShouldNot(Equal(int64(DefaultFSGroupID)))
	})

	It("should setup PodSecurityContext using range for OpenShift in vclusterops", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Spec.NMATLSSecret = "os-nma-secret"
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		// Setup a namespace with the OpenShift annotations. This defines the
		// valid ID that can be used, so will affect the expected result.
		namespace := corev1.Namespace{}
		nm := types.NamespacedName{
			Name: vdb.Namespace,
		}
		Ω(k8sClient.Get(ctx, nm, &namespace)).Should(Succeed())

		if namespace.Annotations == nil {
			namespace.Annotations = make(map[string]string)
		}
		namespace.Annotations[OpenShiftGroupRangeAnnotation] = "1001070000/10000"
		namespace.Annotations[OpenShiftUIDRangeAnnotation] = "1001080000/10000"
		Ω(k8sClient.Update(ctx, &namespace)).Should(Succeed())

		r := MakePodSecurityReconciler(vdbRec, logger, vdb)
		Ω(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Ω(vdb.Spec.PodSecurityContext).ShouldNot(BeNil())
		Ω(vdb.Spec.PodSecurityContext.FSGroup).ShouldNot(BeNil())
		Ω(*vdb.Spec.PodSecurityContext.FSGroup).Should(Equal(int64(1001070000)))
		Ω(vdb.Spec.PodSecurityContext.RunAsUser).ShouldNot(BeNil())
		Ω(*vdb.Spec.PodSecurityContext.RunAsUser).Should(Equal(int64(1001080000)))
	})
})
