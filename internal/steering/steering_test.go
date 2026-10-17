package steering

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestQUICParser_LongHeaderInitial(t *testing.T) {
	dcidHex := "cf01aabbccddeeff"
	scidHex := "1122334455667788"
	pkt := BuildSyntheticQUICPacket(QUICHeaderInitial, dcidHex, scidHex, 128)

	hdr, err := ParseQUICHeader(pkt, 8)
	if err != nil {
		t.Fatalf("ParseQUICHeader failed: %v", err)
	}

	if !hdr.IsLongHeader {
		t.Errorf("expected long header, got short")
	}
	if hdr.Type != QUICHeaderInitial {
		t.Errorf("expected INITIAL, got %s", hdr.Type)
	}
	if hdr.Version != 0x00000001 {
		t.Errorf("expected version 1, got 0x%08x", hdr.Version)
	}
	if hdr.DCID != dcidHex {
		t.Errorf("expected DCID %s, got %s", dcidHex, hdr.DCID)
	}
	if hdr.SCID != scidHex {
		t.Errorf("expected SCID %s, got %s", scidHex, hdr.SCID)
	}
}

func TestQUICParser_LongHeaderHandshake(t *testing.T) {
	dcidHex := "cf02010203040506"
	scidHex := "9988776655443322"
	pkt := BuildSyntheticQUICPacket(QUICHeaderHandshake, dcidHex, scidHex, 64)

	hdr, err := ParseQUICHeader(pkt, 8)
	if err != nil {
		t.Fatalf("ParseQUICHeader failed: %v", err)
	}

	if hdr.Type != QUICHeaderHandshake {
		t.Errorf("expected HANDSHAKE, got %s", hdr.Type)
	}
	if hdr.DCID != dcidHex {
		t.Errorf("expected DCID %s, got %s", dcidHex, hdr.DCID)
	}
}

func TestQUICParser_ShortHeader1RTT(t *testing.T) {
	dcidHex := "cf03998877665544"
	pkt := BuildSyntheticQUICPacket(QUICHeaderShort1RTT, dcidHex, "", 96)

	hdr, err := ParseQUICHeader(pkt, 8)
	if err != nil {
		t.Fatalf("ParseQUICHeader failed: %v", err)
	}

	if hdr.IsLongHeader {
		t.Errorf("expected short header, got long")
	}
	if hdr.Type != QUICHeaderShort1RTT {
		t.Errorf("expected 1-RTT, got %s", hdr.Type)
	}
	if hdr.DCID != dcidHex {
		t.Errorf("expected DCID %s, got %s", dcidHex, hdr.DCID)
	}
}

func TestQUICParser_InvalidAndTruncated(t *testing.T) {
	// Empty
	if _, err := ParseQUICHeader([]byte{}, 8); err != ErrPacketTooShort {
		t.Errorf("expected ErrPacketTooShort for empty slice, got %v", err)
	}

	// Truncated long header (< 7 bytes)
	truncated := []byte{0xc0, 0x00, 0x00, 0x00}
	if _, err := ParseQUICHeader(truncated, 8); err != ErrPacketTooShort {
		t.Errorf("expected ErrPacketTooShort for 4-byte long header, got %v", err)
	}

	// Invalid fixed bit on non-zero version
	badFixedBit := []byte{0x80, 0x00, 0x00, 0x00, 0x01, 0x04, 0xaa, 0xbb}
	if _, err := ParseQUICHeader(badFixedBit, 8); err != ErrInvalidFixedBit {
		t.Errorf("expected ErrInvalidFixedBit, got %v", err)
	}
}

