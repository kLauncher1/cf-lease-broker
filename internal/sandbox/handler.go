package sandbox

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const maxRequestBodyBytes = 64 * 1024 // 64KB strict boundary against memory exhaustion DoS

// SandboxHandler provides REST API endpoints for edge sandbox supervisory telemetry and execution.
type SandboxHandler struct {
	engine *SandboxEngine
}

// NewSandboxHandler initializes the HTTP handler for /api/sandbox/* routes.
func NewSandboxHandler(engine *SandboxEngine) *SandboxHandler {
	return &SandboxHandler{
		engine: engine,
	}
}

func (h *SandboxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sandbox")
	path = strings.TrimPrefix(path, "/")

	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "" || path == "status":
		h.handleStatus(w, r)
	case path == "execute":
		h.handleExecute(w, r)
	case path == "tenants":
		h.handleTenants(w, r)
	case path == "crashes":
		h.handleCrashes(w, r)
	case path == "crashes/clear":
		h.handleClearCrashes(w, r)
	case path == "reset":
		h.handleReset(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *SandboxHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ACTIVE",
		"stats":  stats,
	})
}

func (h *SandboxHandler) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var req ExecutionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":"invalid json request payload"}`, http.StatusBadRequest)
		return
	}

	if req.TenantID == "" {
		req.TenantID = "default"
	}
	if req.InstanceID == "" {
		req.InstanceID = "inst-0"
	}
	if req.PayloadType == "" {
		req.PayloadType = PayloadCompute
	}

	res, err := h.engine.Execute(r.Context(), req)
	if err != nil {
		// Even on error, we return structured JSON response describing isolation
		status := http.StatusOK
		if res != nil && res.Status == StatusCrashed {
			status = http.StatusInternalServerError
		} else if res != nil && res.Status == StatusQuarantined {
			status = http.StatusTooManyRequests
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(res)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func (h *SandboxHandler) handleTenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tenantID := r.URL.Query().Get("tenant_id")
		if tenantID != "" {
			pol, found := h.engine.GetTenantPolicy(tenantID)
			if !found {
				http.Error(w, `{"error":"tenant policy not found"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(pol)
			return
		}
		policies := h.engine.ListTenantPolicies()
		_ = json.NewEncoder(w).Encode(policies)

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
		if err != nil {
			http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
			return
		}

		var pol TenantSandboxPolicy
		if err := json.Unmarshal(body, &pol); err != nil {
			http.Error(w, `{"error":"invalid json policy payload"}`, http.StatusBadRequest)
			return
		}

		if pol.TenantID == "" {
			http.Error(w, `{"error":"tenant_id is required"}`, http.StatusBadRequest)
			return
		}

		if err := h.engine.SetTenantPolicy(pol); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "tenant_id": pol.TenantID})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *SandboxHandler) handleCrashes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	if limStr := r.URL.Query().Get("limit"); limStr != "" {
		if parsed, err := strconv.Atoi(limStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	crashes := h.engine.ListCrashes(limit)
	_ = json.NewEncoder(w).Encode(crashes)
}

func (h *SandboxHandler) handleClearCrashes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	h.engine.ClearCrashes()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "crashes_cleared"})
}

func (h *SandboxHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		TenantID   string `json:"tenant_id"`
		InstanceID string `json:"instance_id"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}

	if req.TenantID == "" {
		req.TenantID = r.URL.Query().Get("tenant_id")
	}
	if req.InstanceID == "" {
		req.InstanceID = r.URL.Query().Get("instance_id")
	}

	if req.TenantID == "" {
		http.Error(w, `{"error":"missing tenant_id"}`, http.StatusBadRequest)
		return
	}
	if req.InstanceID == "" {
		req.InstanceID = "inst-0"
	}

	if err := h.engine.ResetInstance(req.TenantID, req.InstanceID); err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":      "reset_complete",
		"tenant_id":   req.TenantID,
		"instance_id": req.InstanceID,
	})
}
