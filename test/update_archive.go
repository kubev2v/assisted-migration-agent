package test

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"runtime"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// UpdateArchive builds a small OCI archive, retaining caller-supplied entries
// after the image so tests can exercise duplicate and unsafe paths.
func UpdateArchive(version, minimum, architecture string, extra ...TarEntry) ([]byte, string) {
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	config, _ := json.Marshal(v1.Image{
		Platform: v1.Platform{Architecture: architecture, OS: "linux"},
		Config: v1.ImageConfig{Labels: map[string]string{
			v1.AnnotationVersion:  version,
			"min-upgrade-version": minimum,
		}},
	})
	layer := []byte("test layer")
	descriptor := func(data []byte, mediaType string) v1.Descriptor {
		return v1.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(data), Size: int64(len(data))}
	}
	manifest, _ := json.Marshal(v1.Manifest{Versioned: specs.Versioned{SchemaVersion: 2},
		Config: descriptor(config, v1.MediaTypeImageConfig), Layers: []v1.Descriptor{descriptor(layer, v1.MediaTypeImageLayer)}})
	manifestDescriptor := descriptor(manifest, v1.MediaTypeImageManifest)
	index, _ := json.Marshal(v1.Index{Versioned: specs.Versioned{SchemaVersion: 2}, Manifests: []v1.Descriptor{manifestDescriptor}})
	entries := []TarEntry{{Path: "oci-layout", Content: `{"imageLayoutVersion":"1.0.0"}`}, {Path: "index.json", Content: string(index)}}
	for _, data := range [][]byte{config, layer, manifest} {
		entries = append(entries, TarEntry{Path: "blobs/sha256/" + strings.TrimPrefix(digest.FromBytes(data).String(), "sha256:"), Content: string(data)})
	}
	entries = append(entries, extra...)
	var buffer bytes.Buffer
	tw := tar.NewWriter(&buffer)
	for _, entry := range entries {
		_ = tw.WriteHeader(&tar.Header{Name: entry.Path, Size: int64(len(entry.Content)), Mode: 0o600})
		_, _ = tw.Write([]byte(entry.Content))
	}
	_ = tw.Close()
	return buffer.Bytes(), manifestDescriptor.Digest.String()
}
