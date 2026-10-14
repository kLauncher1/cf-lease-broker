package raft

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// NodeRole represents the Raft consensus role of a node.
type NodeRole string

const (
	RoleFollower  NodeRole = "Follower"
	RoleCandidate NodeRole = "Candidate"
	RoleLeader    NodeRole = "Leader"
)

// Command types supported by the distributed Raft state machine.
const (
	CmdGrantLease  = "GRANT_LEASE"
	CmdRevokeLease = "REVOKE_LEASE"
	CmdSyncAccount = "SYNC_ACCOUNT"
	CmdUpdateQuota = "UPDATE_QUOTA"
	CmdNoop        = "NOOP"
)

// NodeConfig holds configuration parameters for a Raft consensus node.
type NodeConfig struct {
	NodeID              string            `json:"node_id"`
	BindAddr            string            `json:"bind_addr"`
	Peers               map[string]string `json:"peers"` // NodeID -> Address (e.g. "http://127.0.0.1:9082")
	DiscoveryDomain     string            `json:"discovery_domain,omitempty"`
	HeartbeatInterval   time.Duration     `json:"heartbeat_interval"`
	ElectionTimeoutMin  time.Duration     `json:"election_timeout_min"`
	ElectionTimeoutMax  time.Duration     `json:"election_timeout_max"`
	DataDir             string            `json:"data_dir,omitempty"`
}

// DefaultNodeConfig returns production defaults for local and edge clustering.
func DefaultNodeConfig(nodeID string) NodeConfig {
	return NodeConfig{
		NodeID:             nodeID,
		BindAddr:           "127.0.0.1:9081",
		Peers:              make(map[string]string),
		HeartbeatInterval:  50 * time.Millisecond,
		ElectionTimeoutMin: 150 * time.Millisecond,
		ElectionTimeoutMax: 300 * time.Millisecond,
		DataDir:            "",
	}
}

// LogEntry represents an immutable state machine entry in the replicated journal.
type LogEntry struct {
	Term      uint64    `json:"term"`
	Index     uint64    `json:"index"`
	Type      string    `json:"type"`
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Checksum  string    `json:"checksum"`
}

// ComputeChecksum calculates SHA-256 integrity hash for the log entry.
func (e *LogEntry) ComputeChecksum() string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%d:%d:%s:%d:%s", e.Term, e.Index, e.Type, e.Timestamp.UnixNano(), string(e.Data))
	return hex.EncodeToString(h.Sum(nil))
}

// RequestVoteArgs payload for candidate election votes.
type RequestVoteArgs struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
}

// RequestVoteReply returns the election ballot result.
type RequestVoteReply struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
}

// AppendEntriesArgs payload for leader heartbeats and log replication.
type AppendEntriesArgs struct {
	Term         uint64     `json:"term"`
	LeaderID     string     `json:"leader_id"`
	PrevLogIndex uint64     `json:"prev_log_index"`
	PrevLogTerm  uint64     `json:"prev_log_term"`
	Entries      []LogEntry `json:"entries"`
	LeaderCommit uint64     `json:"leader_commit"`
}

// AppendEntriesReply returns the follower log replication outcome.
type AppendEntriesReply struct {
	Term       uint64 `json:"term"`
	Success    bool   `json:"success"`
	MatchIndex uint64 `json:"match_index"`
}

// PeerStatus tracks runtime telemetry and health metrics for remote cluster nodes.
type PeerStatus struct {
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	Active        bool      `json:"active"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	MatchIndex    uint64    `json:"match_index"`
	NextIndex     uint64    `json:"next_index"`
	LatencyMs     float64   `json:"latency_ms"`
}

// ClusterStatus represents aggregated observability metrics for the local Raft cluster.
type ClusterStatus struct {
	NodeID          string       `json:"node_id"`
	Role            NodeRole     `json:"role"`
	CurrentTerm     uint64       `json:"current_term"`
	LeaderID        string       `json:"leader_id"`
	CommitIndex     uint64       `json:"commit_index"`
	LastApplied     uint64       `json:"last_applied"`
	LogLength       int          `json:"log_length"`
	Peers           []PeerStatus `json:"peers"`
	FSMAppliedCount int64        `json:"fsm_applied_count"`
	UptimeSeconds   float64      `json:"uptime_seconds"`
}

// FSM is the deterministic Finite State Machine interface applied across the cluster.
type FSM interface {
	Apply(entry LogEntry) (interface{}, error)
	Snapshot() ([]byte, error)
	Restore(snapshot []byte) error
}
