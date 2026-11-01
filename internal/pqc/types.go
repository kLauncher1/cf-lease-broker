package pqc

import (
	"errors"
	"time"
)

// Standard PQC Algorithm identifiers.
const (
	AlgorithmMLKEM768       = "ML-KEM-768"
	AlgorithmMLKEM1024      = "ML-KEM-1024"
	AlgorithmHybridX25519   = "Hybrid-MLKEM768-X25519"
	AlgorithmMLDSA65        = "ML-DSA-65"
	AlgorithmMLDSA87        = "ML-DSA-87"
)

// Entropy source classifications.
const (
	SourceHardwareTRNG = "hardware_trng"
	SourceJitterRNG    = "cpu_jitter"
	SourceHybridPool   = "hybrid_quantum_pool"
)

// Common PQC errors.
var (
	ErrSessionNotFound     = errors.New("pqc: session not found or expired")
	ErrDecapsulationFailed = errors.New("pqc: decapsulation verification failed")
	ErrSignatureInvalid    = errors.New("pqc: digital signature validation failed")
	ErrKeyNotFound         = errors.New("pqc: public key not found")
	ErrEntropyDegraded     = errors.New("pqc: entropy health check below threshold")
	ErrReplayDetected      = errors.New("pqc: counter replay or expired voucher detected")
	ErrPayloadTooLarge     = errors.New("pqc: request payload exceeds limit")
)

// KeyExchangeSession tracks an in-flight or established post-quantum key encapsulation session.
type KeyExchangeSession struct {
	SessionID          string    `json:"session_id"`
	Algorithm          string    `json:"algorithm"`
	ClientPublicKey    string    `json:"client_public_key"`
	ServerPublicKey    string    `json:"server_public_key,omitempty"`
	Ciphertext         string    `json:"ciphertext,omitempty"`
	SharedSecretHex    string    `json:"shared_secret_hex,omitempty"`
	HybridActive       bool      `json:"hybrid_active"`
	HandshakeLatencyUs int64     `json:"handshake_latency_us"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}

// MLDSAKeyPair holds public and private lattice key parameters for digital signing.
type MLDSAKeyPair struct {
	KeyID       string    `json:"key_id"`
	Algorithm   string    `json:"algorithm"`
	PublicKey   []byte    `json:"public_key"`
	PrivateKey  []byte    `json:"-"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

// VoucherSignature represents an authenticated ML-DSA signature over an inter-broker lease voucher.
type VoucherSignature struct {
	KeyID        string    `json:"key_id"`
	Algorithm    string    `json:"algorithm"`
	Signature    string    `json:"signature"`
	StateCounter uint64    `json:"state_counter"`
	SignedAt     time.Time `json:"signed_at"`
	Nonce        string    `json:"nonce"`
}

// EntropyHarvestStats summarizes continuous quantum and jitter entropy harvesting metrics.
type EntropyHarvestStats struct {
	TotalBytesHarvested uint64    `json:"total_bytes_harvested"`
	MinEntropyBits      float64   `json:"min_entropy_bits"`
	ShannonEntropy      float64   `json:"shannon_entropy"`
	TRNGActive          bool      `json:"trng_active"`
	JitterCyclesSampled uint64    `json:"jitter_cycles_sampled"`
	HealthStatus        string    `json:"health_status"`
	LastHarvestTime     time.Time `json:"last_harvest_time"`
}

// PQCStats tracks runtime cryptographic operations and telemetry.
type PQCStats struct {
	TotalKeyExchanges       uint64  `json:"total_key_exchanges"`
	TotalEncapsulations     uint64  `json:"total_encapsulations"`
	TotalDecapsulations     uint64  `json:"total_decapsulations"`
	TotalSignaturesIssued   uint64  `json:"total_signatures_issued"`
	TotalSignaturesVerified uint64  `json:"total_signatures_verified"`
	FailedVerifications     uint64  `json:"failed_verifications"`
	AverageHandshakeUs      float64 `json:"average_handshake_us"`
	ActiveSessionsCount     int     `json:"active_sessions_count"`
}

// PQCConfig controls post-quantum security policies and thresholds.
type PQCConfig struct {
	DefaultKEMAlgorithm    string        `json:"default_kem_algorithm"`
	DefaultDSAAlgorithm    string        `json:"default_dsa_algorithm"`
	SessionTTL             time.Duration `json:"session_ttl"`
	MaxConcurrentSessions  int           `json:"max_concurrent_sessions"`
	EnforceHybridExchange  bool          `json:"enforce_hybrid_exchange"`
	RequireHardwareEntropy bool          `json:"require_hardware_entropy"`
	HarvestInterval        time.Duration `json:"harvest_interval"`
}

// DefaultPQCConfig provides production defaults for zero-trust edge environments.
func DefaultPQCConfig() PQCConfig {
	return PQCConfig{
		DefaultKEMAlgorithm:    AlgorithmHybridX25519,
		DefaultDSAAlgorithm:    AlgorithmMLDSA65,
		SessionTTL:             10 * time.Minute,
		MaxConcurrentSessions:  2048,
		EnforceHybridExchange:  true,
		RequireHardwareEntropy: true,
		HarvestInterval:        30 * time.Second,
	}
}
