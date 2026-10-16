package rotator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

func setupTestEnvironment() (*TokenRotator, *RevocationMesh, *broker.LeaseEngine, *AttestationVerifier) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	mesh := NewRevocationMesh("node-test-1", []byte("test-cluster-secret-key-32bytes"))
	verifier := NewAttestationVerifier([]byte("test-cluster-secret-key-32bytes"))
	cfg := RotatorConfig{
		CheckInterval:         25 * time.Millisecond,
		RenewalThresholdRatio: 0.50,
		GracePeriod:           100 * time.Millisecond,
		MaxRenewals:           10,
		AutoRenewEnabled:      true,
	}
	tr := NewTokenRotator(cfg, mesh, le, verifier)
	return tr, mesh, le, verifier
}

func TestTokenRegistrationAndRenewalThreshold(t *testing.T) {
	tr, _, le, _ := setupTestEnvironment()
	v, err := le.GrantLease("test-service", "Workers", 10*time.Hour)
	if err != nil {
		t.Fatalf("failed to grant lease: %v", err)
	}

	tok := tr.RegisterVoucher(*v)
	if tok == nil {
		t.Fatal("expected registered token, got nil")
	}

	if tok.State != TokenStateActive {
		t.Fatalf("expected state ACTIVE, got %s", tok.State)
	}

	expectedRenewAt := tok.IssuedAt.Add(5 * time.Hour)
	diff := tok.RenewAt.Sub(expectedRenewAt)
	if diff < -time.Second || diff > time.Second {
		t.Fatalf("expected renewAt ~5h, got diff: %v", diff)
	}

	if _, valid := tr.ValidateTokenKey(tok.TokenKey); !valid {
		t.Fatal("expected newly registered token key to be valid")
	}
}

func TestAutomatedBackgroundRenewal(t *testing.T) {
	tr, _, le, _ := setupTestEnvironment()
	v, err := le.GrantLease("test-svc", "Workers", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("grant lease: %v", err)
	}

	tok := tr.RegisterVoucher(*v)
	initialKey := tok.TokenKey

	tr.Start()
	defer tr.Stop()

	// Wait for background worker to trigger renewal past 50ms
	time.Sleep(120 * time.Millisecond)

	updatedTok, ok := tr.GetToken(tok.TokenID)
	if !ok {
		t.Fatal("token missing")
	}

	if updatedTok.RenewalCount == 0 {
		t.Fatal("expected renewal count > 0 after background interval")
	}
	if updatedTok.TokenKey == initialKey {
		t.Fatal("expected token key to rotate to new value")
	}

	// Validate synchronized lease engine voucher
	leaseVoucher, found := le.GetLease(v.LeaseID)
	if !found {
		t.Fatal("lease voucher not found in lease engine")
	}
	if leaseVoucher.TokenKey != updatedTok.TokenKey {
		t.Fatalf("lease engine token key out of sync: expected %s, got %s", updatedTok.TokenKey, leaseVoucher.TokenKey)
	}
}

func TestGracePeriodDualTokenHandover(t *testing.T) {
	tr, _, le, _ := setupTestEnvironment()
	v, _ := le.GrantLease("handover-svc", "Workers", 1*time.Hour)
	tok := tr.RegisterVoucher(*v)
	oldKey := tok.TokenKey

	ev, err := tr.RenewToken(tok.TokenID, "manual_test_renew")
	if err != nil {
		t.Fatalf("failed to renew: %v", err)
	}

	newKey := ev.NewTokenKey

	// Both new and old token keys should be valid during grace period
	if _, valid := tr.ValidateTokenKey(newKey); !valid {
		t.Fatal("expected new key to be valid")
	}
	if _, valid := tr.ValidateTokenKey(oldKey); !valid {
		t.Fatal("expected old key to be valid during grace period")
	}

	// Artificially expire grace period
	tr.mu.Lock()
	tokRef := tr.tokens[tok.TokenID]
	tokRef.GraceUntil = time.Now().UTC().Add(-time.Second)
	tr.mu.Unlock()

	// Sweep
	tr.CheckAndRenew()

	if _, valid := tr.ValidateTokenKey(oldKey); valid {
		t.Fatal("expected old key to be rejected after grace period elapsed")
	}
	if _, valid := tr.ValidateTokenKey(newKey); !valid {
		t.Fatal("new key must remain valid")
	}
}

