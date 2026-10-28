package telemetry

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TelemetryEngine orchestrates the distributed eBPF telemetry pipeline and self-healing engine.
type TelemetryEngine struct {
	config       TelemetryConfig
	ringBuffer   *RingBuffer
	probeDriver  ProbeDriver
	collector    *TelemetryCollector
	detector     *AnomalyDetector
	mitigation   *MitigationEngine
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	probesCount  int64
	anomCount    int64
	started      int32
}

// NewTelemetryEngine initializes all telemetry pipeline components.
func NewTelemetryEngine(cfg TelemetryConfig) *TelemetryEngine {
	rb := NewRingBuffer(cfg.RingBufferSize)
	driver := NewPlatformProbeDriver(rb)
	collector := NewTelemetryCollector()
	detector := NewAnomalyDetector(cfg)
	mitigation := NewMitigationEngine(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	engine := &TelemetryEngine{
		config:      cfg,
		ringBuffer:  rb,
		probeDriver: driver,
		collector:   collector,
		detector:    detector,
		mitigation:  mitigation,
		ctx:         ctx,
		cancel:      cancel,
	}

	return engine
}

// Start spawns the background ringbuffer consumer loop and periodic maintenance worker.
func (e *TelemetryEngine) Start(ctx ...context.Context) {
	if !atomic.CompareAndSwapInt32(&e.started, 0, 1) {
		return // Already running
	}

	if len(ctx) > 0 && ctx[0] != nil {
		e.ctx, e.cancel = context.WithCancel(ctx[0])
	}

	e.wg.Add(2)
	go e.runRingBufferConsumer()
	go e.runMaintenanceLoop()
}

// RegisterTarget tracks an endpoint target and attaches a default socket probe.
func (e *TelemetryEngine) RegisterTarget(targetID, addr, protocol string) {
	if targetID == "" {
		targetID = "default"
	}
	sockID := fmt.Sprintf("sock-%s-%s", targetID, addr)
	_ = e.probeDriver.AttachSocket(sockID, targetID, addr, "127.0.0.1:0")

	// Ingest baseline healthy metrics for this target
	e.IngestProbe(SocketMetrics{
		SocketID:      sockID,
		EndpointID:    targetID,
		RemoteAddr:    addr,
		LocalAddr:     "127.0.0.1:0",
		State:         "ESTABLISHED",
		CWND:          32,
		SSTHRESH:      64,
		RcvWnd:        65535,
		RTTUs:         14500,
		RTTVarUs:      1500,
		BytesInFlight: 1460,
		TotalRetrans:  0,
		RetransRate:   0.0,
		PacketsOut:    100,
		Timestamp:     time.Now(),
	})
}

// Stop terminates background workers cleanly.
func (e *TelemetryEngine) Stop() {
	if !atomic.CompareAndSwapInt32(&e.started, 1, 0) {
		return
	}
	e.cancel()
	e.wg.Wait()
	_ = e.probeDriver.Close()
}

// IngestProbe pushes a socket metrics event into the pipeline immediately.
func (e *TelemetryEngine) IngestProbe(metrics SocketMetrics) {
	atomic.AddInt64(&e.probesCount, 1)
	e.ringBuffer.Push(metrics)
}

// ProcessMetrics synchronously processes metrics through collector, detector, and mitigation.
func (e *TelemetryEngine) ProcessMetrics(metrics SocketMetrics) ([]AnomalyReport, *MitigationAction) {
	atomic.AddInt64(&e.probesCount, 1)
	e.collector.Ingest(metrics)

	epID := metrics.EndpointID
	if epID == "" {
		epID = "default"
	}

	summary, _ := e.collector.GetEndpointSummary(epID)
	anomalies := e.detector.Evaluate(metrics, summary)

	var lastAction *MitigationAction
	if len(anomalies) > 0 {
		atomic.AddInt64(&e.anomCount, int64(len(anomalies)))
		for _, anom := range anomalies {
			if action, triggered := e.mitigation.HandleAnomaly(anom); triggered {
				lastAction = action
			}
		}
	} else {
		// Healthy sample - probe self-healing recovery if HalfOpen
		if action, healed := e.mitigation.HandleHealthySample(epID); healed {
			lastAction = action
		}
	}

	return anomalies, lastAction
}

func (e *TelemetryEngine) runRingBufferConsumer() {
	defer e.wg.Done()
	ticker := time.NewTicker(2 * time.Millisecond) // Ultra-low-latency 2ms polling loop
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			// Drain remaining events before exit
			events := e.ringBuffer.DrainAll()
			for _, ev := range events {
				_, _ = e.ProcessMetrics(ev)
			}
			return
		case <-ticker.C:
			events := e.ringBuffer.Drain(64)
			for _, ev := range events {
				_, _ = e.ProcessMetrics(ev)
			}
		}
	}
}

