package wasmdedup

import (
	"errors"
	"time"
)

// WebAssembly linear memory constants and limits.
const (
	// WasmPageSize is the standard WebAssembly page size (64 KiB).
	WasmPageSize = 65536

	// DefaultInitialPages is the default memory allocated at instance creation (1 MiB / 16 pages).
	DefaultInitialPages = 16

	// DefaultMaxPages is the default maximum memory quota for tenant sandboxes (16 MiB / 256 pages).
	DefaultMaxPages = 256

	// HardCeilingPages enforces the broker 32 MiB physical upper ceiling (512 pages).
	HardCeilingPages = 512

	// DefaultIdleTimeout is the idle time threshold before an instance is eligible for hibernation.
	DefaultIdleTimeout = 30 * time.Second

	// DefaultCompactionInterval is the periodic schedule for dynamic memory compaction sweeps.
	DefaultCompactionInterval = 10 * time.Second
)

// Standard errors for Wasm memory deduplication and page sharing.
var (
	ErrOutOfBounds         = errors.New("wasm linear memory access out of bounds")
	ErrMemoryExceeded      = errors.New("wasm linear memory quota limit exceeded")
	ErrHardCeilingExceeded = errors.New("wasm memory request exceeds broker 32MB physical hard ceiling")
	ErrInstanceNotFound    = errors.New("wasm worker instance not found")
	ErrSnapshotNotFound    = errors.New("wasm worker snapshot not found")
	ErrInvalidPageRange    = errors.New("invalid page index or offset range")
	ErrNilBuffer           = errors.New("memory buffer cannot be nil")
	ErrNegativeDelta       = errors.New("memory grow delta cannot be negative")
)

// InstanceStatus represents the operational lifecycle state of an edge worker instance.
type InstanceStatus string

const (
	StatusActive     InstanceStatus = "ACTIVE"
	StatusIdle       InstanceStatus = "IDLE"
	StatusHibernated InstanceStatus = "HIBERNATED"
	StatusTerminated InstanceStatus = "TERMINATED"
)

// MemoryStats aggregates real-time metrics across all deduplicated worker instances.
type MemoryStats struct {
	ActiveInstances       int     `json:"active_instances"`
	HibernatedInstances   int     `json:"hibernated_instances"`
	TotalSnapshots        int     `json:"total_snapshots"`
	TotalVirtualPages     int     `json:"total_virtual_pages"`
	SharedPhysicalPages   int     `json:"shared_physical_pages"`
	PrivatePhysicalPages  int     `json:"private_physical_pages"`
	UniquePhysicalPages   int     `json:"unique_physical_pages"`
	VirtualMemoryBytes    int64   `json:"virtual_memory_bytes"`
	PhysicalResidentBytes int64   `json:"physical_resident_bytes"`
	SavedMemoryBytes      int64   `json:"saved_memory_bytes"`
	DedupRatio            float64 `json:"dedup_ratio"`
	SavingsPercent        float64 `json:"savings_percent"`
	CoWFaultCount         int64   `json:"cow_fault_count"`
	CompactionCycles      int64   `json:"compaction_cycles"`
	ReclaimedPagesCount   int64   `json:"reclaimed_pages_count"`
	ResurrectionCount     int64   `json:"resurrection_count"`
	AvgResurrectUs        float64 `json:"avg_resurrect_latency_us"`
}

// InstanceMetadata describes an edge worker instance and its memory allocation profile.
type InstanceMetadata struct {
	InstanceID     string         `json:"instance_id"`
	TenantID       string         `json:"tenant_id"`
	TemplateID     string         `json:"template_id,omitempty"`
	Status         InstanceStatus `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	LastAccessedAt time.Time      `json:"last_accessed_at"`
	VirtualPages   int            `json:"virtual_pages"`
	SharedPages    int            `json:"shared_pages"`
	PrivatePages   int            `json:"private_pages"`
	VirtualBytes   int64          `json:"virtual_bytes"`
	PhysicalBytes  int64          `json:"physical_bytes"`
	CoWFaults      int64          `json:"cow_faults"`
}

// DedupConfig configures deduplication, CoW thresholds, and compaction behavior.
type DedupConfig struct {
	InitialPages       int           `json:"initial_pages"`
	MaxPages           int           `json:"max_pages"`
	IdleTimeout        time.Duration `json:"idle_timeout"`
	CompactionInterval time.Duration `json:"compaction_interval"`
	AutoCompact        bool          `json:"auto_compact"`
}

// DefaultDedupConfig returns standard production parameters.
func DefaultDedupConfig() DedupConfig {
	return DedupConfig{
		InitialPages:       DefaultInitialPages,
		MaxPages:           DefaultMaxPages,
		IdleTimeout:        DefaultIdleTimeout,
		CompactionInterval: DefaultCompactionInterval,
		AutoCompact:        true,
	}
}
