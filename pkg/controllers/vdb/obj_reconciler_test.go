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
	"path/filepath"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var _ = Describe("obj_reconcile", func() {
	ctx := context.Background()

	runReconciler := func(vdb *vapi.VerticaDB, expResult ctrl.Result, mode ObjReconcileModeType) {
		// Create any dependent objects for the CRD.
		pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		objr := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, mode)
		Expect(objr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(expResult))
	}

	createCrd := func(vdb *vapi.VerticaDB, doReconcile bool) {
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		nameLookup := vdb.ExtractNamespacedName()

		createdVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, nameLookup, createdVdb)).Should(Succeed())

		if doReconcile {
			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)
		}
	}

	deleteCrd := func(vdb *vapi.VerticaDB) {
		Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed())

		isController := true
		blockOwnerDeletion := true
		expOwnerRef := metav1.OwnerReference{
			Kind:               vapi.VerticaDBKind,
			APIVersion:         vapi.GroupVersion.String(),
			Name:               vdb.Name,
			UID:                vdb.UID,
			Controller:         &isController,
			BlockOwnerDeletion: &blockOwnerDeletion,
		}

		for i := range vdb.Spec.Subclusters {
			svc := &corev1.Service{}
			err := k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[i]), svc)
			if err == nil {
				Expect(svc.ObjectMeta.OwnerReferences).To(ContainElement(expOwnerRef))
				Expect(k8sClient.Delete(ctx, svc)).Should(Succeed())
			} else {
				Expect(errors.IsNotFound(err)).Should(BeTrue())
			}

			stsNm := names.GenStsName(vdb, &vdb.Spec.Subclusters[i])
			sts := &appsv1.StatefulSet{}
			err = k8sClient.Get(ctx, stsNm, sts)
			if err == nil {
				Expect(sts.ObjectMeta.OwnerReferences).To(ContainElement(expOwnerRef))
				Expect(k8sClient.Delete(ctx, sts)).Should(Succeed())
			} else {
				Expect(errors.IsNotFound(err)).Should(BeTrue())
			}
		}
		svc := &corev1.Service{}
		err := k8sClient.Get(ctx, names.GenHlSvcName(vdb), svc)
		if err == nil {
			Expect(svc.ObjectMeta.OwnerReferences).To(ContainElement(expOwnerRef))
			Expect(k8sClient.Delete(ctx, svc)).Should(Succeed())
		} else {
			Expect(errors.IsNotFound(err)).Should(BeTrue())
		}
	}

	Context("When reconciling a VerticaDB CRD", func() {
		It("Should create service objects", func() {
			vdb := vapi.MakeVDB()
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			By("Checking the VerticaDB has an external service object")
			extNameLookup := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			foundSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, extNameLookup, foundSvc)).Should(Succeed())
			Expect(foundSvc.Spec.ClusterIP).ShouldNot(Equal("None"))
			Expect(foundSvc.Spec.Type).Should(Equal(corev1.ServiceTypeClusterIP))
			Expect(foundSvc.Spec.Ports[0].Port).Should(Equal(int32(5433)))
			Expect(foundSvc.Spec.Ports[1].Port).Should(Equal(int32(8443)))

			By("Checking the VerticaDB has a headless service object")
			hlNameLookup := names.GenHlSvcName(vdb)
			foundSvc = &corev1.Service{}
			Expect(k8sClient.Get(ctx, hlNameLookup, foundSvc)).Should(Succeed())
			Expect(foundSvc.Spec.ClusterIP).Should(Equal("None"))
			Expect(foundSvc.Spec.Type).Should(Equal(corev1.ServiceTypeClusterIP))
			Expect(foundSvc.Spec.Ports[0].Port).Should(Equal(int32(builder.VerticaClusterCommPort)))
		})

		It("should have custom type, nodePort, externalIPs, loadBalancerIP, serviceAnnotations and update them in ext service", func() {
			vdb := vapi.MakeVDB()
			desiredType := corev1.ServiceTypeNodePort
			desiredNodePort := int32(30046)
			desiredExternalIPs := []string{"80.10.11.12"}
			desiredLoadBalancerIP := "80.20.21.22"
			desiredServiceAnnotations := map[string]string{"foo": "bar", "dib": "dab"}
			vdb.Spec.Subclusters[0].ServiceType = desiredType
			vdb.Spec.Subclusters[0].ClientNodePort = desiredNodePort
			vdb.Spec.Subclusters[0].ExternalIPs = desiredExternalIPs
			vdb.Spec.Subclusters[0].LoadBalancerIP = desiredLoadBalancerIP
			vdb.Spec.Subclusters[0].ServiceAnnotations = desiredServiceAnnotations

			createCrd(vdb, true)
			defer deleteCrd(vdb)

			extNameLookup := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			foundSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, extNameLookup, foundSvc)).Should(Succeed())
			Expect(foundSvc.Spec.Type).Should(Equal(desiredType))
			Expect(foundSvc.Spec.Ports[0].NodePort).Should(Equal(desiredNodePort))
			Expect(foundSvc.Spec.ExternalIPs).Should(Equal(desiredExternalIPs))
			Expect(foundSvc.Spec.LoadBalancerIP).Should(Equal(desiredLoadBalancerIP))
			Expect(foundSvc.ObjectMeta.Annotations["foo"]).Should(Equal(desiredServiceAnnotations["foo"]))
			Expect(foundSvc.ObjectMeta.Annotations["dib"]).Should(Equal(desiredServiceAnnotations["dib"]))

			// Update crd
			newType := corev1.ServiceTypeLoadBalancer
			newNodePort := int32(30047)
			newExternalIPs := []string{"80.10.11.10"}
			newLoadBalancerIP := "80.20.21.20"
			newServiceAnnotations := map[string]string{"foo": "bar", "dib": "baz"}
			vdb.Spec.Subclusters[0].ServiceType = newType
			vdb.Spec.Subclusters[0].ClientNodePort = newNodePort
			vdb.Spec.Subclusters[0].ExternalIPs = newExternalIPs
			vdb.Spec.Subclusters[0].LoadBalancerIP = newLoadBalancerIP
			vdb.Spec.Subclusters[0].ServiceAnnotations = newServiceAnnotations
			Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

			// Refresh any dependent objects
			pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
			objr := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, ObjReconcileModeAll)
			_, err := objr.Reconcile(ctx, &ctrl.Request{})
			Expect(err).Should(Succeed())

			Expect(k8sClient.Get(ctx, extNameLookup, foundSvc)).Should(Succeed())
			Expect(foundSvc.Spec.Type).Should(Equal(newType))
			Expect(foundSvc.Spec.Ports[0].NodePort).Should(Equal(newNodePort))
			Expect(foundSvc.Spec.ExternalIPs).Should(Equal(newExternalIPs))
			Expect(foundSvc.Spec.LoadBalancerIP).Should(Equal(newLoadBalancerIP))
			Expect(foundSvc.ObjectMeta.Annotations["foo"]).Should(Equal(newServiceAnnotations["foo"]))
			Expect(foundSvc.ObjectMeta.Annotations["dib"]).Should(Equal(newServiceAnnotations["dib"]))
		})

		It("should have custom labels and annotations in service objects and statefulsets", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Labels["my-label"] = "r1"
			vdb.Spec.Labels["vertica.com/second-label"] = "r2"
			vdb.Spec.Annotations["gitRef"] = "1234abc"

			verifyLabelsAnnotations := func(objectMeta *metav1.ObjectMeta, isSts bool) {
				Expect(objectMeta.Labels["my-label"]).Should(Equal("r1"))
				Expect(objectMeta.Labels["vertica.com/second-label"]).Should(Equal("r2"))
				Expect(objectMeta.Annotations["gitRef"]).Should(Equal("1234abc"))
				Expect(objectMeta.Labels["vertica.com/database"]).Should(Equal(vdb.Spec.DBName))
				if isSts {
					Expect(objectMeta.Labels[vmeta.SubclusterNameLabel]).Should(Equal(vdb.Spec.Subclusters[0].Name))
				}
			}

			createCrd(vdb, true)
			defer deleteCrd(vdb)

			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
			verifyLabelsAnnotations(&svc.ObjectMeta, false /* not a sts */)
			Expect(k8sClient.Get(ctx, names.GenHlSvcName(vdb), svc)).Should(Succeed())
			verifyLabelsAnnotations(&svc.ObjectMeta, false /* not subcluster specific */)

			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
			verifyLabelsAnnotations(&sts.ObjectMeta, true /* is a sts */)
		})

		It("should update version in svc objects", func() {
			vdb := vapi.MakeVDB()
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
			svc.Labels[vmeta.OperatorVersionLabel] = vmeta.OperatorVersion100
			Expect(k8sClient.Update(ctx, svc)).Should(Succeed())
			Expect(k8sClient.Get(ctx, names.GenHlSvcName(vdb), svc)).Should(Succeed())
			svc.Labels[vmeta.OperatorVersionLabel] = vmeta.OperatorVersion100
			Expect(k8sClient.Update(ctx, svc)).Should(Succeed())

			pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
			objr := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, ObjReconcileModeAll)
			Expect(objr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

			Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
			Expect(svc.Labels[vmeta.OperatorVersionLabel]).Should(Equal(vmeta.CurOperatorVersion))
			Expect(k8sClient.Get(ctx, names.GenHlSvcName(vdb), svc)).Should(Succeed())
			Expect(svc.Labels[vmeta.OperatorVersionLabel]).Should(Equal(vmeta.CurOperatorVersion))
		})

		It("should create a statefulset with the configured size", func() {
			vdb := vapi.MakeVDB()
			var desiredSize int32 = 16
			vdb.Spec.Subclusters[0].Size = desiredSize

			createCrd(vdb, true)
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

			createCrd(vdb, true)
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

			createCrd(vdb, true)
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

			createCrd(vdb, true)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			currNodeSelector := sts.Spec.Template.Spec.NodeSelector
			Expect(currNodeSelector).Should(Equal(desiredNodeSelector))
		})

		It("should create a statefulset with a configured Affinity", func() {
			vdb := vapi.MakeVDB()
			desiredAffinity := vapi.Affinity{
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

			createCrd(vdb, true)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			currAffinity := sts.Spec.Template.Spec.Affinity
			Expect(*currAffinity).Should(Equal(*builder.GetK8sAffinity(desiredAffinity)))
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

			createCrd(vdb, true)
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

			createCrd(vdb, true)
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

			createCrd(vdb, true)
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
			createCrd(vdb, true)
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
			pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
			objr := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, ObjReconcileModeAll)
			_, err := objr.Reconcile(ctx, &ctrl.Request{})
			Expect(err).Should(Succeed())

			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			curSize = *sts.Spec.Replicas
			Expect(curSize).Should(Equal(newSize))
		})

		It("should have updateStrategy OnDelete for kSafety 0", func() {
			vdb := vapi.MakeVDB()
			vdb.Annotations[vmeta.KSafetyAnnotation] = "0"
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			updateStrategyHelper(ctx, vdb, appsv1.OnDeleteStatefulSetStrategyType)
		})

		It("should have updateStrategy RollingUpdate for kSafety 1", func() {
			vdb := vapi.MakeVDB()
			vdb.Annotations[vmeta.KSafetyAnnotation] = "1"
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			updateStrategyHelper(ctx, vdb, appsv1.RollingUpdateStatefulSetStrategyType)
		})

		It("should allow a custom sidecar for logging", func() {
			vdb := vapi.MakeVDB()
			cpuResource := resource.MustParse("100")
			memResource := resource.MustParse("64Mi")
			vloggerImg := "custom-vlogger:latest"
			pullPolicy := corev1.PullNever
			vdb.Spec.Sidecars = append(vdb.Spec.Sidecars, corev1.Container{
				Name:            "vlogger",
				Image:           vloggerImg,
				ImagePullPolicy: pullPolicy,
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{"cpu": cpuResource, "memory": memResource},
					Requests: corev1.ResourceList{"cpu": cpuResource, "memory": memResource},
				},
			})
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(len(sts.Spec.Template.Spec.Containers)).Should(Equal(2))
			Expect(sts.Spec.Template.Spec.Containers[1].Image).Should(Equal(vloggerImg))
			Expect(sts.Spec.Template.Spec.Containers[1].ImagePullPolicy).Should(Equal(pullPolicy))
			Expect(sts.Spec.Template.Spec.Containers[1].Resources.Limits["cpu"]).Should(Equal(cpuResource))
			Expect(sts.Spec.Template.Spec.Containers[1].Resources.Requests["memory"]).Should(Equal(memResource))
		})

		It("should include imagePullSecrets if specified in the vdb", func() {
			vdb := vapi.MakeVDB()
			const PullSecretName = "docker-info"
			vdb.Spec.ImagePullSecrets = append(vdb.Spec.ImagePullSecrets, vapi.LocalObjectReference{Name: PullSecretName})
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(len(sts.Spec.Template.Spec.ImagePullSecrets)).Should(Equal(1))
			imagePullSecrets := builder.GetK8sLocalObjectReferenceArray(vdb.Spec.ImagePullSecrets)
			Expect(sts.Spec.Template.Spec.ImagePullSecrets).Should(ContainElement(imagePullSecrets[0]))
		})

		It("should requeue if the license secret is not found", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.LicenseSecret = "not-here-1"
			createCrd(vdb, false)
			defer deleteCrd(vdb)

			pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
			objr := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, ObjReconcileModeAll)
			Expect(objr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		})

		It("should requeue if the kerberos secret is not found", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.KerberosSecret = "not-here-2"
			createCrd(vdb, false)
			defer deleteCrd(vdb)

			pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
			objr := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, ObjReconcileModeAll)
			Expect(objr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		})

		It("should requeue if the hadoop conf is not found", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.HadoopConfig = "not-here-3"
			createCrd(vdb, false)
			defer deleteCrd(vdb)

			pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
			objr := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, ObjReconcileModeAll)
			Expect(objr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		})

		It("should succeed if the kerberos secret is setup correctly", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.KerberosSecret = "my-secret-v1"
			secret := builder.BuildKerberosSecretBase(vdb)
			secret.Data[filepath.Base(paths.Krb5Keytab)] = []byte("keytab")
			secret.Data[filepath.Base(paths.Krb5Conf)] = []byte("conf")
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			defer deleteSecret(ctx, vdb, vdb.Spec.KerberosSecret)
			createCrd(vdb, false)
			defer deleteCrd(vdb)

			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)
		})

		It("should requeue if the kerberos secret has a missing keytab", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.KerberosSecret = "my-secret-v2"
			secret := builder.BuildKerberosSecretBase(vdb)
			secret.Data[filepath.Base(paths.Krb5Conf)] = []byte("conf") // Only the krb5.conf
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			defer deleteSecret(ctx, vdb, vdb.Spec.KerberosSecret)
			createCrd(vdb, false)
			defer deleteCrd(vdb)

			runReconciler(vdb, ctrl.Result{Requeue: true}, ObjReconcileModeAll)
		})

		It("should requeue if the ssh secret has a missing keys", func() {
			vdb := vapi.MakeVDB()
			vdb.Annotations[vmeta.SSHSecAnnotation] = "my-secret-v3"
			nm := names.GenNamespacedName(vdb, vdb.GetSSHSecretName())
			secret := builder.BuildSecretBase(nm)
			secret.Data[paths.SSHKeyPaths[0]] = []byte("conf") // Only 1 of the keys
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			defer deleteSecret(ctx, vdb, vdb.GetSSHSecretName())
			createCrd(vdb, false)
			defer deleteCrd(vdb)

			runReconciler(vdb, ctrl.Result{Requeue: true}, ObjReconcileModeAll)
		})

		It("should not proceed with the scale down if uninstall or db_remove_node hasn't happened", func() {
			vdb := vapi.MakeVDB()
			origSize := int32(4)
			sc := &vdb.Spec.Subclusters[0]
			sc.Size = origSize
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			newSize := int32(3)
			vdb.Spec.Subclusters[0].Size = newSize
			Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

			nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())

			pn := names.GenPodNameFromSts(vdb, sts, origSize-1)
			pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
			Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
			objr := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, ObjReconcileModeAll)

			pDetail := &podfacts.PodFact{}
			pDetail.SetIsInstalled(true)
			pDetail.SetDBExists(false)
			pfacts.Detail[pn] = pDetail
			Expect(objr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

			pDetail.SetIsInstalled(false)
			pDetail.SetDBExists(true)
			pfacts.Detail[pn] = pDetail
			Expect(objr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

			pDetail.SetIsInstalled(false)
			pDetail.SetDBExists(false)
			pfacts.Detail[pn] = pDetail
			Expect(objr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			curSize := *sts.Spec.Replicas
			Expect(curSize).Should(Equal(newSize))
		})

		It("should update service object if labels are changing", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{}
			sc := &vdb.Spec.Subclusters[0]
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			nm := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			svc1 := &corev1.Service{}
			Expect(k8sClient.Get(ctx, nm, svc1)).Should(Succeed())

			standby := vdb.BuildTransientSubcluster("")
			pfacts := podfacts.MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
			actor := MakeObjReconciler(vdbRec, logger, vdb, &pfacts, ObjReconcileModeAll)
			objr := actor.(*ObjReconciler)
			// Force a label change to reconcile with the transient subcluster
			svcName := names.GenExtSvcName(vdb, sc)
			expSvc := builder.BuildExtSvc(svcName, vdb, sc, builder.MakeSvcSelectorLabelsForSubclusterNameRouting)
			Expect(objr.reconcileExtSvc(ctx, expSvc, standby)).Should(Succeed())

			// Fetch the service object again.  The selectors should be different.
			svc2 := &corev1.Service{}
			Expect(k8sClient.Get(ctx, nm, svc2)).Should(Succeed())
			Expect(reflect.DeepEqual(svc1.Spec.Selector, svc2.Spec.Selector)).Should(BeFalse())
		})

		It("should create new objects and not update scale of existing if ObjReconcileModePreserveScaling is used", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Subclusters = []vapi.Subcluster{
				{Name: "sc1", Size: 1},
				{Name: "sc2", Size: 1},
			}
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			// Delete a statefulset and make a change that should cause a change
			// in the other statefulset.  If we run with ObjReconcileModeNoScaling
			// we won't make the second change.  We'll only recreate the first sts.
			sc1 := &vdb.Spec.Subclusters[0]
			sc1StsName := names.GenStsName(vdb, sc1)
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, sc1StsName, sts)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, sts)).Should(Succeed())
			sc2 := &vdb.Spec.Subclusters[1]
			sc2.Size = 2

			runReconciler(vdb, ctrl.Result{}, ObjReconcileModePreserveScaling)

			Expect(k8sClient.Get(ctx, sc1StsName, sts)).Should(Succeed())
			sc2StsName := names.GenStsName(vdb, sc2)
			Expect(k8sClient.Get(ctx, sc2StsName, sts)).Should(Succeed())
			Expect(*sts.Spec.Replicas).Should(Equal(int32(1)))
		})

		It("should requeue if vclusterops is enabled but HTTP secret isn't setup properly", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.NMATLSSecret = ""
			vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
			createCrd(vdb, false)
			defer deleteCrd(vdb)

			runReconciler(vdb, ctrl.Result{Requeue: true}, ObjReconcileModeAll)

			// Having a secret name, but not created should force a requeue too
			vdb.Spec.NMATLSSecret = "dummy1"
			runReconciler(vdb, ctrl.Result{Requeue: true}, ObjReconcileModeAll)
		})

		It("should not change size of sts if ObjReconcileModePreserveScaling is used", func() {
			vdb := vapi.MakeVDB()
			origSize := vdb.Spec.Subclusters[0].Size
			createCrd(vdb, false)
			defer deleteCrd(vdb)

			sc := &vdb.Spec.Subclusters[0]

			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)

			// Having a secret name, but not created should force a requeue too
			vdb.Spec.Subclusters[0].Size++
			runReconciler(vdb, ctrl.Result{}, ObjReconcileModePreserveScaling)

			nm := names.GenStsName(vdb, sc)
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(*sts.Spec.Replicas).Should(Equal(origSize))
		})

		It("should not change delete policy of sts if ObjReconcileModeUpdateStrategy is used", func() {
			vdb := vapi.MakeVDB()
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			// Change updateStrategy in sts so that we know the objReconciler
			// won't change it back.
			sc := &vdb.Spec.Subclusters[0]
			nm := names.GenStsName(vdb, sc)
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(sts.Spec.UpdateStrategy.Type).Should(Equal(appsv1.RollingUpdateStatefulSetStrategyType))
			sts.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
			Expect(k8sClient.Update(ctx, sts)).Should(Succeed())

			runReconciler(vdb, ctrl.Result{}, ObjReconcileModePreserveUpdateStrategy)
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(sts.Spec.UpdateStrategy.Type).Should(Equal(appsv1.OnDeleteStatefulSetStrategyType))

			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(sts.Spec.UpdateStrategy.Type).Should(Equal(appsv1.RollingUpdateStatefulSetStrategyType))
		})

		It("should not change generated node port's if service object changes", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Subclusters[0].ServiceType = corev1.ServiceTypeNodePort
			vdb.Spec.Subclusters[0].ClientNodePort = 0      // k8s to generate one
			vdb.Spec.Subclusters[0].VerticaHTTPNodePort = 0 // k8s to generate one
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			nm := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, nm, svc)).Should(Succeed())
			origVerticaNodePort := svc.Spec.Ports[0].NodePort
			origHTTPPort := svc.Spec.Ports[1].NodePort
			Expect(origVerticaNodePort).Should(BeNumerically(">", 0))
			Expect(origHTTPPort).Should(BeNumerically(">", 0))

			// Update the vdb such that a reconciler will need to update the service object
			const NewAnnotationKey = "lb"
			const NewAnnotationVal = "enable"
			vdb.Spec.Subclusters[0].ServiceAnnotations = map[string]string{NewAnnotationKey: NewAnnotationVal}
			Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())
			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)

			Expect(k8sClient.Get(ctx, nm, svc)).Should(Succeed())
			// Ensure we actually update the service object
			Expect(svc.ObjectMeta.Annotations[NewAnnotationKey]).Should(Equal(NewAnnotationVal))
			// Node ports stayed the same
			Expect(svc.Spec.Ports[0].NodePort).Should(Equal(origVerticaNodePort))
			Expect(svc.Spec.Ports[1].NodePort).Should(Equal(origHTTPPort))
		})

		It("should preserve user specified HTTP node port when service object changes", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Subclusters[0].ServiceType = corev1.ServiceTypeNodePort
			vdb.Spec.Subclusters[0].ClientNodePort = 0 // k8s to generate one
			const HTTPNodePort int32 = 30000
			vdb.Spec.Subclusters[0].VerticaHTTPNodePort = HTTPNodePort
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			nm := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, nm, svc)).Should(Succeed())
			Expect(svc.Spec.Ports[1].NodePort).Should(Equal(HTTPNodePort))

			const VerticaNodePort int32 = 30001
			vdb.Spec.Subclusters[0].ClientNodePort = VerticaNodePort
			Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())
			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)

			Expect(k8sClient.Get(ctx, nm, svc)).Should(Succeed())
			Expect(svc.Spec.Ports[0].NodePort).Should(Equal(VerticaNodePort))
			Expect(svc.Spec.Ports[1].NodePort).Should(Equal(HTTPNodePort))
		})

		It("should have consistent labels if multiple subclusters share a service object", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Subclusters = []vapi.Subcluster{
				{Name: "sc1", Size: 1, ServiceName: "conn"},
				{Name: "sc2", Size: 1, ServiceName: "conn"},
			}
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			nm1 := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			nm2 := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[1])
			Expect(nm1).Should(Equal(nm2))
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, nm1, svc)).Should(Succeed())
			Expect(svc.Labels[vmeta.SubclusterNameLabel]).Should(Equal("sc1"))
		})

		It("should set ownerReferencese in PVC", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Subclusters[0].Size = 1
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			sc := &vdb.Spec.Subclusters[0]
			nm := names.GenStsName(vdb, sc)
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(len(sts.Spec.VolumeClaimTemplates)).Should(Equal(1))
			Expect(len(sts.Spec.VolumeClaimTemplates[0].OwnerReferences)).Should(Equal(1))
			Expect(sts.Spec.VolumeClaimTemplates[0].OwnerReferences[0].APIVersion).Should(Equal(vapi.GroupVersion.String()))
			Expect(sts.Spec.VolumeClaimTemplates[0].OwnerReferences[0].Kind).Should(Equal(vapi.VerticaDBKind))
			Expect(sts.Spec.VolumeClaimTemplates[0].OwnerReferences[0].Name).Should(Equal(vdb.Name))
			Expect(sts.Spec.VolumeClaimTemplates[0].OwnerReferences[0].UID).Should(Equal(vdb.UID))
		})

		It("should recreate sts if NMA deployment type is changing", func() {
			vdb := vapi.MakeVDB()
			vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
			vdb.Annotations[vmeta.VersionAnnotation] = vapi.VcluseropsAsDefaultDeploymentMethodMinVersion
			vdb.Spec.NMATLSSecret = "tls-abcdef"
			test.CreateFakeTLSSecret(ctx, vdb, k8sClient, vdb.Spec.NMATLSSecret)
			defer test.DeleteSecret(ctx, k8sClient, vdb.Spec.NMATLSSecret)
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			// To know if the sts was recreated, we are going to modify the sts
			// by adding an annotation. This annotation will be missing after
			// the reconcile, which is proof that it was recreated.
			sc := &vdb.Spec.Subclusters[0]
			nm := names.GenStsName(vdb, sc)
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(sts.Spec.Template.Spec.Containers).Should(HaveLen(1))
			const testAnnotationName = "added-by-test"
			const testAnnotationVal = "ut"
			sts.Annotations = map[string]string{
				testAnnotationName: testAnnotationVal,
			}
			Expect(k8sClient.Update(ctx, sts)).Should(Succeed())

			// Update the vdb so that the reconciler will recreate the sts.
			Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
			vdb.Annotations[vmeta.VersionAnnotation] = vapi.NMAInSideCarDeploymentMinVersion
			Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)
			Expect(k8sClient.Get(ctx, nm, sts)).Should(Succeed())
			Expect(sts.Annotations[testAnnotationName]).ShouldNot(Equal(testAnnotationVal))
			Expect(sts.Spec.Template.Spec.Containers).Should(HaveLen(2))
		})

		It("should not remove manual annotations to service objects", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Subclusters[0].Size = 1
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			svc := &corev1.Service{}
			svcNm := names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, svcNm, svc)).Should(Succeed())
			const manualAnnotationKey = "my-manual-annotation"
			const manualAnnotationVal = "true"
			svc.Annotations[manualAnnotationKey] = manualAnnotationVal
			Expect(k8sClient.Update(ctx, svc)).Should(Succeed())

			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)

			Expect(k8sClient.Get(ctx, svcNm, svc)).Should(Succeed())
			val, ok := svc.Annotations[manualAnnotationKey]
			Expect(ok).Should(BeTrue())
			Expect(val).Should(Equal(manualAnnotationVal))
		})

		It("should add annotations to statefulsets", func() {
			vdb := vapi.MakeVDB()
			vdb.Spec.Subclusters[0].Annotations = map[string]string{
				"sts": "0",
			}
			createCrd(vdb, true)
			defer deleteCrd(vdb)

			sts := &appsv1.StatefulSet{}
			stsNm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
			Expect(k8sClient.Get(ctx, stsNm, sts)).Should(Succeed())
			Expect(sts.Annotations).Should(HaveKeyWithValue("sts", "0"))

			vdb.Spec.Subclusters[0].Annotations = map[string]string{
				"stream": "false",
			}
			Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

			runReconciler(vdb, ctrl.Result{}, ObjReconcileModeAll)

			Expect(k8sClient.Get(ctx, stsNm, sts)).Should(Succeed())
			Expect(sts.Annotations).ShouldNot(HaveKeyWithValue("sts", "0"))
			Expect(sts.Annotations).Should(HaveKeyWithValue("stream", "false"))
		})
	})
})

func updateStrategyHelper(ctx context.Context, vdb *vapi.VerticaDB, expectedUpdateStrategy appsv1.StatefulSetUpdateStrategyType) {
	sts := &appsv1.StatefulSet{}
	nm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
	ExpectWithOffset(1, k8sClient.Get(ctx, nm, sts)).Should(Succeed())
	ExpectWithOffset(1, sts.Spec.UpdateStrategy.Type).Should(Equal(expectedUpdateStrategy))
}
