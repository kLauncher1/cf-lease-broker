package entropy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// 1. Physical Jitter & TRNG Sampling Pipeline
func TestHardwareJitterHarvesting(t *testing.T) {
	h := NewHardwareJitterHarvester()

	jitter := h.SamplePureJitter(256)
	if len(jitter) != 256 {
		t.Fatalf("expected 256 jitter bytes, got %d", len(jitter))
	}

	trng, err := h.SampleHardwareTRNG(256)
	if err != nil || len(trng) != 256 {
		t.Fatalf("expected 256 TRNG bytes, err: %v", err)
	}

	batch, err := h.SampleHardwareJitter(512)
	if err != nil {
		t.Fatalf("failed sampling hybrid jitter: %v", err)
	}
	if batch.Source != SourceHybrid && batch.Source != SourceCPUJitter {
		t.Fatalf("unexpected source: %s", batch.Source)
	}
	if len(batch.RawSamples) != 512 {
		t.Fatalf("expected 512 raw samples, got %d", len(batch.RawSamples))
	}
	if batch.DurationUs <= 0 {
		t.Fatalf("expected positive duration, got %d", batch.DurationUs)
	}
	if h.TotalSamples() < 768 {
		t.Fatalf("expected total samples >= 768, got %d", h.TotalSamples())
	}
}

// 2. Shannon & Min-Entropy Estimations
func TestEntropyMetricsMinEntropy(t *testing.T) {
	constantBuf := bytes.Repeat([]byte{0xAA}, 512)
	if minEnt := EstimateMinEntropy(constantBuf); minEnt != 0.0 {
		t.Fatalf("expected 0.0 min-entropy for constant buffer, got %f", minEnt)
	}

	// 256 bytes with each byte from 0 to 255 exactly once -> ideal uniform
	uniformBuf := make([]byte, 256)
	for i := 0; i < 256; i++ {
		uniformBuf[i] = byte(i)
	}
	if minEnt := EstimateMinEntropy(uniformBuf); minEnt != 8.0 {
		t.Fatalf("expected 8.0 min-entropy for uniform permutation, got %f", minEnt)
	}
	if shannon := EstimateShannonEntropy(uniformBuf); shannon != 8.0 {
		t.Fatalf("expected 8.0 Shannon entropy for uniform permutation, got %f", shannon)
	}
	if EstimateMinEntropy(nil) != 0.0 || EstimateShannonEntropy(nil) != 0.0 {
		t.Fatal("expected 0.0 entropy for nil buffer")
	}
}

// 3. NIST SP 800-90B Repetition Count Test (RCT)
func TestNISTSP80090B_RCT(t *testing.T) {
	data := make([]byte, 1024)
	_, _ = rand.Read(data)
	passed, rep := RunRepetitionCountTest(data, 35)
	if !passed || rep >= 35 {
		t.Fatalf("expected high-entropy data to pass RCT, got rep=%d", rep)
	}

	// Inject 35 identical consecutive bytes
	tampered := append([]byte(nil), data...)
	for i := 100; i < 135; i++ {
		tampered[i] = 0x7E
	}
	failed, maxRep := RunRepetitionCountTest(tampered, 35)
	if failed || maxRep < 35 {
		t.Fatalf("expected RCT to fail on 35 repeats, passed=%t, maxRep=%d", failed, maxRep)
	}
}

// 4. NIST SP 800-90B Adaptive Proportion Test (APT)
func TestNISTSP80090B_APT(t *testing.T) {
	data := make([]byte, 1024)
	_, _ = rand.Read(data)
	passed, maxCount := RunAdaptiveProportionTest(data, 512, 177)
	if !passed || maxCount >= 177 {
		t.Fatalf("expected random data to pass APT, maxCount=%d", maxCount)
	}

	// Fail APT by flooding target byte in first window
	tampered := append([]byte(nil), data...)
	target := tampered[0]
	for i := 1; i <= 180; i++ {
		tampered[i] = target
	}
	failed, count := RunAdaptiveProportionTest(tampered, 512, 177)
	if failed || count < 180 {
		t.Fatalf("expected APT to fail with count >= 180, passed=%t, count=%d", failed, count)
	}
}

