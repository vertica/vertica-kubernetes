package vadmin

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("vdb_context", func() {
	var _ = context.Background()
	It("should return same pointer to context struct", func() {

		vdbContext := GetContextForVdb("default", "test-vdb")
		Expect(vdbContext).ShouldNot(Equal(nil))
		vdbContextOne := GetContextForVdb("default", "test-vdb")
		vdbContextOne.SetBoolValue(UseTLSCert, true)
		vdbContextTwo := GetContextForVdb("default", "test-vdb")
		Expect(vdbContextTwo.GetBoolValue(UseTLSCert)).Should(Equal(true))
		vdbContextOne.SetBoolValue(UseTLSCert, false)
		Expect(vdbContextTwo.GetBoolValue(UseTLSCert)).Should(Equal(false))

	})

	var _ = Describe("test vdb_context secret function", func() {
		ctx := context.Background()

		It("should use VdbContext to get secret", func() {
			dispatcher := mockMTLSVClusterOpsDispatcher()
			dispatcher.VDB.Spec.NMATLSSecret = "vdbcontext-get-secret"
			secret := test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
			defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
			dispatcher.VDB.Spec.DBName = TestDBName
			vdbContext := GetContextForVdb(dispatcher.VDB.Namespace, dispatcher.VDB.Name)
			vdbContextStruct := vdbContext.(*VdbContextStruct) // get actual underlying data type
			// use closure to mock secret retrieval
			vdbContextStruct.retrieveSecret = func(s1, s2 string) (map[string][]byte, error) {
				return secret.Data, nil
			}
			cert, err := vdbContext.GetCertFromSecret(dispatcher.VDB.Spec.NMATLSSecret)
			Ω(err).Should(BeNil())
			Ω(cert.Key).Should(Equal(string(secret.Data[corev1.TLSPrivateKeyKey])))
			Ω(cert.Cert).Should(Equal(string(secret.Data[corev1.TLSCertKey])))
			Ω(cert.CaCert).Should(Equal(string(secret.Data[paths.HTTPServerCACrtName])))
			Ω(vdbContextStruct.secretMap[dispatcher.VDB.Spec.NMATLSSecret]).Should(Equal(secret.Data))
		})

	})

})
