package transcoder

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// TranscoderHandler serves REST API endpoints under /api/transcoder/*.
type TranscoderHandler struct {
	engine *TranscoderEngine
}

// NewTranscoderHandler initializes the HTTP handler for edge transcoding.
func NewTranscoderHandler(engine *TranscoderEngine) *TranscoderHandler {
	return &TranscoderHandler{engine: engine}
}

func (h *TranscoderHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept-Encoding, Authorization, X-Tenant-ID")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/transcoder")
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "status", "":
		h.handleStatus(w, r)
	case "compress":
		h.handleCompress(w, r)
	case "decompress":
		h.handleDecompress(w, r)
	case "inspect":
		h.handleInspect(w, r)
	case "dictionary":
		h.handleDictionary(w, r)
	case "dictionary/train":
		h.handleDictionaryTrain(w, r)
	case "stats":
		h.handleStats(w, r)
	case "reset":
		h.handleReset(w, r)
	default:
		http.Error(w, `{"error":"endpoint_not_found"}`, http.StatusNotFound)
	}
}

func (h *TranscoderHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	stats := h.engine.GetStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":                "operational",
		"default_algorithm":     h.engine.config.DefaultAlgorithm,
		"preferred_algorithms":  h.engine.config.PreferredAlgorithms,
		"compressible_types":    h.engine.config.CompressibleTypes,
		"dictionary_training":   h.engine.config.EnableDictionaryTraining,
		"active_dictionaries":   stats.ActiveDictionariesCount,
		"stats":                 stats,
	})
}

func (h *TranscoderHandler) handleCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	algoStr := r.URL.Query().Get("algo")
	if algoStr == "" {
		algoStr = string(h.engine.config.DefaultAlgorithm)
	}
	algo := CompressionAlgorithm(strings.ToLower(algoStr))

	level := 0
	if lvlStr := r.URL.Query().Get("level"); lvlStr != "" {
		level, _ = strconv.Atoi(lvlStr)
	}
	tenantID := r.URL.Query().Get("tenant")
	if tenantID == "" {
		tenantID = r.Header.Get("X-Tenant-ID")
	}

	useDict := r.URL.Query().Get("dict") == "true"

	// Read input payload bounded to 10MB
	limited := io.LimitReader(r.Body, 10*1024*1024)
	rawBytes, err := io.ReadAll(limited)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to read payload"})
		return
	}

	decision := TranscodeDecision{
		Algorithm:       algo,
		Level:           level,
		UseDictionary:   useDict,
		ContentEncoding: string(algo),
		ShouldCompress:  true,
	}

	compressed, err := h.engine.TranscodeBytes(rawBytes, decision, tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	rawLen := len(rawBytes)
	compLen := len(compressed)
	savings := 0.0
	if rawLen > 0 {
		saved := rawLen - compLen
		if saved > 0 {
			savings = float64(saved) / float64(rawLen) * 100.0
		}
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Encoding", string(algo))
	w.Header().Set("X-CF-Broker-Compression", string(algo))
	w.Header().Set("X-CF-Broker-Raw-Bytes", strconv.Itoa(rawLen))
	w.Header().Set("X-CF-Broker-Compressed-Bytes", strconv.Itoa(compLen))
	w.Header().Set("X-CF-Broker-Savings-Percent", fmt.Sprintf("%.2f%%", savings))

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(compressed)
}

func (h *TranscoderHandler) handleDecompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	algoStr := r.URL.Query().Get("algo")
	if algoStr == "" {
		algoStr = r.Header.Get("Content-Encoding")
	}
	if algoStr == "" {
		algoStr = string(h.engine.config.DefaultAlgorithm)
	}
	algo := CompressionAlgorithm(strings.ToLower(algoStr))

	tenantID := r.URL.Query().Get("tenant")
	if tenantID == "" {
		tenantID = r.Header.Get("X-Tenant-ID")
	}

	limited := io.LimitReader(r.Body, 10*1024*1024)
	compBytes, err := io.ReadAll(limited)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to read payload"})
		return
	}

	decomp, err := h.engine.DecompressBytes(compBytes, algo, tenantID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "decompression_failed: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(decomp)
}

func (h *TranscoderHandler) handleInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	acceptEncoding := r.Header.Get("Accept-Encoding")
	contentType := r.Header.Get("Content-Type")
	contentLength := r.ContentLength

	if r.Method == http.MethodPost && r.Body != nil {
		var req struct {
			AcceptEncoding string `json:"accept_encoding"`
			ContentType    string `json:"content_type"`
			ContentLength  int64  `json:"content_length"`
		}
		limited := io.LimitReader(r.Body, 64*1024)
		if err := json.NewDecoder(limited).Decode(&req); err == nil {
			if req.AcceptEncoding != "" {
				acceptEncoding = req.AcceptEncoding
			}
			if req.ContentType != "" {
				contentType = req.ContentType
			}
			if req.ContentLength > 0 {
				contentLength = req.ContentLength
			}
		}
	}

	decision := h.engine.InspectPayload(acceptEncoding, contentType, contentLength)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(decision)
}

func (h *TranscoderHandler) handleDictionary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	dicts := h.engine.dictManager.ListDictionaries()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"dictionaries": dicts,
		"count":        len(dicts),
	})
}

func (h *TranscoderHandler) handleDictionaryTrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TenantID string   `json:"tenant_id"`
		Samples  []string `json:"samples"`
	}

	limited := io.LimitReader(r.Body, 64*1024)
	if err := json.NewDecoder(limited).Decode(&req); err != nil || len(req.Samples) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload: tenant_id and samples required"})
		return
	}

	byteSamples := make([][]byte, 0, len(req.Samples))
	for _, s := range req.Samples {
		byteSamples = append(byteSamples, []byte(s))
	}

	dict := h.engine.dictManager.TrainDictionary(req.TenantID, byteSamples)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dict.Stats())
}

func (h *TranscoderHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	stats := h.engine.GetStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *TranscoderHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	h.engine.Reset()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "transcoder_reset_complete"})
}
