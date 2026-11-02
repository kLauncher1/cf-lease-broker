package scrubber

import (
	"errors"
	"time"
)

// Common error definitions for the DDoS scrubbing and mitigation domain.
var (
	ErrPacketTooShort     = errors.New("packet length shorter than required header bounds")
	ErrInvalidIPHeader    = errors.New("malformed or unparseable IP header")
	ErrIPBanned           = errors.New("source IP address is actively quarantined in ban list")
	ErrChallengeExpired   = errors.New("cryptographic challenge token has expired")
	ErrChallengeInvalid   = errors.New("cryptographic challenge token verification failed")
	ErrChallengeReplayed  = errors.New("cryptographic challenge nonce has already been consumed")
	ErrTenantPolicyNotFound = errors.New("tenant scrubber policy not found")
)

// AttackType classifies recognized malicious packet vectors.
type AttackType string

const (
	AttackTypeNone             AttackType = "none"
	AttackTypeSYNFlood         AttackType = "syn_flood"
	AttackTypeUDPAmplification AttackType = "udp_amplification"
	AttackTypeHTTPFlood        AttackType = "http_flood"
	AttackTypeMalformedHeader  AttackType = "malformed_header"
	AttackTypeSlowloris        AttackType = "slowloris"
)

// Action defines the line-rate or application action taken for a packet/request.
type Action string

const (
	ActionPass      Action = "XDP_PASS"
	ActionDrop      Action = "XDP_DROP"
	ActionChallenge Action = "XDP_TX_CHALLENGE"
	ActionRateLimit Action = "RATE_LIMIT"
)

// ReputationLevel classifies the trust tier of an IP address.
type ReputationLevel string

const (
	ReputationClean      ReputationLevel = "clean"      // Score >= 80
	ReputationSuspicious ReputationLevel = "suspicious" // Score 50..79
	ReputationAnomalous  ReputationLevel = "anomalous"  // Score 20..49
	ReputationBanned     ReputationLevel = "banned"     // Score < 20
)

// PacketProtocol enumerates L4 transport protocols.
type PacketProtocol string

const (
	ProtocolTCP  PacketProtocol = "TCP"
	ProtocolUDP  PacketProtocol = "UDP"
	ProtocolICMP PacketProtocol = "ICMP"
	ProtocolOther PacketProtocol = "OTHER"
)

// TCPFlags captures relevant TCP control bits.
type TCPFlags struct {
	SYN bool `json:"syn"`
	ACK bool `json:"ack"`
	FIN bool `json:"fin"`
	RST bool `json:"rst"`
	PSH bool `json:"psh"`
	URG bool `json:"urg"`
}

// PacketMetadata encapsulates decomposed L3/L4 packet attributes for zero-copy inspection.
type PacketMetadata struct {
	SrcIP       string         `json:"src_ip"`
	DstIP       string         `json:"dst_ip"`
	SrcPort     int            `json:"src_port"`
	DstPort     int            `json:"dst_port"`
	Protocol    PacketProtocol `json:"protocol"`
	TCPFlags    TCPFlags       `json:"tcp_flags,omitempty"`
	PayloadSize int            `json:"payload_size"`
	Timestamp   time.Time      `json:"timestamp"`
}

// IPReputationRecord maintains stateful behavioral history and EWMA score for a client IP.
type IPReputationRecord struct {
	IP                 string          `json:"ip"`
	Score              float64         `json:"score"` // 0.0 (malicious) to 100.0 (pristine)
	Level              ReputationLevel `json:"level"`
	TotalRequests      int64           `json:"total_requests"`
	TotalDrops         int64           `json:"total_drops"`
	SYNCount           int64           `json:"syn_count"`
	ACKCount           int64           `json:"ack_count"`
	UDPBytes           int64           `json:"udp_bytes"`
	ChallengesIssued   int64           `json:"challenges_issued"`
	ChallengesSolved   int64           `json:"challenges_solved"`
	FirstSeen          time.Time       `json:"first_seen"`
	LastSeen           time.Time       `json:"last_seen"`
	BannedUntil        time.Time       `json:"banned_until,omitempty"`
	BanReason          string          `json:"ban_reason,omitempty"`
	AnomalousSignatures []string       `json:"anomalous_signatures,omitempty"`
}

