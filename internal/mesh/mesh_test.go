package mesh

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHashRing_UniformityAndDistribution(t *testing.T) {
	ring := NewHashRing(128, 3)

	nodes := []PeerNode{
		{ID: "node-iad-1", Region: "iad", Vnodes: 128, Status: NodeStatusActive},
		{ID: "node-sfo-1", Region: "sfo", Vnodes: 128, Status: NodeStatusActive},
		{ID: "node-lhr-1", Region: "lhr", Vnodes: 128, Status: NodeStatusActive},
		{ID: "node-nrt-1", Region: "nrt", Vnodes: 128, Status: NodeStatusActive},
	}

	for _, n := range nodes {
		ring.AddNode(n)
	}

	if ring.VnodeCount() != 4*128 {
		t.Fatalf("Expected 512 vnodes, got %d", ring.VnodeCount())
	}

	// Distribute 10,000 keys and verify balanced allocation
	keyCount := 10000
	counts := make(map[string]int)

	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("token-lease-voucher-%d", i)
		loc, err := ring.LocateKey(key)
		if err != nil {
			t.Fatalf("Failed to locate key %s: %v", key, err)
		}
		counts[loc.PrimaryNode.ID]++
	}

	expectedPerNode := float64(keyCount) / float64(len(nodes))
	for id, count := range counts {
		diff := math.Abs(float64(count) - expectedPerNode)
		skewPct := (diff / expectedPerNode) * 100.0
		// Consistent hashing with 128 vnodes should stay within 25% of uniform mean
		if skewPct > 25.0 {
			t.Errorf("Node %s has high distribution skew: count=%d, expected=%.0f (skew=%.1f%%)",
				id, count, expectedPerNode, skewPct)
		}
	}
}

func TestHashRing_MinimalKeyDisruptionOnNodeRemoval(t *testing.T) {
	ring := NewHashRing(128, 3)

	nodeIDs := []string{"node-1", "node-2", "node-3", "node-4", "node-5"}
	for _, id := range nodeIDs {
		ring.AddNode(PeerNode{ID: id, Region: "iad", Vnodes: 128, Status: NodeStatusActive})
	}

	keyCount := 1000
	initialPlacements := make(map[string]string)

	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("lease-%d", i)
		loc, _ := ring.LocateKey(key)
		initialPlacements[key] = loc.PrimaryNode.ID
	}

	// Remove one node
	removedNode := "node-5"
	ring.RemoveNode(removedNode)

	reassignedCount := 0
	disruptedKeys := 0

	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("lease-%d", i)
		newLoc, _ := ring.LocateKey(key)
		oldNode := initialPlacements[key]

		if oldNode == removedNode {
			reassignedCount++
			if newLoc.PrimaryNode.ID == removedNode {
				t.Fatalf("Key %s still routed to removed node %s", key, removedNode)
			}
		} else if newLoc.PrimaryNode.ID != oldNode {
			disruptedKeys++
		}
	}

	// In consistent hashing, keys not belonging to the removed node should remain on their node
	if disruptedKeys > 0 {
		t.Errorf("Expected 0 unnecessary disruptions, got %d disrupted keys", disruptedKeys)
	}
	if reassignedCount == 0 {
		t.Errorf("Expected some keys originally on %s to be reassigned", removedNode)
	}
}

func TestHashRing_ReplicationFactorDistinctNodes(t *testing.T) {
	ring := NewHashRing(64, 3)

	nodes := []PeerNode{
		{ID: "node-a", Region: "iad", Vnodes: 64, Status: NodeStatusActive},
		{ID: "node-b", Region: "sfo", Vnodes: 64, Status: NodeStatusActive},
		{ID: "node-c", Region: "lhr", Vnodes: 64, Status: NodeStatusActive},
	}
	for _, n := range nodes {
		ring.AddNode(n)
	}

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("replicated-token-%d", i)
		loc, err := ring.LocateKey(key)
		if err != nil {
			t.Fatalf("Unexpected error locating key: %v", err)
		}

		visited := make(map[string]bool)
		visited[loc.PrimaryNode.ID] = true

		for _, rep := range loc.ReplicaNodes {
			if visited[rep.ID] {
				t.Errorf("Duplicate node %s found in replicas for key %s", rep.ID, key)
			}
			visited[rep.ID] = true
		}

		// Total distinct nodes involved should equal replicationFactor (3)
		if len(visited) != 3 {
			t.Errorf("Expected 3 distinct nodes in primary+replicas, got %d", len(visited))
		}
	}
}

func TestGossipProtocol_HMACSignatureAndTampering(t *testing.T) {
	ring := NewHashRing(64, 3)
	secret := []byte("production-mesh-super-secret")
	gossip := NewGossipProtocol("broker-iad-1", "iad", secret, 5*time.Second, 10*time.Second, ring)

	ring.AddNode(PeerNode{ID: "broker-iad-1", Region: "iad", Status: NodeStatusActive})
	ring.AddNode(PeerNode{ID: "broker-lhr-1", Region: "lhr", Status: NodeStatusActive})

	pkt := gossip.CreateGossipPacket()

	if !gossip.VerifySignature(&pkt) {
		t.Fatal("Valid packet failed signature verification")
	}

	// Tamper with packet
	pkt.SenderSeq = 99999
	if gossip.VerifySignature(&pkt) {
		t.Fatal("Tampered packet unexpectedly passed signature verification")
	}
}

