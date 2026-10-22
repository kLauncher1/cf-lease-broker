package mesh

import (
	"errors"
	"time"
)

// Common mesh errors.
var (
	ErrNodeNotFound       = errors.New("peer node not found in mesh")
	ErrNodeAlreadyExists  = errors.New("peer node ID already exists")
	ErrInvalidSignature   = errors.New("gossip packet signature verification failed")
	ErrEmptyRing          = errors.New("consistent hash ring contains no active nodes")
	ErrPayloadTooLarge    = errors.New("mesh request payload exceeds size limit")
	ErrInvalidNodeAddress = errors.New("invalid or empty node address")
)

// NodeStatus defines the operational health state of a broker node in the mesh.
type NodeStatus string

const (
	NodeStatusActive  NodeStatus = "active"
	NodeStatusSuspect NodeStatus = "suspect"
	NodeStatusDead    NodeStatus = "dead"
	NodeStatusLeaving NodeStatus = "leaving"
)

// PeerNode represents an individual broker instance in the cross-region edge mesh.
type PeerNode struct {
	ID            string            `json:"id"`
	Address       string            `json:"address"`
	Region        string            `json:"region"`
	Vnodes        int               `json:"vnodes"`
	Status        NodeStatus        `json:"status"`
	HeartbeatSeq  uint64            `json:"heartbeat_seq"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	LoadWeight    float64           `json:"load_weight"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// GossipPacket carries periodic anti-entropy mesh state updates between broker nodes.
type GossipPacket struct {
	SenderID     string     `json:"sender_id"`
	SenderRegion string     `json:"sender_region"`
	SenderSeq    uint64     `json:"sender_seq"`
	Timestamp    int64      `json:"timestamp"`
	Nodes        []PeerNode `json:"nodes"`
	Signature    string     `json:"signature"`
}

// GossipAck is returned upon successful ingestion of a peer's gossip packet.
type GossipAck struct {
	ReceiverID string `json:"receiver_id"`
	NodesKnown int    `json:"nodes_known"`
	Status     string `json:"status"`
}

// VirtualNode maps a consistent hash point on the 64-bit ring to a physical node.
type VirtualNode struct {
	Hash     uint64 `json:"hash"`
	NodeID   string `json:"node_id"`
	VnodeIdx int    `json:"vnode_idx"`
}

// ShardLocation specifies the primary and replica node ownership for a given key.
type ShardLocation struct {
	Key          string     `json:"key"`
	KeyHash      uint64     `json:"key_hash"`
	PrimaryNode  PeerNode   `json:"primary_node"`
	ReplicaNodes []PeerNode `json:"replica_nodes"`
	TargetRegion string     `json:"target_region"`
	IsLocalOwner bool       `json:"is_local_owner"`
}

// AffinityDecision records edge connection affinity and routing evaluation.
type AffinityDecision struct {
	Key          string   `json:"key"`
	ClientRegion string   `json:"client_region"`
	SelectedNode PeerNode `json:"selected_node"`
	IsLocalServe bool     `json:"is_local_serve"`
	DistanceKm   float64  `json:"distance_km"`
	Reason       string   `json:"reason"`
}

// MeshConfig holds configuration parameters for the local mesh node.
type MeshConfig struct {
	LocalNodeID       string
	LocalRegion       string
	LocalAddress      string
	VnodesPerNode     int
	ReplicationFactor int
	GossipInterval    time.Duration
	HeartbeatTimeout  time.Duration
	SuspectTimeout    time.Duration
	SecretKey         []byte
}

// DefaultMeshConfig provides production defaults for edge mesh clustering.
func DefaultMeshConfig(localID, region, address string) MeshConfig {
	if localID == "" {
		localID = "broker-edge-local"
	}
	if region == "" {
		region = "iad"
	}
	if address == "" {
		address = "127.0.0.1:9081"
	}
	return MeshConfig{
		LocalNodeID:       localID,
		LocalRegion:       region,
		LocalAddress:      address,
		VnodesPerNode:     128,
		ReplicationFactor: 3,
		GossipInterval:    1 * time.Second,
		HeartbeatTimeout:  5 * time.Second,
		SuspectTimeout:    10 * time.Second,
		SecretKey:         []byte("cf-lease-mesh-default-secret-key"),
	}
}

// MeshStats aggregates real-time health and telemetry metrics for the mesh engine.
type MeshStats struct {
	LocalNodeID          string             `json:"local_node_id"`
	LocalRegion          string             `json:"local_region"`
	TotalNodes           int                `json:"total_nodes"`
	ActiveNodes          int                `json:"active_nodes"`
	SuspectNodes         int                `json:"suspect_nodes"`
	DeadNodes            int                `json:"dead_nodes"`
	RingVnodes           int                `json:"ring_vnodes"`
	ConvergenceScore     float64            `json:"convergence_score"`
	PacketsSent          uint64             `json:"packets_sent"`
	PacketsReceived      uint64             `json:"packets_received"`
	SignatureFailures    uint64             `json:"signature_failures"`
	ShardsQueried        uint64             `json:"shards_queried"`
	AffinityLocalHits    uint64             `json:"affinity_local_hits"`
	AffinityRemoteSteers uint64             `json:"affinity_remote_steers"`
	ShardDistribution    map[string]float64 `json:"shard_distribution,omitempty"`
}
