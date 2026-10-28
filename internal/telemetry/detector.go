package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"
)

// AnomalyListener receives real-time notifications when an anomaly is detected.
type AnomalyListener func(report AnomalyReport)

// AnomalyDetector analyzes incoming socket metrics to flag degradation before HTTP timeouts occur.
type AnomalyDetector struct {
	mu                   sync.RWMutex
	config               TelemetryConfig
	recentAnomalies      []AnomalyReport
	maxHistory           int
	consecutiveAnomalies map[string]int // endpointID -> consecutive anomaly count
	listeners            []AnomalyListener
}

// NewAnomalyDetector constructs a real-time detector configured with threshold policies.
func NewAnomalyDetector(cfg TelemetryConfig) *AnomalyDetector {
	return &AnomalyDetector{
		config:               cfg,
		recentAnomalies:      make([]AnomalyReport, 0, 200),
		maxHistory:           200,
		consecutiveAnomalies: make(map[string]int),
		listeners:            make([]AnomalyListener, 0),
	}
}

// OnAnomaly registers a callback fired immediately upon detecting a socket degradation event.
func (d *AnomalyDetector) OnAnomaly(listener AnomalyListener) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listeners = append(d.listeners, listener)
}

// Evaluate analyzes socket metrics against configured thresholds and historical baseline.
func (d *AnomalyDetector) Evaluate(m SocketMetrics, baseline EndpointSummary) []AnomalyReport {
	d.mu.Lock()
	defer d.mu.Unlock()

	var reports []AnomalyReport
	now := time.Now()
	epID := m.EndpointID
	if epID == "" {
		epID = "default"
	}

	// 1. TCP Window Collapse Anomaly (Receiver buffer exhaustion or zero-window stall)
	if m.RcvWnd < d.config.MinRcvWndBytes || (m.CWND < d.config.MinCWNDPackets && m.BytesInFlight > 0) {
		score := 0.85
		if m.RcvWnd == 0 {
			score = 1.0
		}
		reports = append(reports, AnomalyReport{
			ID:              d.generateID("anom-wnd-"),
			EndpointID:      epID,
			SocketID:        m.SocketID,
			Type:            AnomalyWindowCollapse,
			Severity:        SeverityCritical,
			Score:           score,
			Message:         fmt.Sprintf("TCP window collapsed: RcvWnd=%d bytes, CWND=%d packets (min threshold=%d bytes)", m.RcvWnd, m.CWND, d.config.MinRcvWndBytes),
			MetricsSnapshot: m,
			DetectedAt:      now,
		})
	}

	// 2. Retransmission Spike Anomaly (Severe packet loss in flight)
	if m.RetransRate > d.config.MaxRetransRatePercent {
		sev := SeverityWarning
		score := math.Min(1.0, 0.5+(m.RetransRate/100.0)*2.0)
		if m.RetransRate >= 15.0 {
			sev = SeverityCritical
			score = math.Min(1.0, 0.85+(m.RetransRate/100.0))
		}
		reports = append(reports, AnomalyReport{
			ID:              d.generateID("anom-rtx-"),
			EndpointID:      epID,
			SocketID:        m.SocketID,
			Type:            AnomalyRetransSpike,
			Severity:        sev,
			Score:           score,
			Message:         fmt.Sprintf("Packet retransmission spike: %.2f%% (threshold=%.2f%%, total_retrans=%d)", m.RetransRate, d.config.MaxRetransRatePercent, m.TotalRetrans),
			MetricsSnapshot: m,
			DetectedAt:      now,
		})
	}

	// 3. RTT Jitter & Latency Escalation
	if baseline.AverageRTTUs > 0 && float64(m.RTTUs) > (baseline.AverageRTTUs*2.5) && m.RTTUs > 50000 {
		reports = append(reports, AnomalyReport{
			ID:              d.generateID("anom-rtt-"),
			EndpointID:      epID,
			SocketID:        m.SocketID,
			Type:            AnomalyRTTJitter,
			Severity:        SeverityWarning,
			Score:           0.65,
			Message:         fmt.Sprintf("RTT latency escalation: %d µs vs baseline %.1f µs (jitter=%d µs)", m.RTTUs, baseline.AverageRTTUs, m.RTTVarUs),
			MetricsSnapshot: m,
			DetectedAt:      now,
		})
	}

	// 4. Bufferbloat Saturation Anomaly (High in-flight bytes + elevated RTT without packet drop)
	if m.BytesInFlight >= 32768 && baseline.AverageRTTUs > 0 && float64(m.RTTUs) >= (baseline.AverageRTTUs*2.0) && m.RetransRate < 2.0 {
		reports = append(reports, AnomalyReport{
			ID:              d.generateID("anom-bloat-"),
			EndpointID:      epID,
			SocketID:        m.SocketID,
			Type:            AnomalyBufferbloat,
			Severity:        SeverityWarning,
			Score:           0.75,
			Message:         fmt.Sprintf("Bufferbloat queue saturation: BytesInFlight=%d bytes, RTT=%d µs vs baseline %.1f µs", m.BytesInFlight, m.RTTUs, baseline.AverageRTTUs),
			MetricsSnapshot: m,
			DetectedAt:      now,
		})
	}

	// 5. Predictive Pre-Timeout (Detects imminent HTTP timeout before 5s-15s deadline)
	if m.RTTUs >= d.config.PreTimeoutThresholdUs {
		reports = append(reports, AnomalyReport{
			ID:              d.generateID("anom-pto-"),
			EndpointID:      epID,
			SocketID:        m.SocketID,
			Type:            AnomalyPreTimeout,
			Severity:        SeverityCritical,
			Score:           0.95,
			Message:         fmt.Sprintf("Predictive pre-timeout detected: RTT %d µs exceeds pre-timeout threshold %d µs prior to HTTP gateway timeout", m.RTTUs, d.config.PreTimeoutThresholdUs),
			MetricsSnapshot: m,
			DetectedAt:      now,
		})
	}

	// Track consecutive anomalies
	if len(reports) > 0 {
		d.consecutiveAnomalies[epID] += len(reports)
	} else {
		// Decay consecutive anomalies when healthy probe arrives
		if d.consecutiveAnomalies[epID] > 0 {
			d.consecutiveAnomalies[epID]--
		}
	}

	// Append to recent history and notify listeners
	for i := range reports {
		rep := reports[i]
		if len(d.recentAnomalies) >= d.maxHistory {
			d.recentAnomalies = d.recentAnomalies[1:]
		}
		d.recentAnomalies = append(d.recentAnomalies, rep)

		for _, l := range d.listeners {
			go l(rep)
		}
	}

	return reports
}

