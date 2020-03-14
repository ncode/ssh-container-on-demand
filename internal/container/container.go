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
package container

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Container contains the data related to container driver for containers
type Container struct {
	id    string
	image string
}

// New returns a new container manager instance
func New(image string) *Container {
	return &Container{
		image: image,
	}
}

// Setup prepare the environment to run the container
func (c *Container) Setup() (err error) {
	return
}

// Run executes the container and return the id
func (c *Container) Run() (err error) {
	out, err := exec.Command("podman", "run", "-dt", "--rm", "-P", c.image).Output()
	if err != nil {
		return fmt.Errorf("failed running container with image %s: %s", err.Error(), c.image)
	}

	c.id = string(out)[0:13]

	return err
}

// FindPort lookup for the current port in use by the container
func (c *Container) FindPort() (port int, err error) {
	out, err := exec.Command("podman", "port", c.id).Output()
	if err != nil {
		return port, fmt.Errorf("failed finding port of container id %s: %s", c.id, err.Error())

	}

	p := strings.Split(strings.Trim(string(out), "\n"), ":")[1]
	port, err = strconv.Atoi(p)
	if err != nil {
		return port, fmt.Errorf("failed converting port of container id %s: %s", c.id, err.Error())
	}

	return port, err
}

// Stop kill the current running container and destroy it's content
func (c *Container) Stop() (err error) {
	return exec.Command("podman", "stop", c.id).Run()
}
