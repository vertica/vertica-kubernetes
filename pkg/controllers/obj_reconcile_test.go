/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var _ = Describe("obj_reconcile", func() {
	ctx := context.Background()

	createCrd := func(vdb *vapi.VerticaDB) {
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		nameLookup := vdb.ExtractNamespacedName()

		createdVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, nameLookup, createdVdb)).Should(Succeed())

		// Create any dependent objects for the CRD.
		pfacts := MakePodFacts(k8sClient, &cmds.FakePodRunner{})
		objr := MakeObjReconciler(k8sClient, scheme.Scheme, logger, createdVdb, &pfacts)
		_, err := objr.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
	}

	deleteCrd := func(vdb *vapi.VerticaDB) {
		Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed())

		isController := true
		blockOwnerDeletion := true
		expOwnerRef := metav1.OwnerReference{
			Kind:               "VerticaDB",
			APIVersion:         "vertica.com/v1beta1",
			Name:               vdb.Name,
			UID:                vdb.UID,
			Controller:         &isController,
			BlockOwnerDeletion: &blockOwnerDeletion,
		}

		for i := range vdb.Spec.Subclusters {
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[i]), svc)).Should(Succeed())
			Expect(svc.ObjectMeta.OwnerReferences).To(ContainElement(expOwnerRef))
			Expect(k8sClient.Delete(ctx, svc)).Should(Succeed())

			stsNm := names.GenStsName(vdb, &vdb.Spec.Subclusters[i])
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, stsNm, sts)).Should(Succeed())
			Expect(sts.ObjectMeta.OwnerReferences).To(ContainElement(expOwnerRef))
			Expect(k8sClient.Delete(ctx, sts)).Should(Succeed())
		}
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, names.GenHlSvcName(vdb), svc)).Should(Succeed())
		Expect(svc.ObjectMeta.OwnerReferences).To(ContainElement(expOwnerRef))
		Expect(k8sClient.Delete(ctx, svc)).Should(Succeed())
	}

	Context("When reconciling a VerticaDB CRD", func() {
		It("Should create service objects", func() {
			vdb := vapi.MakeVDB()
			createCrd(vdb)
			defer deleteCrd(vdb)

			By("Checking the VerticaDB has an external service object")
			extNameLookup := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			foundSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, extNameLookup, foundSvc)).Should(Succeed())
			Expect(foundSvc.Spec.ClusterIP).ShouldNot(Equal("None"))
			Expect(foundSvc.Spec.Type).Should(Equal(corev1.ServiceTypeClusterIP))
			Expect(foundSvc.Spec.Ports[0].Port).Should(Equal(int32(5433)))
			Expect(foundSvc.Spec.Ports[1].Port).Should(Equal(int32(5444)))

			By("Checking the VerticaDB has a headless service object")
			hlNameLookup := names.GenHlSvcName(vdb)
			foundSvc = &corev1.Service{}
			Expect(k8sClient.Get(ctx, hlNameLookup, foundSvc)).Should(Succeed())
			Expect(foundSvc.Spec.ClusterIP).Should(Equal("None"))
			Expect(foundSvc.Spec.Type).Should(Equal(corev1.ServiceTypeClusterIP))
			Expect(foundSvc.Spec.Ports[0].Port).Should(Equal(int32(22)))
		})

		It("should have custom type, nodePort, and externalIPs and update them in ext service", func() {
			vdb := vapi.MakeVDB()
			desiredType := corev1.ServiceTypeNodePort
			desiredNodePort := int32(30046)
			desiredExternalIPs := []string{"80.10.11.12"}
			vdb.Spec.Subclusters[0].ServiceType = desiredType
			vdb.Spec.Subclusters[0].NodePort = desiredNodePort
			vdb.Spec.Subclusters[0].ExternalIPs = desiredExternalIPs

			createCrd(vdb)
			defer deleteCrd(vdb)

			extNameLookup := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			foundSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, extNameLookup, foundSvc)).Should(Succeed())
			Expect(foundSvc.Spec.Type).Should(Equal(desiredType))
			Expect(foundSvc.Spec.Ports[0].NodePort).Should(Equal(desiredNodePort))
			Expect(foundSvc.Spec.ExternalIPs).Should(Equal(desiredExternalIPs))

			// Update crd
			newType := corev1.ServiceTypeLoadBalancer
			newNodePort := int32(30047)
			newExternalIPs := []string{"80.10.11.10"}
			vdb.Spec.Subclusters[0].ServiceType = newType
			vdb.Spec.Subclusters[0].NodePort = newNodePort
			vdb.Spec.Subclusters[0].ExternalIPs = newExternalIPs
			Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

			// Refresh any dependent objects
			pfacts := MakePodFacts(k8sClient, &cmds.FakePodRunner{})
			objr := MakeObjReconciler(k8sClient, scheme.Scheme, logger, vdb, &pfacts)
			_, err := objr.Reconcile(ctx, &ctrl.Request{})
			Expect(err).Should(Succeed())

			Expect(k8sClient.Get(ctx, extNameLookup, foundSvc)).Should(Succeed())
			Expect(foundSvc.Spec.Type).Should(Equal(newType))
			Expect(foundSvc.Spec.Ports[0].NodePort).Should(Equal(newNodePort))
			Expect(foundSvc.Spec.ExternalIPs).Should(Equal(newExternalIPs))
		})

		It("should have custom labels and annotations in service objects and statefulsets", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Labels["my-label"] = "r1"
			vdb.Spec.Labels["vertica.com/second-label"] = "r2"
			vdb.Spec.Annotations["gitRef"] = "1234abc"

			verifyLabelsAnnotations := func(objectMeta *metav1.ObjectMeta, isScSpecific bool) {
				Expect(objectMeta.Labels["my-label"]).Should(Equal("r1"))
				Expect(objectMeta.Labels["vertica.com/second-label"]).Should(Equal("r2"))
				Expect(objectMeta.Annotations["gitRef"]).Should(Equal("1234abc"))
				Expect(objectMeta.Labels["vertica.com/database"]).Should(Equal(vdb.Spec.DBName))
				if isScSpecific {
					Expect(objectMeta.Labels["vertica.com/subcluster"]).Should(Equal(vdb.Spec.Subclusters[0].Name))
				}
			}

			createCrd(vdb)
			defer deleteCrd(vdb)

			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
			verifyLabelsAnnotations(&svc.ObjectMeta, true /* subcluster specific */)
			Expect(k8sClient.Get(ctx, names.GenHlSvcName(vdb), svc)).Should(Succeed())
			verifyLabelsAnnotations(&svc.ObjectMeta, false /* not subcluster specific */)

			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
			verifyLabelsAnnotations(&sts.ObjectMeta, true /* subcluster specific */)
		})

		It("should create a statefulset with the configured size", func() {
			vdb := vapi.MakeVDB()
			var desiredSize int32 = 16
			vdb.Spec.Subclusters[0].Size = desiredSize

			createCrd(vdb)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			actSize := *sts.Spec.Replicas
			Expect(actSize).Should(Equal(desiredSize))
			Expect(sts.Spec.Template.Spec.Containers[0].Image).Should(Equal(vdb.Spec.Image))
		})

		It("should create a statefulset with a configured pull policy", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.ImagePullPolicy = corev1.PullNever

			createCrd(vdb)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(sts.Spec.Template.Spec.Containers[0].Image).Should(Equal(vdb.Spec.Image))
			Expect(sts.Spec.Template.Spec.Containers[0].ImagePullPolicy).Should(Equal(corev1.PullNever))
		})

		It("should create a statefulset with a configured StorageClassName", func() {
			vdb := vapi.MakeVDB()
			desiredStorageClass := "my-storage"
			vdb.Spec.Local.StorageClass = desiredStorageClass

			createCrd(vdb)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			currStorageClass := *sts.Spec.VolumeClaimTemplates[0].Spec.StorageClassName
			Expect(currStorageClass).Should(Equal(desiredStorageClass))
		})

		It("should create a statefulset with a configured NodeSelector", func() {
			vdb := vapi.MakeVDB()
			desiredNodeSelector := map[string]string{
				"disktype": "ssd",
				"region":   "us-east",
			}
			vdb.Spec.Subclusters[0].NodeSelector = desiredNodeSelector

			createCrd(vdb)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			currNodeSelector := sts.Spec.Template.Spec.NodeSelector
			Expect(currNodeSelector).Should(Equal(desiredNodeSelector))
		})

		It("should create a statefulset with a configured Affinity", func() {
			vdb := vapi.MakeVDB()
			desiredAffinity := &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "foo",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"a", "b", "c"},
									}, {
										Key:      "bar",
										Operator: corev1.NodeSelectorOpNotIn,
										Values:   []string{"d", "e", "f"},
									}, {
										Key:      "foo",
										Operator: corev1.NodeSelectorOpNotIn,
										Values:   []string{"g", "h"},
									},
								},
							},
						},
					},
				},
			}
			vdb.Spec.Subclusters[0].Affinity = desiredAffinity

			createCrd(vdb)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			currAffinity := sts.Spec.Template.Spec.Affinity
			Expect(*currAffinity).Should(Equal(*desiredAffinity))
		})

		It("should create a statefulset with a configured Tolerations", func() {
			vdb := vapi.MakeVDB()
			desiredTolerations := []corev1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}
			vdb.Spec.Subclusters[0].Tolerations = desiredTolerations

			createCrd(vdb)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			currTolerations := sts.Spec.Template.Spec.Tolerations
			Expect(currTolerations).Should(Equal(desiredTolerations))
		})

		It("should create a statefulset with configured Resources", func() {
			vdb := vapi.MakeVDB()
			rl := corev1.ResourceList{}
			rl[corev1.ResourceCPU] = resource.MustParse("1")
			rl[corev1.ResourceMemory] = resource.MustParse("1Gi")
			desiredResources := corev1.ResourceRequirements{
				Requests: rl,
			}
			vdb.Spec.Subclusters[0].Resources = desiredResources

			createCrd(vdb)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			currResources := sts.Spec.Template.Spec.Containers[0].Resources
			Expect(currResources).Should(Equal(desiredResources))
		})

		It("should create multiple sts if multiple subclusters are specified", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, vapi.Subcluster{
				Name: "analytics",
				Size: 8,
			})

			createCrd(vdb)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[1])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(sts.ObjectMeta.Name).Should(MatchRegexp(".*analytics$"))
			curSize := *sts.Spec.Replicas
			Expect(curSize).Should(Equal(int32(8)))
		})

		It("Increasing the size of the subcluster should cause the sts to scale out", func() {
			vdb := vapi.MakeVDB()
			createCrd(vdb)
			defer deleteCrd(vdb)
			origSize := vdb.Spec.Subclusters[0].Size

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			curSize := *sts.Spec.Replicas
			Expect(curSize).Should(Equal(origSize))

			newSize := int32(10)
			vdb.Spec.Subclusters[0].Size = newSize
			Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

			// Refresh any dependent objects
			pfacts := MakePodFacts(k8sClient, &cmds.FakePodRunner{})
			objr := MakeObjReconciler(k8sClient, scheme.Scheme, logger, vdb, &pfacts)
			_, err := objr.Reconcile(ctx, &ctrl.Request{})
			Expect(err).Should(Succeed())

			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			curSize = *sts.Spec.Replicas
			Expect(curSize).Should(Equal(newSize))
		})
	})
})
