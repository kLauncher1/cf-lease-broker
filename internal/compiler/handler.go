package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// CompilerHandler exposes REST endpoints for bundle compiling, dynamic route injection, and zero-copy streaming.
type CompilerHandler struct {
	bundles *BundleManager
	router  *RouteCompiler
	stream  *StreamPipe
}

// NewCompilerHandler creates an HTTP handler managing worker bundles, routes, and streaming.
func NewCompilerHandler(bundles *BundleManager, router *RouteCompiler, stream *StreamPipe) *CompilerHandler {
	return &CompilerHandler{
		bundles: bundles,
		router:  router,
		stream:  stream,
	}
}

// CompileRequest payload for the on-the-fly minifier and packager.
type CompileRequest struct {
	Script            string            `json:"script"`
	TenantID          string            `json:"tenant_id"`
	Name              string            `json:"name"`
	Minify            bool              `json:"minify"`
	CompatibilityDate string            `json:"compatibility_date"`
	MemoryLimitMB     int               `json:"memory_limit_mb"`
	EnvBindings       map[string]string `json:"env_bindings,omitempty"`
}

// MatchRequest payload for testing URL routing.
type MatchRequest struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

func (h *CompilerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/compiler")
	if path == "" {
		path = "/"
	}

	switch {
	case path == "/compile" && r.Method == http.MethodPost:
		h.handleCompile(w, r)
	case path == "/workers" && r.Method == http.MethodGet:
		h.handleListWorkers(w, r)
	case path == "/workers" && r.Method == http.MethodPost:
		h.handleDeployWorker(w, r)
	case path == "/workers" && r.Method == http.MethodDelete:
		h.handleUndeployWorker(w, r)
	case path == "/routes" && r.Method == http.MethodGet:
		h.handleListRoutes(w, r)
	case path == "/routes" && r.Method == http.MethodPost:
		h.handleRegisterRoute(w, r)
	case path == "/routes" && r.Method == http.MethodDelete:
		h.handleDeleteRoute(w, r)
	case path == "/routes/match" && r.Method == http.MethodPost:
		h.handleMatchRoute(w, r)
	case path == "/stream-test" && r.Method == http.MethodGet:
		h.handleStreamTest(w, r)
	default:
		http.Error(w, `{"error":"compiler endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *CompilerHandler) handleCompile(w http.ResponseWriter, r *http.Request) {
	var req CompileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Script) == "" {
		http.Error(w, `{"error":"script cannot be empty"}`, http.StatusBadRequest)
		return
	}

	minified, hasExport := MinifyJavaScript([]byte(req.Script))
	origLen := len(req.Script)
	minLen := len(minified)
	ratio := 0.0
	if origLen > 0 {
		ratio = float64(origLen-minLen) / float64(origLen) * 100.0
	}

	resp := map[string]any{
		"minified_script":   string(minified),
		"original_bytes":    origLen,
		"minified_bytes":    minLen,
		"savings_percent":   fmt.Sprintf("%.1f%%", ratio),
		"has_valid_export":  hasExport,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *CompilerHandler) handleDeployWorker(w http.ResponseWriter, r *http.Request) {
	var req CompileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	cfg := BundleConfig{
		TenantID:          req.TenantID,
		Name:              req.Name,
		Minify:            req.Minify,
		CompatibilityDate: req.CompatibilityDate,
		MemoryLimitMB:     req.MemoryLimitMB,
		EnvBindings:       req.EnvBindings,
	}

	bundle, err := h.bundles.PackageWorker(cfg, []byte(req.Script), nil)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"package error: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(bundle)
}

func (h *CompilerHandler) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.bundles.ListBundles())
}

func (h *CompilerHandler) handleUndeployWorker(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, `{"error":"missing bundle id parameter"}`, http.StatusBadRequest)
		return
	}

	if err := h.bundles.UndeployBundle(id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"undeployed"}`))
}

func (h *CompilerHandler) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.router.ListRoutes())
}

func (h *CompilerHandler) handleRegisterRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pattern  string `json:"pattern"`
		ZoneID   string `json:"zone_id"`
		TenantID string `json:"tenant_id"`
		WorkerID string `json:"worker_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	route, err := h.router.RegisterRoute(body.Pattern, body.ZoneID, body.TenantID, body.WorkerID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(route)
}

func (h *CompilerHandler) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, `{"error":"missing route id parameter"}`, http.StatusBadRequest)
		return
	}

	if err := h.router.RemoveRoute(id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"removed"}`))
}

func (h *CompilerHandler) handleMatchRoute(w http.ResponseWriter, r *http.Request) {
	var body MatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	match, found := h.router.Match(body.Host, body.Path)
	w.Header().Set("Content-Type", "application/json")
	if !found {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"matched": false,
			"host":    body.Host,
			"path":    body.Path,
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"matched": true,
		"route":   match,
	})
}

func (h *CompilerHandler) handleStreamTest(w http.ResponseWriter, r *http.Request) {
	sizeMB := 2
	if s := r.URL.Query().Get("size_mb"); s != "" {
		if val, err := strconv.Atoi(s); err == nil && val > 0 && val <= 50 {
			sizeMB = val
		}
	}

	totalBytes := int64(sizeMB) * 1024 * 1024
	pattern := []byte("Cloudflare-Edge-Zero-Copy-Stream-Chunk-Data-Buffer\n")
	repeats := int(totalBytes / int64(len(pattern)))
	data := bytes.Repeat(pattern, repeats)

	reader := bytes.NewReader(data)
	_, _ = h.stream.PipeStream(r.Context(), w, reader, "application/octet-stream", int64(len(data)))
}
