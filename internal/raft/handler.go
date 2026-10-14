package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxVoteBodyBytes    = 64 * 1024       // 64 KB limit for election ballots
	maxAppendBodyBytes  = 8 * 1024 * 1024 // 8 MB limit for batched log replication
	maxProposeBodyBytes = 4 * 1024 * 1024 // 4 MB limit for command proposals
	maxPeerBodyBytes    = 64 * 1024       // 64 KB limit for peer registration
)

// RaftHandler provides HTTP REST endpoints for consensus communication, observability, and command dispatch.
type RaftHandler struct {
	node      *RaftNode
	journal   *Journal
	discovery *PeerDiscovery
}

// NewRaftHandler creates a new HTTP handler multiplexer for the Raft node.
func NewRaftHandler(node *RaftNode, journal *Journal, discovery *PeerDiscovery) *RaftHandler {
	return &RaftHandler{
		node:      node,
		journal:   journal,
		discovery: discovery,
	}
}

// ProposeRequest represents a command proposal submitted to the Raft cluster.
type ProposeRequest struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// RegisterPeerRequest represents a manual peer registration payload.
type RegisterPeerRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

func (h *RaftHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	w.Header().Set("Content-Type", "application/json")

	switch {
	case strings.HasSuffix(path, "/raft/vote"):
		h.handleVote(w, r)
	case strings.HasSuffix(path, "/raft/append"):
		h.handleAppend(w, r)
	case strings.HasSuffix(path, "/raft/status"):
		h.handleStatus(w, r)
	case strings.HasSuffix(path, "/raft/propose"):
		h.handlePropose(w, r)
	case strings.HasSuffix(path, "/raft/peers"):
		h.handlePeers(w, r)
	case strings.HasSuffix(path, "/raft/journal"):
		h.handleJournal(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *RaftHandler) handleVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var args RequestVoteArgs
	limitedReader := io.LimitReader(r.Body, maxVoteBodyBytes)
	if err := json.NewDecoder(limitedReader).Decode(&args); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"bad request: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	reply := h.node.RequestVote(args)
	_ = json.NewEncoder(w).Encode(reply)
}

func (h *RaftHandler) handleAppend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var args AppendEntriesArgs
	limitedReader := io.LimitReader(r.Body, maxAppendBodyBytes)
	if err := json.NewDecoder(limitedReader).Decode(&args); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"bad request: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	reply := h.node.AppendEntries(args)
	_ = json.NewEncoder(w).Encode(reply)
}

func (h *RaftHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	status := h.node.Status()
	_ = json.NewEncoder(w).Encode(status)
}

func (h *RaftHandler) handlePropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req ProposeRequest
	limitedReader := io.LimitReader(r.Body, maxProposeBodyBytes)
	if err := json.NewDecoder(limitedReader).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"bad request: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	req.Type = strings.TrimSpace(req.Type)
	if req.Type == "" {
		http.Error(w, `{"error":"missing command type"}`, http.StatusBadRequest)
		return
	}

	switch req.Type {
	case CmdGrantLease, CmdRevokeLease, CmdSyncAccount, CmdUpdateQuota, CmdNoop:
		// Valid command type
	default:
		http.Error(w, fmt.Sprintf(`{"error":"unsupported command type: %s"}`, req.Type), http.StatusBadRequest)
		return
	}

	idx, err := h.node.Propose(req.Type, req.Data)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  err.Error(),
			"status": "rejected",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "committed",
		"index":  idx,
	})
}

func (h *RaftHandler) handlePeers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req RegisterPeerRequest
		limitedReader := io.LimitReader(r.Body, maxPeerBodyBytes)
		if err := json.NewDecoder(limitedReader).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"bad request: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		req.ID = strings.TrimSpace(req.ID)
		req.Address = strings.TrimSpace(req.Address)
		if req.ID == "" || req.Address == "" {
			http.Error(w, `{"error":"missing peer id or address"}`, http.StatusBadRequest)
			return
		}
		if len(req.ID) > 128 || len(req.Address) > 512 {
			http.Error(w, `{"error":"peer id or address exceeds maximum allowed length"}`, http.StatusBadRequest)
			return
		}
		h.discovery.RegisterPeer(req.ID, req.Address)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"peer_registered"}`))
		return
	case http.MethodGet:
		peers := h.discovery.GetPeerStatusList()
		_ = json.NewEncoder(w).Encode(peers)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *RaftHandler) handleJournal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	entries := h.journal.AllEntries()
	_ = json.NewEncoder(w).Encode(entries)
}
