package credentialwatcher_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestCredentialwatcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Credentialwatcher Suite")
}
