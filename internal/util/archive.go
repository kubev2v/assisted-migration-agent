package util

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ExtractTarGz extracts all files and directories from a given reader and overrides a specified destination folder
func ExtractTarGz(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer func() {
		_ = gzr.Close()
	}()

	tarReader := tar.NewReader(gzr)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // end of archive
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Clean(filepath.Join(destDir, header.Name))
		// Ensure the target path is inside destDir
		if !strings.HasPrefix(targetPath, filepath.Clean(destDir)+string(os.PathSeparator)) &&
			targetPath != filepath.Clean(destDir) {
			return fmt.Errorf("illegal file path: %s", targetPath)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// create directory
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			// create file
			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				_ = outFile.Close()
				return err
			}
			_ = outFile.Close()
			if err := os.Chmod(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}

	return nil
}

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
