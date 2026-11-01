package zkproof

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"time"
)

// ZKProofEngine provides a unified zero-knowledge proof coordinator for Edge Worker isolation and quota attestations.
type ZKProofEngine struct {
	mu            sync.RWMutex
	prover        *ZKProver
	verifier      *ZKVerifier
	accel         *VectorAccelerator
	config        ZKVerifierConfig
	cancelFunc    context.CancelFunc
	recentResults []*VerificationResult
	maxRecent     int

	// Atomic telemetry counters
	totalGenerated uint64
	totalVerified  uint64
	totalRejected  uint64
	totalLatencyUs uint64
}

// NewZKProofEngine constructs and initializes a new Zero-Knowledge Proof Engine with default keys.
func NewZKProofEngine(cfg ZKVerifierConfig) *ZKProofEngine {
	if cfg.MaxBatchSize <= 0 {
		cfg.MaxBatchSize = 64
	}
	if cfg.NullifierTTL <= 0 {
		cfg.NullifierTTL = 1 * time.Hour
	}

	prover := NewZKProver()
	verifier := NewZKVerifier(cfg.NullifierTTL)
	accel := NewVectorAccelerator(cfg.SimulateAVX512)

	engine := &ZKProofEngine{
		prover:        prover,
		verifier:      verifier,
		accel:         accel,
		config:        cfg,
		recentResults: make([]*VerificationResult, 0, 50),
		maxRecent:     50,
	}

	engine.SeedDefaultVerificationKeys()
	return engine
}

// Start begins background maintenance routines, such as nullifier memory pruning.
func (e *ZKProofEngine) Start() {
	e.mu.Lock()
	if e.cancelFunc != nil {
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFunc = cancel
	e.mu.Unlock()

	go e.runMaintenanceLoop(ctx)
}

// Stop cleanly terminates background maintenance workers.
func (e *ZKProofEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancelFunc != nil {
		e.cancelFunc()
		e.cancelFunc = nil
	}
}

func (e *ZKProofEngine) runMaintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.verifier.PruneExpiredNullifiers(time.Now())
		}
	}
}

// SeedDefaultVerificationKeys provisions production verification parameters for standard statement types.
func (e *ZKProofEngine) SeedDefaultVerificationKeys() {
	makeVK := func(st StatementType, icCount int) *VerificationKey {
		alphaPt := ScalarMult(G1, HashToScalar([]byte("CF_BROKER_ALPHA_PARAM"), []byte(st)))
		betaPt := ScalarMult(H1, HashToScalar([]byte("CF_BROKER_BETA_PARAM"), []byte(st)))
		gammaPt := ScalarMult(G1, HashToScalar([]byte("CF_BROKER_GAMMA_PARAM"), []byte(st)))
		deltaPt := ScalarMult(H1, HashToScalar([]byte("CF_BROKER_DELTA_PARAM"), []byte(st)))

		icList := make([]ProofPoint, icCount)
		for i := 0; i < icCount; i++ {
			pt := ScalarMult(G1, HashToScalar([]byte("CF_BROKER_IC_PARAM"), []byte(st), []byte{byte(i)}))
			icList[i] = PointToProofPoint(pt)
		}

		return &VerificationKey{
			KeyID:     "vk_" + string(st),
			Type:      st,
			Alpha:     PointToProofPoint(alphaPt),
			Beta:      PointToProofPoint(betaPt),
			Gamma:     PointToProofPoint(gammaPt),
			Delta:     PointToProofPoint(deltaPt),
			IC:        icList,
			CreatedAt: time.Now(),
		}
	}

	vkWorker := makeVK(StatementWorkerIsolation, 5) // IC_0 + 4 public inputs
	vkQuota := makeVK(StatementQuotaAttestation, 5)  // IC_0 + 4 public inputs
	vkBoundary := makeVK(StatementBoundaryIntegrity, 3) // IC_0 + 2 public inputs

	for _, vk := range []*VerificationKey{vkWorker, vkQuota, vkBoundary} {
		e.prover.RegisterVerificationKey(vk)
		e.verifier.RegisterKey(vk)
	}
}

// ProveWorkerIsolation generates a verified zk-SNARK proof demonstrating worker sandbox isolation.
func (e *ZKProofEngine) ProveWorkerIsolation(
	stmt ProofStatement,
	witnessSecret string,
	gasWitness uint64,
	memWitness uint32,
) (*ZKProof, error) {
	proof, err := e.prover.GenerateWorkerIsolationProof(stmt, witnessSecret, gasWitness, memWitness)
	if err != nil {
		return nil, err
	}
	atomic.AddUint64(&e.totalGenerated, 1)
	return proof, nil
}

