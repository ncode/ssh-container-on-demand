//go:build cgo

package microvm

import (
	"math"
	"testing"
)

func TestResolveResources(t *testing.T) {
	for _, test := range []struct {
		name      string
		resources Resources
		rootfs    uint64
		memory    uint64
		wantError bool
	}{
		{"small RAM root", Resources{CPUs: 1}, 1 << 20, 512, false},
		{"large RAM root", Resources{CPUs: 1}, 255 << 20, 766, false},
		{"disk root", Resources{CPUs: 1, DiskSizeMiB: 512}, 255 << 20, 512, false},
		{"explicit memory", Resources{CPUs: 1, MemoryMiB: 768}, 1 << 20, 768, false},
		{"zero CPUs", Resources{}, 1 << 20, 0, true},
		{"insufficient memory", Resources{CPUs: 1, MemoryMiB: 512}, 255 << 20, 0, true},
		{"insufficient disk", Resources{CPUs: 1, DiskSizeMiB: 128}, 255 << 20, 0, true},
		{"disk above limit", Resources{CPUs: 1, DiskSizeMiB: 65537}, 1 << 20, 0, true},
		{"oversize memory", Resources{CPUs: 1, MemoryMiB: math.MaxUint64}, 1 << 20, 0, true},
		{"oversize CPUs", Resources{CPUs: math.MaxUint64}, 1 << 20, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.resources.resolve(test.rootfs)
			if test.wantError {
				if err == nil {
					t.Fatalf("accepted %+v", test.resources)
				}
				return
			}
			if err != nil || got.MemoryMiB != test.memory {
				t.Fatalf("resolved = %+v, error = %v; want memory %d", got, err, test.memory)
			}
		})
	}
}
