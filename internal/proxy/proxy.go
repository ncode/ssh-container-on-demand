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
package proxy

import (
	"io"
	"net"
)

func copy(src net.Conn, dst net.Conn, e chan<- error) {
	_, err := io.Copy(src, dst)
	e <- err
}

// Start receives a socket from system, creates a connection to
// the destination container and proxy the communication
func Start(source net.Conn, containerAddress string) (err error) {
	destination, err := net.Dial("tcp", containerAddress)
	if err != nil {
		return err
	}

	defer source.Close()
	defer destination.Close()

	// Copy all data from source socket to
	// destination socket
	sourceToDestination := make(chan error)
	go copy(source, destination, sourceToDestination)

	// Copy all data from destination socket to
	// source socket and block
	destinationToSource := make(chan error)
	go copy(destination, source, destinationToSource)

	for {
		select {
		case err := <-sourceToDestination:
			return err
		case err := <-destinationToSource:
			return err
		}
	}
}
