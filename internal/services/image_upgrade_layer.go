package services

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	maxImageUpgradeLayerBytes   int64 = 16 << 30
	maxImageUpgradeLayerEntries       = 1000000
	maxImageUpgradeZstdWindow         = 64 << 20
)

type imageUpgradeLayerBudget struct {
	bytes   int64
	entries int
}

func validateImageUpgradeLayer(ctx context.Context, source io.Reader, mediaType string, budget *imageUpgradeLayerBudget) error {
	var reader io.Reader = imageUpgradeContextReader{ctx: ctx, r: source}
	switch mediaType {
	case v1.MediaTypeImageLayer:
	case v1.MediaTypeImageLayerGzip:
		decoder, err := gzip.NewReader(reader)
		if err != nil {
			return fmt.Errorf("invalid gzip layer: %w", err)
		}
		defer func() { _ = decoder.Close() }()
		reader = decoder
	case v1.MediaTypeImageLayerZstd:
		decoder, err := zstd.NewReader(reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxMemory(maxImageUpgradeZstdWindow))
		if err != nil {
			return fmt.Errorf("invalid zstd layer: %w", err)
		}
		defer decoder.Close()
		reader = decoder
	default:
		return fmt.Errorf("unsupported OCI layer media type")
	}

	limited := &io.LimitedReader{R: imageUpgradeContextReader{ctx: ctx, r: reader}, N: budget.bytes + 1}
	counter := &imageUpgradeCountingReader{r: limited}
	tr := tar.NewReader(counter)
	for {
		before := counter.n
		header, err := tr.Next()
		if limited.N == 0 {
			return fmt.Errorf("expanded layer data exceeds size limit")
		}
		if err == io.EOF {
			if counter.n-before < 1024 {
				return fmt.Errorf("layer tar is missing end-of-archive blocks")
			}
			break
		}
		if err != nil {
			return fmt.Errorf("invalid layer tar: %w", err)
		}
		if budget.entries == 0 {
			return fmt.Errorf("too many layer entries")
		}
		budget.entries--
		if header.Typeflag == tar.TypeGNUSparse {
			return fmt.Errorf("sparse layer entries are not supported")
		}
		for key := range header.PAXRecords {
			if strings.HasPrefix(key, "GNU.sparse.") {
				return fmt.Errorf("sparse layer entries are not supported")
			}
		}
		if header.Size > limited.N-1 {
			return fmt.Errorf("expanded layer data exceeds size limit")
		}
		_, err = io.Copy(io.Discard, imageUpgradeContextReader{ctx: ctx, r: tr})
		if limited.N == 0 {
			return fmt.Errorf("expanded layer data exceeds size limit")
		}
		if err != nil {
			return fmt.Errorf("invalid layer contents: %w", err)
		}
	}

	// Read through compression trailers so truncated streams and bad checksums fail.
	var buffer [32 << 10]byte
	for {
		n, err := limited.Read(buffer[:])
		if limited.N == 0 {
			return fmt.Errorf("expanded layer data exceeds size limit")
		}
		for _, b := range buffer[:n] {
			if b != 0 {
				return fmt.Errorf("unexpected data after layer tar")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid layer compression stream: %w", err)
		}
	}
	budget.bytes = limited.N - 1
	return nil
}
