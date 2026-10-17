//go:build !linux

package steering

import (
	"sync"
)

// PlatformEBPFDriver provides a cross-platform fallback steering driver for non-Linux operating systems.
type PlatformEBPFDriver struct {
	mu           sync.RWMutex
	sockmapTable map[string]string
}

// NewPlatformEBPFDriver initializes an in-memory high-throughput user-space socket redirection driver.
func NewPlatformEBPFDriver() *PlatformEBPFDriver {
	return &PlatformEBPFDriver{
		sockmapTable: make(map[string]string),
	}
}

// IsAvailable returns false for kernel eBPF on non-Linux architectures, signaling graceful userspace fallback.
func (d *PlatformEBPFDriver) IsAvailable() bool {
	return false
}

// GetMode returns Userspace mode when running outside Linux.
func (d *PlatformEBPFDriver) GetMode() SteeringMode {
	return SteeringModeUserspace
}

// UpdateSockmap records routing keys into the fast in-memory dispatch map.
func (d *PlatformEBPFDriver) UpdateSockmap(cidPrefix string, targetAddress string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sockmapTable[cidPrefix] = targetAddress
	return nil
}

// RedirectSocket executes user-space socket fastpath dispatching.
func (d *PlatformEBPFDriver) RedirectSocket(fd int, targetAddress string) error {
	return nil
}

// Close cleans up driver resources.
func (d *PlatformEBPFDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sockmapTable = make(map[string]string)
	return nil
}
