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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	rotateHTTPSCertNewNMASecretName     = "rotate-https-new-cert-test-secret"     //nolint:gosec
	rotateHTTPSCertCurrentNMASecretName = "rotate-https-current-cert-test-secret" //nolint:gosec
)

var _ = Describe("httpscertrotation_reconciler", func() {
	ctx := context.Background()

	It("https cert reconciler should requeue", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.EncryptSpreadComm = vapi.EncryptSpreadCommDisabled
		vdb.Spec.Subclusters[0].Size = 3
		vdb.Spec.HTTPSNMATLS.Secret = rotateHTTPSCertNewNMASecretName
		vapi.SetVDBForTLS(vdb)
		test.CreateFakeTLSSecret(ctx, vdb, k8sClient, vdb.Spec.HTTPSNMATLS.Secret)
		defer test.DeleteSecret(ctx, k8sClient, vdb.Spec.HTTPSNMATLS.Secret)
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
		vdb.Status.TLSConfig = []vapi.TLSConfig{
			{
				Secret: rotateHTTPSCertCurrentNMASecretName,
				Name:   vapi.HTTPSNMATLSConfigName,
			},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		r := MakeHTTPSCertRotationReconciler(vdbRec, logger, vdb, dispatcher, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
	})

})
