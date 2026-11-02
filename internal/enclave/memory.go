package enclave

import (
	"crypto/subtle"
	"fmt"
	"runtime"
	"sync"
)

// Zeroize securely scrubs a byte slice in place, ensuring compiler optimizations
// do not eliminate the overwrite.
func Zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(b)
}

// ConstantTimeCompare wraps subtle.ConstantTimeCompare for tamper detection.
func ConstantTimeCompare(x, y []byte) bool {
	return subtle.ConstantTimeCompare(x, y) == 1
}

// EnclaveMemoryPool manages hardware-isolated Enclave Page Cache (EPC) memory.
type EnclaveMemoryPool struct {
	mu             sync.RWMutex
	capacityBytes  int64
	pageSize       int
	totalPages     int
	allocatedPages int
	rawMemory      []byte
	pages          []EnclavePage
	tenantAlloc    map[string]map[int]bool
}

// NewEnclaveMemoryPool initializes an isolated EPC memory pool.
func NewEnclaveMemoryPool(capacityMB, pageSizeKB int) *EnclaveMemoryPool {
	if capacityMB <= 0 {
		capacityMB = 128
	}
	if pageSizeKB <= 0 {
		pageSizeKB = 4
	}

	pageSize := pageSizeKB * 1024
	capacityBytes := int64(capacityMB) * 1024 * 1024
	totalPages := int(capacityBytes / int64(pageSize))

	raw := make([]byte, capacityBytes)
	pages := make([]EnclavePage, totalPages)
	for i := 0; i < totalPages; i++ {
		pages[i] = EnclavePage{
			PageID:      i,
			Address:     uint64(i * pageSize),
			Size:        pageSize,
			IsAllocated: false,
			IsZeroed:    true,
			TenantID:    "",
		}
	}

	return &EnclaveMemoryPool{
		capacityBytes:  capacityBytes,
		pageSize:       pageSize,
		totalPages:     totalPages,
		allocatedPages: 0,
		rawMemory:      raw,
		pages:          pages,
		tenantAlloc:    make(map[string]map[int]bool),
	}
}

// Allocate reserves continuous or segmented EPC pages for a specified tenant.
func (p *EnclaveMemoryPool) Allocate(tenantID string, count int) ([]int, error) {
	if count <= 0 {
		return nil, fmt.Errorf("page count must be positive")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.allocatedPages+count > p.totalPages {
		return nil, ErrEnclaveOutOfMemory
	}

	allocated := make([]int, 0, count)
	for i := 0; i < p.totalPages && len(allocated) < count; i++ {
		if !p.pages[i].IsAllocated {
			p.pages[i].IsAllocated = true
			p.pages[i].IsZeroed = false
			p.pages[i].TenantID = tenantID
			allocated = append(allocated, i)
		}
	}

	if len(allocated) < count {
		// Roll back on unexpected shortage
		for _, id := range allocated {
			p.pages[id].IsAllocated = false
			p.pages[id].TenantID = ""
		}
		return nil, ErrEnclaveOutOfMemory
	}

	p.allocatedPages += count
	if _, ok := p.tenantAlloc[tenantID]; !ok {
		p.tenantAlloc[tenantID] = make(map[int]bool)
	}
	for _, id := range allocated {
		p.tenantAlloc[tenantID][id] = true
	}

	return allocated, nil
}

// Free deallocates pages belonging to tenant, immediately zeroizing their content.
func (p *EnclaveMemoryPool) Free(tenantID string, pageIDs []int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tPages, exists := p.tenantAlloc[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s has no allocated EPC pages", tenantID)
	}

	for _, id := range pageIDs {
		if id < 0 || id >= p.totalPages {
			return ErrOutOfBounds
		}
		if !tPages[id] || p.pages[id].TenantID != tenantID {
			return fmt.Errorf("page %d is not owned by tenant %s", id, tenantID)
		}

		// Hardware-mode scrub: overwrite page memory with zeros
		offset := id * p.pageSize
		Zeroize(p.rawMemory[offset : offset+p.pageSize])

		p.pages[id].IsAllocated = false
		p.pages[id].IsZeroed = true
		p.pages[id].TenantID = ""
		delete(tPages, id)
		p.allocatedPages--
	}

	if len(tPages) == 0 {
		delete(p.tenantAlloc, tenantID)
	}

	return nil
}

// Write writes confidential data into a tenant's allocated EPC page with boundary enforcement.
func (p *EnclaveMemoryPool) Write(tenantID string, pageID int, offset int, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pageID < 0 || pageID >= p.totalPages {
		return ErrOutOfBounds
	}
	page := &p.pages[pageID]
	if !page.IsAllocated || page.TenantID != tenantID {
		return fmt.Errorf("unauthorized memory write: page %d not allocated to tenant %s", pageID, tenantID)
	}

	if offset < 0 || offset+len(data) > p.pageSize {
		return ErrOutOfBounds
	}

	rawOffset := pageID*p.pageSize + offset
	copy(p.rawMemory[rawOffset:rawOffset+len(data)], data)
	return nil
}

// Read reads confidential data from a tenant's allocated EPC page with boundary enforcement.
func (p *EnclaveMemoryPool) Read(tenantID string, pageID int, offset int, length int) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if pageID < 0 || pageID >= p.totalPages {
		return nil, ErrOutOfBounds
	}
	page := &p.pages[pageID]
	if !page.IsAllocated || page.TenantID != tenantID {
		return nil, fmt.Errorf("unauthorized memory read: page %d not allocated to tenant %s", pageID, tenantID)
	}

	if offset < 0 || length < 0 || offset+length > p.pageSize {
		return nil, ErrOutOfBounds
	}

	out := make([]byte, length)
	rawOffset := pageID*p.pageSize + offset
	copy(out, p.rawMemory[rawOffset:rawOffset+length])
	return out, nil
}

// Stats returns current memory metrics.
func (p *EnclaveMemoryPool) Stats() (totalBytes, allocatedBytes int64, pageCount, allocatedPages int) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.capacityBytes, int64(p.allocatedPages * p.pageSize), p.totalPages, p.allocatedPages
}

// Reset zeroizes the entire memory pool and clears all tenant allocations.
func (p *EnclaveMemoryPool) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	Zeroize(p.rawMemory)
	for i := 0; i < p.totalPages; i++ {
		p.pages[i].IsAllocated = false
		p.pages[i].IsZeroed = true
		p.pages[i].TenantID = ""
	}
	p.allocatedPages = 0
	p.tenantAlloc = make(map[string]map[int]bool)
}
