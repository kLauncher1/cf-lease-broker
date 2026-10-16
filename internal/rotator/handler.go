package rotator

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const maxRequestBodyBytes = 64 * 1024 // 64KB defense against buffer exhaustion DoS

// RotatorHandler serves REST API endpoints for token rotation, mesh gossip, and attestation.
type RotatorHandler struct {
	rotator  *TokenRotator
	mesh     *RevocationMesh
	verifier *AttestationVerifier
}

// NewRotatorHandler constructs an HTTP handler routing /api/rotator/* requests.
func NewRotatorHandler(rotator *TokenRotator, mesh *RevocationMesh, verifier *AttestationVerifier) *RotatorHandler {
	return &RotatorHandler{
		rotator:  rotator,
		mesh:     mesh,
		verifier: verifier,
	}
}

func (h *RotatorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/rotator")
	path = strings.TrimPrefix(path, "/")

	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "" || path == "status":
		h.handleStatus(w, r)
	case path == "tokens":
		h.handleTokens(w, r)
	case path == "renew":
		h.handleRenew(w, r)
	case path == "revoke":
		h.handleRevoke(w, r)
	case path == "gossip":
		h.handleGossip(w, r)
	case path == "delegate":
		h.handleDelegate(w, r)
	case path == "challenge":
		h.handleChallenge(w, r)
	case path == "tombstones":
		h.handleTombstones(w, r)
	case path == "peers":
		h.handlePeers(w, r)
	case path == "events":
		h.handleEvents(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *RotatorHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	summary := h.rotator.GetSummary()
	_ = json.NewEncoder(w).Encode(summary)
}

func (h *RotatorHandler) handleTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	tokens := h.rotator.ListTokens()
	_ = json.NewEncoder(w).Encode(tokens)
}

func (h *RotatorHandler) handleRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TokenID string `json:"token_id"`
		Reason  string `json:"reason"`
	}
	limited := io.LimitReader(r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		http.Error(w, `{"error":"malformed json payload"}`, http.StatusBadRequest)
		return
	}

	if req.TokenID == "" {
		http.Error(w, `{"error":"missing token_id"}`, http.StatusBadRequest)
		return
	}
	if req.Reason == "" {
		req.Reason = "manual_api_trigger"
	}

	ev, err := h.rotator.RenewToken(req.TokenID, req.Reason)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	_ = json.NewEncoder(w).Encode(ev)
}

func (h *RotatorHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TokenID string `json:"token_id"`
		Reason  string `json:"reason"`
	}
	limited := io.LimitReader(r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		http.Error(w, `{"error":"malformed json payload"}`, http.StatusBadRequest)
		return
	}

	if req.TokenID == "" {
		http.Error(w, `{"error":"missing token_id"}`, http.StatusBadRequest)
		return
	}
	if req.Reason == "" {
		req.Reason = "operator_revocation"
	}

	if err := h.rotator.RevokeToken(req.TokenID, req.Reason); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"revoked","token_id":"` + req.TokenID + `"}`))
}

func (h *RotatorHandler) handleGossip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var packet GossipPacket
	limited := io.LimitReader(r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(limited).Decode(&packet); err != nil {
		http.Error(w, `{"error":"malformed json payload"}`, http.StatusBadRequest)
		return
	}

	applied, total := h.mesh.ProcessGossipPacket(packet)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "gossip_processed",
		"applied_count":   applied,
		"total_count":     total,
		"receiver_node":   h.mesh.NodeID(),
		"avg_latency_ms":  h.mesh.GetAveragePropagationLatencyMs(),
	})
}

func (h *RotatorHandler) handleDelegate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req DelegatedRefreshRequest
	limited := io.LimitReader(r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		http.Error(w, `{"error":"malformed json payload"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.rotator.ProcessDelegatedRefresh(&req)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	_ = json.NewEncoder(w).Encode(resp)
}

func (h *RotatorHandler) handleChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	nonce, ts := h.verifier.GenerateChallenge()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"nonce":     nonce,
		"timestamp": ts,
		"provider":  "Hardware TPM 2.0 / CNG Attestation Nonce Generator",
	})
}

func (h *RotatorHandler) handleTombstones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	tombstones := h.mesh.ListTombstones()
	_ = json.NewEncoder(w).Encode(tombstones)
}

func (h *RotatorHandler) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		peers := h.mesh.ListPeers()
		_ = json.NewEncoder(w).Encode(peers)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			NodeID  string `json:"node_id"`
			Address string `json:"address"`
		}
		limited := io.LimitReader(r.Body, maxRequestBodyBytes)
		if err := json.NewDecoder(limited).Decode(&req); err != nil {
			http.Error(w, `{"error":"malformed json payload"}`, http.StatusBadRequest)
			return
		}
		if req.NodeID == "" || req.Address == "" {
			http.Error(w, `{"error":"missing node_id or address"}`, http.StatusBadRequest)
			return
		}
		h.mesh.RegisterPeer(req.NodeID, req.Address)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"peer_registered","node_id":"` + req.NodeID + `"}`))
		return
	}
	http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
}

func (h *RotatorHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events := h.rotator.GetRecentEvents(limit)
	_ = json.NewEncoder(w).Encode(events)
}
