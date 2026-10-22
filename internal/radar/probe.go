package radar

import (
	"context"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// AnomalyListener receives notifications on sudden latency shifts or degradation.
type AnomalyListener func(iata string, oldRTT, newRTT float64, degraded bool)

// ProbeEngine orchestrates synthetic edge probes and tracks real-time PoP telemetry.
type ProbeEngine struct {
	mu           sync.RWMutex
	pops         map[string]PoPInfo
	stats        map[string]*ProbeStats
	httpClient   *http.Client
	probeFunc    func(endpoint string) (int64, error)
	alpha        float64
	running      bool
	stopChan     chan struct{}
	listeners    []AnomalyListener
	totalProbes  int64
	lastProbeRun time.Time
}

// NewProbeEngine initializes the edge health probe engine.
func NewProbeEngine(popList []PoPInfo) *ProbeEngine {
	pe := &ProbeEngine{
		pops:       make(map[string]PoPInfo),
		stats:      make(map[string]*ProbeStats),
		httpClient: &http.Client{Timeout: 2 * time.Second},
		alpha:      0.25, // Exponential Moving Average smoothing factor
		stopChan:   make(chan struct{}),
	}

	for _, p := range popList {
		pe.pops[p.IATA] = p
		// Baseline initial telemetry based on geographic tier
		baseRTT := 15.0 + float64(p.Tier)*10.0
		pe.stats[p.IATA] = &ProbeStats{
			LastRTTMs:     int64(baseRTT),
			EMARTTMs:      baseRTT,
			JitterMs:      1.5,
			LossRate:      0.0,
			Health:        HealthOptimal,
			HealthScore:   98,
			ProbeCount:    1,
			SuccessCount:  1,
			LastProbeTime: time.Now().UTC(),
		}
	}

	// Default synthetic probe via HTTP HEAD request
	pe.probeFunc = func(endpoint string) (int64, error) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
		if err != nil {
			return -1, err
		}
		resp, err := pe.httpClient.Do(req)
		if err != nil {
			return -1, err
		}
		_ = resp.Body.Close()
		return time.Since(start).Milliseconds(), nil
	}

	return pe
}

// SetCustomProbeFunc overrides the probe dispatcher (ideal for unit testing).
func (pe *ProbeEngine) SetCustomProbeFunc(fn func(endpoint string) (int64, error)) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.probeFunc = fn
}

// OnAnomaly registers a callback for sudden latency shifts or degradation.
func (pe *ProbeEngine) OnAnomaly(listener AnomalyListener) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.listeners = append(pe.listeners, listener)
}

// RecordProbeResult updates the telemetry state for a PoP with a new probe measurement.
func (pe *ProbeEngine) RecordProbeResult(iata string, rttMs int64, success bool) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	atomic.AddInt64(&pe.totalProbes, 1)
	st, exists := pe.stats[iata]
	if !exists {
		st = &ProbeStats{
			Health:      HealthHealthy,
			HealthScore: 80,
		}
		pe.stats[iata] = st
	}

	oldEMA := st.EMARTTMs
	st.ProbeCount++
	st.LastProbeTime = time.Now().UTC()

	var triggerAnomaly bool
	var degraded bool

	if !success || rttMs < 0 {
		st.ConsecutiveFailures++
		st.LossRate = (st.LossRate*0.8) + 0.2 // Weight loss
		if st.ConsecutiveFailures >= 3 {
			st.Health = HealthOffline
			st.HealthScore = 0
			degraded = true
			triggerAnomaly = true
		} else {
			st.Health = HealthDegraded
			st.HealthScore = 40
			degraded = true
			triggerAnomaly = true
		}
	} else {
		st.ConsecutiveFailures = 0
		st.SuccessCount++
		st.LastRTTMs = rttMs

		// Exponential Moving Average (EMA) and Jitter Calculation
		diff := math.Abs(float64(rttMs) - oldEMA)
		st.JitterMs = (st.JitterMs * 0.75) + (diff * 0.25)
		st.EMARTTMs = (st.EMARTTMs * (1 - pe.alpha)) + (float64(rttMs) * pe.alpha)
		st.LossRate = st.LossRate * 0.8 // Decay loss rate on success

		// BGP Route Change / Latency Spike Detection
		// Spike triggered if RTT increases by > 35ms or more than 1.8x existing EMA
		if float64(rttMs) > (oldEMA*1.8) && (float64(rttMs)-oldEMA) > 30.0 {
			triggerAnomaly = true
			st.Health = HealthDegraded
			st.HealthScore = 55
			degraded = true
		} else {
			health, score := pe.evaluateHealth(st)
			st.Health = health
			st.HealthScore = score
		}
	}

	if triggerAnomaly {
		for _, l := range pe.listeners {
			l(iata, oldEMA, float64(rttMs), degraded)
		}
	}
}

