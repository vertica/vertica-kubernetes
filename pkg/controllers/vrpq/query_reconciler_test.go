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

package vrpq

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/types"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("query_reconcile", func() {
	ctx := context.Background()

	It("should requeue if VerticaDB doesn't exist", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		req := ctrl.Request{NamespacedName: vapi.MakeSampleVrpqName()}
		Expect(vrpqRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should update query conditions if the vclusterops API succeeded", func() {
		vdb := v1.MakeVDB()
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		recon := MakeRestorePointsQueryReconciler(vrpqRec, vrpq, logger)

		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		// make sure that Quering condition is updated to false and
		// QueryComplete condition is updated to True
<<<<<<< HEAD
		// message is updated to "Query successful"
		Expect(vrpq.IsStatusConditionFalse(vapi.Querying)).Should(BeTrue())
		Expect(vrpq.IsStatusConditionTrue(vapi.QueryComplete)).Should(BeTrue())
		Expect(vrpq.Status.State).Should(Equal(stateSuccessQuery))
=======
		Expect(vrpq.IsStatusConditionFalse(vapi.Querying)).Should(BeTrue())
		Expect(vrpq.IsStatusConditionTrue(vapi.QueryComplete)).Should(BeTrue())
>>>>>>> vnext

	})

	It("should set azure parms in config parms map when using azb:// scheme and accountKey", func() {
		vdb := v1.MakeVDB()
		vdb.Spec.Communal.Path = "azb://account/container/path1"
		createAzureAccountKeyCredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		parms := contructAuthParmsMapForVrpq(ctx, vrpq, "AzureStorageCredentials")
		ExpectWithOffset(1, parms.GetValue("AzureStorageCredentials")).ShouldNot(ContainSubstring(cloud.AzureSharedAccessSignature))
		ExpectWithOffset(1, parms.GetValue("AzureStorageCredentials")).Should(ContainSubstring(cloud.AzureAccountKey))
	})

	It("should set azure parms in config parms map when using azb:// scheme and shared access signature", func() {
		vdb := v1.MakeVDB()
		vdb.Spec.Communal.Path = "azb://account/container/path2"
		createAzureSASCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		params := contructAuthParmsMapForVrpq(ctx, vrpq, "AzureStorageCredentials")
		ExpectWithOffset(1, params.GetValue("AzureStorageCredentials")).Should(ContainSubstring(cloud.AzureSharedAccessSignature))
		ExpectWithOffset(1, params.GetValue("AzureStorageCredentials")).ShouldNot(ContainSubstring(cloud.AzureAccountKey))
	})

	It("should not create an auth parms if no communal path given", func() {
		vdb := v1.MakeVDB()
		vdb.Spec.Communal.Path = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		contructAuthParmsHelperForVrpq(ctx, vrpq, "", "")
	})

	It("should add additional server config parms to config parms map", func() {
		vdb := v1.MakeVDB()
		vdb.Spec.Communal.AdditionalConfig = map[string]string{
			"Parm1": "parm1",
		}
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		g := constructVrpqQuery(ctx, vrpq)
		g.SetAdditionalConfigParms()
		Expect(g.ConfigurationParams.ContainKeyValuePair("Parm1", "parm1")).Should(Equal(true))
	})
})

func contructAuthParmsMapForVrpq(ctx context.Context,
	vrpq *vapi.VerticaRestorePointsQuery, key string) *types.CiMap {
	g := constructVrpqQuery(ctx, vrpq)
	_, ok := g.ConfigurationParams.Get(key)
	ExpectWithOffset(1, ok).Should(Equal(true))
	return g.ConfigurationParams
}

func contructAuthParmsHelperForVrpq(ctx context.Context, vrpq *vapi.VerticaRestorePointsQuery, key, value string) {
	g := constructVrpqQuery(ctx, vrpq)
	if g.Vdb.Spec.Communal.Path == "" {
		ExpectWithOffset(1, g.ConfigurationParams.Size()).Should(Equal(0))
		return
	}
	if value == "" {
		_, ok := g.ConfigurationParams.Get(key)
		ExpectWithOffset(1, ok).Should(Equal(true))
		return
	}
	ExpectWithOffset(1, g.ConfigurationParams.ContainKeyValuePair(key, value)).Should(Equal(true))
}

func constructVrpqQuery(ctx context.Context, vrpq *vapi.VerticaRestorePointsQuery) *QueryReconciler {
	g := &QueryReconciler{
		VRec: vrpqRec,
		Vrpq: vrpq,
		Log:  logger,
		ConfigParamsGenerator: config.ConfigParamsGenerator{
			VRec: vrpqRec,
			Log:  logger,
		},
	}

	res, err := g.collectInfoFromVdb(ctx)
	ExpectWithOffset(1, err).Should(Succeed())
	ExpectWithOffset(1, res).Should(Equal(ctrl.Result{}))
	return g
}