func TestGossipMeshSub5msPropagation(t *testing.T) {
	secret := []byte("gossip-secret-32-byte-string-ok")
	meshA := NewRevocationMesh("broker-node-a", secret)
	meshB := NewRevocationMesh("broker-node-b", secret)

	meshA.LinkDirectPeer(meshB)
	meshB.LinkDirectPeer(meshA)

	start := time.Now()
	tombstone := meshA.Revoke("lse_mesh_test_01", "tok_mesh_test_01", "compromised_token")
	elapsed := time.Since(start)

	if elapsed >= 5*time.Millisecond {
		t.Fatalf("expected propagation under 5ms, took: %v", elapsed)
	}

	// Mesh B must have received the revocation immediately
	if !meshB.IsRevoked("lse_mesh_test_01") {
		t.Fatal("expected mesh B to immediately recognize revoked lease")
	}
	if !meshB.IsRevoked("tok_mesh_test_01") {
		t.Fatal("expected mesh B to immediately recognize revoked token")
	}

	retrieved, found := meshB.GetTombstone("lse_mesh_test_01")
	if !found {
		t.Fatal("tombstone missing in mesh B")
	}
	if retrieved.SignatureHex != tombstone.SignatureHex {
		t.Fatal("signature mismatch across mesh nodes")
	}
	if meshB.GetAveragePropagationLatencyMs() > 5.0 {
		t.Fatalf("average propagation latency too high: %f ms", meshB.GetAveragePropagationLatencyMs())
	}
}

func TestGossipAntiEntropyAndDeduplication(t *testing.T) {
	secret := []byte("anti-entropy-secret-key-32bytes")
	mesh := NewRevocationMesh("node-eval", secret)

	t1 := RevocationTombstone{
		LeaseID:           "lse_ae_1",
		TokenID:           "tok_ae_1",
		Reason:            "anti_entropy_test",
		RevokedAt:         time.Now().UTC(),
		RevokedAtUnixNano: time.Now().UTC().UnixNano(),
		RevokerNodeID:     "remote-node",
		SequenceNum:       1,
	}
	t1.SignatureHex = mesh.SignTombstone(t1)

	packet := GossipPacket{
		SenderNodeID: "remote-node",
		Sequence:     1,
		Tombstones:   []RevocationTombstone{t1, t1}, // duplicate in packet
		SentAt:       time.Now().UTC(),
	}

	applied, total := mesh.ProcessGossipPacket(packet)
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if applied != 1 {
		t.Fatalf("expected 1 applied (deduplicated), got %d", applied)
	}

	// Tampered signature should be rejected
	tampered := t1
	tampered.LeaseID = "lse_ae_tampered"
	tampered.SignatureHex = "bad-sig-hex"
	if mesh.ApplyRemoteTombstone(tampered) {
		t.Fatal("expected tampered tombstone to be rejected")
	}
}

