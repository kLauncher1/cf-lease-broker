package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_PushDrainAndOverflow(t *testing.T) {
	rb := NewRingBuffer(4)
	if rb.Capacity() != 4 {
		t.Fatalf("expected capacity 4, got %d", rb.Capacity())
	}

	for i := 1; i <= 6; i++ {
		rb.Push(SocketMetrics{
			SocketID:   fmt.Sprintf("sock-%d", i),
			EndpointID: "ep-1",
			CWND:       uint32(i * 10),
		})
	}

	if rb.Len() != 4 {
		t.Fatalf("expected len 4, got %d", rb.Len())
	}
	if rb.DroppedCount() != 2 {
		t.Fatalf("expected dropped count 2, got %d", rb.DroppedCount())
	}

	drained := rb.DrainAll()
	if len(drained) != 4 {
		t.Fatalf("expected 4 drained items, got %d", len(drained))
	}
	// Items 1 and 2 were dropped; items 3, 4, 5, 6 should be present
	if drained[0].SocketID != "sock-3" || drained[3].SocketID != "sock-6" {
		t.Fatalf("unexpected drained order: %+v", drained)
	}

	rb.Reset()
	if rb.Len() != 0 || rb.DroppedCount() != 0 {
		t.Fatalf("expected empty buffer after reset, got len=%d dropped=%d", rb.Len(), rb.DroppedCount())
	}
}

func TestCollector_EWMAAndSocketAggregation(t *testing.T) {
	collector := NewTelemetryCollector()

	m1 := SocketMetrics{
		SocketID:      "sock-1",
		EndpointID:    "ep-iad",
		CWND:          32,
		RcvWnd:        65535,
		RTTUs:         10000,
		RetransRate:   0.0,
		PacketsOut:    100,
		BytesInFlight: 1460,
		Timestamp:     time.Now().Add(-10 * time.Minute),
	}
	collector.Ingest(m1)

	s, found := collector.GetSocket("sock-1")
	if !found || s.CWND != 32 {
		t.Fatalf("expected socket found with CWND 32, got %+v", s)
	}

	// Ingest second sample with higher RTT
	m2 := SocketMetrics{
		SocketID:    "sock-1",
		EndpointID:  "ep-iad",
		CWND:        32,
		RcvWnd:      32768,
		RTTUs:       20000,
		RetransRate: 5.0,
		Timestamp:   time.Now(),
	}
	collector.Ingest(m2)

	summary, found := collector.GetEndpointSummary("ep-iad")
	if !found {
		t.Fatalf("expected endpoint summary for ep-iad")
	}
	// EWMA: 0.2 * 20000 + 0.8 * 10000 = 12000
	if summary.AverageRTTUs != 12000 {
		t.Fatalf("expected EWMA RTT 12000, got %f", summary.AverageRTTUs)
	}
	if summary.MinRcvWnd != 32768 {
		t.Fatalf("expected min RcvWnd 32768, got %d", summary.MinRcvWnd)
	}

	// Test stale socket pruning
	pruned := collector.PruneStaleSockets(5 * time.Minute)
	// sock-1 timestamp was updated by m2 to now, so it shouldn't be pruned
	if pruned != 0 {
		t.Fatalf("expected 0 pruned, got %d", pruned)
	}
}

func TestDetector_WindowCollapseAnomaly(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	detector := NewAnomalyDetector(cfg)

	// Healthy socket
	healthy := SocketMetrics{
		SocketID:      "sock-1",
		EndpointID:    "ep-1",
		CWND:          32,
		RcvWnd:        65535,
		RTTUs:         15000,
		BytesInFlight: 1460,
	}
	reports := detector.Evaluate(healthy, EndpointSummary{AverageRTTUs: 15000})
	if len(reports) != 0 {
		t.Fatalf("expected 0 anomalies for healthy socket, got %d", len(reports))
	}

	// Collapsed window (0 bytes)
	collapsed := SocketMetrics{
		SocketID:      "sock-1",
		EndpointID:    "ep-1",
		CWND:          1,
		RcvWnd:        0,
		RTTUs:         25000,
		BytesInFlight: 8192,
	}
	reports = detector.Evaluate(collapsed, EndpointSummary{AverageRTTUs: 15000})
	if len(reports) == 0 {
		t.Fatalf("expected window collapse anomaly")
	}
	found := false
	for _, rep := range reports {
		if rep.Type == AnomalyWindowCollapse && rep.Severity == SeverityCritical {
			found = true
			if rep.Score < 0.85 {
				t.Fatalf("expected high severity score, got %f", rep.Score)
			}
		}
	}
	if !found {
		t.Fatalf("window collapse anomaly not found in reports: %+v", reports)
	}
}

