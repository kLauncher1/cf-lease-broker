package telemetry

import (
	"sort"
	"sync"
	"time"
)

const ewmaAlpha = 0.2 // Smoothing factor for moving averages

type endpointState struct {
	endpointID   string
	ewmaRTT      float64
	ewmaRetrans  float64
	ewmaCWND     float64
	minRcvWnd    uint32
	lastUpdate   time.Time
	sampleCount  int
}

// TelemetryCollector aggregates and tracks socket-level telemetry across endpoints.
type TelemetryCollector struct {
	mu            sync.RWMutex
	sockets       map[string]*SocketMetrics // sockID -> latest metrics
	endpointMap   map[string]*endpointState // endpointID -> rolling stats
	endpointSocks map[string]map[string]bool // endpointID -> set of sockIDs
}

// NewTelemetryCollector initializes an empty socket telemetry collector.
func NewTelemetryCollector() *TelemetryCollector {
	return &TelemetryCollector{
		sockets:       make(map[string]*SocketMetrics),
		endpointMap:   make(map[string]*endpointState),
		endpointSocks: make(map[string]map[string]bool),
	}
}

// Ingest records new socket metrics and updates rolling statistical baselines.
func (c *TelemetryCollector) Ingest(metrics SocketMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if metrics.Timestamp.IsZero() {
		metrics.Timestamp = time.Now()
	}

	// Calculate retransmission rate if not provided
	if metrics.RetransRate == 0 && metrics.PacketsOut > 0 && metrics.TotalRetrans > 0 {
		metrics.RetransRate = (float64(metrics.TotalRetrans) / float64(metrics.PacketsOut+metrics.TotalRetrans)) * 100.0
	}

	copied := metrics
	c.sockets[metrics.SocketID] = &copied

	// Associate socket with endpoint
	epID := metrics.EndpointID
	if epID == "" {
		epID = "default"
	}

	if _, exists := c.endpointSocks[epID]; !exists {
		c.endpointSocks[epID] = make(map[string]bool)
	}
	c.endpointSocks[epID][metrics.SocketID] = true

	// Update endpoint EWMA statistics
	st, exists := c.endpointMap[epID]
	if !exists {
		st = &endpointState{
			endpointID:  epID,
			ewmaRTT:     float64(metrics.RTTUs),
			ewmaRetrans: metrics.RetransRate,
			ewmaCWND:    float64(metrics.CWND),
			minRcvWnd:   metrics.RcvWnd,
			lastUpdate:  metrics.Timestamp,
			sampleCount: 1,
		}
		c.endpointMap[epID] = st
	} else {
		st.ewmaRTT = (ewmaAlpha * float64(metrics.RTTUs)) + ((1.0 - ewmaAlpha) * st.ewmaRTT)
		st.ewmaRetrans = (ewmaAlpha * metrics.RetransRate) + ((1.0 - ewmaAlpha) * st.ewmaRetrans)
		st.ewmaCWND = (ewmaAlpha * float64(metrics.CWND)) + ((1.0 - ewmaAlpha) * st.ewmaCWND)
		if metrics.RcvWnd < st.minRcvWnd || st.minRcvWnd == 0 {
			st.minRcvWnd = metrics.RcvWnd
		}
		st.lastUpdate = metrics.Timestamp
		st.sampleCount++
	}
}

// GetSocket returns the latest metrics for a given socket ID.
func (c *TelemetryCollector) GetSocket(sockID string) (*SocketMetrics, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	s, found := c.sockets[sockID]
	if !found {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// ListSockets returns a snapshot of all active sockets sorted by ID.
func (c *TelemetryCollector) ListSockets() []SocketMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]SocketMetrics, 0, len(c.sockets))
	for _, s := range c.sockets {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SocketID < out[j].SocketID
	})
	return out
}

// GetEndpointMetrics returns all active socket metrics associated with an endpoint.
func (c *TelemetryCollector) GetEndpointMetrics(endpointID string) []SocketMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	sockSet, found := c.endpointSocks[endpointID]
	if !found {
		return nil
	}

	out := make([]SocketMetrics, 0, len(sockSet))
	for sockID := range sockSet {
		if s, ok := c.sockets[sockID]; ok {
			out = append(out, *s)
		}
	}
	return out
}

// GetEndpointSummary aggregates rolling stats and active socket count for an endpoint.
func (c *TelemetryCollector) GetEndpointSummary(endpointID string) (EndpointSummary, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	st, found := c.endpointMap[endpointID]
	if !found {
		return EndpointSummary{}, false
	}

	activeCount := len(c.endpointSocks[endpointID])
	return EndpointSummary{
		EndpointID:     st.endpointID,
		ActiveSockets:  activeCount,
		AverageRTTUs:   st.ewmaRTT,
		AverageRetrans: st.ewmaRetrans,
		MinRcvWnd:      st.minRcvWnd,
	}, true
}

// ListEndpointSummaries returns summary statistics across all known endpoints.
func (c *TelemetryCollector) ListEndpointSummaries() []EndpointSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]EndpointSummary, 0, len(c.endpointMap))
	for epID, st := range c.endpointMap {
		activeCount := len(c.endpointSocks[epID])
		out = append(out, EndpointSummary{
			EndpointID:     st.endpointID,
			ActiveSockets:  activeCount,
			AverageRTTUs:   st.ewmaRTT,
			AverageRetrans: st.ewmaRetrans,
			MinRcvWnd:      st.minRcvWnd,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].EndpointID < out[j].EndpointID
	})
	return out
}

// PruneStaleSockets removes sockets that have not received telemetry updates within maxAge.
func (c *TelemetryCollector) PruneStaleSockets(maxAge time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	pruned := 0

	for sockID, s := range c.sockets {
		if now.Sub(s.Timestamp) > maxAge {
			delete(c.sockets, sockID)
			if sockSet, ok := c.endpointSocks[s.EndpointID]; ok {
				delete(sockSet, sockID)
				if len(sockSet) == 0 {
					delete(c.endpointSocks, s.EndpointID)
					delete(c.endpointMap, s.EndpointID)
				}
			}
			pruned++
		}
	}

	for epID, st := range c.endpointMap {
		if socks, ok := c.endpointSocks[epID]; !ok || len(socks) == 0 {
			if now.Sub(st.lastUpdate) > maxAge {
				delete(c.endpointMap, epID)
			}
		}
	}
	return pruned
}

// TotalSocketsCount returns the current count of tracked sockets.
func (c *TelemetryCollector) TotalSocketsCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.sockets)
}

// Reset clears all tracked sockets and endpoint baselines.
func (c *TelemetryCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sockets = make(map[string]*SocketMetrics)
	c.endpointMap = make(map[string]*endpointState)
	c.endpointSocks = make(map[string]map[string]bool)
}