func TestHardwareAttestationDelegatedRefresh(t *testing.T) {
	tr, _, le, verifier := setupTestEnvironment()
	v, _ := le.GrantLease("attested-svc", "Workers", 2*time.Hour)
	tok := tr.RegisterVoucher(*v)

	nonce, ts := verifier.GenerateChallenge()
	pcrDigest := strings.Repeat("a", 64)
	tpmQuote := "TPM2_Quote[PCR0-7=1234567890abcdef,EK=abcdef1234567890]"

	req := &DelegatedRefreshRequest{
		TokenID:         tok.TokenID,
		LeaseID:         tok.LeaseID,
		TenantID:        tok.TenantID,
		TargetService:   tok.TargetService,
		DelegatedByNode: "edge-worker-node-1",
		Nonce:           nonce,
		HardwareQuote:   tpmQuote,
		PCRDigest:       pcrDigest,
		Timestamp:       ts,
	}

	resp, err := tr.ProcessDelegatedRefresh(req)
	if err != nil {
		t.Fatalf("delegated refresh failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}
	if resp.NewTokenKey == "" {
		t.Fatal("expected new token key")
	}

	// Replay attack test: re-submitting with the same consumed nonce must fail
	_, replayErr := tr.ProcessDelegatedRefresh(req)
	if replayErr == nil {
		t.Fatal("expected replay attack with consumed nonce to be rejected")
	}

	// Malformed PCR digest test
	nonce2, ts2 := verifier.GenerateChallenge()
	reqBadPCR := &DelegatedRefreshRequest{
		TokenID:         tok.TokenID,
		Nonce:           nonce2,
		HardwareQuote:   tpmQuote,
		PCRDigest:       "too-short",
		Timestamp:       ts2,
	}
	_, badErr := tr.ProcessDelegatedRefresh(reqBadPCR)
	if badErr == nil {
		t.Fatal("expected malformed PCR digest to be rejected")
	}
}

func TestRotatorHTTPEndpoints(t *testing.T) {
	tr, mesh, le, verifier := setupTestEnvironment()
	v, _ := le.GrantLease("http-svc", "Workers", 1*time.Hour)
	tok := tr.RegisterVoucher(*v)

	handler := NewRotatorHandler(tr, mesh, verifier)
	server := httptest.NewServer(handler)
	defer server.Close()

	// 1. GET /api/rotator/status
	res, err := http.Get(server.URL + "/api/rotator/status")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/rotator/status failed: %v, code: %d", err, res.StatusCode)
	}

	// 2. GET /api/rotator/tokens
	res, err = http.Get(server.URL + "/api/rotator/tokens")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/rotator/tokens failed: %v, code: %d", err, res.StatusCode)
	}

	// 3. POST /api/rotator/renew
	renewBody, _ := json.Marshal(map[string]string{
		"token_id": tok.TokenID,
		"reason":   "api_test",
	})
	res, err = http.Post(server.URL+"/api/rotator/renew", "application/json", bytes.NewReader(renewBody))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/rotator/renew failed: %v, code: %d", err, res.StatusCode)
	}

	// 4. GET /api/rotator/challenge
	res, err = http.Get(server.URL + "/api/rotator/challenge")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/rotator/challenge failed: %v, code: %d", err, res.StatusCode)
	}
	var chal map[string]interface{}
	_ = json.NewDecoder(res.Body).Decode(&chal)
	if chal["nonce"] == nil || chal["timestamp"] == nil {
		t.Fatal("expected nonce and timestamp in challenge response")
	}

	// 5. POST /api/rotator/revoke
	revokeBody, _ := json.Marshal(map[string]string{
		"token_id": tok.TokenID,
		"reason":   "http_test_revoke",
	})
	res, err = http.Post(server.URL+"/api/rotator/revoke", "application/json", bytes.NewReader(revokeBody))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/rotator/revoke failed: %v, code: %d", err, res.StatusCode)
	}
}

func TestRotatorBodySizeBoundingAndDoS(t *testing.T) {
	tr, mesh, _, verifier := setupTestEnvironment()
	handler := NewRotatorHandler(tr, mesh, verifier)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Send an oversized payload > 64KB
	hugePayload := strings.Repeat("A", 128*1024)
	res, err := http.Post(server.URL+"/api/rotator/renew", "application/json", strings.NewReader(hugePayload))
	if err != nil {
		t.Fatalf("network error: %v", err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 Bad Request for oversized body, got: %d", res.StatusCode)
	}
}

func TestRotatorHighConcurrency(t *testing.T) {
	tr, mesh, le, _ := setupTestEnvironment()
	v, _ := le.GrantLease("concurrent-svc", "Workers", 24*time.Hour)
	tok := tr.RegisterVoucher(*v)

	var wg sync.WaitGroup
	workers := 40

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%3 == 0 {
				_, _ = tr.RenewToken(tok.TokenID, "concurrent_renew")
			} else if idx%3 == 1 {
				_ = tr.GetSummary()
				_ = tr.ListTokens()
			} else {
				mesh.Revoke("lse_concur_"+string(rune('a'+idx)), "", "concurrent_mesh")
			}
		}(i)
	}

	wg.Wait()
}
