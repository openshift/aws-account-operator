package credentialwatcher_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/openshift/aws-account-operator/pkg/credentialwatcher"
)

var _ = Describe("Credentialwatcher", func() {
	Context("Tests the Credential Timer Fuzz functionality", func() {
		It("Should always return a number between or equal to the limits", func() {
			var i int64
			var min int64 = 5
			var max int64 = 30
			test_min := int(min * 60)
			test_max := int(max * 60)
			for i = 0; i <= 100; i++ {
				j := GetFuzzLength(i, min, max)
				Expect(j).To(BeNumerically(">=", test_min))
				Expect(j).To(BeNumerically("<=", test_max))
			}
		})
	})
})
