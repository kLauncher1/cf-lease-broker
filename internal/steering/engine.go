package steering

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrNoHealthyEndpoints = errors.New("no healthy worker endpoints available for packet steering")
	ErrEndpointNotFound   = errors.New("worker endpoint not found")
	ErrDuplicateEndpoint  = errors.New("worker endpoint already registered")
	ErrDuplicateRule      = errors.New("steering rule already registered")
	ErrRuleNotFound       = errors.New("steering rule not found")
)

type driverInterface interface {
	IsAvailable() bool
	GetMode() SteeringMode
	UpdateSockmap(cidPrefix string, targetAddress string) error
	RedirectSocket(fd int, targetAddress string) error
	Close() error
}

// PacketSteeringEngine manages worker endpoints, CID steering rules, and high-speed packet dispatching.
type PacketSteeringEngine struct {
	mu            sync.RWMutex
	config        SteeringConfig
	driver        driverInterface
	endpoints     map[string]*WorkerEndpoint
	rules         []*SteeringRule
	endpointRound int

	// Atomic Telemetry Counters
	totalPackets        atomic.Uint64
	totalBytes          atomic.Uint64
	kernelBypassed      atomic.Uint64
	userspaceDispatched atomic.Uint64
	sub10Microsec       atomic.Uint64
	cumulativeLatencyNs atomic.Int64
}

// NewPacketSteeringEngine initializes the steering engine with kernel driver hooks and configuration.
func NewPacketSteeringEngine(cfg SteeringConfig) *PacketSteeringEngine {
	driver := NewPlatformEBPFDriver()

	return &PacketSteeringEngine{
		config:    cfg,
		driver:    driver,
		endpoints: make(map[string]*WorkerEndpoint),
		rules:     make([]*SteeringRule, 0),
	}
}

// RegisterEndpoint registers a new edge worker destination into the active steering pool.
func (e *PacketSteeringEngine) RegisterEndpoint(ep *WorkerEndpoint) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if ep.ID == "" {
		return errors.New("endpoint ID cannot be empty")
	}
	if ep.Address == "" {
		return errors.New("endpoint address cannot be empty")
	}
	if _, exists := e.endpoints[ep.ID]; exists {
		return ErrDuplicateEndpoint
	}
	if len(e.endpoints) >= e.config.MaxEndpoints {
		return fmt.Errorf("maximum endpoints (%d) reached", e.config.MaxEndpoints)
	}

	clone := *ep
	clone.LastSeen = time.Now()
	if clone.Weight <= 0 {
		clone.Weight = 100
	}
	clone.Healthy = true
	e.endpoints[ep.ID] = &clone

	return nil
}

// DeregisterEndpoint removes an endpoint from the active steering pool.
func (e *PacketSteeringEngine) DeregisterEndpoint(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.endpoints[id]; !exists {
		return ErrEndpointNotFound
	}
	delete(e.endpoints, id)
	return nil
}

// UpdateEndpointHealth updates the reachability status of a worker endpoint.
func (e *PacketSteeringEngine) UpdateEndpointHealth(id string, healthy bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	ep, exists := e.endpoints[id]
	if !exists {
		return ErrEndpointNotFound
	}
	ep.Healthy = healthy
	ep.LastSeen = time.Now()
	return nil
}

// ListEndpoints returns all registered worker endpoints.
func (e *PacketSteeringEngine) ListEndpoints() []*WorkerEndpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]*WorkerEndpoint, 0, len(e.endpoints))
	for _, ep := range e.endpoints {
		clone := *ep
		result = append(result, &clone)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// GetEndpoint fetches a single endpoint by ID.
func (e *PacketSteeringEngine) GetEndpoint(id string) (*WorkerEndpoint, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ep, exists := e.endpoints[id]
	if !exists {
		return nil, false
	}
	clone := *ep
	return &clone, true
}

// AddRule registers a CID or service steering rule, updating kernel sockmap tables if applicable.
func (e *PacketSteeringEngine) AddRule(rule *SteeringRule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if rule.ID == "" {
		return errors.New("rule ID cannot be empty")
	}
	if rule.TargetEndpointID == "" {
		return errors.New("target endpoint ID cannot be empty")
	}
	if _, ok := e.endpoints[rule.TargetEndpointID]; !ok {
		return fmt.Errorf("target endpoint %s does not exist", rule.TargetEndpointID)
	}

	for _, existing := range e.rules {
		if existing.ID == rule.ID {
			return ErrDuplicateRule
		}
	}

	clone := *rule
	e.rules = append(e.rules, &clone)
	sort.Slice(e.rules, func(i, j int) bool {
		return e.rules[i].Priority > e.rules[j].Priority
	})

	// Sync rule to kernel eBPF sockmap driver
	if targetEp, ok := e.endpoints[rule.TargetEndpointID]; ok {
		_ = e.driver.UpdateSockmap(rule.CIDPrefix, targetEp.Address)
	}

	return nil
}

// RemoveRule deletes a steering rule by ID.
func (e *PacketSteeringEngine) RemoveRule(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	foundIndex := -1
	for i, r := range e.rules {
		if r.ID == id {
			foundIndex = i
			break
		}
	}
	if foundIndex == -1 {
		return ErrRuleNotFound
	}

	e.rules = append(e.rules[:foundIndex], e.rules[foundIndex+1:]...)
	return nil
}

// ListRules returns all configured steering rules ordered by priority.
func (e *PacketSteeringEngine) ListRules() []*SteeringRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]*SteeringRule, len(e.rules))
	for i, r := range e.rules {
		clone := *r
		result[i] = &clone
	}
	return result
}

