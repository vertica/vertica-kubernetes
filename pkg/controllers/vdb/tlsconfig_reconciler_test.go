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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("httpstls_reconciler", func() {
	ctx := context.Background()

	It("tls config reconciler skips reconcile loop when conditions are not met", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.EncryptSpreadComm = vapi.EncryptSpreadCommDisabled
		vdb.Spec.Subclusters[0].Size = 3
		vdb.Spec.HTTPSNMATLS.Secret = rotateHTTPSCertNewNMASecretName
		test.CreateFakeTLSSecret(ctx, vdb, k8sClient, vdb.GetHTTPSNMATLSSecret())
		defer test.DeleteSecret(ctx, k8sClient, vdb.GetHTTPSNMATLSSecret())
		test.CreateFakeTLSSecret(ctx, vdb, k8sClient, rotateHTTPSCertCurrentNMASecretName)
		defer test.DeleteSecret(ctx, k8sClient, rotateHTTPSCertCurrentNMASecretName)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 3)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)

		r := MakeTLSConfigReconciler(vdbRec, logger, vdb, fpr, dispatcher, pfacts, vapi.HTTPSNMATLSConfigName, &TLSConfigManager{})
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		vapi.SetVDBForTLS(vdb)
		Expect(vdb.IsStatusConditionTrue(vapi.DBInitialized)).Should(Equal(false))
		r = MakeTLSConfigReconciler(vdbRec, logger, vdb, fpr, dispatcher, pfacts, vapi.HTTPSNMATLSConfigName, &TLSConfigManager{})
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		vdb.Status.TLSConfigs = []vapi.TLSConfigStatus{
			{
				Secret: rotateHTTPSCertCurrentNMASecretName,
				Name:   vapi.HTTPSNMATLSConfigName,
			},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("verify the parameters passed to set_tls_config API", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.EncryptSpreadComm = vapi.EncryptSpreadCommDisabled
		vdb.Spec.Subclusters[0].Size = 3
		vdb.Spec.HTTPSNMATLS.Secret = rotateHTTPSCertNewNMASecretName
		test.CreateFakeTLSSecret(ctx, vdb, k8sClient, vdb.GetHTTPSNMATLSSecret())

		// sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 3)
		dispatcher := mockVClusterOpsDispatcher(vdb)
		vapi.SetVDBForTLS(vdb)
		const tryVerify = "try_verify"
		man := &TLSConfigManager{
			Vdb:        vdb,
			Dispatcher: dispatcher,
		}
		man.NewTLSMode = tryVerify
		man.NewSecret = rotateHTTPSCertNewNMASecretName
		r := MakeTLSConfigReconciler(vdbRec, logger, vdb, fpr, dispatcher, pfacts, vapi.HTTPSNMATLSConfigName, man)

		initiatorPod := &podfacts.PodFact{}
		tlsConfigReconciler := r.(*TLSConfigReconciler)
		Expect(tlsConfigReconciler.runDDLToConfigureTLS(ctx, initiatorPod, true)).Should(Succeed())
	})

})
