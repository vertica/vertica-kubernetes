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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/types"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

const testS3SseCustomerKeySecret = "ssec-secret"
const testS3SseKmsKeyID = "fakeid"
const testSseVerticaOlderVersion = "v12.0.0"

var _ = Describe("init_db", func() {
	ctx := context.Background()

	It("should be able to read the auth from secret", func() {
		vdb := vapi.MakeVDB()
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			PRunner: fpr,
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec: vdbRec,
				Log:  logger,
				Vdb:  vdb,
			},
		}
		Expect(g.GetCommunalAuth(ctx)).Should(Equal(fmt.Sprintf("%s:%s", testAccessKey, testSecretKey)))
	})

	It("should return s3 endpoint stripped of https/http", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Endpoint = "https://192.168.0.1"

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			PRunner: fpr,
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec: vdbRec,
				Log:  logger,
				Vdb:  vdb,
			},
		}

		Expect(g.GetCommunalEndpoint()).Should(Equal("192.168.0.1"))

		Expect(g.GetEnableHTTPS()).Should(Equal("1"))

		vdb.Spec.Communal.Endpoint = "http://fqdn.example.com:8080"

		Expect(g.GetCommunalEndpoint()).Should(Equal("fqdn.example.com:8080"))
		Expect(g.GetEnableHTTPS()).Should(Equal("0"))

		vdb.Spec.Communal.Endpoint = "https://minio/"
		Expect(g.GetCommunalEndpoint()).Should(Equal("minio"))

		vdb.Spec.Communal.Endpoint = "https://minio:3000/"
		Expect(g.GetCommunalEndpoint()).Should(Equal("minio:3000"))
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
			PRunner: fpr,
			PFacts:  pfacts,
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec: vdbRec,
				Log:  logger,
				Vdb:  vdb,
			},
		}
		podList := []*PodFact{}
		for i := range pfacts.Detail {
			podList = append(podList, pfacts.Detail[i])
		}
		ok := g.checkPodList(podList)
		Expect(ok).Should(BeFalse())
	})

	It("should set hdfs config dir in config parms map if hdfs communal path is used", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "webhdfs://myhdfscluster1"
		vdb.Spec.HadoopConfig = "hadoop-conf"
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		contructAuthParmsHelper(ctx, vdb, "HadoopConfDir", "")
	})

	It("should have minimal config parms map if hdfs is used and no hdfs config dir was specified", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "webhdfs://myhdfscluster2"
		vdb.Spec.HadoopConfig = ""
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			PRunner: fpr,
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec:                vdbRec,
				Log:                 logger,
				Vdb:                 vdb,
				ConfigurationParams: types.MakeCiMap(),
			},
		}

		res, err := g.ConstructConfigParms(ctx)
		ExpectWithOffset(1, err).Should(Succeed())
		ExpectWithOffset(1, res).Should(Equal(ctrl.Result{}))
		Expect(g.ConfigurationParams.Size()).Should(Equal(2))
		v, ok := g.ConfigurationParams.Get("InitialDefaultSubclusterName")
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(vdb.Spec.Subclusters[0].Name))
		v, ok = g.ConfigurationParams.Get("EncryptSpreadComm")
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(vapi.EncryptSpreadCommWithVertica))
	})

	It("should set google parms in config parms map when using GCloud", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "gs://vertica-fleeting/mydb"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		contructAuthParmsHelper(ctx, vdb, "GCSAuth", "")
	})

	It("should read communal credentials from correct places", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "gs://test/mydb"
		secretName := "gcp-secret"
		// Operator will try to read communal credentials from GSM. We will
		// get a read error since we cannot access the secret in the unit test.
		// But that error will indicate we are reading the credentials from
		// the correct place.
		vdb.Spec.Communal.CredentialSecret = "gsm://" + secretName

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			PRunner: fpr,
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec:                vdbRec,
				Log:                 logger,
				Vdb:                 vdb,
				ConfigurationParams: types.MakeCiMap(),
			},
		}

		res, err := g.ConstructConfigParms(ctx)
		ExpectWithOffset(1, err).Should(MatchError(ContainSubstring("failed to create secretmanager client")))
		ExpectWithOffset(1, res).Should(Equal(ctrl.Result{}))
		_, ok := g.ConfigurationParams.Get("GCSAuth")
		Expect(ok).Should(BeFalse())

		// Operator will read communal credentials from a secret in k8s if
		// the secret name does not have the prefix "gsm://".
		g.Vdb.Spec.Communal.CredentialSecret = secretName
		createK8sCredSecret(ctx, g.Vdb)
		defer deleteCommunalCredSecret(ctx, g.Vdb)
		res, err = g.ConstructConfigParms(ctx)
		ExpectWithOffset(1, err).Should(Succeed())
		ExpectWithOffset(1, res).Should(Equal(ctrl.Result{}))
		v, ok := g.ConfigurationParams.Get("GCSAuth")
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(fmt.Sprintf("%s:%s", testAccessKey, testSecretKey)))
	})

	It("should set azure parms in config parms map when using azb:// scheme and accountKey", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "azb://account/container/path1"
		createAzureAccountKeyCredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		parms := ContructAuthParmsMap(ctx, vdb, "AzureStorageCredentials")
		ExpectWithOffset(1, parms.GetValue("AzureStorageCredentials")).ShouldNot(ContainSubstring(cloud.AzureSharedAccessSignature))
		ExpectWithOffset(1, parms.GetValue("AzureStorageCredentials")).Should(ContainSubstring(cloud.AzureAccountKey))
	})

	It("should set azure parms in config parms map when using azb:// scheme and shared access signature", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "azb://account/container/path2"
		createAzureSASCredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		parms := ContructAuthParmsMap(ctx, vdb, "AzureStorageCredentials")
		ExpectWithOffset(1, parms.GetValue("AzureStorageCredentials")).Should(ContainSubstring(cloud.AzureSharedAccessSignature))
		ExpectWithOffset(1, parms.GetValue("AzureStorageCredentials")).ShouldNot(ContainSubstring(cloud.AzureAccountKey))
	})

	It("should not create an auth parms if no communal path given", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = ""

		contructAuthParmsHelper(ctx, vdb, "", "")
	})

	It("should requeue if trying to use Kerberos but have an older engine version", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.AdditionalConfig[vmeta.KerberosRealmConfig] = "VERTICACORP.COM"
		vdb.Spec.Communal.AdditionalConfig[vmeta.KerberosServiceNameConfig] = "vert"
		// Setting this annotation will set the version in the vdb.  The version
		// was picked because it isn't compatible with Kerberos.
		vdb.Annotations[vmeta.VersionAnnotation] = "v11.0.1"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			PRunner: fpr,
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec:                vdbRec,
				Log:                 logger,
				Vdb:                 vdb,
				ConfigurationParams: types.MakeCiMap(),
			},
		}

		res, err := g.ConstructConfigParms(ctx)
		ExpectWithOffset(1, err).Should(Succeed())
		ExpectWithOffset(1, res).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should return correct protocol when calling getEndpointProtocol", func() {
		Expect(config.GetEndpointProtocol("")).Should(Equal(cloud.AzureDefaultProtocol))
		Expect(config.GetEndpointProtocol("192.168.0.1")).Should(Equal(cloud.AzureDefaultProtocol))
		Expect(config.GetEndpointProtocol("accountname.mcr.net")).Should(Equal(cloud.AzureDefaultProtocol))
		Expect(config.GetEndpointProtocol("https://accountname.mcr.net")).Should(Equal(cloud.AzureDefaultProtocol))
		Expect(config.GetEndpointProtocol("http://accountname.mcr.net:300")).Should(Equal("HTTP"))
		Expect(config.GetEndpointProtocol("http://192.168.0.1")).Should(Equal("HTTP"))
	})

	It("should return host/port without protocol when calling getEndpointHostPort", func() {
		Expect(config.GetEndpointHostPort("192.168.0.1")).Should(Equal("192.168.0.1"))
		Expect(config.GetEndpointHostPort("hostname:10000")).Should(Equal("hostname:10000"))
		Expect(config.GetEndpointHostPort("http://hostname")).Should(Equal("hostname"))
		Expect(config.GetEndpointHostPort("https://tlsHost:3000")).Should(Equal("tlsHost:3000"))
		Expect(config.GetEndpointHostPort("account@myhost")).Should(Equal("account@myhost"))
		Expect(config.GetEndpointHostPort("azb://account/container/db/")).Should(Equal("account/container/db"))

	})

	It("should set SSE-S3 server-side encryption in config parms map", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseS3
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		contructAuthParmsHelper(ctx, vdb, config.S3ServerSideEncryption, config.SseAlgorithmAES256)
	})

	It("should SSE-KMS server-side encryption in config parms map", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseKMS
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		contructAuthParmsHelper(ctx, vdb, config.S3ServerSideEncryption, config.SseAlgorithmAWSKMS)
	})

	It("should be able to read the sse-c clientkey from secret", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseC
		vdb.Spec.Communal.S3SseCustomerKeySecret = testS3SseCustomerKeySecret
		createS3SseCustomerKeySecret(ctx, vdb)
		defer deleteS3SseCustomerKeySecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			PRunner: fpr,
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec:                vdbRec,
				Log:                 logger,
				Vdb:                 vdb,
				ConfigurationParams: types.MakeCiMap(),
			},
		}
		res, err := g.SetS3SseCustomerKey(ctx)
		ExpectWithOffset(1, err).Should(Succeed())
		ExpectWithOffset(1, res).Should(Equal(ctrl.Result{}))
		Expect(g.ConfigurationParams.ContainKeyValuePair(config.S3SseCustomerKey, testClientKey)).Should(Equal(true))
	})

	It("should SSE-C server-side encryption in config parms map", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseC
		vdb.Spec.Communal.S3SseCustomerKeySecret = testS3SseCustomerKeySecret
		createS3CredSecret(ctx, vdb)
		createS3SseCustomerKeySecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		defer deleteS3SseCustomerKeySecret(ctx, vdb)

		contructAuthParmsHelper(ctx, vdb, config.S3SseCustomerAlgorithm, config.SseAlgorithmAES256)
	})

	It("should include sseKmsKeyId when S3 server-side encryption is SSE-KMS", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseKMS
		vdb.Spec.Communal.AdditionalConfig = map[string]string{}
		vdb.Spec.Communal.AdditionalConfig[vapi.S3SseKmsKeyID] = testS3SseKmsKeyID
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		contructAuthParmsHelper(ctx, vdb, vapi.S3SseKmsKeyID, testS3SseKmsKeyID)
	})

	It("should requeue if trying to use S3 server-side encryption but have an older engine version", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseS3
		// Setting this annotation will set the version in the vdb.  The version
		// was picked because it isn't compatible with server-side encryption.
		vdb.Annotations[vmeta.VersionAnnotation] = testSseVerticaOlderVersion
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		g := GenericDatabaseInitializer{
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec:                vdbRec,
				Log:                 logger,
				Vdb:                 vdb,
				ConfigurationParams: types.MakeCiMap(),
			},
		}

		res, err := g.ConstructConfigParms(ctx)
		ExpectWithOffset(1, err).Should(Succeed())
		ExpectWithOffset(1, res).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should return correct parm/algorithm when calling getServerSideEncryptionAlgorithm", func() {
		vdb := vapi.MakeVDB()

		g := GenericDatabaseInitializer{
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				Vdb:                 vdb,
				ConfigurationParams: types.MakeCiMap(),
			},
		}
		g.Vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseS3
		g.SetServerSideEncryptionAlgorithm()
		Expect(g.ConfigurationParams.ContainKeyValuePair(config.S3ServerSideEncryption, config.SseAlgorithmAES256)).Should(Equal(true))
		g.Vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseKMS
		g.SetServerSideEncryptionAlgorithm()
		Expect(g.ConfigurationParams.ContainKeyValuePair(config.S3ServerSideEncryption, config.SseAlgorithmAWSKMS)).Should(Equal(true))
		g.Vdb.Spec.Communal.S3ServerSideEncryption = vapi.SseC
		g.SetServerSideEncryptionAlgorithm()
		Expect(g.ConfigurationParams.ContainKeyValuePair(config.S3SseCustomerAlgorithm, config.SseAlgorithmAES256)).Should(Equal(true))
	})

	It("should add additional server config parms to config parms map", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.AdditionalConfig = map[string]string{
			"Parm1": "parm1",
		}

		g := GenericDatabaseInitializer{
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec:                vdbRec,
				Vdb:                 vdb,
				ConfigurationParams: types.MakeCiMap(),
			},
		}
		g.SetAdditionalConfigParms()
		Expect(g.ConfigurationParams.ContainKeyValuePair("Parm1", "parm1")).Should(Equal(true))
	})

	It("should skip additional config parm if already present", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.AdditionalConfig = map[string]string{
			"Parm1": "parm1",
			"Parm2": "parm2",
		}

		g := GenericDatabaseInitializer{
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec:                vdbRec,
				Vdb:                 vdb,
				Log:                 logger,
				ConfigurationParams: types.MakeCiMap(),
			},
		}
		g.ConfigurationParams.Set("Parm1", "value")
		g.SetAdditionalConfigParms()
		Expect(g.ConfigurationParams.ContainKeyValuePair("Parm1", "value")).Should(Equal(true))
		Expect(g.ConfigurationParams.ContainKeyValuePair("Parm2", "parm2")).Should(Equal(true))
	})
})

func contructAuthParmsHelper(ctx context.Context, vdb *vapi.VerticaDB, key, value string) {
	g := ConstructDBInitializer(ctx, vdb)
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

func ContructAuthParmsMap(ctx context.Context, vdb *vapi.VerticaDB, key string) *types.CiMap {
	g := ConstructDBInitializer(ctx, vdb)
	_, ok := g.ConfigurationParams.Get(key)
	ExpectWithOffset(1, ok).Should(Equal(true))
	return g.ConfigurationParams
}

func ConstructDBInitializer(ctx context.Context, vdb *vapi.VerticaDB) *GenericDatabaseInitializer {
	g := &GenericDatabaseInitializer{
		ConfigParamsGenerator: config.ConfigParamsGenerator{
			VRec:                vdbRec,
			Log:                 logger,
			Vdb:                 vdb,
			ConfigurationParams: types.MakeCiMap(),
		},
	}

	res, err := g.ConstructConfigParms(ctx)
	ExpectWithOffset(1, err).Should(Succeed())
	ExpectWithOffset(1, res).Should(Equal(ctrl.Result{}))
	return g
}
