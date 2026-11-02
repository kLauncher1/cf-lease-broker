package wasmdedup

import (
	"bytes"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

// PageHash is a 32-byte SHA-256 digest identifying a 64 KiB memory page.
type PageHash [32]byte

// PhysicalPage encapsulates a single 64 KiB physical memory buffer.
type PhysicalPage struct {
	Hash      PageHash
	Data      []byte
	RefCount  int64
	IsShared  bool
	IsZero    bool
}

// IncRef atomically increments the reference count.
func (p *PhysicalPage) IncRef() int64 {
	return atomic.AddInt64(&p.RefCount, 1)
}

// DecRef atomically decrements the reference count.
func (p *PhysicalPage) DecRef() int64 {
	return atomic.AddInt64(&p.RefCount, -1)
}

// GetRefCount returns the current reference count.
func (p *PhysicalPage) GetRefCount() int64 {
	return atomic.LoadInt64(&p.RefCount)
}

// PagePool maintains an index of shared physical pages and the canonical zero page.
type PagePool struct {
	mu           sync.RWMutex
	sharedPages  map[PageHash]*PhysicalPage
	zeroPage     *PhysicalPage
	zeroHash     PageHash
	reclaimedCnt int64
}

// NewPagePool initializes the page repository and seeds the canonical zero page.
func NewPagePool() *PagePool {
	zeroData := make([]byte, WasmPageSize)
	zeroHash := sha256.Sum256(zeroData)

	zeroPage := &PhysicalPage{
		Hash:     zeroHash,
		Data:     zeroData,
		RefCount: 1, // permanent base reference
		IsShared: true,
		IsZero:   true,
	}

	pool := &PagePool{
		sharedPages: make(map[PageHash]*PhysicalPage),
		zeroPage:    zeroPage,
		zeroHash:    zeroHash,
	}
	pool.sharedPages[zeroHash] = zeroPage

	return pool
}

// ComputeHash computes the SHA-256 digest of a 64 KiB page buffer.
func ComputeHash(data []byte) PageHash {
	return sha256.Sum256(data)
}

// IsAllZero checks if a byte slice consists entirely of zero bytes.
func IsAllZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// GetZeroPage returns a reference to the canonical immutable zero page.
func (p *PagePool) GetZeroPage() *PhysicalPage {
	p.zeroPage.IncRef()
	return p.zeroPage
}

// InternPage takes a 64 KiB page buffer, computes its fingerprint, and returns a shared physical page.
// If an identical page already exists in the pool, its reference count is incremented and returned.
func (p *PagePool) InternPage(data []byte) *PhysicalPage {
	if len(data) != WasmPageSize {
		// Pad or truncate safely to exactly WasmPageSize
		padded := make([]byte, WasmPageSize)
		copy(padded, data)
		data = padded
	}

	if IsAllZero(data) {
		return p.GetZeroPage()
	}

	hash := ComputeHash(data)

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, found := p.sharedPages[hash]; found {
		if bytes.Equal(existing.Data, data) {
			existing.IncRef()
			return existing
		}
	}

	// Create new immutable shared physical page
	pageCopy := make([]byte, WasmPageSize)
	copy(pageCopy, data)

	page := &PhysicalPage{
		Hash:     hash,
		Data:     pageCopy,
		RefCount: 1,
		IsShared: true,
		IsZero:   false,
	}

	p.sharedPages[hash] = page
	return page
}

// ReleasePage decrements the reference count of a physical page.
// If the reference count drops to zero and it is not the zero page, it is pruned from the pool.
func (p *PagePool) ReleasePage(page *PhysicalPage) {
	if page == nil {
		return
	}

	if page.IsZero {
		page.DecRef()
		return
	}

	if page.DecRef() <= 0 {
		p.mu.Lock()
		defer p.mu.Unlock()

		// Double-check under lock
		if page.GetRefCount() <= 0 {
			delete(p.sharedPages, page.Hash)
			atomic.AddInt64(&p.reclaimedCnt, 1)
		}
	}
}

// AllocPrivatePage allocates an isolated mutable 64 KiB page not tracked in the shared pool.
func (p *PagePool) AllocPrivatePage(initialData []byte) *PhysicalPage {
	buf := make([]byte, WasmPageSize)
	if len(initialData) > 0 {
		copy(buf, initialData)
	}

	return &PhysicalPage{
		Hash:     PageHash{},
		Data:     buf,
		RefCount: 1,
		IsShared: false,
		IsZero:   false,
	}
}

// SharedCount returns the number of active distinct shared pages in the pool.
func (p *PagePool) SharedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.sharedPages)
}

// ReclaimedCount returns total pages freed by the pool.
func (p *PagePool) ReclaimedCount() int64 {
	return atomic.LoadInt64(&p.reclaimedCnt)
}