func TestGossipProtocol_StateConvergenceAndResurrection(t *testing.T) {
	ring := NewHashRing(64, 3)
	secret := []byte("gossip-secret")
	gossip := NewGossipProtocol("local-node", "iad", secret, 50*time.Millisecond, 100*time.Millisecond, ring)

	peer := PeerNode{
		ID:            "peer-remote-1",
		Address:       "10.0.1.20:9081",
		Region:        "fra",
		Status:        NodeStatusActive,
		HeartbeatSeq:  10,
		LastHeartbeat: time.Now(),
	}
	ring.AddNode(peer)

	// Verify initially active
	n, _ := ring.GetNode("peer-remote-1")
	if n.Status != NodeStatusActive {
		t.Fatalf("Expected active, got %s", n.Status)
	}

	// Wait for heartbeat timeout -> suspect
	time.Sleep(70 * time.Millisecond)
	suspects, _ := gossip.SweepFailures()
	if suspects != 1 {
		t.Errorf("Expected 1 suspect node, got %d", suspects)
	}
	n, _ = ring.GetNode("peer-remote-1")
	if n.Status != NodeStatusSuspect {
		t.Fatalf("Expected suspect, got %s", n.Status)
	}

	// Wait for suspect timeout -> dead
	time.Sleep(60 * time.Millisecond)
	_, dead := gossip.SweepFailures()
	if dead != 1 {
		t.Errorf("Expected 1 dead node, got %d", dead)
	}
	n, _ = ring.GetNode("peer-remote-1")
	if n.Status != NodeStatusDead {
		t.Fatalf("Expected dead, got %s", n.Status)
	}

	// Now peer sends fresh gossip packet with higher heartbeat seq -> resurrected
	resurrectedPeer := PeerNode{
		ID:            "peer-remote-1",
		Address:       "10.0.1.20:9081",
		Region:        "fra",
		Status:        NodeStatusActive,
		HeartbeatSeq:  15,
		LastHeartbeat: time.Now(),
	}

	remoteGossip := NewGossipProtocol("peer-remote-1", "fra", secret, time.Second, time.Second, ring)
	incomingPkt := remoteGossip.CreateGossipPacket()
	incomingPkt.Nodes = []PeerNode{resurrectedPeer}
	incomingPkt.Signature = remoteGossip.ComputeSignature(incomingPkt.SenderID, incomingPkt.SenderSeq, incomingPkt.Timestamp, len(incomingPkt.Nodes))

	ack, err := gossip.ProcessPacket(incomingPkt)
	if err != nil {
		t.Fatalf("Failed to process packet: %v", err)
	}
	if ack.Status != "converged" {
		t.Errorf("Expected converged status, got %s", ack.Status)
	}

	n, _ = ring.GetNode("peer-remote-1")
	if n.Status != NodeStatusActive {
		t.Fatalf("Expected resurrected node to be active, got %s", n.Status)
	}
}

func TestAffinityEngine_RegionalSteering(t *testing.T) {
	affinity := NewAffinityEngine("node-iad", "iad")

	loc := &ShardLocation{
		Key: "user-session-tok",
		PrimaryNode: PeerNode{
			ID:     "node-iad",
			Region: "iad",
			Status: NodeStatusActive,
		},
		ReplicaNodes: []PeerNode{
			{ID: "node-lhr", Region: "lhr", Status: NodeStatusActive},
			{ID: "node-nrt", Region: "nrt", Status: NodeStatusActive},
		},
	}

	// Case 1: Client from Tokyo ('nrt') should steer to 'node-nrt'
	dec1 := affinity.ResolveAffinity("nrt", loc)
	if dec1.SelectedNode.ID != "node-nrt" {
		t.Errorf("Expected selection 'node-nrt', got %s", dec1.SelectedNode.ID)
	}
	if dec1.IsLocalServe {
		t.Error("Expected IsLocalServe to be false for Tokyo client")
	}

	// Case 2: Client from Ashburn/US-East ('iad') should serve locally on 'node-iad'
	dec2 := affinity.ResolveAffinity("iad", loc)
	if dec2.SelectedNode.ID != "node-iad" {
		t.Errorf("Expected selection 'node-iad', got %s", dec2.SelectedNode.ID)
	}
	if !dec2.IsLocalServe {
		t.Error("Expected IsLocalServe to be true for local client")
	}

	// Case 3: Verify distance calculation
	dist := affinity.CalculateRegionDistance("iad", "lhr")
	if dist < 5000 || dist > 7000 {
		t.Errorf("Expected IAD to LHR distance between 5000-7000km, got %.1f km", dist)
	}
}

