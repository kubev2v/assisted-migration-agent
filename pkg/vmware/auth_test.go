package vmware

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Normalize And Validate URL", func() {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		// accepted
		{"host appends sdk", "https://vcenter.example.com", "https://vcenter.example.com/sdk", false},
		{"host with sdk kept", "https://vcenter.example.com/sdk", "https://vcenter.example.com/sdk", false},
		{"public ip literal", "https://203.0.113.10/sdk", "https://203.0.113.10/sdk", false},
		{"trailing slash", "https://vcenter.example.com/", "https://vcenter.example.com/sdk", false},

		// rejected — SSRF guard
		{"http scheme", "http://vcenter.example.com/sdk", "", true},
		{"file scheme", "file:///etc/passwd", "", true},
		{"empty host", "https:///sdk", "", true},
		{"loopback v4", "https://127.0.0.1/sdk", "", true},
		{"loopback v6", "https://[::1]/sdk", "", true},
		{"link-local metadata", "https://169.254.169.254/sdk", "", true},
		{"link-local v6", "https://[fe80::1]/sdk", "", true},
		{"unspecified v4", "https://0.0.0.0/sdk", "", true},
		{"unspecified v6", "https://[::]/sdk", "", true},
	}

	for _, tt := range tests {
		It(tt.name, func() {
			got, err := NormalizeAndValidateURL(tt.in)
			if tt.wantErr {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(err).To(Succeed())
			Expect(got).To(Equal(tt.want))
		})
	}
})
