package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// SandboxInstance represents an isolated execution container for a tenant workload.
type SandboxInstance struct {
	ID       string               `json:"id"`
	TenantID string               `json:"tenant_id"`
	Status   InstanceStatus       `json:"status"`
	Memory   *LinearMemory        `json:"memory"`
	Gas      *GasMeter            `json:"gas"`
	Policy   *TenantSandboxPolicy `json:"policy"`
	LastUsed time.Time            `json:"last_used"`
	mu       sync.Mutex
}

// CrashJournal manages an in-memory circular ring buffer of recent crash reports.
type CrashJournal struct {
	mu         sync.RWMutex
	entries    []*CrashReport
	maxEntries int
}

// NewCrashJournal creates a thread-safe crash journal with capacity capping.
func NewCrashJournal(maxEntries int) *CrashJournal {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxCrashJournalSize
	}
	return &CrashJournal{
		entries:    make([]*CrashReport, 0, maxEntries),
		maxEntries: maxEntries,
	}
}

// Append records a crash report into the circular ring buffer.
func (cj *CrashJournal) Append(report *CrashReport) {
	cj.mu.Lock()
	defer cj.mu.Unlock()
	if len(cj.entries) >= cj.maxEntries {
		cj.entries = cj.entries[1:] // Evict oldest entry
	}
	cj.entries = append(cj.entries, report)
}

// List returns a cloned slice of recent crash reports, newest first.
func (cj *CrashJournal) List(limit int) []*CrashReport {
	cj.mu.RLock()
	defer cj.mu.RUnlock()
	n := len(cj.entries)
	if n == 0 {
		return []*CrashReport{}
	}
	if limit <= 0 || limit > n {
		limit = n
	}
	result := make([]*CrashReport, limit)
	for i := 0; i < limit; i++ {
		result[i] = cj.entries[n-1-i]
	}
	return result
}

// Clear flushes all entries from the journal.
func (cj *CrashJournal) Clear() {
	cj.mu.Lock()
	defer cj.mu.Unlock()
	cj.entries = make([]*CrashReport, 0, cj.maxEntries)
}

// SandboxEngine supervises tenant Wasm sandbox allocations, runtime execution, and fault isolation.
type SandboxEngine struct {
	cfg                  SandboxConfig
	instances            map[string]*SandboxInstance
	policies             map[string]*TenantSandboxPolicy
	journal              *CrashJournal
	startTime            time.Time
	mu                   sync.RWMutex
	totalExecutions      int64
	successfulExecutions int64
	gasConsumedTotal     uint64
	oomInterceptions     int64
	panicRecoveries      int64
}

// DefaultSandboxConfig returns production-ready sandbox supervisory defaults.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		HardCeilingPages:    HardCeilingMemoryPages,
		DefaultGasLimit:     DefaultGasLimit,
		MaxCrashJournalSize: DefaultMaxCrashJournalSize,
		QuarantineThreshold: DefaultQuarantineThreshold,
	}
}

// NewSandboxEngine initializes the Wasm isolation supervisor engine.
func NewSandboxEngine(cfg SandboxConfig) *SandboxEngine {
	if cfg.HardCeilingPages <= 0 {
		cfg.HardCeilingPages = HardCeilingMemoryPages
	}
	if cfg.DefaultGasLimit == 0 {
		cfg.DefaultGasLimit = DefaultGasLimit
	}
	if cfg.MaxCrashJournalSize <= 0 {
		cfg.MaxCrashJournalSize = DefaultMaxCrashJournalSize
	}
	if cfg.QuarantineThreshold <= 0 {
		cfg.QuarantineThreshold = DefaultQuarantineThreshold
	}
	return &SandboxEngine{
		cfg:       cfg,
		instances: make(map[string]*SandboxInstance),
		policies:  make(map[string]*TenantSandboxPolicy),
		journal:   NewCrashJournal(cfg.MaxCrashJournalSize),
		startTime: time.Now(),
	}
}

