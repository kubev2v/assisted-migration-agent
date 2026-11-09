package converters_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConverters(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Converters Suite")
}
