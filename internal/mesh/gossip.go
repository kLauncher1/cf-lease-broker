package mesh

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// GossipProtocol manages anti-entropy gossip exchange and node failure detection.
type GossipProtocol struct {
	mu                sync.RWMutex
	localID           string
	localRegion       string
	secretKey         []byte
	heartbeatTimeout  time.Duration
	suspectTimeout    time.Duration
	ring              *HashRing
	sequenceCounter   uint64
	packetsSent       uint64
	packetsReceived   uint64
	signatureFailures uint64
}

// NewGossipProtocol constructs a new gossip protocol instance.
func NewGossipProtocol(localID, localRegion string, secretKey []byte, hbTimeout, suspectTimeout time.Duration, ring *HashRing) *GossipProtocol {
	if hbTimeout <= 0 {
		hbTimeout = 5 * time.Second
	}
	if suspectTimeout <= 0 {
		suspectTimeout = 10 * time.Second
	}
	if len(secretKey) == 0 {
		secretKey = []byte("cf-lease-mesh-default-secret-key")
	}
	return &GossipProtocol{
		localID:          localID,
		localRegion:      localRegion,
		secretKey:        secretKey,
		heartbeatTimeout: hbTimeout,
		suspectTimeout:   suspectTimeout,
		ring:             ring,
	}
}

// ComputeSignature calculates the HMAC-SHA256 signature for a gossip payload.
func (g *GossipProtocol) ComputeSignature(senderID string, seq uint64, timestamp int64, nodeCount int) string {
	mac := hmac.New(sha256.New, g.secretKey)
	payload := fmt.Sprintf("%s:%d:%d:%d", senderID, seq, timestamp, nodeCount)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature validates the HMAC-SHA256 signature using constant-time comparison.
func (g *GossipProtocol) VerifySignature(packet *GossipPacket) bool {
	expected := g.ComputeSignature(packet.SenderID, packet.SenderSeq, packet.Timestamp, len(packet.Nodes))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(packet.Signature)) == 1
}

// CreateGossipPacket builds a signed gossip packet containing current cluster membership.
func (g *GossipProtocol) CreateGossipPacket() GossipPacket {
	g.mu.Lock()
	seq := atomic.AddUint64(&g.sequenceCounter, 1)
	g.mu.Unlock()

	now := time.Now()
	nodes := g.ring.ListNodes()

	// Ensure local node heartbeat is refreshed
	for i := range nodes {
		if nodes[i].ID == g.localID {
			nodes[i].HeartbeatSeq = seq
			nodes[i].LastHeartbeat = now
			nodes[i].Status = NodeStatusActive
			g.ring.AddNode(nodes[i])
			break
		}
	}

	sig := g.ComputeSignature(g.localID, seq, now.Unix(), len(nodes))
	atomic.AddUint64(&g.packetsSent, 1)

	return GossipPacket{
		SenderID:     g.localID,
		SenderRegion: g.localRegion,
		SenderSeq:    seq,
		Timestamp:    now.Unix(),
		Nodes:        nodes,
		Signature:    sig,
	}
}

// ProcessPacket validates and ingests a peer's incoming gossip packet into the ring.
func (g *GossipProtocol) ProcessPacket(packet GossipPacket) (*GossipAck, error) {
	atomic.AddUint64(&g.packetsReceived, 1)

	if !g.VerifySignature(&packet) {
		atomic.AddUint64(&g.signatureFailures, 1)
		return nil, ErrInvalidSignature
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()

	for _, peer := range packet.Nodes {
		// Never overwrite local node status from an external peer
		if peer.ID == g.localID {
			continue
		}

		existing, exists := g.ring.GetNode(peer.ID)
		if !exists {
			// Brand new node discovered via gossip
			peer.LastHeartbeat = now
			g.ring.AddNode(peer)
			continue
		}

		// Update if incoming peer state has a higher heartbeat sequence
		if peer.HeartbeatSeq > existing.HeartbeatSeq {
			existing.HeartbeatSeq = peer.HeartbeatSeq
			existing.LastHeartbeat = now
			existing.Address = peer.Address
			existing.Region = peer.Region
			existing.Vnodes = peer.Vnodes
			existing.LoadWeight = peer.LoadWeight

			// If peer reported dead or suspect, propagate that unless recently seen
			if peer.Status == NodeStatusDead {
				existing.Status = NodeStatusDead
				g.ring.UpdateNodeStatus(peer.ID, NodeStatusDead)
			} else {
				// Resurrect if previously dead
				if existing.Status == NodeStatusDead {
					existing.Status = NodeStatusActive
				}
				g.ring.AddNode(existing)
			}
		}
	}

	return &GossipAck{
		ReceiverID: g.localID,
		NodesKnown: len(g.ring.ListNodes()),
		Status:     "converged",
	}, nil
}

// SweepFailures scans the membership table and transitions unresponsive nodes to Suspect or Dead.
func (g *GossipProtocol) SweepFailures() (suspectCount, deadCount int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	nodes := g.ring.ListNodes()

	for _, n := range nodes {
		if n.ID == g.localID {
			continue
		}

		elapsed := now.Sub(n.LastHeartbeat)
		if elapsed > g.suspectTimeout && n.Status != NodeStatusDead {
			g.ring.UpdateNodeStatus(n.ID, NodeStatusDead)
			deadCount++
		} else if elapsed > g.heartbeatTimeout && n.Status == NodeStatusActive {
			g.ring.UpdateNodeStatus(n.ID, NodeStatusSuspect)
			suspectCount++
		}
	}

	return suspectCount, deadCount
}

// ConvergenceScore returns an estimated metric (0.0 to 1.0) indicating cluster health.
func (g *GossipProtocol) ConvergenceScore() float64 {
	nodes := g.ring.ListNodes()
	if len(nodes) == 0 {
		return 1.0
	}
	active := 0
	for _, n := range nodes {
		if n.Status == NodeStatusActive {
			active++
		}
	}
	return float64(active) / float64(len(nodes))
}

// Telemetry returns packet transfer statistics.
func (g *GossipProtocol) Telemetry() (sent, received, sigFailures uint64) {
	return atomic.LoadUint64(&g.packetsSent),
		atomic.LoadUint64(&g.packetsReceived),
		atomic.LoadUint64(&g.signatureFailures)
}
