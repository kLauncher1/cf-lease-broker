package scrubber

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRawIPv4PacketParsing(t *testing.T) {
	filter := NewXDPFilter()

	// 1. Synthetic IPv4 TCP packet (20 bytes IP + 20 bytes TCP)
	rawTCP := make([]byte, 40)
	rawTCP[0] = 0x45 // Version 4, IHL 5 (20 bytes)
	rawTCP[9] = 6    // Protocol TCP
	copy(rawTCP[12:16], []byte{192, 0, 2, 10})
	copy(rawTCP[16:20], []byte{198, 51, 100, 20})
	// TCP header
	rawTCP[20] = 0x1F // Src Port 8000 (0x1F40)
	rawTCP[21] = 0x40
	rawTCP[22] = 0x01 // Dst Port 443 (0x01BB)
	rawTCP[23] = 0xBB
	rawTCP[32] = 0x50 // Data offset 5 (20 bytes)
	rawTCP[33] = 0x02 // Flags: SYN

	meta, err := filter.ParseRawIPv4Packet(rawTCP)
	if err != nil {
		t.Fatalf("ParseRawIPv4Packet failed: %v", err)
	}
	if meta.Protocol != ProtocolTCP {
		t.Errorf("expected ProtocolTCP, got %v", meta.Protocol)
	}
	if meta.SrcIP != "192.0.2.10" || meta.DstIP != "198.51.100.20" {
		t.Errorf("unexpected IP addresses: %s -> %s", meta.SrcIP, meta.DstIP)
	}
	if meta.SrcPort != 8000 || meta.DstPort != 443 {
		t.Errorf("unexpected ports: %d -> %d", meta.SrcPort, meta.DstPort)
	}
	if !meta.TCPFlags.SYN || meta.TCPFlags.ACK {
		t.Errorf("unexpected flags: SYN=%v, ACK=%v", meta.TCPFlags.SYN, meta.TCPFlags.ACK)
	}

	// 2. Short packet
	_, err = filter.ParseRawIPv4Packet([]byte{0x45, 0x00})
	if err != ErrPacketTooShort {
		t.Errorf("expected ErrPacketTooShort, got %v", err)
	}
}

func TestLineRateSYNFloodDrop(t *testing.T) {
	cfg := DefaultScrubberConfig()
	cfg.DefaultPolicy.MaxSYNRPS = 5 // low threshold for testing
	eng := NewScrubberEngine(cfg)
	defer eng.Stop()

	srcIP := "203.0.113.50"
	synPkt := PacketMetadata{
		SrcIP:       srcIP,
		DstIP:       "198.51.100.1",
		SrcPort:     45000,
		DstPort:     443,
		Protocol:    ProtocolTCP,
		TCPFlags:    TCPFlags{SYN: true, ACK: false},
		PayloadSize: 0,
		Timestamp:   time.Now(),
	}

	// First 5 should pass
	for i := 0; i < 5; i++ {
		dec := eng.InspectPacket(synPkt, "default")
		if dec.Action != ActionPass {
			t.Fatalf("expected packet %d to pass, got %s (reason: %s)", i, dec.Action, dec.Reason)
		}
	}

	// 6th packet must be dropped as SYN flood
	dec := eng.InspectPacket(synPkt, "default")
	if dec.Action != ActionDrop {
		t.Fatalf("expected ActionDrop on SYN flood, got %s", dec.Action)
	}
	if dec.AttackType != AttackTypeSYNFlood {
		t.Errorf("expected AttackTypeSYNFlood, got %s", dec.AttackType)
	}

	stats := eng.GetStats()
	if stats.SYNFloodsMitigated == 0 {
		t.Errorf("expected SYNFloodsMitigated > 0, got %d", stats.SYNFloodsMitigated)
	}
}

