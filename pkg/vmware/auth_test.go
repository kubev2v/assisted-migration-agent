package vmware

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NormalizeAndValidateURL", func() {
	DescribeTable("normalizes allowed URLs",
		func(input, expected string) {
			result, err := NormalizeAndValidateURL(input)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(expected))
		},
		Entry("appends sdk to the host", "https://vcenter.example.com", "https://vcenter.example.com/sdk"),
		Entry("preserves the sdk path", "https://vcenter.example.com/sdk", "https://vcenter.example.com/sdk"),
		Entry("accepts a public IP literal", "https://203.0.113.10/sdk", "https://203.0.113.10/sdk"),
		Entry("replaces a trailing slash", "https://vcenter.example.com/", "https://vcenter.example.com/sdk"),
	)

	DescribeTable("rejects disallowed URLs",
		func(input string) {
			_, err := NormalizeAndValidateURL(input)
			Expect(err).To(HaveOccurred())
		},
		Entry("HTTP scheme", "http://vcenter.example.com/sdk"),
		Entry("file scheme", "file:///etc/passwd"),
		Entry("empty host", "https:///sdk"),
		Entry("IPv4 loopback", "https://127.0.0.1/sdk"),
		Entry("IPv6 loopback", "https://[::1]/sdk"),
		Entry("link-local metadata address", "https://169.254.169.254/sdk"),
		Entry("IPv6 link-local address", "https://[fe80::1]/sdk"),
		Entry("unspecified IPv4 address", "https://0.0.0.0/sdk"),
		Entry("unspecified IPv6 address", "https://[::]/sdk"),
	)
})
