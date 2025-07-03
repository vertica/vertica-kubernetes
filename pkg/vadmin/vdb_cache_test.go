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
		InitCertCacheForVdb("default", "test-vdb", fetcher)
		vdbCertCache := GetCertCacheForVdb("default", "test-vdb")
		Expect(vdbCertCache).ShouldNot(Equal(nil))
		vdbCertCacheOne := GetCertCacheForVdb("default", "test-vdb")
		vdbCertCacheTwo := GetCertCacheForVdb("default", "test-vdb")
		Expect(vdbCertCacheTwo).Should(Equal(vdbCertCacheOne))

		cert, err := vdbCertCacheOne.ReadCertFromSecret(ctx, TestNMATLSSecret)
		Ω(err).Should(BeNil())
		Ω(cert.Key).Should(Equal(test.TestKeyValue))
		Ω(cert.Cert).Should(Equal(test.TestCertValue))
		Ω(cert.CaCert).Should(Equal(test.TestCaCertValue))
		Ω(reflect.DeepEqual(vdbCertCacheOne, vdbCertCacheTwo)).Should(Equal(true))
		cert, err = vdbCertCacheOne.ReadCertFromSecret(ctx, TestNMATLSSecret)
		Ω(cert.Key).Should(Equal(test.TestKeyValue))
		Ω(cert.Cert).Should(Equal(test.TestCertValue))
		Ω(cert.CaCert).Should(Equal(test.TestCaCertValue))
		vdbCertCacheTwo.ClearCacheBySecretName(TestNMATLSSecret)
		cert, err = vdbCertCacheOne.ReadCertFromSecret(ctx, TestNMATLSSecret)
		Ω(err).ShouldNot(BeNil())
		cert, err = vdbCertCacheOne.ReadCertFromSecret(ctx, TestClientServerSecret)
		Ω(err).Should(BeNil())
		Ω(cert.Key).Should(Equal(test.TestKeyValue))
		Ω(cert.Cert).Should(Equal(test.TestCertValue))
		Ω(cert.CaCert).Should(Equal(test.TestCaCertValue))
		vdbCertCacheTwo.ClearCacheBySecretName(TestClientServerSecret)
		cert, err = vdbCertCacheOne.ReadCertFromSecret(ctx, TestClientServerSecret)
		Ω(err).ShouldNot(BeNil())
		vdbCertCacheTwo.SaveCertIntoCache(TestClientServerSecret, secret.Data)
		cert, err = vdbCertCacheOne.ReadCertFromSecret(ctx, TestClientServerSecret)
		Ω(err).Should(BeNil())
		Ω(cert.Key).Should(Equal(test.TestKeyValue))
		Ω(cert.Cert).Should(Equal(test.TestCertValue))
		Ω(cert.CaCert).Should(Equal(test.TestCaCertValue))

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
