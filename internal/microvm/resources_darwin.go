//go:build cgo

package microvm

import (
	"errors"
	"fmt"
	"os"

	"github.com/Code-Hex/vz/v3"
)

const mib = 1 << 20
const maxRootFSBytes = 1 << 30

type Resources struct {
	CPUs, MemoryMiB, DiskSizeMiB uint64
}

func rootFSSize(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("cached root filesystem: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxRootFSBytes || info.Size()%512 != 0 {
		return 0, errors.New("invalid cached root filesystem archive")
	}
	return uint64(info.Size()), nil
}

func (r Resources) resolve(rootfsBytes uint64) (Resources, error) {
	minCPU := uint64(vz.VirtualMachineConfigurationMinimumAllowedCPUCount())
	maxCPU := uint64(vz.VirtualMachineConfigurationMaximumAllowedCPUCount())
	if r.CPUs < minCPU || r.CPUs > maxCPU {
		return Resources{}, fmt.Errorf("microvm.cpus must be between %d and %d on this Mac", minCPU, maxCPU)
	}
	minMemory := max(uint64(512), (vz.VirtualMachineConfigurationMinimumAllowedMemorySize()+mib-1)/mib)
	if r.DiskSizeMiB == 0 {
		minMemory = max(minMemory, (2*rootfsBytes+256*mib+mib-1)/mib)
	} else {
		minDisk := (rootfsBytes+mib-1)/mib + 128
		if r.DiskSizeMiB < minDisk || r.DiskSizeMiB > 64*1024 {
			return Resources{}, fmt.Errorf("microvm.disk_size_mib must be 0 or between %d and %d for this image", minDisk, 64*1024)
		}
	}
	if r.MemoryMiB == 0 {
		r.MemoryMiB = minMemory
	}
	maxMemory := vz.VirtualMachineConfigurationMaximumAllowedMemorySize() / mib
	if r.MemoryMiB < minMemory || r.MemoryMiB > maxMemory {
		return Resources{}, fmt.Errorf("microvm.memory_mib must be 0 or between %d and %d with this image and disk setting", minMemory, maxMemory)
	}
	return r, nil
}
