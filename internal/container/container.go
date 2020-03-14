package container

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Container contains the data related to container driver for containers
type Container struct {
	user  string
	id    string
	image string
}

// New returns a new container manager instance
func New(user string, image string) *Container {
	return &Container{
		user:  user,
		image: image,
	}
}

// Setup prepare the environment to run the container
func (c *Container) Setup() (err error) {
	return
}

// Run executes the container and return the id
func (c *Container) Run() (err error) {
	out, err := exec.Command(
		"su", "-l", c.user, "-c", fmt.Sprintf("podman run -dt --rm -P %s", c.image),
	).Output()
	if err != nil {
		return fmt.Errorf("failed running container with image %s: %s", err.Error(), c.image)
	}

	c.id = string(out)[0:13]

	return err
}

// FindPort lookup for the current port in use by the container
func (c *Container) FindPort() (port int, err error) {
	out, err := exec.Command(
		"su", "-l", c.user, "-c", fmt.Sprintf("podman port %s", c.id),
	).Output()
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
	return exec.Command(
		"su", "-l", c.user, "-c", "podman", "stop", c.id,
	).Run()
}
