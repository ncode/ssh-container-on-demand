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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ncode/ssh-container-on-demand/internal/proxy"
)

const WorkerCommand = "_microvm-worker"

// Server starts a disposable Linux microVM for each accepted connection.
type Server struct {
	executable, image, kernel, initrd string
	maxConnections                    int
	bootTimeout                       time.Duration
}

// NewServer validates the shared root filesystem and resolves the worker executable before serving.
func NewServer(image, kernel, initrd string, maxConnections int, bootTimeout time.Duration) (*Server, error) {
	if image == "" {
		return nil, errors.New("cached root filesystem path is empty")
	}
	if maxConnections < 1 {
		return nil, errors.New("max-connections must be positive")
	}
	if bootTimeout <= 0 {
		return nil, errors.New("boot-timeout must be positive")
	}
	image, err := filepath.Abs(image)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(image)
	if err != nil {
		return nil, fmt.Errorf("cached root filesystem: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, errors.New("cached root filesystem must be a nonempty regular file")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find worker executable: %w", err)
	}
	return &Server{executable: executable, image: image, kernel: kernel, initrd: initrd, maxConnections: maxConnections, bootTimeout: bootTimeout}, nil
}

// Serve owns listener and waits for all guests to be reaped before returning.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	var active sync.WaitGroup
	defer func() {
		cancel()
		listener.Close()
		active.Wait()
	}()
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	slots := make(chan struct{}, s.maxConnections)
	for {
		client, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			active.Add(1)
			go func(client net.Conn) {
				defer active.Done()
				defer func() { <-slots }()
				if err := s.handle(ctx, client); err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled) {
					log.Printf("microVM for %s: %v", client.RemoteAddr(), err)
				}
			}(client)
		default:
			client.Close()
		}
	}
}

func (s *Server) handle(parent context.Context, client net.Conn) error {
	defer client.Close()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() {
		<-ctx.Done()
		client.Close()
	}()
	finishBuffering := bufferClient(client, cancel)
	boot, stopBoot := context.WithTimeout(ctx, s.bootTimeout)
	defer stopBoot()

	// Keep the Unix socket path short regardless of TMPDIR or the image path.
	dir, err := os.MkdirTemp("/tmp", "sshvm-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("remove microVM state: %v", err)
		}
	}()
	socket := filepath.Join(dir, "ssh.sock")
	// Isolate native VM resources and allow cancellation even during native startup.
	vm := exec.CommandContext(ctx, s.executable, WorkerCommand, dir, s.kernel, s.initrd, s.image)
	vm.Stdout, vm.Stderr = os.Stderr, os.Stderr
	if err := vm.Start(); err != nil {
		return fmt.Errorf("start microVM worker: %w", err)
	}
	exited := make(chan struct{})
	var exitErr error
	go func() {
		exitErr = vm.Wait()
		close(exited)
	}()
	defer func() { cancel(); <-exited }()

	guest, err := waitForSSH(boot, socket, exited)
	if err != nil {
		if errors.Is(err, errVMExited) && exitErr != nil {
			err = fmt.Errorf("microVM worker exited before guest SSH was ready: %w", exitErr)
		} else {
			err = fmt.Errorf("waiting for guest SSH on vsock port 22: %w", err)
		}
		if file, openErr := os.Open(filepath.Join(dir, "serial.log")); openErr == nil {
			defer file.Close()
			if info, statErr := file.Stat(); statErr == nil {
				offset := max(info.Size()-4096, 0)
				if _, seekErr := file.Seek(offset, io.SeekStart); seekErr == nil {
					if tail, readErr := io.ReadAll(io.LimitReader(file, 4096)); readErr == nil && len(tail) > 0 {
						return fmt.Errorf("%w; guest serial: %s", err, tail)
					}
				}
			}
		}
		return err
	}
	stopBoot()
	return proxy.Bridge(finishBuffering(), guest)
}

// Buffer the initial client bytes so we can notice disconnects during boot.
// ponytail: once 16 KiB are buffered, disconnect detection waits for relay or the boot timeout.
func bufferClient(client net.Conn, cancel context.CancelFunc) func() net.Conn {
	var pending bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := io.CopyN(&pending, client, 16*1024); err != nil {
			if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
				cancel()
			}
		}
	}()
	return func() net.Conn {
		client.SetReadDeadline(time.Now())
		<-done
		client.SetReadDeadline(time.Time{})
		return &readerConn{Conn: client, reader: io.MultiReader(&pending, client)}
	}
}

type readerConn struct {
	net.Conn
	reader io.Reader
}

func (c *readerConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

var errVMExited = errors.New("microVM worker exited before SSH was ready")

func waitForSSH(ctx context.Context, socket string, exited <-chan struct{}) (net.Conn, error) {
	const probeTimeout = 250 * time.Millisecond
	d := net.Dialer{Timeout: probeTimeout}
	retry := time.NewTicker(100 * time.Millisecond)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-exited:
			return nil, errVMExited
		default:
		}
		conn, err := d.DialContext(ctx, "unix", socket)
		if err == nil {
			deadline := time.Now().Add(probeTimeout)
			if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
				deadline = limit
			}
			conn.SetReadDeadline(deadline)
			// The worker can accept the Unix connection before guest sshd is listening.
			reader := bufio.NewReader(conn)
			if prefix, err := reader.Peek(4); err == nil && string(prefix) == "SSH-" {
				conn.SetReadDeadline(time.Time{})
				return &readerConn{Conn: conn, reader: reader}, nil
			}
			conn.Close()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-exited:
			return nil, errVMExited
		case <-retry.C:
		}
	}
}
