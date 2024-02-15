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

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/showrestorepoints"

	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/types"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("query_reconcile", func() {
	ctx := context.Background()

	It("should failed the reconciler with admintools", func() {
		vdb := vapi.MakeVDB()
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		recon := MakeRestorePointsQueryReconciler(vrpqRec, vrpq, logger)
		result, err := recon.Reconcile(ctx, &ctrl.Request{})

		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err.Error()).To(ContainSubstring("ShowRestorePoints is not supported for admintools deployments"))
	})

	It("should update query conditions and state if the vclusterops API succeeded", func() {
		vdb := vapi.MakeVDB()
		secretName := "tls-1"
		vdb.Spec.NMATLSSecret = secretName
		setupAPIFunc := func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger) {
			return &MockVClusterOps{}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(vdb, setupAPIFunc)
		createS3CredSecret(ctx, dispatcher.VDB)
		defer deleteCommunalCredSecret(ctx, dispatcher.VDB)
		test.CreateVDB(ctx, k8sClient, dispatcher.VDB)
		defer test.DeleteVDB(ctx, k8sClient, dispatcher.VDB)
		test.CreatePods(ctx, k8sClient, dispatcher.VDB, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, dispatcher.VDB)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, k8sClient, secretName)
		defer test.DeleteSecret(ctx, k8sClient, secretName)

		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		err := constructVrpqDispatcher(ctx, vrpq, dispatcher)
		Ω(err).Should(Succeed())

		// make sure that Quering condition is updated to false and
		// QueryComplete condition is updated to True
		// message is updated to "Query successful"
		Expect(vrpq.IsStatusConditionFalse(v1beta1.Querying)).Should(BeTrue())
		Expect(vrpq.IsStatusConditionTrue(v1beta1.QueryComplete)).Should(BeTrue())
		Expect(vrpq.Status.State).Should(Equal(stateSuccessQuery))
	})

	It("should set azure parms in config parms map when using azb:// scheme and accountKey", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "azb://account/container/path1"
		createAzureAccountKeyCredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		parms := contructAuthParmsMapForVrpq(ctx, vrpq, "AzureStorageCredentials")
		ExpectWithOffset(1, parms.GetValue("AzureStorageCredentials")).ShouldNot(ContainSubstring(cloud.AzureSharedAccessSignature))
		ExpectWithOffset(1, parms.GetValue("AzureStorageCredentials")).Should(ContainSubstring(cloud.AzureAccountKey))
	})

	It("should set azure parms in config parms map when using azb:// scheme and shared access signature", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "azb://account/container/path2"
		createAzureSASCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		params := contructAuthParmsMapForVrpq(ctx, vrpq, "AzureStorageCredentials")
		ExpectWithOffset(1, params.GetValue("AzureStorageCredentials")).Should(ContainSubstring(cloud.AzureSharedAccessSignature))
		ExpectWithOffset(1, params.GetValue("AzureStorageCredentials")).ShouldNot(ContainSubstring(cloud.AzureAccountKey))
	})

	It("should not create an auth parms if no communal path given", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		contructAuthParmsHelperForVrpq(ctx, vrpq, "", "")
	})

	It("should add additional server config parms to config parms map", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.AdditionalConfig = map[string]string{
			"Parm1": "parm1",
		}
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		g := constructVrpqQuery(ctx, vrpq)
		g.SetAdditionalConfigParms()
		Expect(g.ConfigurationParams.ContainKeyValuePair("Parm1", "parm1")).Should(Equal(true))
	})
})

func contructAuthParmsMapForVrpq(ctx context.Context,
	vrpq *v1beta1.VerticaRestorePointsQuery, key string) *types.CiMap {
	g := constructVrpqQuery(ctx, vrpq)
	_, ok := g.ConfigurationParams.Get(key)
	ExpectWithOffset(1, ok).Should(Equal(true))
	return g.ConfigurationParams
}

func contructAuthParmsHelperForVrpq(ctx context.Context, vrpq *v1beta1.VerticaRestorePointsQuery, key, value string) {
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

func constructVrpqQuery(ctx context.Context, vrpq *v1beta1.VerticaRestorePointsQuery) *QueryReconciler {
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

func constructVrpqDispatcher(ctx context.Context, vrpq *v1beta1.VerticaRestorePointsQuery, dispatcher *vadmin.VClusterOps) error {
	g := &QueryReconciler{
		VRec: vrpqRec,
		Vrpq: vrpq,
		Log:  logger,
		ConfigParamsGenerator: config.ConfigParamsGenerator{
			VRec: vrpqRec,
			Log:  logger,
		},
	}
	opts := []showrestorepoints.Option{}
	opts = append(opts,
		showrestorepoints.WithInitiator(vrpq.ExtractNamespacedName(), "192.168.0.1"),
		showrestorepoints.WithCommunalPath("/communal"),
	)
	err := g.runShowRestorePoints(ctx, dispatcher, opts)
	return err
}
