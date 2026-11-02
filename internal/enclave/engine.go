package enclave

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ExecutionPayload models a confidential execution request inside the enclave.
type ExecutionPayload struct {
	TenantID string `json:"tenant_id"`
	TaskType string `json:"task_type"`
	Input    string `json:"input"`
}

// ExecutionResult models the verified output from enclave execution.
type ExecutionResult struct {
	TenantID   string `json:"tenant_id"`
	TaskType   string `json:"task_type"`
	Output     string `json:"output"`
	OutputHash string `json:"output_hash"`
	PageID     int    `json:"page_id"`
	DurationUs int64  `json:"duration_us"`
}

// EnclaveEngine orchestrates confidential memory, attestation quotes, and secret sealing.
type EnclaveEngine struct {
	mu           sync.RWMutex
	cfg          EnclaveConfig
	status       EnclaveStatus
	pool         *EnclaveMemoryPool
	attest       *AttestationEngine
	sealer       *SecretSealer
	driver       driverInterface
	peerQuotes   map[string]*AttestationQuote
	totalLatency int64 // cumulative latency in microseconds

	// Atomic counters
	quotesGenerated int64
	quotesVerified  int64
	secretsSealed   int64
	secretsUnsealed int64
	executionsRun   int64
}

// driverInterface unifies Linux and other platform driver implementations.
type driverInterface interface {
	IsAvailable() bool
	GetHardwareType() EnclaveType
	GetDevicePath() string
	ExecuteIOCTL(cmd uint, arg uintptr) error
	Close() error
}

// NewEnclaveEngine constructs and initializes the confidential enclave subsystem.
func NewEnclaveEngine(cfg EnclaveConfig) *EnclaveEngine {
	driver := NewPlatformEnclaveDriver()

	activeType := cfg.EnclaveType
	if driver.IsAvailable() && cfg.EnableHardwareDriver {
		activeType = driver.GetHardwareType()
	}
	cfg.EnclaveType = activeType

	pool := NewEnclaveMemoryPool(cfg.EPCCapacityMB, cfg.PageSizeKB)
	attest := NewAttestationEngine(cfg)
	sealer := NewSecretSealer(cfg.HardwareRootKey, attest.mrenclave, attest.mrsigner)

	return &EnclaveEngine{
		cfg:        cfg,
		status:     EnclaveStatusActive,
		pool:       pool,
		attest:     attest,
		sealer:     sealer,
		driver:     driver,
		peerQuotes: make(map[string]*AttestationQuote),
	}
}

// Start begins any periodic maintenance or health checks.
func (e *EnclaveEngine) Start(ctx context.Context) {
	e.mu.Lock()
	e.status = EnclaveStatusActive
	e.mu.Unlock()
}

// Stop cleanly zeroizes enclave memory pools and closes hardware drivers.
func (e *EnclaveEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status = EnclaveStatusDegraded
	e.pool.Reset()
	_ = e.driver.Close()
}

// MemoryPool returns the underlying EPC memory pool.
func (e *EnclaveEngine) MemoryPool() *EnclaveMemoryPool {
	return e.pool
}

// Driver reports the underlying hardware driver interface.
func (e *EnclaveEngine) Driver() driverInterface {
	return e.driver
}

// GenerateQuote produces a signed attestation quote for the given user data and nonce.
func (e *EnclaveEngine) GenerateQuote(userData, nonce string) (*AttestationQuote, error) {
	start := time.Now()
	quote, err := e.attest.GenerateQuote(userData, nonce)
	if err != nil {
		return nil, err
	}
	atomic.AddInt64(&e.quotesGenerated, 1)
	atomic.AddInt64(&e.totalLatency, time.Since(start).Microseconds())
	return quote, nil
}

// VerifyQuote validates a quote against the supplied verification policy or default.
func (e *EnclaveEngine) VerifyQuote(quote *AttestationQuote, policy *QuoteVerificationPolicy) error {
	start := time.Now()
	pol := e.cfg.DefaultPolicy
	if policy != nil {
		pol = *policy
	}

	err := e.attest.VerifyQuote(quote, pol)
	if err != nil {
		return err
	}

	atomic.AddInt64(&e.quotesVerified, 1)
	atomic.AddInt64(&e.totalLatency, time.Since(start).Microseconds())
	return nil
}

// SealSecret derives an enclave-bound key and encrypts plaintext into a SealedBlob.
func (e *EnclaveEngine) SealSecret(tenantID string, plaintext []byte, policy SealingPolicy, keyID string) (*SealedBlob, error) {
	start := time.Now()
	blob, err := e.sealer.Seal(plaintext, policy, keyID)
	if err != nil {
		return nil, err
	}
	atomic.AddInt64(&e.secretsSealed, 1)
	atomic.AddInt64(&e.totalLatency, time.Since(start).Microseconds())
	return blob, nil
}

// UnsealSecret authenticates and decrypts a SealedBlob into plaintext.
func (e *EnclaveEngine) UnsealSecret(tenantID string, blob *SealedBlob) ([]byte, error) {
	start := time.Now()
	plaintext, err := e.sealer.Unseal(blob)
	if err != nil {
		return nil, err
	}
	atomic.AddInt64(&e.secretsUnsealed, 1)
	atomic.AddInt64(&e.totalLatency, time.Since(start).Microseconds())
	return plaintext, nil
}

