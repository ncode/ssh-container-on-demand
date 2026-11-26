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
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const guestRoot = "/rootfs"

func main() {
	if len(os.Args) == 3 && os.Args[1] == "unpack" {
		if err := unpackRootFS(os.Stdin, os.Args[2]); err != nil {
			log.Fatalf("unpack OCI rootfs: %q", err.Error())
		}
		return
	}
	if len(os.Args) != 1 {
		log.Fatal("invalid guest bootstrap arguments")
	}
	if err := prepareGuest(); err != nil {
		log.Printf("prepare guest: %q", err.Error())
	} else if err := serveSSH(); err != nil {
		log.Printf("serve SSH: %q", err.Error())
	}
	for {
		time.Sleep(time.Hour)
	}
}

func prepareGuest() error {
	if err := os.MkdirAll("/dev", 0755); err != nil {
		return err
	}
	if err := unix.Mount("devtmpfs", "/dev", "devtmpfs", 0, ""); err != nil {
		return err
	}
	serial, err := os.OpenFile("/dev/hvc0", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	log.SetOutput(serial)
	if err := unix.Sethostname([]byte("sshvm")); err != nil {
		return fmt.Errorf("set guest hostname: %w", err)
	}
	if err := os.MkdirAll("/dev/pts", 0755); err != nil {
		return err
	}
	if err := unix.Mount("devpts", "/dev/pts", "devpts", unix.MS_NOSUID|unix.MS_NOEXEC, "mode=0620,ptmxmode=0666"); err != nil {
		return err
	}
	if err := os.MkdirAll("/proc", 0755); err != nil {
		return err
	}
	if err := unix.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return err
	}
	if err := os.Mkdir(guestRoot, 0755); err != nil {
		return err
	}
	if err := unix.Mount("tmpfs", guestRoot, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "mode=0755"); err != nil {
		return err
	}
	if err := os.Chown(guestRoot, guestUID, guestUID); err != nil {
		return err
	}
	var src *os.File
	for range 50 {
		src, err = os.Open("/dev/vda")
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	defer src.Close()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "unpack", guestRoot)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: guestUID, Gid: guestUID}}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = src, serial, serial
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unpack OCI root filesystem: %w", err)
	}
	if err := os.Chown(guestRoot, 0, 0); err != nil {
		return err
	}
	if err := os.Chmod(guestRoot, 0755); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(guestRoot, "bin/sh"))
	if err != nil || (info.Mode()&os.ModeSymlink == 0 && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0)) {
		return errors.New("OCI image has no usable /bin/sh")
	}
	dev := filepath.Join(guestRoot, "dev")
	info, err = os.Lstat(dev)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dev, 0755); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !info.IsDir() {
		return errors.New("OCI root filesystem /dev must be a directory")
	}
	if err := unix.Mount("tmpfs", dev, "tmpfs", unix.MS_NOSUID|unix.MS_NOEXEC, "size=1m,mode=0755"); err != nil {
		return err
	}
	for _, device := range []struct {
		name         string
		major, minor uint32
	}{
		{"null", 1, 3}, {"zero", 1, 5}, {"urandom", 1, 9}, {"random", 1, 8}, {"tty", 5, 0},
	} {
		path := filepath.Join(dev, device.name)
		if err := unix.Mknod(path, unix.S_IFCHR|0666, int(unix.Mkdev(device.major, device.minor))); err != nil {
			return err
		}
		if err := os.Chmod(path, 0666); err != nil {
			return err
		}
	}
	return provisionGuestAccount(guestRoot)
}
