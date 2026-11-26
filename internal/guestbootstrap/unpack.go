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
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
)

type hardLink struct {
	name, target string
}

func unpackRootFS(src io.Reader, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	reader := tar.NewReader(src)
	dirs := make(map[string]os.FileMode)
	var links []hardLink
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := path.Clean(header.Name)
		if name == "." && header.Typeflag == tar.TypeDir {
			continue
		}
		if name == "." || !fs.ValidPath(name) {
			return fmt.Errorf("invalid tar path %q", header.Name)
		}
		if err := root.MkdirAll(path.Dir(name), 0755); err != nil {
			return err
		}
		mode := os.FileMode(header.Mode) & 0777
		switch header.Typeflag {
		case tar.TypeDir:
			err = root.MkdirAll(name, 0755)
			dirs[name] = mode
		case tar.TypeReg, tar.TypeRegA:
			err = writeRegular(root, name, mode, reader)
		case tar.TypeSymlink:
			err = root.Symlink(header.Linkname, name)
		case tar.TypeLink:
			target := path.Clean(header.Linkname)
			if target == "." || !fs.ValidPath(target) {
				return fmt.Errorf("invalid hard-link target %q", header.Linkname)
			}
			links = append(links, hardLink{name, target})
		default:
			return fmt.Errorf("unsupported OCI tar entry type %d for %q", header.Typeflag, name)
		}
		if err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
	}
	for len(links) > 0 {
		pending := links[:0]
		for _, link := range links {
			if err := root.Link(link.target, link.name); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					pending = append(pending, link)
					continue
				}
				return fmt.Errorf("link %s: %w", link.name, err)
			}
		}
		if len(pending) == len(links) {
			return fmt.Errorf("unresolved hard link %s to %s", pending[0].name, pending[0].target)
		}
		links = pending
	}
	names := make([]string, 0, len(dirs))
	for name := range dirs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, name := range names {
		if err := root.Chmod(name, dirs[name]); err != nil {
			return err
		}
	}
	return nil
}

func writeRegular(root *os.Root, name string, mode os.FileMode, reader io.Reader) error {
	out, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, reader); err != nil {
		return err
	}
	if err := out.Chmod(mode); err != nil {
		return err
	}
	return out.Close()
}
