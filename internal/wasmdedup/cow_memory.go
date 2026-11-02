package wasmdedup

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// CoWLinearMemory implements a Copy-on-Write page-table backed WebAssembly linear memory.
type CoWLinearMemory struct {
	mu            sync.RWMutex
	instanceID    string
	tenantID      string
	pool          *PagePool
	pageTable     []*PhysicalPage
	maxPages      int
	cowFaultCount int64
}

// NewCoWLinearMemory creates a new zero-initialized linear memory backed by the canonical zero page.
func NewCoWLinearMemory(instanceID, tenantID string, initialPages, maxPages int, pool *PagePool) (*CoWLinearMemory, error) {
	if pool == nil {
		return nil, ErrNilBuffer
	}
	if initialPages < 0 {
		return nil, fmt.Errorf("initial pages cannot be negative (%d)", initialPages)
	}
	if maxPages < initialPages {
		return nil, fmt.Errorf("max pages (%d) cannot be less than initial pages (%d)", maxPages, initialPages)
	}
	if maxPages > HardCeilingPages {
		return nil, fmt.Errorf("%w: requested %d pages exceeds broker ceiling %d pages (32MB)",
			ErrHardCeilingExceeded, maxPages, HardCeilingPages)
	}

	table := make([]*PhysicalPage, initialPages)
	for i := 0; i < initialPages; i++ {
		table[i] = pool.GetZeroPage()
	}

	return &CoWLinearMemory{
		instanceID: instanceID,
		tenantID:   tenantID,
		pool:       pool,
		pageTable:  table,
		maxPages:   maxPages,
	}, nil
}

// NewFromSharedPages initializes an instance sharing a set of base template physical pages.
func NewFromSharedPages(instanceID, tenantID string, basePages []*PhysicalPage, maxPages int, pool *PagePool) (*CoWLinearMemory, error) {
	if pool == nil {
		return nil, ErrNilBuffer
	}
	initialPages := len(basePages)
	if maxPages < initialPages {
		return nil, fmt.Errorf("max pages (%d) cannot be less than base pages (%d)", maxPages, initialPages)
	}
	if maxPages > HardCeilingPages {
		return nil, fmt.Errorf("%w: requested %d pages exceeds broker ceiling %d pages",
			ErrHardCeilingExceeded, maxPages, HardCeilingPages)
	}

	table := make([]*PhysicalPage, initialPages)
	for i, page := range basePages {
		page.IncRef()
		table[i] = page
	}

	return &CoWLinearMemory{
		instanceID: instanceID,
		tenantID:   tenantID,
		pool:       pool,
		pageTable:  table,
		maxPages:   maxPages,
	}, nil
}

// Read extracts a byte slice from linear memory with boundary enforcement across page tables.
func (m *CoWLinearMemory) Read(offset, length int) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("%w: negative offset (%d) or length (%d)", ErrOutOfBounds, offset, length)
	}
	if length == 0 {
		return []byte{}, nil
	}

	totalBytes := len(m.pageTable) * WasmPageSize
	if offset+length > totalBytes {
		return nil, fmt.Errorf("%w: read range [%d:%d] exceeds memory size %d bytes",
			ErrOutOfBounds, offset, offset+length, totalBytes)
	}

	out := make([]byte, length)
	bytesRead := 0

	for bytesRead < length {
		currentOffset := offset + bytesRead
		pageIdx := currentOffset / WasmPageSize
		pageOff := currentOffset % WasmPageSize

		remainingInPage := WasmPageSize - pageOff
		toRead := length - bytesRead
		if toRead > remainingInPage {
			toRead = remainingInPage
		}

		page := m.pageTable[pageIdx]
		copy(out[bytesRead:bytesRead+toRead], page.Data[pageOff:pageOff+toRead])
		bytesRead += toRead
	}

	return out, nil
}

