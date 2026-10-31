package replication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVectorClock_CausalityAndComparisons(t *testing.T) {
	vc1 := NewVectorClock()
	vc2 := NewVectorClock()

	if rel := vc1.Compare(vc2); rel != ClockEqual {
		t.Fatalf("expected empty clocks to be equal, got %s", rel)
	}

	vc1.Increment("node-a")
	if rel := vc1.Compare(vc2); rel != ClockAfter {
		t.Fatalf("expected vc1 after vc2, got %s", rel)
	}
	if rel := vc2.Compare(vc1); rel != ClockBefore {
		t.Fatalf("expected vc2 before vc1, got %s", rel)
	}

	vc2.Increment("node-b")
	// vc1: {node-a:1}, vc2: {node-b:1} -> concurrent
	if rel := vc1.Compare(vc2); rel != ClockConcurrent {
		t.Fatalf("expected concurrent relation, got %s", rel)
	}

	// Merge
	merged := vc1.Merge(vc2)
	if merged.Get("node-a") != 1 || merged.Get("node-b") != 1 {
		t.Fatalf("unexpected merged clock: %v", merged)
	}
	if !merged.DominatesOrEqual(vc1) || !merged.DominatesOrEqual(vc2) {
		t.Fatalf("merged clock must dominate both parents")
	}

	// JSON serialization
	b, err := merged.ToJSON()
	if err != nil {
		t.Fatalf("failed to serialize vector clock: %v", err)
	}
	parsed, err := ParseVectorClock(b)
	if err != nil {
		t.Fatalf("failed to parse vector clock: %v", err)
	}
	if rel := parsed.Compare(merged); rel != ClockEqual {
		t.Fatalf("parsed clock does not match original: %v vs %v", parsed, merged)
	}
}

func TestDeterministicTieBreak(t *testing.T) {
	t1 := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 31, 10, 0, 1, 0, time.UTC)

	// Distinct timestamps: higher timestamp wins
	if !DeterministicTieBreak(t2, t1, "node-a", "node-b") {
		t.Fatalf("expected t2 to win over t1")
	}
	if DeterministicTieBreak(t1, t2, "node-a", "node-b") {
		t.Fatalf("expected t1 to lose to t2")
	}

	// Equal timestamps: hash tiebreak is deterministic and asymmetric
	win1 := DeterministicTieBreak(t1, t1, "node-ord", "node-lhr")
	win2 := DeterministicTieBreak(t1, t1, "node-lhr", "node-ord")
	if win1 == win2 {
		t.Fatalf("tie break must be strictly asymmetric: win1=%v, win2=%v", win1, win2)
	}
}

func TestPNCounter_Convergence(t *testing.T) {
	c1 := NewPNCounter()
	c2 := NewPNCounter()

	// Node 1 operations
	c1.Inc("node-1", 10)
	c1.Dec("node-1", 3) // value = 7

	// Node 2 operations
	c2.Inc("node-2", 20)
	c2.Dec("node-2", 5) // value = 15

	if c1.Value() != 7 {
		t.Fatalf("c1 value expected 7, got %d", c1.Value())
	}
	if c2.Value() != 15 {
		t.Fatalf("c2 value expected 15, got %d", c2.Value())
	}

	// Merge c2 into c1
	m1 := c1.Merge(c2)
	// Merge c1 into c2
	m2 := c2.Merge(c1)

	if m1.Value() != 22 {
		t.Fatalf("m1 value expected 22, got %d", m1.Value())
	}
	if m2.Value() != 22 {
		t.Fatalf("m2 value expected 22, got %d", m2.Value())
	}
	if m1.Value() != m2.Value() {
		t.Fatalf("PNCounter failed convergence: m1=%d, m2=%d", m1.Value(), m2.Value())
	}
}

