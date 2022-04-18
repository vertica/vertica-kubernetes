/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("init_db", func() {
	ctx := context.Background()

	It("should be able to read the auth from secret", func() {
		vdb := vapi.MakeVDB()
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			VRec:    vdbRec,
			Log:     logger,
			Vdb:     vdb,
			PRunner: fpr,
		}
		Expect(g.getCommunalAuth(ctx)).Should(Equal(fmt.Sprintf("%s:%s", testAccessKey, testSecretKey)))
	})

	It("should return s3 endpoint stripped of https/http", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Endpoint = "https://192.168.0.1"

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			VRec:    vdbRec,
			Log:     logger,
			Vdb:     vdb,
			PRunner: fpr,
		}

		Expect(g.getCommunalEndpoint()).Should(Equal("192.168.0.1"))

		Expect(g.getEnableHTTPS()).Should(Equal("1"))

		vdb.Spec.Communal.Endpoint = "http://fqdn.example.com:8080"

		Expect(g.getCommunalEndpoint()).Should(Equal("fqdn.example.com:8080"))
		Expect(g.getEnableHTTPS()).Should(Equal("0"))

		vdb.Spec.Communal.Endpoint = "https://minio/"
		Expect(g.getCommunalEndpoint()).Should(Equal("minio"))

		vdb.Spec.Communal.Endpoint = "https://minio:3000/"
		Expect(g.getCommunalEndpoint()).Should(Equal("minio:3000"))
	})

	It("should fail to get host list if some pods not running", func() {
		vdb := vapi.MakeVDB()
		const ScIndex = 0
		const ScSize = 2
		vdb.Spec.Subclusters[ScIndex].Size = ScSize
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		const PodIndex = 0
		test.SetPodStatus(ctx, k8sClient, 1 /* funcOffset */, names.GenPodName(vdb, &vdb.Spec.Subclusters[ScIndex], PodIndex),
			ScIndex, PodIndex, test.AllPodsRunning)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, ScSize)

		g := GenericDatabaseInitializer{
			VRec:    vdbRec,
			Log:     logger,
			Vdb:     vdb,
			PRunner: fpr,
			PFacts:  pfacts,
		}
		podList := []*PodFact{}
		for i := range pfacts.Detail {
			podList = append(podList, pfacts.Detail[i])
		}
		ok := g.checkPodList(podList)
		Expect(ok).Should(BeFalse())
	})

	It("should setup auth file with hdfs config dir if hdfs communal path is used", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "webhdfs://myhdfscluster1"
		vdb.Spec.Communal.HadoopConfig = "hadoop-conf"
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		_ = contructAuthParmsHelper(ctx, vdb, "HadoopConf")
	})

	It("should create an empty auth file if hdfs is used and no hdfs config dir was specified", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "webhdfs://myhdfscluster2"
		vdb.Spec.Communal.HadoopConfig = ""
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		cmds := contructAuthParmsHelper(ctx, vdb, "cat")
		Expect(len(cmds[0].Command)).Should(Equal(3))
		Expect(cmds[0].Command[2]).Should(ContainSubstring(fmt.Sprintf("%s<<< ''", paths.AuthParmsFile)))
	})

	It("should create a auth file with google parms when using GCloud", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "gs://vertica-fleeting/mydb"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		_ = contructAuthParmsHelper(ctx, vdb, "GCSAuth")
	})

	It("should create an auth file with azure parms when using azb:// scheme and accountKey", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "azb://account/container/path1"
		createAzureAccountKeyCredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		cmds := contructAuthParmsHelper(ctx, vdb, "AzureStorageCredentials")
		Expect(len(cmds[0].Command)).Should(Equal(3))
		Expect(cmds[0].Command[2]).Should(ContainSubstring(cloud.AzureAccountKey))
		Expect(cmds[0].Command[2]).ShouldNot(ContainSubstring(cloud.AzureSharedAccessSignature))
	})

	It("should create an auth file with azure parms when using azb:// scheme and shared access signature", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "azb://account/container/path2"
		createAzureSASCredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		cmds := contructAuthParmsHelper(ctx, vdb, "AzureStorageCredentials")
		Expect(len(cmds[0].Command)).Should(Equal(3))
		Expect(cmds[0].Command[2]).ShouldNot(ContainSubstring(cloud.AzureAccountKey))
		Expect(cmds[0].Command[2]).Should(ContainSubstring(cloud.AzureSharedAccessSignature))
	})

	It("should include Kerberos parms if there are kerberos settings", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.KerberosRealm = "EXAMPLE.COM"
		vdb.Spec.Communal.KerberosServiceName = "vertica"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		_ = contructAuthParmsHelper(ctx, vdb, "KerberosRealm")
	})

	It("should requeue if trying to use Kerberos but have an older engine version", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.KerberosRealm = "VERTICACORP.COM"
		vdb.Spec.Communal.KerberosServiceName = "vert"
		// Setting this annotation will set the version in the vdb.  The version
		// was picked because it isn't compatible with Kerberos.
		vdb.Annotations[vapi.VersionAnnotation] = "v11.0.1"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			VRec:    vdbRec,
			Log:     logger,
			Vdb:     vdb,
			PRunner: fpr,
		}

		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		res, err := g.ConstructAuthParms(ctx, atPod)
		ExpectWithOffset(1, err).Should(Succeed())
		ExpectWithOffset(1, res).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should return correct protocol when calling getEndpointProtocol", func() {
		Expect(getEndpointProtocol("")).Should(Equal(cloud.AzureDefaultProtocol))
		Expect(getEndpointProtocol("192.168.0.1")).Should(Equal(cloud.AzureDefaultProtocol))
		Expect(getEndpointProtocol("accountname.mcr.net")).Should(Equal(cloud.AzureDefaultProtocol))
		Expect(getEndpointProtocol("https://accountname.mcr.net")).Should(Equal(cloud.AzureDefaultProtocol))
		Expect(getEndpointProtocol("http://accountname.mcr.net:300")).Should(Equal("HTTP"))
		Expect(getEndpointProtocol("http://192.168.0.1")).Should(Equal("HTTP"))
	})

	It("should return host/port without protocol when calling getEndpointHostPort", func() {
		Expect(getEndpointHostPort("192.168.0.1")).Should(Equal("192.168.0.1"))
		Expect(getEndpointHostPort("hostname:10000")).Should(Equal("hostname:10000"))
		Expect(getEndpointHostPort("http://hostname")).Should(Equal("hostname"))
		Expect(getEndpointHostPort("https://tlsHost:3000")).Should(Equal("tlsHost:3000"))
		Expect(getEndpointHostPort("account@myhost")).Should(Equal("account@myhost"))
		Expect(getEndpointHostPort("azb://account/container/db/")).Should(Equal("account/container/db"))

	})

})

func contructAuthParmsHelper(ctx context.Context, vdb *vapi.VerticaDB, mustHaveCmd string) []cmds.CmdHistory {
	fpr := &cmds.FakePodRunner{}
	g := GenericDatabaseInitializer{
		VRec:    vdbRec,
		Log:     logger,
		Vdb:     vdb,
		PRunner: fpr,
	}

	atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
	res, err := g.ConstructAuthParms(ctx, atPod)
	ExpectWithOffset(1, err).Should(Succeed())
	ExpectWithOffset(1, res).Should(Equal(ctrl.Result{}))
	c := fpr.FindCommands(mustHaveCmd)
	ExpectWithOffset(1, len(c)).Should(Equal(1))
	return c
}
