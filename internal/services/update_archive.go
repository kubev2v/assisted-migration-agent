package services

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"runtime"
	"strings"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	maxUpdateMetadata   = 1 << 20
	maxUpdateEntries    = 10000
	minimumUpgradeLabel = "min-upgrade-version"
)

var (
	updateDigestPattern     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	unsafeVersionCharacters = regexp.MustCompile(`[^A-Za-z0-9._-]`)
)

type updateArchiveEntry struct {
	offset int64
	size   int64
	digest string
}

type updateCountingReader struct {
	r io.Reader
	n int64
}

func (r *updateCountingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

type updateContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r updateContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func inspectUpdateArchive(ctx context.Context, filename, currentVersion, expectedDigest string) (models.UpdateImage, error) {
	return inspectUpdateArchiveForPlatform(ctx, filename, currentVersion, expectedDigest, runtime.GOARCH)
}

func inspectUpdateArchiveForPlatform(ctx context.Context, filename, currentVersion, expectedDigest, architecture string) (models.UpdateImage, error) {
	invalid := func(message string) (models.UpdateImage, error) {
		return models.UpdateImage{}, srvErrors.NewInvalidUpdateImageError(message)
	}
	f, err := os.Open(filename)
	if err != nil {
		return models.UpdateImage{}, err
	}
	defer func() { _ = f.Close() }()
	counter := &updateCountingReader{r: updateContextReader{ctx: ctx, r: f}}
	tr := tar.NewReader(counter)
	entries := make(map[string]updateArchiveEntry)
	for count := 0; ; count++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return models.UpdateImage{}, ctx.Err()
			}
			return invalid("invalid OCI archive: " + err.Error())
		}
		if count >= maxUpdateEntries {
			return invalid("too many archive entries")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "." && h.Typeflag == tar.TypeDir {
			continue
		}
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return invalid("unsafe archive path")
		}
		if _, exists := entries[name]; exists {
			return invalid("duplicate archive entry: " + name)
		}
		if h.Typeflag == tar.TypeDir {
			entries[name] = updateArchiveEntry{size: -1}
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return invalid("unsupported archive entry type")
		}
		entry := updateArchiveEntry{offset: counter.n, size: h.Size}
		hash := sha256.New()
		if _, err := io.Copy(hash, tr); err != nil {
			if ctx.Err() != nil {
				return models.UpdateImage{}, ctx.Err()
			}
			return invalid("cannot read archive entry: " + err.Error())
		}
		entry.digest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
		if strings.HasPrefix(name, "blobs/") && name != "blobs/sha256/"+strings.TrimPrefix(entry.digest, "sha256:") {
			return invalid("blob content digest mismatch")
		}
		entries[name] = entry
	}
	readJSON := func(name string, target any) error {
		entry, exists := entries[name]
		if !exists || entry.size < 0 || entry.size > maxUpdateMetadata {
			return fmt.Errorf("missing or oversized OCI metadata: %s", name)
		}
		decoder := json.NewDecoder(io.NewSectionReader(f, entry.offset, entry.size))
		if err := decoder.Decode(target); err != nil {
			return err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("trailing OCI metadata: %s", name)
		}
		return nil
	}
	var layout v1.ImageLayout
	if err := readJSON("oci-layout", &layout); err != nil || layout.Version != "1.0.0" {
		return invalid("missing or invalid OCI layout")
	}
	var index v1.Index
	if err := readJSON("index.json", &index); err != nil || index.SchemaVersion != 2 || len(index.Manifests) != 1 {
		return invalid("OCI archive must contain exactly one image manifest")
	}
	manifestDescriptor := index.Manifests[0]
	if manifestDescriptor.MediaType != v1.MediaTypeImageManifest {
		return invalid("unsupported image manifest type")
	}
	if expectedDigest != "" && expectedDigest != manifestDescriptor.Digest.String() {
		return invalid("expected image digest mismatch")
	}
	checkDescriptor := func(d v1.Descriptor) (string, error) {
		digest := d.Digest.String()
		if !updateDigestPattern.MatchString(digest) {
			return "", fmt.Errorf("unsupported OCI digest")
		}
		name := "blobs/sha256/" + strings.TrimPrefix(digest, "sha256:")
		entry, exists := entries[name]
		if !exists || entry.size < 0 || entry.size != d.Size || entry.digest != digest {
			return "", fmt.Errorf("missing or invalid referenced blob: %s", digest)
		}
		return name, nil
	}
	manifestName, err := checkDescriptor(manifestDescriptor)
	if err != nil {
		return invalid(err.Error())
	}
	var manifest v1.Manifest
	if err := readJSON(manifestName, &manifest); err != nil || manifest.SchemaVersion != 2 || manifest.Config.MediaType != v1.MediaTypeImageConfig {
		return invalid("invalid OCI manifest")
	}
	configName, err := checkDescriptor(manifest.Config)
	if err != nil {
		return invalid(err.Error())
	}
	for _, layer := range manifest.Layers {
		switch layer.MediaType {
		case v1.MediaTypeImageLayer, v1.MediaTypeImageLayerGzip, v1.MediaTypeImageLayerZstd:
		default:
			return invalid("unsupported OCI layer media type")
		}
		if _, err := checkDescriptor(layer); err != nil {
			return invalid(err.Error())
		}
	}
	var image v1.Image
	if err := readJSON(configName, &image); err != nil {
		return invalid("invalid OCI image configuration")
	}
	if image.OS != "linux" || image.Architecture != architecture {
		return invalid("image platform does not match the appliance")
	}
	if p := manifestDescriptor.Platform; p != nil && (p.OS != image.OS || p.Architecture != image.Architecture) {
		return invalid("inconsistent image platform")
	}
	version := image.Config.Labels[v1.AnnotationVersion]
	safeVersion := unsafeVersionCharacters.ReplaceAllString(version, "")
	if safeVersion == "" || safeVersion == "." || safeVersion == ".." || len(safeVersion) > 200 {
		return invalid("missing or invalid image version label")
	}
	if err := checkMinimumUpgradeVersion(currentVersion, image.Config.Labels[minimumUpgradeLabel]); err != nil {
		return invalid(err.Error())
	}
	return models.UpdateImage{Version: version, Digest: manifestDescriptor.Digest.String()}, nil
}