// ExecuteConfidentialTask isolates execution within a temporary EPC page, guaranteeing zeroization.
func (e *EnclaveEngine) ExecuteConfidentialTask(tenantID string, taskType string, input []byte) (*ExecutionResult, error) {
	start := time.Now()
	if tenantID == "" {
		tenantID = "default"
	}

	// 1. Allocate 1 EPC page in hardware enclave pool
	pages, err := e.pool.Allocate(tenantID, 1)
	if err != nil {
		return nil, fmt.Errorf("enclave memory allocation failed: %w", err)
	}
	pageID := pages[0]
	defer func() {
		_ = e.pool.Free(tenantID, []int{pageID})
	}()

	// 2. Write payload into isolated EPC page
	if len(input) > e.pool.pageSize {
		return nil, fmt.Errorf("input size %d exceeds page capacity %d", len(input), e.pool.pageSize)
	}
	if err := e.pool.Write(tenantID, pageID, 0, input); err != nil {
		return nil, fmt.Errorf("enclave write error: %w", err)
	}

	// 3. Execute in-enclave computation (e.g. token hash, transform, voucher validation)
	inMem, err := e.pool.Read(tenantID, pageID, 0, len(input))
	if err != nil {
		return nil, fmt.Errorf("enclave read error: %w", err)
	}
	defer Zeroize(inMem)

	var output []byte
	switch taskType {
	case "sha256":
		h := sha256.Sum256(inMem)
		output = []byte(hex.EncodeToString(h[:]))
	case "token_verify":
		mac := sha256.Sum256(append([]byte("verified:"), inMem...))
		output = []byte(hex.EncodeToString(mac[:]))
	default: // "identity" / "pass"
		output = append([]byte("enclave-processed:"), inMem...)
	}

	outHash := sha256.Sum256(output)
	duration := time.Since(start).Microseconds()

	atomic.AddInt64(&e.executionsRun, 1)
	atomic.AddInt64(&e.totalLatency, duration)

	return &ExecutionResult{
		TenantID:   tenantID,
		TaskType:   taskType,
		Output:     string(output),
		OutputHash: hex.EncodeToString(outHash[:]),
		PageID:     pageID,
		DurationUs: duration,
	}, nil
}

// RecordPeerAttestation caches an attestation quote from a remote peer broker.
func (e *EnclaveEngine) RecordPeerAttestation(peerID string, quote *AttestationQuote) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.peerQuotes[peerID] = quote
}

// GetPeerAttestation retrieves a cached peer quote by ID.
func (e *EnclaveEngine) GetPeerAttestation(peerID string) (*AttestationQuote, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	q, ok := e.peerQuotes[peerID]
	return q, ok
}

// ListPeerAttestations lists all cached peer broker attestation quotes.
func (e *EnclaveEngine) ListPeerAttestations() map[string]*AttestationQuote {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make(map[string]*AttestationQuote, len(e.peerQuotes))
	for k, v := range e.peerQuotes {
		out[k] = v
	}
	return out
}

// GetStats compiles current enclave telemetry and performance metrics.
func (e *EnclaveEngine) GetStats() EnclaveStats {
	totalBytes, allocBytes, pageCount, allocPages := e.pool.Stats()

	qGen := atomic.LoadInt64(&e.quotesGenerated)
	qVer := atomic.LoadInt64(&e.quotesVerified)
	sSeal := atomic.LoadInt64(&e.secretsSealed)
	sUnseal := atomic.LoadInt64(&e.secretsUnsealed)
	execs := atomic.LoadInt64(&e.executionsRun)
	totalOps := qGen + qVer + sSeal + sUnseal + execs

	var avgLat float64
	if totalOps > 0 {
		tot := atomic.LoadInt64(&e.totalLatency)
		avgLat = float64(tot) / float64(totalOps)
	}

	e.mu.RLock()
	st := e.status
	e.mu.RUnlock()

	return EnclaveStats{
		EnclaveType:       e.cfg.EnclaveType,
		Status:            st,
		EPCTotalBytes:     totalBytes,
		EPCAllocatedBytes: allocBytes,
		PageCount:         pageCount,
		AllocatedPages:    allocPages,
		QuotesGenerated:   qGen,
		QuotesVerified:    qVer,
		SecretsSealed:     sSeal,
		SecretsUnsealed:   sUnseal,
		ExecutionsRun:     execs,
		AvgLatencyUs:      avgLat,
	}
}

// Reset clears state, zeroizes memory, and reinitializes stats.
func (e *EnclaveEngine) Reset() {
	e.mu.Lock()
	e.peerQuotes = make(map[string]*AttestationQuote)
	e.mu.Unlock()

	e.pool.Reset()
	atomic.StoreInt64(&e.quotesGenerated, 0)
	atomic.StoreInt64(&e.quotesVerified, 0)
	atomic.StoreInt64(&e.secretsSealed, 0)
	atomic.StoreInt64(&e.secretsUnsealed, 0)
	atomic.StoreInt64(&e.executionsRun, 0)
	atomic.StoreInt64(&e.totalLatency, 0)
}
