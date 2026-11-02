package wasmdedup

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// WorkerSnapshot captures an immutable, lightweight point-in-time state of an edge worker instance.
type WorkerSnapshot struct {
	SnapshotID string            `json:"snapshot_id"`
	InstanceID string            `json:"instance_id"`
	TenantID   string            `json:"tenant_id"`
	TemplateID string            `json:"template_id,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	PageCount  int               `json:"page_count"`
	VirtualBytes int64           `json:"virtual_bytes"`
	Pages      []*PhysicalPage   `json:"-"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// SnapshotManager handles snapshot creation, cataloging, and sub-millisecond worker resurrection.
type SnapshotManager struct {
	mu                  sync.RWMutex
	pool                *PagePool
	snapshots           map[string]*WorkerSnapshot
	resurrectCount      int64
	totalResurrectUs    int64
}

// NewSnapshotManager initializes a snapshot repository linked to the shared page pool.
func NewSnapshotManager(pool *PagePool) *SnapshotManager {
	return &SnapshotManager{
		pool:      pool,
		snapshots: make(map[string]*WorkerSnapshot),
	}
}

// generateID produces a secure random identifier.
func generateID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}

// CreateSnapshot creates an immutable, deduplicated snapshot from an active CoWLinearMemory instance.
func (sm *SnapshotManager) CreateSnapshot(instance *CoWLinearMemory, templateID string, metadata map[string]string) (*WorkerSnapshot, error) {
	if instance == nil {
		return nil, ErrNilBuffer
	}

	snapshotPages := instance.SnapshotPages()
	internedPages := make([]*PhysicalPage, len(snapshotPages))

	// Ensure all pages in snapshot are interned immutable shared pages
	for i, page := range snapshotPages {
		if page.IsShared || page.IsZero {
			internedPages[i] = page
		} else {
			// Private page: intern into the pool to make it immutable and shared
			interned := sm.pool.InternPage(page.Data)
			internedPages[i] = interned
			sm.pool.ReleasePage(page) // release the temporary snapshot ref on private page
		}
	}

	snapID := generateID("snap")
	snapshot := &WorkerSnapshot{
		SnapshotID:   snapID,
		InstanceID:   instance.instanceID,
		TenantID:     instance.tenantID,
		TemplateID:   templateID,
		CreatedAt:    time.Now().UTC(),
		PageCount:    len(internedPages),
		VirtualBytes: int64(len(internedPages) * WasmPageSize),
		Pages:        internedPages,
		Metadata:     metadata,
	}

	sm.mu.Lock()
	sm.snapshots[snapID] = snapshot
	sm.mu.Unlock()

	return snapshot, nil
}

// Resurrect restores a new, active worker instance from a snapshot in sub-millisecond time.
func (sm *SnapshotManager) Resurrect(snapshotID, newInstanceID string, maxPages int) (*CoWLinearMemory, time.Duration, error) {
	start := time.Now()

	sm.mu.RLock()
	snap, found := sm.snapshots[snapshotID]
	sm.mu.RUnlock()

	if !found {
		return nil, 0, ErrSnapshotNotFound
	}

	if newInstanceID == "" {
		newInstanceID = generateID("inst")
	}

	instance, err := NewFromSharedPages(newInstanceID, snap.TenantID, snap.Pages, maxPages, sm.pool)
	if err != nil {
		return nil, 0, err
	}

	elapsed := time.Since(start)
	elapsedUs := elapsed.Microseconds()
	if elapsedUs == 0 {
		elapsedUs = 1
	}

	atomic.AddInt64(&sm.resurrectCount, 1)
	atomic.AddInt64(&sm.totalResurrectUs, elapsedUs)

	return instance, elapsed, nil
}

// GetSnapshot retrieves a snapshot record by ID.
func (sm *SnapshotManager) GetSnapshot(snapshotID string) (*WorkerSnapshot, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	snap, found := sm.snapshots[snapshotID]
	return snap, found
}

// ListSnapshots returns all stored snapshot records.
func (sm *SnapshotManager) ListSnapshots() []*WorkerSnapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	out := make([]*WorkerSnapshot, 0, len(sm.snapshots))
	for _, snap := range sm.snapshots {
		out = append(out, snap)
	}
	return out
}

// DeleteSnapshot removes a snapshot and releases its shared page references.
func (sm *SnapshotManager) DeleteSnapshot(snapshotID string) error {
	sm.mu.Lock()
	snap, found := sm.snapshots[snapshotID]
	if !found {
		sm.mu.Unlock()
		return ErrSnapshotNotFound
	}
	delete(sm.snapshots, snapshotID)
	sm.mu.Unlock()

	for _, page := range snap.Pages {
		sm.pool.ReleasePage(page)
	}
	return nil
}

// ResurrectionStats returns the total resurrection operations and average latency in microseconds.
func (sm *SnapshotManager) ResurrectionStats() (count int64, avgUs float64) {
	count = atomic.LoadInt64(&sm.resurrectCount)
	totalUs := atomic.LoadInt64(&sm.totalResurrectUs)
	if count > 0 {
		avgUs = float64(totalUs) / float64(count)
	}
	return count, avgUs
}
