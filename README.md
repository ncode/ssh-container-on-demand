# ssh container on demand

### What is it? 

Small utility to trigger dynamic allocation of ssh rootless containers using podman started by systemd socket activation.

### How it works?

When a connection to a defined port in ssh-container-on-demand.socket will trigger the activation of ssh-container-on-demand.server passing the incoming socket fd to ssh-container-on-demand. Based on it's configuration file it will spin up a specific defined image using podman running ssh, will get the allocated port from this container and proxy the incoming ssh connection to this just spawned container.

```
ssh -o Port=2222 host
\_ ssh-container-on-demand.socket
  \_ ssh-container-on-demand.service
    \_ ssh-container-on-demand
      \_ podman start container
        \_ $ shell in your newly spawned container
```

### Why would I want that?

I've no idea, but I thought it would be fun to implement.

### How to use?

I've only tested it with Centos8, but any platform that can run rootless podman should work without issues.

```
$ dnf install -y podman
$ git clone https://github.com/ncode/ssh-container-on-demand.git
$ cd ssh-container-on-demand
$ go build
$ adduser podman
$ cp init/* /etc/systemd/system/
$ cp ssh-container-on-demand /usr/local/bin
$ systemctl daemon-reload
```

Now you need to create a ~podman/.ssh-container-on-demand.yaml with the container image you would like to use

```yaml
container:
  image: my.super.ssh.container.image
```