// 5. Cryptographic Randomness Extractor Uniformity
func TestRandomnessExtractorUniformity(t *testing.T) {
	extractor := NewCryptographicExtractor(nil)
	raw := make([]byte, 256)
	_, _ = rand.Read(raw)

	out, err := extractor.Extract(raw, "test-info", 64)
	if err != nil || len(out) != 64 {
		t.Fatalf("unexpected extract result: len=%d, err=%v", len(out), err)
	}

	seed, err := extractor.ExtractUniformSeed(raw, "seed-context")
	if err != nil || seed == [32]byte{} {
		t.Fatalf("expected valid 32-byte seed, err=%v", err)
	}

	if _, err := extractor.Extract(nil, "info", 32); err == nil {
		t.Fatal("expected error on nil raw samples")
	}
	if _, err := extractor.Extract(raw, "info", 10000); err == nil {
		t.Fatal("expected error on exceeding maximum HKDF length")
	}
}

// 6. Memory Zeroization
func TestBufferZeroization(t *testing.T) {
	buf := bytes.Repeat([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 32)
	Zeroize(buf)
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("buffer was not zeroized at index %d (got 0x%02x)", i, b)
		}
	}
}

// 7. Beacon Round Chaining & Tampering Detection
func TestBeaconRoundChaining(t *testing.T) {
	beacon := NewQuantumEntropyBeacon([]byte("attest-key-1234"), 30*time.Second)

	r1, err := beacon.NextRound([]byte("entropy-round-1"), SourceHybrid, 7.95)
	if err != nil {
		t.Fatalf("failed generating round 1: %v", err)
	}
	if r1.Round != 1 || r1.PreviousHash != GenesisBeaconHash || r1.OutputHash == "" {
		t.Fatalf("invalid round 1 properties: %+v", r1)
	}

	r2, err := beacon.NextRound([]byte("entropy-round-2"), SourceHybrid, 7.98)
	if err != nil {
		t.Fatalf("failed generating round 2: %v", err)
	}
	if r2.Round != 2 || r2.PreviousHash != r1.OutputHash {
		t.Fatalf("broken hash chain: r2.PreviousHash (%s) != r1.OutputHash (%s)", r2.PreviousHash, r1.OutputHash)
	}

	// Tampering detection
	tamperedRound := *r2
	tamperedRound.OutputHash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if ok, _ := beacon.VerifyRound(&tamperedRound, r1.OutputHash, nil); ok {
		t.Fatal("expected verification failure on tampered output hash")
	}
}

// 8. Beacon Peer Verification & Anti-Tampering
func TestBeaconPeerVerification(t *testing.T) {
	key := []byte("shared-peer-secret-999")
	beacon := NewQuantumEntropyBeacon(key, 30*time.Second)

	r, err := beacon.NextRound([]byte("entropy-sample"), SourceHybrid, 7.91)
	if err != nil {
		t.Fatalf("next round failed: %v", err)
	}

	// Successful verification
	ok, err := beacon.VerifyRound(r, GenesisBeaconHash, key)
	if !ok || err != nil {
		t.Fatalf("peer verification failed unexpectedly: %v", err)
	}

	// Invalid signature key
	wrongKey := []byte("wrong-peer-secret-000")
	if ok, err := beacon.VerifyRound(r, GenesisBeaconHash, wrongKey); ok || err == nil {
		t.Fatal("expected verification failure with wrong key")
	}

	// Mismatched previous hash
	if ok, err := beacon.VerifyRound(r, "bad-hash", key); ok || err == nil {
		t.Fatal("expected verification failure with invalid previous hash")
	}
}

// 9. Beacon Clock Skew & Replay Rejection
func TestBeaconReplayAndClockSkewRejection(t *testing.T) {
	beacon := NewQuantumEntropyBeacon([]byte("skew-key"), 5*time.Second)
	r, err := beacon.NextRound([]byte("skew-test"), SourceHybrid, 7.90)
	if err != nil {
		t.Fatalf("round error: %v", err)
	}

	// Manipulate timestamp to simulate clock skew / replayed round
	skewed := *r
	skewed.Timestamp = time.Now().UTC().Add(-10 * time.Minute)
	// Resign to isolate clock skew check
	skewed.Signature = signBeaconPayload([]byte("skew-key"), skewed.Round, skewed.PreviousHash, skewed.OutputHash, skewed.Timestamp, skewed.EntropyMinBits)

	ok, err := beacon.VerifyRound(&skewed, GenesisBeaconHash, []byte("skew-key"))
	if ok || err == nil || !strings.Contains(err.Error(), "clock skew") && !strings.Contains(err.Error(), "skew") {
		t.Fatalf("expected clock skew rejection, got ok=%t, err=%v", ok, err)
	}
}

