//go:build !linux

package enclave

import (
	"sync"
)

// PlatformEnclaveDriver provides a cross-platform confidential computing emulation driver for Windows/macOS.
type PlatformEnclaveDriver struct {
	mu           sync.RWMutex
	available    bool
	hardwareType EnclaveType
	devicePath   string
}

// NewPlatformEnclaveDriver returns an active confidential emulation driver.
func NewPlatformEnclaveDriver() *PlatformEnclaveDriver {
	return &PlatformEnclaveDriver{
		available:    false,
		hardwareType: EnclaveTypeEmulated,
		devicePath:   "emulated://confidential-vm",
	}
}

// IsAvailable indicates whether hardware acceleration is present.
func (d *PlatformEnclaveDriver) IsAvailable() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.available
}

// GetHardwareType reports detected hardware (returns Emulated on non-Linux).
func (d *PlatformEnclaveDriver) GetHardwareType() EnclaveType {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.hardwareType
}

// GetDevicePath returns the virtual device identifier.
func (d *PlatformEnclaveDriver) GetDevicePath() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.devicePath
}

// ExecuteIOCTL simulates an enclave ioctl command in userspace emulation.
func (d *PlatformEnclaveDriver) ExecuteIOCTL(cmd uint, arg uintptr) error {
	return nil
}

// Close releases simulated driver resources.
func (d *PlatformEnclaveDriver) Close() error {
	return nil
}
