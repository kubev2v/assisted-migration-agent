package test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"runtime"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type ImageUpgradeLayer struct {
	Data      []byte
	MediaType string
}

// ImageUpgradeArchive builds a small OCI archive, retaining caller-supplied entries
// after the image so tests can exercise duplicate and unsafe paths.
func ImageUpgradeArchive(version, minimum, architecture string, extra ...TarEntry) ([]byte, string) {
	layer := BuildTarGz(TarEntry{Path: "fixture.txt", Content: "test layer"})
	reader, _ := gzip.NewReader(bytes.NewReader(layer))
	uncompressed, _ := io.ReadAll(reader)
	_ = reader.Close()
	rootFS := v1.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.FromBytes(uncompressed)}}
	return ImageUpgradeArchiveWithLayers(version, minimum, architecture, rootFS, []ImageUpgradeLayer{{Data: layer, MediaType: v1.MediaTypeImageLayerGzip}}, extra...)
}

func ImageUpgradeArchiveWithRootFS(version, minimum, architecture string, rootFS v1.RootFS, extra ...TarEntry) ([]byte, string) {
	layer := BuildTarGz(TarEntry{Path: "fixture.txt", Content: "test layer"})
	return ImageUpgradeArchiveWithLayers(version, minimum, architecture, rootFS, []ImageUpgradeLayer{{Data: layer, MediaType: v1.MediaTypeImageLayerGzip}}, extra...)
}

func ImageUpgradeArchiveWithLayers(version, minimum, architecture string, rootFS v1.RootFS, layers []ImageUpgradeLayer, extra ...TarEntry) ([]byte, string) {
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	config, _ := json.Marshal(v1.Image{
		Platform: v1.Platform{Architecture: architecture, OS: "linux"},
		RootFS:   rootFS,
		Config: v1.ImageConfig{Labels: map[string]string{
			v1.AnnotationVersion:                 version,
			"io.kubev2v.oma.min-upgrade-version": minimum,
		}},
	})
	descriptor := func(data []byte, mediaType string) v1.Descriptor {
		return v1.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(data), Size: int64(len(data))}
	}
	layerDescriptors := make([]v1.Descriptor, 0, len(layers))
	blobs := [][]byte{config}
	for _, layer := range layers {
		layerDescriptors = append(layerDescriptors, descriptor(layer.Data, layer.MediaType))
		blobs = append(blobs, layer.Data)
	}
	manifest, _ := json.Marshal(v1.Manifest{Versioned: specs.Versioned{SchemaVersion: 2},
		Config: descriptor(config, v1.MediaTypeImageConfig), Layers: layerDescriptors})
	manifestDescriptor := descriptor(manifest, v1.MediaTypeImageManifest)
	index, _ := json.Marshal(v1.Index{Versioned: specs.Versioned{SchemaVersion: 2}, Manifests: []v1.Descriptor{manifestDescriptor}})
	entries := []TarEntry{{Path: "oci-layout", Content: `{"imageLayoutVersion":"1.0.0"}`}, {Path: "index.json", Content: string(index)}}
	blobs = append(blobs, manifest)
	seen := make(map[string]bool)
	for _, data := range blobs {
		name := "blobs/sha256/" + strings.TrimPrefix(digest.FromBytes(data).String(), "sha256:")
		if seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, TarEntry{Path: name, Content: string(data)})
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