func TestMeshEngine_LifecycleAndOperations(t *testing.T) {
	cfg := DefaultMeshConfig("engine-local", "iad", "127.0.0.1:9081")
	engine := NewMeshEngine(cfg)
	engine.Start()
	defer engine.Stop()

	// Register peers
	err := engine.RegisterPeer(PeerNode{
		ID:      "peer-lhr",
		Address: "10.0.2.1:9081",
		Region:  "lhr",
		Status:  NodeStatusActive,
	})
	if err != nil {
		t.Fatalf("Failed to register peer: %v", err)
	}

	loc, err := engine.LocateShard("token-9941")
	if err != nil {
		t.Fatalf("Failed to locate shard: %v", err)
	}
	if loc.PrimaryNode.ID == "" {
		t.Error("Primary node ID should not be empty")
	}

	decision, err := engine.ResolveAffinity("lhr", "token-9941")
	if err != nil {
		t.Fatalf("Failed to resolve affinity: %v", err)
	}
	if decision.ClientRegion != "lhr" {
		t.Errorf("Expected region lhr, got %s", decision.ClientRegion)
	}

	stats := engine.GetStats()
	if stats.TotalNodes < 2 {
		t.Errorf("Expected at least 2 nodes, got %d", stats.TotalNodes)
	}
}

func TestMeshHandler_RESTEndpoints(t *testing.T) {
	cfg := DefaultMeshConfig("handler-test-node", "iad", "127.0.0.1:9081")
	engine := NewMeshEngine(cfg)
	handler := NewMeshHandler(engine)

	// 1. GET /api/mesh/status
	req := httptest.NewRequest(http.MethodGet, "/api/mesh/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for status, got %d", rec.Code)
	}

	// 2. POST /api/mesh/nodes
	nodeJSON := `{"id":"peer-test-fra","address":"10.0.3.1:9081","region":"fra","vnodes":64}`
	req = httptest.NewRequest(http.MethodPost, "/api/mesh/nodes", bytes.NewBufferString(nodeJSON))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 for register peer, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. GET /api/mesh/nodes
	req = httptest.NewRequest(http.MethodGet, "/api/mesh/nodes", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for list nodes, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "peer-test-fra") {
		t.Errorf("Expected peer-test-fra in nodes list: %s", rec.Body.String())
	}

	// 4. GET /api/mesh/ring
	req = httptest.NewRequest(http.MethodGet, "/api/mesh/ring", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for ring, got %d", rec.Code)
	}

	// 5. GET /api/mesh/locate?key=test-token-key
	req = httptest.NewRequest(http.MethodGet, "/api/mesh/locate?key=test-token-key", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for locate, got %d", rec.Code)
	}

	// 6. GET /api/mesh/affinity?key=test-token-key&region=fra
	req = httptest.NewRequest(http.MethodGet, "/api/mesh/affinity?key=test-token-key&region=fra", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for affinity, got %d", rec.Code)
	}

	// 7. GET /api/mesh/gossip/packet
	req = httptest.NewRequest(http.MethodGet, "/api/mesh/gossip/packet", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for export gossip packet, got %d", rec.Code)
	}

	// 8. DELETE /api/mesh/nodes?id=peer-test-fra
	req = httptest.NewRequest(http.MethodDelete, "/api/mesh/nodes?id=peer-test-fra", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for delete node, got %d", rec.Code)
	}
}

func TestMeshHandler_RequestBodyBounding(t *testing.T) {
	cfg := DefaultMeshConfig("body-test-node", "iad", "127.0.0.1:9081")
	engine := NewMeshEngine(cfg)
	handler := NewMeshHandler(engine)

	// Oversized payload (> 64KB)
	hugePayload := strings.Repeat("A", 70*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/mesh/nodes", strings.NewReader(hugePayload))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// JSON decoding must fail or be rejected
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for oversized body, got %d", rec.Code)
	}
}

func TestMesh_HighConcurrency(t *testing.T) {
	cfg := DefaultMeshConfig("concurrent-node", "iad", "127.0.0.1:9081")
	engine := NewMeshEngine(cfg)
	engine.Start()
	defer engine.Stop()

	// Seed multiple initial peers
	for i := 0; i < 5; i++ {
		_ = engine.RegisterPeer(PeerNode{
			ID:      fmt.Sprintf("worker-peer-%d", i),
			Address: fmt.Sprintf("10.0.0.%d:9081", i+1),
			Region:  "iad",
			Status:  NodeStatusActive,
		})
	}

	var wg sync.WaitGroup
	workers := 40
	iterations := 100

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := fmt.Sprintf("concurrent-token-%d-%d", workerID, i)
				_, _ = engine.LocateShard(key)
				_, _ = engine.ResolveAffinity("nrt", key)
				_ = engine.GetStats()

				if i%20 == 0 {
					pkt := engine.ExportGossipPacket()
					_ = engine.gossip.VerifySignature(&pkt)
				}
			}
		}(w)
	}

	wg.Wait()
}
