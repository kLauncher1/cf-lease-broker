package radar

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// RadarHandler serves HTTP REST endpoints under /api/radar/*.
type RadarHandler struct {
	router      *RadarRouter
	probeEngine *ProbeEngine
}

// NewRadarHandler creates a new HTTP handler for Radar APIs.
func NewRadarHandler(router *RadarRouter, probeEngine *ProbeEngine) *RadarHandler {
	return &RadarHandler{
		router:      router,
		probeEngine: probeEngine,
	}
}

// ServeHTTP multiplexes incoming radar REST requests.
func (h *RadarHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	p := strings.TrimPrefix(r.URL.Path, "/api/radar")
	p = strings.TrimPrefix(p, "/")

	switch {
	case p == "status" || p == "":
		h.handleStatus(w, r)
	case p == "pops":
		h.handlePoPs(w, r)
	case p == "probe":
		h.handleProbe(w, r)
	case p == "route":
		h.handleRoute(w, r)
	case p == "events":
		h.handleEvents(w, r)
	case p == "migrate":
		h.handleMigrate(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *RadarHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	status := h.router.GetStatusSummary()
	_ = json.NewEncoder(w).Encode(status)
}

type popWithStats struct {
	PoPInfo
	Stats ProbeStats `json:"stats"`
}

func (h *RadarHandler) handlePoPs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	region := r.URL.Query().Get("region")
	popList := DefaultPoPList()
	if region != "" {
		popList = ListPoPsByRegion(region)
	}

	allStats := h.probeEngine.GetAllStats()
	results := make([]popWithStats, 0, len(popList))
	for _, p := range popList {
		st, ok := allStats[p.IATA]
		if !ok {
			st = ProbeStats{Health: HealthHealthy, EMARTTMs: 25.0}
		}
		results = append(results, popWithStats{
			PoPInfo: p,
			Stats:   st,
		})
	}

	_ = json.NewEncoder(w).Encode(results)
}

type probeReq struct {
	Colo        string `json:"colo"`
	Concurrency int    `json:"concurrency"`
}

func (h *RadarHandler) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req probeReq
	if r.Body != nil {
		limited := io.LimitReader(r.Body, 64*1024)
		_ = json.NewDecoder(limited).Decode(&req)
	}
	if req.Colo == "" {
		req.Colo = r.URL.Query().Get("colo")
	}

	if req.Colo != "" {
		rtt, err := h.probeEngine.ProbePoP(strings.ToUpper(req.Colo))
		if err != nil {
			http.Error(w, `{"error":"failed to probe colo: `+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"colo":    strings.ToUpper(req.Colo),
			"rtt_ms":  rtt,
			"success": err == nil,
		})
		return
	}

	// Probe all PoPs
	results := h.probeEngine.ProbeAll(req.Concurrency)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"probed_count": len(results),
		"results":      results,
	})
}

type routeReq struct {
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Country  string  `json:"country"`
	TenantID string  `json:"tenant_id"`
}

func (h *RadarHandler) handleRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req routeReq
	if r.Method == http.MethodPost && r.Body != nil {
		limited := io.LimitReader(r.Body, 64*1024)
		_ = json.NewDecoder(limited).Decode(&req)
	}

	// Query parameter overrides
	if latStr := r.URL.Query().Get("lat"); latStr != "" {
		if lat, err := strconv.ParseFloat(latStr, 64); err == nil {
			req.Lat = lat
		}
	}
	if lonStr := r.URL.Query().Get("lon"); lonStr != "" {
		if lon, err := strconv.ParseFloat(lonStr, 64); err == nil {
			req.Lon = lon
		}
	}
	if country := r.URL.Query().Get("country"); country != "" {
		req.Country = country
	}
	if tenant := r.URL.Query().Get("tenant"); tenant != "" {
		req.TenantID = tenant
	}

	loc := GeoPoint{Lat: req.Lat, Lon: req.Lon}
	if loc.Lat == 0.0 && loc.Lon == 0.0 && req.Country != "" {
		if geo, found := CountryToGeo(req.Country); found {
			loc = geo
		}
	}

	var rec RouteRecommendation
	if req.TenantID != "" {
		rec = h.router.ResolveTenantColo(req.TenantID, loc)
	} else {
		rec = h.router.ResolveOptimalColo(loc, false)
	}

	_ = json.NewEncoder(w).Encode(rec)
}

func (h *RadarHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	bgp := h.router.GetRecentBGPEvents(limit)
	mig := h.router.GetRecentMigrations(limit)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"bgp_events":        bgp,
		"migration_events":  mig,
		"total_bgp_count":   len(bgp),
		"total_mig_count":   len(mig),
	})
}

type migrateReq struct {
	FromColo string `json:"from_colo"`
	Reason   string `json:"reason"`
}

func (h *RadarHandler) handleMigrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req migrateReq
	limited := io.LimitReader(r.Body, 64*1024)
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON request: `+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	if req.FromColo == "" {
		http.Error(w, `{"error":"from_colo is required"}`, http.StatusBadRequest)
		return
	}
	if req.Reason == "" {
		req.Reason = "Manual operator migration"
	}

	events := h.router.MigrateLeasesFromColo(strings.ToUpper(req.FromColo), req.Reason)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "migrated",
		"migrated_leases": len(events),
		"events":          events,
	})
}
