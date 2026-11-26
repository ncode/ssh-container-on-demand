package microvm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 10 && os.Args[1] == WorkerCommand {
		os.Args = append([]string{os.Args[0], "-test.run=^TestMicroVMWorkerHelper$", "--"}, os.Args[1:]...)
	}
	os.Exit(m.Run())
}

func TestServeDisposableGuests(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.raw")
	if err := os.WriteFile(base, []byte("base"), 0400); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{executable: executable, image: base, kernel: "kernel", initrd: "initrd", resources: Resources{CPUs: 2, MemoryMiB: 768, DiskSizeMiB: 384}, maxConnections: 2, bootTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = server.Serve(ctx, listener)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("server did not stop")
		}
	})
	connect := func() (net.Conn, string) {
		t.Helper()
		conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(conn, "SSH-client\r\n"); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(conn)
		banner, err := reader.ReadString('\n')
		if err != nil || banner != "SSH-testvm\r\n" {
			t.Fatalf("banner = %q, error = %v", banner, err)
		}
		path, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return conn, strings.TrimSuffix(path, "\n")
	}
	first, firstDir := connect()
	second, secondDir := connect()
	if firstDir == secondDir {
		t.Fatal("guest state directories must be distinct")
	}
	for _, path := range []string{filepath.Join(firstDir, "session"), filepath.Join(secondDir, "session")} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "changed" {
			t.Fatalf("disk %s = %q, error = %v", path, data, err)
		}
	}
	data, err := os.ReadFile(base)
	if err != nil || string(data) != "base" {
		t.Fatalf("base disk = %q, error = %v", data, err)
	}
	third, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	third.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = third.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("excess client was not closed")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("excess client timed out instead of being closed: %v", err)
	}
	first.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := os.Stat(firstDir)
		if os.IsNotExist(err) {
			break
		}
		if err != nil || time.Now().After(deadline) {
			t.Fatalf("first guest directory still exists or cannot be inspected: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
		if serveErr != nil {
			t.Fatalf("Serve: %v", serveErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
	if _, err := os.Stat(secondDir); !os.IsNotExist(err) {
		t.Fatalf("second guest directory not removed: %v", err)
	}
	second.Close()
}

func TestMicroVMWorkerHelper(t *testing.T) {
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if args == nil {
		return
	}
	if len(args) != 9 || args[0] != WorkerCommand {
		t.Fatalf("unexpected worker arguments: %q", args)
	}
	if got := strings.Join(args[5:8], " "); got != "2 768 384" {
		t.Fatalf("worker resources = %q", got)
	}
	dir := args[1]
	if hostname := args[8]; !hostnamePattern.MatchString(hostname) || !strings.HasSuffix(hostname, "-"+strings.TrimPrefix(filepath.Base(dir), stateDirPrefix)) {
		t.Fatalf("worker hostname = %q for state %q", hostname, dir)
	}
	data, err := os.ReadFile(args[4])
	if err != nil || string(data) != "base" {
		t.Fatalf("root filesystem = %q, error = %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "ssh.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	first, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, "SSH-testvm\r\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "SSH-client\r\n" {
		t.Fatalf("client banner = %q, error = %v", line, err)
	}
	if _, err := fmt.Fprintln(conn, dir); err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Time{})
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatal(err)
	}
}

func TestBootTimeout(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sshvm-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "ssh.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(io.Discard, conn)
			}()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	conn, err := waitForSSH(ctx, socket, make(chan struct{}))
	if conn != nil {
		conn.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForSSH error = %v, want deadline exceeded", err)
	}
}