// ChallengeToken represents a cryptographically verifiable challenge issued to anomalous clients.
type ChallengeToken struct {
	TokenID   string    `json:"token_id"`
	ClientIP  string    `json:"client_ip"`
	TenantID  string    `json:"tenant_id"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Nonce     string    `json:"nonce"`
	Signature string    `json:"signature"`
	Verified  bool      `json:"verified"`
}

// ScrubDecision is the deterministic outcome of a packet or request evaluation.
type ScrubDecision struct {
	Action          Action          `json:"action"`
	AttackType      AttackType      `json:"attack_type"`
	ClientIP        string          `json:"client_ip"`
	TenantID        string          `json:"tenant_id"`
	ReputationScore float64         `json:"reputation_score"`
	ReputationLevel ReputationLevel `json:"reputation_level"`
	LatencyUs       int64           `json:"latency_us"`
	Reason          string          `json:"reason"`
	ChallengeIssued *ChallengeToken `json:"challenge_issued,omitempty"`
}

// TenantScrubberPolicy defines per-tenant line-rate mitigation bounds and tolerances.
type TenantScrubberPolicy struct {
	TenantID             string  `json:"tenant_id"`
	MaxRPS               int64   `json:"max_rps"`
	BurstAllowance       int64   `json:"burst_allowance"`
	ChallengeThreshold   float64 `json:"challenge_threshold"` // Reputation score below which challenge is triggered (e.g. 50.0)
	DropThreshold        float64 `json:"drop_threshold"`      // Reputation score below which packets are dropped (e.g. 20.0)
	MaxSYNRPS            int64   `json:"max_syn_rps"`         // Line-rate SYN rate ceiling per source IP
	MaxAmplificationSize int     `json:"max_amplification_sz"` // Max allowed UDP payload on known amplification ports
	EnableLineRateXDP    bool    `json:"enable_line_rate_xdp"`
	StrictBanning        bool    `json:"strict_banning"`
}

// ScrubberStats aggregates high-throughput telemetry metrics across the scrubbing pipeline.
type ScrubberStats struct {
	TotalPacketsInspected int64   `json:"total_packets_inspected"`
	PacketsPassed         int64   `json:"packets_passed"`
	PacketsDropped        int64   `json:"packets_dropped"`
	ChallengesIssued      int64   `json:"challenges_issued"`
	ChallengesVerified    int64   `json:"challenges_verified"`
	ActiveBans            int     `json:"active_bans"`
	TrackedIPs            int     `json:"tracked_ips"`
	AvgMitigationLatencyUs float64 `json:"avg_mitigation_latency_us"`
	SYNFloodsMitigated    int64   `json:"syn_floods_mitigated"`
	UDPFloodsMitigated    int64   `json:"udp_floods_mitigated"`
	L7FloodsMitigated     int64   `json:"l7_floods_mitigated"`
	MalformedPacketsDropped int64 `json:"malformed_packets_dropped"`
	DriverMode            string  `json:"driver_mode"`
	UptimeSeconds         int64   `json:"uptime_seconds"`
}

// ScrubberConfig configures runtime engine thresholds and cryptographic secrets.
type ScrubberConfig struct {
	HMACSecret             []byte        `json:"-"`
	ChallengeTTL           time.Duration `json:"challenge_ttl"`
	DefaultBanDuration     time.Duration `json:"default_ban_duration"`
	ReputationDecayPeriod  time.Duration `json:"reputation_decay_period"`
	DefaultPolicy          TenantScrubberPolicy `json:"default_policy"`
}

// DefaultScrubberConfig returns hardened production defaults.
func DefaultScrubberConfig() ScrubberConfig {
	return ScrubberConfig{
		HMACSecret:            []byte("cf-broker-xdp-scrubber-secret-key-32b"),
		ChallengeTTL:          5 * time.Minute,
		DefaultBanDuration:    15 * time.Minute,
		ReputationDecayPeriod: 30 * time.Minute,
		DefaultPolicy: TenantScrubberPolicy{
			TenantID:             "default",
			MaxRPS:               1000,
			BurstAllowance:       250,
			ChallengeThreshold:   50.0,
			DropThreshold:        20.0,
			MaxSYNRPS:            100,
			MaxAmplificationSize: 512,
			EnableLineRateXDP:    true,
			StrictBanning:        true,
		},
	}
}