func (e *TelemetryEngine) runMaintenanceLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.collector.PruneStaleSockets(5 * time.Minute)
		}
	}
}

// SimulateDegradation injects synthetic degraded socket telemetry to trigger detection & mitigation.
func (e *TelemetryEngine) SimulateDegradation(endpointID string, kind AnomalyType) ([]AnomalyReport, *MitigationAction) {
	if endpointID == "" {
		endpointID = "worker-sim"
	}
	sockID := fmt.Sprintf("sock-sim-%s", endpointID)

	var metrics SocketMetrics
	now := time.Now()

	switch kind {
	case AnomalyWindowCollapse:
		metrics = SocketMetrics{
			SocketID:      sockID,
			EndpointID:    endpointID,
			State:         "ESTABLISHED",
			CWND:          1,
			RcvWnd:        0, // Zero-window collapse
			RTTUs:         25000,
			BytesInFlight: 8192,
			PacketsOut:    100,
			Timestamp:     now,
		}
	case AnomalyRetransSpike:
		metrics = SocketMetrics{
			SocketID:      sockID,
			EndpointID:    endpointID,
			State:         "ESTABLISHED",
			CWND:          32,
			RcvWnd:        65535,
			RTTUs:         45000,
			BytesInFlight: 4096,
			TotalRetrans:  30,
			PacketsOut:    70,
			RetransRate:   30.0, // 30% packet loss
			Timestamp:     now,
		}
	case AnomalyPreTimeout:
		metrics = SocketMetrics{
			SocketID:      sockID,
			EndpointID:    endpointID,
			State:         "ESTABLISHED",
			CWND:          8,
			RcvWnd:        32768,
			RTTUs:         650000, // 650ms predictive pre-timeout (HTTP timeout is 5s)
			BytesInFlight: 16384,
			PacketsOut:    20,
			Timestamp:     now,
		}
	default:
		metrics = SocketMetrics{
			SocketID:      sockID,
			EndpointID:    endpointID,
			State:         "ESTABLISHED",
			CWND:          16,
			RcvWnd:        8192,
			RTTUs:         180000,
			BytesInFlight: 2048,
			TotalRetrans:  8,
			PacketsOut:    92,
			RetransRate:   8.0,
			Timestamp:     now,
		}
	}

	return e.ProcessMetrics(metrics)
}

// ResolveRoute returns the optimal endpoint and circuit state.
func (e *TelemetryEngine) ResolveRoute(endpointID string) (targetEndpoint string, state CircuitState) {
	return e.mitigation.ResolveRoute(endpointID)
}

// GetCircuitState returns the circuit breaker state for an endpoint.
func (e *TelemetryEngine) GetCircuitState(endpointID string) CircuitState {
	return e.mitigation.GetCircuitState(endpointID)
}

// SetStandbyRoute registers an explicit reroute destination.
func (e *TelemetryEngine) SetStandbyRoute(primary, standby string) {
	e.mitigation.SetStandbyRoute(primary, standby)
}

// GetStats compiles comprehensive pipeline performance statistics.
func (e *TelemetryEngine) GetStats() TelemetryStats {
	totalMit, avgLat, maxLat, sub10Rate, openCircuits, activeReroutes := e.mitigation.GetStats()

	return TelemetryStats{
		TotalProbesIngested:        atomic.LoadInt64(&e.probesCount),
		ActiveSocketsCount:         e.collector.TotalSocketsCount(),
		TotalAnomaliesDetected:     atomic.LoadInt64(&e.anomCount),
		TotalMitigationsTriggered:  totalMit,
		AverageMitigationLatencyUs: avgLat,
		MaxMitigationLatencyUs:     maxLat,
		Sub10msEnforcementRate:     sub10Rate,
		ActiveCircuitsOpen:         openCircuits,
		ActiveReroutesCount:        activeReroutes,
	}
}

// Collector returns the underlying collector.
func (e *TelemetryEngine) Collector() *TelemetryCollector {
	return e.collector
}

// Detector returns the underlying anomaly detector.
func (e *TelemetryEngine) Detector() *AnomalyDetector {
	return e.detector
}

// Mitigation returns the underlying mitigation engine.
func (e *TelemetryEngine) Mitigation() *MitigationEngine {
	return e.mitigation
}

// Driver returns the platform probe driver.
func (e *TelemetryEngine) Driver() ProbeDriver {
	return e.probeDriver
}

// Reset restores all components to initial state.
func (e *TelemetryEngine) Reset() {
	e.ringBuffer.Reset()
	e.collector.Reset()
	e.detector.Reset()
	e.mitigation.Reset()
	atomic.StoreInt64(&e.probesCount, 0)
	atomic.StoreInt64(&e.anomCount, 0)
}
