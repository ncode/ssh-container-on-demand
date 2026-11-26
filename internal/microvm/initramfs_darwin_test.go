//go:build cgo

package microvm

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestKernelPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Image")
	data := make([]byte, 64)
	copy(data[56:60], "ARM\x64")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := KernelPath(path); err != nil || got != path {
		t.Fatalf("path = %q, error = %v", got, err)
	}
	for _, invalid := range []string{"", dir} {
		if _, err := KernelPath(invalid); err == nil {
			t.Fatalf("accepted invalid kernel %q", invalid)
		}
	}
	data[56] = 0
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := KernelPath(path); err == nil {
		t.Fatal("accepted invalid kernel magic")
	}
}

func TestStableHostKey(t *testing.T) {
	dir := t.TempDir()
	first, err := stableHostKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := stableHostKey(dir)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("host key was not reused: %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, "ssh_host_ed25519_key"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := stableHostKey(dir); err == nil {
		t.Fatal("accepted a non-private host key")
	}
}

func TestAppendCPIO(t *testing.T) {
	var archive bytes.Buffer
	appendCPIO(&archive, "init", 0100755, []byte("data"))
	data := archive.Bytes()
	if len(data) < 110 || string(data[:6]) != "070701" {
		t.Fatal("invalid newc header")
	}
	if _, err := hex.DecodeString(string(data[6:110])); err != nil {
		t.Fatal(err)
	}
	mode, err := strconv.ParseUint(string(data[14:22]), 16, 32)
	if err != nil || mode != 0100755 {
		t.Fatalf("mode = %o, error = %v", mode, err)
	}
	size, err := strconv.ParseUint(string(data[54:62]), 16, 32)
	if err != nil || size != 4 {
		t.Fatalf("size = %d, error = %v", size, err)
	}
	if !bytes.Equal(data[110:115], []byte("init\x00")) || !bytes.Equal(data[116:120], []byte("data")) || len(data)%4 != 0 {
		t.Fatalf("invalid archive payload %q", data[110:])
	}
}