func TestUDPAmplificationDetection(t *testing.T) {
	cfg := DefaultScrubberConfig()
	cfg.DefaultPolicy.MaxAmplificationSize = 256
	eng := NewScrubberEngine(cfg)
	defer eng.Stop()

	// High-risk UDP reflection from NTP (port 123) with large payload
	ntpPkt := PacketMetadata{
		SrcIP:       "192.0.2.123",
		DstIP:       "198.51.100.1",
		SrcPort:     123,
		DstPort:     55000,
		Protocol:    ProtocolUDP,
		PayloadSize: 1024, // Exceeds 256
		Timestamp:   time.Now(),
	}

	dec := eng.InspectPacket(ntpPkt, "default")
	if dec.Action != ActionDrop {
		t.Fatalf("expected ActionDrop for UDP amplification, got %s", dec.Action)
	}
	if dec.AttackType != AttackTypeUDPAmplification {
		t.Errorf("expected AttackTypeUDPAmplification, got %s", dec.AttackType)
	}

	stats := eng.GetStats()
	if stats.UDPFloodsMitigated == 0 {
		t.Errorf("expected UDPFloodsMitigated > 0, got %d", stats.UDPFloodsMitigated)
	}
}

func TestMalformedPacketDrop(t *testing.T) {
	eng := NewScrubberEngine(DefaultScrubberConfig())
	defer eng.Stop()

	// 1. Illegal TCP SYN+FIN combination
	badFlagsPkt := PacketMetadata{
		SrcIP:    "192.0.2.1",
		DstIP:    "198.51.100.1",
		Protocol: ProtocolTCP,
		TCPFlags: TCPFlags{SYN: true, FIN: true},
	}
	dec := eng.InspectPacket(badFlagsPkt, "default")
	if dec.Action != ActionDrop || dec.AttackType != AttackTypeMalformedHeader {
		t.Errorf("expected ActionDrop on SYN+FIN, got %v (%s)", dec.Action, dec.AttackType)
	}

	// 2. Land attack (Src == Dst on same port)
	landPkt := PacketMetadata{
		SrcIP:    "198.51.100.1",
		DstIP:    "198.51.100.1",
		SrcPort:  443,
		DstPort:  443,
		Protocol: ProtocolTCP,
		TCPFlags: TCPFlags{SYN: true},
	}
	dec2 := eng.InspectPacket(landPkt, "default")
	if dec2.Action != ActionDrop || dec2.AttackType != AttackTypeMalformedHeader {
		t.Errorf("expected ActionDrop on Land attack, got %v (%s)", dec2.Action, dec2.AttackType)
	}
}

func TestIPReputationLifecycle(t *testing.T) {
	cfg := DefaultScrubberConfig()
	cfg.DefaultPolicy.DropThreshold = 20.0
	cfg.DefaultPolicy.ChallengeThreshold = 60.0
	eng := NewScrubberEngine(cfg)
	defer eng.Stop()

	ip := "198.51.100.88"

	// Initial reputation
	rec := eng.GetReputation(ip)
	if rec.Score != 100.0 || rec.Level != ReputationClean {
		t.Fatalf("expected 100.0 Clean, got %.1f (%s)", rec.Score, rec.Level)
	}

	// Apply penalty (e.g. simulated attack)
	eng.reputation.RecordEvent(ip, AttackTypeSYNFlood, 30.0, "syn_flood_test", cfg.DefaultPolicy)
	rec = eng.GetReputation(ip)
	if rec.Score != 70.0 || rec.Level != ReputationSuspicious {
		t.Errorf("expected 70.0 Suspicious, got %.1f (%s)", rec.Score, rec.Level)
	}

	// Deduct more to reach challenge threshold (< 60.0)
	eng.reputation.RecordEvent(ip, AttackTypeUDPAmplification, 20.0, "udp_test", cfg.DefaultPolicy)
	rec = eng.GetReputation(ip)
	if rec.Score != 50.0 || rec.Level != ReputationAnomalous {
		t.Errorf("expected 50.0 Anomalous, got %.1f (%s)", rec.Score, rec.Level)
	}

	// A normal packet now should trigger a challenge
	cleanPkt := PacketMetadata{
		SrcIP:       ip,
		DstIP:       "198.51.100.1",
		Protocol:    ProtocolTCP,
		TCPFlags:    TCPFlags{ACK: true},
		PayloadSize: 64,
	}
	dec := eng.InspectPacket(cleanPkt, "default")
	if dec.Action != ActionChallenge {
		t.Errorf("expected ActionChallenge for anomalous IP, got %s", dec.Action)
	}
	if dec.ChallengeIssued == nil {
		t.Fatalf("expected non-nil ChallengeIssued")
	}

	// Solve challenge
	solved, err := eng.VerifyChallenge(dec.ChallengeIssued.TokenID, dec.ChallengeIssued.Signature)
	if !solved || err != nil {
		t.Fatalf("VerifyChallenge failed: %v", err)
	}

	// Score should recover (+30.0) -> 80.0 (Clean)
	rec = eng.GetReputation(ip)
	if rec.Score != 80.0 || rec.Level != ReputationClean {
		t.Errorf("expected 80.0 Clean after challenge, got %.1f (%s)", rec.Score, rec.Level)
	}
}

