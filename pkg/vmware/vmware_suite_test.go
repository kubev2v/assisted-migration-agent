package vmware

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestVMware(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VMware Suite")
}
