package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("eventtrigger_webhook", func() {
	// validate VerticaDB spec values
	It("should succeed with all valid fields", func() {
		et := MakeET()
		_, err := et.ValidateCreate()
		Expect(err).Should(Succeed())
		_, err = et.ValidateUpdate(et)
		Expect(err).Should(Succeed())
	})

	It("should fail if reference object type is not VerticaDB", func() {
		et := MakeET()
		et.Spec.References[0].Object.Kind = "Pod"
		_, err := et.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
		_, err = et.ValidateUpdate(et)
		Expect(err).ShouldNot(Succeed())
	})

	It("should fail on multiple reference objects", func() {
		et := MakeET()
		name := MakeVDBName().Name
		ref := ETReference{
			Object: &ETRefObject{
				APIVersion: GroupVersion.String(),
				Kind:       VerticaDBKind,
				Name:       name,
			},
		}

		et.Spec.References = append(et.Spec.References, ref)

		_, err := et.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
		_, err = et.ValidateUpdate(et)
		Expect(err).ShouldNot(Succeed())
	})

	It("should fail on multiple matches conditions", func() {
		et := MakeET()
		match := ETMatch{
			Condition: &ETCondition{
				Type:   string(DBInitialized),
				Status: corev1.ConditionTrue,
			},
		}
		et.Spec.Matches = append(et.Spec.Matches, match)

		_, err := et.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
		_, err = et.ValidateUpdate(et)
		Expect(err).ShouldNot(Succeed())
	})

	It("should fail if job name is not specified", func() {
		et := MakeET()
		et.Spec.Template.Metadata.Name = ""
		et.Spec.Template.Metadata.GenerateName = ""
		_, err := et.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
		et.Spec.Template.Metadata.GenerateName = "job1-"
		_, err = et.ValidateCreate()
		Expect(err).Should(Succeed())
		et.Spec.Template.Metadata.Name = "job1"
		et.Spec.Template.Metadata.GenerateName = ""
		_, err = et.ValidateCreate()
		Expect(err).Should(Succeed())
	})
})
