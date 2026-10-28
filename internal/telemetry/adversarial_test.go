package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAdversarial_DoubleCountingProbeIngest checks if async ingestion double-counts probes.
func TestAdversarial_DoubleCountingProbeIngest(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	engine := NewTelemetryEngine(cfg)
	engine.Start()
	defer engine.Stop()

	// Ingest exactly 5 probes via IngestProbe
	for i := 0; i < 5; i++ {
		engine.IngestProbe(SocketMetrics{
			SocketID:   "sock-dc-1",
			EndpointID: "ep-dc",
			CWND:       32,
			RcvWnd:     65535,
			RTTUs:      10000,
			Timestamp:  time.Now(),
		})
	}

	// Wait for ring buffer consumer to drain
	time.Sleep(20 * time.Millisecond)

	stats := engine.GetStats()
	if stats.TotalProbesIngested != 5 {
		t.Errorf("FAIL: Expected 5 TotalProbesIngested, got %d (double counting detected)", stats.TotalProbesIngested)
	}
}

// TestAdversarial_CanaryRecoveryFlaw verifies that CanarySuccess is properly reset when tripping back to Open.
func TestAdversarial_CanaryRecoveryFlaw(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	cfg.CircuitCooldownDuration = 5 * time.Millisecond
	mit := NewMitigationEngine(cfg)

	// 1. Initial trip to Open
	anomaly := AnomalyReport{
		ID:         "anom-1",
		EndpointID: "ep-canary",
		Severity:   SeverityCritical,
		Score:      0.95,
		DetectedAt: time.Now(),
	}
	_, _ = mit.HandleAnomaly(anomaly)

	// Wait for cooldown to transition to HalfOpen
	time.Sleep(10 * time.Millisecond)
	if mit.GetCircuitState("ep-canary") != CircuitHalfOpen {
		t.Fatalf("expected CircuitHalfOpen, got %s", mit.GetCircuitState("ep-canary"))
	}

	// 2. First healthy probe in HalfOpen -> CanarySuccess should be 1, NOT recovered yet
	_, healed := mit.HandleHealthySample("ep-canary")
	if healed {
		t.Fatalf("should not heal after only 1 healthy sample")
	}

	// 3. Degradation occurs again in HalfOpen -> Should trip back to Open and RESET CanarySuccess to 0!
	anomaly2 := AnomalyReport{
		ID:         "anom-2",
		EndpointID: "ep-canary",
		Severity:   SeverityCritical,
		Score:      0.95,
		DetectedAt: time.Now(),
	}
	_, tripped := mit.HandleAnomaly(anomaly2)
	if !tripped {
		t.Fatalf("expected circuit to trip back to Open from HalfOpen")
	}
	if mit.GetCircuitState("ep-canary") != CircuitOpen {
		t.Fatalf("expected CircuitOpen after second anomaly, got %s", mit.GetCircuitState("ep-canary"))
	}

	// Wait for cooldown again to enter HalfOpen
	time.Sleep(10 * time.Millisecond)
	if mit.GetCircuitState("ep-canary") != CircuitHalfOpen {
		t.Fatalf("expected CircuitHalfOpen, got %s", mit.GetCircuitState("ep-canary"))
	}

	// 4. Now receive ONLY ONE healthy sample in this new HalfOpen period!
	// It MUST NOT heal yet because CanarySuccess should have been reset to 0 upon tripping!
	_, healedPrematurely := mit.HandleHealthySample("ep-canary")
	if healedPrematurely {
		t.Errorf("FAIL: Circuit healed prematurely after only 1 healthy probe due to leaked CanarySuccess from prior HalfOpen cycle!")
	}
}

// TestAdversarial_ZeroTimestampLatencyUnderflow verifies mitigation latency calculation when DetectedAt is zero.
func TestAdversarial_ZeroTimestampLatencyUnderflow(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	mit := NewMitigationEngine(cfg)

	// Anomaly with zero DetectedAt
	anomaly := AnomalyReport{
		ID:         "anom-zero-time",
		EndpointID: "ep-zero",
		Severity:   SeverityCritical,
		Score:      0.90,
		// DetectedAt is left as time.Time{} (zero value)
	}

	act, triggered := mit.HandleAnomaly(anomaly)
	if !triggered || act == nil {
		t.Fatalf("expected mitigation triggered")
	}

	if act.TriggerLatencyUs > 10000 {
		t.Errorf("FAIL: TriggerLatencyUs overflowed to %d µs due to zero-timestamp DetectedAt", act.TriggerLatencyUs)
	}

	_, _, maxLat, sub10Rate, _, _ := mit.GetStats()
	if sub10Rate < 100.0 || maxLat > 10000 {
		t.Errorf("FAIL: Stats corrupted: maxLat=%d µs, sub10Rate=%.2f%%", maxLat, sub10Rate)
	}
}

// TestAdversarial_BufferbloatDetection verifies that bufferbloat is properly flagged as an anomaly.
func TestAdversarial_BufferbloatDetection(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	detector := NewAnomalyDetector(cfg)

	// Saturated buffer: large bytes in flight, elevated RTT, zero packet loss
	bloatMetrics := SocketMetrics{
		SocketID:      "sock-bb",
		EndpointID:    "ep-bb",
		CWND:          64,
		RcvWnd:        65535,
		RTTUs:         120000, // Elevated RTT
		BytesInFlight: 65536,  // High in-flight queue
		RetransRate:   0.0,    // No loss
	}
	baseline := EndpointSummary{
		AverageRTTUs: 20000, // Baseline was 20ms
	}

	reports := detector.Evaluate(bloatMetrics, baseline)
	foundBloat := false
	for _, r := range reports {
		if r.Type == AnomalyBufferbloat {
			foundBloat = true
			break
		}
	}
	if !foundBloat {
		t.Errorf("FAIL: Bufferbloat saturation anomaly was not detected for high in-flight bytes + elevated RTT")
	}
}