func TestDetector_RetransSpikeAndPredictivePreTimeout(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	detector := NewAnomalyDetector(cfg)

	// Retransmission spike
	spikeMetrics := SocketMetrics{
		SocketID:    "sock-2",
		EndpointID:  "ep-2",
		CWND:        16,
		RcvWnd:      65535,
		RTTUs:       30000,
		RetransRate: 18.5, // 18.5% packet loss
	}
	reports := detector.Evaluate(spikeMetrics, EndpointSummary{AverageRTTUs: 30000})
	if len(reports) == 0 {
		t.Fatalf("expected retransmission spike anomaly")
	}
	if reports[0].Type != AnomalyRetransSpike || reports[0].Severity != SeverityCritical {
		t.Fatalf("unexpected report: %+v", reports[0])
	}

	// Predictive pre-timeout: RTT exceeds 500,000 µs (500ms) long before 5s HTTP timeout
	preTimeoutMetrics := SocketMetrics{
		SocketID:      "sock-3",
		EndpointID:    "ep-3",
		CWND:          8,
		RcvWnd:        32768,
		RTTUs:         550000, // 550ms
		BytesInFlight: 8192,
	}
	reports = detector.Evaluate(preTimeoutMetrics, EndpointSummary{AverageRTTUs: 50000})
	foundPTO := false
	for _, rep := range reports {
		if rep.Type == AnomalyPreTimeout {
			foundPTO = true
			if rep.Severity != SeverityCritical || rep.Score < 0.90 {
				t.Fatalf("expected critical predictive pre-timeout, got %+v", rep)
			}
		}
	}
	if !foundPTO {
		t.Fatalf("predictive pre-timeout not detected")
	}
}

func TestMitigation_Sub10MillisecondCircuitBreakAndReroute(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	mit := NewMitigationEngine(cfg)
	mit.SetStandbyRoute("worker-iad", "worker-lhr-standby")

	anomaly := AnomalyReport{
		ID:         "anom-test-1",
		EndpointID: "worker-iad",
		Type:       AnomalyWindowCollapse,
		Severity:   SeverityCritical,
		Score:      0.95,
		DetectedAt: time.Now(),
	}

	action, triggered := mit.HandleAnomaly(anomaly)
	if !triggered || action == nil {
		t.Fatalf("expected autonomous mitigation to trigger")
	}

	// Crucial Requirement: Trigger latency must be strictly under 10 milliseconds (10,000 µs)
	if action.TriggerLatencyUs > 10000 {
		t.Fatalf("SLA violation: mitigation took %d µs (must be < 10,000 µs)", action.TriggerLatencyUs)
	}
	if action.NewState != CircuitOpen {
		t.Fatalf("expected CircuitOpen state, got %s", action.NewState)
	}
	if action.RerouteTarget != "worker-lhr-standby" {
		t.Fatalf("expected reroute target worker-lhr-standby, got %s", action.RerouteTarget)
	}

	// Verify route resolution routes to standby
	target, state := mit.ResolveRoute("worker-iad")
	if state != CircuitOpen || target != "worker-lhr-standby" {
		t.Fatalf("expected reroute to worker-lhr-standby with CircuitOpen, got %s, %s", target, state)
	}

	// Verify statistics
	total, _, maxLat, sub10Rate, openCount, rerouteCount := mit.GetStats()
	if total != 1 || maxLat > 10000 || sub10Rate != 100.0 || openCount != 1 || rerouteCount != 1 {
		t.Fatalf("unexpected stats: total=%d, maxLat=%d, sub10Rate=%f, open=%d, reroutes=%d",
			total, maxLat, sub10Rate, openCount, rerouteCount)
	}
}

func TestMitigation_SelfHealingRecovery(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	cfg.CircuitCooldownDuration = 10 * time.Millisecond // Fast cooldown for test
	mit := NewMitigationEngine(cfg)

	anomaly := AnomalyReport{
		ID:         "anom-heal",
		EndpointID: "worker-nrt",
		Severity:   SeverityCritical,
		Score:      0.90,
		DetectedAt: time.Now(),
	}
	_, _ = mit.HandleAnomaly(anomaly)

	if mit.GetCircuitState("worker-nrt") != CircuitOpen {
		t.Fatalf("expected CircuitOpen")
	}

	// Wait for cooldown to expire
	time.Sleep(15 * time.Millisecond)

	// State should transition to HalfOpen
	if mit.GetCircuitState("worker-nrt") != CircuitHalfOpen {
		t.Fatalf("expected CircuitHalfOpen after cooldown, got %s", mit.GetCircuitState("worker-nrt"))
	}

	// First healthy probe in HalfOpen
	_, healed := mit.HandleHealthySample("worker-nrt")
	if healed {
		t.Fatalf("should require 2 consecutive healthy probes to recover")
	}

	// Second healthy probe in HalfOpen -> should self-heal!
	act, healed := mit.HandleHealthySample("worker-nrt")
	if !healed || act == nil || act.NewState != CircuitClosed {
		t.Fatalf("expected self-healing recovery to CircuitClosed")
	}

	target, state := mit.ResolveRoute("worker-nrt")
	if state != CircuitClosed || target != "worker-nrt" {
		t.Fatalf("expected route resolved to worker-nrt in CircuitClosed state")
	}
}

