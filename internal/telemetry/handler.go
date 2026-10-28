package telemetry

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// TelemetryHandler provides REST API endpoints for socket telemetry, anomaly inspection, and mitigation.
type TelemetryHandler struct {
	engine *TelemetryEngine
}

// NewTelemetryHandler mounts the telemetry engine HTTP API.
func NewTelemetryHandler(engine *TelemetryEngine) *TelemetryHandler {
	return &TelemetryHandler{
		engine: engine,
	}
}

func (h *TelemetryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/telemetry")
	path = strings.TrimPrefix(path, "/")

	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "" || path == "status":
		h.handleStatus(w, r)
	case path == "sockets":
		h.handleSockets(w, r)
	case path == "anomalies":
		h.handleAnomalies(w, r)
	case path == "mitigations":
		h.handleMitigations(w, r)
	case path == "circuits":
		h.handleCircuits(w, r)
	case path == "simulate":
		h.handleSimulate(w, r)
	case path == "ingest":
		h.handleIngest(w, r)
	case path == "reroute":
		h.handleReroute(w, r)
	case path == "reset":
		h.handleReset(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "not_found",
			"path":  r.URL.Path,
		})
	}
}

func (h *TelemetryHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	stats := h.engine.GetStats()
	mode := h.engine.Driver().GetMode()
	isKernel := h.engine.Driver().IsAvailable()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":                     "active",
		"driver_mode":                mode,
		"kernel_ebpf_active":         isKernel,
		"stats":                      stats,
		"config":                     h.engine.config,
	})
}

func (h *TelemetryHandler) handleSockets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sockets := h.engine.Collector().ListSockets()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sockets": sockets,
		"count":   len(sockets),
	})
}

func (h *TelemetryHandler) handleAnomalies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	anomalies := h.engine.Detector().ListRecentAnomalies(limit)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"anomalies": anomalies,
		"count":     len(anomalies),
	})
}

func (h *TelemetryHandler) handleMitigations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	actions := h.engine.Mitigation().ListActions(limit)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"mitigations": actions,
		"count":       len(actions),
	})
}

func (h *TelemetryHandler) handleCircuits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	circuits := h.engine.Mitigation().ListActiveCircuits()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"circuits": circuits,
		"count":    len(circuits),
	})
}

func (h *TelemetryHandler) handleSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		EndpointID string `json:"endpoint_id"`
		Type       string `json:"type"`
	}

	limited := io.LimitReader(r.Body, 64*1024)
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_json_payload"})
		return
	}

	anomType := AnomalyType(req.Type)
	if anomType == "" {
		anomType = AnomalyWindowCollapse
	}

	anomalies, action := h.engine.SimulateDegradation(req.EndpointID, anomType)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"simulated_type":      anomType,
		"anomalies_detected":  anomalies,
		"mitigation_action":   action,
		"circuit_state":       h.engine.GetCircuitState(req.EndpointID),
	})
}

func (h *TelemetryHandler) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var metrics SocketMetrics
	limited := io.LimitReader(r.Body, 64*1024)
	if err := json.NewDecoder(limited).Decode(&metrics); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_socket_metrics_payload"})
		return
	}

	anomalies, action := h.engine.ProcessMetrics(metrics)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":              "ingested",
		"anomalies_detected":  len(anomalies),
		"mitigation_action":   action,
	})
}

func (h *TelemetryHandler) handleReroute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PrimaryEndpoint string `json:"primary_endpoint"`
		StandbyEndpoint string `json:"standby_endpoint"`
	}

	limited := io.LimitReader(r.Body, 64*1024)
	if err := json.NewDecoder(limited).Decode(&req); err != nil || req.PrimaryEndpoint == "" || req.StandbyEndpoint == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing primary_endpoint or standby_endpoint"})
		return
	}

	h.engine.SetStandbyRoute(req.PrimaryEndpoint, req.StandbyEndpoint)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":           "configured",
		"primary_endpoint": req.PrimaryEndpoint,
		"standby_endpoint": req.StandbyEndpoint,
	})
}

func (h *TelemetryHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	h.engine.Reset()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reset_complete"})
}