// TestAdversarial_FlappingBackoff verifies that rapid flapping triggers exponential cooldown backoff.
func TestAdversarial_FlappingBackoff(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	cfg.CircuitCooldownDuration = 20 * time.Millisecond
	mit := NewMitigationEngine(cfg)

	// Trip 1
	mit.HandleAnomaly(AnomalyReport{EndpointID: "ep-flap", Severity: SeverityCritical, Score: 0.95, DetectedAt: time.Now()})
	time.Sleep(25 * time.Millisecond)
	// Heal to Closed
	mit.HandleHealthySample("ep-flap")
	mit.HandleHealthySample("ep-flap")

	// Trip 2 immediately (flapping!)
	mit.HandleAnomaly(AnomalyReport{EndpointID: "ep-flap", Severity: SeverityCritical, Score: 0.95, DetectedAt: time.Now()})

	// After base cooldown (20ms), with backoff it should NOT transition to HalfOpen yet
	time.Sleep(25 * time.Millisecond)
	state := mit.GetCircuitState("ep-flap")
	if state != CircuitOpen {
		t.Errorf("FAIL: Expected circuit to remain CircuitOpen due to flapping backoff, but got %s", state)
	}
}

// TestAdversarial_PruneStaleSocketsCleansEndpointMap verifies that pruning stale sockets also cleans endpointMap.
func TestAdversarial_PruneStaleSocketsCleansEndpointMap(t *testing.T) {
	collector := NewTelemetryCollector()
	collector.Ingest(SocketMetrics{
		SocketID:   "sock-old",
		EndpointID: "ep-dead",
		Timestamp:  time.Now().Add(-10 * time.Minute),
	})

	if len(collector.ListEndpointSummaries()) != 1 {
		t.Fatalf("expected 1 endpoint summary")
	}

	pruned := collector.PruneStaleSockets(5 * time.Minute)
	if pruned != 1 {
		t.Fatalf("expected 1 socket pruned, got %d", pruned)
	}

	summaries := collector.ListEndpointSummaries()
	if len(summaries) != 0 {
		t.Errorf("FAIL: Stale endpoint was not pruned from endpointMap: %+v", summaries)
	}
}

// TestAdversarial_APIValidation ensures bad requests receive 400 Bad Request.
func TestAdversarial_APIValidation(t *testing.T) {
	cfg := DefaultTelemetryConfig()
	engine := NewTelemetryEngine(cfg)
	handler := NewTelemetryHandler(engine)

	// 1. Ingest with missing socket_id
	emptyBody := `{"remote_addr":"1.2.3.4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/telemetry/ingest", strings.NewReader(emptyBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("FAIL: Expected 400 Bad Request for ingest with missing socket_id, got %d", rec.Code)
	}

	// 2. Reroute with primary == standby
	selfReroute := `{"primary_endpoint":"ep-same","standby_endpoint":"ep-same"}`
	req = httptest.NewRequest(http.MethodPost, "/api/telemetry/reroute", strings.NewReader(selfReroute))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("FAIL: Expected 400 Bad Request for self-rerouting loop, got %d", rec.Code)
	}
}

// TestAdversarial_RingBufferZeroCapacity verifies safe behavior when capacity is 0.
func TestAdversarial_RingBufferZeroCapacity(t *testing.T) {
	rb := &RingBuffer{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("FAIL: RingBuffer panicked on zero capacity: %v", r)
		}
	}()
	rb.Push(SocketMetrics{SocketID: "sock-safe"})
}

// TestAdversarial_RingBufferConcurrentDrainAndSaturation stresses ringbuffer under extreme saturation.
func TestAdversarial_RingBufferConcurrentDrainAndSaturation(t *testing.T) {
	rb := NewRingBuffer(32)
	var wg sync.WaitGroup
	var pushedTotal int64
	var drainedTotal int64
	stop := make(chan struct{})

	// 10 concurrent producers saturating the buffer
	for p := 0; p < 10; p++ {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				rb.Push(SocketMetrics{
					SocketID: "sock-burst",
					PacketsOut: uint32(i),
				})
				atomic.AddInt64(&pushedTotal, 1)
			}
		}(p)
	}

	// 2 concurrent consumers draining
	for c := 0; c < 2; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					items := rb.DrainAll()
					atomic.AddInt64(&drainedTotal, int64(len(items)))
					return
				default:
					items := rb.Drain(16)
					atomic.AddInt64(&drainedTotal, int64(len(items)))
					time.Sleep(100 * time.Microsecond)
				}
			}
		}()
	}

	time.Sleep(10 * time.Millisecond)
	close(stop)
	wg.Wait()

	dropped := rb.DroppedCount()
	t.Logf("RingBuffer Stress: Pushed=%d, Drained=%d, Dropped=%d, Remainder=%d",
		pushedTotal, drainedTotal, dropped, rb.Len())
	if pushedTotal != (drainedTotal + int64(dropped) + int64(rb.Len())) {
		t.Errorf("FAIL: Accounting mismatch: Pushed (%d) != Drained (%d) + Dropped (%d) + Remainder (%d)",
			pushedTotal, drainedTotal, dropped, rb.Len())
	}
}
