package vmware

import "testing"

func TestNormalizeAndValidateURL(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeAndValidateURL(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