// Write applies data into linear memory, triggering Copy-on-Write faults for shared pages.
func (m *CoWLinearMemory) Write(offset int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	length := len(data)
	if offset < 0 || length < 0 {
		return fmt.Errorf("%w: negative offset (%d) or length (%d)", ErrOutOfBounds, offset, length)
	}
	if length == 0 {
		return nil
	}

	totalBytes := len(m.pageTable) * WasmPageSize
	if offset+length > totalBytes {
		return fmt.Errorf("%w: write range [%d:%d] exceeds memory size %d bytes",
			ErrOutOfBounds, offset, offset+length, totalBytes)
	}

	bytesWritten := 0
	for bytesWritten < length {
		currentOffset := offset + bytesWritten
		pageIdx := currentOffset / WasmPageSize
		pageOff := currentOffset % WasmPageSize

		remainingInPage := WasmPageSize - pageOff
		toWrite := length - bytesWritten
		if toWrite > remainingInPage {
			toWrite = remainingInPage
		}

		currentPage := m.pageTable[pageIdx]

		// Copy-on-Write: If the page is shared (refCount > 1 or IsShared), make a private mutable copy
		if currentPage.IsShared || currentPage.GetRefCount() > 1 || currentPage.IsZero {
			atomic.AddInt64(&m.cowFaultCount, 1)

			privatePage := m.pool.AllocPrivatePage(currentPage.Data)
			m.pool.ReleasePage(currentPage)
			m.pageTable[pageIdx] = privatePage
			currentPage = privatePage
		}

		copy(currentPage.Data[pageOff:pageOff+toWrite], data[bytesWritten:bytesWritten+toWrite])
		bytesWritten += toWrite
	}

	return nil
}

// Grow dynamically expands linear memory by deltaPages conforming to Wasm memory.grow.
func (m *CoWLinearMemory) Grow(deltaPages int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if deltaPages < 0 {
		return len(m.pageTable), ErrNegativeDelta
	}
	if deltaPages == 0 {
		return len(m.pageTable), nil
	}

	prevPages := len(m.pageTable)
	targetPages := prevPages + deltaPages

	if targetPages > m.maxPages {
		return prevPages, fmt.Errorf("%w: target %d pages exceeds tenant quota %d pages",
			ErrMemoryExceeded, targetPages, m.maxPages)
	}
	if targetPages > HardCeilingPages {
		return prevPages, fmt.Errorf("%w: target %d pages exceeds broker hard ceiling %d pages (32MB)",
			ErrHardCeilingExceeded, targetPages, HardCeilingPages)
	}

	for i := 0; i < deltaPages; i++ {
		m.pageTable = append(m.pageTable, m.pool.GetZeroPage())
	}

	return prevPages, nil
}

// PageCount returns the current number of linear memory pages.
func (m *CoWLinearMemory) PageCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pageTable)
}

// ByteCount returns the total virtual size in bytes.
func (m *CoWLinearMemory) ByteCount() int {
	return m.PageCount() * WasmPageSize
}

// SharedPageCount returns the number of pages currently shared with the pool.
func (m *CoWLinearMemory) SharedPageCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, page := range m.pageTable {
		if page.IsShared || page.IsZero {
			count++
		}
	}
	return count
}

// PrivatePageCount returns the number of private mutable pages owned exclusively by this instance.
func (m *CoWLinearMemory) PrivatePageCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, page := range m.pageTable {
		if !page.IsShared && !page.IsZero {
			count++
		}
	}
	return count
}

// CoWFaults returns the count of Copy-on-Write faults triggered on this instance.
func (m *CoWLinearMemory) CoWFaults() int64 {
	return atomic.LoadInt64(&m.cowFaultCount)
}

// SnapshotPages extracts a safe snapshot copy of page references, incrementing reference counts.
func (m *CoWLinearMemory) SnapshotPages() []*PhysicalPage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pages := make([]*PhysicalPage, len(m.pageTable))
	for i, p := range m.pageTable {
		p.IncRef()
		pages[i] = p
	}
	return pages
}

// Release releases all physical page references held by this instance.
func (m *CoWLinearMemory) Release() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.pageTable {
		m.pool.ReleasePage(p)
	}
	m.pageTable = nil
}
