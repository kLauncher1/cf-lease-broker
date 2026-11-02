//go:build !linux

package scrubber

import (
	"sync"
)

// PlatformScrubberDriver provides a cross-platform line-rate emulation driver for non-Linux platforms.
type PlatformScrubberDriver struct {
	mu            sync.RWMutex
	mode          string
	attachedIfaces map[string]bool
}

// NewPlatformScrubberDriver initializes an in-memory high-throughput userspace driver.
func NewPlatformScrubberDriver() *PlatformScrubberDriver {
	return &PlatformScrubberDriver{
		mode:          "xdp_emulated_userspace",
		attachedIfaces: make(map[string]bool),
	}
}

// IsAvailable returns false for kernel XDP on non-Linux architectures, signaling userspace operation.
func (d *PlatformScrubberDriver) IsAvailable() bool {
	return false
}

// GetMode returns the userspace emulation mode.
func (d *PlatformScrubberDriver) GetMode() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.mode
}

// AttachInterface records the interface into the simulated attached map.
func (d *PlatformScrubberDriver) AttachInterface(iface string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.attachedIfaces[iface] = true
	return nil
}

// DetachInterface removes the interface from the attached map.
func (d *PlatformScrubberDriver) DetachInterface(iface string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.attachedIfaces, iface)
	return nil
}

// InspectFastPath handles userspace emulation packet pass-through.
func (d *PlatformScrubberDriver) InspectFastPath(pkt PacketMetadata) (Action, bool) {
	return ActionPass, false
}

// Close cleans up simulated driver resources.
func (d *PlatformScrubberDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.attachedIfaces = make(map[string]bool)
	return nil
}
