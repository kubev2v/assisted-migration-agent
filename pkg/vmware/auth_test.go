package vmware

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("Normalize And Validate URL",
	func(input, want string, wantErr bool) {
		got, err := NormalizeAndValidateURL(input)
		if wantErr {
			Expect(err).To(HaveOccurred())
			return
		}

		Expect(err).To(Succeed())
		Expect(got).To(Equal(want))
	},
	Entry("host appends sdk", "https://vcenter.example.com", "https://vcenter.example.com/sdk", false),

	Entry("host with sdk kept", "https://vcenter.example.com/sdk", "https://vcenter.example.com/sdk", false),

	Entry("public ip literal", "https://203.0.113.10/sdk", "https://203.0.113.10/sdk", false),

	Entry("trailing slash", "https://vcenter.example.com/", "https://vcenter.example.com/sdk", false),

	Entry("http scheme", "http://vcenter.example.com/sdk", "", true),

	Entry("file scheme", "file:///etc/passwd", "", true),

	Entry("empty host", "https:///sdk", "", true),

	Entry("loopback v4", "https://127.0.0.1/sdk", "", true),

	Entry("loopback v6", "https://[::1]/sdk", "", true),

	Entry("link-local metadata", "https://169.254.169.254/sdk", "", true),

	Entry("link-local v6", "https://[fe80::1]/sdk", "", true),

	Entry("unspecified v4", "https://0.0.0.0/sdk", "", true),

	Entry("unspecified v6", "https://[::]/sdk", "", true),
)