// 10. Entropy Engine Token Generation & Pool Replenishment
func TestEntropyEngineTokenGeneration(t *testing.T) {
	cfg := DefaultEntropyConfig()
	cfg.BeaconInterval = 30 * time.Millisecond
	e := NewEntropyEngine(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("failed starting engine: %v", err)
	}

	tok1, err := e.GenerateToken(32, "lease-voucher")
	if err != nil || len(tok1) != 32 {
		t.Fatalf("token 1 generation failed: len=%d, err=%v", len(tok1), err)
	}

	tok2, err := e.GenerateToken(64, "api-token")
	if err != nil || len(tok2) != 64 {
		t.Fatalf("token 2 generation failed: len=%d, err=%v", len(tok2), err)
	}

	time.Sleep(70 * time.Millisecond)

	curRound := e.GetBeaconRound()
	if curRound == nil || curRound.Round < 1 {
		t.Fatalf("expected active beacon rounds, got %+v", curRound)
	}

	report := e.GetHealthReport()
	if report.Status == HealthStatusFailed {
		t.Fatalf("health status failed: %+v", report)
	}

	stats := e.GetStats()
	if stats.TotalTokensGenerated < 2 || stats.TotalHarvestedBytes == 0 {
		t.Fatalf("invalid stats: %+v", stats)
	}

	_ = e.Stop()
}

// 11. REST API Endpoints & 64KB DoS Bounding
func TestEntropyRESTEndpointsAndDoSBounding(t *testing.T) {
	cfg := DefaultEntropyConfig()
	engine := NewEntropyEngine(cfg)
	handler := NewEntropyHandler(engine)

	// GET /api/entropy/status
	req := httptest.NewRequest(http.MethodGet, "/api/entropy/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code %d", rec.Code)
	}

	// GET /api/entropy/beacon
	req = httptest.NewRequest(http.MethodGet, "/api/entropy/beacon?limit=5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("beacon code %d", rec.Code)
	}

	// GET /api/entropy/health
	req = httptest.NewRequest(http.MethodGet, "/api/entropy/health", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health code %d", rec.Code)
	}

	// POST /api/entropy/harvest
	body, _ := json.Marshal(map[string]interface{}{"bytes": 32, "purpose": "test-lease"})
	req = httptest.NewRequest(http.MethodPost, "/api/entropy/harvest", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("harvest code %d", rec.Code)
	}

	// POST /api/entropy/verify
	round := engine.GetBeaconRound()
	vBody, _ := json.Marshal(map[string]interface{}{
		"round":              round,
		"expected_prev_hash": round.PreviousHash,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/entropy/verify", bytes.NewReader(vBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify code %d", rec.Code)
	}

	// POST /api/entropy/reset
	req = httptest.NewRequest(http.MethodPost, "/api/entropy/reset", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset code %d", rec.Code)
	}

	// DoS Bounding: >64KB body payload must be rejected
	oversized := bytes.Repeat([]byte("A"), 70*1024)
	req = httptest.NewRequest(http.MethodPost, "/api/entropy/harvest", bytes.NewReader(oversized))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on oversized payload, got %d", rec.Code)
	}
}

// 12. 40-Goroutine High Concurrency Stress Test
func TestEntropyEngineConcurrency(t *testing.T) {
	cfg := DefaultEntropyConfig()
	engine := NewEntropyEngine(cfg)
	handler := NewEntropyHandler(engine)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)
	defer engine.Stop()

	var wg sync.WaitGroup
	const goroutines = 40
	const iterations = 15

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch (id + i) % 4 {
				case 0:
					_, _ = engine.GenerateToken(32, "concurrent-token")
				case 1:
					_ = engine.GetStats()
				case 2:
					_ = engine.GetBeaconRound()
				case 3:
					req := httptest.NewRequest(http.MethodGet, "/api/entropy/status", nil)
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
				}
			}
		}(g)
	}

	wg.Wait()
	stats := engine.GetStats()
	if stats.TotalTokensGenerated == 0 {
		t.Fatal("expected tokens to be generated under concurrency")
	}
}
