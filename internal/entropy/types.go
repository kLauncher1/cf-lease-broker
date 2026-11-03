package entropy

import (
	"crypto/rand"
	"time"
)

// EntropySource indicates the origin of harvested physical or pseudorandom entropy.
type EntropySource string

const (
	SourceHardwareTRNG EntropySource = "hardware-trng"
	SourceCPUJitter   EntropySource = "cpu-jitter"
	SourceHybrid      EntropySource = "hybrid-quantum-jitter"
	SourceFallback    EntropySource = "os-csprng"
)

// HealthStatus represents the continuous NIST SP 800-90B health status.
type HealthStatus string

const (
	HealthStatusOptimal  HealthStatus = "optimal"
	HealthStatusDegraded HealthStatus = "degraded"
	HealthStatusFailed   HealthStatus = "failed"
)

// SampleBatch holds raw physical entropy samples collected from jitter or TRNG sources.
type SampleBatch struct {
	RawSamples  []byte        `json:"-"`
	Source      EntropySource `json:"source"`
	SampleCount int           `json:"sample_count"`
	Timestamp   time.Time     `json:"timestamp"`
	DurationUs  int64         `json:"duration_us"`
}

// NISTHealthReport encapsulates SP 800-90B Section 4.4 continuous online test results.
type NISTHealthReport struct {
	RCTPassed       bool         `json:"rct_passed"`
	APTPassed       bool         `json:"apt_passed"`
	RepetitionCount int          `json:"repetition_count"`
	MaxProportion   int          `json:"max_proportion"`
	WindowSize      int          `json:"window_size"`
	ShannonEntropy  float64      `json:"shannon_entropy"`
	MinEntropy      float64      `json:"min_entropy"`
	Status          HealthStatus `json:"status"`
	EvaluatedAt     time.Time    `json:"evaluated_at"`
	Error           string       `json:"error,omitempty"`
}

// BeaconRound represents an immutable round in the Quantum Entropy Beacon hash chain.
type BeaconRound struct {
	Round          uint64        `json:"round"`
	Timestamp      time.Time     `json:"timestamp"`
	PreviousHash   string        `json:"previous_hash"`
	OutputHash     string        `json:"output_hash"`
	Signature      string        `json:"signature"`
	EntropyMinBits float64       `json:"entropy_min_bits"`
	Source         EntropySource `json:"source"`
}

// EntropyConfig configures the entropy engine, beacon generator, and health thresholds.
type EntropyConfig struct {
	BeaconInterval      time.Duration
	PoolCapacity        int
	AttestationKey      []byte
	MinEntropyThreshold float64
	JitterSampleCount   int
	MaxClockSkew        time.Duration
	RCTCutoff           int
	APTWindow           int
	APTCutoff           int
}

// DefaultEntropyConfig returns production defaults for quantum entropy beacon operation.
func DefaultEntropyConfig() EntropyConfig {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		copy(key, []byte("cf-quantum-entropy-beacon-default-key-32b"))
	}
	return EntropyConfig{
		BeaconInterval:      5 * time.Second,
		PoolCapacity:        2048,
		AttestationKey:      key,
		MinEntropyThreshold: 6.0,
		JitterSampleCount:   512,
		MaxClockSkew:        30 * time.Second,
		RCTCutoff:           35,  // SP 800-90B Section 4.4.1 cutoff for 8-bit symbols (alpha=2^-20)
		APTWindow:           512, // SP 800-90B Section 4.4.2 window size
		APTCutoff:           177, // Cutoff threshold for alpha=2^-20 on 8-bit symbols
	}
}

// EntropyStats exposes real-time operational telemetry for monitoring and metrics.
type EntropyStats struct {
	TotalHarvestedBytes  uint64           `json:"total_harvested_bytes"`
	TotalTokensGenerated uint64           `json:"total_tokens_generated"`
	CurrentPoolSize      int              `json:"current_pool_size"`
	BeaconCurrentRound   uint64           `json:"beacon_current_round"`
	BeaconLastOutputHash string           `json:"beacon_last_output_hash"`
	ActiveSource         EntropySource    `json:"active_source"`
	LastHealthReport     NISTHealthReport `json:"last_health_report"`
	UptimeSeconds        int64            `json:"uptime_seconds"`
	RCTFailureCount      uint64           `json:"rct_failure_count"`
	APTFailureCount      uint64           `json:"apt_failure_count"`
}
