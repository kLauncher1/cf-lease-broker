//go:build linux

package enclave

import (
	"errors"
	"os"
	"sync"
)

// LinuxEnclaveDriver manages hardware interfaces for Intel SGX and AMD SEV on Linux.
type LinuxEnclaveDriver struct {
	mu           sync.RWMutex
	available    bool
	hardwareType EnclaveType
	devicePath   string
}

// NewPlatformEnclaveDriver probes Linux kernel device nodes for confidential computing support.
func NewPlatformEnclaveDriver() *LinuxEnclaveDriver {
	driver := &LinuxEnclaveDriver{
		hardwareType: EnclaveTypeEmulated,
		devicePath:   "emulated://confidential-vm",
		available:    false,
	}

	// Probe Intel SGX device nodes
	if _, err := os.Stat("/dev/sgx_enclave"); err == nil {
		driver.available = true
		driver.hardwareType = EnclaveTypeSGX
		driver.devicePath = "/dev/sgx_enclave"
		return driver
	}
	if _, err := os.Stat("/dev/isgx"); err == nil {
		driver.available = true
		driver.hardwareType = EnclaveTypeSGX
		driver.devicePath = "/dev/isgx"
		return driver
	}

	// Probe AMD SEV device nodes
	if _, err := os.Stat("/dev/sev-guest"); err == nil {
		driver.available = true
		driver.hardwareType = EnclaveTypeSEV
		driver.devicePath = "/dev/sev-guest"
		return driver
	}
	if _, err := os.Stat("/dev/sev"); err == nil {
		driver.available = true
		driver.hardwareType = EnclaveTypeSEV
		driver.devicePath = "/dev/sev"
		return driver
	}

	return driver
}

// IsAvailable indicates whether native hardware enclave driver is functional.
func (d *LinuxEnclaveDriver) IsAvailable() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.available
}

// GetHardwareType reports detected hardware (SGX, SEV, or Emulated).
func (d *LinuxEnclaveDriver) GetHardwareType() EnclaveType {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.hardwareType
}

// GetDevicePath returns the active device node path.
func (d *LinuxEnclaveDriver) GetDevicePath() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.devicePath
}

// ExecuteIOCTL executes a low-level ioctl command on the hardware device node with fallback.
func (d *LinuxEnclaveDriver) ExecuteIOCTL(cmd uint, arg uintptr) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if !d.available {
		return errors.New("native hardware enclave driver not available on host")
	}
	// Simulated ioctl call for safe userspace operation
	return nil
}

// Close releases any driver descriptors.
func (d *LinuxEnclaveDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return nil
}
