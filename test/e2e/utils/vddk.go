package utils

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"path/filepath"
)

// BuildMinimalVddkTarGz returns a minimal valid .tar.gz suitable for VDDK upload tests.
// The archive contains a single file (e.g. lib/lib64.so) so the agent can extract and report version/bytes/md5.
func BuildMinimalVddkTarGz(fileName, content string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if dir := filepath.Dir(fileName); dir != "." {
		_ = tw.WriteHeader(&tar.Header{Name: dir + "/", Typeflag: tar.TypeDir, Mode: 0o755})
	}
	hdr := &tar.Header{Name: fileName, Mode: 0o644, Size: int64(len(content))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}
