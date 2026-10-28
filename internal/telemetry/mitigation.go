package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitBreaker manages per-endpoint trip status, cooldown, and canary probing.
type CircuitBreaker struct {
	EndpointID    string
	State         CircuitState
	RerouteTarget string
	TrippedAt     time.Time
	LastTestedAt  time.Time
	CanarySuccess int
	FlapCount     int
	LastHealedAt  time.Time
}

// MitigationEngine manages sub-10ms circuit breaking, autonomous traffic rerouting, and self-healing.
type MitigationEngine struct {
	mu                   sync.RWMutex
	config               TelemetryConfig
	circuits             map[string]*CircuitBreaker // endpointID -> breaker
	rerouteRules         map[string]string          // endpointID -> configured standby target
	activeReroutes       map[string]string          // endpointID -> active reroute target
	actions              []MitigationAction
	maxActions           int
	totalMitigations     int64
	totalLatencyUs       int64
	maxLatencyUs         int64
	sub10msEnforcedCount int64
}

// NewMitigationEngine constructs an autonomous self-healing engine.
func NewMitigationEngine(cfg TelemetryConfig) *MitigationEngine {
	return &MitigationEngine{
		config:         cfg,
		circuits:       make(map[string]*CircuitBreaker),
		rerouteRules:   make(map[string]string),
		activeReroutes: make(map[string]string),
		actions:        make([]MitigationAction, 0, 200),
		maxActions:     200,
	}
}

// SetStandbyRoute registers an alternative endpoint to reroute to when an endpoint degrades.
func (m *MitigationEngine) SetStandbyRoute(primaryEndpoint, standbyEndpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rerouteRules[primaryEndpoint] = standbyEndpoint
}

func (m *MitigationEngine) effectiveCooldown(cb *CircuitBreaker) time.Duration {
	cooldown := m.config.CircuitCooldownDuration
	if cb.FlapCount > 0 {
		shift := cb.FlapCount
		if shift > 5 {
			shift = 5
		}
		cooldown = m.config.CircuitCooldownDuration * time.Duration(1<<shift)
	}
	return cooldown
}

// GetCircuitState returns the current circuit state for an endpoint.
func (m *MitigationEngine) GetCircuitState(endpointID string) CircuitState {
	m.mu.Lock()
	defer m.mu.Unlock()

	cb, exists := m.circuits[endpointID]
	if !exists {
		return CircuitClosed
	}

	// Check if cooldown expired to automatically promote Open to HalfOpen
	if cb.State == CircuitOpen && time.Since(cb.TrippedAt) >= m.effectiveCooldown(cb) {
		cb.State = CircuitHalfOpen
		cb.LastTestedAt = time.Now()
	}

	return cb.State
}

// ResolveRoute returns the active target endpoint and circuit state.
// If the primary endpoint is tripped, traffic is rerouted to its standby target.
func (m *MitigationEngine) ResolveRoute(endpointID string) (targetEndpoint string, state CircuitState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cb, exists := m.circuits[endpointID]
	if !exists {
		return endpointID, CircuitClosed
	}

	// Cooldown expiration check
	if cb.State == CircuitOpen && time.Since(cb.TrippedAt) >= m.config.CircuitCooldownDuration {
		cb.State = CircuitHalfOpen
		cb.LastTestedAt = time.Now()
	}

	if cb.State == CircuitOpen {
		if target, hasReroute := m.activeReroutes[endpointID]; hasReroute && target != "" {
			return target, CircuitOpen
		}
	}

	return endpointID, cb.State
}

