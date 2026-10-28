package telemetry

import (
	"time"
)

// AnomalyType identifies specific socket-level network degradation patterns.
type AnomalyType string

const (
	AnomalyWindowCollapse AnomalyType = "tcp_window_collapse"
	AnomalyRetransSpike   AnomalyType = "retransmission_spike"
	AnomalyRTTJitter      AnomalyType = "rtt_jitter_escalation"
	AnomalyBufferbloat    AnomalyType = "bufferbloat_saturation"
	AnomalyPreTimeout     AnomalyType = "predictive_pre_timeout"
)

// AnomalySeverity categorizes the impact of detected socket degradation.
type AnomalySeverity string

const (
	SeverityInfo     AnomalySeverity = "info"
	SeverityWarning  AnomalySeverity = "warning"
	SeverityCritical AnomalySeverity = "critical"
)

// CircuitState represents the state machine of the autonomous circuit breaker.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"    // Normal operation; traffic allowed
	CircuitOpen     CircuitState = "open"      // Degraded; traffic rerouted or blocked
	CircuitTripped  CircuitState = CircuitOpen // Compatibility alias for tripped/open state
	CircuitHalfOpen CircuitState = "half_open" // Canary probing recovery
)

// ProbeDriver defines the cross-platform interface for socket telemetry sampling.
type ProbeDriver interface {
	IsAvailable() bool
	GetMode() string
	AttachSocket(sockID, endpointID, remoteAddr, localAddr string) error
	DetachSocket(sockID string) error
	SampleSocket(sockID string) (*SocketMetrics, error)
	InjectProbeEvent(metrics SocketMetrics)
	Close() error
}

// SocketMetrics captures kernel-level TCP socket metrics obtained via eBPF probes.
type SocketMetrics struct {
	SocketID      string    `json:"socket_id"`
	RemoteAddr    string    `json:"remote_addr"`
	LocalAddr     string    `json:"local_addr"`
	EndpointID    string    `json:"endpoint_id"`
	State         string    `json:"state"`
	CWND          uint32    `json:"cwnd"`           // Congestion window (packets)
	SSTHRESH      uint32    `json:"ssthresh"`       // Slow start threshold
	RcvWnd        uint32    `json:"rcv_wnd"`        // Advertised receive window (bytes)
	RTTUs         uint32    `json:"rtt_us"`         // Round trip time in microseconds
	RTTVarUs      uint32    `json:"rtt_var_us"`     // RTT variance / jitter in microseconds
	BytesInFlight uint32    `json:"bytes_in_flight"`// Unacknowledged bytes
	TotalRetrans  uint32    `json:"total_retrans"`  // Cumulative retransmitted packets
	RetransRate   float64   `json:"retrans_rate"`   // Retransmission rate percentage (0.0 - 100.0)
	PacketsOut    uint32    `json:"packets_out"`    // Packets sent
	Timestamp     time.Time `json:"timestamp"`
}

// AnomalyReport encapsulates a detected degradation incident.
type AnomalyReport struct {
	ID              string          `json:"id"`
	EndpointID      string          `json:"endpoint_id"`
	SocketID        string          `json:"socket_id"`
	Type            AnomalyType     `json:"type"`
	Severity        AnomalySeverity `json:"severity"`
	Score           float64         `json:"score"` // 0.0 (healthy) - 1.0 (imminent failure)
	Message         string          `json:"message"`
	MetricsSnapshot SocketMetrics   `json:"metrics_snapshot"`
	DetectedAt      time.Time       `json:"detected_at"`
	Mitigated       bool            `json:"mitigated"`
}