func TestORSet_AddWinsAndConvergence(t *testing.T) {
	s1 := NewORSet()
	s2 := NewORSet()

	// Node 1 adds "session-abc" with tag tag1
	tag1 := s1.Add("session-abc", "tag-1")
	// Node 2 concurrently adds "session-abc" with tag tag2
	tag2 := s2.Add("session-abc", "tag-2")

	// Node 1 removes "session-abc" (removes tag-1)
	s1.Remove("session-abc")
	if s1.Contains("session-abc") {
		t.Fatalf("s1 should not contain session-abc after remove")
	}

	// Node 2 still has tag-2
	if !s2.Contains("session-abc") {
		t.Fatalf("s2 should contain session-abc")
	}

	// Merge s2 into s1: Add-Wins semantics ensure tag-2 survives
	m1 := s1.Merge(s2)
	m2 := s2.Merge(s1)

	if !m1.Contains("session-abc") || !m2.Contains("session-abc") {
		t.Fatalf("Add-wins semantics failed: session-abc should survive concurrent add/remove")
	}

	elems1 := m1.Elements()
	elems2 := m2.Elements()
	if len(elems1) != 1 || elems1[0] != "session-abc" {
		t.Fatalf("expected [session-abc], got %v", elems1)
	}
	if len(elems2) != 1 || elems2[0] != "session-abc" {
		t.Fatalf("expected [session-abc], got %v", elems2)
	}
	_ = tag1
	_ = tag2
}

func TestLWWRegister_Merge(t *testing.T) {
	r1 := NewLWWRegister("initial", "node-1")
	r2 := NewLWWRegister("initial", "node-2")

	r1.Set("val-from-node-1", "node-1")
	time.Sleep(2 * time.Millisecond)
	r2.Set("val-from-node-2", "node-2")

	r1.Merge(r2)
	val, _, _ := r1.Get()
	if val != "val-from-node-2" {
		t.Fatalf("expected newer value 'val-from-node-2', got '%s'", val)
	}
}

func TestGeoReplication_MultiNodeConvergence(t *testing.T) {
	cfg1 := ReplicationConfig{LocalNodeID: "node-ord", Region: "ord"}
	cfg2 := ReplicationConfig{LocalNodeID: "node-lhr", Region: "lhr"}
	cfg3 := ReplicationConfig{LocalNodeID: "node-nrt", Region: "nrt"}

	eng1 := NewGeoReplicationEngine(cfg1)
	eng2 := NewGeoReplicationEngine(cfg2)
	eng3 := NewGeoReplicationEngine(cfg3)

	defer eng1.Stop()
	defer eng2.Stop()
	defer eng3.Stop()

	// Node 1 writes k1
	_, err := eng1.Put("tenantA", "config:theme", []byte("dark"), nil)
	if err != nil {
		t.Fatalf("eng1 Put failed: %v", err)
	}

	// Node 2 writes k2
	_, err = eng2.Put("tenantA", "config:locale", []byte("en-US"), nil)
	if err != nil {
		t.Fatalf("eng2 Put failed: %v", err)
	}

	// Node 3 increments a distributed counter
	_, err = eng3.UpdateCounter("tenantA", "metrics:requests", 100, nil)
	if err != nil {
		t.Fatalf("eng3 UpdateCounter failed: %v", err)
	}

	// Exchange sync packets: eng1 -> eng2, eng2 -> eng3, eng3 -> eng1
	pkt1 := eng1.GenerateSyncPacket(nil)
	_, err = eng2.ProcessSyncPacket(pkt1)
	if err != nil {
		t.Fatalf("eng2 ProcessSyncPacket failed: %v", err)
	}

	pkt2 := eng2.GenerateSyncPacket(nil)
	_, err = eng3.ProcessSyncPacket(pkt2)
	if err != nil {
		t.Fatalf("eng3 ProcessSyncPacket failed: %v", err)
	}

	pkt3 := eng3.GenerateSyncPacket(nil)
	_, err = eng1.ProcessSyncPacket(pkt3)
	if err != nil {
		t.Fatalf("eng1 ProcessSyncPacket failed: %v", err)
	}

	// Second round of sync to ensure complete convergence across all 3 nodes
	pkt1b := eng1.GenerateSyncPacket(nil)
	_, _ = eng2.ProcessSyncPacket(pkt1b)
	_, _ = eng3.ProcessSyncPacket(pkt1b)

	// Verify all 3 nodes have k1
	for idx, eng := range []*GeoReplicationEngine{eng1, eng2, eng3} {
		e1, err := eng.Get("tenantA", "config:theme", nil)
		if err != nil || string(e1.Value) != "dark" {
			t.Fatalf("node %d failed to read config:theme: %v", idx+1, err)
		}
		e2, err := eng.Get("tenantA", "config:locale", nil)
		if err != nil || string(e2.Value) != "en-US" {
			t.Fatalf("node %d failed to read config:locale: %v", idx+1, err)
		}
		cnt, ok := eng.GetCounter("tenantA", "metrics:requests")
		if !ok || cnt != 100 {
			t.Fatalf("node %d failed to read counter metrics:requests: val=%d, ok=%v", idx+1, cnt, ok)
		}
	}
}