func instanceKey(tenantID, instanceID string) string {
	return tenantID + ":" + instanceID
}

// SetTenantPolicy registers or updates an isolation policy for a specific tenant.
func (e *SandboxEngine) SetTenantPolicy(policy TenantSandboxPolicy) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if policy.TenantID == "" {
		return fmt.Errorf("tenant_id cannot be empty")
	}
	if policy.MaxMemoryPages <= 0 {
		policy.MaxMemoryPages = DefaultMaxMemoryPages
	}
	if policy.MaxMemoryPages > e.cfg.HardCeilingPages {
		policy.MaxMemoryPages = e.cfg.HardCeilingPages
	}
	if policy.GasLimit == 0 {
		policy.GasLimit = e.cfg.DefaultGasLimit
	}
	if policy.MaxCrashThreshold <= 0 {
		policy.MaxCrashThreshold = e.cfg.QuarantineThreshold
	}
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = time.Now()
	}
	policy.UpdatedAt = time.Now()
	e.policies[policy.TenantID] = &policy
	return nil
}

// GetTenantPolicy retrieves the isolation policy for a tenant.
func (e *SandboxEngine) GetTenantPolicy(tenantID string) (*TenantSandboxPolicy, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	pol, ok := e.policies[tenantID]
	if !ok {
		return nil, false
	}
	clone := *pol
	return &clone, true
}

// ListTenantPolicies returns all registered tenant isolation policies.
func (e *SandboxEngine) ListTenantPolicies() []TenantSandboxPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]TenantSandboxPolicy, 0, len(e.policies))
	for _, p := range e.policies {
		out = append(out, *p)
	}
	return out
}

// GetOrCreateInstance fetches or creates an isolated sandbox instance for a tenant.
func (e *SandboxEngine) GetOrCreateInstance(tenantID, instanceID string) (*SandboxInstance, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	if instanceID == "" {
		instanceID = "inst-0"
	}
	key := instanceKey(tenantID, instanceID)

	e.mu.Lock()
	defer e.mu.Unlock()
	if inst, exists := e.instances[key]; exists {
		return inst, nil
	}

	policy, ok := e.policies[tenantID]
	if !ok {
		policy = &TenantSandboxPolicy{
			TenantID:           tenantID,
			MaxMemoryPages:     DefaultMaxMemoryPages,
			GasLimit:           e.cfg.DefaultGasLimit,
			MaxExecutionTimeMs: 5000,
			MaxCrashThreshold:  e.cfg.QuarantineThreshold,
			CurrentCrashes:     0,
			Quarantined:        false,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}
		e.policies[tenantID] = policy
	}

	mem, err := NewLinearMemory(DefaultInitialMemoryPages, policy.MaxMemoryPages)
	if err != nil {
		return nil, err
	}

	inst := &SandboxInstance{
		ID:       instanceID,
		TenantID: tenantID,
		Status:   StatusIdle,
		Memory:   mem,
		Gas:      NewGasMeter(policy.GasLimit),
		Policy:   policy,
		LastUsed: time.Now(),
	}
	e.instances[key] = inst
	return inst, nil
}

