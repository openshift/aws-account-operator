package awsclient_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestAwsclient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "awsclient Suite")
}