// MitigationAction logs autonomous self-healing mitigations and timing.
type MitigationAction struct {
	ActionID          string       `json:"action_id"`
	EndpointID        string       `json:"endpoint_id"`
	TriggerAnomalyID  string       `json:"trigger_anomaly_id"`
	ActionType        string       `json:"action_type"` // "circuit_break", "reroute", "recover"
	PreviousState     CircuitState `json:"previous_state"`
	NewState          CircuitState `json:"new_state"`
	RerouteTarget     string       `json:"reroute_target"`
	TriggerLatencyUs  int64        `json:"trigger_latency_us"` // Latency from anomaly to mitigation (target < 10,000 µs)
	ExecutedAt        time.Time    `json:"executed_at"`
}

// EndpointSummary aggregates recent socket telemetry for an endpoint.
type EndpointSummary struct {
	EndpointID       string       `json:"endpoint_id"`
	ActiveSockets    int          `json:"active_sockets"`
	AverageRTTUs     float64      `json:"avg_rtt_us"`
	AverageRetrans   float64      `json:"avg_retrans_rate"`
	MinRcvWnd        uint32       `json:"min_rcv_wnd"`
	AnomalyScore     float64      `json:"anomaly_score"`
	CircuitState     CircuitState `json:"circuit_state"`
	RerouteTarget    string       `json:"reroute_target"`
	LastMitigationAt time.Time    `json:"last_mitigation_at"`
}

// TelemetryConfig configures thresholds for socket anomaly detection and mitigation.
type TelemetryConfig struct {
	MinRcvWndBytes              uint32        `json:"min_rcv_wnd_bytes"`               // Collapse threshold (e.g. 4096 bytes)
	MinCWNDPackets              uint32        `json:"min_cwnd_packets"`               // Collapse threshold (e.g. 4 packets)
	MaxRetransRatePercent       float64       `json:"max_retrans_rate_percent"`       // Retrans spike threshold (e.g. 5.0%)
	MaxRTTUs                    uint32        `json:"max_rtt_us"`                     // RTT escalation threshold (e.g. 150000 µs)
	PreTimeoutThresholdUs       uint32        `json:"pre_timeout_threshold_us"`       // Predictive timeout (e.g. 500000 µs / 500ms)
	CircuitBreakScoreThreshold  float64       `json:"circuit_break_score_threshold"`  // Composite score threshold (e.g. 0.65)
	ConsecutiveAnomalyThreshold int           `json:"consecutive_anomaly_threshold"`  // Consecutive anomalies before trip (e.g. 2)
	CircuitCooldownDuration     time.Duration `json:"circuit_cooldown_duration"`      // Time before HalfOpen canary (e.g. 2s)
	RingBufferSize              int           `json:"ring_buffer_size"`               // Probe event ring buffer size (e.g. 1024)
}

// DefaultTelemetryConfig returns production-ready anomaly thresholds.
func DefaultTelemetryConfig() TelemetryConfig {
	return TelemetryConfig{
		MinRcvWndBytes:              4096,
		MinCWNDPackets:              4,
		MaxRetransRatePercent:       5.0,
		MaxRTTUs:                    150000,
		PreTimeoutThresholdUs:       500000,
		CircuitBreakScoreThreshold:  0.65,
		ConsecutiveAnomalyThreshold: 2,
		CircuitCooldownDuration:     2 * time.Second,
		RingBufferSize:              1024,
	}
}

// TelemetryStats provides operational telemetry and SLA compliance metrics.
type TelemetryStats struct {
	TotalProbesIngested         int64   `json:"total_probes_ingested"`
	ActiveSocketsCount          int     `json:"active_sockets_count"`
	TotalAnomaliesDetected      int64   `json:"total_anomalies_detected"`
	TotalMitigationsTriggered   int64   `json:"total_mitigations_triggered"`
	AverageMitigationLatencyUs  float64 `json:"average_mitigation_latency_us"`
	MaxMitigationLatencyUs      int64   `json:"max_mitigation_latency_us"`
	Sub10msEnforcementRate      float64 `json:"sub_10ms_enforcement_rate"` // Percentage of mitigations under 10ms
	ActiveCircuitsOpen          int     `json:"active_circuits_open"`
	ActiveReroutesCount         int     `json:"active_reroutes_count"`
}