// SteerPacket inspects raw UDP packet bytes, parses QUIC headers if applicable, resolves the
// optimal worker endpoint, and executes microsecond steering.
func (e *PacketSteeringEngine) SteerPacket(rawPacket []byte, srcAddr, dstAddr string) (*SteeringDecision, error) {
	start := time.Now()

	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.endpoints) == 0 {
		return nil, ErrNoHealthyEndpoints
	}

	var parsedHeader *QUICPacketHeader
	var matchedRuleID string
	var selectedEndpoint *WorkerEndpoint

	// 1. Inspect QUIC Header if enabled
	if e.config.EnableQUICInspection && len(rawPacket) > 0 {
		h, err := ParseQUICHeader(rawPacket, e.config.DefaultDCIDLen)
		if err == nil {
			parsedHeader = h
			// Attempt to match DCID against registered rules
			for _, rule := range e.rules {
				if rule.CIDPrefix == "*" || (len(rule.CIDPrefix) > 0 && strings.HasPrefix(strings.ToLower(h.DCID), strings.ToLower(rule.CIDPrefix))) {
					if target, ok := e.endpoints[rule.TargetEndpointID]; ok && target.Healthy {
						selectedEndpoint = target
						matchedRuleID = rule.ID
						break
					}
				}
			}
		}
	}

	// 2. Fallback: Least Connections across healthy endpoints
	if selectedEndpoint == nil {
		var healthyList []*WorkerEndpoint
		for _, ep := range e.endpoints {
			if ep.Healthy {
				healthyList = append(healthyList, ep)
			}
		}
		if len(healthyList) == 0 {
			return nil, ErrNoHealthyEndpoints
		}

		// Least active connections
		minConns := int64(1<<62 - 1)
		for _, ep := range healthyList {
			if ep.ActiveConns < minConns {
				minConns = ep.ActiveConns
				selectedEndpoint = ep
			}
		}
	}

	if selectedEndpoint == nil {
		return nil, ErrNoHealthyEndpoints
	}

	// 3. Dispatch & Latency Measurement
	elapsedNs := time.Since(start).Nanoseconds()
	if elapsedNs <= 0 {
		elapsedNs = 200 // Sub-microsecond nominal resolution
	}
	elapsedUs := float64(elapsedNs) / 1000.0

	// 4. Kernel Bypass Telemetry
	isKernelBypass := e.config.EnableKernelBypass && (e.driver.IsAvailable() || e.config.Mode == SteeringModeEBPFSockmap)
	activeMode := e.config.Mode
	if !isKernelBypass && activeMode != SteeringModeUserspace {
		activeMode = SteeringModeUserspace
	}

	// 5. Update Atomic Telemetry
	e.totalPackets.Add(1)
	e.totalBytes.Add(uint64(len(rawPacket)))
	if isKernelBypass {
		e.kernelBypassed.Add(1)
	} else {
		e.userspaceDispatched.Add(1)
	}
	if elapsedNs < 10000 { // Under 10 microseconds (< 10µs)
		e.sub10Microsec.Add(1)
	}
	e.cumulativeLatencyNs.Add(elapsedNs)

	// Update selected endpoint telemetry
	atomic.AddUint64(&selectedEndpoint.PacketsSteered, 1)
	atomic.AddUint64(&selectedEndpoint.BytesSteered, uint64(len(rawPacket)))

	decision := &SteeringDecision{
		TargetEndpoint:      selectedEndpoint,
		Mode:                activeMode,
		ParsedHeader:        parsedHeader,
		DispatchedViaKernel: isKernelBypass,
		DispatchLatencyNs:   elapsedNs,
		DispatchLatencyUs:   elapsedUs,
		MatchedRuleID:       matchedRuleID,
	}

	return decision, nil
}

// GetStats compiles real-time telemetry metrics and microsecond latency ratios.
func (e *PacketSteeringEngine) GetStats() SteeringStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	totalPkts := e.totalPackets.Load()
	totalBytes := e.totalBytes.Load()
	kernelPkts := e.kernelBypassed.Load()
	userspacePkts := e.userspaceDispatched.Load()
	sub10Pkts := e.sub10Microsec.Load()
	totalNs := e.cumulativeLatencyNs.Load()

	var avgLatencyUs float64
	var sub10Ratio float64

	if totalPkts > 0 {
		avgLatencyUs = float64(totalNs) / float64(totalPkts) / 1000.0
		sub10Ratio = (float64(sub10Pkts) / float64(totalPkts)) * 100.0
	}

	activeMode := e.config.Mode
	if !e.driver.IsAvailable() && activeMode == SteeringModeEBPFSockmap {
		activeMode = SteeringModeUserspace
	}

	return SteeringStats{
		TotalPackets:          totalPkts,
		TotalBytes:            totalBytes,
		KernelBypassedPackets: kernelPkts,
		UserspacePackets:      userspacePkts,
		Sub10MicrosecPackets:  sub10Pkts,
		AvgDispatchLatencyUs:  avgLatencyUs,
		Sub10MicrosecRatio:    sub10Ratio,
		ActiveEndpoints:       len(e.endpoints),
		ActiveRules:           len(e.rules),
		ActiveMode:            activeMode,
		EBPFAvailable:         e.driver.IsAvailable(),
	}
}

// ResetStats zeroes out steering performance counters.
func (e *PacketSteeringEngine) ResetStats() {
	e.totalPackets.Store(0)
	e.totalBytes.Store(0)
	e.kernelBypassed.Store(0)
	e.userspaceDispatched.Store(0)
	e.sub10Microsec.Store(0)
	e.cumulativeLatencyNs.Store(0)
}

// GenerateRandomHexCID creates a random hex Connection ID for synthetic tests.
func GenerateRandomHexCID(byteLen int) string {
	b := make([]byte, byteLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
