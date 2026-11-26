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
package cmd

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ncode/ssh-container-on-demand/internal/microvm"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	var listen string
	var maxConnections int
	var bootTimeout time.Duration
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Start a Linux microVM per SSH connection",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			kernel, err := microvm.KernelPath(viper.GetString("microvm.kernel.path"))
			if err != nil {
				return err
			}
			image := viper.GetString("microvm.image")
			if image == "" {
				return errors.New("set microvm.image to a Linux ARM64 OCI image")
			}
			rootfs, err := microvm.PrepareOCI(ctx, image)
			if err != nil {
				return err
			}
			initrd, cleanup, err := microvm.PrepareInitramfs(viper.GetString("microvm.ssh_public_key"))
			if err != nil {
				return err
			}
			defer cleanup()
			server, err := microvm.NewServer(rootfs, kernel, initrd, maxConnections, bootTimeout)
			if err != nil {
				return err
			}
			listener, err := net.Listen("tcp", listen)
			if err != nil {
				return err
			}
			log.Printf("listening for SSH connections on %s", listener.Addr())
			return server.Serve(ctx, listener)
		},
	}
	serve.Flags().StringVar(&listen, "listen", "127.0.0.1:2222", "TCP address to listen on")
	serve.Flags().IntVar(&maxConnections, "max-connections", 2, "maximum number of concurrent microVMs")
	serve.Flags().DurationVar(&bootTimeout, "boot-timeout", 30*time.Second, "time to wait for guest SSH")
	rootCmd.AddCommand(serve)
	worker := &cobra.Command{
		Use:              microvm.WorkerCommand + " STATE_DIR KERNEL INITRAMFS ROOTFS",
		Hidden:           true,
		Args:             cobra.ExactArgs(4),
		PersistentPreRun: func(_ *cobra.Command, _ []string) {}, // The parent owns configuration.
		RunE: func(_ *cobra.Command, args []string) error {
			return microvm.RunWorker(args[0], args[1], args[2], args[3])
		},
	}
	rootCmd.AddCommand(worker)
}
