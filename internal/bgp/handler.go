package bgp

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BGPHandler exposes REST control and simulation endpoints for the BGP broadcaster.
type BGPHandler struct {
	engine *BGPEngine
}

// NewBGPHandler constructs an HTTP handler routing `/api/bgp/*` requests.
func NewBGPHandler(engine *BGPEngine) *BGPHandler {
	return &BGPHandler{engine: engine}
}

// ServeHTTP dispatches REST requests under `/api/bgp/*`.
func (h *BGPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	p := strings.TrimPrefix(r.URL.Path, "/api/bgp")
	p = strings.TrimPrefix(p, "/")

	switch {
	case p == "status" || p == "":
		h.handleStatus(w, r)
	case p == "peers" && r.Method == http.MethodGet:
		h.handleListPeers(w, r)
	case p == "peers" && r.Method == http.MethodPost:
		h.handleRegisterPeer(w, r)
	case p == "peers/reconnect" && r.Method == http.MethodPost:
		h.handleReconnectPeer(w, r)
	case p == "prefixes" && r.Method == http.MethodGet:
		h.handleListPrefixes(w, r)
	case p == "prefixes/announce" && r.Method == http.MethodPost:
		h.handleAnnouncePrefix(w, r)
	case p == "prefixes/withdraw" && r.Method == http.MethodPost:
		h.handleWithdrawPrefix(w, r)
	case p == "divert" && r.Method == http.MethodPost:
		h.handleDivertPoP(w, r)
	case p == "communities" && r.Method == http.MethodGet:
		h.handleListCommunities(w, r)
	case p == "communities" && r.Method == http.MethodPost:
		h.handleAddCommunity(w, r)
	case p == "history" && r.Method == http.MethodGet:
		h.handleWithdrawalHistory(w, r)
	case p == "stats" && r.Method == http.MethodGet:
		h.handleStats(w, r)
	case p == "reset" && r.Method == http.MethodPost:
		h.handleReset(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *BGPHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":              "active",
		"active_peers":        stats.ActivePeersCount,
		"announced_prefixes":  stats.AnnouncedPrefixes,
		"withdrawn_prefixes":  stats.WithdrawnPrefixes,
		"total_announcements": stats.TotalAnnouncements,
		"total_withdrawals":   stats.TotalWithdrawals,
		"avg_withdrawal_us":   stats.AvgWithdrawalUs,
		"sla_violations":      stats.SLAViolationCount,
	})
}

func (h *BGPHandler) handleListPeers(w http.ResponseWriter, r *http.Request) {
	peers := h.engine.ListPeers()
	_ = json.NewEncoder(w).Encode(peers)
}

func (h *BGPHandler) handleRegisterPeer(w http.ResponseWriter, r *http.Request) {
	var cfg PeerConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&cfg); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if cfg.PeerASN == 0 || cfg.PeerIP == "" {
		http.Error(w, `{"error":"peer_asn and peer_ip are required"}`, http.StatusBadRequest)
		return
	}
	p, err := h.engine.RegisterPeer(cfg)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(p.GetSnapshot())
}

func (h *BGPHandler) handleReconnectPeer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PeerASN uint32 `json:"peer_asn"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}
	p, ok := h.engine.GetPeer(body.PeerASN)
	if !ok {
		http.Error(w, `{"error":"peer session not found"}`, http.StatusNotFound)
		return
	}
	p.ResetSession()
	p.TriggerConnect()
	_ = p.HandleOpen(4, uint16(body.PeerASN), 90)
	_ = json.NewEncoder(w).Encode(p.GetSnapshot())
}

func (h *BGPHandler) handleListPrefixes(w http.ResponseWriter, r *http.Request) {
	prefixes := h.engine.Broadcaster().ListPrefixes()
	_ = json.NewEncoder(w).Encode(prefixes)
}

func (h *BGPHandler) handleAnnouncePrefix(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR        string   `json:"cidr"`
		OriginASN   uint32   `json:"origin_asn"`
		NextHop     string   `json:"next_hop"`
		Communities []string `json:"communities"`
		MED         uint32   `json:"med"`
		LocalPref   uint32   `json:"local_pref"`
		TargetPoPs  []string `json:"target_pops"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if req.CIDR == "" {
		http.Error(w, `{"error":"cidr is required"}`, http.StatusBadRequest)
		return
	}
	if req.OriginASN == 0 {
		req.OriginASN = 64512
	}
	if req.NextHop == "" {
		req.NextHop = "198.51.100.1"
	}
	pr, err := h.engine.AnnouncePrefix(req.CIDR, req.OriginASN, req.NextHop, req.Communities, req.MED, req.LocalPref, req.TargetPoPs)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(pr)
}

func (h *BGPHandler) handleWithdrawPrefix(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR   string `json:"cidr"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if req.CIDR == "" {
		http.Error(w, `{"error":"cidr is required"}`, http.StatusBadRequest)
		return
	}
	if req.Reason == "" {
		req.Reason = "Manual operator withdrawal"
	}
	res, err := h.engine.WithdrawPrefix(req.CIDR, req.Reason)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *BGPHandler) handleDivertPoP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FailedPoP string `json:"failed_pop"`
		TargetPoP string `json:"target_pop"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if req.FailedPoP == "" || req.TargetPoP == "" {
		http.Error(w, `{"error":"failed_pop and target_pop are required"}`, http.StatusBadRequest)
		return
	}
	res, err := h.engine.DivertPoP(req.FailedPoP, req.TargetPoP, req.Reason)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *BGPHandler) handleListCommunities(w http.ResponseWriter, r *http.Request) {
	rules := h.engine.CommunityRouter().ListRules()
	_ = json.NewEncoder(w).Encode(rules)
}

func (h *BGPHandler) handleAddCommunity(w http.ResponseWriter, r *http.Request) {
	var rule TransitPricingRule
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&rule); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if rule.RuleID == "" {
		rule.RuleID = "rule-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	h.engine.CommunityRouter().AddRule(&rule)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(rule)
}

func (h *BGPHandler) handleWithdrawalHistory(w http.ResponseWriter, r *http.Request) {
	history := h.engine.Broadcaster().ListWithdrawalHistory()
	_ = json.NewEncoder(w).Encode(history)
}

func (h *BGPHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *BGPHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	h.engine.Reset()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reset_completed"})
}
