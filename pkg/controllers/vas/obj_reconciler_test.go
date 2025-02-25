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

package vas

import (
	"context"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("obj_reconcile", func() {
	ctx := context.Background()

	It("should create/update hpa", func() {
		vas := vapi.MakeVASWithMetrics()
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteHPA(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		nm := names.GenHPAName(vas)
		Expect(k8sClient.Get(ctx, nm, hpa)).Should(Succeed())
		Expect(hpa.Spec.ScaleTargetRef.Name).Should(Equal(vas.Name))
		Expect(*hpa.Spec.MinReplicas).Should(Equal(*vas.Spec.CustomAutoscaler.Hpa.MinReplicas))
		Expect(hpa.Spec.MaxReplicas).Should(Equal(vas.Spec.CustomAutoscaler.Hpa.MaxReplicas))
		Expect(hpa.Spec.Metrics[0].Resource.Name).Should(Equal(corev1.ResourceName("cpu")))

		// Update hpa
		newRep := int32(10)
		vas.Spec.CustomAutoscaler.Hpa.MaxReplicas = newRep
		r = MakeObjReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())
		Expect(k8sClient.Get(ctx, nm, hpa)).Should(Succeed())
		Expect(hpa.Spec.MaxReplicas).Should(Equal(newRep))
	})

	It("should create/update scaledObject", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		so := &kedav1alpha1.ScaledObject{}
		nm := names.GenScaledObjectName(vas)
		Expect(k8sClient.Get(ctx, nm, so)).Should(Succeed())
		Expect(so.Spec.ScaleTargetRef.Name).Should(Equal(vas.Name))
		Expect(*so.Spec.MinReplicaCount).Should(Equal(*vas.Spec.CustomAutoscaler.ScaledObject.MinReplicas))
		Expect(*so.Spec.MaxReplicaCount).Should(Equal(*vas.Spec.CustomAutoscaler.ScaledObject.MaxReplicas))
		Expect(len(so.Spec.Triggers)).Should(Equal(len(vas.Spec.CustomAutoscaler.ScaledObject.Metrics)))
		metric := &vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0]
		Expect(so.Spec.Triggers[0].Metadata["serverAddress"]).Should(Equal(metric.Prometheus.ServerAddress))

		// Update scaledObject
		newRep := int32(10)
		vas.Spec.CustomAutoscaler.ScaledObject.MaxReplicas = &newRep
		r = MakeObjReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())
		Expect(k8sClient.Get(ctx, nm, so)).Should(Succeed())
		Expect(*so.Spec.MaxReplicaCount).Should(Equal(newRep))
	})
})
