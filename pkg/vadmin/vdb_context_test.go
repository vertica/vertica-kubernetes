package vadmin

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("vdb_context", func() {
	var _ = context.Background()
	It("should return same pointer to context struct", func() {

		vdbContext := GetContextForVdb("default", "test-vdb")
		Expect(vdbContext).ShouldNot(Equal(nil))
		vdbContextOne := GetContextForVdb("default", "test-vdb")
		vdbContextOne.SetBoolValue(UseTlsCert, true)
		vdbContextTwo := GetContextForVdb("default", "test-vdb")
		Expect(vdbContextTwo.GetBoolValue(UseTlsCert)).Should(Equal(true))
		vdbContextOne.SetBoolValue(UseTlsCert, false)
		Expect(vdbContextTwo.GetBoolValue(UseTlsCert)).Should(Equal(false))

	})

})
