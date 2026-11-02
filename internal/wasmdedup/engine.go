package wasmdedup

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DedupEngine coordinates memory deduplication, CoW page sharing, snapshots, and dynamic compaction.
type DedupEngine struct {
	mu          sync.RWMutex
	pool        *PagePool
	snapshotMgr *SnapshotManager
	compactor   *Compactor
	instances   map[string]*CoWLinearMemory
	metadata    map[string]*InstanceMetadata
	templates   map[string][]*PhysicalPage
	config      DedupConfig

	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// NewDedupEngine creates an unstarted deduplication engine with the given configuration.
func NewDedupEngine(cfg DedupConfig) *DedupEngine {
	if cfg.InitialPages <= 0 {
		cfg.InitialPages = DefaultInitialPages
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = DefaultMaxPages
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.CompactionInterval <= 0 {
		cfg.CompactionInterval = DefaultCompactionInterval
	}

	pool := NewPagePool()
	snapshotMgr := NewSnapshotManager(pool)
	compactor := NewCompactor(pool)

	return &DedupEngine{
		pool:        pool,
		snapshotMgr: snapshotMgr,
		compactor:   compactor,
		instances:   make(map[string]*CoWLinearMemory),
		metadata:    make(map[string]*InstanceMetadata),
		templates:   make(map[string][]*PhysicalPage),
		config:      cfg,
	}
}

// Start launches background compaction and maintenance loops.
func (e *DedupEngine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.cancelFunc != nil {
		e.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	e.cancelFunc = cancel
	e.mu.Unlock()

	e.wg.Add(1)
	go e.maintenanceLoop(runCtx)
}

// Stop cleanly terminates background maintenance workers.
func (e *DedupEngine) Stop() {
	e.mu.Lock()
	if e.cancelFunc != nil {
		e.cancelFunc()
		e.cancelFunc = nil
	}
	e.mu.Unlock()
	e.wg.Wait()
}

// maintenanceLoop periodically executes compaction sweeps if enabled.
func (e *DedupEngine) maintenanceLoop(ctx context.Context) {
	defer e.wg.Done()
	ticker := time.NewTicker(e.config.CompactionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.config.AutoCompact {
				_ = e.RunCompaction()
			}
		}
	}
}

// RegisterTemplate parses a raw Wasm or binary payload into shared 64 KiB pages for zero-copy instantiation.
func (e *DedupEngine) RegisterTemplate(templateID string, baseData []byte) error {
	if templateID == "" {
		return fmt.Errorf("template ID cannot be empty")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Release existing template pages if updating
	if existing, found := e.templates[templateID]; found {
		for _, p := range existing {
			e.pool.ReleasePage(p)
		}
	}

	numPages := (len(baseData) + WasmPageSize - 1) / WasmPageSize
	if numPages == 0 {
		numPages = e.config.InitialPages
	}
	if numPages > HardCeilingPages {
		return fmt.Errorf("%w: template exceeds 32MB ceiling", ErrHardCeilingExceeded)
	}

	pages := make([]*PhysicalPage, numPages)
	for i := 0; i < numPages; i++ {
		start := i * WasmPageSize
		end := start + WasmPageSize
		var chunk []byte
		if start < len(baseData) {
			if end > len(baseData) {
				chunk = baseData[start:]
			} else {
				chunk = baseData[start:end]
			}
		}
		pages[i] = e.pool.InternPage(chunk)
	}

	e.templates[templateID] = pages
	return nil
}

// SpawnInstance creates a new edge worker instance, optionally backed by a pre-warmed template.
func (e *DedupEngine) SpawnInstance(instanceID, tenantID, templateID string, initialPages, maxPages int) (*CoWLinearMemory, error) {
	if instanceID == "" {
		instanceID = generateID("inst")
	}
	if tenantID == "" {
		tenantID = "default"
	}
	if maxPages <= 0 {
		maxPages = e.config.MaxPages
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.instances[instanceID]; exists {
		return nil, fmt.Errorf("instance ID %s already exists", instanceID)
	}

	var inst *CoWLinearMemory
	var err error

	if templateID != "" {
		tmplPages, found := e.templates[templateID]
		if !found {
			return nil, fmt.Errorf("template %s not found", templateID)
		}
		inst, err = NewFromSharedPages(instanceID, tenantID, tmplPages, maxPages, e.pool)
	} else {
		if initialPages <= 0 {
			initialPages = e.config.InitialPages
		}
		inst, err = NewCoWLinearMemory(instanceID, tenantID, initialPages, maxPages, e.pool)
	}

	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	meta := &InstanceMetadata{
		InstanceID:     instanceID,
		TenantID:       tenantID,
		TemplateID:     templateID,
		Status:         StatusActive,
		CreatedAt:      now,
		LastAccessedAt: now,
		VirtualPages:   inst.PageCount(),
		SharedPages:    inst.SharedPageCount(),
		PrivatePages:   inst.PrivatePageCount(),
		VirtualBytes:   int64(inst.ByteCount()),
		PhysicalBytes:  int64(inst.PrivatePageCount() * WasmPageSize),
		CoWFaults:      0,
	}

	e.instances[instanceID] = inst
	e.metadata[instanceID] = meta

	return inst, nil
}

// GetInstance returns an active instance and updates its last accessed timestamp.
func (e *DedupEngine) GetInstance(instanceID string) (*CoWLinearMemory, bool) {
	e.mu.RLock()
	inst, found := e.instances[instanceID]
	meta := e.metadata[instanceID]
	e.mu.RUnlock()

	if found && meta != nil {
		e.mu.Lock()
		meta.LastAccessedAt = time.Now().UTC()
		e.mu.Unlock()
	}
	return inst, found
}

// TerminateInstance releases memory pages and purges an instance.
func (e *DedupEngine) TerminateInstance(instanceID string) error {
	e.mu.Lock()
	inst, found := e.instances[instanceID]
	if !found {
		e.mu.Unlock()
		return ErrInstanceNotFound
	}
	delete(e.instances, instanceID)
	delete(e.metadata, instanceID)
	e.mu.Unlock()

	inst.Release()
	return nil
}

// CreateSnapshot takes a snapshot of an active instance.
func (e *DedupEngine) CreateSnapshot(instanceID, templateID string, metadata map[string]string) (*WorkerSnapshot, error) {
	e.mu.RLock()
	inst, found := e.instances[instanceID]
	e.mu.RUnlock()

	if !found {
		return nil, ErrInstanceNotFound
	}

	return e.snapshotMgr.CreateSnapshot(inst, templateID, metadata)
}

// Resurrect restores an instance from a snapshot in sub-millisecond latency.
func (e *DedupEngine) Resurrect(snapshotID, newInstanceID string, maxPages int) (*CoWLinearMemory, time.Duration, error) {
	inst, elapsed, err := e.snapshotMgr.Resurrect(snapshotID, newInstanceID, maxPages)
	if err != nil {
		return nil, 0, err
	}

	now := time.Now().UTC()
	meta := &InstanceMetadata{
		InstanceID:     inst.instanceID,
		TenantID:       inst.tenantID,
		Status:         StatusActive,
		CreatedAt:      now,
		LastAccessedAt: now,
		VirtualPages:   inst.PageCount(),
		SharedPages:    inst.SharedPageCount(),
		PrivatePages:   inst.PrivatePageCount(),
		VirtualBytes:   int64(inst.ByteCount()),
		PhysicalBytes:  int64(inst.PrivatePageCount() * WasmPageSize),
		CoWFaults:      0,
	}

	e.mu.Lock()
	e.instances[inst.instanceID] = inst
	e.metadata[inst.instanceID] = meta
	e.mu.Unlock()

	return inst, elapsed, nil
}

// RunCompaction executes an on-demand memory deduplication pass across all active instances.
func (e *DedupEngine) RunCompaction() CompactionResult {
	e.mu.RLock()
	activeList := make([]*CoWLinearMemory, 0, len(e.instances))
	for _, inst := range e.instances {
		activeList = append(activeList, inst)
	}
	e.mu.RUnlock()

	result := e.compactor.Compact(activeList)

	// Update metadata reflection
	e.mu.Lock()
	for id, inst := range e.instances {
		if meta, found := e.metadata[id]; found {
			meta.SharedPages = inst.SharedPageCount()
			meta.PrivatePages = inst.PrivatePageCount()
			meta.PhysicalBytes = int64(inst.PrivatePageCount() * WasmPageSize)
			meta.CoWFaults = inst.CoWFaults()
		}
	}
	e.mu.Unlock()

	return result
}

// GetStats compiles comprehensive deduplication and memory savings metrics.
func (e *DedupEngine) GetStats() MemoryStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	activeCount := len(e.instances)
	totalVirtualPages := 0
	totalPrivatePages := 0
	totalSharedPages := 0
	var totalCoWFaults int64 = 0

	for _, inst := range e.instances {
		totalVirtualPages += inst.PageCount()
		totalPrivatePages += inst.PrivatePageCount()
		totalSharedPages += inst.SharedPageCount()
		totalCoWFaults += inst.CoWFaults()
	}

	sharedPhysical := e.pool.SharedCount()
	uniquePhysical := sharedPhysical + totalPrivatePages

	virtualBytes := int64(totalVirtualPages) * WasmPageSize
	residentBytes := int64(uniquePhysical) * WasmPageSize

	var savedBytes int64 = 0
	if virtualBytes > residentBytes {
		savedBytes = virtualBytes - residentBytes
	}

	var dedupRatio float64 = 1.0
	var savingsPercent float64 = 0.0

	if residentBytes > 0 && virtualBytes > 0 {
		dedupRatio = float64(virtualBytes) / float64(residentBytes)
		savingsPercent = (float64(savedBytes) / float64(virtualBytes)) * 100.0
	}

	cycles, reclaimed, _, _ := e.compactor.Stats()
	resurrectCount, avgResurrectUs := e.snapshotMgr.ResurrectionStats()

	return MemoryStats{
		ActiveInstances:       activeCount,
		HibernatedInstances:   0,
		TotalSnapshots:        len(e.snapshotMgr.ListSnapshots()),
		TotalVirtualPages:     totalVirtualPages,
		SharedPhysicalPages:   sharedPhysical,
		PrivatePhysicalPages:  totalPrivatePages,
		UniquePhysicalPages:   uniquePhysical,
		VirtualMemoryBytes:    virtualBytes,
		PhysicalResidentBytes: residentBytes,
		SavedMemoryBytes:      savedBytes,
		DedupRatio:            dedupRatio,
		SavingsPercent:        savingsPercent,
		CoWFaultCount:         totalCoWFaults,
		CompactionCycles:      cycles,
		ReclaimedPagesCount:   reclaimed,
		ResurrectionCount:     resurrectCount,
		AvgResurrectUs:        avgResurrectUs,
	}
}

// ListInstances returns a copy of metadata for all registered instances.
func (e *DedupEngine) ListInstances() []*InstanceMetadata {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]*InstanceMetadata, 0, len(e.metadata))
	for _, meta := range e.metadata {
		// Clone metadata
		c := *meta
		out = append(out, &c)
	}
	return out
}

// Reset clears instances, templates, and snapshots for testing or benchmark suites.
func (e *DedupEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, inst := range e.instances {
		inst.Release()
	}
	e.instances = make(map[string]*CoWLinearMemory)
	e.metadata = make(map[string]*InstanceMetadata)

	for _, tmpl := range e.templates {
		for _, p := range tmpl {
			e.pool.ReleasePage(p)
		}
	}
	e.templates = make(map[string][]*PhysicalPage)

	e.pool = NewPagePool()
	e.snapshotMgr = NewSnapshotManager(e.pool)
	e.compactor = NewCompactor(e.pool)
}
