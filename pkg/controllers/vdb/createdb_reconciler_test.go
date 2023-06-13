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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("createdb_reconciler", func() {
	ctx := context.Background()

	It("should not call create_db if db already exists", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		dispatcher := vadmin.Fake{}
		r := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		lastCall := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCall.Command).ShouldNot(ContainElements("/opt/vertica/bin/admintools", "create_db"))
	})

	It("should run create db if db doesn't exist", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 3)
		dispatcher := vadmin.Fake{}
		r := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		hist := fpr.FindCommands("/opt/vertica/bin/admintools -t create_db")
		Expect(len(hist)).Should(Equal(1))
		hist = fpr.FindCommands("rm", paths.AuthParmsFile)
		Expect(len(hist)).Should(Equal(1))
	})

	It("host list for create db should only include pods from first subcluster", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, vapi.Subcluster{Name: "secondary", Size: 2})
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 1)
		dispatcher := vadmin.Fake{}
		act := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		r := act.(*CreateDBReconciler)
		hostList, ok := r.getPodList()
		Expect(ok).Should(BeTrue())
		Expect(len(hostList)).Should(Equal(1))
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Expect(hostList[0].dnsName).Should(ContainSubstring(pn.Name))
	})

	It("host list should contain 1 pod when kSafety is 0", func() {
		const firstScSize = 3
		hostListLength := createMultiPodSubclusterForKsafe(ctx, vapi.KSafety0, firstScSize)
		Expect(hostListLength).Should(Equal(1))
	})

	It("host list should contain all pods when kSafety is 1", func() {
		const firstScSize = 3
		hostListLength := createMultiPodSubclusterForKsafe(ctx, vapi.KSafety1, firstScSize)
		Expect(hostListLength).Should(Equal(firstScSize))
	})

	It("should skip reconciler entirely if initPolicy is not Create", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyRevive

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		dispatcher := vadmin.Fake{}
		r := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(len(fpr.Histories)).Should(Equal(0))
	})

	It("should have DDL to encrypt spread if that setting is used", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.EncryptSpreadComm = vapi.EncryptSpreadCommWithVertica
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size))
		dispatcher := vadmin.Fake{}
		r := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		hist := fpr.FindCommands("alter database default set parameter EncryptSpreadComm")
		Expect(len(hist)).Should(Equal(1))

		// The restart condition variable should be set to true also
		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Status.Conditions)).Should(BeNumerically(">=", vapi.VerticaRestartNeededIndex))
		Expect(fetchVdb.Status.Conditions[vapi.VerticaRestartNeededIndex].Status).Should(Equal(corev1.ConditionTrue))
	})

	It("should generate a requeue error for various known createdb errors", func() {
		vdb := vapi.MakeVDB()

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		dispatcher := vadmin.Fake{}
		act := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		r := act.(*CreateDBReconciler)
		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)

		errStrings := []string{
			"Unable to connect to endpoint",
			"The specified bucket does not exist.",
			"Communal location [s3://blah] is not empty",
			"You are trying to access your S3 bucket using the wrong region. If you are using S3",
			"The authorization header is malformed; the region 'us-east-1' is wrong; expecting 'eu-central-1'.",
			"An error occurred during kerberos authentication",
		}

		for i := range errStrings {
			fpr.Results = cmds.CmdResults{
				atPod: []cmds.CmdResult{
					{
						Stdout: errStrings[i],
						Err:    errors.New("at command failed"),
					},
				},
			}
			Expect(r.execCmd(ctx, atPod, []string{"create_db"})).Should(Equal(ctrl.Result{Requeue: true}), "Failing with '%s'", errStrings[i])
		}

		fpr.Results = cmds.CmdResults{
			atPod: []cmds.CmdResult{
				{
					Stdout: "*** Unknown error",
					Err:    errors.New("at command failed"),
				},
			},
		}
		res, err := r.execCmd(ctx, atPod, []string{"create_db"})
		Expect(err).ShouldNot(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
	})

	It("should always run AT commands from the first pod of the first primary subcluster", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sec", IsPrimary: false, Size: 1},
			{Name: "pri", IsPrimary: true, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size+vdb.Spec.Subclusters[1].Size))
		dispatcher := vadmin.Fake{}
		r := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(len(fpr.Histories)).Should(BeNumerically(">", 0))
		hist := fpr.FindCommands("-t create_db")
		Expect(len(hist)).Should(Equal(1))
		Expect(hist[0].Pod.Name).Should(Equal(names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0).Name))
	})

	It("should use option with create_db if skipping install", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyCreateSkipPackageInstall
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = "v12.0.1-0"
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size))
		dispatcher := vadmin.Fake{}
		r := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(len(fpr.Histories)).Should(BeNumerically(">", 0))
		hist := fpr.FindCommands("-t create_db")
		Expect(len(hist)).Should(Equal(1))
		Expect(hist[0].Command).Should(ContainElement("--skip-package-install"))
	})

	It("should call mkdir when creating db using posix path", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "/host/db"
		vdb.Spec.Communal.Endpoint = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size))
		dispatcher := vadmin.Fake{}
		r := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(len(fpr.Histories)).Should(BeNumerically(">", 0))
		hist := fpr.FindCommands("mkdir")
		Expect(len(hist)).Should(Equal(1))
	})
})

// Helper function for kSafety verification
func createMultiPodSubclusterForKsafe(ctx context.Context, ksafe vapi.KSafetyType, firstScSize int32) int {
	vdb := vapi.MakeVDB()
	vdb.Spec.Subclusters[0].Size = firstScSize
	vdb.Spec.KSafety = ksafe
	vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, vapi.Subcluster{Name: "secondary", Size: 2})
	test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
	defer test.DeletePods(ctx, k8sClient, vdb)

	fpr := &cmds.FakePodRunner{}
	pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 1)
	dispatcher := vadmin.Fake{}
	act := MakeCreateDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
	r := act.(*CreateDBReconciler)
	hostList, ok := r.getPodList()
	Expect(ok).Should(BeTrue())

	return len(hostList)
}
