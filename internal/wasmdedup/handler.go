package wasmdedup

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// DedupHandler provides RESTful endpoints under /api/wasmdedup/* with 64KB DoS bounding.
type DedupHandler struct {
	engine *DedupEngine
}

// NewDedupHandler initializes the HTTP router for the Wasm deduplication engine.
func NewDedupHandler(engine *DedupEngine) *DedupHandler {
	return &DedupHandler{engine: engine}
}

// ServeHTTP dispatches incoming requests to specialized sub-handlers.
func (h *DedupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	p := strings.TrimPrefix(r.URL.Path, "/api/wasmdedup")
	p = strings.TrimPrefix(p, "/")

	switch {
	case p == "status" || p == "":
		h.handleStatus(w, r)
	case p == "stats":
		h.handleStats(w, r)
	case p == "instances":
		h.handleListInstances(w, r)
	case p == "instance/spawn":
		h.handleSpawn(w, r)
	case p == "instance/write":
		h.handleWrite(w, r)
	case p == "instance/read":
		h.handleRead(w, r)
	case p == "instance/terminate":
		h.handleTerminate(w, r)
	case p == "template/register":
		h.handleRegisterTemplate(w, r)
	case p == "snapshot/create":
		h.handleCreateSnapshot(w, r)
	case p == "snapshot/resurrect":
		h.handleResurrect(w, r)
	case p == "snapshots":
		h.handleListSnapshots(w, r)
	case p == "compact":
		h.handleCompact(w, r)
	case p == "reset":
		h.handleReset(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *DedupHandler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"engine":          "wasm_dedup_cow",
		"status":          "ACTIVE",
		"dedup_ratio":     stats.DedupRatio,
		"savings_percent": stats.SavingsPercent,
		"saved_bytes":     stats.SavedMemoryBytes,
		"shared_pages":    stats.SharedPhysicalPages,
		"private_pages":   stats.PrivatePhysicalPages,
		"active_instances": stats.ActiveInstances,
		"avg_resurrect_us": stats.AvgResurrectUs,
	})
}

func (h *DedupHandler) handleStats(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(h.engine.GetStats())
}

func (h *DedupHandler) handleListInstances(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(h.engine.ListInstances())
}

type spawnReq struct {
	InstanceID   string `json:"instance_id"`
	TenantID     string `json:"tenant_id"`
	TemplateID   string `json:"template_id"`
	InitialPages int    `json:"initial_pages"`
	MaxPages     int    `json:"max_pages"`
}

func (h *DedupHandler) handleSpawn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req spawnReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
		return
	}

	inst, err := h.engine.SpawnInstance(req.InstanceID, req.TenantID, req.TemplateID, req.InitialPages, req.MaxPages)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"instance_id":   inst.instanceID,
		"tenant_id":     inst.tenantID,
		"virtual_pages": inst.PageCount(),
		"shared_pages":  inst.SharedPageCount(),
		"private_pages": inst.PrivatePageCount(),
	})
}

type writeReq struct {
	InstanceID string `json:"instance_id"`
	Offset     int    `json:"offset"`
	DataHex    string `json:"data_hex"`
	DataText   string `json:"data_text"`
}

func (h *DedupHandler) handleWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req writeReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
		return
	}

	inst, found := h.engine.GetInstance(req.InstanceID)
	if !found {
		http.Error(w, `{"error":"instance not found"}`, http.StatusNotFound)
		return
	}

	var data []byte
	var err error
	if req.DataHex != "" {
		data, err = hex.DecodeString(req.DataHex)
		if err != nil {
			http.Error(w, `{"error":"invalid hex payload"}`, http.StatusBadRequest)
			return
		}
	} else {
		data = []byte(req.DataText)
	}

	if err := inst.Write(req.Offset, data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"instance_id":   inst.instanceID,
		"bytes_written": len(data),
		"cow_faults":    inst.CoWFaults(),
		"shared_pages":  inst.SharedPageCount(),
		"private_pages": inst.PrivatePageCount(),
	})
}

func (h *DedupHandler) handleRead(w http.ResponseWriter, r *http.Request) {
	instanceID := r.URL.Query().Get("instance_id")
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	length, _ := strconv.Atoi(r.URL.Query().Get("length"))
	if length <= 0 {
		length = 64
	}

	inst, found := h.engine.GetInstance(instanceID)
	if !found {
		http.Error(w, `{"error":"instance not found"}`, http.StatusNotFound)
		return
	}

	buf, err := inst.Read(offset, length)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"instance_id": instanceID,
		"offset":      offset,
		"length":      len(buf),
		"data_hex":    hex.EncodeToString(buf),
		"data_text":   string(buf),
	})
}

func (h *DedupHandler) handleTerminate(w http.ResponseWriter, r *http.Request) {
	instanceID := r.URL.Query().Get("instance_id")
	if instanceID == "" {
		var req struct {
			InstanceID string `json:"instance_id"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
		instanceID = req.InstanceID
	}

	if err := h.engine.TerminateInstance(instanceID); err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"instance_id": instanceID,
		"status":      "TERMINATED",
	})
}

func (h *DedupHandler) handleRegisterTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TemplateID string `json:"template_id"`
		DataHex    string `json:"data_hex"`
		DataText   string `json:"data_text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
		return
	}

	var data []byte
	if req.DataHex != "" {
		data, _ = hex.DecodeString(req.DataHex)
	} else {
		data = []byte(req.DataText)
	}

	if err := h.engine.RegisterTemplate(req.TemplateID, data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"template_id": req.TemplateID,
		"status":      "REGISTERED",
	})
}

func (h *DedupHandler) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		InstanceID string            `json:"instance_id"`
		TemplateID string            `json:"template_id"`
		Metadata   map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
		return
	}

	snap, err := h.engine.CreateSnapshot(req.InstanceID, req.TemplateID, req.Metadata)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(snap)
}

func (h *DedupHandler) handleResurrect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SnapshotID    string `json:"snapshot_id"`
		NewInstanceID string `json:"new_instance_id"`
		MaxPages      int    `json:"max_pages"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
		return
	}

	inst, elapsed, err := h.engine.Resurrect(req.SnapshotID, req.NewInstanceID, req.MaxPages)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"instance_id":    inst.instanceID,
		"latency_us":     elapsed.Microseconds(),
		"virtual_pages":  inst.PageCount(),
		"shared_pages":   inst.SharedPageCount(),
		"sub_millisecond": elapsed.Microseconds() < 1000,
	})
}

func (h *DedupHandler) handleListSnapshots(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(h.engine.snapshotMgr.ListSnapshots())
}

func (h *DedupHandler) handleCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	res := h.engine.RunCompaction()
	_ = json.NewEncoder(w).Encode(res)
}

func (h *DedupHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	h.engine.Reset()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "RESET_COMPLETE"})
}