// HandleAnomaly evaluates an anomaly report and triggers autonomous mitigation within 10 milliseconds.
func (m *MitigationEngine) HandleAnomaly(anomaly AnomalyReport) (*MitigationAction, bool) {
	startTime := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	epID := anomaly.EndpointID
	if epID == "" {
		epID = "default"
	}

	cb, exists := m.circuits[epID]
	if !exists {
		cb = &CircuitBreaker{
			EndpointID: epID,
			State:      CircuitClosed,
		}
		m.circuits[epID] = cb
	}

	// If already open, verify reroute is active
	if cb.State == CircuitOpen {
		return nil, false
	}

	// Trigger criteria: Critical severity OR Score >= threshold
	shouldTrip := anomaly.Severity == SeverityCritical || anomaly.Score >= m.config.CircuitBreakScoreThreshold
	if !shouldTrip {
		return nil, false
	}

	// Determine standby reroute target
	standby, hasStandby := m.rerouteRules[epID]
	if !hasStandby || standby == "" {
		standby = epID + "-standby"
	}

	prevState := cb.State
	if !cb.LastHealedAt.IsZero() && time.Since(cb.LastHealedAt) < 10*time.Second {
		cb.FlapCount++
	} else if prevState == CircuitClosed {
		cb.FlapCount = 0
	}
	cb.State = CircuitOpen
	cb.TrippedAt = startTime
	cb.CanarySuccess = 0
	cb.RerouteTarget = standby
	m.activeReroutes[epID] = standby

	// Compute latency from anomaly detection to mitigation execution in microseconds
	latencyUs := int64(1)
	if !anomaly.DetectedAt.IsZero() {
		latencyUs = time.Since(anomaly.DetectedAt).Microseconds()
	}
	if latencyUs <= 0 || latencyUs > 10000000 {
		latencyUs = time.Since(startTime).Microseconds()
	}
	if latencyUs <= 0 {
		latencyUs = 1 // At least 1 µs
	}

	action := MitigationAction{
		ActionID:         m.generateID("act-trip-"),
		EndpointID:       epID,
		TriggerAnomalyID: anomaly.ID,
		ActionType:       "circuit_break",
		PreviousState:    prevState,
		NewState:         CircuitOpen,
		RerouteTarget:    standby,
		TriggerLatencyUs: latencyUs,
		ExecutedAt:       time.Now(),
	}

	m.recordAction(action, latencyUs)
	return &action, true
}

// HandleHealthySample checks if an endpoint in HalfOpen state can be healed back to Closed.
func (m *MitigationEngine) HandleHealthySample(endpointID string) (*MitigationAction, bool) {
	startTime := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	cb, exists := m.circuits[endpointID]
	if !exists {
		return nil, false
	}

	// Automatic transition from Open to HalfOpen if cooldown passed
	if cb.State == CircuitOpen && time.Since(cb.TrippedAt) >= m.effectiveCooldown(cb) {
		cb.State = CircuitHalfOpen
	}

	if cb.State != CircuitHalfOpen {
		return nil, false
	}

	cb.CanarySuccess++
	if cb.CanarySuccess >= 2 { // Requires 2 consecutive healthy probes to fully recover
		prevState := cb.State
		cb.State = CircuitClosed
		cb.CanarySuccess = 0
		cb.LastHealedAt = startTime
		cb.RerouteTarget = ""
		delete(m.activeReroutes, endpointID)

		latencyUs := time.Since(startTime).Microseconds()
		if latencyUs == 0 {
			latencyUs = 1
		}

		action := MitigationAction{
			ActionID:         m.generateID("act-heal-"),
			EndpointID:       endpointID,
			ActionType:       "recover",
			PreviousState:    prevState,
			NewState:         CircuitClosed,
			RerouteTarget:    "",
			TriggerLatencyUs: latencyUs,
			ExecutedAt:       time.Now(),
		}

		m.recordAction(action, latencyUs)
		return &action, true
	}

	return nil, false
}

