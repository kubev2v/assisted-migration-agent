package services_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("Image upgrade archive validation", func() {
	inspect := func(data []byte, currentVersion string) (models.ImageUpgradeImage, error) {
		filename := filepath.Join(GinkgoT().TempDir(), "image.tar")
		Expect(os.WriteFile(filename, data, 0o600)).To(Succeed())
		return services.InspectImageUpgradeArchiveForTest(context.Background(), filename, currentVersion)
	}

	It("validates all referenced content and returns the manifest digest and version", func() {
		data, digest := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		image, err := inspect(data, "v0.31.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(image).To(Equal(models.ImageUpgradeImage{Version: "v0.32.0", Digest: digest}))
	})

	It("validates a skopeo OCI export with published Konflux appliance metadata", func() {
		filename := filepath.Join("testdata", "image_upgrade", "konflux-oci.tar")
		image, err := services.InspectImageUpgradeArchiveForPlatformForTest(context.Background(), filename, "main-release-0.31-212e974", "amd64")
		Expect(err).NotTo(HaveOccurred())
		Expect(image.Version).To(Equal("main-release-0.32-1e77135"))
		Expect(image.Digest).To(MatchRegexp(`^sha256:[a-f0-9]{64}$`))

		_, err = services.InspectImageUpgradeArchiveForPlatformForTest(context.Background(), filename, "main-release-0.30-fffffff", "amd64")
		Expect(srvErrors.IsInvalidImageUpgradeImageError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("requires at least main-release-0.31-212e974"))
	})

	DescribeTable("rejects invalid image rootfs configurations", func(rootFS v1.RootFS, message string) {
		data, _ := test.ImageUpgradeArchiveWithRootFS("v0.32.0", "v0.30.0", "", rootFS)
		_, err := inspect(data, "v0.31.0")
		Expect(srvErrors.IsInvalidImageUpgradeImageError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring(message))
	},
		Entry("missing rootfs type", v1.RootFS{DiffIDs: []digest.Digest{digest.FromString("test layer")}}, "unsupported image rootfs type"),
		Entry("unsupported rootfs type", v1.RootFS{Type: "unknown", DiffIDs: []digest.Digest{digest.FromString("test layer")}}, "unsupported image rootfs type"),
		Entry("missing diff IDs", v1.RootFS{Type: "layers"}, "missing image rootfs diff IDs"),
		Entry("too few diff IDs", v1.RootFS{Type: "layers", DiffIDs: []digest.Digest{}}, "diff ID count does not match layer count"),
		Entry("too many diff IDs", v1.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.FromString("first"), digest.FromString("second")}}, "diff ID count does not match layer count"),
		Entry("invalid digest format", v1.RootFS{Type: "layers", DiffIDs: []digest.Digest{"not-a-digest"}}, "invalid image rootfs diff ID"),
		Entry("invalid digest length", v1.RootFS{Type: "layers", DiffIDs: []digest.Digest{"sha256:abcd"}}, "invalid image rootfs diff ID"),
		Entry("invalid digest encoding", v1.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.Digest("sha256:" + strings.Repeat("z", 64))}}, "invalid image rootfs diff ID"),
	)

	It("does not compare rootfs diff IDs against stored layer digests", func() {
		data, expectedDigest := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		image, err := inspect(data, "v0.31.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(image).To(Equal(models.ImageUpgradeImage{Version: "v0.32.0", Digest: expectedDigest}))
	})

	DescribeTable("rejects sparse PAX entries before expanding their logical content", func(version string) {
		filename := filepath.Join("testdata", "image_upgrade", "sparse-pax-"+version+".tar")
		f, err := os.Open(filename)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(f.Close)
		reader := tar.NewReader(f)
		header, err := reader.Next()
		Expect(err).NotTo(HaveOccurred())
		Expect(header.Typeflag).To(Equal(byte(tar.TypeReg)))
		Expect(header.Size).To(Equal(int64(1 << 50)))

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err = services.InspectImageUpgradeArchiveForTest(ctx, filename, "v0.31.0")
		Expect(srvErrors.IsInvalidImageUpgradeImageError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("sparse archive entries are not supported"))
	},
		Entry("GNU PAX 0.1", "0.1"),
		Entry("GNU PAX 1.0", "1.0"),
	)

	DescribeTable("rejects invalid archives", func(kind string) {
		data, _ := test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		switch kind {
		case "not tar":
			data = []byte("not an OCI archive")
		case "missing version":
			data, _ = test.ImageUpgradeArchive("", "", "")
		case "reserved version":
			data, _ = test.ImageUpgradeArchive("..", "", "")
		case "minimum version":
			data, _ = test.ImageUpgradeArchive("v0.32.0", "v0.32.0", "")
		case "architecture":
			data, _ = test.ImageUpgradeArchive("v0.32.0", "", "unsupported")
		case "unsafe path":
			data, _ = test.ImageUpgradeArchive("v0.32.0", "", "", test.TarEntry{Path: "../outside", Content: "bad"})
		case "duplicate":
			data, _ = test.ImageUpgradeArchive("v0.32.0", "", "", test.TarEntry{Path: "index.json", Content: "{}"})
		case "corrupt layer":
			offset := bytes.Index(data, []byte{0x1f, 0x8b, 0x08})
			Expect(offset).To(BeNumerically(">=", 0))
			data[offset+10] ^= 1
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
			data, _ = test.ImageUpgradeArchive(strings.Repeat("v", 1<<20), "", "")
		}
		_, err := inspect(data, "v0.31.0")
		Expect(srvErrors.IsInvalidImageUpgradeImageError(err)).To(BeTrue())
	}, Entry("not tar", "not tar"), Entry("missing version", "missing version"),
		Entry("reserved version", "reserved version"), Entry("unsupported minimum", "minimum version"),
		Entry("wrong architecture", "architecture"), Entry("unsafe path", "unsafe path"),
		Entry("duplicate metadata", "duplicate"), Entry("corrupt layer", "corrupt layer"),
		Entry("missing blob", "missing blob"), Entry("truncated archive", "truncated"),
		Entry("oversized metadata", "oversized metadata"))

	DescribeTable("enforces the build's minimum upgrade version", func(current, minimum, errorText string) {
		data, _ := test.ImageUpgradeArchive("candidate", minimum, "")
		_, err := inspect(data, current)
		if errorText == "" {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(srvErrors.IsInvalidImageUpgradeImageError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(errorText))
		}
	},
		Entry("minimum absent", "v0.31.0", "", "missing io.kubev2v.oma.min-upgrade-version"),
		Entry("both versions absent", "", "", "missing io.kubev2v.oma.min-upgrade-version"),
		Entry("production at minimum", "v0.31.0", "v0.31.0", ""),
		Entry("production at minimum patch", "v0.31.5", "v0.31.5", ""),
		Entry("production newer", "v0.31.1", "v0.31.0", ""),
		Entry("production older patch", "v0.31.4", "v0.31.5", "requires at least"),
		Entry("production older", "v0.30.9", "v0.31.0", "requires at least"),
		Entry("production newer minor resets patch", "v0.32.0", "v0.31.99", ""),
		Entry("production newer major resets minor", "v1.0.0", "v0.99.99", ""),
		Entry("production older major with newer minor", "v0.99.99", "v1.0.0", "requires at least"),
		Entry("production numeric ordering", "v0.10.0", "v0.9.0", ""),
		Entry("dev next release", "main-release-0.32-1e77135", "main-release-0.31-212e974", ""),
		Entry("stable next release", "stable-release-0.31-a29d6d3", "stable-release-0.30-722023a", ""),
		Entry("dev older", "main-release-0.30-fffffff", "main-release-0.31-212e974", "requires at least"),
		Entry("stable older", "stable-release-0.29-fffffff", "stable-release-0.30-722023a", "requires at least"),
		Entry("dev at boundary", "main-release-0.31-212e974", "main-release-0.31-212e974", ""),
		Entry("stable at boundary", "stable-release-0.30-722023a", "stable-release-0.30-722023a", ""),
		Entry("long running SHA", "main-release-0.31-212e974abcdef01", "main-release-0.31-212e974", "invalid running appliance version"),
		Entry("long minimum SHA", "main-release-0.31-212e974", "main-release-0.31-212e974abcdef01", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("dev same release different SHA", "main-release-0.31-fffffff", "main-release-0.31-212e974", ""),
		Entry("stable same release different SHA", "stable-release-0.30-0000000", "stable-release-0.30-722023a", ""),
		Entry("dev numeric ordering", "main-release-0.10-aaaaaaa", "main-release-0.9-bbbbbbb", ""),
		Entry("stable major ordering", "stable-release-1.0-aaaaaaa", "stable-release-0.99-bbbbbbb", ""),
		Entry("cross channel", "main-release-0.32-1e77135", "stable-release-0.30-722023a", ""),
		Entry("cross channel reversed", "stable-release-0.32-1e77135", "main-release-0.30-722023a", ""),
		Entry("cross channel equality", "main-release-0.31-1e77135", "stable-release-0.31-722023a", ""),
		Entry("cross channel older minor", "main-release-0.30-1e77135", "stable-release-0.31-722023a", "requires at least"),
		Entry("cross channel newer major resets minor", "main-release-1.0-1e77135", "stable-release-0.99-722023a", ""),
		Entry("dev against production", "main-release-0.32-1e77135", "v0.30.0", ""),
		Entry("dev against production ignores minimum patch", "main-release-0.31-1e77135", "v0.31.5", ""),
		Entry("stable against production ignores minimum patch", "stable-release-0.31-722023a", "v0.31.5", ""),
		Entry("dev against production older minor", "main-release-0.30-1e77135", "v0.31.5", "requires at least"),
		Entry("stable against production older minor", "stable-release-0.30-722023a", "v0.31.5", "requires at least"),
		Entry("dev against production newer major resets minor", "main-release-1.0-1e77135", "v0.99.5", ""),
		Entry("stable against production older major with newer minor", "stable-release-0.99-722023a", "v1.0.0", "requires at least"),
		Entry("production against stable", "v1.0.0", "stable-release-0.30-722023a", ""),
		Entry("production against dev equality", "v0.31.0", "main-release-0.31-1e77135", ""),
		Entry("production against stable equality", "v0.31.0", "stable-release-0.31-722023a", ""),
		Entry("production against dev older minor", "v0.30.99", "main-release-0.31-1e77135", "requires at least"),
		Entry("mixed channel numeric ordering", "main-release-0.10-1e77135", "v0.9.99", ""),
		Entry("mixed channel leading zeros", "main-release-01.00-1e77135", "v0.099.5", ""),
		Entry("local build", "DEV", "main-release-0.31-212e974", "invalid running appliance version"),
		Entry("PR build", "on-pr-212e974", "main-release-0.31-212e974", "invalid running appliance version"),
		Entry("unknown running version", "DEV", "v0.30.0", "invalid running appliance version"),
		Entry("malformed minimum", "v0.31.0", "garbage", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("production leading zero", "v0.31.0", "v0.031.0", ""),
		Entry("production leading zero older", "v0.030.9", "v0.31.0", "requires at least"),
		Entry("branch leading zero", "main-release-0.32-1e77135", "main-release-0.031-212e974", ""),
		Entry("short SHA", "main-release-0.32-1e77135", "main-release-0.31-212e97", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("uppercase SHA", "main-release-0.32-1e77135", "main-release-0.31-212E974", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("empty running version", "", "v0.30.0", "invalid running appliance version"),
		Entry("unknown literal", "unknown", "v0.30.0", "invalid running appliance version"),
		Entry("legacy running version", "v0.0.0", "v0.30.0", "invalid running appliance version"),
		Entry("legacy minimum", "v0.31.0", "v0.0.0", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("PR minimum", "v0.31.0", "on-pr-212e974", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("PR pair", "on-pr-212e974", "on-pr-212e974", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("dev minimum", "v0.31.0", "DEV", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("unknown minimum", "v0.31.0", "unknown", "invalid io.kubev2v.oma.min-upgrade-version"),
		Entry("malformed running version", "garbage", "v0.30.0", "invalid running appliance version"),
		Entry("short running SHA", "main-release-0.31-212e97", "main-release-0.31-212e974", "invalid running appliance version"),
		Entry("uppercase running SHA", "main-release-0.31-212E974", "main-release-0.31-212e974", "invalid running appliance version"),
		Entry("prerelease minimum", "v0.31.0", "v0.30.0-rc.1", "invalid io.kubev2v.oma.min-upgrade-version"),
	)
})