// ProveQuotaAttestation generates an anonymous proof of quota compliance without leaking Cloudflare credentials.
func (e *ZKProofEngine) ProveQuotaAttestation(
	stmt ProofStatement,
	apiKeyWitness string,
	usageWitness uint64,
	blindingFactor *big.Int,
) (*ZKProof, error) {
	proof, err := e.prover.GenerateQuotaAttestationProof(stmt, apiKeyWitness, usageWitness, blindingFactor)
	if err != nil {
		return nil, err
	}
	atomic.AddUint64(&e.totalGenerated, 1)
	return proof, nil
}

// ProveBoundaryIntegrity generates a zero-knowledge argument verifying inter-broker peering rules.
func (e *ZKProofEngine) ProveBoundaryIntegrity(
	stmt ProofStatement,
	interBrokerSecret string,
) (*ZKProof, error) {
	proof, err := e.prover.GenerateBoundaryIntegrityProof(stmt, interBrokerSecret)
	if err != nil {
		return nil, err
	}
	atomic.AddUint64(&e.totalGenerated, 1)
	return proof, nil
}

// Verify evaluates a single zk-SNARK proof against registered verification keys and nullifiers.
func (e *ZKProofEngine) Verify(proof *ZKProof) (*VerificationResult, error) {
	res, err := e.verifier.VerifyProof(proof)
	e.recordVerification(res)
	return res, err
}

// VerifyBatch evaluates multiple proofs in a single pass using the hardware vector accelerator.
func (e *ZKProofEngine) VerifyBatch(proofs []*ZKProof) ([]*VerificationResult, error) {
	if len(proofs) == 0 {
		return nil, nil
	}
	results, err := e.accel.VerifyBatch(e.verifier, proofs)
	for _, res := range results {
		if res != nil {
			e.recordVerification(res)
		}
	}
	return results, err
}

func (e *ZKProofEngine) recordVerification(res *VerificationResult) {
	if res == nil {
		return
	}
	if res.Valid {
		atomic.AddUint64(&e.totalVerified, 1)
	} else {
		atomic.AddUint64(&e.totalRejected, 1)
	}
	atomic.AddUint64(&e.totalLatencyUs, uint64(res.VerificationTimeUs))

	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.recentResults) >= e.maxRecent {
		e.recentResults = e.recentResults[1:]
	}
	e.recentResults = append(e.recentResults, res)
}

// GetStats compiles real-time telemetry metrics on the zk-SNARK verifier.
func (e *ZKProofEngine) GetStats() ZKVerifierStats {
	gen := atomic.LoadUint64(&e.totalGenerated)
	ver := atomic.LoadUint64(&e.totalVerified)
	rej := atomic.LoadUint64(&e.totalRejected)
	lat := atomic.LoadUint64(&e.totalLatencyUs)

	totalOps := ver + rej
	var avgLat float64
	if totalOps > 0 {
		avgLat = float64(lat) / float64(totalOps)
	}

	accelStats := e.accel.GetStats()

	return ZKVerifierStats{
		TotalProofsGenerated:    gen,
		TotalProofsVerified:     ver,
		TotalProofsRejected:     rej,
		TotalBatchVerifications: accelStats.TotalBatchRuns,
		ActiveVerificationKeys:  len(e.verifier.ListKeys()),
		AverageLatencyUs:        avgLat,
		VectorSpeedupRatio:      accelStats.SpeedupFactor,
		NullifiersTracked:       e.verifier.NullifiersCount(),
	}
}

// ListKeys returns all registered verification keys.
func (e *ZKProofEngine) ListKeys() []*VerificationKey {
	return e.verifier.ListKeys()
}

// ListRecentVerifications returns up to 50 recent verification results.
func (e *ZKProofEngine) ListRecentVerifications() []*VerificationResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*VerificationResult, len(e.recentResults))
	copy(out, e.recentResults)
	return out
}

// Reset clears metrics and nullifier histories.
func (e *ZKProofEngine) Reset() {
	atomic.StoreUint64(&e.totalGenerated, 0)
	atomic.StoreUint64(&e.totalVerified, 0)
	atomic.StoreUint64(&e.totalRejected, 0)
	atomic.StoreUint64(&e.totalLatencyUs, 0)

	e.verifier.ResetNullifiers()

	e.mu.Lock()
	e.recentResults = make([]*VerificationResult, 0, e.maxRecent)
	e.mu.Unlock()
}

// Prover exposes the underlying prover instance.
func (e *ZKProofEngine) Prover() *ZKProver {
	return e.prover
}

// Verifier exposes the underlying verifier instance.
func (e *ZKProofEngine) Verifier() *ZKVerifier {
	return e.verifier
}

// Accelerator exposes the underlying vector accelerator instance.
func (e *ZKProofEngine) Accelerator() *VectorAccelerator {
	return e.accel
}
