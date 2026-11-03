package entropy

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const maxEntropyBodySize = 64 * 1024 // 64KB strict DoS protection

// EntropyHandler serves RESTful endpoints under /api/entropy/*.
type EntropyHandler struct {
	engine *EntropyEngine
}

// NewEntropyHandler initializes an HTTP handler for entropy operations.
func NewEntropyHandler(engine *EntropyEngine) *EntropyHandler {
	return &EntropyHandler{engine: engine}
}

// ServeHTTP routes incoming requests under /api/entropy/*.
func (h *EntropyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	subPath := strings.TrimPrefix(r.URL.Path, "/api/entropy")
	subPath = strings.TrimPrefix(subPath, "/")

	switch subPath {
	case "status", "":
		if r.Method == http.MethodGet {
			h.handleStatus(w, r)
			return
		}
	case "beacon":
		if r.Method == http.MethodGet {
			h.handleBeacon(w, r)
			return
		}
	case "harvest":
		if r.Method == http.MethodPost {
			h.handleHarvest(w, r)
			return
		}
	case "health":
		if r.Method == http.MethodGet {
			h.handleHealth(w, r)
			return
		}
	case "verify":
		if r.Method == http.MethodPost {
			h.handleVerify(w, r)
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
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "entropy endpoint not found"})
}

func (h *EntropyHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	curRound := h.engine.GetBeaconRound()
	var roundNum uint64
	if curRound != nil {
		roundNum = curRound.Round
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"active_source":          stats.ActiveSource,
		"health_status":          stats.LastHealthReport.Status,
		"min_entropy_bits":       stats.LastHealthReport.MinEntropy,
		"shannon_entropy":        stats.LastHealthReport.ShannonEntropy,
		"current_round":          roundNum,
		"pool_size_bytes":        stats.CurrentPoolSize,
		"total_harvested_bytes":  stats.TotalHarvestedBytes,
		"total_tokens_generated": stats.TotalTokensGenerated,
		"uptime_seconds":         stats.UptimeSeconds,
	})
}

func (h *EntropyHandler) handleBeacon(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	current := h.engine.GetBeaconRound()
	history := h.engine.GetBeaconHistory(limit)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"current": current,
		"history": history,
		"count":   len(history),
	})
}

func (h *EntropyHandler) handleHarvest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bytes   int    `json:"bytes"`
		Purpose string `json:"purpose"`
	}

	// Strictly limit request body to 64KB to prevent memory exhaustion attacks
	body := io.LimitReader(r.Body, maxEntropyBodySize)
	if err := json.NewDecoder(body).Decode(&req); err != nil && err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed json payload or body too large"})
		return
	}

	if req.Bytes <= 0 {
		req.Bytes = 32
	}
	if req.Bytes > 1024 {
		req.Bytes = 1024
	}
	if req.Purpose == "" {
		req.Purpose = "generic-token"
	}

	token, err := h.engine.GenerateToken(req.Bytes, req.Purpose)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer Zeroize(token)

	health := h.engine.GetHealthReport()

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token_hex":        hex.EncodeToString(token),
		"length":           len(token),
		"purpose":          req.Purpose,
		"min_entropy_bits": health.MinEntropy,
		"source":           SourceHybrid,
	})
}

func (h *EntropyHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	report := h.engine.GetHealthReport()
	_ = json.NewEncoder(w).Encode(report)
}

func (h *EntropyHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Round            *BeaconRound `json:"round"`
		ExpectedPrevHash string       `json:"expected_prev_hash"`
		VerificationKey  string       `json:"verification_key"`
	}

	// Strictly limit request body to 64KB
	body := io.LimitReader(r.Body, maxEntropyBodySize)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed json payload or body too large"})
		return
	}

	if req.Round == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "round object is required"})
		return
	}

	var key []byte
	if req.VerificationKey != "" {
		k, err := hex.DecodeString(req.VerificationKey)
		if err == nil {
			key = k
		} else {
			key = []byte(req.VerificationKey)
		}
	}

	verified, err := h.engine.VerifyBeaconRound(req.Round, req.ExpectedPrevHash, key)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"verified": verified,
		"round":    req.Round.Round,
		"error":    errMsg,
	})
}

func (h *EntropyHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *EntropyHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	h.engine.Reset()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "entropy_subsystem_reset_complete"})
}
