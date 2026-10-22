package mesh

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MeshEngine coordinates the cross-region broker topology, consistent hash ring, and gossip state.
type MeshEngine struct {
	mu                   sync.RWMutex
	cfg                  MeshConfig
	ring                 *HashRing
	gossip               *GossipProtocol
	affinity             *AffinityEngine
	ctx                  context.Context
	cancel               context.CancelFunc
	running              bool
	shardsQueried        uint64
	affinityLocalHits    uint64
	affinityRemoteSteers uint64
}

// NewMeshEngine constructs and initializes a multi-region anycast edge mesh coordinator.
func NewMeshEngine(cfg MeshConfig) *MeshEngine {
	ring := NewHashRing(cfg.VnodesPerNode, cfg.ReplicationFactor)
	gossip := NewGossipProtocol(
		cfg.LocalNodeID,
		cfg.LocalRegion,
		cfg.SecretKey,
		cfg.HeartbeatTimeout,
		cfg.SuspectTimeout,
		ring,
	)
	affinity := NewAffinityEngine(cfg.LocalNodeID, cfg.LocalRegion)

	ctx, cancel := context.WithCancel(context.Background())

	engine := &MeshEngine{
		cfg:      cfg,
		ring:     ring,
		gossip:   gossip,
		affinity: affinity,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Register the local node into the ring by default
	localNode := PeerNode{
		ID:            cfg.LocalNodeID,
		Address:       cfg.LocalAddress,
		Region:        cfg.LocalRegion,
		Vnodes:        cfg.VnodesPerNode,
		Status:        NodeStatusActive,
		HeartbeatSeq:  1,
		LastHeartbeat: time.Now(),
		LoadWeight:    1.0,
	}
	ring.AddNode(localNode)

	return engine
}

// Start launches the background failure detection and anti-entropy gossip loops.
func (m *MeshEngine) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	go m.runMaintenanceLoop()
}

// Stop cleanly terminates background mesh goroutines.
func (m *MeshEngine) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.cancel()
	m.mu.Unlock()
}

func (m *MeshEngine) runMaintenanceLoop() {
	ticker := time.NewTicker(m.cfg.GossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			// Sweep for suspect and dead nodes
			m.gossip.SweepFailures()
		}
	}
}

// RegisterPeer adds or updates a remote broker node in the mesh topology.
func (m *MeshEngine) RegisterPeer(node PeerNode) error {
	node.ID = strings.TrimSpace(node.ID)
	if node.ID == "" {
		return ErrNodeNotFound
	}
	if node.Address == "" {
		return ErrInvalidNodeAddress
	}
	if node.Vnodes <= 0 {
		node.Vnodes = m.cfg.VnodesPerNode
	}
	if node.Status == "" {
		node.Status = NodeStatusActive
	}
	if node.LastHeartbeat.IsZero() {
		node.LastHeartbeat = time.Now()
	}

	m.ring.AddNode(node)
	return nil
}

// DeregisterPeer removes a peer node from the mesh ring.
func (m *MeshEngine) DeregisterPeer(nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrNodeNotFound
	}
	m.ring.RemoveNode(nodeID)
	return nil
}

// LocateShard maps a token or resource key to its primary and replica shard owners.
func (m *MeshEngine) LocateShard(key string) (*ShardLocation, error) {
	atomic.AddUint64(&m.shardsQueried, 1)

	loc, err := m.ring.LocateKey(key)
	if err != nil {
		return nil, err
	}

	loc.IsLocalOwner = loc.PrimaryNode.ID == m.cfg.LocalNodeID
	return loc, nil
}

// ResolveAffinity calculates anycast edge affinity and steering recommendations.
func (m *MeshEngine) ResolveAffinity(clientRegion, key string) (*AffinityDecision, error) {
	loc, err := m.LocateShard(key)
	if err != nil {
		return nil, err
	}

	decision := m.affinity.ResolveAffinity(clientRegion, loc)
	if decision.IsLocalServe {
		atomic.AddUint64(&m.affinityLocalHits, 1)
	} else {
		atomic.AddUint64(&m.affinityRemoteSteers, 1)
	}

	return decision, nil
}

// ProcessIncomingGossip validates and merges an anti-entropy packet from a peer.
func (m *MeshEngine) ProcessIncomingGossip(packet GossipPacket) (*GossipAck, error) {
	return m.gossip.ProcessPacket(packet)
}

// ExportGossipPacket creates an authenticated gossip payload to broadcast to peers.
func (m *MeshEngine) ExportGossipPacket() GossipPacket {
	return m.gossip.CreateGossipPacket()
}

// ListNodes returns all nodes currently known to the mesh.
func (m *MeshEngine) ListNodes() []PeerNode {
	return m.ring.ListNodes()
}

// GetNode returns details for a specific node ID.
func (m *MeshEngine) GetNode(nodeID string) (PeerNode, bool) {
	return m.ring.GetNode(nodeID)
}

// GetRingDistribution returns shard percentages for all active nodes.
func (m *MeshEngine) GetRingDistribution() map[string]float64 {
	return m.ring.GetShardDistribution()
}

// GetStats compiles real-time telemetry metrics across mesh components.
func (m *MeshEngine) GetStats() MeshStats {
	nodes := m.ring.ListNodes()
	active := 0
	suspect := 0
	dead := 0

	for _, n := range nodes {
		switch n.Status {
		case NodeStatusActive:
			active++
		case NodeStatusSuspect:
			suspect++
		case NodeStatusDead:
			dead++
		}
	}

	sent, recv, sigFails := m.gossip.Telemetry()

	return MeshStats{
		LocalNodeID:          m.cfg.LocalNodeID,
		LocalRegion:          m.cfg.LocalRegion,
		TotalNodes:           len(nodes),
		ActiveNodes:          active,
		SuspectNodes:         suspect,
		DeadNodes:            dead,
		RingVnodes:           m.ring.VnodeCount(),
		ConvergenceScore:     m.gossip.ConvergenceScore(),
		PacketsSent:          sent,
		PacketsReceived:      recv,
		SignatureFailures:    sigFails,
		ShardsQueried:        atomic.LoadUint64(&m.shardsQueried),
		AffinityLocalHits:    atomic.LoadUint64(&m.affinityLocalHits),
		AffinityRemoteSteers: atomic.LoadUint64(&m.affinityRemoteSteers),
		ShardDistribution:    m.ring.GetShardDistribution(),
	}
}