// ResetInstance restores an instance from crashed or quarantined state to IDLE with fresh zeroed memory.
func (e *SandboxEngine) ResetInstance(tenantID, instanceID string) error {
	key := instanceKey(tenantID, instanceID)
	e.mu.Lock()
	defer e.mu.Unlock()

	inst, exists := e.instances[key]
	if !exists {
		return ErrInstanceNotFound
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.Memory.Zeroize()
	_ = inst.Memory.Write(0, make([]byte, inst.Memory.Bytes()))
	inst.Gas.Reset(inst.Policy.GasLimit)
	inst.Status = StatusIdle
	inst.LastUsed = time.Now()

	if inst.Policy != nil {
		inst.Policy.CurrentCrashes = 0
		inst.Policy.Quarantined = false
	}
	return nil
}

// Execute handles guest execution with host-level isolation, gas accounting, and panic recovery.
func (e *SandboxEngine) Execute(ctx context.Context, req ExecutionRequest) (res *ExecutionResult, retErr error) {
	atomic.AddInt64(&e.totalExecutions, 1)

	inst, err := e.GetOrCreateInstance(req.TenantID, req.InstanceID)
	if err != nil {
		return nil, err
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.Policy != nil && inst.Policy.Quarantined {
		return &ExecutionResult{
			ExecutionID: generateExecutionID(),
			TenantID:    req.TenantID,
			InstanceID:  req.InstanceID,
			Status:      StatusQuarantined,
			Error:       ErrSandboxQuarantined.Error(),
		}, ErrSandboxQuarantined
	}

	execID := generateExecutionID()
	gasBudget := req.GasBudget
	if gasBudget == 0 {
		gasBudget = inst.Policy.GasLimit
	}
	inst.Gas.Reset(gasBudget)
	inst.Status = StatusRunning
	inst.LastUsed = time.Now()
	startTime := time.Now()

	// Panic isolation guard: ensures panic or memory fault in guest never terminates host broker
	defer func() {
		if r := recover(); r != nil {
			atomic.AddInt64(&e.panicRecoveries, 1)
			inst.Status = StatusCrashed
			stack := string(debug.Stack())
			crash := &CrashReport{
				CrashID:     "crash-" + execID,
				TenantID:    req.TenantID,
				InstanceID:  req.InstanceID,
				Timestamp:   time.Now(),
				Reason:      fmt.Sprintf("guest runtime panic: %v", r),
				StackTrace:  stack,
				Recovered:   true,
				GasUsed:     inst.Gas.Used(),
				MemoryPages: inst.Memory.Pages(),
				MemoryBytes: inst.Memory.Bytes(),
			}
			e.journal.Append(crash)
			if inst.Policy != nil {
				inst.Policy.CurrentCrashes++
				if inst.Policy.CurrentCrashes >= inst.Policy.MaxCrashThreshold {
					inst.Policy.Quarantined = true
					inst.Status = StatusQuarantined
				}
			}
			duration := float64(time.Since(startTime).Microseconds()) / 1000.0
			res = &ExecutionResult{
				ExecutionID:     execID,
				TenantID:        req.TenantID,
				InstanceID:      req.InstanceID,
				Status:          StatusCrashed,
				DurationMs:      duration,
				GasConsumed:     inst.Gas.Used(),
				GasRemaining:    0,
				PeakMemoryPages: inst.Memory.PeakPages(),
				PeakMemoryBytes: inst.Memory.PeakBytes(),
				Crash:           crash,
				Error:           fmt.Sprintf("execution trapped: %v", r),
			}
			retErr = ErrSandboxCrashed
		}
	}()

	output, execErr := e.runMicroInterpreter(ctx, inst, req)
	duration := float64(time.Since(startTime).Microseconds()) / 1000.0
	atomic.AddUint64(&e.gasConsumedTotal, inst.Gas.Used())

	if execErr != nil {
		if errorsIsOOM(execErr) {
			atomic.AddInt64(&e.oomInterceptions, 1)
		}
		inst.Status = StatusIdle
		return &ExecutionResult{
			ExecutionID:     execID,
			TenantID:        req.TenantID,
			InstanceID:      req.InstanceID,
			Status:          StatusIdle,
			DurationMs:      duration,
			GasConsumed:     inst.Gas.Used(),
			GasRemaining:    inst.Gas.Remaining(),
			PeakMemoryPages: inst.Memory.PeakPages(),
			PeakMemoryBytes: inst.Memory.PeakBytes(),
			Output:          nil,
			Error:           execErr.Error(),
		}, execErr
	}

	atomic.AddInt64(&e.successfulExecutions, 1)
	inst.Status = StatusIdle

	return &ExecutionResult{
		ExecutionID:     execID,
		TenantID:        req.TenantID,
		InstanceID:      req.InstanceID,
		Status:          StatusIdle,
		DurationMs:      duration,
		GasConsumed:     inst.Gas.Used(),
		GasRemaining:    inst.Gas.Remaining(),
		PeakMemoryPages: inst.Memory.PeakPages(),
		PeakMemoryBytes: inst.Memory.PeakBytes(),
		Output:          output,
		Error:           "",
	}, nil
}

func (e *SandboxEngine) runMicroInterpreter(ctx context.Context, inst *SandboxInstance, req ExecutionRequest) ([]byte, error) {
	switch req.PayloadType {
	case PayloadCompute:
		if err := inst.Gas.Consume(GasBasicOp * 5); err != nil {
			return nil, err
		}
		if err := inst.Gas.Consume(GasArithmetic * 10); err != nil {
			return nil, err
		}
		resData := []byte(fmt.Sprintf("computed_hash_result:%d", time.Now().UnixNano()))
		if err := inst.Memory.Write(0, resData); err != nil {
			return nil, err
		}
		if err := inst.Gas.Consume(GasMemoryAccess); err != nil {
			return nil, err
		}
		return inst.Memory.Read(0, len(resData))

	case PayloadInfiniteLoop:
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			if err := inst.Gas.ConsumeLoopStep(); err != nil {
				return nil, err
			}
		}

	case PayloadMemoryAlloc:
		pages := req.RequestedPages
		if pages <= 0 {
			pages = 100
		}
		if err := inst.Gas.Consume(GasMemoryGrowPerPage * uint64(pages)); err != nil {
			return nil, err
		}
		_, err := inst.Memory.Grow(pages)
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("allocated_%d_pages_total_%d_pages", pages, inst.Memory.Pages())), nil

	case PayloadPanicFault:
		panic("SIGSEGV: invalid memory pointer dereference (0x00000000)")

	case PayloadCustom:
		if len(req.Payload) > 0 {
			if err := inst.Gas.Consume(GasBasicOp * uint64(len(req.Payload))); err != nil {
				return nil, err
			}
			_ = inst.Memory.Write(0, req.Payload)
			return req.Payload, nil
		}
		return []byte("empty_payload"), nil

	default:
		return nil, ErrInvalidPayload
	}
}

