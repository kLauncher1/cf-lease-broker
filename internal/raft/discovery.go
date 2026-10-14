package raft

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// PeerDiscovery handles dynamic cluster peer discovery via DNS SRV records with static fallback.
type PeerDiscovery struct {
	mu              sync.RWMutex
	selfID          string
	domain          string
	staticPeers     map[string]string
	discoveredPeers map[string]*PeerStatus
}

// NewPeerDiscovery initializes the discovery engine.
func NewPeerDiscovery(selfID string, domain string, staticPeers map[string]string) *PeerDiscovery {
	pd := &PeerDiscovery{
		selfID:          selfID,
		domain:          domain,
		staticPeers:     make(map[string]string),
		discoveredPeers: make(map[string]*PeerStatus),
	}

	for id, addr := range staticPeers {
		if id != selfID {
			cleanAddr := normalizeAddr(addr)
			pd.staticPeers[id] = cleanAddr
			pd.discoveredPeers[id] = &PeerStatus{
				ID:            id,
				Address:       cleanAddr,
				Active:        true,
				LastHeartbeat: time.Now().UTC(),
				NextIndex:     1,
			}
		}
	}

	return pd
}

// DiscoverPeers queries DNS SRV records (e.g. _raft._tcp.<domain>) or returns static peer mappings.
func (pd *PeerDiscovery) DiscoverPeers() map[string]string {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	result := make(map[string]string)
	for id, addr := range pd.staticPeers {
		result[id] = addr
	}

	if pd.domain == "" {
		return result
	}

	// Attempt DNS SRV resolution
	_, addrs, err := net.LookupSRV("raft", "tcp", pd.domain)
	if err == nil && len(addrs) > 0 {
		for _, srv := range addrs {
			target := strings.TrimSuffix(srv.Target, ".")
			peerAddr := fmt.Sprintf("http://%s:%d", target, srv.Port)
			peerID := fmt.Sprintf("node-%s-%d", strings.ReplaceAll(target, ".", "-"), srv.Port)

			if peerID == pd.selfID {
				continue
			}

			result[peerID] = peerAddr
			if _, exists := pd.discoveredPeers[peerID]; !exists {
				pd.discoveredPeers[peerID] = &PeerStatus{
					ID:            peerID,
					Address:       peerAddr,
					Active:        true,
					LastHeartbeat: time.Now().UTC(),
					NextIndex:     1,
				}
			}
		}
	}

	return result
}

// RecordHeartbeat logs peer responsiveness and latency.
func (pd *PeerDiscovery) RecordHeartbeat(peerID string, latency time.Duration, success bool) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	peer, exists := pd.discoveredPeers[peerID]
	if !exists {
		return
	}

	peer.Active = success
	peer.LastHeartbeat = time.Now().UTC()
	if success {
		peer.LatencyMs = float64(latency.Microseconds()) / 1000.0
	}
}

// UpdateIndices updates nextIndex and matchIndex for a peer.
func (pd *PeerDiscovery) UpdateIndices(peerID string, nextIdx, matchIdx uint64) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if peer, exists := pd.discoveredPeers[peerID]; exists {
		if nextIdx > 0 {
			peer.NextIndex = nextIdx
		}
		if matchIdx > 0 {
			peer.MatchIndex = matchIdx
		}
	}
}

// GetPeerStatusList returns a list of peer status snapshots.
func (pd *PeerDiscovery) GetPeerStatusList() []PeerStatus {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	list := make([]PeerStatus, 0, len(pd.discoveredPeers))
	for _, p := range pd.discoveredPeers {
		list = append(list, *p)
	}
	return list
}

// RegisterPeer manually adds or updates a peer in the discovery table.
func (pd *PeerDiscovery) RegisterPeer(id, address string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	cleanAddr := normalizeAddr(address)
	pd.staticPeers[id] = cleanAddr
	if _, exists := pd.discoveredPeers[id]; !exists {
		pd.discoveredPeers[id] = &PeerStatus{
			ID:            id,
			Address:       cleanAddr,
			Active:        true,
			LastHeartbeat: time.Now().UTC(),
			NextIndex:     1,
		}
	} else {
		pd.discoveredPeers[id].Address = cleanAddr
	}
}

func normalizeAddr(addr string) string {
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		return "http://" + addr
	}
	return addr
}
