package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("eventtrigger_webhook", func() {
	// validate VerticaDB spec values
	It("should succeed with all valid fields", func() {
		et := MakeET()
		Expect(et.ValidateCreate()).Should(Succeed())
	})

	It("should fail if reference object type is not VerticaDB", func() {
		et := MakeET()
		et.Spec.References[0].Object.Kind = "Pod"
		Expect(et.ValidateCreate()).ShouldNot(Succeed())
	})

	It("should fail if reference object apiVersion is not known", func() {
		et := MakeET()
		et.Spec.References[0].Object.APIVersion = "version"
		Expect(et.ValidateCreate()).ShouldNot(Succeed())
	})
})
