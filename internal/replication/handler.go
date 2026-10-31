package replication

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ReplicationHandler exposes REST API endpoints for the geo-replication engine.
type ReplicationHandler struct {
	engine *GeoReplicationEngine
}

// NewReplicationHandler instantiates a new HTTP handler for the replication subsystem.
func NewReplicationHandler(engine *GeoReplicationEngine) *ReplicationHandler {
	return &ReplicationHandler{
		engine: engine,
	}
}

// ServeHTTP routes incoming requests under /api/replication/* with strict 64KB body bounding.
func (h *ReplicationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/replication")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "status" && r.Method == http.MethodGet:
		h.handleStatus(w, r)
	case path == "put" && r.Method == http.MethodPost:
		h.handlePut(w, r)
	case path == "get" && r.Method == http.MethodGet:
		h.handleGet(w, r)
	case path == "delete" && (r.Method == http.MethodDelete || r.Method == http.MethodPost):
		h.handleDelete(w, r)
	case path == "crdt/counter" && r.Method == http.MethodPost:
		h.handleCounter(w, r)
	case path == "crdt/set" && r.Method == http.MethodPost:
		h.handleSet(w, r)
	case path == "sync" && r.Method == http.MethodPost:
		h.handleSync(w, r)
	case path == "export" && r.Method == http.MethodGet:
		h.handleExport(w, r)
	case path == "peers" && r.Method == http.MethodGet:
		h.handlePeers(w, r)
	case path == "stats" && r.Method == http.MethodGet:
		h.handleStats(w, r)
	case path == "reset" && r.Method == http.MethodPost:
		h.handleReset(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "unknown replication endpoint",
			"path":  r.URL.Path,
		})
	}
}

func (h *ReplicationHandler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	stats := h.engine.Stats()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "online",
		"stats":  stats,
		"peers":  h.engine.ListPeers(),
	})
}

type putPayload struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value"`
}

func (h *ReplicationHandler) handlePut(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request body too large or unreadable"})
		return
	}

	var req putPayload
	if err := json.Unmarshal(body, &req); err != nil || req.Key == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload: namespace and key are required"})
		return
	}
	if req.Namespace == "" {
		req.Namespace = "default"
	}

	clientCtx := ExtractSessionContext(r)
	entry, err := h.engine.Put(req.Namespace, req.Key, []byte(req.Value), clientCtx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	AttachConsistencyHeaders(w, clientCtx, entry.VectorClock)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(entry)
}

func (h *ReplicationHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing key query parameter"})
		return
	}

	clientCtx := ExtractSessionContext(r)
	entry, err := h.engine.Get(namespace, key, clientCtx)
	if err != nil {
		if err == ErrKeyNotFound {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err == ErrStaleReadViolation {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	AttachConsistencyHeaders(w, clientCtx, entry.VectorClock)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"namespace":    entry.Namespace,
		"key":          entry.Key,
		"value":        string(entry.Value),
		"vector_clock": entry.VectorClock,
		"timestamp":    entry.Timestamp,
		"node_id":      entry.NodeID,
	})
}

func (h *ReplicationHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing key query parameter"})
		return
	}

	clientCtx := ExtractSessionContext(r)
	if err := h.engine.Delete(namespace, key, clientCtx); err != nil {
		if err == ErrKeyNotFound {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "key": key})
}

type counterPayload struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Delta     int64  `json:"delta"`
}

func (h *ReplicationHandler) handleCounter(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request body too large"})
		return
	}

	var req counterPayload
	if err := json.Unmarshal(body, &req); err != nil || req.Key == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload: key required"})
		return
	}
	if req.Namespace == "" {
		req.Namespace = "default"
	}

	clientCtx := ExtractSessionContext(r)
	val, err := h.engine.UpdateCounter(req.Namespace, req.Key, req.Delta, clientCtx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"namespace": req.Namespace,
		"key":       req.Key,
		"value":     val,
		"crdt_type": "pn_counter",
	})
}

type setPayload struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Op        string `json:"op"`
	Element   string `json:"element"`
}

func (h *ReplicationHandler) handleSet(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request body too large"})
		return
	}

	var req setPayload
	if err := json.Unmarshal(body, &req); err != nil || req.Key == "" || req.Element == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid payload: key and element are required"})
		return
	}
	if req.Namespace == "" {
		req.Namespace = "default"
	}

	clientCtx := ExtractSessionContext(r)
	elems, err := h.engine.UpdateSet(req.Namespace, req.Key, req.Op, req.Element, clientCtx)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"namespace": req.Namespace,
		"key":       req.Key,
		"elements":  elems,
		"crdt_type": "or_set",
	})
}

func (h *ReplicationHandler) handleSync(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request body too large"})
		return
	}

	var pkt ReplicationPacket
	if err := json.Unmarshal(body, &pkt); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid replication packet format"})
		return
	}

	ack, err := h.engine.ProcessSyncPacket(&pkt)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ack)
}

func (h *ReplicationHandler) handleExport(w http.ResponseWriter, _ *http.Request) {
	pkt := h.engine.GenerateSyncPacket(nil)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(pkt)
}

func (h *ReplicationHandler) handlePeers(w http.ResponseWriter, _ *http.Request) {
	peers := h.engine.ListPeers()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"count": len(peers),
		"peers": peers,
	})
}

func (h *ReplicationHandler) handleStats(w http.ResponseWriter, _ *http.Request) {
	stats := h.engine.Stats()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *ReplicationHandler) handleReset(w http.ResponseWriter, _ *http.Request) {
	h.engine.ResetStore()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reset_completed"})
}