func TestGeoReplication_ConcurrentConflictResolution(t *testing.T) {
	cfg1 := ReplicationConfig{LocalNodeID: "node-ord", Region: "ord"}
	cfg2 := ReplicationConfig{LocalNodeID: "node-lhr", Region: "lhr"}

	eng1 := NewGeoReplicationEngine(cfg1)
	eng2 := NewGeoReplicationEngine(cfg2)

	// Both nodes concurrently write to the same key
	_, err := eng1.Put("store", "item-42", []byte("price-100"), nil)
	if err != nil {
		t.Fatalf("eng1 Put failed: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	_, err = eng2.Put("store", "item-42", []byte("price-120"), nil)
	if err != nil {
		t.Fatalf("eng2 Put failed: %v", err)
	}

	// Sync eng2 -> eng1
	pkt2 := eng2.GenerateSyncPacket(nil)
	ack, err := eng1.ProcessSyncPacket(pkt2)
	if err != nil {
		t.Fatalf("ProcessSyncPacket failed: %v", err)
	}
	if ack.ConflictsSolved != 1 {
		t.Fatalf("expected 1 conflict solved, got %d", ack.ConflictsSolved)
	}

	// Verify resolved value (eng2 was written 5ms later, so price-120 wins)
	entry, err := eng1.Get("store", "item-42", nil)
	if err != nil || string(entry.Value) != "price-120" {
		t.Fatalf("expected 'price-120', got '%s'", string(entry.Value))
	}

	stats := eng1.Stats()
	if stats.ConflictsDetected != 1 || stats.ConflictsResolved != 1 {
		t.Fatalf("telemetry mismatch: detected=%d, resolved=%d", stats.ConflictsDetected, stats.ConflictsResolved)
	}
}

func TestConsistency_ReadYourWrites(t *testing.T) {
	cfg1 := ReplicationConfig{LocalNodeID: "node-ord", Region: "ord"}
	cfg2 := ReplicationConfig{LocalNodeID: "node-lhr", Region: "lhr"}

	eng1 := NewGeoReplicationEngine(cfg1)
	eng2 := NewGeoReplicationEngine(cfg2)

	clientCtx := &ClientSessionContext{
		ClientID:             "worker-client-01",
		TenantID:             "tenant-alpha",
		RequestedConsistency: ConsistencyReadYourWrites,
		LastSeenVectorClock:  NewVectorClock(),
	}

	// Client writes to Node 1
	entry, err := eng1.Put("tenant-alpha", "user:status", []byte("authenticated"), clientCtx)
	if err != nil {
		t.Fatalf("eng1 Put failed: %v", err)
	}
	clientCtx.LastSeenVectorClock = entry.VectorClock.Clone()

	// Read on Node 1 with RYW -> must succeed immediately
	res1, err := eng1.Get("tenant-alpha", "user:status", clientCtx)
	if err != nil || string(res1.Value) != "authenticated" {
		t.Fatalf("eng1 RYW read failed: %v", err)
	}

	// Read on Node 2 with RYW BEFORE replication -> must fail with ErrStaleReadViolation
	_, err = eng2.Get("tenant-alpha", "user:status", clientCtx)
	if err != ErrStaleReadViolation {
		t.Fatalf("expected ErrStaleReadViolation on un-replicated node, got %v", err)
	}

	// Now sync Node 1 -> Node 2
	pkt := eng1.GenerateSyncPacket(nil)
	_, err = eng2.ProcessSyncPacket(pkt)
	if err != nil {
		t.Fatalf("ProcessSyncPacket failed: %v", err)
	}

	// Read on Node 2 with RYW AFTER replication -> must succeed!
	res2, err := eng2.Get("tenant-alpha", "user:status", clientCtx)
	if err != nil || string(res2.Value) != "authenticated" {
		t.Fatalf("eng2 RYW read failed after sync: %v", err)
	}

	stats := eng2.Stats()
	if stats.ReadYourWritesHits < 1 {
		t.Fatalf("expected ReadYourWritesHits >= 1, got %d", stats.ReadYourWritesHits)
	}
}

func TestConsistency_MonotonicRead(t *testing.T) {
	cfg := ReplicationConfig{LocalNodeID: "node-ord", Region: "ord"}
	eng := NewGeoReplicationEngine(cfg)

	// Write version 1
	e1, _ := eng.Put("app", "token", []byte("tok-v1"), nil)
	// Write version 2
	e2, _ := eng.Put("app", "token", []byte("tok-v2"), nil)

	// Client that has already observed version 2
	clientCtx := &ClientSessionContext{
		ClientID:             "worker-client-02",
		RequestedConsistency: ConsistencyMonotonicRead,
		LastSeenVectorClock:  e2.VectorClock.Clone(),
	}

	// Read should yield tok-v2
	res, err := eng.Get("app", "token", clientCtx)
	if err != nil || string(res.Value) != "tok-v2" {
		t.Fatalf("expected tok-v2, got %v", err)
	}

	// Synthesize a stale read check: if an entry has older clock than client
	staleEntry := &ReplicationEntry{
		Namespace:   "app",
		Key:         "token",
		Value:       []byte("tok-v1"),
		VectorClock: e1.VectorClock,
	}
	tracker := NewSessionTracker()
	err = tracker.VerifyConsistency(clientCtx, staleEntry, eng.currentClock)
	if err != ErrStaleReadViolation {
		t.Fatalf("expected ErrStaleReadViolation on monotonic read check, got %v", err)
	}
}

func TestHMAC_TamperDetection(t *testing.T) {
	cfg := ReplicationConfig{LocalNodeID: "node-ord", HMACSecret: "secure-key"}
	eng := NewGeoReplicationEngine(cfg)

	pkt := eng.GenerateSyncPacket(nil)
	if !eng.VerifyPacket(pkt) {
		t.Fatalf("fresh packet must pass HMAC verification")
	}

	// Tamper with packet
	pkt.Epoch += 999
	if eng.VerifyPacket(pkt) {
		t.Fatalf("tampered packet must fail HMAC verification")
	}

	// Tamper with signature
	pkt.SignatureHex = "001122334455"
	if eng.VerifyPacket(pkt) {
		t.Fatalf("invalid signature must fail verification")
	}
}

func TestReplicationHandler_RESTEndpoints(t *testing.T) {
	cfg := ReplicationConfig{LocalNodeID: "node-ord", Region: "ord"}
	eng := NewGeoReplicationEngine(cfg)
	handler := NewReplicationHandler(eng)

	// 1. Status
	req := httptest.NewRequest(http.MethodGet, "/api/replication/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code expected 200, got %d", rec.Code)
	}

	// 2. PUT
	putBody := `{"namespace":"api","key":"setting","value":"enabled"}`
	req = httptest.NewRequest(http.MethodPost, "/api/replication/put", strings.NewReader(putBody))
	req.Header.Set("X-CF-Broker-Client-ID", "test-client")
	req.Header.Set("X-CF-Broker-Consistency", "read_your_writes")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put code expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	clientClock := rec.Header().Get("X-CF-Broker-VectorClock")
	if clientClock == "" {
		t.Fatalf("expected X-CF-Broker-VectorClock header in PUT response")
	}

	// 3. GET
	req = httptest.NewRequest(http.MethodGet, "/api/replication/get?namespace=api&key=setting", nil)
	req.Header.Set("X-CF-Broker-Client-ID", "test-client")
	req.Header.Set("X-CF-Broker-Consistency", "read_your_writes")
	req.Header.Set("X-CF-Broker-VectorClock", clientClock)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get code expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	// 4. CRDT Counter
	cntBody := `{"namespace":"api","key":"page_views","delta":42}`
	req = httptest.NewRequest(http.MethodPost, "/api/replication/crdt/counter", strings.NewReader(cntBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("counter code expected 200, got %d", rec.Code)
	}

	// 5. CRDT Set
	setBody := `{"namespace":"api","key":"active_tags","op":"set_add","element":"feature_flag_a"}`
	req = httptest.NewRequest(http.MethodPost, "/api/replication/crdt/set", strings.NewReader(setBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set code expected 200, got %d", rec.Code)
	}

	// 6. Export
	req = httptest.NewRequest(http.MethodGet, "/api/replication/export", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export code expected 200, got %d", rec.Code)
	}
	var exportPkt ReplicationPacket
	if err := json.Unmarshal(rec.Body.Bytes(), &exportPkt); err != nil {
		t.Fatalf("failed to decode exported packet: %v", err)
	}
	if len(exportPkt.Entries) < 3 {
		t.Fatalf("expected at least 3 entries in export packet, got %d", len(exportPkt.Entries))
	}

	// 7. Sync
	syncBytes, _ := json.Marshal(exportPkt)
	req = httptest.NewRequest(http.MethodPost, "/api/replication/sync", bytes.NewReader(syncBytes))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync code expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	// 8. Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/replication/delete?namespace=api&key=setting", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete code expected 200, got %d", rec.Code)
	}

	// 9. Stats
	req = httptest.NewRequest(http.MethodGet, "/api/replication/stats", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats code expected 200, got %d", rec.Code)
	}

	// 10. Reset
	req = httptest.NewRequest(http.MethodPost, "/api/replication/reset", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset code expected 200, got %d", rec.Code)
	}
}

func TestReplicationHandler_BodyLimitAndDoS(t *testing.T) {
	cfg := ReplicationConfig{LocalNodeID: "node-ord"}
	eng := NewGeoReplicationEngine(cfg)
	handler := NewReplicationHandler(eng)

	// Payload larger than 64KB
	hugeVal := strings.Repeat("A", 70*1024)
	hugeJSON := fmt.Sprintf(`{"namespace":"default","key":"big","value":"%s"}`, hugeVal)

	req := httptest.NewRequest(http.MethodPost, "/api/replication/put", strings.NewReader(hugeJSON))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should be rejected with 400 Bad Request due to truncation of JSON syntax
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on oversized payload, got %d", rec.Code)
	}
}

func TestGeoReplication_HighConcurrency40Goroutines(t *testing.T) {
	cfg := ReplicationConfig{LocalNodeID: "node-concurrent", Region: "ord"}
	eng := NewGeoReplicationEngine(cfg)
	defer eng.Stop()

	var wg sync.WaitGroup
	workers := 40
	iterations := 50

	for i := 0; i < workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			clientCtx := &ClientSessionContext{
				ClientID:             fmt.Sprintf("worker-%d", workerID),
				TenantID:             "tenant-load",
				RequestedConsistency: ConsistencyReadYourWrites,
				LastSeenVectorClock:  NewVectorClock(),
			}

			for j := 0; j < iterations; j++ {
				k := fmt.Sprintf("key-%d", j%10)
				val := fmt.Sprintf("val-%d-%d", workerID, j)

				// Concurrent PUT
				entry, err := eng.Put("load", k, []byte(val), clientCtx)
				if err != nil {
					t.Errorf("worker %d Put error: %v", workerID, err)
					return
				}
				clientCtx.LastSeenVectorClock = entry.VectorClock.Clone()

				// Concurrent GET
				_, err = eng.Get("load", k, clientCtx)
				if err != nil {
					t.Errorf("worker %d Get error: %v", workerID, err)
					return
				}

				// Concurrent CRDT Counter
				_, err = eng.UpdateCounter("load", "shared_hits", 1, clientCtx)
				if err != nil {
					t.Errorf("worker %d Counter error: %v", workerID, err)
					return
				}

				// Concurrent CRDT Set
				_, err = eng.UpdateSet("load", "shared_tags", "set_add", fmt.Sprintf("tag-%d", workerID), clientCtx)
				if err != nil {
					t.Errorf("worker %d Set error: %v", workerID, err)
					return
				}
			}
		}()
	}

	wg.Wait()

	cnt, ok := eng.GetCounter("load", "shared_hits")
	if !ok || cnt != int64(workers*iterations) {
		t.Fatalf("expected counter %d, got %d (ok=%v)", workers*iterations, cnt, ok)
	}

	tags, ok := eng.GetSet("load", "shared_tags")
	if !ok || len(tags) != workers {
		t.Fatalf("expected %d unique tags, got %d", workers, len(tags))
	}

	stats := eng.Stats()
	if stats.CRDTOperations != int64(workers*iterations*2) {
		t.Fatalf("expected %d CRDT ops, got %d", workers*iterations*2, stats.CRDTOperations)
	}
}
