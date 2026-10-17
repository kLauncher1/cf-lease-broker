package steering

import (
	"time"
)

// SteeringMode indicates the packet dispatch mechanism used by the edge broker.
type SteeringMode string

const (
	// SteeringModeEBPFSockmap dispatches sockets directly in the kernel via eBPF sockmap/sk_msg.
	SteeringModeEBPFSockmap SteeringMode = "ebpf-sockmap"
	// SteeringModeEBPFXDP dispatches packets at line-rate in the network driver before Linux TCP/IP stack.
	SteeringModeEBPFXDP SteeringMode = "ebpf-xdp"
	// SteeringModeUserspace dispatches packets in user-space via high-speed zero-copy steering pipeline.
	SteeringModeUserspace SteeringMode = "userspace-bypass"
)

// QUICHeaderType represents the QUIC packet header type per RFC 9000.
type QUICHeaderType string

const (
	QUICHeaderInitial   QUICHeaderType = "INITIAL"
	QUICHeader0RTT      QUICHeaderType = "0-RTT"
	QUICHeaderHandshake QUICHeaderType = "HANDSHAKE"
	QUICHeaderRetry     QUICHeaderType = "RETRY"
	QUICHeaderShort1RTT QUICHeaderType = "1-RTT"
	QUICHeaderUnknown   QUICHeaderType = "UNKNOWN"
)

// QUICPacketHeader holds parsed metadata extracted from a UDP packet carrying QUIC traffic.
type QUICPacketHeader struct {
	IsLongHeader  bool           `json:"is_long_header"`
	Type          QUICHeaderType `json:"type"`
	Version       uint32         `json:"version,omitempty"`
	DCID          string         `json:"dcid"` // Hex-encoded Destination Connection ID
	SCID          string         `json:"scid,omitempty"` // Hex-encoded Source Connection ID
	KeyPhase      bool           `json:"key_phase,omitempty"`
	SpinBit       bool           `json:"spin_bit,omitempty"`
	PacketNumber  uint64         `json:"packet_number,omitempty"`
	TokenLength   int            `json:"token_length,omitempty"`
	PayloadLength int            `json:"payload_length"`
}

// WorkerEndpoint models an upstream Cloudflare Worker or Edge microservice destination.
type WorkerEndpoint struct {
	ID             string    `json:"id"`
	Colo           string    `json:"colo"`
	Address        string    `json:"address"`
	Weight         int       `json:"weight"`
	ActiveConns    int64     `json:"active_conns"`
	Healthy        bool      `json:"healthy"`
	PacketsSteered uint64    `json:"packets_steered"`
	BytesSteered   uint64    `json:"bytes_steered"`
	AvgLatencyUs   float64   `json:"avg_latency_us"`
	LastSeen       time.Time `json:"last_seen"`
}

// SteeringRule maps specific QUIC Connection ID prefixes or service tags to dedicated worker endpoints.
type SteeringRule struct {
	ID               string `json:"id"`
	CIDPrefix        string `json:"cid_prefix"` // Hex prefix match, e.g. "cf01" or "*" for default
	TargetEndpointID string `json:"target_endpoint_id"`
	Priority         int    `json:"priority"` // Higher number evaluated first
	Description      string `json:"description"`
}

// SteeringDecision records the outcome and microsecond telemetry of a packet dispatch.
type SteeringDecision struct {
	TargetEndpoint      *WorkerEndpoint   `json:"target_endpoint"`
	Mode                SteeringMode      `json:"mode"`
	ParsedHeader        *QUICPacketHeader `json:"parsed_header,omitempty"`
	DispatchedViaKernel bool              `json:"dispatched_via_kernel"`
	DispatchLatencyNs   int64             `json:"dispatch_latency_ns"`
	DispatchLatencyUs   float64           `json:"dispatch_latency_us"`
	MatchedRuleID       string            `json:"matched_rule_id,omitempty"`
}

// SteeringStats tracks aggregate throughput and microsecond dispatch telemetry.
type SteeringStats struct {
	TotalPackets          uint64       `json:"total_packets"`
	TotalBytes            uint64       `json:"total_bytes"`
	KernelBypassedPackets uint64       `json:"kernel_bypassed_packets"`
	UserspacePackets      uint64       `json:"userspace_packets"`
	Sub10MicrosecPackets  uint64       `json:"sub_10_microsec_packets"`
	AvgDispatchLatencyUs  float64      `json:"avg_dispatch_latency_us"`
	Sub10MicrosecRatio    float64      `json:"sub_10_microsec_ratio"`
	ActiveEndpoints       int          `json:"active_endpoints"`
	ActiveRules           int          `json:"active_rules"`
	ActiveMode            SteeringMode `json:"active_mode"`
	EBPFAvailable         bool         `json:"ebpf_available"`
}

// SteeringConfig configures the HTTP/3 eBPF packet steering engine.
type SteeringConfig struct {
	Mode                 SteeringMode  `json:"mode"`
	EnableKernelBypass   bool          `json:"enable_kernel_bypass"`
	EnableQUICInspection bool          `json:"enable_quic_inspection"`
	DefaultWorkerPort    int           `json:"default_worker_port"`
	MaxEndpoints         int           `json:"max_endpoints"`
	DefaultDCIDLen       int           `json:"default_dcid_len"` // Default DCID length for short headers (e.g. 8 bytes)
	StatsWindowInterval  time.Duration `json:"stats_window_interval"`
}

// DefaultSteeringConfig returns production-grade defaults for zero-overhead packet steering.
func DefaultSteeringConfig() SteeringConfig {
	return SteeringConfig{
		Mode:                 SteeringModeEBPFSockmap,
		EnableKernelBypass:   true,
		EnableQUICInspection: true,
		DefaultWorkerPort:    443,
		MaxEndpoints:         64,
		DefaultDCIDLen:       8,
		StatsWindowInterval:  time.Second,
	}
}
