package zkproof

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ZKProofHandler provides RESTful HTTP management and verification endpoints mounted under /api/zkproof/*.
type ZKProofHandler struct {
	engine *ZKProofEngine
}

// NewZKProofHandler constructs an HTTP handler around the Zero-Knowledge Proof Engine.
func NewZKProofHandler(engine *ZKProofEngine) *ZKProofHandler {
	return &ZKProofHandler{engine: engine}
}

func (h *ZKProofHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/zkproof")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "status" && r.Method == http.MethodGet:
		h.handleStatus(w, r)
	case path == "prove/isolation" && r.Method == http.MethodPost:
		h.handleProveIsolation(w, r)
	case path == "prove/quota" && r.Method == http.MethodPost:
		h.handleProveQuota(w, r)
	case path == "verify" && r.Method == http.MethodPost:
		h.handleVerify(w, r)
	case path == "verify/batch" && r.Method == http.MethodPost:
		h.handleVerifyBatch(w, r)
	case path == "keys" && r.Method == http.MethodGet:
		h.handleKeys(w, r)
	case path == "stats" && r.Method == http.MethodGet:
		h.handleStats(w, r)
	case path == "recent" && r.Method == http.MethodGet:
		h.handleRecent(w, r)
	case path == "reset" && r.Method == http.MethodPost:
		h.handleReset(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *ZKProofHandler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	stats := h.engine.GetStats()
	accelStats := h.engine.Accelerator().GetStats()

	resp := map[string]interface{}{
		"status":            "active",
		"subsystem":         "zero_knowledge_edge_proof_verifier",
		"stats":             stats,
		"vector_accel":      accelStats,
		"active_keys_count": len(h.engine.ListKeys()),
	}
	_ = json.NewEncoder(w).Encode(resp)
}

type proveIsolationRequest struct {
	Statement     ProofStatement `json:"statement"`
	WitnessSecret string         `json:"witness_secret"`
	GasWitness    uint64         `json:"gas_witness"`
	MemWitness    uint32         `json:"mem_witness"`
}

func (h *ZKProofHandler) handleProveIsolation(w http.ResponseWriter, r *http.Request) {
	var req proveIsolationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	proof, err := h.engine.ProveWorkerIsolation(req.Statement, req.WitnessSecret, req.GasWitness, req.MemWitness)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(proof)
}

type proveQuotaRequest struct {
	Statement     ProofStatement `json:"statement"`
	APIKeyWitness string         `json:"api_key_witness"`
	UsageWitness  uint64         `json:"usage_witness"`
}

func (h *ZKProofHandler) handleProveQuota(w http.ResponseWriter, r *http.Request) {
	var req proveQuotaRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	proof, err := h.engine.ProveQuotaAttestation(req.Statement, req.APIKeyWitness, req.UsageWitness, nil)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(proof)
}

func (h *ZKProofHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	var proof ZKProof
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&proof); err != nil {
		http.Error(w, `{"error":"invalid proof json"}`, http.StatusBadRequest)
		return
	}

	res, err := h.engine.Verify(&proof)
	if err != nil && res == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if !res.Valid {
		w.WriteHeader(http.StatusUnprocessableEntity)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *ZKProofHandler) handleVerifyBatch(w http.ResponseWriter, r *http.Request) {
	var proofs []*ZKProof
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&proofs); err != nil {
		http.Error(w, `{"error":"invalid batch proof json"}`, http.StatusBadRequest)
		return
	}

	results, err := h.engine.VerifyBatch(proofs)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(results)
}

func (h *ZKProofHandler) handleKeys(w http.ResponseWriter, _ *http.Request) {
	keys := h.engine.ListKeys()
	_ = json.NewEncoder(w).Encode(keys)
}

func (h *ZKProofHandler) handleStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.engine.GetStats()
	accelStats := h.engine.Accelerator().GetStats()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"verifier": stats,
		"vector":   accelStats,
	})
}

func (h *ZKProofHandler) handleRecent(w http.ResponseWriter, _ *http.Request) {
	recent := h.engine.ListRecentVerifications()
	_ = json.NewEncoder(w).Encode(recent)
}

func (h *ZKProofHandler) handleReset(w http.ResponseWriter, _ *http.Request) {
	h.engine.Reset()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reset_successful"})
}