func errorsIsOOM(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrMemoryLimitExceeded) || errors.Is(err, ErrMemoryHardCeilingExceeded)
}

func generateExecutionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GetStats compiles real-time telemetry metrics.
func (e *SandboxEngine) GetStats() SandboxStats {
	e.mu.RLock()
	activeSandboxes := len(e.instances)
	quarantined := 0
	for _, p := range e.policies {
		if p.Quarantined {
			quarantined++
		}
	}
	e.mu.RUnlock()

	uptime := int64(time.Since(e.startTime).Seconds())
	return SandboxStats{
		ActiveSandboxes:      activeSandboxes,
		TotalExecutions:      atomic.LoadInt64(&e.totalExecutions),
		SuccessfulExecutions: atomic.LoadInt64(&e.successfulExecutions),
		GasConsumedTotal:     atomic.LoadUint64(&e.gasConsumedTotal),
		OOMInterceptions:     atomic.LoadInt64(&e.oomInterceptions),
		PanicRecoveries:      atomic.LoadInt64(&e.panicRecoveries),
		QuarantinedInstances: quarantined,
		HardCeilingPages:     e.cfg.HardCeilingPages,
		HardCeilingBytes:     e.cfg.HardCeilingPages * WasmPageSize,
		UptimeSeconds:        uptime,
	}
}

// ListCrashes returns recent crash reports from the circular journal.
func (e *SandboxEngine) ListCrashes(limit int) []*CrashReport {
	return e.journal.List(limit)
}

// ClearCrashes clears the crash journal.
func (e *SandboxEngine) ClearCrashes() {
	e.journal.Clear()
}