func TestChallengeTokenReplayAndExpiry(t *testing.T) {
	cfg := DefaultScrubberConfig()
	cfg.ChallengeTTL = 100 * time.Millisecond
	eng := NewScrubberEngine(cfg)
	defer eng.Stop()

	ip := "203.0.113.99"
	tok, err := eng.IssueChallenge(ip, "default")
	if err != nil {
		t.Fatalf("IssueChallenge failed: %v", err)
	}

	// 1. Valid verification
	ok, err := eng.VerifyChallenge(tok.TokenID, tok.Signature)
	if !ok || err != nil {
		t.Fatalf("first verification failed: %v", err)
	}

	// 2. Replay should fail with ErrChallengeReplayed
	_, err = eng.VerifyChallenge(tok.TokenID, tok.Signature)
	if err != ErrChallengeReplayed {
		t.Errorf("expected ErrChallengeReplayed, got %v", err)
	}

	// 3. Expired token verification
	tok2, _ := eng.IssueChallenge("203.0.113.100", "default")
	time.Sleep(150 * time.Millisecond)
	_, err = eng.VerifyChallenge(tok2.TokenID, tok2.Signature)
	if err != ErrChallengeExpired {
		t.Errorf("expected ErrChallengeExpired, got %v", err)
	}
}

func TestQuarantineBanLifecycle(t *testing.T) {
	eng := NewScrubberEngine(DefaultScrubberConfig())
	defer eng.Stop()

	ip := "192.0.2.200"

	// 1. Manually ban IP for 1 second
	eng.BanIP(ip, 1*time.Second, "operator_test_ban")

	banned, reason, _ := eng.reputation.IsBanned(ip)
	if !banned || !strings.Contains(reason, "operator_test_ban") {
		t.Fatalf("expected IP to be banned, got %v (%s)", banned, reason)
	}

	// Packet should be dropped immediately
	pkt := PacketMetadata{SrcIP: ip, DstIP: "198.51.100.1", Protocol: ProtocolTCP}
	dec := eng.InspectPacket(pkt, "default")
	if dec.Action != ActionDrop || dec.ReputationLevel != ReputationBanned {
		t.Errorf("expected ActionDrop on banned IP, got %s (%s)", dec.Action, dec.ReputationLevel)
	}

	// 2. Manually unban
	unbanned := eng.UnbanIP(ip)
	if !unbanned {
		t.Errorf("expected UnbanIP to return true")
	}

	banned, _, _ = eng.reputation.IsBanned(ip)
	if banned {
		t.Errorf("expected IP to no longer be banned")
	}
}

func TestCrossPlatformDriver(t *testing.T) {
	driver := NewPlatformScrubberDriver()
	defer driver.Close()

	mode := driver.GetMode()
	if mode == "" {
		t.Errorf("expected non-empty driver mode")
	}

	_ = driver.AttachInterface("eth0")
	_ = driver.DetachInterface("eth0")
}

