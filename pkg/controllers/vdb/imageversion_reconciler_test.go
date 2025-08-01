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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("k8s/version_reconcile", func() {
	ctx := context.Background()

	It("should update annotations in vdb since they differ", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{
			vmeta.VClusterOpsAnnotation: vmeta.VClusterOpsAnnotationTrue,
		}
		vdb.Spec.Subclusters[0].Size = 1
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		podName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results = cmds.CmdResults{
			podName: []cmds.CmdResult{
				{
					Stdout: `Vertica Analytic Database v11.1.1-0
vertica(v11.1.0) built by @re-docker2 from tag@releases/VER_10_1_RELEASE_BUILD_10_20210413 on 'Wed Jun  2 2021' $BuildId$
`,
				},
			},
		}
		r := MakeImageVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, false, nil, false)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.ObjectMeta.Annotations).ShouldNot(BeNil())
		Expect(fetchVdb.ObjectMeta.Annotations[vmeta.VersionAnnotation]).Should(Equal("v11.1.1-0"))
		Expect(fetchVdb.ObjectMeta.Annotations[vmeta.BuildRefAnnotation]).Should(Equal("releases/VER_10_1_RELEASE_BUILD_10_20210413"))
		Expect(fetchVdb.ObjectMeta.Annotations[vmeta.BuildDateAnnotation]).Should(Equal("Wed Jun  2 2021"))
	})

	It("should update annotations in configmap since they differ", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{
			vmeta.VClusterOpsAnnotation: vmeta.VClusterOpsAnnotationTrue,
			vmeta.VersionAnnotation:     "v23.4.0",
		}
		const sbName = "sb1"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1, Type: vapi.SecondarySubcluster},
			{Name: "default", Size: 1, Type: vapi.PrimarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sbName, Subclusters: []vapi.SandboxSubcluster{{Name: vdb.Spec.Subclusters[0].Name}}},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{vdb.Spec.Subclusters[0].Name}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateConfigMap(ctx, k8sClient, vdb, "", "", sbName)
		defer test.DeleteConfigMap(ctx, k8sClient, vdb, sbName)

		fpr := &cmds.FakePodRunner{}

		pfacts := podfacts.MakePodFactsForSandboxWithCacheManager(vdbRec, fpr, logger, TestPassword, sbName, vdbRec.CacheManager)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		podName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results = cmds.CmdResults{
			podName: []cmds.CmdResult{{Stdout: mockVerticaVersionOutput("v11.1.1-0")}},
		}
		r := MakeImageVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, false, nil, false)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		cm := &corev1.ConfigMap{}
		nm := names.GenSandboxConfigMapName(vdb, sbName)
		Expect(k8sClient.Get(ctx, nm, cm)).Should(Succeed())
		Expect(cm.ObjectMeta.Annotations).ShouldNot(BeNil())
		Expect(cm.ObjectMeta.Annotations[vmeta.VersionAnnotation]).Should(Equal("v11.1.1-0"))
	})

	It("should fail the reconciler if doing a downgrade", func() {
		vdb := vapi.MakeVDB()
		const OrigVersion = "v11.0.1"
		vdb.ObjectMeta.Annotations = map[string]string{
			vmeta.VersionAnnotation: OrigVersion,
		}
		vdb.Spec.Subclusters[0].Size = 1
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		podName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results = cmds.CmdResults{
			podName: []cmds.CmdResult{{Stdout: mockVerticaVersionOutput("v11.0.0-0")}},
		}
		r := MakeImageVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, true, nil, false)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

		// Ensure we didn't update the vdb
		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.ObjectMeta.Annotations[vmeta.VersionAnnotation]).Should(Equal(OrigVersion))
	})

	It("should fail if versions are the same during online upgrade", func() {
		vdb := vapi.MakeVDB()
		const OrigVersion = "v24.3.0-1"
		vdb.ObjectMeta.Annotations = map[string]string{
			vmeta.VersionAnnotation: OrigVersion,
		}

		vdb.Spec.Subclusters[0].Size = 1
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		cond := vapi.MakeCondition(vapi.OnlineUpgradeInProgress, metav1.ConditionTrue, "")
		cond.LastTransitionTime = metav1.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
		vdb.Status.Conditions = []metav1.Condition{
			*cond,
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		podName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results = cmds.CmdResults{
			podName: []cmds.CmdResult{{Stdout: mockVerticaVersionOutput(OrigVersion)}},
		}
		act := MakeImageVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, true, nil, false)
		r := act.(*ImageVersionReconciler)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

		newVersion := map[string]string{
			vmeta.VersionAnnotation: OrigVersion,
		}
		_, failureReason, _ := r.isUpgradePathSupported(ctx, newVersion)
		Expect(failureReason).Should(ContainSubstring("Versions are the same and can cause issues"))
	})

	It("should fail the reconciler if we use wrong image", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())

		r := MakeImageVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, true, nil, false)
		// both the vclusterops annotation and admintoolsExists are false
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err.Error()).Should(ContainSubstring("image vertica-k8s:latest is meant for vclusterops style"))

		// Update both the vclusterops annotation and admintoolsExists to true
		vdb.ObjectMeta.Annotations = map[string]string{
			vmeta.VClusterOpsAnnotation: vmeta.VClusterOpsAnnotationTrue,
		}
		podWithNoDB := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pfacts.Detail[podWithNoDB].SetAdmintoolsExists(true)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err.Error()).Should(ContainSubstring("image vertica-k8s:latest is meant for admintools style"))
	})

	It("should fail the reconclier if NMA sidecar deployment is not supported by version", func() {
		annotations := map[string]string{
			vmeta.VClusterOpsAnnotation: vmeta.VClusterOpsAnnotationTrue,
		}
		testNMARunningMode(ctx, vapi.VcluseropsAsDefaultDeploymentMethodMinVersion,
			vapi.NMAInSideCarDeploymentMinVersion, annotations)
	})

	It("should fail the reconciler if we try to use an old NMA and fetch NMA certs from GSM", func() {
		const gsmCertNotSupported = "v23.4.0"
		testNMATLSSecretWithVersion(ctx, "gsm://projects/123456789/secrets/test/versions/6",
			gsmCertNotSupported,
			vapi.NMATLSSecretInGSMMinVersion,
			false /* does not have NMA sidecar */)
	})

	It("should fail the reconciler if we try to use an old NMA and fetch NMA certs from AWS", func() {
		testNMATLSSecretWithVersion(ctx, "awssm://my-secret-arn",
			vapi.VcluseropsAsDefaultDeploymentMethodMinVersion,
			vapi.NMATLSSecretInAWSSecretsManagerMinVersion,
			true /* does not have NMA sidecar */)
	})
})

