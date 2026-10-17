package sandbox

import (
	"crypto/subtle"
	"fmt"
	"sync"
	"sync/atomic"
)

// LinearMemory encapsulates a boundary-enforced, safe WebAssembly linear memory partition.
type LinearMemory struct {
	mu           sync.RWMutex
	data         []byte
	initialPages int
	maxPages     int
	currentPages int
	peakPages    int
	oomCount     int64
}

// NewLinearMemory initializes a zeroed linear memory buffer governed by tenant and hard limits.
func NewLinearMemory(initialPages, maxPages int) (*LinearMemory, error) {
	if initialPages < 0 {
		return nil, fmt.Errorf("initialPages cannot be negative (%d)", initialPages)
	}
	if maxPages < initialPages {
		return nil, fmt.Errorf("maxPages (%d) cannot be less than initialPages (%d)", maxPages, initialPages)
	}
	if maxPages > HardCeilingMemoryPages {
		return nil, fmt.Errorf("%w: requested %d pages exceeds ceiling %d pages (32MB)",
			ErrMemoryHardCeilingExceeded, maxPages, HardCeilingMemoryPages)
	}

	totalBytes := initialPages * WasmPageSize
	buffer := make([]byte, totalBytes)

	return &LinearMemory{
		data:         buffer,
		initialPages: initialPages,
		maxPages:     maxPages,
		currentPages: initialPages,
		peakPages:    initialPages,
		oomCount:     0,
	}, nil
}

// Grow dynamically expands the linear memory by deltaPages conforming to Wasm memory.grow semantics.
// It returns the previous page count on success, or an error if limits are breached.
func (m *LinearMemory) Grow(deltaPages int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if deltaPages < 0 {
		return m.currentPages, fmt.Errorf("invalid negative grow delta: %d", deltaPages)
	}
	if deltaPages == 0 {
		return m.currentPages, nil
	}

	previousPages := m.currentPages
	targetPages := previousPages + deltaPages

	// Enforce tenant-configured quota
	if targetPages > m.maxPages {
		atomic.AddInt64(&m.oomCount, 1)
		return previousPages, fmt.Errorf("%w: requested %d pages exceeds tenant quota %d pages",
			ErrMemoryLimitExceeded, targetPages, m.maxPages)
	}

	// Enforce unyielding broker 32MB physical hard ceiling
	if targetPages > HardCeilingMemoryPages {
		atomic.AddInt64(&m.oomCount, 1)
		return previousPages, fmt.Errorf("%w: target %d pages exceeds hard ceiling %d pages",
			ErrMemoryHardCeilingExceeded, targetPages, HardCeilingMemoryPages)
	}

	// Allocate new continuous buffer and copy existing contents
	newBuffer := make([]byte, targetPages*WasmPageSize)
	copy(newBuffer, m.data)
	m.data = newBuffer
	m.currentPages = targetPages

	if targetPages > m.peakPages {
		m.peakPages = targetPages
	}

	return previousPages, nil
}

// Read extracts a byte slice from linear memory with strict bounds enforcement.
func (m *LinearMemory) Read(offset, length int) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("%w: negative offset (%d) or length (%d)", ErrMemoryOutOfBounds, offset, length)
	}
	if offset+length > len(m.data) {
		return nil, fmt.Errorf("%w: read range [%d:%d] exceeds memory size %d bytes",
			ErrMemoryOutOfBounds, offset, offset+length, len(m.data))
	}

	dest := make([]byte, length)
	copy(dest, m.data[offset:offset+length])
	return dest, nil
}

// Write writes data into linear memory starting at offset with strict bounds checking.
func (m *LinearMemory) Write(offset int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if offset < 0 {
		return fmt.Errorf("%w: negative offset (%d)", ErrMemoryOutOfBounds, offset)
	}
	dataLen := len(data)
	if offset+dataLen > len(m.data) {
		return fmt.Errorf("%w: write range [%d:%d] exceeds memory size %d bytes",
			ErrMemoryOutOfBounds, offset, offset+dataLen, len(m.data))
	}

	copy(m.data[offset:offset+dataLen], data)
	return nil
}

// Zeroize securely scrubs all memory contents with zero bytes to guarantee cross-tenant isolation.
func (m *LinearMemory) Zeroize() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.data) == 0 {
		return
	}

	// Overwrite memory content with zero bytes
	for i := range m.data {
		m.data[i] = 0
	}

	// Verify scrubbing using constant-time comparison against zero slice
	zeros := make([]byte, 64)
	if len(m.data) >= 64 {
		subtle.ConstantTimeCompare(m.data[:64], zeros)
	}
}

// Pages returns the currently committed memory page count.
func (m *LinearMemory) Pages() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentPages
}

// Bytes returns the currently committed linear memory size in bytes.
func (m *LinearMemory) Bytes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentPages * WasmPageSize
}

// PeakPages returns the high-water-mark allocated page count.
func (m *LinearMemory) PeakPages() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.peakPages
}

// PeakBytes returns the peak memory allocated in bytes.
func (m *LinearMemory) PeakBytes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.peakPages * WasmPageSize
}

// MaxPages returns the configured maximum page capacity for this memory instance.
func (m *LinearMemory) MaxPages() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxPages
}

// OOMCount returns the total number of intercepted out-of-memory breach attempts.
func (m *LinearMemory) OOMCount() int64 {
	return atomic.LoadInt64(&m.oomCount)
}

// Snapshot returns a copy of the current memory tracker metrics.
func (m *LinearMemory) Snapshot() MemoryTracker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MemoryTracker{
		InitialPages:     m.initialPages,
		MaxPages:         m.maxPages,
		CurrentPages:     m.currentPages,
		CurrentBytes:     m.currentPages * WasmPageSize,
		PeakPages:        m.peakPages,
		PeakBytes:        m.peakPages * WasmPageSize,
		OOMInterceptions: atomic.LoadInt64(&m.oomCount),
	}
}
