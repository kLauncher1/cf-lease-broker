package pqc

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const maxRequestBodyBytes = 64 * 1024 // 64 KB bounding for DoS protection

// PQCHandler serves the REST API endpoints under /api/pqc/*.
type PQCHandler struct {
	engine *PQCEngine
}

// NewPQCHandler constructs an HTTP handler backed by the PQC engine.
func NewPQCHandler(engine *PQCEngine) *PQCHandler {
	return &PQCHandler{engine: engine}
}

// ServeHTTP routes incoming PQC control and inspection requests.
func (h *PQCHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/pqc")
	path = strings.TrimPrefix(path, "/")

	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "status" || path == "":
		h.handleStatus(w, r)
	case path == "kem/keypair" && r.Method == http.MethodPost:
		h.handleKEMKeypair(w, r)
	case path == "kem/encapsulate" && r.Method == http.MethodPost:
		h.handleKEMEncapsulate(w, r)
	case path == "kem/decapsulate" && r.Method == http.MethodPost:
		h.handleKEMDecapsulate(w, r)
	case path == "sign" && r.Method == http.MethodPost:
		h.handleSign(w, r)
	case path == "verify" && r.Method == http.MethodPost:
		h.handleVerify(w, r)
	case path == "entropy" && r.Method == http.MethodGet:
		h.handleEntropy(w, r)
	case path == "entropy/harvest" && r.Method == http.MethodPost:
		h.handleHarvest(w, r)
	case path == "stats" && r.Method == http.MethodGet:
		h.handleStats(w, r)
	case path == "reset" && r.Method == http.MethodPost:
		h.handleReset(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "endpoint not found"})
	}
}

func (h *PQCHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	entStats := h.engine.GetEntropyStats()

	resp := map[string]any{
		"status":                 "operational",
		"active_kem_algorithm":   h.engine.config.DefaultKEMAlgorithm,
		"active_dsa_algorithm":   h.engine.config.DefaultDSAAlgorithm,
		"hybrid_exchange_active": h.engine.config.EnforceHybridExchange,
		"entropy_health":         entStats.HealthStatus,
		"shannon_entropy":        entStats.ShannonEntropy,
		"min_entropy_bits":       entStats.MinEntropyBits,
		"trng_available":         entStats.TRNGActive,
		"stats":                  stats,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *PQCHandler) handleKEMKeypair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Algorithm string `json:"algorithm"`
		Hybrid    *bool  `json:"hybrid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&req); err != nil && err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json payload"})
		return
	}

	hybrid := true
	if req.Hybrid != nil {
		hybrid = *req.Hybrid
	}

	sess, err := h.engine.CreateKEMSession(req.Algorithm, hybrid)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sess)
}

func (h *PQCHandler) handleKEMEncapsulate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Algorithm       string `json:"algorithm"`
		PeerPublicKey   string `json:"peer_public_key"`
		Hybrid          *bool  `json:"hybrid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json payload"})
		return
	}

	if req.PeerPublicKey == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "peer_public_key is required"})
		return
	}

	hybrid := true
	if req.Hybrid != nil {
		hybrid = *req.Hybrid
	}

	res, latUs, err := h.engine.EncapsulateAgainstPublicKey(req.Algorithm, req.PeerPublicKey, hybrid)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ciphertext_hex":       hex.EncodeToString(res.Ciphertext),
		"shared_secret_hex":    hex.EncodeToString(res.SharedSecret),
		"handshake_latency_us": latUs,
	})
}

func (h *PQCHandler) handleKEMDecapsulate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID  string `json:"session_id"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json payload"})
		return
	}

	if req.SessionID == "" || req.Ciphertext == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id and ciphertext required"})
		return
	}

	secret, latUs, err := h.engine.DecapsulateSession(req.SessionID, req.Ciphertext)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id":           req.SessionID,
		"shared_secret_hex":    hex.EncodeToString(secret),
		"handshake_latency_us": latUs,
		"status":               "decapsulated",
	})
}

func (h *PQCHandler) handleSign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		KeyID   string         `json:"key_id"`
		Voucher map[string]any `json:"voucher"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json payload"})
		return
	}

	if req.KeyID == "" {
		req.KeyID = "broker-edge-pqc-root"
	}

	sig, err := h.engine.SignVoucher(req.KeyID, req.Voucher)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sig)
}

func (h *PQCHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Voucher   map[string]any    `json:"voucher"`
		Signature *VoucherSignature `json:"signature"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json payload"})
		return
	}

	if req.Signature == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "signature object required"})
		return
	}

	valid := h.engine.VerifyVoucher(req.Voucher, req.Signature)
	status := "rejected"
	if valid {
		status = "verified"
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"valid":  valid,
		"status": status,
		"key_id": req.Signature.KeyID,
	})
}

func (h *PQCHandler) handleEntropy(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(h.engine.GetEntropyStats())
}

func (h *PQCHandler) handleHarvest(w http.ResponseWriter, r *http.Request) {
	bytesCount := 32
	if q := r.URL.Query().Get("bytes"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 1024 {
			bytesCount = n
		}
	}

	data := h.engine.HarvestEntropy(bytesCount)
	stats := h.engine.GetEntropyStats()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "harvested",
		"bytes_harvested": len(data),
		"shannon_entropy": stats.ShannonEntropy,
		"min_entropy":     stats.MinEntropyBits,
		"health":          stats.HealthStatus,
	})
}

func (h *PQCHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(h.engine.GetStats())
}

func (h *PQCHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	h.engine.Reset()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reset_complete"})
}
