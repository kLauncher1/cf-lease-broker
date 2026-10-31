package replication

import (
	"errors"
	"time"
)

// Common errors returned by the geo-replication engine.
var (
	ErrKeyNotFound           = errors.New("key not found in replication store")
	ErrStaleReadViolation    = errors.New("monotonic read violation: observed older clock than client session")
	ErrReplicationSyncFailed = errors.New("replication packet failed integrity or signature verification")
	ErrInvalidCRDTOperation  = errors.New("invalid CRDT operation specified")
	ErrPeerAlreadyRegistered = errors.New("peer node already registered in replication mesh")
	ErrPeerNotFound          = errors.New("peer node not found in replication mesh")
)

// ClockRelation describes the partial ordering between two vector clocks.
type ClockRelation string

const (
	ClockEqual      ClockRelation = "equal"
	ClockBefore     ClockRelation = "before"
	ClockAfter      ClockRelation = "after"
	ClockConcurrent ClockRelation = "concurrent"
)

// ConsistencyLevel specifies the isolation and freshness guarantee requested.
type ConsistencyLevel string

const (
	ConsistencyEventual       ConsistencyLevel = "eventual"
	ConsistencyReadYourWrites ConsistencyLevel = "read_your_writes"
	ConsistencyMonotonicRead  ConsistencyLevel = "monotonic_read"
	ConsistencyStrongQuorum   ConsistencyLevel = "strong_quorum"
)

// CRDTOpType defines the type of CRDT mutation.
type CRDTOpType string

const (
	CRDTCounterDelta CRDTOpType = "counter_delta"
	CRDTSetAdd       CRDTOpType = "set_add"
	CRDTSetRemove    CRDTOpType = "set_remove"
)

// VectorClock represents a logical vector clock mapping NodeID to monotonic counter.
type VectorClock map[string]uint64

// ReplicationEntry represents a versioned KV entry stored in the geo-replication layer.
type ReplicationEntry struct {
	Namespace   string            `json:"namespace"`
	Key         string            `json:"key"`
	Value       []byte            `json:"value,omitempty"`
	VectorClock VectorClock       `json:"vector_clock"`
	Timestamp   time.Time         `json:"timestamp"`
	NodeID      string            `json:"node_id"`
	TenantID    string            `json:"tenant_id,omitempty"`
	Deleted     bool              `json:"deleted"`
	IsCRDT      bool              `json:"is_crdt,omitempty"`
	CRDTType    string            `json:"crdt_type,omitempty"`
	CRDTState   []byte            `json:"crdt_state,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ClientSessionContext holds client-side consistency tokens for monotonic read & RYW.
type ClientSessionContext struct {
	ClientID             string      `json:"client_id"`
	TenantID             string      `json:"tenant_id"`
	LastSeenVectorClock  VectorClock `json:"last_seen_vector_clock"`
	LastWriteTimestamp   time.Time   `json:"last_write_timestamp"`
	RequestedConsistency ConsistencyLevel `json:"requested_consistency"`
}

// PeerNode represents a participating broker replica in the geo-replication mesh.
type PeerNode struct {
	NodeID    string    `json:"node_id"`
	Region    string    `json:"region"`
	Endpoint  string    `json:"endpoint"`
	LastSeen  time.Time `json:"last_seen"`
	Healthy   bool      `json:"healthy"`
	LatencyMs float64   `json:"latency_ms"`
}

// ReplicationPacket carries a batch of mutations between broker nodes.
type ReplicationPacket struct {
	PacketID     string             `json:"packet_id"`
	OriginNodeID string             `json:"origin_node_id"`
	OriginRegion string             `json:"origin_region"`
	Epoch        int64              `json:"epoch"`
	Entries      []ReplicationEntry `json:"entries"`
	GeneratedAt  time.Time          `json:"generated_at"`
	SignatureHex string             `json:"signature_hex"`
}

// SyncAck acknowledges receipt and processing of a replication packet.
type SyncAck struct {
	PacketID        string      `json:"packet_id"`
	ProcessedCount  int         `json:"processed_count"`
	ConflictsSolved int         `json:"conflicts_solved"`
	LocalNodeID     string      `json:"local_node_id"`
	CurrentClock    VectorClock `json:"current_clock"`
	Timestamp       time.Time   `json:"timestamp"`
}

// ReplicationConfig defines engine configuration parameters.
type ReplicationConfig struct {
	LocalNodeID     string        `json:"local_node_id"`
	Region          string        `json:"region"`
	SyncInterval    time.Duration `json:"sync_interval"`
	MaxBatchSize    int           `json:"max_batch_size"`
	HMACSecret      string        `json:"-"`
	ConsistencyMode ConsistencyLevel `json:"consistency_mode"`
	ClientWaitLimit time.Duration `json:"client_wait_limit"`
}

// ReplicationStats records telemetry counters for the geo-replication engine.
type ReplicationStats struct {
	LocalNodeID         string      `json:"local_node_id"`
	Region              string      `json:"region"`
	CurrentClock        VectorClock `json:"current_clock"`
	TotalKeys           int64       `json:"total_keys"`
	SyncPacketsSent     int64       `json:"sync_packets_sent"`
	SyncPacketsRecv     int64       `json:"sync_packets_recv"`
	ConflictsDetected   int64       `json:"conflicts_detected"`
	ConflictsResolved   int64       `json:"conflicts_resolved"`
	CRDTOperations      int64       `json:"crdt_operations"`
	ReadYourWritesHits  int64       `json:"read_your_writes_hits"`
	MonotonicReadHits   int64       `json:"monotonic_read_hits"`
	ActivePeerCount     int         `json:"active_peer_count"`
	LastSyncTime        time.Time   `json:"last_sync_time"`
}