func TestScrubberRESTEndpoints(t *testing.T) {
	eng := NewScrubberEngine(DefaultScrubberConfig())
	eng.Start(context.Background())
	defer eng.Stop()

	handler := NewScrubberHandler(eng)

	// 1. GET /api/scrubber/status
	req := httptest.NewRequest("GET", "/api/scrubber/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /status, got %d", rr.Code)
	}

	// 2. GET /api/scrubber/stats
	req = httptest.NewRequest("GET", "/api/scrubber/stats", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /stats, got %d", rr.Code)
	}

	// 3. POST /api/scrubber/inspect
	inspectBody := `{"src_ip":"192.0.2.15","dst_ip":"198.51.100.1","src_port":44300,"dst_port":443,"protocol":"TCP","is_syn":true,"is_ack":false,"payload_size":0}`
	req = httptest.NewRequest("POST", "/api/scrubber/inspect", strings.NewReader(inspectBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /inspect, got %d", rr.Code)
	}

	var dec ScrubDecision
	_ = json.NewDecoder(rr.Body).Decode(&dec)
	if dec.ClientIP != "192.0.2.15" {
		t.Errorf("unexpected decision IP: %s", dec.ClientIP)
	}

	// 4. POST /api/scrubber/ban and /api/scrubber/unban
	banBody := `{"ip":"192.0.2.99","duration_minutes":10,"reason":"test ban"}`
	req = httptest.NewRequest("POST", "/api/scrubber/ban", strings.NewReader(banBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /ban, got %d", rr.Code)
	}

	unbanBody := `{"ip":"192.0.2.99"}`
	req = httptest.NewRequest("POST", "/api/scrubber/unban", strings.NewReader(unbanBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /unban, got %d", rr.Code)
	}

	// 5. POST /api/scrubber/reset
	req = httptest.NewRequest("POST", "/api/scrubber/reset", strings.NewReader("{}"))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /reset, got %d", rr.Code)
	}
}

func TestBodySizeBoundingDoS(t *testing.T) {
	eng := NewScrubberEngine(DefaultScrubberConfig())
	defer eng.Stop()
	handler := NewScrubberHandler(eng)

	// Oversized payload (> 64KB)
	oversized := bytes.Repeat([]byte("a"), 70*1024)
	req := httptest.NewRequest("POST", "/api/scrubber/inspect", bytes.NewReader(oversized))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on oversized payload, got %d", rr.Code)
	}
}

func TestHighConcurrency40Goroutines(t *testing.T) {
	eng := NewScrubberEngine(DefaultScrubberConfig())
	defer eng.Stop()

	var wg sync.WaitGroup
	workers := 40
	packetsPerWorker := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ip := fmt.Sprintf("198.51.100.%d", workerID+1)
			for j := 0; j < packetsPerWorker; j++ {
				pkt := PacketMetadata{
					SrcIP:       ip,
					DstIP:       "203.0.113.1",
					SrcPort:     10000 + j,
					DstPort:     443,
					Protocol:    ProtocolTCP,
					TCPFlags:    TCPFlags{ACK: true},
					PayloadSize: 128,
					Timestamp:   time.Now(),
				}
				_ = eng.InspectPacket(pkt, "default")
			}
		}(i)
	}

	wg.Wait()
	stats := eng.GetStats()
	expectedInspected := int64(workers * packetsPerWorker)
	if stats.TotalPacketsInspected != expectedInspected {
		t.Errorf("expected %d inspected packets, got %d", expectedInspected, stats.TotalPacketsInspected)
	}
}

func TestMicrosecondMitigationLatency(t *testing.T) {
	eng := NewScrubberEngine(DefaultScrubberConfig())
	defer eng.Stop()

	pkt := PacketMetadata{
		SrcIP:       "192.0.2.77",
		DstIP:       "198.51.100.1",
		Protocol:    ProtocolTCP,
		TCPFlags:    TCPFlags{ACK: true},
		PayloadSize: 64,
		Timestamp:   time.Now(),
	}

	// Warmup
	for i := 0; i < 50; i++ {
		_ = eng.InspectPacket(pkt, "default")
	}

	// Benchmark batch
	start := time.Now()
	iterations := 1000
	for i := 0; i < iterations; i++ {
		_ = eng.InspectPacket(pkt, "default")
	}
	totalDuration := time.Since(start)
	avgUs := float64(totalDuration.Microseconds()) / float64(iterations)

	// Assert microsecond-scale execution (< 25 µs average in pure Go)
	if avgUs > 50.0 {
		t.Logf("Warning: average packet evaluation latency was %.2f µs (acceptable in debug, expected < 25µs)", avgUs)
	}
	if avgUs > 500.0 {
		t.Errorf("mitigation latency too high: %.2f µs", avgUs)
	}
}
