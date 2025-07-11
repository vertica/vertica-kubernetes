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

package vadmin

import (
	"context"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/test"
)

var _ = Describe("vdb_context", func() {
	var ctx = context.Background()
	It("should return same strings used to create secret", func() {

		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.HTTPSNMATLS.Secret = TestNMATLSSecret
		secret := test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, TestNMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		fetcher := &cloud.SecretFetcher{
			Client:   dispatcher.Client,
			Log:      dispatcher.Log,
			Obj:      dispatcher.VDB,
			EVWriter: dispatcher.EVWriter,
		}
		dispatcher.CacheManager.InitCertCacheForVdb("default", "test-vdb", fetcher)
		defer dispatcher.CacheManager.DestroyCertCacheForVdb("default", "test-vdb")

		vdbCertCache := dispatcher.CacheManager.GetCertCacheForVdb("default", "test-vdb")
		Expect(vdbCertCache).ShouldNot(Equal(nil))
		vdbCertCacheOne := dispatcher.CacheManager.GetCertCacheForVdb("default", "test-vdb")
		vdbCertCacheTwo := dispatcher.CacheManager.GetCertCacheForVdb("default", "test-vdb")
		Expect(vdbCertCacheTwo).Should(Equal(vdbCertCacheOne))

		cert, err := vdbCertCacheOne.ReadCertFromSecret(ctx, TestNMATLSSecret)
		Ω(err).Should(BeNil())
		Ω(cert.Key).Should(Equal(test.TestKeyValue))
		Ω(cert.Cert).Should(Equal(test.TestCertValue))
		Ω(cert.CaCert).Should(Equal(test.TestCaCertValue))
		Ω(reflect.DeepEqual(vdbCertCacheOne, vdbCertCacheTwo)).Should(Equal(true))
		cert, _ = vdbCertCacheTwo.ReadCertFromSecret(ctx, TestNMATLSSecret)
		Ω(cert.Key).Should(Equal(test.TestKeyValue))
		Ω(cert.Cert).Should(Equal(test.TestCertValue))
		Ω(cert.CaCert).Should(Equal(test.TestCaCertValue))
		vdbCertCacheTwo.ClearCacheBySecretName(TestNMATLSSecret)
		_, err = vdbCertCacheTwo.ReadCertFromSecret(ctx, TestNMATLSSecret)
		Ω(err).Should(BeNil())

		_, err = vdbCertCacheOne.ReadCertFromSecret(ctx, TestClientServerSecret)
		Ω(err).ShouldNot(BeNil())
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, TestClientServerSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, TestClientServerSecret)
		cert, err = vdbCertCacheOne.ReadCertFromSecret(ctx, TestClientServerSecret)
		Ω(err).Should(BeNil())
		Ω(cert.Key).Should(Equal(test.TestKeyValue))
		Ω(cert.Cert).Should(Equal(test.TestCertValue))
		Ω(cert.CaCert).Should(Equal(test.TestCaCertValue))
		vdbCertCacheTwo.ClearCacheBySecretName(TestClientServerSecret)
		_, err = vdbCertCacheTwo.ReadCertFromSecret(ctx, TestClientServerSecret)
		Ω(err).Should(BeNil())
		vdbCertCacheTwo.SaveCertIntoCache(TestClientServerSecret, secret.Data)
		cert, err = vdbCertCacheOne.ReadCertFromSecret(ctx, TestClientServerSecret)
		Ω(err).Should(BeNil())
		Ω(cert.Key).Should(Equal(test.TestKeyValue))
		Ω(cert.Cert).Should(Equal(test.TestCertValue))
		Ω(cert.CaCert).Should(Equal(test.TestCaCertValue))
		inCache := vdbCertCacheTwo.IsCertInCache(TestClientServerSecret)
		Ω(inCache).Should(Equal(true))
		vdbCertCacheTwo.ClearCacheBySecretName(TestClientServerSecret)
		inCache = vdbCertCacheTwo.IsCertInCache(TestClientServerSecret)
		Ω(inCache).Should(Equal(false))
	})

	var _ = Describe("test vdb_context secret function", func() {
		// ctx := context.Background()

		It("should use VdbContext to get secret", func() {
			/* dispatcher := mockMTLSVClusterOpsDispatcher()
			dispatcher.VDB.Spec.NMATLSSecret = "vdbcontext-get-secret"
			secret := test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
			defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
			dispatcher.VDB.Spec.DBName = TestDBName
			vdbContext := GetContextForVdb(dispatcher.VDB.Namespace, dispatcher.VDB.Name)
			vdbContextStruct := vdbContext.(*VdbContextStruct) // get actual underlying data type
			// use closure to mock secret retrieval
			vdbContextStruct.retrieveSecret = func(s1, s2 string, fetcher cloud.SecretFetcher) (map[string][]byte, error) {
				return secret.Data, nil
			}
			fetcher := cloud.SecretFetcher{
				Client:   dispatcher.Client,
				Log:      dispatcher.Log,
				Obj:      dispatcher.VDB,
				EVWriter: dispatcher.EVWriter,
			}
			cert, err := vdbContext.GetCertFromSecret(dispatcher.VDB.Spec.NMATLSSecret, fetcher)
			Ω(err).Should(BeNil())
			Ω(cert.Key).Should(Equal(string(secret.Data[corev1.TLSPrivateKeyKey])))
			Ω(cert.Cert).Should(Equal(string(secret.Data[corev1.TLSCertKey])))
			Ω(cert.CaCert).Should(Equal(string(secret.Data[paths.HTTPServerCACrtName])))
			Ω(vdbContextStruct.secretMap[dispatcher.VDB.Spec.NMATLSSecret]).Should(Equal(secret.Data)) */
		})

	})

})
