package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	guestName = "guest"
	guestUID  = 1000
	guestHome = "/home/" + guestName
)

func provisionGuestAccount(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, name := range []string{"etc", "home", strings.TrimPrefix(guestHome, "/")} {
		if err := root.Mkdir(name, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("guest path %s must be a directory", name)
		}
	}
	home := strings.TrimPrefix(guestHome, "/")
	if err := root.Chown(home, guestUID, guestUID); err != nil {
		return err
	}
	if err := root.Chmod(home, 0750); err != nil {
		return err
	}
	passwd := fmt.Sprintf("%s:x:%d:%d::%s:/bin/sh", guestName, guestUID, guestUID, guestHome)
	if err := writeGuestRecord(root, "etc/passwd", passwd); err != nil {
		return err
	}
	return writeGuestRecord(root, "etc/group", fmt.Sprintf("%s:x:%d:", guestName, guestUID))
}

func writeGuestRecord(root *os.Root, path, record string) error {
	var data []byte
	info, err := root.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return fmt.Errorf("invalid guest account file %s", path)
		}
		data, err = root.ReadFile(path)
		if err != nil {
			return err
		}
		// Replace the file rather than modifying a potential archive hard link.
		if err := root.Remove(path); err != nil {
			return err
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(withGuestRecord(string(data), record)); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func withGuestRecord(data, record string) string {
	if data == "" {
		return record + "\n"
	}
	var out strings.Builder
	for line := range strings.SplitSeq(strings.TrimSuffix(data, "\n"), "\n") {
		fields := strings.SplitN(line, ":", 4)
		if len(fields) >= 3 {
			id, err := strconv.ParseUint(fields[2], 10, 32)
			if fields[0] == guestName || (err == nil && id == guestUID) {
				continue
			}
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteString(record)
	out.WriteByte('\n')
	return out.String()
}