func TestTelemetryEngine_EndToEndPipeline(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	cfg.CircuitCooldownDuration = 50 * time.Millisecond
	engine := NewTelemetryEngine(cfg)
	engine.Start()
	defer engine.Stop()

	engine.RegisterTarget("worker-test", "10.0.1.50:443", "tcp")
	engine.SetStandbyRoute("worker-test", "worker-backup")

	// Baseline route check
	target, state := engine.ResolveRoute("worker-test")
	if state != CircuitClosed || target != "worker-test" {
		t.Fatalf("expected initial healthy route worker-test")
	}

	// Simulate degradation: Window collapse
	anomalies, action := engine.SimulateDegradation("worker-test", AnomalyWindowCollapse)
	if len(anomalies) == 0 {
		t.Fatalf("expected anomalies detected during simulation")
	}
	if action == nil || action.NewState != CircuitOpen {
		t.Fatalf("expected mitigation action tripping circuit")
	}
	if action.TriggerLatencyUs > 10000 {
		t.Fatalf("trigger latency exceeded 10ms: %d µs", action.TriggerLatencyUs)
	}

	// Verify reroute
	reroutedTarget, reroutedState := engine.ResolveRoute("worker-test")
	if reroutedState != CircuitOpen || reroutedTarget != "worker-backup" {
		t.Fatalf("expected reroute to worker-backup, got %s, %s", reroutedTarget, reroutedState)
	}

	stats := engine.GetStats()
	if stats.TotalAnomaliesDetected == 0 || stats.TotalMitigationsTriggered == 0 {
		t.Fatalf("stats not updated: %+v", stats)
	}
	if stats.Sub10msEnforcementRate != 100.0 {
		t.Fatalf("expected 100%% sub-10ms enforcement, got %f", stats.Sub10msEnforcementRate)
	}
}

func TestTelemetryHandler_RESTEndpoints(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	engine := NewTelemetryEngine(cfg)
	engine.Start()
	defer engine.Stop()

	handler := NewTelemetryHandler(engine)

	// 1. GET /api/telemetry/status
	req := httptest.NewRequest(http.MethodGet, "/api/telemetry/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var statusResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil || statusResp["status"] != "active" {
		t.Fatalf("invalid status response: %s", rec.Body.String())
	}

	// 2. POST /api/telemetry/simulate
	simBody := `{"endpoint_id":"api-worker","type":"tcp_window_collapse"}`
	req = httptest.NewRequest(http.MethodPost, "/api/telemetry/simulate", strings.NewReader(simBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on simulate, got %d", rec.Code)
	}

	// 3. GET /api/telemetry/anomalies
	req = httptest.NewRequest(http.MethodGet, "/api/telemetry/anomalies", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on anomalies, got %d", rec.Code)
	}

	// 4. GET /api/telemetry/mitigations
	req = httptest.NewRequest(http.MethodGet, "/api/telemetry/mitigations", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on mitigations, got %d", rec.Code)
	}

	// 5. POST /api/telemetry/reroute
	rerouteBody := `{"primary_endpoint":"api-worker","standby_endpoint":"api-worker-standby"}`
	req = httptest.NewRequest(http.MethodPost, "/api/telemetry/reroute", strings.NewReader(rerouteBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on reroute, got %d", rec.Code)
	}

	// 6. 64KB Request Body Bounding
	hugePayload := bytes.Repeat([]byte("a"), 70*1024)
	req = httptest.NewRequest(http.MethodPost, "/api/telemetry/ingest", bytes.NewReader(hugePayload))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on oversized payload, got %d", rec.Code)
	}
}

func TestTelemetryEngine_Concurrency40Goroutines(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	engine := NewTelemetryEngine(cfg)
	engine.Start()
	defer engine.Stop()

	var wg sync.WaitGroup
	workers := 40
	iterations := 25

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ep := fmt.Sprintf("worker-%d", workerID%4)

			for i := 0; i < iterations; i++ {
				metrics := SocketMetrics{
					SocketID:      fmt.Sprintf("sock-%d-%d", workerID, i),
					EndpointID:    ep,
					CWND:          uint32(16 + (i % 16)),
					RcvWnd:        uint32(32768 + (i * 100)),
					RTTUs:         uint32(10000 + (workerID * 500)),
					RetransRate:   float64(i % 3),
					BytesInFlight: 1460,
					PacketsOut:    50,
					Timestamp:     time.Now(),
				}
				engine.IngestProbe(metrics)
				_ = engine.GetCircuitState(ep)
				_, _ = engine.ResolveRoute(ep)
				_ = engine.GetStats()
			}
		}(w)
	}

	wg.Wait()
}
