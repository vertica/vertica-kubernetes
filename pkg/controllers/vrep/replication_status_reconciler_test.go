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
	"reflect"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstatus"
	vrepstatus "github.com/vertica/vertica-kubernetes/pkg/vrepstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/vertica/vertica-kubernetes/pkg/test"
)

var _ = Describe("query_reconcile", func() {
	ctx := context.Background()

	It("should update replication status conditions and states if the vclusterops API succeeded", func() {
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
		targetVdb.Spec.NMATLSSecret = testTargetTLSSecretName
		targetVdb.UID = testTargetVdbUID

		test.CreateVDB(ctx, k8sClient, targetVdb)
		defer test.DeleteVDB(ctx, k8sClient, targetVdb)
		test.CreatePods(ctx, k8sClient, targetVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, targetVdb)

		setupAPIFunc := func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger) {
			return &mockAsyncReplicationVClusterOps{}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetupAndTarget(sourceVdb, targetVdb, setupAPIFunc)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, k8sClient, testTLSSecretName)
		defer test.DeleteSecret(ctx, k8sClient, testTLSSecretName)
		test.CreateFakeTLSSecret(ctx, dispatcher.TargetVDB, k8sClient, testTargetTLSSecretName)
		defer test.DeleteSecret(ctx, k8sClient, testTargetTLSSecretName)

		vrep := v1beta1.MakeVrep()
		vrep.Spec.Mode = v1beta1.ReplicationModeAsync
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		err := vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating,
				metav1.ConditionTrue, "Started")}, stateReplicating, testTransactionID)
		Expect(err).ShouldNot(HaveOccurred())

		r := &ReplicationStatusReconciler{
			Client: k8sClient,
			VRec:   vrepRec,
			Vrep:   vrep,
			Log:    logger,
		}
		err = r.runReplicationStatus(ctx, dispatcher, []replicationstatus.Option{})
		Expect(err).ShouldNot(HaveOccurred())
		// make sure that Replicating condition is updated to false and
		// ReplicationComplete condition is updated to true
		// state message is updated to "Replication successful"
		Expect(vrep.IsStatusConditionFalse(v1beta1.Replicating)).Should(BeTrue())
		Expect(vrep.IsStatusConditionTrue(v1beta1.ReplicationComplete)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateSucceededReplication))
	})

	It("should exit reconcile loop early if replication is complete, not ready, or doesn't have a transaction ID", func() {
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
		vrep.Spec.Mode = v1beta1.ReplicationModeAsync
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		recon := MakeReplicationStatusReconciler(k8sClient, vrepRec, vrep, logger)

		// Case 1: replication complete
		err := vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationComplete,
				metav1.ConditionTrue, "Succeeded")}, stateSucceededReplication, 0)
		Expect(err).ShouldNot(HaveOccurred())
		result, err := recon.Reconcile(ctx, &ctrl.Request{})

		expected := &ReplicationStatusReconciler{
			Client:     k8sClient,
			VRec:       vrepRec,
			Vrep:       vrep,
			Log:        logger.WithName("ReplicationStatusReconciler"),
			TargetInfo: &ReplicationInfo{},
		}
		original, ok := recon.(*ReplicationStatusReconciler)
		Expect(ok).Should(BeTrue())
		// make sure ReplicationStatusReconciler fields are untouched
		Expect(reflect.DeepEqual(expected, original)).Should(BeTrue())

		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).ShouldNot(HaveOccurred())
		// make sure status conditions and state are retained
		Expect(vrep.IsStatusConditionTrue(v1beta1.ReplicationComplete)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateSucceededReplication))

		// Reset conditions/state
		err = vrepstatus.Reset(ctx, vrepRec.Client, vrepRec.Log, vrep)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(vrep.IsStatusConditionPresent(v1beta1.ReplicationComplete)).Should(BeFalse())
		Expect(vrep.Status.State).Should(Equal(""))

		// Case 2: replication not ready
		err = vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationReady,
				metav1.ConditionFalse, "IncompatibleSourceDB")}, stateIncompatibleDB, 0)
		Expect(err).ShouldNot(HaveOccurred())
		result, err = recon.Reconcile(ctx, &ctrl.Request{})

		original, ok = recon.(*ReplicationStatusReconciler)
		Expect(ok).Should(BeTrue())
		// make sure ReplicationStatusReconciler fields are untouched
		Expect(reflect.DeepEqual(expected, original)).Should(BeTrue())

		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).ShouldNot(HaveOccurred())
		// make sure status conditions and state are retained
		Expect(vrep.IsStatusConditionFalse(v1beta1.ReplicationReady)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateIncompatibleDB))

		// Reset conditions/state
		err = vrepstatus.Reset(ctx, vrepRec.Client, vrepRec.Log, vrep)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(vrep.IsStatusConditionPresent(v1beta1.ReplicationReady)).Should(BeFalse())
		Expect(vrep.Status.State).Should(Equal(""))

		// Case 3: no transaction ID present (set to 0)
		err = vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating,
				metav1.ConditionTrue, "Started")}, stateReplicating, 0)
		Expect(err).ShouldNot(HaveOccurred())
		result, err = recon.Reconcile(ctx, &ctrl.Request{})

		original, ok = recon.(*ReplicationStatusReconciler)
		Expect(ok).Should(BeTrue())
		// make sure ReplicationReconciler fields are untouched
		Expect(reflect.DeepEqual(expected, original)).Should(BeTrue())

		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).Should(HaveOccurred())
		// make sure status conditions and state are retained
		Expect(vrep.IsStatusConditionTrue(v1beta1.Replicating)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateReplicating))
	})

	It("should exit reconcile loop early if replication mode isn't async", func() {
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
		vrep.Spec.Mode = v1beta1.ReplicationModeSync
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		recon := MakeReplicationStatusReconciler(k8sClient, vrepRec, vrep, logger)
		err := vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating,
				metav1.ConditionTrue, "Started")}, stateReplicating, 0)
		Expect(err).ShouldNot(HaveOccurred())
		result, err := recon.Reconcile(ctx, &ctrl.Request{})

		expected := &ReplicationStatusReconciler{
			Client:     k8sClient,
			VRec:       vrepRec,
			Vrep:       vrep,
			Log:        logger.WithName("ReplicationStatusReconciler"),
			TargetInfo: &ReplicationInfo{},
		}

		original, ok := recon.(*ReplicationStatusReconciler)
		Expect(ok).Should(BeTrue())
		// make sure ReplicationReconciler fields are untouched
		Expect(reflect.DeepEqual(expected, original)).Should(BeTrue())

		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).ShouldNot(HaveOccurred())
		// make sure status conditions and state are retained
		Expect(vrep.IsStatusConditionTrue(v1beta1.Replicating)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateReplicating))
	})
})
