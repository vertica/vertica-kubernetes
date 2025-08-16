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
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("tls_reconciler", func() {
	ctx := context.Background()

	It("tls reconciler skips nma configmap update when rollback condition is set", func() {
		const oldSecret = "old-secret"
		vdb := vapi.MakeVDB()
		vapi.SetVDBForTLS(vdb)
		vdb.Spec.HTTPSNMATLS.Secret = oldSecret
		test.CreateFakeTLSSecret(ctx, vdb, k8sClient, vdb.GetHTTPSNMATLSSecret())
		defer test.DeleteSecret(ctx, k8sClient, vdb.GetHTTPSNMATLSSecret())
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		configMapName := names.GenNMACertConfigMap(vdb)
		defer deleteConfigMap(ctx, vdb, configMapName.Name)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 3)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, &testPassword)

		// trigger reconcile to set config map
		r := MakeTLSReconciler(vdbRec, logger, vdb, fpr, dispatcher, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		// set rollback condition and update secret
		cond := vapi.MakeCondition(vapi.TLSCertRollbackNeeded, metav1.ConditionTrue, vapi.RollbackAfterHTTPSCertRotationReason)
		Expect(vdbstatus.UpdateCondition(ctx, vdbRec.GetClient(), vdb, cond)).Error().Should(BeNil())
		vdb.Spec.HTTPSNMATLS.Secret = "new-secret"

		// re-trigger reconcile
		r = MakeTLSReconciler(vdbRec, logger, vdb, fpr, dispatcher, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		// verify the content of the config map have not changed
		cm, res, err := getConfigMap(ctx, vdbRec, vdb, configMapName)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(cm.Data[builder.NMASecretNameEnv]).Should(Equal(oldSecret))
	})
})
