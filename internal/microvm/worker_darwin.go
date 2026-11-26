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
package microvm

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Code-Hex/vz/v3"
	"github.com/ncode/ssh-container-on-demand/internal/proxy"
)

// RunWorker owns a single VM until it stops or the parent reaps the worker.
func RunWorker(dir, kernel, initrd, rootfs string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cfg, err := workerConfiguration(dir, kernel, initrd, rootfs)
	if err != nil {
		return fmt.Errorf("configure Apple Virtualization VM: %w", err)
	}
	vm, err := vz.NewVirtualMachine(cfg)
	if err != nil {
		return fmt.Errorf("create Apple Virtualization VM: %w", err)
	}
	if err := vm.Start(); err != nil {
		return fmt.Errorf("start Apple Virtualization VM: %w", err)
	}
	defer func() {
		if vm.CanStop() {
			_ = vm.Stop()
		}
	}()
	listener, err := exposeSSH(vm, filepath.Join(dir, "ssh.sock"))
	if err != nil {
		return fmt.Errorf("expose SSH vsock: %w", err)
	}
	defer listener.Close()
	for state := range vm.StateChangedNotify() {
		switch state {
		case vz.VirtualMachineStateStopped:
			return nil
		case vz.VirtualMachineStateError:
			return errors.New("Apple Virtualization VM failed")
		}
	}
	return errors.New("lost Apple Virtualization VM state notifications")
}

func workerConfiguration(dir, kernel, initrd, rootfs string) (*vz.VirtualMachineConfiguration, error) {
	boot, err := vz.NewLinuxBootLoader(kernel, vz.WithInitrd(initrd), vz.WithCommandLine("console=hvc0 rdinit=/init panic=1"))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(rootfs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<30 || info.Size()%512 != 0 {
		return nil, errors.New("invalid OCI root filesystem archive")
	}
	memory := max(uint64(512<<20), 2*uint64(info.Size())+(256<<20))
	memory = (memory + (1 << 20) - 1) & ^uint64((1<<20)-1)
	cfg, err := vz.NewVirtualMachineConfiguration(boot, 1, memory)
	if err != nil {
		return nil, err
	}
	id, err := vz.NewGenericMachineIdentifier()
	if err != nil {
		return nil, err
	}
	platform, err := vz.NewGenericPlatformConfiguration(vz.WithGenericMachineIdentifier(id))
	if err != nil {
		return nil, err
	}
	cfg.SetPlatformVirtualMachineConfiguration(platform)

	disk, err := diskDevice(rootfs, true)
	if err != nil {
		return nil, err
	}
	cfg.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{disk})
	rng, err := vz.NewVirtioEntropyDeviceConfiguration()
	if err != nil {
		return nil, err
	}
	cfg.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{rng})
	sock, err := vz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		return nil, err
	}
	cfg.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{sock})
	attachment, err := vz.NewFileSerialPortAttachment(filepath.Join(dir, "serial.log"), false)
	if err != nil {
		return nil, err
	}
	console, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(attachment)
	if err != nil {
		return nil, err
	}
	cfg.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{console})
	valid, err := cfg.Validate()
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, errors.New("invalid configuration")
	}
	return cfg, nil
}

func diskDevice(path string, readOnly bool) (*vz.VirtioBlockDeviceConfiguration, error) {
	attachment, err := vz.NewDiskImageStorageDeviceAttachmentWithCacheAndSync(path, readOnly,
		vz.DiskImageCachingModeCached, vz.DiskImageSynchronizationModeFsync)
	if err != nil {
		return nil, err
	}
	return vz.NewVirtioBlockDeviceConfiguration(attachment)
}

func exposeSSH(vm *vz.VirtualMachine, socket string) (net.Listener, error) {
	devices := vm.SocketDevices()
	if len(devices) != 1 {
		return nil, fmt.Errorf("expected one virtio-vsock device, got %d", len(devices))
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			guest, err := devices[0].Connect(22)
			if err != nil || guest == nil {
				client.Close()
				continue
			}
			go proxy.Bridge(client, guest)
		}
	}()
	return listener, nil
}