// ForceCircuitState manually sets circuit state for testing or operator override.
func (m *MitigationEngine) ForceCircuitState(endpointID string, state CircuitState, rerouteTarget string) MitigationAction {
	startTime := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	cb, exists := m.circuits[endpointID]
	if !exists {
		cb = &CircuitBreaker{EndpointID: endpointID}
		m.circuits[endpointID] = cb
	}

	prevState := cb.State
	cb.State = state
	cb.TrippedAt = startTime

	if state == CircuitOpen {
		if rerouteTarget == "" {
			rerouteTarget = endpointID + "-standby"
		}
		cb.RerouteTarget = rerouteTarget
		m.activeReroutes[endpointID] = rerouteTarget
	} else if state == CircuitClosed {
		cb.RerouteTarget = ""
		delete(m.activeReroutes, endpointID)
	}

	latencyUs := time.Since(startTime).Microseconds()
	if latencyUs == 0 {
		latencyUs = 1
	}

	act := MitigationAction{
		ActionID:         m.generateID("act-force-"),
		EndpointID:       endpointID,
		ActionType:       "manual_override",
		PreviousState:    prevState,
		NewState:         state,
		RerouteTarget:    rerouteTarget,
		TriggerLatencyUs: latencyUs,
		ExecutedAt:       time.Now(),
	}

	m.recordAction(act, latencyUs)
	return act
}

func (m *MitigationEngine) recordAction(action MitigationAction, latencyUs int64) {
	if len(m.actions) >= m.maxActions {
		m.actions = m.actions[1:]
	}
	m.actions = append(m.actions, action)

	atomic.AddInt64(&m.totalMitigations, 1)
	atomic.AddInt64(&m.totalLatencyUs, latencyUs)
	if latencyUs <= 10000 { // 10ms threshold in µs
		atomic.AddInt64(&m.sub10msEnforcedCount, 1)
	}
	if latencyUs > atomic.LoadInt64(&m.maxLatencyUs) {
		atomic.StoreInt64(&m.maxLatencyUs, latencyUs)
	}
}

// ListActions returns recent mitigation actions up to limit.
func (m *MitigationEngine) ListActions(limit int) []MitigationAction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.actions)
	if total == 0 {
		return nil
	}
	if limit <= 0 || limit > total {
		limit = total
	}

	start := total - limit
	out := make([]MitigationAction, limit)
	copy(out, m.actions[start:])
	return out
}

// ListActiveCircuits returns state information for all tracked circuits.
func (m *MitigationEngine) ListActiveCircuits() map[string]CircuitBreaker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]CircuitBreaker, len(m.circuits))
	for k, v := range m.circuits {
		out[k] = *v
	}
	return out
}

// GetStats returns mitigation statistics including sub-10ms enforcement rate.
func (m *MitigationEngine) GetStats() (total int64, avgLatencyUs float64, maxLatencyUs int64, sub10msRate float64, openCount int, rerouteCount int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total = atomic.LoadInt64(&m.totalMitigations)
	totLat := atomic.LoadInt64(&m.totalLatencyUs)
	maxLatencyUs = atomic.LoadInt64(&m.maxLatencyUs)
	sub10Count := atomic.LoadInt64(&m.sub10msEnforcedCount)

	if total > 0 {
		avgLatencyUs = float64(totLat) / float64(total)
		sub10msRate = (float64(sub10Count) / float64(total)) * 100.0
	} else {
		sub10msRate = 100.0 // Default 100% when no anomalies have occurred
	}

	for _, cb := range m.circuits {
		if cb.State == CircuitOpen {
			openCount++
		}
	}
	rerouteCount = len(m.activeReroutes)

	return total, avgLatencyUs, maxLatencyUs, sub10msRate, openCount, rerouteCount
}

// Reset clears all circuit breaker states, reroutes, and action history.
func (m *MitigationEngine) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.circuits = make(map[string]*CircuitBreaker)
	m.activeReroutes = make(map[string]string)
	m.actions = make([]MitigationAction, 0, m.maxActions)
	atomic.StoreInt64(&m.totalMitigations, 0)
	atomic.StoreInt64(&m.totalLatencyUs, 0)
	atomic.StoreInt64(&m.maxLatencyUs, 0)
	atomic.StoreInt64(&m.sub10msEnforcedCount, 0)
}

func (m *MitigationEngine) generateID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}
