package account

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAccount(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Account Suite")
}
