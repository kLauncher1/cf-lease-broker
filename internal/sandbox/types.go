package sandbox

import (
	"errors"
	"time"
)

// WebAssembly linear memory constants and limits.
const (
	// WasmPageSize is the standard WebAssembly page size per specification (64 KiB).
	WasmPageSize = 65536

	// DefaultInitialMemoryPages is the default memory allocated at sandbox spawn (1 MiB).
	DefaultInitialMemoryPages = 16

	// DefaultMaxMemoryPages is the default maximum memory quota for tenant sandboxes (16 MiB).
	DefaultMaxMemoryPages = 256

	// HardCeilingMemoryPages enforces the unyielding 32 MiB physical upper ceiling for edge worker sandboxes.
	HardCeilingMemoryPages = 512

	// DefaultGasLimit is the default instruction budget per execution request.
	DefaultGasLimit = 1_000_000

	// DefaultMaxCrashJournalSize is the maximum number of recent crash reports retained in memory.
	DefaultMaxCrashJournalSize = 500

	// DefaultQuarantineThreshold is the consecutive crash limit before a tenant instance is quarantined.
	DefaultQuarantineThreshold = 5
)

// Standard sandbox execution errors.
var (
	ErrMemoryLimitExceeded       = errors.New("wasm linear memory limit exceeded")
	ErrMemoryOutOfBounds         = errors.New("wasm memory access out of bounds")
	ErrMemoryHardCeilingExceeded = errors.New("wasm memory request exceeds broker 32MB hard ceiling")
	ErrGasExhausted              = errors.New("wasm execution gas budget exhausted")
	ErrSandboxCrashed            = errors.New("wasm sandbox crashed due to unhandled runtime fault")
	ErrSandboxQuarantined        = errors.New("wasm sandbox quarantined due to repeated crash faults")
	ErrInstanceNotFound          = errors.New("wasm sandbox instance not found")
	ErrInvalidPayload            = errors.New("invalid wasm execution payload")
)

// InstanceStatus models the operational lifecycle state of an edge sandbox instance.
type InstanceStatus string

const (
	StatusIdle        InstanceStatus = "IDLE"
	StatusRunning     InstanceStatus = "RUNNING"
	StatusCrashed     InstanceStatus = "CRASHED"
	StatusQuarantined InstanceStatus = "QUARANTINED"
	StatusTerminated  InstanceStatus = "TERMINATED"
)

// MemoryTracker holds live and high-water-mark memory utilization metrics.
type MemoryTracker struct {
	InitialPages     int   `json:"initial_pages"`
	MaxPages         int   `json:"max_pages"`
	CurrentPages     int   `json:"current_pages"`
	CurrentBytes     int   `json:"current_bytes"`
	PeakPages        int   `json:"peak_pages"`
	PeakBytes        int   `json:"peak_bytes"`
	OOMInterceptions int64 `json:"oom_interceptions"`
}

// GasTracker records instruction consumption and budget depletion during execution.
type GasTracker struct {
	Limit     uint64 `json:"limit"`
	Consumed  uint64 `json:"consumed"`
	Remaining uint64 `json:"remaining"`
	LoopSteps uint64 `json:"loop_steps"`
	Exhausted bool   `json:"exhausted"`
}

// CrashReport captures fault diagnostics when an edge sandbox execution traps or panics.
type CrashReport struct {
	CrashID      string    `json:"crash_id"`
	TenantID     string    `json:"tenant_id"`
	InstanceID   string    `json:"instance_id"`
	Timestamp    time.Time `json:"timestamp"`
	Reason       string    `json:"reason"`
	StackTrace   string    `json:"stack_trace"`
	Recovered    bool      `json:"recovered"`
	GasUsed      uint64    `json:"gas_used"`
	MemoryPages  int       `json:"memory_pages"`
	MemoryBytes  int       `json:"memory_bytes"`
}

// ExecutionPayloadType specifies the micro-interpreter execution profile.
type ExecutionPayloadType string

const (
	PayloadCompute      ExecutionPayloadType = "compute"
	PayloadInfiniteLoop ExecutionPayloadType = "infinite_loop"
	PayloadMemoryAlloc  ExecutionPayloadType = "memory_alloc"
	PayloadPanicFault   ExecutionPayloadType = "panic_fault"
	PayloadCustom       ExecutionPayloadType = "custom"
)

// ExecutionRequest represents a guest workload submission to the edge Wasm sandbox.
type ExecutionRequest struct {
	TenantID       string                 `json:"tenant_id"`
	InstanceID     string                 `json:"instance_id"`
	PayloadType    ExecutionPayloadType   `json:"payload_type"`
	Payload        []byte                 `json:"payload,omitempty"`
	GasBudget      uint64                 `json:"gas_budget,omitempty"`
	RequestedPages int                    `json:"requested_pages,omitempty"`
	InputData      map[string]interface{} `json:"input_data,omitempty"`
}

// ExecutionResult records the outcome, telemetry, and isolation state of a sandbox invocation.
type ExecutionResult struct {
	ExecutionID     string         `json:"execution_id"`
	TenantID        string         `json:"tenant_id"`
	InstanceID      string         `json:"instance_id"`
	Status          InstanceStatus `json:"status"`
	DurationMs      float64        `json:"duration_ms"`
	GasConsumed     uint64         `json:"gas_consumed"`
	GasRemaining    uint64         `json:"gas_remaining"`
	PeakMemoryPages int            `json:"peak_memory_pages"`
	PeakMemoryBytes int            `json:"peak_memory_bytes"`
	Output          []byte         `json:"output,omitempty"`
	Crash           *CrashReport   `json:"crash,omitempty"`
	Error           string         `json:"error,omitempty"`
}

// TenantSandboxPolicy configures per-tenant isolation boundaries, memory quotas, and thresholds.
type TenantSandboxPolicy struct {
	TenantID            string    `json:"tenant_id"`
	MaxMemoryPages      int       `json:"max_memory_pages"`
	GasLimit            uint64    `json:"gas_limit"`
	MaxExecutionTimeMs  int64     `json:"max_execution_time_ms"`
	MaxCrashThreshold   int       `json:"max_crash_threshold"`
	CurrentCrashes      int       `json:"current_crashes"`
	Quarantined         bool      `json:"quarantined"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// SandboxStats aggregates broker-wide sandbox metrics for telemetry dashboards and Prometheus.
type SandboxStats struct {
	ActiveSandboxes      int    `json:"active_sandboxes"`
	TotalExecutions      int64  `json:"total_executions"`
	SuccessfulExecutions int64  `json:"successful_executions"`
	GasConsumedTotal     uint64 `json:"gas_consumed_total"`
	OOMInterceptions     int64  `json:"oom_interceptions"`
	PanicRecoveries      int64  `json:"panic_recoveries"`
	QuarantinedInstances int    `json:"quarantined_instances"`
	HardCeilingPages     int    `json:"hard_ceiling_pages"`
	HardCeilingBytes     int    `json:"hard_ceiling_bytes"`
	UptimeSeconds        int64  `json:"uptime_seconds"`
}

// SandboxConfig configures global parameters for the sandbox supervisor engine.
type SandboxConfig struct {
	HardCeilingPages     int    `json:"hard_ceiling_pages"`
	DefaultGasLimit      uint64 `json:"default_gas_limit"`
	MaxCrashJournalSize  int    `json:"max_crash_journal_size"`
	QuarantineThreshold  int    `json:"quarantine_threshold"`
}
