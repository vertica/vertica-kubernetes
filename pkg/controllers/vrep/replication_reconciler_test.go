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

package vrep

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	vdbcontroller "github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/mockvops"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstart"
	vrepstatus "github.com/vertica/vertica-kubernetes/pkg/vrepstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const minimumVer = "v24.3.0"

var _ = Describe("query_reconcile", func() {
	ctx := context.Background()

	It("should update replication status conditions and states if the vclusterops API succeeded", func() {
		sourceVdbName := v1beta1.MakeSourceVDBName()
		sourceVdb := vapi.MakeVDB()
		sourceVdb.Name = sourceVdbName.Name
		sourceVdb.Namespace = sourceVdbName.Namespace
		sourceVdb.Annotations[vmeta.VersionAnnotation] = minimumVer
		sourceVdb.Spec.NMATLSSecret = testTLSSecretName
		setupAPIFunc := func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger) {
			return &mockvops.MockVClusterOps{}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(sourceVdb, setupAPIFunc)
		test.CreateVDB(ctx, k8sClient, sourceVdb)
		defer test.DeleteVDB(ctx, k8sClient, sourceVdb)
		test.CreatePods(ctx, k8sClient, sourceVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, sourceVdb)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, k8sClient, testTLSSecretName)
		defer test.DeleteSecret(ctx, k8sClient, testTLSSecretName)

		targetVdbName := v1beta1.MakeTargetVDBName()
		targetVdb := vapi.MakeVDB()
		targetVdb.Name = targetVdbName.Name
		targetVdb.Namespace = targetVdbName.Namespace
		targetVdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		targetVdb.Annotations[vmeta.VersionAnnotation] = minimumVer
		targetVdb.UID = testTargetVdbUID
		test.CreateVDB(ctx, k8sClient, targetVdb)
		defer test.DeleteVDB(ctx, k8sClient, targetVdb)
		test.CreatePods(ctx, k8sClient, targetVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, targetVdb)

		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		r := &ReplicationReconciler{
			VRec: vrepRec,
			Vrep: vrep,
			Log:  logger,
		}
		err := r.runReplicateDB(ctx, dispatcher, []replicationstart.Option{})
		Expect(err).ShouldNot(HaveOccurred())
		// make sure that Replicating condition is updated to false and
		// ReplicationComplete condition is updated to true
		// state message is updated to "Replication successful"
		Expect(vrep.IsStatusConditionFalse(v1beta1.Replicating)).Should(BeTrue())
		Expect(vrep.IsStatusConditionTrue(v1beta1.ReplicationComplete)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateSucceededReplication))
	})

	It("should exit reconcile loop early if replication is complete or not ready", func() {
		sourceVdbName := v1beta1.MakeSourceVDBName()
		sourceVdb := vapi.MakeVDB()
		sourceVdb.Name = sourceVdbName.Name
		sourceVdb.Namespace = sourceVdbName.Namespace
		sourceVdb.Annotations[vmeta.VersionAnnotation] = minimumVer
		sourceVdb.Spec.NMATLSSecret = testTLSSecretName
		test.CreateVDB(ctx, k8sClient, sourceVdb)
		defer test.DeleteVDB(ctx, k8sClient, sourceVdb)
		test.CreatePods(ctx, k8sClient, sourceVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, sourceVdb)

		targetVdbName := v1beta1.MakeTargetVDBName()
		targetVdb := vapi.MakeVDB()
		targetVdb.Name = targetVdbName.Name
		targetVdb.Namespace = targetVdbName.Namespace
		targetVdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		targetVdb.Annotations[vmeta.VersionAnnotation] = minimumVer
		targetVdb.UID = testTargetVdbUID
		test.CreateVDB(ctx, k8sClient, targetVdb)
		defer test.DeleteVDB(ctx, k8sClient, targetVdb)
		test.CreatePods(ctx, k8sClient, targetVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, targetVdb)

		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		recon := MakeReplicationReconciler(vrepRec, vrep, logger)
		err := vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationComplete,
				metav1.ConditionTrue, "Succeeded")}, stateSucceededReplication)
		Expect(err).ShouldNot(HaveOccurred())
		result, err := recon.Reconcile(ctx, &ctrl.Request{})

		expected := &ReplicationReconciler{
			VRec:       vrepRec,
			Vrep:       vrep,
			Log:        logger.WithName("ReplicationReconciler"),
			SourceInfo: &ReplicationInfo{},
			TargetInfo: &ReplicationInfo{},
		}
		original, ok := recon.(*ReplicationReconciler)
		Expect(ok).Should(BeTrue())
		// make sure ReplicationReconciler fields are untouched
		Expect(reflect.DeepEqual(expected, original)).Should(BeTrue())

		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).ShouldNot(HaveOccurred())
		// make sure status conditions and state are retained
		Expect(vrep.IsStatusConditionTrue(v1beta1.ReplicationComplete)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateSucceededReplication))

		err = vrepstatus.Reset(ctx, vrepRec.Client, vrepRec.Log, vrep)
		Expect(err).ShouldNot(HaveOccurred())
		// make sure status conditions and state are cleared
		Expect(vrep.IsStatusConditionPresent(v1beta1.ReplicationComplete)).Should(BeFalse())
		Expect(vrep.Status.State).Should(Equal(""))

		err = vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationReady,
				metav1.ConditionFalse, "IncompatibleSourceDB")}, stateIncompatibleDB)
		Expect(err).ShouldNot(HaveOccurred())
		result, err = recon.Reconcile(ctx, &ctrl.Request{})

		original, ok = recon.(*ReplicationReconciler)
		Expect(ok).Should(BeTrue())
		// make sure ReplicationReconciler fields are untouched
		Expect(reflect.DeepEqual(expected, original)).Should(BeTrue())
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).ShouldNot(HaveOccurred())
		// make sure status conditions and state are retained
		Expect(vrep.IsStatusConditionFalse(v1beta1.ReplicationReady)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateIncompatibleDB))
	})

	It("should set correct username and password", func() {
		sourceVdbName := v1beta1.MakeSourceVDBName()
		sourceVdb := vapi.MakeVDB()
		sourceVdb.Name = sourceVdbName.Name
		sourceVdb.Namespace = sourceVdbName.Namespace
		sourceVdb.Annotations[vmeta.VersionAnnotation] = minimumVer
		sourceVdb.Spec.NMATLSSecret = testTLSSecretName
		test.CreateVDB(ctx, k8sClient, sourceVdb)
		defer test.DeleteVDB(ctx, k8sClient, sourceVdb)
		test.CreatePods(ctx, k8sClient, sourceVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, sourceVdb)

		targetVdbName := v1beta1.MakeTargetVDBName()
		targetVdb := vapi.MakeVDB()
		targetVdb.Name = targetVdbName.Name
		targetVdb.Namespace = targetVdbName.Namespace
		targetVdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		targetVdb.Annotations[vmeta.VersionAnnotation] = minimumVer
		targetVdb.UID = testTargetVdbUID
		test.CreateVDB(ctx, k8sClient, targetVdb)
		defer test.DeleteVDB(ctx, k8sClient, targetVdb)
		test.CreatePods(ctx, k8sClient, targetVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, targetVdb)

		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		// create custom superuser password secret for source vdb
		test.CreateSuperuserPasswordSecret(ctx, sourceVdb, k8sClient, testCustomPasswordSecretName, testPassword)
		defer deleteSecret(ctx, sourceVdb, testCustomPasswordSecretName)

		// no username provided
		username, password, err := setUsernameAndPassword(ctx, k8sClient, logger, vrepRec, sourceVdb, &vrep.Spec.Source)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(username).Should(Equal(vapi.SuperUser))
		Expect(password).Should(Equal(""))

		vrep.Spec.Source.UserName = testCustomUserName
		Expect(k8sClient.Update(ctx, vrep)).Should(Succeed())

		// username provided, password secret not provided
		username, password, err = setUsernameAndPassword(ctx, k8sClient, logger, vrepRec, sourceVdb, &vrep.Spec.Source)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(username).Should(Equal(testCustomUserName))
		Expect(password).Should(Equal(""))

		vrep.Spec.Source.PasswordSecret = testCustomPasswordSecretName
		Expect(k8sClient.Update(ctx, vrep)).Should(Succeed())

		// username and password secret provided
		username, password, err = setUsernameAndPassword(ctx, k8sClient, logger, vrepRec, sourceVdb, &vrep.Spec.Source)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(username).Should(Equal(testCustomUserName))
		Expect(password).Should(Equal(testPassword))

	})

	It("should return a reasonable error message if the sandbox has no nodes", func() {
		sourcePodfacts := vdbcontroller.PodFacts{SandboxName: "dne"}
		targetPodfacts := vdbcontroller.PodFacts{SandboxName: "dne"}
		targetPodfacts.Detail = make(vdbcontroller.PodFactDetail)
		targetPodfacts.Detail[types.NamespacedName{}] = &vdbcontroller.PodFact{}
		r := &ReplicationReconciler{
			SourcePFacts: &sourcePodfacts,
			TargetPFacts: &targetPodfacts,
		}
		err := r.checkSandboxExists()
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).Should(Equal("source sandbox 'dne' does not exist or has no nodes assigned to it"))
		sourcePodfacts.Detail = targetPodfacts.Detail
		targetPodfacts.Detail = make(vdbcontroller.PodFactDetail)
		err = r.checkSandboxExists()
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).Should(Equal("target sandbox 'dne' does not exist or has no nodes assigned to it"))
		// if both have nodes, even with sandboxes, shouldn't be an error
		targetPodfacts.Detail = sourcePodfacts.Detail
		err = r.checkSandboxExists()
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("should correctly chose a hostname based on service addresses", func() {
		sourceVdbName := v1beta1.MakeSourceVDBName()
		sourceVdb := vapi.MakeVDB()
		sourceVdb.Name = sourceVdbName.Name
		sourceVdb.Namespace = sourceVdbName.Namespace
		sourceVdb.Annotations[vmeta.VersionAnnotation] = minimumVer
		sourceVdb.Spec.NMATLSSecret = testTLSSecretName
		test.CreateVDB(ctx, k8sClient, sourceVdb)
		defer test.DeleteVDB(ctx, k8sClient, sourceVdb)
		test.CreateSvcs(ctx, k8sClient, sourceVdb)
		defer test.DeleteSvcs(ctx, k8sClient, sourceVdb)
		test.CreatePods(ctx, k8sClient, sourceVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, sourceVdb)
		sourceSvcName := names.GenExtSvcName(sourceVdb, &sourceVdb.Spec.Subclusters[0])
		sourceSvcHostname := fmt.Sprintf("%s.%s.svc.cluster.local", sourceSvcName.Name, sourceSvcName.Namespace)

		targetVdbName := v1beta1.MakeTargetVDBName()
		targetSandboxName := "sb"
		targetSandboxScName := "sb-sc"
		targetVdb := vapi.MakeVDB()
		targetVdb.Name = targetVdbName.Name
		targetVdb.Namespace = targetVdbName.Namespace
		targetVdb.Annotations[vmeta.VersionAnnotation] = minimumVer
		targetVdb.UID = testTargetVdbUID
		targetVdb.Spec.Subclusters = append(targetVdb.Spec.Subclusters, vapi.Subcluster{Name: targetSandboxScName, Size: 1,
			ServiceType: corev1.ServiceTypeClusterIP, Type: vapi.SecondarySubcluster})
		targetVdb.Spec.Sandboxes = []vapi.Sandbox{{Name: targetSandboxName, Subclusters: []vapi.SubclusterName{{Name: targetSandboxScName}}}}
		test.CreateVDB(ctx, k8sClient, targetVdb)
		defer test.DeleteVDB(ctx, k8sClient, targetVdb)
		test.CreateSvcs(ctx, k8sClient, targetVdb)
		defer test.DeleteSvcs(ctx, k8sClient, targetVdb)
		test.CreatePods(ctx, k8sClient, targetVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, targetVdb)
		targetSvcName := names.GenExtSvcName(targetVdb, &targetVdb.Spec.Subclusters[0])
		targetSvcHostname := fmt.Sprintf("%s.%s.svc.cluster.local", targetSvcName.Name, targetSvcName.Namespace)

		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		r := &ReplicationReconciler{
			VRec:       vrepRec,
			Vrep:       vrep,
			Log:        logger.WithName("ReplicationReconciler"),
			SourceInfo: &ReplicationInfo{Vdb: sourceVdb},
			TargetInfo: &ReplicationInfo{Vdb: targetVdb},
		}
		err := r.determineSourceAndTargetHosts(ctx)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(r.SourceInfo.Hostname).Should(Equal(sourceSvcHostname))
		Expect(r.TargetInfo.Hostname).Should(Equal(targetSvcHostname))

		// specifying a service should be fine
		r.Vrep.Spec.Source.ServiceName = sourceVdb.Spec.Subclusters[0].Name
		err = r.determineSourceAndTargetHosts(ctx)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(r.SourceInfo.Hostname).Should(Equal(sourceSvcHostname))
		Expect(r.TargetInfo.Hostname).Should(Equal(targetSvcHostname))

		// should validate service is part of cluster
		r.Vrep.Spec.Source.ServiceName = targetSandboxScName
		err = r.determineSourceAndTargetHosts(ctx)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).Should(ContainSubstring("could not find service"))
		Expect(err.Error()).Should(ContainSubstring(r.Vrep.Spec.Source.ServiceName))
		Expect(err.Error()).Should(ContainSubstring("the main cluster"))
	})
})