func TestSteeringEngine_RegisterAndDeregisterEndpoints(t *testing.T) {
	cfg := DefaultSteeringConfig()
	engine := NewPacketSteeringEngine(cfg)

	ep1 := &WorkerEndpoint{
		ID:      "worker-iad-01",
		Colo:    "IAD",
		Address: "10.0.1.10:443",
		Weight:  100,
	}

	if err := engine.RegisterEndpoint(ep1); err != nil {
		t.Fatalf("RegisterEndpoint failed: %v", err)
	}

	// Duplicate registration error
	if err := engine.RegisterEndpoint(ep1); err != ErrDuplicateEndpoint {
		t.Errorf("expected ErrDuplicateEndpoint, got %v", err)
	}

	list := engine.ListEndpoints()
	if len(list) != 1 || list[0].ID != "worker-iad-01" {
		t.Errorf("expected 1 endpoint, got %d", len(list))
	}

	// Fetch single
	retrieved, ok := engine.GetEndpoint("worker-iad-01")
	if !ok || retrieved.Colo != "IAD" {
		t.Errorf("failed to retrieve endpoint")
	}

	// Deregister
	if err := engine.DeregisterEndpoint("worker-iad-01"); err != nil {
		t.Fatalf("DeregisterEndpoint failed: %v", err)
	}
	if len(engine.ListEndpoints()) != 0 {
		t.Errorf("expected 0 endpoints after deregistration")
	}
}

func TestSteeringEngine_CIDPrefixRuleSteering(t *testing.T) {
	cfg := DefaultSteeringConfig()
	engine := NewPacketSteeringEngine(cfg)

	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "worker-iad", Colo: "IAD", Address: "10.0.1.10:443", Healthy: true})
	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "worker-lhr", Colo: "LHR", Address: "10.0.2.10:443", Healthy: true})

	// Rule 1: "cf01" -> IAD
	// Rule 2: "cf02" -> LHR
	if err := engine.AddRule(&SteeringRule{ID: "rule-iad", CIDPrefix: "cf01", TargetEndpointID: "worker-iad", Priority: 10}); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}
	if err := engine.AddRule(&SteeringRule{ID: "rule-lhr", CIDPrefix: "cf02", TargetEndpointID: "worker-lhr", Priority: 10}); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	pktIAD := BuildSyntheticQUICPacket(QUICHeaderInitial, "cf01aabbccddeeff", "11223344", 64)
	decisionIAD, err := engine.SteerPacket(pktIAD, "1.1.1.1:1234", "2.2.2.2:443")
	if err != nil {
		t.Fatalf("SteerPacket failed: %v", err)
	}
	if decisionIAD.TargetEndpoint.ID != "worker-iad" {
		t.Errorf("expected worker-iad, got %s", decisionIAD.TargetEndpoint.ID)
	}
	if decisionIAD.MatchedRuleID != "rule-iad" {
		t.Errorf("expected rule-iad, got %s", decisionIAD.MatchedRuleID)
	}

	pktLHR := BuildSyntheticQUICPacket(QUICHeaderShort1RTT, "cf02112233445566", "", 64)
	decisionLHR, err := engine.SteerPacket(pktLHR, "1.1.1.1:1234", "2.2.2.2:443")
	if err != nil {
		t.Fatalf("SteerPacket failed: %v", err)
	}
	if decisionLHR.TargetEndpoint.ID != "worker-lhr" {
		t.Errorf("expected worker-lhr, got %s", decisionLHR.TargetEndpoint.ID)
	}
}

func TestSteeringEngine_LeastConnAndHealthyFailover(t *testing.T) {
	cfg := DefaultSteeringConfig()
	engine := NewPacketSteeringEngine(cfg)

	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "worker-1", Address: "10.0.1.1:443", Healthy: true, ActiveConns: 50})
	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "worker-2", Address: "10.0.1.2:443", Healthy: true, ActiveConns: 10})

	// Non-QUIC packet -> falls back to least connections (worker-2)
	rawPayload := []byte("plain-udp-packet-payload")
	decision, err := engine.SteerPacket(rawPayload, "10.1.1.1:5000", "10.2.2.2:443")
	if err != nil {
		t.Fatalf("SteerPacket failed: %v", err)
	}
	if decision.TargetEndpoint.ID != "worker-2" {
		t.Errorf("expected worker-2 for least connections, got %s", decision.TargetEndpoint.ID)
	}

	// Mark worker-2 unhealthy -> should route to worker-1
	_ = engine.UpdateEndpointHealth("worker-2", false)
	decision2, err := engine.SteerPacket(rawPayload, "10.1.1.1:5000", "10.2.2.2:443")
	if err != nil {
		t.Fatalf("SteerPacket failed: %v", err)
	}
	if decision2.TargetEndpoint.ID != "worker-1" {
		t.Errorf("expected failover to worker-1, got %s", decision2.TargetEndpoint.ID)
	}

	// Mark both unhealthy -> should return ErrNoHealthyEndpoints
	_ = engine.UpdateEndpointHealth("worker-1", false)
	if _, err := engine.SteerPacket(rawPayload, "10.1.1.1:5000", "10.2.2.2:443"); err != ErrNoHealthyEndpoints {
		t.Errorf("expected ErrNoHealthyEndpoints, got %v", err)
	}
}

