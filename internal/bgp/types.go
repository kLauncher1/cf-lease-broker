package bgp

import (
	"errors"
	"time"
)

// Standard BGP message types according to RFC 4271.
const (
	TypeOpen         uint8 = 1
	TypeUpdate       uint8 = 2
	TypeNotification uint8 = 3
	TypeKeepalive    uint8 = 4
	TypeRouteRefresh uint8 = 5 // RFC 2918
)

// Standard BGP Path Attribute Type Codes (RFC 4271 / RFC 1997).
const (
	AttrOrigin          uint8 = 1
	AttrASPath          uint8 = 2
	AttrNextHop         uint8 = 3
	AttrMultiExitDisc   uint8 = 4
	AttrLocalPref       uint8 = 5
	AttrAtomicAggregate uint8 = 6
	AttrAggregator      uint8 = 7
	AttrCommunities     uint8 = 8
)

// Origin attribute values.
const (
	OriginIGP        uint8 = 0
	OriginEGP        uint8 = 1
	OriginIncomplete uint8 = 2
)

// AS Path segment types.
const (
	ASSet      uint8 = 1
	ASSequence uint8 = 2
)

// RFC 1997 Well-Known BGP Communities.
const (
	CommunityNoExport          = "NO_EXPORT"          // 0xFFFFFF01
	CommunityNoAdvertise       = "NO_ADVERTISE"       // 0xFFFFFF02
	CommunityNoExportSubConfed = "NO_EXPORT_SUBCONFED" // 0xFFFFFF03
)

// Custom Transit Traffic Engineering Communities.
const (
	CommunityTransitLowCost    = "64512:100"
	CommunityTransitLowLatency = "64512:200"
	CommunityTransitBalanced   = "64512:300"
	CommunityRegionAmericas    = "64512:1000"
	CommunityRegionEMEA        = "64512:2000"
	CommunityRegionAPAC        = "64512:3000"
	CommunityDoNotRoute        = "64512:9999"
)

// FSMState denotes the BGP-4 Finite State Machine state (RFC 4271 Section 8).
type FSMState string

const (
	StateIdle        FSMState = "IDLE"
	StateConnect     FSMState = "CONNECT"
	StateActive      FSMState = "ACTIVE"
	StateOpenSent    FSMState = "OPENSENT"
	StateOpenConfirm FSMState = "OPENCONFIRM"
	StateEstablished FSMState = "ESTABLISHED"
)

// PrefixStatus indicates the broadcast status of an Anycast prefix.
type PrefixStatus string

const (
	PrefixActive    PrefixStatus = "ACTIVE"
	PrefixWithdrawn PrefixStatus = "WITHDRAWN"
	PrefixDiverted  PrefixStatus = "DIVERTED"
)

// PeerConfig specifies configuration parameters for an upstream BGP peer.
type PeerConfig struct {
	PeerASN           uint32        `json:"peer_asn"`
	PeerIP            string        `json:"peer_ip"`
	LocalASN          uint32        `json:"local_asn"`
	LocalIP           string        `json:"local_ip"`
	HoldTime          time.Duration `json:"hold_time"`
	KeepaliveInterval time.Duration `json:"keepalive_interval"`
	Description       string        `json:"description,omitempty"`
	TransitCostPerMb  float64       `json:"transit_cost_per_mb"`
	Tier1Provider     string        `json:"tier1_provider"`
}

// AnycastPrefix represents an Anycast IP network prefix announced to upstream transits.
type AnycastPrefix struct {
	CIDR            string       `json:"cidr"`
	OriginASN       uint32       `json:"origin_asn"`
	NextHop         string       `json:"next_hop"`
	Communities     []string     `json:"communities"`
	MED             uint32       `json:"med"`
	LocalPref       uint32       `json:"local_pref"`
	Status          PrefixStatus `json:"status"`
	AnnouncedAt     time.Time    `json:"announced_at"`
	WithdrawnAt     *time.Time   `json:"withdrawn_at,omitempty"`
	TargetPoPs      []string     `json:"target_pops"`
	ActivePeers     []uint32     `json:"active_peers"`
	WithdrawCount   int          `json:"withdraw_count"`
	LastDurationUs  int64        `json:"last_duration_us"`
	DiversionTarget string       `json:"diversion_target,omitempty"`
}

// WithdrawalResult captures microsecond-scale metrics of a prefix withdrawal event.
type WithdrawalResult struct {
	CIDR           string    `json:"cidr"`
	Reason         string    `json:"reason"`
	PeersNotified  int       `json:"peers_notified"`
	DurationUs     int64     `json:"duration_us"`
	WithinSLA      bool      `json:"within_sla"` // DurationUs <= 5000 (5ms SLA)
	ExecutedAt     time.Time `json:"executed_at"`
	DivertedToPoP  string    `json:"diverted_to_pop,omitempty"`
	AlternativeASN uint32    `json:"alternative_asn,omitempty"`
}

// DiversionResult details a traffic diversion initiated by a PoP failure or partition.
type DiversionResult struct {
	FailedPoP        string              `json:"failed_pop"`
	AlternativePoP   string              `json:"alternative_pop"`
	AffectedPrefixes []string            `json:"affected_prefixes"`
	WithdrawalEvents []*WithdrawalResult `json:"withdrawal_events"`
	TotalDurationUs  int64               `json:"total_duration_us"`
	CompletedAt      time.Time           `json:"completed_at"`
}

// TransitPricingRule defines policy-based dynamic community tagging and local preference.
type TransitPricingRule struct {
	RuleID          string   `json:"rule_id"`
	TenantTier      string   `json:"tenant_tier"`      // "enterprise", "pro", "free", "*"
	ServicePattern  string   `json:"service_pattern"`  // "workers", "r2", "kv", "*"
	MaxCostPerMb    float64  `json:"max_cost_per_mb"`
	PreferredASN    uint32   `json:"preferred_asn"`
	AssignedTags    []string `json:"assigned_tags"`
	LocalPreference uint32   `json:"local_preference"`
}

// BGPStats contains operational telemetry and counters for the broadcaster.
type BGPStats struct {
	ActivePeersCount    int     `json:"active_peers_count"`
	AnnouncedPrefixes   int     `json:"announced_prefixes"`
	WithdrawnPrefixes   int     `json:"withdrawn_prefixes"`
	TotalAnnouncements  int64   `json:"total_announcements"`
	TotalWithdrawals    int64   `json:"total_withdrawals"`
	TotalKeepalivesSent int64   `json:"total_keepalives_sent"`
	TotalPacketsSent    int64   `json:"total_packets_sent"`
	TotalPacketsRecv    int64   `json:"total_packets_recv"`
	AvgWithdrawalUs     float64 `json:"avg_withdrawal_us"`
	MinWithdrawalUs     int64   `json:"min_withdrawal_us"`
	MaxWithdrawalUs     int64   `json:"max_withdrawal_us"`
	SLAViolationCount   int64   `json:"sla_violation_count"` // Withdrawals > 5000us
	LastWithdrawalUs    int64   `json:"last_withdrawal_us"`
}

// Common error definitions.
var (
	ErrInvalidPacketLen = errors.New("bgp: invalid packet length")
	ErrInvalidMarker    = errors.New("bgp: missing or invalid 16-byte marker")
	ErrPeerNotFound     = errors.New("bgp: peer session not found")
	ErrPrefixNotFound   = errors.New("bgp: anycast prefix not found")
	ErrInvalidCIDR      = errors.New("bgp: invalid cidr address")
	ErrSessionNotReady  = errors.New("bgp: peer session not in established state")
)
