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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstart"
	vrepstatus "github.com/vertica/vertica-kubernetes/pkg/vrepstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/vertica/vertica-kubernetes/pkg/test"
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
		sourceSecretName := "tls-1"
		sourceVdb.Spec.NMATLSSecret = sourceSecretName
		setupAPIFunc := func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger) {
			return &MockVClusterOps{}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(sourceVdb, setupAPIFunc)
		// s3 cred secret can be reused by the target vdb
		createS3CredSecret(ctx, sourceVdb)
		defer deleteCommunalCredSecret(ctx, sourceVdb)
		test.CreateVDB(ctx, k8sClient, sourceVdb)
		defer test.DeleteVDB(ctx, k8sClient, sourceVdb)
		test.CreatePods(ctx, k8sClient, sourceVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, sourceVdb)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, k8sClient, sourceSecretName)
		defer test.DeleteSecret(ctx, k8sClient, sourceSecretName)

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
			Client: k8sClient,
			VRec:   vrepRec,
			Vrep:   vrep,
			Log:    logger,
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
		sourceSecretName := "tls-1"
		sourceVdb.Spec.NMATLSSecret = sourceSecretName
		setupAPIFunc := func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger) {
			return &MockVClusterOps{}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(sourceVdb, setupAPIFunc)
		// s3 cred secret can be reused by the target vdb
		createS3CredSecret(ctx, sourceVdb)
		defer deleteCommunalCredSecret(ctx, sourceVdb)
		test.CreateVDB(ctx, k8sClient, sourceVdb)
		defer test.DeleteVDB(ctx, k8sClient, sourceVdb)
		test.CreatePods(ctx, k8sClient, sourceVdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, sourceVdb)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, k8sClient, sourceSecretName)
		defer test.DeleteSecret(ctx, k8sClient, sourceSecretName)

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

		recon := MakeReplicationReconciler(k8sClient, vrepRec, vrep, logger)
		err := vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationComplete,
				metav1.ConditionTrue, "Succeeded")}, stateSucceededReplication)
		Expect(err).ShouldNot(HaveOccurred())
		result, err := recon.Reconcile(ctx, &ctrl.Request{})

		expected := &ReplicationReconciler{
			Client: k8sClient,
			VRec:   vrepRec,
			Vrep:   vrep,
			Log:    logger.WithName("ReplicationReconciler"),
		}
		original, ok := recon.(*ReplicationReconciler)
		Expect(ok).Should(BeTrue())
		Expect(reflect.DeepEqual(expected, original)).Should(BeTrue())

		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).ShouldNot(HaveOccurred())
		Expect(vrep.IsStatusConditionTrue(v1beta1.ReplicationComplete)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateSucceededReplication))

		err = vrepstatus.Reset(ctx, vrepRec.Client, vrepRec.Log, vrep)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(vrep.IsStatusConditionPresent(v1beta1.ReplicationComplete)).Should(BeFalse())
		Expect(vrep.Status.State).Should(Equal(""))

		err = vrepstatus.Update(ctx, vrepRec.Client, vrepRec.Log, vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.ReplicationReady,
				metav1.ConditionFalse, "IncompatibleSourceDB")}, stateIncompatibleDB)
		Expect(err).ShouldNot(HaveOccurred())
		result, err = recon.Reconcile(ctx, &ctrl.Request{})

		original, ok = recon.(*ReplicationReconciler)
		Expect(ok).Should(BeTrue())
		Expect(reflect.DeepEqual(expected, original)).Should(BeTrue())
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).ShouldNot(HaveOccurred())
		Expect(vrep.IsStatusConditionFalse(v1beta1.ReplicationReady)).Should(BeTrue())
		Expect(vrep.Status.State).Should(Equal(stateIncompatibleDB))
	})
})
