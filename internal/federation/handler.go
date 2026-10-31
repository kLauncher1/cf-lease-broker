package federation

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// FederationHandler mounts RESTful HTTP endpoints under /api/federation/*.
type FederationHandler struct {
	engine *FederationEngine
}

// NewFederationHandler creates an HTTP handler bound to a FederationEngine.
func NewFederationHandler(engine *FederationEngine) *FederationHandler {
	return &FederationHandler{engine: engine}
}

// ServeHTTP routes incoming requests under /api/federation/*.
func (h *FederationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	subPath := strings.TrimPrefix(r.URL.Path, "/api/federation")
	subPath = strings.TrimPrefix(subPath, "/")

	switch {
	case subPath == "" || subPath == "status" || subPath == "overview":
		h.handleStatus(w, r)
	case subPath == "clusters" || strings.HasPrefix(subPath, "clusters/"):
		h.handleClusters(w, r)
	case subPath == "tunnels" || strings.HasPrefix(subPath, "tunnels/"):
		h.handleTunnels(w, r, subPath)
	case subPath == "routes" || strings.HasPrefix(subPath, "routes/"):
		h.handleRoutes(w, r, subPath)
	case subPath == "sovereignty" || strings.HasPrefix(subPath, "sovereignty/"):
		h.handleSovereignty(w, r, subPath)
	case subPath == "reset":
		h.handleReset(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "endpoint_not_found",
			"subpath": subPath,
		})
	}
}

func (h *FederationHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *FederationHandler) handleClusters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" {
			c, found := h.engine.GetCluster(id)
			if !found {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "cluster not found"})
				return
			}
			_ = json.NewEncoder(w).Encode(c)
			return
		}
		_ = json.NewEncoder(w).Encode(h.engine.ListClusters())

	case http.MethodPost:
		var c ClusterNode
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&c); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := h.engine.RegisterCluster(&c); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "registered", "cluster": c})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing id query parameter"})
			return
		}
		if err := h.engine.DeregisterCluster(id); err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deregistered", "id": id})

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *FederationHandler) handleTunnels(w http.ResponseWriter, r *http.Request, subPath string) {
	tm := h.engine.TunnelManager()

	switch {
	case subPath == "tunnels/probe" && r.Method == http.MethodPost:
		var req struct {
			TunnelID  string  `json:"tunnel_id"`
			LatencyMs float64 `json:"latency_ms"`
			LossPct   float64 `json:"loss_pct"`
			JitterMs  float64 `json:"jitter_ms"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		tun, err := tm.SimulateProbe(req.TunnelID, req.LatencyMs, req.LossPct, req.JitterMs)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "probed", "tunnel": tun})

	case subPath == "tunnels/handshake" && r.Method == http.MethodPost:
		var req struct {
			TunnelID      string `json:"tunnel_id"`
			PeerPublicKey string `json:"peer_public_key"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := tm.PerformHandshake(req.TunnelID, req.PeerPublicKey); err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		tun, _ := tm.GetTunnel(req.TunnelID)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "handshake_completed", "tunnel": tun})

	case subPath == "tunnels" && r.Method == http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" {
			tun, found := tm.GetTunnel(id)
			if !found {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "tunnel not found"})
				return
			}
			_ = json.NewEncoder(w).Encode(tun)
			return
		}
		_ = json.NewEncoder(w).Encode(tm.ListTunnels())

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *FederationHandler) handleRoutes(w http.ResponseWriter, r *http.Request, subPath string) {
	router := h.engine.OverlayRouter()

	switch {
	case subPath == "routes/resolve" && r.Method == http.MethodPost:
		var req struct {
			TenantID       string             `json:"tenant_id"`
			Destination    string             `json:"destination"`
			ClientRegion   string             `json:"client_region"`
			Classification DataClassification `json:"classification"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		dec, err := h.engine.ResolveFederationRoute(req.TenantID, req.Destination, req.ClientRegion, req.Classification)
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":    err.Error(),
				"decision": dec,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(dec)

	case subPath == "routes" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(router.ListRoutes())

	case (subPath == "routes" || subPath == "routes/advertise") && r.Method == http.MethodPost:
		var rt OverlayRoute
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&rt); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := router.AdvertiseRoute(&rt); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "advertised", "route": rt})

	case subPath == "routes" && r.Method == http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing id query parameter"})
			return
		}
		if err := router.WithdrawRoute(id); err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "withdrawn", "id": id})

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *FederationHandler) handleSovereignty(w http.ResponseWriter, r *http.Request, subPath string) {
	sov := h.engine.SovereigntyValidator()

	switch {
	case (subPath == "sovereignty" || subPath == "sovereignty/rules") && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(sov.ListRules())

	case (subPath == "sovereignty" || subPath == "sovereignty/rules") && r.Method == http.MethodPost:
		var rule SovereigntyRule
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&rule); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := sov.AddRule(&rule); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "rule_added", "rule": rule})

	case (subPath == "sovereignty" || subPath == "sovereignty/rules") && r.Method == http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing id query parameter"})
			return
		}
		if err := sov.RemoveRule(id); err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "rule_removed", "id": id})

	case subPath == "sovereignty/validate" && r.Method == http.MethodPost:
		var req struct {
			TenantID        string             `json:"tenant_id"`
			TargetClusterID string             `json:"target_cluster_id"`
			TargetRegion    string             `json:"target_region"`
			Classification  DataClassification `json:"classification"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		res := sov.ValidateCompliance(req.TenantID, req.TargetClusterID, req.TargetRegion, req.Classification)
		if !res.Allowed {
			w.WriteHeader(http.StatusForbidden)
		}
		_ = json.NewEncoder(w).Encode(res)

	case subPath == "sovereignty/audit" && r.Method == http.MethodGet:
		limit := 50
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}
		_ = json.NewEncoder(w).Encode(sov.ListAuditLogs(limit))

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *FederationHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	h.engine.SeedDefaultTopology()
	h.engine.SovereigntyValidator().ClearAuditLogs()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "topology_reset_to_default"})
}
