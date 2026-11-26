//go:build cgo

package microvm

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"embed"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

//go:embed assets
var initAssets embed.FS

func PrepareInitramfs(keyPath string) (string, func(), error) {
	if keyPath == "" {
		return "", nil, errors.New("set microvm.ssh_public_key to an OpenSSH public key file")
	}
	contents, err := os.ReadFile(keyPath)
	if err != nil {
		return "", nil, fmt.Errorf("microvm.ssh_public_key: %w", err)
	}
	key, _, options, rest, err := ssh.ParseAuthorizedKey(contents)
	if err != nil || len(options) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return "", nil, errors.New("microvm.ssh_public_key must contain one OpenSSH public key without options")
	}
	bootstrap, err := initAssets.ReadFile("assets/guest-bootstrap")
	if err != nil {
		return "", nil, errors.New("guest bootstrap is not embedded; build the service with make")
	}
	cache, err := cachePath()
	if err != nil {
		return "", nil, err
	}
	hostKey, err := stableHostKey(cache)
	if err != nil {
		return "", nil, err
	}
	signer, err := ssh.ParsePrivateKey(hostKey)
	if err != nil {
		return "", nil, err
	}
	log.Printf("SSH host key fingerprint: %s", ssh.FingerprintSHA256(signer.PublicKey()))
	var archive bytes.Buffer
	appendCPIO(&archive, "init", 0100755, bootstrap)
	appendCPIO(&archive, "authorized_key", 0100400, ssh.MarshalAuthorizedKey(key))
	appendCPIO(&archive, "host_key", 0100400, hostKey)
	appendCPIO(&archive, "TRAILER!!!", 0, nil)
	file, err := os.CreateTemp("", "sshvm-initramfs-*.cpio")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := file.Write(archive.Bytes()); err != nil {
		file.Close()
		cleanup()
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

func stableHostKey(cache string) ([]byte, error) {
	path := filepath.Join(cache, "ssh_host_ed25519_key")
	key, err := readHostKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(private, "ssh-container-on-demand")
	if err != nil {
		return nil, err
	}
	key = pem.EncodeToMemory(block)
	temp, err := os.CreateTemp(cache, ".host-key-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(key); err != nil {
		temp.Close()
		return nil, err
	}
	if err := temp.Close(); err != nil {
		return nil, err
	}
	if err := os.Link(temp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return readHostKey(path)
}

func readHostKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("cached SSH host key must be a private regular file")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if _, err := ssh.ParsePrivateKey(key); err != nil {
		return nil, fmt.Errorf("invalid cached SSH host key: %w", err)
	}
	return key, nil
}

func appendCPIO(archive *bytes.Buffer, name string, mode int, data []byte) {
	archive.WriteString("070701")
	for _, value := range []int{0, mode, 0, 0, 1, 0, len(data), 0, 0, 0, 0, len(name) + 1, 0} {
		fmt.Fprintf(archive, "%08x", value)
	}
	archive.WriteString(name)
	archive.WriteByte(0)
	for archive.Len()%4 != 0 {
		archive.WriteByte(0)
	}
	archive.Write(data)
	for archive.Len()%4 != 0 {
		archive.WriteByte(0)
	}
}
