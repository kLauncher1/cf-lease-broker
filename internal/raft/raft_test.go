package raft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

func TestJournal_AppendAndIntegrity(t *testing.T) {
	j, err := NewJournal("")
	if err != nil {
		t.Fatalf("failed to create journal: %v", err)
	}

	entry1, err := j.Append(1, CmdGrantLease, []byte(`{"lease_id":"lse_001"}`))
	if err != nil {
		t.Fatalf("failed to append entry1: %v", err)
	}
	if entry1.Index != 1 || entry1.Term != 1 {
		t.Fatalf("unexpected entry1 index/term: %d/%d", entry1.Index, entry1.Term)
	}
	if entry1.Checksum == "" {
		t.Fatal("expected non-empty SHA-256 checksum")
	}

	entry2, err := j.Append(1, CmdUpdateQuota, []byte(`{"account_id":"acc1","worker_delta":100}`))
	if err != nil {
		t.Fatalf("failed to append entry2: %v", err)
	}
	if entry2.Index != 2 {
		t.Fatalf("expected index 2, got %d", entry2.Index)
	}

	if j.LastIndex() != 2 {
		t.Fatalf("expected LastIndex 2, got %d", j.LastIndex())
	}

	all := j.AllEntries()
	if len(all) != 3 { // dummy entry 0 + 2 items
		t.Fatalf("expected 3 total entries in journal, got %d", len(all))
	}
}

func TestJournal_AppendEntriesTruncation(t *testing.T) {
	j, _ := NewJournal("")
	_, _ = j.Append(1, "TYPE1", []byte("data1"))
	_, _ = j.Append(1, "TYPE2", []byte("data2"))
	_, _ = j.Append(1, "TYPE3", []byte("data3"))

	// Verify out-of-order rejection
	ok, _ := j.AppendEntries(10, 1, nil)
	if ok {
		t.Fatal("expected rejection when prevLogIndex is beyond last index")
	}

	// Overwrite entry at index 3 with higher term
	newEntries := []LogEntry{
		{Term: 2, Index: 3, Type: "TYPE3_NEW", Data: []byte("data3_new"), Timestamp: time.Now().UTC()},
		{Term: 2, Index: 4, Type: "TYPE4_NEW", Data: []byte("data4_new"), Timestamp: time.Now().UTC()},
	}

	success, matchIdx := j.AppendEntries(2, 1, newEntries)
	if !success || matchIdx != 4 {
		t.Fatalf("expected append success with matchIdx 4, got %v / %d", success, matchIdx)
	}

	ent3, _ := j.GetEntry(3)
	if ent3.Term != 2 || string(ent3.Data) != "data3_new" {
		t.Fatalf("expected truncated and updated entry 3, got %+v", ent3)
	}
}

func TestJournal_DiskPersistenceAndRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "raft_wal_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	j1, err := NewJournal(tmpDir)
	if err != nil {
		t.Fatalf("failed to initialize WAL journal: %v", err)
	}

	_, _ = j1.Append(1, CmdGrantLease, []byte(`{"lease_id":"disk_01"}`))
	_, _ = j1.Append(2, CmdUpdateQuota, []byte(`{"account_id":"acc1"}`))
	_ = j1.Close()

	// Reopen from disk
	j2, err := NewJournal(tmpDir)
	if err != nil {
		t.Fatalf("failed to restore journal from disk: %v", err)
	}
	defer j2.Close()

	if j2.LastIndex() != 2 {
		t.Fatalf("expected restored last index 2, got %d", j2.LastIndex())
	}
	recoveredEnt, err := j2.GetEntry(1)
	if err != nil || string(recoveredEnt.Data) != `{"lease_id":"disk_01"}` {
		t.Fatalf("recovered entry mismatch: %v", recoveredEnt)
	}
}