// mockVerticaVersionOutput will generate fake output from vertica --version for
// a given version. The version must be in the form of v23.4.0.
func mockVerticaVersionOutput(mockVersion string) string {
	return fmt.Sprintf(`Vertica Analytic Database %s
built by test from tag@abcdef on 'Dec 21 2023'`, mockVersion)
}

// testReconcileWithNMATLSecret will run the reconciler twice with the given
// name of the NMA TLS Secret. The first time it will use the old version and
// expect the reconciler to requeue. Then it will run it again but with the new
// version and expect it to succeed.
func testNMATLSSecretWithVersion(ctx context.Context, secretName, oldVersion, newVersion string, hasNMASidecar bool) {
	vdb := vapi.MakeVDB()
	vdb.Spec.Subclusters[0].Size = 1
	vdb.ObjectMeta.Annotations = map[string]string{
		vmeta.VClusterOpsAnnotation: vmeta.VClusterOpsAnnotationTrue,
	}
	vdb.Spec.HTTPSNMATLS.Secret = secretName
	test.CreateVDB(ctx, k8sClient, vdb)
	defer test.DeleteVDB(ctx, k8sClient, vdb)
	test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
	defer test.DeletePods(ctx, k8sClient, vdb)

	fpr := &cmds.FakePodRunner{}
	pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
	Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
	podName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
	fpr.Results = cmds.CmdResults{
		podName: []cmds.CmdResult{{Stdout: mockVerticaVersionOutput(oldVersion)}},
	}
	pfacts.Detail[podName].SetHasNMASidecar(hasNMASidecar)

	r := MakeImageVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, true, nil, false)
	Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

	fpr.Results = cmds.CmdResults{
		podName: []cmds.CmdResult{{Stdout: mockVerticaVersionOutput(newVersion)}},
	}
	res, _ := r.Reconcile(ctx, &ctrl.Request{})
	Expect(res).Should(Equal(ctrl.Result{}))
}

func testNMARunningMode(ctx context.Context, badVersion,
	goodVersion string, annotations map[string]string) {
	vdb := vapi.MakeVDB()
	vdb.ObjectMeta.Annotations = annotations
	vdb.Spec.Subclusters[0].Size = 1
	test.CreateVDB(ctx, k8sClient, vdb)
	defer test.DeleteVDB(ctx, k8sClient, vdb)
	test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
	defer test.DeletePods(ctx, k8sClient, vdb)

	fpr := &cmds.FakePodRunner{}
	pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
	Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
	podName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)

	fpr.Results = cmds.CmdResults{
		podName: []cmds.CmdResult{{Stdout: mockVerticaVersionOutput(badVersion)}},
	}
	r := MakeImageVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, true, nil, false)
	res, err := r.Reconcile(ctx, &ctrl.Request{})
	Expect(res).Should(Equal(ctrl.Result{Requeue: true}))
	Expect(err).Should(Succeed())
	fpr.Results = cmds.CmdResults{
		podName: []cmds.CmdResult{{Stdout: mockVerticaVersionOutput(goodVersion)}},
	}
	res, err = r.Reconcile(ctx, &ctrl.Request{})
	Expect(res).Should(Equal(ctrl.Result{}))
	Expect(err).Should(Succeed())
}
