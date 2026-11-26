# ssh container on demand

Builds use Go modules and require Go 1.25.7 or later. The first build may
download dependencies.

## macOS: disposable Linux microVMs

On Apple silicon macOS 13 or later, the `serve` command starts a disposable
Linux ARM64 microVM for each accepted TCP connection. It uses Apple's
Virtualization.framework directly. The signed project binary starts its own
worker process for each guest.

### Quick start

With Xcode or the Command Line Tools installed, run from the repository root:

```sh
make
cp config.example.yaml config.yaml
# Edit config.yaml and set the public key and kernel paths.
./ssh-container-on-demand --config config.yaml serve
```

If you do not have an SSH key yet, create one with `ssh-keygen -t ed25519`.
Set `microvm.image` to a Linux ARM64 OCI image containing `/bin/sh`, and set
`microvm.ssh_public_key` to your public key file. Set `microvm.kernel.path` to
an uncompressed ARM64 Linux Image with built-in initramfs, virtio PCI, block,
console and vsock, devtmpfs, tmpfs, procfs, sysfs, and Unix PTY support. A
[Firecracker CI kernel candidate](https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/20261125-454a9e84e707-0/aarch64/vmlinux-6.18.62)
has been tested on Apple silicon. Firecracker publishes it for demonstration
use; the service does not download it automatically or run Firecracker.

`make` builds and embeds the Linux guest bootstrap and signs the macOS binary.
First startup pulls and caches the flattened OCI root filesystem; image tags
are resolved on each startup and cached by digest under
`~/Library/Caches/ssh-container-on-demand`. The guest uses a Go SSH server
over vsock; no Debian runner, cloud-init or OpenSSH server is required.

In another terminal:

```sh
ssh -i "$HOME/.ssh/id_ed25519" -p 2222 guest@127.0.0.1
```

The flattened root filesystem is limited to 1 GiB. The guest shell runs as the
non-root `guest` user (UID 1000) inside the image root filesystem. The guest
has provisioned `/etc/passwd` and `/etc/group` records, a writable, disposable
home directory and login working directory at `/home/guest`, and hostname
`sshvm`. OCI entrypoint and command are not run. The SSH host key is stable per
local user cache and differs from the previous Debian-based implementation;
verify it before trusting it.

The default listener is `127.0.0.1:2222`, with at most two simultaneous
connections; excess connections are closed. Set `--listen`,
`--max-connections`, and `--boot-timeout` (default `30s`) as needed. A VM starts
before SSH authentication: do not expose the listener directly to untrusted
clients. SSH multiplexing over one TCP connection reuses its VM.

Each guest reads the shared immutable root filesystem archive and extracts it
into private RAM, with no guest network or host directory mounts. Guest writes
are discarded when its connection ends or the server shuts down normally
(SIGINT or SIGTERM). VM memory grows with the size of the flattened image.
The SSH chroot has read-only guest `/proc` and `/sys` mounts; `/proc` shows
only processes visible to the logged-in user (`hidepid=2`).

## Linux: planned Cloud Hypervisor backend

Serving SSH is currently supported only on macOS. The planned Linux backend is
[Cloud Hypervisor](https://www.cloudhypervisor.org/docs/prologue/quick-start/)
on a host with access to `/dev/kvm`.

1. Reuse the per-connection listener, root filesystem archive, Go init and
   cleanup flow. Start one Cloud Hypervisor process without a guest NIC or host
   mounts.
2. Forward SSH to guest port 22 over [Cloud Hypervisor vsock](https://github.com/cloud-hypervisor/cloud-hypervisor/blob/main/docs/vsock.md).
   Send `CONNECT 22` followed by a newline and consume the `OK` reply before
   relaying SSH. Reuse the OCI preparation path and `microvm.image` configuration.
3. Verify two independent SSH connections, discarded guest writes, and VM
   cleanup on disconnect on a KVM host before adding systemd units.
