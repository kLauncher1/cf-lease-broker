package mesh

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// HashRing manages a consistent-hashing ring with virtual nodes for uniform token sharding.
type HashRing struct {
	mu                sync.RWMutex
	vnodesPerNode     int
	replicationFactor int
	ring              []VirtualNode
	nodes             map[string]PeerNode
}

// NewHashRing creates an initialized consistent hashing ring.
func NewHashRing(vnodesPerNode, replicationFactor int) *HashRing {
	if vnodesPerNode <= 0 {
		vnodesPerNode = 128
	}
	if replicationFactor <= 0 {
		replicationFactor = 3
	}
	return &HashRing{
		vnodesPerNode:     vnodesPerNode,
		replicationFactor: replicationFactor,
		ring:              make([]VirtualNode, 0),
		nodes:             make(map[string]PeerNode),
	}
}

// hashKey computes a 64-bit uniform hash using truncated SHA-256.
func hashKey(key string) uint64 {
	sum := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(sum[:8])
}

// AddNode registers a node and its virtual nodes onto the consistent hash ring.
func (r *HashRing) AddNode(node PeerNode) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If node exists, remove its old vnodes before re-adding
	if _, exists := r.nodes[node.ID]; exists {
		r.removeVnodesLocked(node.ID)
	}

	r.nodes[node.ID] = node

	// If node is dead or leaving, don't place vnodes in active ring
	if node.Status == NodeStatusDead || node.Status == NodeStatusLeaving {
		return
	}

	count := node.Vnodes
	if count <= 0 {
		count = r.vnodesPerNode
	}

	for i := 0; i < count; i++ {
		vnodeKey := fmt.Sprintf("%s#vn-%d", node.ID, i)
		vnode := VirtualNode{
			Hash:     hashKey(vnodeKey),
			NodeID:   node.ID,
			VnodeIdx: i,
		}
		r.ring = append(r.ring, vnode)
	}

	sort.Slice(r.ring, func(i, j int) bool {
		return r.ring[i].Hash < r.ring[j].Hash
	})
}

// RemoveNode unregisters a node and purges all its virtual nodes from the ring.
func (r *HashRing) RemoveNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.nodes, nodeID)
	r.removeVnodesLocked(nodeID)
}

// UpdateNodeStatus updates a node's operational status and adjusts ring membership.
func (r *HashRing) UpdateNodeStatus(nodeID string, status NodeStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return
	}

	prevStatus := node.Status
	node.Status = status
	r.nodes[nodeID] = node

	// If transitioned to dead/leaving, remove from ring
	if (status == NodeStatusDead || status == NodeStatusLeaving) &&
		(prevStatus != NodeStatusDead && prevStatus != NodeStatusLeaving) {
		r.removeVnodesLocked(nodeID)
	} else if (prevStatus == NodeStatusDead || prevStatus == NodeStatusLeaving) &&
		(status == NodeStatusActive || status == NodeStatusSuspect) {
		// Resurrected to ring
		count := node.Vnodes
		if count <= 0 {
			count = r.vnodesPerNode
		}
		for i := 0; i < count; i++ {
			vnodeKey := fmt.Sprintf("%s#vn-%d", node.ID, i)
			vnode := VirtualNode{
				Hash:     hashKey(vnodeKey),
				NodeID:   node.ID,
				VnodeIdx: i,
			}
			r.ring = append(r.ring, vnode)
		}
		sort.Slice(r.ring, func(i, j int) bool {
			return r.ring[i].Hash < r.ring[j].Hash
		})
	}
}

func (r *HashRing) removeVnodesLocked(nodeID string) {
	newRing := make([]VirtualNode, 0, len(r.ring))
	for _, vn := range r.ring {
		if vn.NodeID != nodeID {
			newRing = append(newRing, vn)
		}
	}
	r.ring = newRing
}

// LocateKey identifies the primary coordinator node and replicas for a given token/key.
func (r *HashRing) LocateKey(key string) (*ShardLocation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.ring) == 0 {
		return nil, ErrEmptyRing
	}

	kHash := hashKey(key)

	// Binary search to find the first virtual node with Hash >= kHash
	idx := sort.Search(len(r.ring), func(i int) bool {
		return r.ring[i].Hash >= kHash
	})

	// Wrap around if kHash is greater than all vnodes
	if idx == len(r.ring) {
		idx = 0
	}

	primaryID := r.ring[idx].NodeID
	primaryNode, exists := r.nodes[primaryID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	// Collect distinct replica physical nodes
	replicas := make([]PeerNode, 0, r.replicationFactor-1)
	visited := map[string]bool{primaryID: true}

	ringLen := len(r.ring)
	for i := 1; i < ringLen && len(replicas) < r.replicationFactor-1; i++ {
		replicaIdx := (idx + i) % ringLen
		repID := r.ring[replicaIdx].NodeID
		if !visited[repID] {
			visited[repID] = true
			if repNode, ok := r.nodes[repID]; ok && repNode.Status != NodeStatusDead {
				replicas = append(replicas, repNode)
			}
		}
	}

	return &ShardLocation{
		Key:          key,
		KeyHash:      kHash,
		PrimaryNode:  primaryNode,
		ReplicaNodes: replicas,
		TargetRegion: primaryNode.Region,
	}, nil
}

// GetShardDistribution calculates the percentage of the ring owned by each physical node.
func (r *HashRing) GetShardDistribution() map[string]float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dist := make(map[string]float64)
	total := len(r.ring)
	if total == 0 {
		return dist
	}

	counts := make(map[string]int)
	for _, vn := range r.ring {
		counts[vn.NodeID]++
	}

	for id, count := range counts {
		dist[id] = (float64(count) / float64(total)) * 100.0
	}
	return dist
}

// GetNode returns a snapshot of a registered peer node.
func (r *HashRing) GetNode(nodeID string) (PeerNode, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	node, exists := r.nodes[nodeID]
	return node, exists
}

// ListNodes returns a slice of all registered peer nodes.
func (r *HashRing) ListNodes() []PeerNode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]PeerNode, 0, len(r.nodes))
	for _, node := range r.nodes {
		res = append(res, node)
	}
	return res
}

// VnodeCount returns the total number of active virtual nodes currently on the ring.
func (r *HashRing) VnodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.ring)
}

// ActiveNodeCount returns the number of healthy/active physical nodes.
func (r *HashRing) ActiveNodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active := 0
	for _, n := range r.nodes {
		if n.Status == NodeStatusActive || n.Status == NodeStatusSuspect {
			active++
		}
	}
	return active
}
