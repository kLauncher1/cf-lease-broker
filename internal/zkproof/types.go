package zkproof

import (
	"errors"
	"time"
)

// Common error definitions for the Zero-Knowledge Proof subsystem.
var (
	ErrInvalidProofPoint       = errors.New("zkproof: invalid elliptic curve proof point")
	ErrInvalidPublicInputs     = errors.New("zkproof: public inputs count does not match verification key")
	ErrProofVerificationFailed = errors.New("zkproof: proof verification equation check failed")
	ErrProofReplayDetected     = errors.New("zkproof: proof nullifier already consumed (replay attack detected)")
	ErrKeyNotFound             = errors.New("zkproof: verification key not found")
	ErrUnknownStatementType    = errors.New("zkproof: unsupported proof statement type")
	ErrInvalidWitness          = errors.New("zkproof: secret witness does not satisfy statement constraints")
)

// StatementType identifies the cryptographic assertion being proven in zero knowledge.
type StatementType string

const (
	// StatementWorkerIsolation proves Edge Worker execution conforms to linear memory and gas ceilings.
	StatementWorkerIsolation StatementType = "worker_isolation"
	// StatementQuotaAttestation proves tenant resource usage <= quota limit without exposing Cloudflare API keys.
	StatementQuotaAttestation StatementType = "quota_attestation"
	// StatementBoundaryIntegrity proves inter-broker tenant lease boundary invariants.
	StatementBoundaryIntegrity StatementType = "boundary_integrity"
)

// ProofPoint represents an affine point (X, Y) on the elliptic curve over F_p.
type ProofPoint struct {
	X        string `json:"x"`
	Y        string `json:"y"`
	Infinity bool   `json:"infinity,omitempty"`
}

// ZKProof models a succinct non-interactive argument of knowledge (zk-SNARK).
type ZKProof struct {
	ProofID      string        `json:"proof_id"`
	Type         StatementType `json:"type"`
	A            ProofPoint    `json:"a"`
	B            ProofPoint    `json:"b"`
	C            ProofPoint    `json:"c"`
	PublicInputs []string      `json:"public_inputs"`
	Commitment   string        `json:"commitment"`
	Nullifier    string        `json:"nullifier"`
	CreatedAt    int64         `json:"created_at"`
}

// VerificationKey contains public reference parameters for verifying a zk-SNARK proof.
type VerificationKey struct {
	KeyID     string        `json:"key_id"`
	Type      StatementType `json:"type"`
	Alpha     ProofPoint    `json:"alpha"`
	Beta      ProofPoint    `json:"beta"`
	Gamma     ProofPoint    `json:"gamma"`
	Delta     ProofPoint    `json:"delta"`
	IC        []ProofPoint  `json:"ic"` // Input consistency points
	CreatedAt time.Time     `json:"created_at"`
}

// ProofStatement encapsulates the public statement parameters.
type ProofStatement struct {
	StatementID        string            `json:"statement_id"`
	Type               StatementType     `json:"type"`
	TenantID           string            `json:"tenant_id"`
	WorkerHash         string            `json:"worker_hash,omitempty"`
	GasLimit           uint64            `json:"gas_limit,omitempty"`
	MemoryCeilingPages uint32            `json:"memory_ceiling_pages,omitempty"`
	CurrentUsage       uint64            `json:"current_usage,omitempty"`
	MaxQuota           uint64            `json:"max_quota,omitempty"`
	PublicParameters   map[string]string `json:"public_parameters,omitempty"`
}

// VerificationResult summarizes the outcome of single or batch proof verification.
type VerificationResult struct {
	Valid              bool          `json:"valid"`
	ProofID            string        `json:"proof_id"`
	StatementType      StatementType `json:"statement_type"`
	VerificationTimeUs int64         `json:"verification_time_us"`
	VectorAccelerated  bool          `json:"vector_accelerated"`
	Reason             string        `json:"reason,omitempty"`
}

// ZKVerifierConfig configures proof generation, verification, and hardware vectorization.
type ZKVerifierConfig struct {
	EnableVectorAccel bool          `json:"enable_vector_accel"`
	SimulateAVX512    bool          `json:"simulate_avx512"`
	MaxBatchSize      int           `json:"max_batch_size"`
	NullifierTTL      time.Duration `json:"nullifier_ttl"`
}

// DefaultVerifierConfig provides production-ready configuration defaults.
func DefaultVerifierConfig() ZKVerifierConfig {
	return ZKVerifierConfig{
		EnableVectorAccel: true,
		SimulateAVX512:    true,
		MaxBatchSize:      64,
		NullifierTTL:      1 * time.Hour,
	}
}

// ZKVerifierStats tracks real-time verification throughput and hardware vectorization metrics.
type ZKVerifierStats struct {
	TotalProofsGenerated    uint64  `json:"total_proofs_generated"`
	TotalProofsVerified     uint64  `json:"total_proofs_verified"`
	TotalProofsRejected     uint64  `json:"total_proofs_rejected"`
	TotalBatchVerifications uint64  `json:"total_batch_verifications"`
	ActiveVerificationKeys  int     `json:"active_verification_keys"`
	AverageLatencyUs        float64 `json:"average_latency_us"`
	VectorSpeedupRatio      float64 `json:"vector_speedup_ratio"`
	NullifiersTracked       int     `json:"nullifiers_tracked"`
}
