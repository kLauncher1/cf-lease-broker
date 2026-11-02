package wasmdedup

import (
	"sync"
	"sync/atomic"
	"time"
)

// CompactionResult summarizes the outcome of a memory compaction pass.
type CompactionResult struct {
	Timestamp      time.Time     `json:"timestamp"`
	Duration       time.Duration `json:"duration"`
	DurationUs     int64         `json:"duration_us"`
	ScannedPages   int           `json:"scanned_pages"`
	ReclaimedPages int           `json:"reclaimed_pages"`
	SavedBytes     int64         `json:"saved_bytes"`
	ZeroedMerged   int           `json:"zeroed_merged"`
	SharedMerged   int           `json:"shared_merged"`
}

// Compactor executes dynamic page deduplication and compaction across active instances.
type Compactor struct {
	mu             sync.RWMutex
	pool           *PagePool
	cycleCount     int64
	totalReclaimed int64
	totalSavedBytes int64
	lastResult     CompactionResult
}

// NewCompactor initializes a compactor instance bound to the shared page pool.
func NewCompactor(pool *PagePool) *Compactor {
	return &Compactor{
		pool: pool,
	}
}

// Compact scans all provided worker memory instances, coalescing identical private pages into shared pages.
func (c *Compactor) Compact(instances []*CoWLinearMemory) CompactionResult {
	start := time.Now()
	atomic.AddInt64(&c.cycleCount, 1)

	scanned := 0
	reclaimed := 0
	zeroMerged := 0
	sharedMerged := 0

	for _, inst := range instances {
		if inst == nil {
			continue
		}

		inst.mu.Lock()
		for idx, page := range inst.pageTable {
			scanned++

			// Skip pages that are already shared or canonical zero
			if page.IsShared || page.IsZero {
				continue
			}

			// Case 1: Page contains only zero bytes -> merge into canonical zero page
			if IsAllZero(page.Data) {
				inst.pool.ReleasePage(page)
				inst.pageTable[idx] = c.pool.GetZeroPage()
				reclaimed++
				zeroMerged++
				continue
			}

			// Case 2: Private page matches an existing shared page in the pool
			hash := ComputeHash(page.Data)
			c.pool.mu.RLock()
			existingShared, exists := c.pool.sharedPages[hash]
			c.pool.mu.RUnlock()

			if exists {
				existingShared.IncRef()
				inst.pool.ReleasePage(page)
				inst.pageTable[idx] = existingShared
				reclaimed++
				sharedMerged++
				continue
			}

			// Case 3: Intern private page as a new immutable shared candidate
			sharedPage := c.pool.InternPage(page.Data)
			inst.pool.ReleasePage(page)
			inst.pageTable[idx] = sharedPage
			// Note: First interning retains the page, subsequent matches will be reclaimed
		}
		inst.mu.Unlock()
	}

	savedBytes := int64(reclaimed) * WasmPageSize
	duration := time.Since(start)

	atomic.AddInt64(&c.totalReclaimed, int64(reclaimed))
	atomic.AddInt64(&c.totalSavedBytes, savedBytes)

	res := CompactionResult{
		Timestamp:      time.Now().UTC(),
		Duration:       duration,
		DurationUs:     duration.Microseconds(),
		ScannedPages:   scanned,
		ReclaimedPages: reclaimed,
		SavedBytes:     savedBytes,
		ZeroedMerged:   zeroMerged,
		SharedMerged:   sharedMerged,
	}

	c.mu.Lock()
	c.lastResult = res
	c.mu.Unlock()

	return res
}

// Stats returns cumulative compaction statistics.
func (c *Compactor) Stats() (cycles int64, reclaimed int64, savedBytes int64, last CompactionResult) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cycles = atomic.LoadInt64(&c.cycleCount)
	reclaimed = atomic.LoadInt64(&c.totalReclaimed)
	savedBytes = atomic.LoadInt64(&c.totalSavedBytes)
	last = c.lastResult
	return
}
