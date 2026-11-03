package services_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("Update archive validation", func() {
	inspect := func(data []byte, currentVersion, expectedDigest string) (models.UpdateImage, error) {
		filename := filepath.Join(GinkgoT().TempDir(), "image.tar")
		Expect(os.WriteFile(filename, data, 0o600)).To(Succeed())
		return services.InspectUpdateArchiveForTest(context.Background(), filename, currentVersion, expectedDigest)
	}

	It("validates all referenced content and returns the manifest digest and version", func() {
		data, digest := test.UpdateArchive("v0.32.0", "v0.30.0", "")
		image, err := inspect(data, "v0.31.0", digest)
		Expect(err).NotTo(HaveOccurred())
		Expect(image).To(Equal(models.UpdateImage{Version: "v0.32.0", Digest: digest}))
	})

	It("validates a skopeo OCI export with published Konflux appliance metadata", func() {
		filename := filepath.Join("testdata", "update", "konflux-oci.tar")
		image, err := services.InspectUpdateArchiveForPlatformForTest(context.Background(), filename, "main-release-0.31-212e974", "", "amd64")
		Expect(err).NotTo(HaveOccurred())
		Expect(image.Version).To(Equal("main-release-0.32-1e77135"))
		Expect(image.Digest).To(MatchRegexp(`^sha256:[a-f0-9]{64}$`))

		_, err = services.InspectUpdateArchiveForPlatformForTest(context.Background(), filename, "main-release-0.30-fffffff", "", "amd64")
		Expect(srvErrors.IsInvalidUpdateImageError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("requires at least main-release-0.31-212e974"))
	})

	DescribeTable("rejects invalid archives", func(kind string) {
		data, _ := test.UpdateArchive("v0.32.0", "v0.30.0", "")
		switch kind {
		case "not tar":
			data = []byte("not an OCI archive")
		case "missing version":
			data, _ = test.UpdateArchive("", "", "")
		case "reserved version":
			data, _ = test.UpdateArchive("..", "", "")
		case "minimum version":
			data, _ = test.UpdateArchive("v0.32.0", "v0.32.0", "")
		case "architecture":
			data, _ = test.UpdateArchive("v0.32.0", "", "unsupported")
		case "unsafe path":
			data, _ = test.UpdateArchive("v0.32.0", "", "", test.TarEntry{Path: "../outside", Content: "bad"})
		case "duplicate":
			data, _ = test.UpdateArchive("v0.32.0", "", "", test.TarEntry{Path: "index.json", Content: "{}"})
		case "corrupt layer":
			data = bytes.Replace(data, []byte("test layer"), []byte("bad! layer"), 1)
		case "missing blob":
			var buffer bytes.Buffer
			reader := tar.NewReader(bytes.NewReader(data))
			writer := tar.NewWriter(&buffer)
			removed := false
			for {
				header, err := reader.Next()
				if err == io.EOF {
					break
				}
				Expect(err).NotTo(HaveOccurred())
				if !removed && strings.HasPrefix(header.Name, "blobs/") {
					removed = true
					continue
				}
				Expect(writer.WriteHeader(header)).To(Succeed())
				_, err = io.Copy(writer, reader)
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(writer.Close()).To(Succeed())
			data = buffer.Bytes()
		case "truncated":
			data = data[:100]
		case "oversized metadata":
			data, _ = test.UpdateArchive(strings.Repeat("v", 1<<20), "", "")
		}
		_, err := inspect(data, "v0.31.0", "")
		Expect(srvErrors.IsInvalidUpdateImageError(err)).To(BeTrue())
	}, Entry("not tar", "not tar"), Entry("missing version", "missing version"),
		Entry("reserved version", "reserved version"), Entry("unsupported minimum", "minimum version"),
		Entry("wrong architecture", "architecture"), Entry("unsafe path", "unsafe path"),
		Entry("duplicate metadata", "duplicate"), Entry("corrupt layer", "corrupt layer"),
		Entry("missing blob", "missing blob"), Entry("truncated archive", "truncated"),
		Entry("oversized metadata", "oversized metadata"))

	It("rejects an expected manifest digest mismatch", func() {
		data, _ := test.UpdateArchive("v0.32.0", "", "")
		_, err := inspect(data, "v0.31.0", "sha256:"+strings.Repeat("0", 64))
		Expect(srvErrors.IsInvalidUpdateImageError(err)).To(BeTrue())
	})

	DescribeTable("enforces the build's minimum upgrade version", func(current, minimum, errorText string) {
		data, _ := test.UpdateArchive("candidate", minimum, "")
		_, err := inspect(data, current, "")
		if errorText == "" {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(srvErrors.IsInvalidUpdateImageError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(errorText))
		}
	},
		Entry("optional minimum absent", "DEV", "", ""),
		Entry("production at minimum", "v0.31.0", "v0.31.0", ""),
		Entry("production newer", "v0.31.1", "v0.31.0", ""),
		Entry("production older", "v0.30.9", "v0.31.0", "requires at least"),
		Entry("production numeric ordering", "v0.10.0", "v0.9.0", ""),
		Entry("dev next release", "main-release-0.32-1e77135", "main-release-0.31-212e974", ""),
		Entry("stable next release", "stable-release-0.31-a29d6d3", "stable-release-0.30-722023a", ""),
		Entry("dev older", "main-release-0.30-fffffff", "main-release-0.31-212e974", "requires at least"),
		Entry("stable older", "stable-release-0.29-fffffff", "stable-release-0.30-722023a", "requires at least"),
		Entry("dev at boundary", "main-release-0.31-212e974", "main-release-0.31-212e974", ""),
		Entry("stable at boundary", "stable-release-0.30-722023a", "stable-release-0.30-722023a", ""),
		Entry("matching abbreviated commit", "main-release-0.31-212e974abcdef01", "main-release-0.31-212e974", ""),
		Entry("matching full minimum commit", "main-release-0.31-212e974", "main-release-0.31-212e974abcdef01", ""),
		Entry("dev same release different SHA", "main-release-0.31-fffffff", "main-release-0.31-212e974", ""),
		Entry("stable same release different SHA", "stable-release-0.30-0000000", "stable-release-0.30-722023a", ""),
		Entry("dev numeric ordering", "main-release-0.10-aaaaaaa", "main-release-0.9-bbbbbbb", ""),
		Entry("stable major ordering", "stable-release-1.0-aaaaaaa", "stable-release-0.99-bbbbbbb", ""),
		Entry("cross channel", "main-release-0.32-1e77135", "stable-release-0.30-722023a", "same release channel"),
		Entry("cross channel reversed", "stable-release-0.32-1e77135", "main-release-0.30-722023a", "same release channel"),
		Entry("dev against production", "main-release-0.32-1e77135", "v0.30.0", "expected a production"),
		Entry("production against stable", "v1.0.0", "stable-release-0.30-722023a", "same release channel"),
		Entry("local build", "DEV", "main-release-0.31-212e974", "same release channel"),
		Entry("PR build", "on-pr-212e974", "main-release-0.31-212e974", "same release channel"),
		Entry("unknown running version", "DEV", "v0.30.0", "expected a production"),
		Entry("malformed minimum", "v0.31.0", "garbage", "invalid min-upgrade-version"),
		Entry("production leading zero", "v0.31.0", "v0.031.0", "invalid min-upgrade-version"),
		Entry("branch leading zero", "main-release-0.32-1e77135", "main-release-0.031-212e974", "invalid min-upgrade-version"),
		Entry("short SHA", "main-release-0.32-1e77135", "main-release-0.31-212e97", "invalid min-upgrade-version"),
		Entry("uppercase SHA", "main-release-0.32-1e77135", "main-release-0.31-212E974", "invalid min-upgrade-version"),
		Entry("prerelease minimum", "v0.31.0", "v0.30.0-rc.1", "invalid min-upgrade-version"),
	)
})