func TestPeerDiscovery_StaticAndHealth(t *testing.T) {
	static := map[string]string{
		"node-1": "127.0.0.1:9081",
		"node-2": "http://127.0.0.1:9082",
	}
	pd := NewPeerDiscovery("node-1", "", static)

	peers := pd.DiscoverPeers()
	if len(peers) != 1 || peers["node-2"] != "http://127.0.0.1:9082" {
		t.Fatalf("unexpected discovered peers: %+v", peers)
	}

	pd.RecordHeartbeat("node-2", 1500*time.Microsecond, true)
	statusList := pd.GetPeerStatusList()
	if len(statusList) != 1 || !statusList[0].Active || statusList[0].LatencyMs <= 0 {
		t.Fatalf("unexpected peer status: %+v", statusList)
	}

	pd.RegisterPeer("node-3", "127.0.0.1:9083")
	if len(pd.DiscoverPeers()) != 2 {
		t.Fatalf("expected 2 discovered peers after manual registration")
	}
}

func TestBrokerFSM_ApplyAllCommandsAndSnapshot(t *testing.T) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	fsm := NewBrokerFSM(pm, le)

	// 1. CmdGrantLease
	voucherJSON := []byte(`{
		"lease_id": "lse_raft_01",
		"account_id": "cf-tenant-alpha",
		"target_service": "auth-svc",
		"resource_type": "Workers",
		"token_key": "cftok_12345",
		"endpoint_url": "https://auth.cf-tenant-alpha.workers.dev"
	}`)
	_, err := fsm.Apply(LogEntry{Type: CmdGrantLease, Data: voucherJSON})
	if err != nil {
		t.Fatalf("CmdGrantLease failed: %v", err)
	}
	leases := le.ListLeases()
	if len(leases) != 1 || leases[0].LeaseID != "lse_raft_01" {
		t.Fatalf("lease not registered in LeaseEngine: %+v", leases)
	}

	// 2. CmdUpdateQuota
	quotaJSON := []byte(`{"account_id":"cf-tenant-alpha","worker_delta":500,"r2_delta_mb":12.5,"kv_read_delta":3}`)
	_, err = fsm.Apply(LogEntry{Type: CmdUpdateQuota, Data: quotaJSON})
	if err != nil {
		t.Fatalf("CmdUpdateQuota failed: %v", err)
	}
	acc, _ := pm.GetAccount("cf-tenant-alpha")
	if acc.WorkerUsed != 14700 { // Pre-seeded 14200 + 500
		t.Fatalf("expected WorkerUsed 14700, got %d", acc.WorkerUsed)
	}

	// 3. CmdRevokeLease
	revokeJSON := []byte(`{"lease_id":"lse_raft_01"}`)
	_, err = fsm.Apply(LogEntry{Type: CmdRevokeLease, Data: revokeJSON})
	if err != nil {
		t.Fatalf("CmdRevokeLease failed: %v", err)
	}
	leases = le.ListLeases()
	if len(leases) == 0 || !leases[0].IsRevoked {
		t.Fatalf("lease should be revoked: %+v", leases)
	}

	// 4. Snapshot & Restore
	snapBytes, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	newPM := broker.NewPoolManager()
	newLE := broker.NewLeaseEngine(newPM)
	newFSM := NewBrokerFSM(newPM, newLE)

	if err := newFSM.Restore(snapBytes); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	restoredLeases := newLE.ListLeases()
	if len(restoredLeases) != 1 || restoredLeases[0].LeaseID != "lse_raft_01" {
		t.Fatalf("restored leases mismatch: %+v", restoredLeases)
	}
}

