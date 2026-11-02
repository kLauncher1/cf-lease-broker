package scrubber

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// ScrubberHandler exposes RESTful management and inspection endpoints under /api/scrubber/*.
type ScrubberHandler struct {
	engine *ScrubberEngine
}

// NewScrubberHandler constructs an HTTP handler backed by the scrubber engine.
func NewScrubberHandler(engine *ScrubberEngine) *ScrubberHandler {
	return &ScrubberHandler{engine: engine}
}

// ServeHTTP routes incoming requests under /api/scrubber/*.
func (h *ScrubberHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/scrubber")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" || path == "status":
		h.handleStatus(w, r)
	case path == "stats":
		h.handleStats(w, r)
	case path == "inspect":
		h.handleInspect(w, r)
	case path == "reputation":
		h.handleReputation(w, r)
	case path == "challenge":
		h.handleChallenge(w, r)
	case path == "challenge/verify":
		h.handleChallengeVerify(w, r)
	case path == "bans":
		h.handleListBans(w, r)
	case path == "ban":
		h.handleBan(w, r)
	case path == "unban":
		h.handleUnban(w, r)
	case path == "policies" || path == "rules":
		h.handlePolicies(w, r)
	case path == "reset":
		h.handleReset(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "unknown scrubber endpoint",
			"path":  r.URL.Path,
		})
	}
}

func (h *ScrubberHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "active",
		"driver_mode":     stats.DriverMode,
		"uptime_seconds":  stats.UptimeSeconds,
		"active_bans":     stats.ActiveBans,
		"total_inspected": stats.TotalPacketsInspected,
		"mitigation_us":   stats.AvgMitigationLatencyUs,
	})
}

func (h *ScrubberHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(h.engine.GetStats())
}

type inspectRequest struct {
	SrcIP       string `json:"src_ip"`
	DstIP       string `json:"dst_ip"`
	SrcPort     int    `json:"src_port"`
	DstPort     int    `json:"dst_port"`
	Protocol    string `json:"protocol"`
	IsSYN       bool   `json:"is_syn"`
	IsACK       bool   `json:"is_ack"`
	PayloadSize int    `json:"payload_size"`
	TenantID    string `json:"tenant_id"`
}

func (h *ScrubberHandler) handleInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var req inspectRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json payload: " + err.Error()})
		return
	}

	if req.TenantID == "" {
		req.TenantID = "default"
	}
	if req.SrcIP == "" {
		req.SrcIP = "198.51.100.1"
	}
	if req.DstIP == "" {
		req.DstIP = "203.0.113.1"
	}

	proto := ProtocolTCP
	if strings.EqualFold(req.Protocol, "UDP") {
		proto = ProtocolUDP
	} else if strings.EqualFold(req.Protocol, "ICMP") {
		proto = ProtocolICMP
	}

	pkt := PacketMetadata{
		SrcIP:       req.SrcIP,
		DstIP:       req.DstIP,
		SrcPort:     req.SrcPort,
		DstPort:     req.DstPort,
		Protocol:    proto,
		TCPFlags:    TCPFlags{SYN: req.IsSYN, ACK: req.IsACK},
		PayloadSize: req.PayloadSize,
		Timestamp:   time.Now(),
	}

	decision := h.engine.InspectPacket(pkt, req.TenantID)
	_ = json.NewEncoder(w).Encode(decision)
}

func (h *ScrubberHandler) handleReputation(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing ip query parameter"})
		return
	}
	rec := h.engine.GetReputation(ip)
	_ = json.NewEncoder(w).Encode(rec)
}

func (h *ScrubberHandler) handleChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ip := r.URL.Query().Get("ip")
	tenant := r.URL.Query().Get("tenant")
	if tenant == "" {
		tenant = "default"
	}

	if ip == "" {
		var req struct {
			IP     string `json:"ip"`
			Tenant string `json:"tenant"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req)
		ip = req.IP
		if req.Tenant != "" {
			tenant = req.Tenant
		}
	}

	if ip == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing client ip"})
		return
	}

	tok, err := h.engine.IssueChallenge(ip, tenant)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(tok)
}

func (h *ScrubberHandler) handleChallengeVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Token    string `json:"token"`
		Solution string `json:"solution"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json: " + err.Error()})
		return
	}

	ok, err := h.engine.VerifyChallenge(req.Token, req.Solution)
	if !ok || err != nil {
		w.WriteHeader(http.StatusForbidden)
		msg := "verification failed"
		if err != nil {
			msg = err.Error()
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"verified": false,
			"error":    msg,
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"verified": true,
		"message":  "challenge solved; reputation score restored",
	})
}

func (h *ScrubberHandler) handleListBans(w http.ResponseWriter, r *http.Request) {
	bans := h.engine.ListBans()
	_ = json.NewEncoder(w).Encode(bans)
}

func (h *ScrubberHandler) handleBan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		IP              string `json:"ip"`
		DurationMinutes int    `json:"duration_minutes"`
		Reason          string `json:"reason"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json: " + err.Error()})
		return
	}

	if req.IP == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing ip address"})
		return
	}

	dur := 15 * time.Minute
	if req.DurationMinutes > 0 {
		dur = time.Duration(req.DurationMinutes) * time.Minute
	}
	reason := req.Reason
	if reason == "" {
		reason = "manual administrative quarantine"
	}

	h.engine.BanIP(req.IP, dur, reason)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "banned",
		"ip":      req.IP,
		"expires": time.Now().Add(dur).Format(time.RFC3339),
		"reason":  reason,
	})
}

func (h *ScrubberHandler) handleUnban(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json: " + err.Error()})
		return
	}

	existed := h.engine.UnbanIP(req.IP)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "unbanned",
		"ip":      req.IP,
		"existed": existed,
	})
}

func (h *ScrubberHandler) handlePolicies(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(h.engine.ListTenantPolicies())
}

func (h *ScrubberHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	h.engine.Reset()
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "reset",
		"message": "all scrubber state, rate trackers, and metrics cleared",
	})
}
