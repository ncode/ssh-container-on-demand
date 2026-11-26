package main

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func archive(t *testing.T, entries ...struct {
	header tar.Header
	data   string
}) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	for _, entry := range entries {
		header := entry.header
		header.Size = int64(len(entry.data))
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestUnpackRootFS(t *testing.T) {
	type entry = struct {
		header tar.Header
		data   string
	}
	data := archive(t,
		entry{header: tar.Header{Name: "first", Typeflag: tar.TypeLink, Linkname: "bin/busybox"}},
		entry{header: tar.Header{Name: "bin/busybox", Mode: 0755}, data: "binary"},
		entry{header: tar.Header{Name: "bin/sh", Typeflag: tar.TypeSymlink, Linkname: "/bin/busybox", Mode: 0777}},
	)
	dir := t.TempDir()
	if err := unpackRootFS(bytes.NewReader(data), dir); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(dir, "bin/sh"))
	if err != nil || target != "/bin/busybox" {
		t.Fatalf("shell link = %q, error = %v", target, err)
	}
	for _, name := range []string{"bin/busybox", "first"} {
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil || string(content) != "binary" {
			t.Fatalf("%s = %q, error = %v", name, content, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0755 {
			t.Fatalf("%s mode = %v, error = %v", name, info, err)
		}
	}
}

func TestUnpackRejectsSymlinkEscape(t *testing.T) {
	type entry = struct {
		header tar.Header
		data   string
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	outside := filepath.Join(parent, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	data := archive(t,
		entry{header: tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../outside"}},
		entry{header: tar.Header{Name: "link/escaped", Mode: 0644}, data: "unsafe"},
	)
	if err := unpackRootFS(bytes.NewReader(data), root); err == nil {
		t.Fatal("symlink escape was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists or cannot be inspected: %v", err)
	}
}
