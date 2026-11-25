package services_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("Image upgrade layer validation", func() {
	var layer, compressed, zstandard []byte
	BeforeEach(func() {
		compressed = test.BuildTarGz(test.TarEntry{Path: "fixture.txt", Content: "layer payload"})
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		Expect(err).NotTo(HaveOccurred())
		layer, err = io.ReadAll(reader)
		Expect(err).NotTo(HaveOccurred())
		Expect(reader.Close()).To(Succeed())
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
		Expect(err).NotTo(HaveOccurred())
		zstandard = encoder.EncodeAll(layer, nil)
		Expect(encoder.Close()).To(Succeed())
	})

	DescribeTable("accepts valid layers with their declared compression", func(mediaType string) {
		data := layer
		switch mediaType {
		case v1.MediaTypeImageLayerGzip:
			data = compressed
		case v1.MediaTypeImageLayerZstd:
			data = zstandard
		}
		rootFS := v1.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.FromBytes(layer)}}
		archive, expectedDigest := test.ImageUpgradeArchiveWithLayers("v0.32.0", "v0.30.0", "", rootFS, []test.ImageUpgradeLayer{{Data: data, MediaType: mediaType}})
		filename := filepath.Join(GinkgoT().TempDir(), "image.tar")
		Expect(os.WriteFile(filename, archive, 0o600)).To(Succeed())
		image, err := services.InspectImageUpgradeArchiveForTest(context.Background(), filename, "v0.31.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(image.Digest).To(Equal(expectedDigest))
	},
		Entry("tar", v1.MediaTypeImageLayer),
		Entry("gzip", v1.MediaTypeImageLayerGzip),
		Entry("zstd", v1.MediaTypeImageLayerZstd),
	)

	DescribeTable("rejects malformed layers even when their blob digest matches", func(kind, mediaType string) {
		data := layer
		switch kind {
		case "garbage":
			data = []byte("test layer")
		case "compressed garbage":
			var buffer bytes.Buffer
			writer := gzip.NewWriter(&buffer)
			_, err := writer.Write([]byte("test layer"))
			Expect(err).NotTo(HaveOccurred())
			Expect(writer.Close()).To(Succeed())
			data = buffer.Bytes()
		case "gzip instead of tar", "gzip instead of zstd":
			data = compressed
		case "zstd instead of gzip":
			data = zstandard
		case "truncated tar":
			data = layer[:700]
		case "missing tar terminator":
			data = layer[:len(layer)-1024]
		case "partial tar terminator":
			data = layer[:len(layer)-512]
		case "truncated gzip":
			data = compressed[:len(compressed)-4]
		case "bad gzip checksum":
			data = bytes.Clone(compressed)
			data[len(data)-8] ^= 1
		case "truncated zstd":
			data = zstandard[:len(zstandard)-2]
		case "bad zstd checksum":
			data = bytes.Clone(zstandard)
			data[len(data)-1] ^= 1
		case "trailing garbage":
			data = append(bytes.Clone(layer), []byte("garbage")...)
		case "sparse PAX 0.1", "sparse PAX 1.0":
			version := "0.1"
			if kind == "sparse PAX 1.0" {
				version = "1.0"
			}
			var err error
			data, err = os.ReadFile(filepath.Join("testdata", "image_upgrade", "sparse-pax-"+version+".tar"))
			Expect(err).NotTo(HaveOccurred())
		}
		rootFS := v1.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.FromBytes(layer)}}
		archive, _ := test.ImageUpgradeArchiveWithLayers("v0.32.0", "v0.30.0", "", rootFS, []test.ImageUpgradeLayer{{Data: data, MediaType: mediaType}})
		filename := filepath.Join(GinkgoT().TempDir(), "image.tar")
		Expect(os.WriteFile(filename, archive, 0o600)).To(Succeed())
		_, err := services.InspectImageUpgradeArchiveForTest(context.Background(), filename, "v0.31.0")
		Expect(srvErrors.IsInvalidImageUpgradeImageError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("layer"))
	},
		Entry("non-tar bytes", "garbage", v1.MediaTypeImageLayer),
		Entry("non-gzip bytes", "garbage", v1.MediaTypeImageLayerGzip),
		Entry("non-zstd bytes", "garbage", v1.MediaTypeImageLayerZstd),
		Entry("gzip containing non-tar bytes", "compressed garbage", v1.MediaTypeImageLayerGzip),
		Entry("tar declared as gzip", "tar instead of gzip", v1.MediaTypeImageLayerGzip),
		Entry("gzip declared as tar", "gzip instead of tar", v1.MediaTypeImageLayer),
		Entry("gzip declared as zstd", "gzip instead of zstd", v1.MediaTypeImageLayerZstd),
		Entry("zstd declared as gzip", "zstd instead of gzip", v1.MediaTypeImageLayerGzip),
		Entry("truncated tar", "truncated tar", v1.MediaTypeImageLayer),
		Entry("missing tar terminator", "missing tar terminator", v1.MediaTypeImageLayer),
		Entry("partial tar terminator", "partial tar terminator", v1.MediaTypeImageLayer),
		Entry("truncated gzip trailer", "truncated gzip", v1.MediaTypeImageLayerGzip),
		Entry("bad gzip checksum", "bad gzip checksum", v1.MediaTypeImageLayerGzip),
		Entry("truncated zstd trailer", "truncated zstd", v1.MediaTypeImageLayerZstd),
		Entry("bad zstd checksum", "bad zstd checksum", v1.MediaTypeImageLayerZstd),
		Entry("trailing garbage", "trailing garbage", v1.MediaTypeImageLayer),
		Entry("sparse PAX 0.1", "sparse PAX 0.1", v1.MediaTypeImageLayer),
		Entry("sparse PAX 1.0", "sparse PAX 1.0", v1.MediaTypeImageLayer),
	)

	DescribeTable("bounds expanded bytes across layers", func(mediaType string) {
		data := layer
		switch mediaType {
		case v1.MediaTypeImageLayerGzip:
			data = compressed
		case v1.MediaTypeImageLayerZstd:
			data = zstandard
		}
		readers := []io.Reader{bytes.NewReader(data), bytes.NewReader(data)}
		err := services.ValidateImageUpgradeLayersForTest(context.Background(), readers, mediaType, int64(2*len(layer)-1), 2)
		Expect(err).To(MatchError(ContainSubstring("exceeds size limit")))
		readers = []io.Reader{bytes.NewReader(data), bytes.NewReader(data)}
		Expect(services.ValidateImageUpgradeLayersForTest(context.Background(), readers, mediaType, int64(2*len(layer)), 2)).To(Succeed())
	},
		Entry("tar", v1.MediaTypeImageLayer),
		Entry("gzip", v1.MediaTypeImageLayerGzip),
		Entry("zstd", v1.MediaTypeImageLayerZstd),
	)

	It("bounds entries across layers", func() {
		readers := []io.Reader{bytes.NewReader(layer), bytes.NewReader(layer)}
		err := services.ValidateImageUpgradeLayersForTest(context.Background(), readers, v1.MediaTypeImageLayer, int64(2*len(layer)), 1)
		Expect(err).To(MatchError(ContainSubstring("too many layer entries")))
	})

	It("rejects oversized zstd windows before decoding their contents", func() {
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithSingleSegment(false))
		Expect(err).NotTo(HaveOccurred())
		data := encoder.EncodeAll(layer, nil)
		Expect(encoder.Close()).To(Succeed())
		// Advertise a 128 MiB window without allocating a large fixture.
		data[5] = (27 - 10) << 3
		var header zstd.Header
		Expect(header.Decode(data)).To(Succeed())
		Expect(header.SingleSegment).To(BeFalse())
		Expect(header.WindowSize).To(Equal(uint64(128 << 20)))
		err = services.ValidateImageUpgradeLayersForTest(context.Background(), []io.Reader{bytes.NewReader(data)}, v1.MediaTypeImageLayerZstd, int64(len(layer)), 1)
		Expect(err).To(HaveOccurred())
	})

	DescribeTable("stops reading a layer when cancelled", func(mediaType string) {
		data := layer
		switch mediaType {
		case v1.MediaTypeImageLayerGzip:
			data = compressed
		case v1.MediaTypeImageLayerZstd:
			data = zstandard
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reader := &cancellingImageUpgradeLayerReader{reader: bytes.NewReader(data), cancel: cancel}
		err := services.ValidateImageUpgradeLayersForTest(ctx, []io.Reader{reader}, mediaType, int64(len(layer)), 1)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	},
		Entry("tar", v1.MediaTypeImageLayer),
		Entry("gzip", v1.MediaTypeImageLayerGzip),
		Entry("zstd", v1.MediaTypeImageLayerZstd),
	)

	It("accepts filesystem links and device entries", func() {
		var buffer bytes.Buffer
		writer := tar.NewWriter(&buffer)
		for _, header := range []*tar.Header{
			{Name: "directory/", Typeflag: tar.TypeDir, Mode: 0o755},
			{Name: "file", Typeflag: tar.TypeReg, Mode: 0o644},
			{Name: "symlink", Typeflag: tar.TypeSymlink, Linkname: "/file"},
			{Name: "hardlink", Typeflag: tar.TypeLink, Linkname: "file"},
			{Name: "character", Typeflag: tar.TypeChar},
			{Name: "block", Typeflag: tar.TypeBlock},
			{Name: "fifo", Typeflag: tar.TypeFifo},
		} {
			Expect(writer.WriteHeader(header)).To(Succeed())
		}
		Expect(writer.Close()).To(Succeed())
		err := services.ValidateImageUpgradeLayersForTest(context.Background(), []io.Reader{bytes.NewReader(buffer.Bytes())}, v1.MediaTypeImageLayer, int64(buffer.Len()), 7)
		Expect(err).NotTo(HaveOccurred())
	})
})

type cancellingImageUpgradeLayerReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r *cancellingImageUpgradeLayerReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()
	return n, err
}