func TestRaftNode_SingleNodeLifecycle(t *testing.T) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	fsm := NewBrokerFSM(pm, le)
	j, _ := NewJournal("")
	pd := NewPeerDiscovery("node-single", "", nil)

	cfg := DefaultNodeConfig("node-single")
	cfg.ElectionTimeoutMin = 50 * time.Millisecond
	cfg.ElectionTimeoutMax = 100 * time.Millisecond
	cfg.HeartbeatInterval = 30 * time.Millisecond

	node := NewRaftNode(cfg, j, pd, fsm)
	node.Start()
	defer node.Stop()

	// Wait for single node to become leader
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		status := node.Status()
		if status.Role == RoleLeader {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if node.Status().Role != RoleLeader {
		t.Fatalf("expected node to become leader, got %s", node.Status().Role)
	}

	// Propose command
	idx, err := node.Propose(CmdUpdateQuota, []byte(`{"account_id":"cf-tenant-alpha","worker_delta":200}`))
	if err != nil {
		t.Fatalf("failed to propose command: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected proposed index 1, got %d", idx)
	}

	time.Sleep(60 * time.Millisecond)
	acc, _ := pm.GetAccount("cf-tenant-alpha")
	if acc.WorkerUsed != 14400 {
		t.Fatalf("expected WorkerUsed 14400, got %d", acc.WorkerUsed)
	}
}

func TestRaftNode_ThreeNodeClusterElectionAndFailover(t *testing.T) {
	nodeIDs := []string{"node-a", "node-b", "node-c"}
	servers := make(map[string]*httptest.Server)
	nodes := make(map[string]*RaftNode)
	journals := make(map[string]*Journal)
	fsms := make(map[string]*BrokerFSM)
	peerMap := make(map[string]string)

	// Step 1: Create dummy listeners so we know all addresses
	handlers := make(map[string]*RaftHandler)
	for _, id := range nodeIDs {
		h := &RaftHandler{}
		handlers[id] = h
		srv := httptest.NewServer(h)
		servers[id] = srv
		peerMap[id] = srv.URL
	}
	defer func() {
		for _, srv := range servers {
			srv.Close()
		}
	}()

	// Step 2: Initialize nodes with real peer addresses
	for _, id := range nodeIDs {
		pm := broker.NewPoolManager()
		le := broker.NewLeaseEngine(pm)
		fsm := NewBrokerFSM(pm, le)
		fsms[id] = fsm

		j, _ := NewJournal("")
		journals[id] = j

		pd := NewPeerDiscovery(id, "", peerMap)

		cfg := DefaultNodeConfig(id)
		cfg.ElectionTimeoutMin = 80 * time.Millisecond
		cfg.ElectionTimeoutMax = 180 * time.Millisecond
		cfg.HeartbeatInterval = 30 * time.Millisecond

		rn := NewRaftNode(cfg, j, pd, fsm)
		nodes[id] = rn

		// Bind handler
		realHandler := NewRaftHandler(rn, j, pd)
		*handlers[id] = *realHandler

		rn.Start()
	}
	defer func() {
		for _, rn := range nodes {
			rn.Stop()
		}
	}()

	// Step 3: Wait for cluster to elect a leader
	var leaderID string
	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, id := range nodeIDs {
			if nodes[id].Status().Role == RoleLeader {
				leaderID = id
				break
			}
		}
		if leaderID != "" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	if leaderID == "" {
		t.Fatal("timed out waiting for 3-node cluster leader election")
	}

	leaderNode := nodes[leaderID]

	// Step 4: Propose command on leader and verify replication across quorum
	voucherData := []byte(fmt.Sprintf(`{"lease_id":"lse_clustered_1","account_id":"cf-tenant-alpha","target_service":"test-svc"}`))
	propIdx, err := leaderNode.Propose(CmdGrantLease, voucherData)
	if err != nil {
		t.Fatalf("proposal on leader failed: %v", err)
	}
	if propIdx == 0 {
		t.Fatal("expected non-zero proposal index")
	}

	// Wait for replication and commit on majority
	time.Sleep(150 * time.Millisecond)
	committedCount := 0
	for _, id := range nodeIDs {
		if journals[id].CommitIndex() >= propIdx {
			committedCount++
		}
	}
	if committedCount < 2 {
		t.Fatalf("expected majority (>=2) commit, got %d", committedCount)
	}

	// Step 5: Leader Failover test - stop the leader
	leaderNode.Stop()
	servers[leaderID].Close()

	// Wait for remaining 2 nodes to elect a new leader
	var newLeaderID string
	failoverDeadline := time.Now().Add(3000 * time.Millisecond)
	for time.Now().Before(failoverDeadline) {
		for _, id := range nodeIDs {
			if id == leaderID {
				continue
			}
			if nodes[id].Status().Role == RoleLeader {
				newLeaderID = id
				break
			}
		}
		if newLeaderID != "" {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}

	if newLeaderID == "" {
		t.Fatal("timed out waiting for leader failover")
	}
	if newLeaderID == leaderID {
		t.Fatal("new leader should not be the failed leader")
	}

	// New leader must be able to propose and commit with majority (2 of 3)
	_, err = nodes[newLeaderID].Propose(CmdUpdateQuota, []byte(`{"account_id":"cf-tenant-bravo","worker_delta":100}`))
	if err != nil {
		t.Fatalf("failed to propose on new failover leader: %v", err)
	}
}

func TestRaftHandler_HTTPEndpoints(t *testing.T) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	fsm := NewBrokerFSM(pm, le)
	j, _ := NewJournal("")
	pd := NewPeerDiscovery("node-test", "", nil)

	cfg := DefaultNodeConfig("node-test")
	node := NewRaftNode(cfg, j, pd, fsm)
	node.Start()
	defer node.Stop()

	handler := NewRaftHandler(node, j, pd)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// 1. GET /raft/status
	resp, err := http.Get(srv.URL + "/raft/status")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /raft/status failed: %v", err)
	}
	var status ClusterStatus
	_ = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if status.NodeID != "node-test" {
		t.Fatalf("expected node-test, got %s", status.NodeID)
	}

	// 2. POST /raft/peers
	peerPayload := []byte(`{"id":"peer-99","address":"http://127.0.0.1:9099"}`)
	resp, err = http.Post(srv.URL+"/raft/peers", "application/json", bytes.NewReader(peerPayload))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /raft/peers failed: %v", err)
	}
	resp.Body.Close()

	// 3. GET /raft/peers
	resp, err = http.Get(srv.URL + "/raft/peers")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /raft/peers failed: %v", err)
	}
	var peers []PeerStatus
	_ = json.NewDecoder(resp.Body).Decode(&peers)
	resp.Body.Close()
	if len(peers) != 1 || peers[0].ID != "peer-99" {
		t.Fatalf("expected registered peer-99: %+v", peers)
	}

	// 4. GET /raft/journal
	resp, err = http.Get(srv.URL + "/raft/journal")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /raft/journal failed: %v", err)
	}
	var entries []LogEntry
	_ = json.NewDecoder(resp.Body).Decode(&entries)
	resp.Body.Close()
	if len(entries) == 0 {
		t.Fatal("expected at least dummy entry in journal")
	}
}

