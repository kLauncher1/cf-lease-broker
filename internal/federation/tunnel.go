package federation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// TunnelManager coordinates secure point-to-point overlay WAN tunnels across clusters.
type TunnelManager struct {
	mu             sync.RWMutex
	tunnels        map[string]*WANTunnel
	localClusterID string
}

// NewTunnelManager creates a new WAN tunnel manager for the local cluster.
func NewTunnelManager(localClusterID string) *TunnelManager {
	return &TunnelManager{
		tunnels:        make(map[string]*WANTunnel),
		localClusterID: localClusterID,
	}
}

// RegisterTunnel registers an overlay WAN tunnel into the manager.
func (tm *TunnelManager) RegisterTunnel(tun *WANTunnel) error {
	if tun == nil {
		return fmt.Errorf("cannot register nil tunnel")
	}
	if tun.ID == "" {
		return fmt.Errorf("tunnel ID cannot be empty")
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.tunnels[tun.ID]; exists {
		return ErrTunnelAlreadyExists
	}

	if tun.MTU <= 0 {
		tun.MTU = 1420 // WireGuard default MTU
	}
	if tun.KeepaliveSec <= 0 {
		tun.KeepaliveSec = 25
	}
	if tun.EstablishedAt.IsZero() {
		tun.EstablishedAt = time.Now()
	}
	if tun.LastHandshake.IsZero() {
		tun.LastHandshake = time.Now()
	}

	// Deep copy to prevent race conditions
	tCopy := *tun
	tm.tunnels[tun.ID] = &tCopy
	return nil
}

// DeregisterTunnel removes a tunnel from the manager by ID.
func (tm *TunnelManager) DeregisterTunnel(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.tunnels[id]; !exists {
		return ErrTunnelNotFound
	}
	delete(tm.tunnels, id)
	return nil
}

// GetTunnel retrieves a tunnel by its unique ID.
func (tm *TunnelManager) GetTunnel(id string) (*WANTunnel, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tun, exists := tm.tunnels[id]
	if !exists {
		return nil, false
	}
	tCopy := *tun
	return &tCopy, true
}

// ListTunnels returns all registered WAN tunnels sorted deterministically by ID.
func (tm *TunnelManager) ListTunnels() []*WANTunnel {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make([]*WANTunnel, 0, len(tm.tunnels))
	for _, tun := range tm.tunnels {
		tCopy := *tun
		result = append(result, &tCopy)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// FindTunnelByRemoteCluster searches for an active tunnel targeting a specific remote cluster ID.
func (tm *TunnelManager) FindTunnelByRemoteCluster(clusterID string) (*WANTunnel, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, tun := range tm.tunnels {
		if tun.RemoteClusterID == clusterID && tun.Status == TunnelStatusConnected {
			tCopy := *tun
			return &tCopy, true
		}
	}

	// Fallback to any tunnel to that cluster even if degraded
	for _, tun := range tm.tunnels {
		if tun.RemoteClusterID == clusterID && tun.Status != TunnelStatusClosed {
			tCopy := *tun
			return &tCopy, true
		}
	}

	return nil, false
}

// RecordTraffic increments throughput counters for a specific tunnel.
func (tm *TunnelManager) RecordTraffic(tunnelID string, bytesSent, bytesReceived int64, pktsSent, pktsReceived int64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tun, exists := tm.tunnels[tunnelID]
	if !exists {
		return ErrTunnelNotFound
	}

	tun.BytesSent += bytesSent
	tun.BytesReceived += bytesReceived
	tun.PacketsSent += pktsSent
	tun.PacketsReceived += pktsReceived
	return nil
}

// SimulateProbe updates latency, jitter, and packet loss metrics for a tunnel.
func (tm *TunnelManager) SimulateProbe(tunnelID string, latencyMs, lossPct, jitterMs float64) (*WANTunnel, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tun, exists := tm.tunnels[tunnelID]
	if !exists {
		return nil, ErrTunnelNotFound
	}

	tun.LatencyMs = latencyMs
	tun.PacketLossPct = lossPct
	tun.JitterMs = jitterMs
	tun.LastHandshake = time.Now()

	if lossPct > 20.0 || latencyMs > 350.0 {
		tun.Status = TunnelStatusDegraded
	} else if tun.Status == TunnelStatusDegraded {
		tun.Status = TunnelStatusConnected
	}

	tCopy := *tun
	return &tCopy, nil
}

// UpdateTunnelStatus sets the operational status of a tunnel.
func (tm *TunnelManager) UpdateTunnelStatus(tunnelID string, status TunnelStatus) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tun, exists := tm.tunnels[tunnelID]
	if !exists {
		return ErrTunnelNotFound
	}

	tun.Status = status
	if status == TunnelStatusConnected {
		tun.LastHandshake = time.Now()
	}
	return nil
}

// PerformHandshake simulates a cryptographic key exchange (e.g. Noise IK pattern) on the tunnel.
func (tm *TunnelManager) PerformHandshake(tunnelID string, peerPubKey string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tun, exists := tm.tunnels[tunnelID]
	if !exists {
		return ErrTunnelNotFound
	}

	if peerPubKey != "" {
		tun.PeerPublicKey = peerPubKey
	} else if tun.PeerPublicKey == "" {
		var raw [32]byte
		_, _ = rand.Read(raw[:])
		tun.PeerPublicKey = hex.EncodeToString(raw[:])
	}

	tun.Status = TunnelStatusConnected
	tun.LastHandshake = time.Now()
	return nil
}

// CalculateAggregateMetrics computes aggregate network performance across all tunnels.
func (tm *TunnelManager) CalculateAggregateMetrics() (avgLatency float64, totalBytes int64, connectedCount int) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if len(tm.tunnels) == 0 {
		return 0, 0, 0
	}

	var sumLatency float64
	var activeLatencyCount int

	for _, tun := range tm.tunnels {
		totalBytes += (tun.BytesSent + tun.BytesReceived)
		if tun.Status == TunnelStatusConnected {
			connectedCount++
			if tun.LatencyMs > 0 {
				sumLatency += tun.LatencyMs
				activeLatencyCount++
			}
		}
	}

	if activeLatencyCount > 0 {
		avgLatency = sumLatency / float64(activeLatencyCount)
	}
	return avgLatency, totalBytes, connectedCount
}