// evaluateHealth calculates a composite health status and score (0-100).
func (pe *ProbeEngine) evaluateHealth(st *ProbeStats) (PoPHealth, int) {
	if st.ConsecutiveFailures >= 3 {
		return HealthOffline, 0
	}
	if st.ConsecutiveFailures > 0 || st.LossRate > 0.15 || st.EMARTTMs > 140.0 {
		score := int(100.0 - (st.EMARTTMs * 0.3) - (st.LossRate * 100.0))
		if score < 20 {
			score = 20
		}
		return HealthDegraded, score
	}
	if st.EMARTTMs <= 35.0 && st.JitterMs <= 5.0 && st.LossRate == 0.0 {
		return HealthOptimal, 99
	}
	score := int(100.0 - (st.EMARTTMs * 0.15) - (st.JitterMs * 2.0))
	if score < 75 {
		score = 75
	}
	return HealthHealthy, score
}

// ProbePoP executes an immediate synthetic probe for a designated PoP.
func (pe *ProbeEngine) ProbePoP(iata string) (int64, error) {
	pe.mu.RLock()
	pop, ok := pe.pops[iata]
	probeFn := pe.probeFunc
	pe.mu.RUnlock()

	if !ok {
		return -1, http.ErrNotSupported
	}

	rtt, err := probeFn(pop.ProbeEndpoint)
	pe.RecordProbeResult(iata, rtt, err == nil)
	return rtt, err
}

// ProbeAll concurrently pings all registered edge PoPs.
func (pe *ProbeEngine) ProbeAll(concurrency int) map[string]int64 {
	pe.mu.RLock()
	popList := make([]PoPInfo, 0, len(pe.pops))
	for _, p := range pe.pops {
		popList = append(popList, p)
	}
	pe.mu.RUnlock()

	if concurrency <= 0 {
		concurrency = 10
	}

	results := make(map[string]int64)
	var resMu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, p := range popList {
		wg.Add(1)
		sem <- struct{}{}
		go func(pop PoPInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			rtt, _ := pe.ProbePoP(pop.IATA)
			resMu.Lock()
			results[pop.IATA] = rtt
			resMu.Unlock()
		}(p)
	}

	wg.Wait()
	pe.mu.Lock()
	pe.lastProbeRun = time.Now().UTC()
	pe.mu.Unlock()
	return results
}

// GetStats returns telemetry metrics for a given edge PoP.
func (pe *ProbeEngine) GetStats(iata string) (ProbeStats, bool) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	st, exists := pe.stats[iata]
	if !exists {
		return ProbeStats{}, false
	}
	return *st, true
}

// GetAllStats returns a snapshot of all active PoP telemetry.
func (pe *ProbeEngine) GetAllStats() map[string]ProbeStats {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	snapshot := make(map[string]ProbeStats, len(pe.stats))
	for k, v := range pe.stats {
		snapshot[k] = *v
	}
	return snapshot
}

// Start begins the background periodic synthetic probing ticker.
func (pe *ProbeEngine) Start(interval time.Duration) {
	pe.mu.Lock()
	if pe.running {
		pe.mu.Unlock()
		return
	}
	pe.running = true
	pe.stopChan = make(chan struct{})
	pe.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-pe.stopChan:
				return
			case <-ticker.C:
				pe.ProbeAll(10)
			}
		}
	}()
}

// Stop halts the background periodic probing loop.
func (pe *ProbeEngine) Stop() {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	if !pe.running {
		return
	}
	pe.running = false
	close(pe.stopChan)
}
