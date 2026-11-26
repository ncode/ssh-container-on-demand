//go:build cgo

package microvm

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func TestExportRootFSFlattensLayers(t *testing.T) {
	layer := func(files map[string]string, link bool) v1.Layer {
		t.Helper()
		var data bytes.Buffer
		writer := tar.NewWriter(&data)
		for name, content := range files {
			if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(writer, content); err != nil {
				t.Fatal(err)
			}
		}
		if link {
			if err := writer.WriteHeader(&tar.Header{Name: "bin/sh", Typeflag: tar.TypeSymlink, Linkname: "/bin/busybox", Mode: 0777}); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		result, err := tarball.LayerFromReader(&data)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	image, err := mutate.AppendLayers(empty.Image,
		layer(map[string]string{"old": "old", "changed": "before"}, false),
		layer(map[string]string{".wh.old": "", "changed": "after"}, true))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rootfs.tar")
	if err := exportRootFS(image, path); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size()%512 != 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("rootfs stat = %v, error = %v", info, err)
	}
	files := make(map[string]string)
	sawShell := false
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "bin/sh" && header.Typeflag == tar.TypeSymlink && header.Linkname == "/bin/busybox" {
			sawShell = true
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = string(content)
	}
	if len(files) != 1 || files["changed"] != "after" {
		t.Fatalf("flattened files = %v", files)
	}
	if !sawShell {
		t.Fatal("absolute /bin/sh symlink was lost")
	}
}
