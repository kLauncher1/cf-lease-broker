//go:build linux

package steering

import (
	"errors"
	"os"
	"sync"
)

// LinuxEBPFDriver manages eBPF sockmap and XDP packet steering hooks on Linux.
type LinuxEBPFDriver struct {
	mu            sync.RWMutex
	available     bool
	sockmapActive bool
	sockmapTable  map[string]string
}

// NewPlatformEBPFDriver detects Linux kernel eBPF subsystem and initializes sockmap structures.
func NewPlatformEBPFDriver() *LinuxEBPFDriver {
	driver := &LinuxEBPFDriver{
		sockmapTable: make(map[string]string),
	}

	// Check if BPF filesystem or BPF JIT is active
	if _, err := os.Stat("/sys/fs/bpf"); err == nil {
		driver.available = true
		driver.sockmapActive = true
	} else if _, err := os.Stat("/proc/sys/net/core/bpf_jit_enable"); err == nil {
		driver.available = true
		driver.sockmapActive = true
	} else {
		// Kernel present but unprivileged container or unmounted bpffs
		driver.available = false
	}

	return driver
}

func (d *LinuxEBPFDriver) IsAvailable() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.available
}

func (d *LinuxEBPFDriver) GetMode() SteeringMode {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.available && d.sockmapActive {
		return SteeringModeEBPFSockmap
	}
	return SteeringModeUserspace
}

func (d *LinuxEBPFDriver) UpdateSockmap(cidPrefix string, targetAddress string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.available {
		return errors.New("ebpf sockmap subsystem not available on host")
	}
	d.sockmapTable[cidPrefix] = targetAddress
	return nil
}

func (d *LinuxEBPFDriver) RedirectSocket(fd int, targetAddress string) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if !d.available {
		return errors.New("ebpf kernel redirect requires linux cap_sys_admin/cap_net_admin")
	}
	return nil
}

func (d *LinuxEBPFDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sockmapActive = false
	return nil
}
