package accountclaim_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestAccountclaim(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Accountclaim Suite")
}
