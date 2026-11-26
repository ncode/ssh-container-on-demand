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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ncode/ssh-container-on-demand/internal/guestconfig"
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
	account, err := readGuestAccount()
	if err != nil {
		log.Printf("read guest account: %q", err.Error())
	} else if err := prepareGuest(account); err != nil {
		log.Printf("prepare guest: %q", err.Error())
	} else if err := serveSSH(account); err != nil {
		log.Printf("serve SSH: %q", err.Error())
	}
	for {
		time.Sleep(time.Hour)
	}
}

func readGuestAccount() (guestconfig.Account, error) {
	var account guestconfig.Account
	data, err := os.ReadFile("/guest_account")
	if err != nil {
		return account, err
	}
	if err := json.Unmarshal(data, &account); err != nil {
		return account, err
	}
	return account, account.Validate()
}

func prepareGuest(account guestconfig.Account) error {
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
	if err := os.MkdirAll("/dev/pts", 0755); err != nil {
		return err
	}
	ptsOptions := fmt.Sprintf("uid=%d,mode=0620,ptmxmode=0000", account.UID)
	if err := unix.Mount("devpts", "/dev/pts", "devpts", unix.MS_NOSUID|unix.MS_NOEXEC, ptsOptions); err != nil {
		return fmt.Errorf("mount devpts: %w", err)
	}
	if err := os.MkdirAll("/proc", 0755); err != nil {
		return err
	}
	if err := unix.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return err
	}
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return err
	}
	args := strings.Fields(string(cmdline))
	var hostname string
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "sshvm.hostname="); ok {
			hostname = value
			break
		}
	}
	if hostname == "" {
		return errors.New("missing guest hostname")
	}
	if err := unix.Sethostname([]byte(hostname)); err != nil {
		return fmt.Errorf("set guest hostname: %w", err)
	}
	if err := os.Mkdir(guestRoot, 0755); err != nil {
		return err
	}
	if err := mountGuestRoot(slices.Contains(args, "sshvm.disk=ext4")); err != nil {
		return fmt.Errorf("mount writable guest root: %w", err)
	}
	if err := os.Chown(guestRoot, int(account.UID), int(account.UID)); err != nil {
		return err
	}
	src, err := openGuestDisk("/dev/vda")
	if err != nil {
		return err
	}
	defer src.Close()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "unpack", guestRoot)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(account.UID), Gid: uint32(account.UID)}}
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
	dev, err := guestMountpoint("dev")
	if err != nil {
		return err
	}
	if err := unix.Mount("tmpfs", dev, "tmpfs", unix.MS_NOSUID|unix.MS_NOEXEC, "size=1m,mode=0755"); err != nil {
		return err
	}
	// The SSH server and its chrooted sessions need to see the same PTYs.
	pts := filepath.Join(dev, "pts")
	if err := os.Mkdir(pts, 0755); err != nil {
		return err
	}
	if err := unix.Mount("/dev/pts", pts, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("expose guest /dev/pts: %w", err)
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
	const flags = unix.MS_RDONLY | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC
	for _, fs := range []struct{ name, kind, options string }{
		{"proc", "proc", "hidepid=2"},
		{"sys", "sysfs", ""},
	} {
		target, err := guestMountpoint(fs.name)
		if err != nil {
			return err
		}
		if err := unix.Mount(fs.kind, target, fs.kind, flags, fs.options); err != nil {
			return fmt.Errorf("mount guest /%s: %w", fs.name, err)
		}
	}
	return provisionGuestAccount(guestRoot, account)
}

func mountGuestRoot(diskBacked bool) error {
	flags := uintptr(unix.MS_NOSUID | unix.MS_NODEV)
	if diskBacked {
		disk, err := openGuestDisk("/dev/vdb")
		if err != nil {
			return err
		}
		disk.Close()
		return unix.Mount("/dev/vdb", guestRoot, "ext4", flags, "")
	}
	return unix.Mount("tmpfs", guestRoot, "tmpfs", flags, "mode=0755")
}

func openGuestDisk(path string) (*os.File, error) {
	for range 50 {
		file, err := os.Open(path)
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return file, err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("waiting for guest disk %s: %w", path, os.ErrNotExist)
}

func guestMountpoint(name string) (string, error) {
	target := filepath.Join(guestRoot, name)
	if err := os.Mkdir(target, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("guest mountpoint /%s must be a directory", name)
	}
	return target, nil
}
