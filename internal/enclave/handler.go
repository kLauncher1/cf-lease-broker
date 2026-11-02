package enclave

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const maxEnclaveBodySize = 64 * 1024 // 64KB strict DoS protection

// EnclaveHandler serves RESTful endpoints under /api/enclave/*.
type EnclaveHandler struct {
	engine *EnclaveEngine
}

// NewEnclaveHandler initializes the HTTP handler for enclave operations.
func NewEnclaveHandler(engine *EnclaveEngine) *EnclaveHandler {
	return &EnclaveHandler{engine: engine}
}

// ServeHTTP routes incoming requests under /api/enclave/*.
func (h *EnclaveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	subPath := strings.TrimPrefix(r.URL.Path, "/api/enclave")
	subPath = strings.TrimPrefix(subPath, "/")

	switch subPath {
	case "status", "":
		if r.Method == http.MethodGet {
			h.handleStatus(w, r)
			return
		}
	case "quote":
		if r.Method == http.MethodPost {
			h.handleQuote(w, r)
			return
		}
	case "verify":
		if r.Method == http.MethodPost {
			h.handleVerify(w, r)
			return
		}
	case "seal":
		if r.Method == http.MethodPost {
			h.handleSeal(w, r)
			return
		}
	case "unseal":
		if r.Method == http.MethodPost {
			h.handleUnseal(w, r)
			return
		}
	case "execute":
		if r.Method == http.MethodPost {
			h.handleExecute(w, r)
			return
		}
	case "peers":
		if r.Method == http.MethodGet {
			h.handlePeers(w, r)
			return
		}
	case "stats":
		if r.Method == http.MethodGet {
			h.handleStats(w, r)
			return
		}
	case "reset":
		if r.Method == http.MethodPost {
			h.handleReset(w, r)
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "enclave endpoint not found"})
}

func (h *EnclaveHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	driver := h.engine.Driver()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"enclave_type":      stats.EnclaveType,
		"status":            stats.Status,
		"driver_available":  driver.IsAvailable(),
		"device_path":       driver.GetDevicePath(),
		"epc_total_mb":      stats.EPCTotalBytes / (1024 * 1024),
		"epc_allocated_mb":  stats.EPCAllocatedBytes / (1024 * 1024),
		"quotes_generated":  stats.QuotesGenerated,
		"secrets_sealed":    stats.SecretsSealed,
	})
}

func (h *EnclaveHandler) handleQuote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserData string `json:"user_data"`
		Nonce    string `json:"nonce"`
	}

	body := io.LimitReader(r.Body, maxEnclaveBodySize)
	if err := json.NewDecoder(body).Decode(&req); err != nil && err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed json payload"})
		return
	}

	quote, err := h.engine.GenerateQuote(req.UserData, req.Nonce)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(quote)
}

func (h *EnclaveHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quote  *AttestationQuote        `json:"quote"`
		Policy *QuoteVerificationPolicy `json:"policy"`
	}

	body := io.LimitReader(r.Body, maxEnclaveBodySize)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed json payload"})
		return
	}

	if req.Quote == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "quote is required"})
		return
	}

	err := h.engine.VerifyQuote(req.Quote, req.Policy)
	if err != nil {
		w.WriteHeader(http.StatusOK) // Return 200 with valid: false for policy query
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":  false,
			"reason": err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":  true,
		"reason": "quote signature and policy constraints fully satisfied",
	})
}

func (h *EnclaveHandler) handleSeal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID  string        `json:"tenant_id"`
		Plaintext string        `json:"plaintext"`
		Policy    SealingPolicy `json:"policy"`
		KeyID     string        `json:"key_id"`
	}

	body := io.LimitReader(r.Body, maxEnclaveBodySize)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed json payload"})
		return
	}

	plainBytes := []byte(req.Plaintext)
	defer Zeroize(plainBytes)

	blob, err := h.engine.SealSecret(req.TenantID, plainBytes, req.Policy, req.KeyID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(blob)
}

func (h *EnclaveHandler) handleUnseal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string      `json:"tenant_id"`
		Blob     *SealedBlob `json:"blob"`
	}

	body := io.LimitReader(r.Body, maxEnclaveBodySize)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed json payload"})
		return
	}

	if req.Blob == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "blob is required"})
		return
	}

	plaintext, err := h.engine.UnsealSecret(req.TenantID, req.Blob)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer Zeroize(plaintext)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"plaintext": string(plaintext),
	})
}

func (h *EnclaveHandler) handleExecute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		TaskType string `json:"task_type"`
		Input    string `json:"input"`
	}

	body := io.LimitReader(r.Body, maxEnclaveBodySize)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed json payload"})
		return
	}

	inputBytes := []byte(req.Input)
	defer Zeroize(inputBytes)

	res, err := h.engine.ExecuteConfidentialTask(req.TenantID, req.TaskType, inputBytes)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *EnclaveHandler) handlePeers(w http.ResponseWriter, r *http.Request) {
	peers := h.engine.ListPeerAttestations()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"peer_count": len(peers),
		"peers":      peers,
	})
}

func (h *EnclaveHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *EnclaveHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	h.engine.Reset()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "enclave reset successfully"})
}
