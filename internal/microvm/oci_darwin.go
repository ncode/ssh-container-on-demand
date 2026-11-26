//go:build cgo

/*
Copyright © 2020 Juliano Martinez <juliano@martinez.io>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package microvm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// PrepareOCI resolves an OCI image and caches its flattened root filesystem.
func PrepareOCI(ctx context.Context, reference string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ref, err := name.ParseReference(reference)
	if err != nil {
		return "", fmt.Errorf("microvm.image: %w", err)
	}
	img, err := remote.Image(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithPlatform(v1.Platform{OS: "linux", Architecture: "arm64"}))
	if err != nil {
		return "", fmt.Errorf("resolve OCI image: %w", err)
	}
	imageConfig, err := img.ConfigFile()
	if err != nil {
		return "", fmt.Errorf("read OCI image config: %w", err)
	}
	if imageConfig.OS != "linux" || imageConfig.Architecture != "arm64" {
		return "", fmt.Errorf("microvm.image must target linux/arm64, got %s/%s", imageConfig.OS, imageConfig.Architecture)
	}
	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("read OCI image digest: %w", err)
	}
	cache, err := cachePath()
	if err != nil {
		return "", err
	}
	disk := filepath.Join(cache, "rootfs-"+digest.Hex+".tar")
	if info, err := os.Stat(disk); err == nil {
		if !info.Mode().IsRegular() || info.Size() == 0 || info.Size()%512 != 0 {
			return "", fmt.Errorf("invalid cached root filesystem %s", disk)
		}
		log.Printf("using cached OCI image %s", digest)
		return disk, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	log.Printf("preparing OCI image %s", digest)
	work, err := os.MkdirTemp(cache, ".export-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	prepared := filepath.Join(work, "rootfs.tar")
	if err := exportRootFS(img, prepared); err != nil {
		return "", fmt.Errorf("export OCI root filesystem: %w", err)
	}
	if err := os.Chmod(prepared, 0400); err != nil {
		return "", err
	}
	if err := os.Link(prepared, disk); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		info, err := os.Stat(disk)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 || info.Size()%512 != 0 {
			return "", fmt.Errorf("invalid cached root filesystem %s", disk)
		}
	}
	return disk, nil
}

func cachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "ssh-container-on-demand")
	return dir, os.MkdirAll(dir, 0700)
}

// KernelPath validates an uncompressed ARM64 Linux Image.
func KernelPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("set microvm.kernel.path to an uncompressed ARM64 Linux Image")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("microvm.kernel.path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("microvm.kernel.path is not an uncompressed ARM64 Linux Image")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var header [64]byte
	if _, err := io.ReadFull(file, header[:]); err != nil || string(header[56:60]) != "ARM\x64" {
		return "", errors.New("microvm.kernel.path is not an uncompressed ARM64 Linux Image")
	}
	return path, nil
}
func exportRootFS(img v1.Image, path string) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	reader := mutate.Extract(img)
	defer reader.Close()
	n, err := io.Copy(out, io.LimitReader(reader, maxRootFSBytes+1))
	if err != nil {
		return err
	}
	if n > maxRootFSBytes {
		return errors.New("OCI root filesystem exceeds 1 GiB")
	}
	if err := out.Truncate((n + 511) / 512 * 512); err != nil {
		return err
	}
	return out.Close()
}
