package test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"path/filepath"
	"sort"
)

// TarEntry describes a single file to include in the archive.
type TarEntry struct {
	Path    string
	Content string
}

// BuildTarGz builds a .tar.gz with the given files, creating parent directories as needed.
func BuildTarGz(entries ...TarEntry) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	dirs := make(map[string]bool)
	for _, e := range entries {
		for d := filepath.Dir(e.Path); d != "."; d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	var dirList []string
	for d := range dirs {
		dirList = append(dirList, d)
	}
	sort.Strings(dirList)
	for _, d := range dirList {
		_ = tw.WriteHeader(&tar.Header{Name: d + "/", Typeflag: tar.TypeDir, Mode: 0755})
	}
	for _, e := range entries {
		_ = tw.WriteHeader(&tar.Header{Name: e.Path, Mode: 0644, Size: int64(len(e.Content))})
		_, _ = tw.Write([]byte(e.Content))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}