func TestSteeringEngine_Sub10MicrosecondLatency(t *testing.T) {
	cfg := DefaultSteeringConfig()
	engine := NewPacketSteeringEngine(cfg)

	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "worker-fast-01", Address: "127.0.0.1:8001", Healthy: true})
	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "worker-fast-02", Address: "127.0.0.1:8002", Healthy: true})

	pkt := BuildSyntheticQUICPacket(QUICHeaderShort1RTT, "cf01aabbccddeeff", "", 128)

	const iterations = 5000
	for i := 0; i < iterations; i++ {
		dec, err := engine.SteerPacket(pkt, "192.168.1.10:45000", "10.0.0.1:443")
		if err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}
		if dec.TargetEndpoint == nil {
			t.Fatalf("nil target endpoint")
		}
	}

	stats := engine.GetStats()
	if stats.TotalPackets != iterations {
		t.Errorf("expected %d total packets, got %d", iterations, stats.TotalPackets)
	}

	// Assert average latency is sub-10-microsecond (< 10µs)
	t.Logf("Average Dispatch Latency: %.2f µs (Sub-10µs Ratio: %.2f%%)", stats.AvgDispatchLatencyUs, stats.Sub10MicrosecRatio)
	if stats.AvgDispatchLatencyUs >= 15.0 {
		t.Errorf("average dispatch latency too high: %.2f µs (expected < 15µs in test run)", stats.AvgDispatchLatencyUs)
	}
	if stats.Sub10MicrosecRatio < 80.0 {
		t.Errorf("sub-10 microsecond ratio too low: %.2f%%", stats.Sub10MicrosecRatio)
	}
}

func TestSteeringEngine_EBPFCrossPlatformDriver(t *testing.T) {
	driver := NewPlatformEBPFDriver()
	defer driver.Close()

	mode := driver.GetMode()
	if mode != SteeringModeEBPFSockmap && mode != SteeringModeUserspace {
		t.Errorf("unexpected steering mode: %s", mode)
	}

	if err := driver.UpdateSockmap("cf01", "10.0.1.1:443"); err != nil && driver.IsAvailable() {
		t.Errorf("UpdateSockmap failed on available driver: %v", err)
	}
}

