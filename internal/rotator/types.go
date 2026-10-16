package rotator

import (
	"time"
)

// TokenState represents the current lifecycle phase of a rotated token.
type TokenState string

const (
	TokenStateActive   TokenState = "ACTIVE"
	TokenStateRenewing TokenState = "RENEWING"
	TokenStateRenewed  TokenState = "RENEWED"
	TokenStateRevoked  TokenState = "REVOKED"
	TokenStateExpired  TokenState = "EXPIRED"
)

// RotatedToken models a managed Cloudflare edge token with proactive renewal metadata.
type RotatedToken struct {
	TokenID          string     `json:"token_id"`
	LeaseID          string     `json:"lease_id"`
	AccountID        string     `json:"account_id"`
	TenantID         string     `json:"tenant_id"`
	TargetService    string     `json:"target_service"`
	TokenKey         string     `json:"token_key"`
	PreviousTokenKey string     `json:"previous_token_key,omitempty"`
	IssuedAt         time.Time  `json:"issued_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	RenewAt          time.Time  `json:"renew_at"`
	GraceUntil       time.Time  `json:"grace_until,omitempty"`
	RenewalCount     int        `json:"renewal_count"`
	State            TokenState `json:"state"`
	LastRenewedAt    time.Time  `json:"last_renewed_at,omitempty"`
	AttestationHash  string     `json:"attestation_hash,omitempty"`
}

// RotatorConfig defines parameters governing proactive renewal and handover.
type RotatorConfig struct {
	CheckInterval          time.Duration `json:"check_interval"`
	RenewalThresholdRatio  float64       `json:"renewal_threshold_ratio"` // e.g. 0.80 triggers at 80% of TTL
	GracePeriod            time.Duration `json:"grace_period"`             // e.g. 30s handover dual validity
	MaxRenewals            int           `json:"max_renewals"`
	AutoRenewEnabled       bool          `json:"auto_renew_enabled"`
}

// DefaultRotatorConfig provides production-grade defaults for edge token rotation.
func DefaultRotatorConfig() RotatorConfig {
	return RotatorConfig{
		CheckInterval:         2 * time.Second,
		RenewalThresholdRatio: 0.80,
		GracePeriod:           30 * time.Second,
		MaxRenewals:           100,
		AutoRenewEnabled:      true,
	}
}

// RenewalEvent records an audit record whenever a token is rotated.
type RenewalEvent struct {
	EventID     string    `json:"event_id"`
	TokenID     string    `json:"token_id"`
	LeaseID     string    `json:"lease_id"`
	OldTokenKey string    `json:"old_token_key"`
	NewTokenKey string    `json:"new_token_key"`
	RenewedAt   time.Time `json:"renewed_at"`
	DurationMs  int64     `json:"duration_ms"`
	Reason      string    `json:"reason"`
	Attested    bool      `json:"attested"`
}

// RevocationTombstone carries cryptographic proof that a lease or token was invalidated.
type RevocationTombstone struct {
	LeaseID            string    `json:"lease_id"`
	TokenID            string    `json:"token_id"`
	Reason             string    `json:"reason"`
	RevokedAt          time.Time `json:"revoked_at"`
	RevokedAtUnixNano  int64     `json:"revoked_at_unix_nano"`
	RevokerNodeID      string    `json:"revoker_node_id"`
	SequenceNum        uint64    `json:"sequence_num"`
	SignatureHex       string    `json:"signature_hex"`
}

// GossipPeer tracks a participating broker node in the revocation mesh.
type GossipPeer struct {
	NodeID    string    `json:"node_id"`
	Address   string    `json:"address"`
	LastSeen  time.Time `json:"last_seen"`
	Status    string    `json:"status"`
	LatencyMs float64   `json:"latency_ms"`
}

// GossipPacket is exchanged between mesh nodes to propagate tombstones and membership.
type GossipPacket struct {
	SenderNodeID string                `json:"sender_node_id"`
	Sequence     uint64                `json:"sequence"`
	Tombstones   []RevocationTombstone `json:"tombstones"`
	Peers        []GossipPeer          `json:"peers,omitempty"`
	SentAt       time.Time             `json:"sent_at"`
}

// DelegatedRefreshRequest is sent by an edge worker or secondary broker to refresh a token.
type DelegatedRefreshRequest struct {
	TokenID         string `json:"token_id"`
	LeaseID         string `json:"lease_id"`
	TenantID        string `json:"tenant_id"`
	TargetService   string `json:"target_service"`
	DelegatedByNode string `json:"delegated_by_node"`
	Nonce           string `json:"nonce"`
	HardwareQuote   string `json:"hardware_quote"`
	PCRDigest       string `json:"pcr_digest"`
	Timestamp       int64  `json:"timestamp"`
}

// DelegatedRefreshResponse returns the result of an attested token refresh.
type DelegatedRefreshResponse struct {
	Success            bool      `json:"success"`
	TokenID            string    `json:"token_id"`
	NewTokenKey        string    `json:"new_token_key,omitempty"`
	ExpiresAt          time.Time `json:"expires_at,omitempty"`
	AttestationVerdict string    `json:"attestation_verdict"`
	Error              string    `json:"error,omitempty"`
}

// RotatorSummary provides real-time telemetry for monitoring panels and health checks.
type RotatorSummary struct {
	ActiveTokens           int       `json:"active_tokens"`
	RenewedCount           int       `json:"renewed_count"`
	RevokedCount           int       `json:"revoked_count"`
	MeshPeersCount         int       `json:"mesh_peers_count"`
	AvgPropagationLatencyMs float64  `json:"avg_propagation_latency_ms"`
	AutoRenewEnabled       bool      `json:"auto_renew_enabled"`
	CheckIntervalSec       float64   `json:"check_interval_sec"`
	GracePeriodSec         float64   `json:"grace_period_sec"`
	LastSweepAt            time.Time `json:"last_sweep_at"`
}
