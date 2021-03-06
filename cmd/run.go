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
	"fmt"
	"log"

	"github.com/coreos/go-systemd/activation"
	"github.com/ncode/ssh-container-on-demand/internal/container"
	"github.com/ncode/ssh-container-on-demand/internal/proxy"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the ssh-container-on-demand expecting a fd comming from systemd",
	Run: func(cmd *cobra.Command, args []string) {
		listeners, err := activation.Listeners()
		if err != nil {
			log.Fatalln(err)
		}

		if len(listeners) != 1 {
			log.Fatalln("Unexpected number of socket activation fds")
		}

		source, err := listeners[0].Accept()
		if err != nil {
			log.Fatalln(err)
		}

		image := viper.GetString("container.image")
		if image == "" {
			log.Fatal("Invalid image")
		}

		c := container.New(
			viper.GetString("container.image"),
		)

		err = c.Run()
		if err != nil {
			log.Fatalln(err.Error())
		}

		port, err := c.FindPort()
		if err != nil {
			log.Fatalln(err.Error())
		}
		defer c.Stop()

		err = proxy.Start(source, fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			log.Fatalln(err.Error())
		}
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
