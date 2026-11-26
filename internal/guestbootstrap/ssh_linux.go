package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	server "github.com/gliderlabs/ssh"
	"github.com/mdlayher/vsock"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

func serveSSH() error {
	authorized, err := os.ReadFile("/authorized_key")
	if err != nil {
		return err
	}
	key, _, options, rest, err := ssh.ParseAuthorizedKey(authorized)
	if err != nil || len(options) != 0 || len(rest) != 0 {
		return errors.New("invalid authorized SSH key")
	}
	hostKey, err := os.ReadFile("/host_key")
	if err != nil {
		return err
	}
	signer, err := ssh.ParsePrivateKey(hostKey)
	if err != nil {
		return err
	}
	listener, err := vsock.ListenContextID(unix.VMADDR_CID_ANY, 22, nil)
	if err != nil {
		return err
	}
	defer listener.Close()
	srv := &server.Server{
		HostSigners: []server.Signer{signer},
		PublicKeyHandler: func(ctx server.Context, provided server.PublicKey) bool {
			return ctx.User() == guestName && server.KeysEqual(key, provided)
		},
		Handler: serveSession,
	}
	return srv.Serve(listener)
}

func serveSession(session server.Session) {
	args := []string{"-l"}
	if command := session.RawCommand(); command != "" {
		args = []string{"-c", command}
	}
	cmd := exec.CommandContext(session.Context(), "/bin/sh", args...)
	cmd.Dir = guestHome
	cmd.Env = []string{"HOME=" + guestHome, "USER=" + guestName, "LOGNAME=" + guestName, "PATH=/usr/local/bin:/usr/bin:/bin"}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot:     guestRoot,
		Credential: &syscall.Credential{Uid: guestUID, Gid: guestUID},
	}
	term, windows, hasPTY := session.Pty()
	var err error
	if hasPTY {
		cmd.Env = append(cmd.Env, "TERM="+term.Term)
		var file *os.File
		file, err = pty.StartWithSize(cmd, windowSize(term.Window))
		if err == nil {
			defer file.Close()
			go io.Copy(file, session)
			go func() {
				for {
					select {
					case window, ok := <-windows:
						if !ok {
							return
						}
						_ = pty.Setsize(file, windowSize(window))
					case <-session.Context().Done():
						return
					}
				}
			}()
			_, _ = io.Copy(session, file)
			err = cmd.Wait()
		}
	} else {
		cmd.Stdout, cmd.Stderr = session, session.Stderr()
		var input io.WriteCloser
		input, err = cmd.StdinPipe()
		if err == nil {
			go func() {
				_, _ = io.Copy(input, session)
				input.Close()
			}()
			err = cmd.Run()
		}
	}
	if err == nil {
		_ = session.Exit(0)
		return
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		_ = session.Exit(exit.ExitCode())
		return
	}
	fmt.Fprintln(session.Stderr(), err)
	_ = session.Exit(1)
}

func windowSize(window server.Window) *pty.Winsize {
	height, width := window.Height, window.Width
	if height <= 0 || height > 65535 {
		height = 24
	}
	if width <= 0 || width > 65535 {
		width = 80
	}
	return &pty.Winsize{Rows: uint16(height), Cols: uint16(width)}
}
