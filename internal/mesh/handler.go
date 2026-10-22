package mesh

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// MeshHandler provides HTTP REST endpoints for inspecting and controlling the edge mesh.
type MeshHandler struct {
	engine *MeshEngine
}

// NewMeshHandler constructs an HTTP handler backed by the mesh engine.
func NewMeshHandler(engine *MeshEngine) *MeshHandler {
	return &MeshHandler{
		engine: engine,
	}
}

// ServeHTTP routes incoming requests under /api/mesh/* with strict 64KB body bounds.
func (h *MeshHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/mesh")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "status" || path == "":
		h.handleStatus(w, r)
	case path == "nodes":
		h.handleNodes(w, r)
	case path == "ring":
		h.handleRing(w, r)
	case path == "locate":
		h.handleLocate(w, r)
	case path == "affinity":
		h.handleAffinity(w, r)
	case path == "gossip":
		h.handleGossip(w, r)
	case path == "gossip/packet":
		h.handleExportGossip(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "mesh endpoint not found"})
	}
}

func (h *MeshHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	stats := h.engine.GetStats()
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *MeshHandler) handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		nodes := h.engine.ListNodes()
		_ = json.NewEncoder(w).Encode(nodes)

	case http.MethodPost:
		limited := io.LimitReader(r.Body, 64*1024)
		var node PeerNode
		if err := json.NewDecoder(limited).Decode(&node); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid peer node json payload"})
			return
		}
		if err := h.engine.RegisterPeer(node); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered", "node_id": node.ID})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing id parameter"})
			return
		}
		if err := h.engine.DeregisterPeer(id); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deregistered", "node_id": id})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *MeshHandler) handleRing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	dist := h.engine.GetRingDistribution()
	resp := map[string]interface{}{
		"total_vnodes":       h.engine.ring.VnodeCount(),
		"active_nodes":       h.engine.ring.ActiveNodeCount(),
		"replication_factor": h.engine.cfg.ReplicationFactor,
		"distribution":       dist,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *MeshHandler) handleLocate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing key parameter"})
		return
	}

	loc, err := h.engine.LocateShard(key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(loc)
}

func (h *MeshHandler) handleAffinity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	key := r.URL.Query().Get("key")
	region := r.URL.Query().Get("region")
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing key parameter"})
		return
	}

	decision, err := h.engine.ResolveAffinity(region, key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(decision)
}

func (h *MeshHandler) handleGossip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	limited := io.LimitReader(r.Body, 64*1024)
	var pkt GossipPacket
	if err := json.NewDecoder(limited).Decode(&pkt); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid gossip packet payload"})
		return
	}

	ack, err := h.engine.ProcessIncomingGossip(pkt)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(ack)
}

func (h *MeshHandler) handleExportGossip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	pkt := h.engine.ExportGossipPacket()
	_ = json.NewEncoder(w).Encode(pkt)
}
