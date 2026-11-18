package sizer

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSizer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sizer Suite")
}
