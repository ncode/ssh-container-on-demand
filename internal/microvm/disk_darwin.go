//go:build cgo

package microvm

import (
	"fmt"
	"os"

	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/ext4"
)

func createRootDisk(path string, size int64) error {
	image, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer image.Close()
	if err := image.Truncate(size); err != nil {
		return err
	}
	if _, err := ext4.Create(file.New(image, false), size, 0, 512, nil); err != nil {
		return fmt.Errorf("format private ext4 disk: %w", err)
	}
	return image.Close()
}