func TestRaftHandler_SecurityAndInputSanitization(t *testing.T) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	fsm := NewBrokerFSM(pm, le)
	j, _ := NewJournal("")
	pd := NewPeerDiscovery("sec-node", "", nil)

	cfg := DefaultNodeConfig("sec-node")
	node := NewRaftNode(cfg, j, pd, fsm)
	node.Start()
	defer node.Stop()

	handler := NewRaftHandler(node, j, pd)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// 1. Method restriction: GET on /raft/vote must return 405
	resp, err := http.Get(srv.URL + "/raft/vote")
	if err != nil || resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on GET /raft/vote, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Reject unsupported command type in /raft/propose
	badCmd := []byte(`{"type":"MALICIOUS_DROP_TABLE","data":{}}`)
	resp, err = http.Post(srv.URL+"/raft/propose", "application/json", bytes.NewReader(badCmd))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on unsupported command, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 3. Reject oversized peer registration
	hugeID := strings.Repeat("A", 200)
	peerPayload := []byte(fmt.Sprintf(`{"id":"%s","address":"http://127.0.0.1:9099"}`, hugeID))
	resp, err = http.Post(srv.URL+"/raft/peers", "application/json", bytes.NewReader(peerPayload))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on oversized peer ID, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. Null byte path safety in NewJournal
	if _, err := NewJournal("data\x00/evil"); err == nil {
		t.Error("expected error for dataDir containing null bytes")
	}
}