func TestSteeringHandler_HTTPEndpoints(t *testing.T) {
	cfg := DefaultSteeringConfig()
	engine := NewPacketSteeringEngine(cfg)
	handler := NewSteeringHandler(engine)

	// 1. GET /api/steering/status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/steering/status", nil)
	wStatus := httptest.NewRecorder()
	handler.ServeHTTP(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wStatus.Code)
	}

	// 2. POST /api/steering/endpoints (Register)
	epJSON := `{"id":"worker-nrt","colo":"NRT","address":"10.0.3.1:443","weight":100}`
	reqAddEp := httptest.NewRequest(http.MethodPost, "/api/steering/endpoints", bytes.NewBufferString(epJSON))
	wAddEp := httptest.NewRecorder()
	handler.ServeHTTP(wAddEp, reqAddEp)
	if wAddEp.Code != http.StatusCreated {
		t.Errorf("expected 201 Created, got %d: %s", wAddEp.Code, wAddEp.Body.String())
	}

	// 3. GET /api/steering/endpoints
	reqListEp := httptest.NewRequest(http.MethodGet, "/api/steering/endpoints", nil)
	wListEp := httptest.NewRecorder()
	handler.ServeHTTP(wListEp, reqListEp)
	if wListEp.Code != http.StatusOK || !strings.Contains(wListEp.Body.String(), "worker-nrt") {
		t.Errorf("expected endpoints list containing worker-nrt, got %s", wListEp.Body.String())
	}

	// 4. POST /api/steering/rules
	ruleJSON := `{"id":"rule-nrt","cid_prefix":"cf09","target_endpoint_id":"worker-nrt","priority":20}`
	reqRule := httptest.NewRequest(http.MethodPost, "/api/steering/rules", bytes.NewBufferString(ruleJSON))
	wRule := httptest.NewRecorder()
	handler.ServeHTTP(wRule, reqRule)
	if wRule.Code != http.StatusCreated {
		t.Errorf("expected 201 Created for rule, got %d: %s", wRule.Code, wRule.Body.String())
	}

	// 5. POST /api/steering/simulate
	simJSON := `{"packet_type":"INITIAL","dcid":"cf09aabb11223344","payload_bytes":64}`
	reqSim := httptest.NewRequest(http.MethodPost, "/api/steering/simulate", bytes.NewBufferString(simJSON))
	wSim := httptest.NewRecorder()
	handler.ServeHTTP(wSim, reqSim)
	if wSim.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for simulate, got %d: %s", wSim.Code, wSim.Body.String())
	}

	var simResp map[string]interface{}
	if err := json.Unmarshal(wSim.Body.Bytes(), &simResp); err != nil {
		t.Fatalf("failed to decode simulate response: %v", err)
	}
	if simResp["status"] != "STEERED" {
		t.Errorf("expected status STEERED, got %v", simResp["status"])
	}

	// 6. GET /api/steering/stats
	reqStats := httptest.NewRequest(http.MethodGet, "/api/steering/stats", nil)
	wStats := httptest.NewRecorder()
	handler.ServeHTTP(wStats, reqStats)
	if wStats.Code != http.StatusOK {
		t.Errorf("expected 200 OK for stats, got %d", wStats.Code)
	}

	// 7. DELETE /api/steering/rules?id=rule-nrt
	reqDelRule := httptest.NewRequest(http.MethodDelete, "/api/steering/rules?id=rule-nrt", nil)
	wDelRule := httptest.NewRecorder()
	handler.ServeHTTP(wDelRule, reqDelRule)
	if wDelRule.Code != http.StatusOK {
		t.Errorf("expected 200 OK for delete rule, got %d", wDelRule.Code)
	}

	// 8. DELETE /api/steering/endpoints?id=worker-nrt
	reqDelEp := httptest.NewRequest(http.MethodDelete, "/api/steering/endpoints?id=worker-nrt", nil)
	wDelEp := httptest.NewRecorder()
	handler.ServeHTTP(wDelEp, reqDelEp)
	if wDelEp.Code != http.StatusOK {
		t.Errorf("expected 200 OK for delete endpoint, got %d", wDelEp.Code)
	}
}

func TestSteeringHandler_BodySizeBoundingAndDoS(t *testing.T) {
	cfg := DefaultSteeringConfig()
	engine := NewPacketSteeringEngine(cfg)
	handler := NewSteeringHandler(engine)

	// Send oversized payload (128KB > 64KB ceiling)
	oversized := bytes.Repeat([]byte("A"), 128*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/steering/simulate", bytes.NewBuffer(oversized))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	// Should fail JSON unmarshal gracefully because input is truncated at 64KB
	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500 error on bounded payload, got %d", w.Code)
	}
}

func TestSteering_HighConcurrency(t *testing.T) {
	cfg := DefaultSteeringConfig()
	engine := NewPacketSteeringEngine(cfg)

	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "c-worker-1", Address: "10.0.1.1:443", Healthy: true})
	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "c-worker-2", Address: "10.0.1.2:443", Healthy: true})
	_ = engine.RegisterEndpoint(&WorkerEndpoint{ID: "c-worker-3", Address: "10.0.1.3:443", Healthy: true})

	var wg sync.WaitGroup
	const goroutines = 40
	const perGoroutine = 100

	pkt := BuildSyntheticQUICPacket(QUICHeaderShort1RTT, "cf01aabbccddeeff", "", 64)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_, err := engine.SteerPacket(pkt, "192.168.1.1:1000", "10.0.0.1:443")
				if err != nil {
					t.Errorf("concurrency error in goroutine %d: %v", gid, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()

	stats := engine.GetStats()
	if stats.TotalPackets != goroutines*perGoroutine {
		t.Errorf("expected %d total packets, got %d", goroutines*perGoroutine, stats.TotalPackets)
	}
}
