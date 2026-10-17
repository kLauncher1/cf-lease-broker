package steering

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const maxRequestBodyBytes = 64 * 1024 // 64KB defense against buffer exhaustion DoS

// SteeringHandler serves REST API endpoints for HTTP/3 eBPF packet steering and diagnostic tests.
type SteeringHandler struct {
	engine *PacketSteeringEngine
}

// NewSteeringHandler constructs an HTTP handler routing /api/steering/* requests.
func NewSteeringHandler(engine *PacketSteeringEngine) *SteeringHandler {
	return &SteeringHandler{
		engine: engine,
	}
}

func (h *SteeringHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/steering")
	path = strings.TrimPrefix(path, "/")

	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "" || path == "status":
		h.handleStatus(w, r)
	case path == "endpoints":
		h.handleEndpoints(w, r)
	case path == "rules":
		h.handleRules(w, r)
	case path == "simulate":
		h.handleSimulate(w, r)
	case path == "stats":
		h.handleStats(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *SteeringHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ACTIVE",
		"mode":            stats.ActiveMode,
		"ebpf_available":  stats.EBPFAvailable,
		"stats":           stats,
		"endpoints_count": stats.ActiveEndpoints,
		"rules_count":     stats.ActiveRules,
	})
}

func (h *SteeringHandler) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		eps := h.engine.ListEndpoints()
		_ = json.NewEncoder(w).Encode(eps)

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
		if err != nil {
			http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
			return
		}
		var ep WorkerEndpoint
		if err := json.Unmarshal(body, &ep); err != nil {
			http.Error(w, `{"error":"invalid json payload"}`, http.StatusBadRequest)
			return
		}
		if err := h.engine.RegisterEndpoint(&ep); err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered", "id": ep.ID})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, `{"error":"missing id parameter"}`, http.StatusBadRequest)
			return
		}
		if err := h.engine.DeregisterEndpoint(id); err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deregistered", "id": id})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *SteeringHandler) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules := h.engine.ListRules()
		_ = json.NewEncoder(w).Encode(rules)

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
		if err != nil {
			http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
			return
		}
		var rule SteeringRule
		if err := json.Unmarshal(body, &rule); err != nil {
			http.Error(w, `{"error":"invalid json payload"}`, http.StatusBadRequest)
			return
		}
		if err := h.engine.AddRule(&rule); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "created", "id": rule.ID})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, `{"error":"missing id parameter"}`, http.StatusBadRequest)
			return
		}
		if err := h.engine.RemoveRule(id); err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

type simulateRequest struct {
	RawHex       string `json:"raw_hex,omitempty"`
	PacketType   string `json:"packet_type,omitempty"` // INITIAL, 1-RTT, etc.
	DCID         string `json:"dcid,omitempty"`
	SCID         string `json:"scid,omitempty"`
	PayloadBytes int    `json:"payload_bytes,omitempty"`
	SrcAddr      string `json:"src_addr,omitempty"`
	DstAddr      string `json:"dst_addr,omitempty"`
}

func (h *SteeringHandler) handleSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var req simulateRequest
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}

	var rawPacket []byte
	if req.RawHex != "" {
		p, err := hex.DecodeString(strings.TrimSpace(req.RawHex))
		if err != nil {
			http.Error(w, `{"error":"invalid raw_hex string"}`, http.StatusBadRequest)
			return
		}
		rawPacket = p
	} else {
		// Build synthetic QUIC packet
		pType := QUICHeaderInitial
		if req.PacketType == "1-RTT" || req.PacketType == "SHORT" {
			pType = QUICHeaderShort1RTT
		}
		dcid := req.DCID
		if dcid == "" {
			dcid = "cf01a1b2c3d4e5f6"
		}
		scid := req.SCID
		if scid == "" {
			scid = "1122334455667788"
		}
		bytesCount := req.PayloadBytes
		if bytesCount <= 0 {
			bytesCount = 64
		}
		rawPacket = BuildSyntheticQUICPacket(pType, dcid, scid, bytesCount)
	}

	srcAddr := req.SrcAddr
	if srcAddr == "" {
		srcAddr = "192.0.2.100:54321"
	}
	dstAddr := req.DstAddr
	if dstAddr == "" {
		dstAddr = "198.51.100.1:443"
	}

	decision, err := h.engine.SteerPacket(rawPacket, srcAddr, dstAddr)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"decision":             decision,
		"raw_packet_len_bytes": len(rawPacket),
		"raw_packet_preview":   hex.EncodeToString(rawPacket[:min(32, len(rawPacket))]),
		"status":               "STEERED",
	})
}

func (h *SteeringHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(stats)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