// ComputeCompositeScore calculates an aggregate health score (0.0=perfect, 1.0=failing) for an endpoint.
func (d *AnomalyDetector) ComputeCompositeScore(m SocketMetrics, baseline EndpointSummary) float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var wndScore, rtxScore, rttScore float64

	// Window collapse factor
	if m.RcvWnd < d.config.MinRcvWndBytes {
		if m.RcvWnd == 0 {
			wndScore = 1.0
		} else {
			wndScore = 1.0 - (float64(m.RcvWnd) / float64(d.config.MinRcvWndBytes))
		}
	}

	// Retransmission factor
	if m.RetransRate > 0 {
		rtxScore = math.Min(1.0, m.RetransRate/20.0)
	}

	// RTT factor
	if d.config.PreTimeoutThresholdUs > 0 {
		rttScore = math.Min(1.0, float64(m.RTTUs)/float64(d.config.PreTimeoutThresholdUs))
	}

	composite := (0.35 * wndScore) + (0.35 * rtxScore) + (0.30 * rttScore)
	if composite > 1.0 {
		composite = 1.0
	}
	return composite
}

// GetConsecutiveAnomalies returns the number of consecutive anomalies detected for an endpoint.
func (d *AnomalyDetector) GetConsecutiveAnomalies(endpointID string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.consecutiveAnomalies[endpointID]
}

// ListRecentAnomalies returns a copy of recent anomaly reports up to limit.
func (d *AnomalyDetector) ListRecentAnomalies(limit int) []AnomalyReport {
	d.mu.RLock()
	defer d.mu.RUnlock()

	total := len(d.recentAnomalies)
	if total == 0 {
		return nil
	}
	if limit <= 0 || limit > total {
		limit = total
	}

	start := total - limit
	out := make([]AnomalyReport, limit)
	copy(out, d.recentAnomalies[start:])
	return out
}

// Reset clears historical anomalies and consecutive counters.
func (d *AnomalyDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recentAnomalies = make([]AnomalyReport, 0, d.maxHistory)
	d.consecutiveAnomalies = make(map[string]int)
}

func (d *AnomalyDetector) generateID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}
