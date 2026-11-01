package pqc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPQC_EntropyHarvestingAndHealthChecks(t *testing.T) {
	h := NewEntropyHarvester()

	data := h.Harvest(64)
	if len(data) != 64 {
		t.Fatalf("expected 64 bytes harvested, got %d", len(data))
	}

	stats := h.GetStats()
	if !stats.TRNGActive {
		t.Fatalf("expected TRNG to be active")
	}
	if stats.ShannonEntropy < 7.0 {
		t.Fatalf("expected Shannon entropy >= 7.0, got %f", stats.ShannonEntropy)
	}
	if stats.MinEntropyBits < 5.0 {
		t.Fatalf("expected Min-entropy >= 5.0, got %f", stats.MinEntropyBits)
	}
	if stats.HealthStatus != "healthy" {
		t.Fatalf("expected healthy status, got %s", stats.HealthStatus)
	}
}

func TestPQC_MLKEM_KeyGenAndEncapsulationRoundtrip(t *testing.T) {
	algs := []string{AlgorithmMLKEM768, AlgorithmMLKEM1024}

	for _, alg := range algs {
		t.Run(alg, func(t *testing.T) {
			seed := [32]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
			kp, err := GenerateKEMKeyPair(alg, seed, false)
			if err != nil {
				t.Fatalf("keygen failed: %v", err)
			}
			if len(kp.PublicKeyBytes) == 0 || len(kp.PrivateKeyBytes) == 0 {
				t.Fatalf("empty keys generated")
			}

			entropy := []byte("random-entropy-seed-for-encapsulation-32b")
			res, err := Encapsulate(alg, kp.PublicKeyBytes, entropy, false)
			if err != nil {
				t.Fatalf("encapsulation failed: %v", err)
			}
			if len(res.Ciphertext) == 0 || len(res.SharedSecret) != 32 {
				t.Fatalf("invalid encapsulation result")
			}

			decSecret, err := Decapsulate(alg, kp, res.Ciphertext, false)
			if err != nil {
				t.Fatalf("decapsulation failed: %v", err)
			}

			if !ConstantTimeSecretEqual(res.SharedSecret, decSecret) {
				t.Fatalf("shared secret mismatch: enc=%s, dec=%s", hex.EncodeToString(res.SharedSecret), hex.EncodeToString(decSecret))
			}
		})
	}
}

func TestPQC_HybridKEM_X25519Integration(t *testing.T) {
	seed := [32]byte{0xAA, 0xBB, 0xCC, 0xDD}
	kp, err := GenerateKEMKeyPair(AlgorithmHybridX25519, seed, true)
	if err != nil {
		t.Fatalf("hybrid keygen failed: %v", err)
	}

	if len(kp.ECDHPublicKey) != 32 || kp.ECDHPrivateKey == nil {
		t.Fatalf("expected 32-byte ECDH public key")
	}

	entropy := []byte("high-entropy-client-seed-for-hybrid-kem-32b")
	res, err := Encapsulate(AlgorithmHybridX25519, kp.PublicKeyBytes, entropy, true)
	if err != nil {
		t.Fatalf("hybrid encapsulation failed: %v", err)
	}

	// Verify hybrid master secret length
	if len(res.SharedSecret) != 32 {
		t.Fatalf("expected 32-byte shared secret, got %d", len(res.SharedSecret))
	}

	decSecret, err := Decapsulate(AlgorithmHybridX25519, kp, res.Ciphertext, true)
	if err != nil {
		t.Fatalf("hybrid decapsulation failed: %v", err)
	}

	if !ConstantTimeSecretEqual(res.SharedSecret, decSecret) {
		t.Fatalf("hybrid shared secrets do not match")
	}
}

func TestPQC_CiphertextTamperingDetection(t *testing.T) {
	seed := [32]byte{0x12, 0x34, 0x56, 0x78}
	kp, _ := GenerateKEMKeyPair(AlgorithmMLKEM768, seed, false)
	res, _ := Encapsulate(AlgorithmMLKEM768, kp.PublicKeyBytes, []byte("seed-entropy-test"), false)

	// Tamper with ciphertext byte
	tampered := make([]byte, len(res.Ciphertext))
	copy(tampered, res.Ciphertext)
	tampered[len(tampered)/2] ^= 0xFF

	decSecret, err := Decapsulate(AlgorithmMLKEM768, kp, tampered, false)
	if err == nil && ConstantTimeSecretEqual(res.SharedSecret, decSecret) {
		t.Fatalf("tampered ciphertext must not yield identical shared secret")
	}
}

func TestPQC_MLDSA_SignAndVerifyRoundtrip(t *testing.T) {
	algs := []string{AlgorithmMLDSA65, AlgorithmMLDSA87}

	for _, alg := range algs {
		t.Run(alg, func(t *testing.T) {
			seed := [32]byte{0x99, 0x88, 0x77, 0x66}
			kp, err := GenerateMLDSAKeyPair("test-key", alg, seed)
			if err != nil {
				t.Fatalf("ML-DSA keygen failed: %v", err)
			}

			message := []byte("cloud-broker-voucher-payload-assertion")
			tracker := NewStateCounterTracker()

			sig, err := SignMessage(kp, message, 100, []byte("entropy-seed"))
			if err != nil {
				t.Fatalf("signing failed: %v", err)
			}

			if sig.StateCounter != 100 || sig.KeyID != "test-key" {
				t.Fatalf("invalid signature metadata")
			}

			valid := VerifySignature(kp.PublicKey, alg, message, sig, tracker)
			if !valid {
				t.Fatalf("expected signature to be valid")
			}
		})
	}
}

func TestPQC_MLDSA_SignatureTamperingAndReplay(t *testing.T) {
	seed := [32]byte{0x11, 0x22, 0x33, 0x44}
	kp, _ := GenerateMLDSAKeyPair("tamper-test", AlgorithmMLDSA65, seed)
	tracker := NewStateCounterTracker()

	message := []byte("authentic-lease-statement")
	sig, _ := SignMessage(kp, message, 1, []byte("entropy"))

	// 1. Initial verification succeeds
	if !VerifySignature(kp.PublicKey, AlgorithmMLDSA65, message, sig, tracker) {
		t.Fatalf("expected initial signature verification to succeed")
	}

	// 2. Replay with same counter must fail
	if VerifySignature(kp.PublicKey, AlgorithmMLDSA65, message, sig, tracker) {
		t.Fatalf("expected replay of identical counter to fail")
	}

	// 3. Message alteration must fail
	sig2, _ := SignMessage(kp, message, 2, []byte("entropy"))
	alteredMessage := []byte("authentic-lease-statement-TAMPERED")
	if VerifySignature(kp.PublicKey, AlgorithmMLDSA65, alteredMessage, sig2, tracker) {
		t.Fatalf("expected altered message verification to fail")
	}
}

func TestPQC_InterBrokerVoucherSigning(t *testing.T) {
	cfg := DefaultPQCConfig()
	engine := NewPQCEngine(cfg)

	voucher := map[string]any{
		"lease_id": "lease-cf-alpha-99",
		"tenant":   "tenant-us-east",
		"service":  "Workers",
		"issued":   time.Now().Unix(),
	}

	sig, err := engine.SignVoucher("broker-edge-pqc-root", voucher)
	if err != nil {
		t.Fatalf("SignVoucher failed: %v", err)
	}

	valid := engine.VerifyVoucher(voucher, sig)
	if !valid {
		t.Fatalf("VerifyVoucher failed on authentic voucher")
	}

	// Corrupt voucher payload
	corrupted := map[string]any{
		"lease_id": "lease-cf-alpha-99",
		"tenant":   "tenant-us-ATTACKER",
		"service":  "Workers",
		"issued":   time.Now().Unix(),
	}
	if engine.VerifyVoucher(corrupted, sig) {
		t.Fatalf("VerifyVoucher must fail on corrupted voucher payload")
	}
}

func TestPQC_SessionExpiryAndPurge(t *testing.T) {
	cfg := DefaultPQCConfig()
	cfg.SessionTTL = 10 * time.Millisecond
	engine := NewPQCEngine(cfg)

	sess, err := engine.CreateKEMSession(AlgorithmMLKEM768, false)
	if err != nil {
		t.Fatalf("CreateKEMSession failed: %v", err)
	}

	stats := engine.GetStats()
	if stats.ActiveSessionsCount != 1 {
		t.Fatalf("expected 1 active session, got %d", stats.ActiveSessionsCount)
	}

	time.Sleep(30 * time.Millisecond)
	pruned := engine.PruneExpiredSessions()
	if pruned != 1 {
		t.Fatalf("expected 1 pruned session, got %d", pruned)
	}

	statsAfter := engine.GetStats()
	if statsAfter.ActiveSessionsCount != 0 {
		t.Fatalf("expected 0 active sessions after purge, got %d", statsAfter.ActiveSessionsCount)
	}

	// Decapsulation of expired session must fail
	_, _, err = engine.DecapsulateSession(sess.SessionID, "001122")
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestPQC_RESTEndpoints(t *testing.T) {
	cfg := DefaultPQCConfig()
	engine := NewPQCEngine(cfg)
	handler := NewPQCHandler(engine)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()

	// 1. GET /status
	res, err := client.Get(server.URL + "/status")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("GET /status failed: %v", err)
	}

	// 2. POST /kem/keypair
	reqBody := []byte(`{"algorithm":"ML-KEM-768","hybrid":true}`)
	res, err = client.Post(server.URL+"/kem/keypair", "application/json", bytes.NewReader(reqBody))
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /kem/keypair failed: %v", err)
	}
	var sess KeyExchangeSession
	_ = json.NewDecoder(res.Body).Decode(&sess)
	if sess.SessionID == "" || sess.ServerPublicKey == "" {
		t.Fatalf("invalid KEM session response: %+v", sess)
	}

	// 3. POST /kem/encapsulate
	encReq, _ := json.Marshal(map[string]any{
		"algorithm":       sess.Algorithm,
		"peer_public_key": sess.ServerPublicKey,
		"hybrid":          true,
	})
	res, err = client.Post(server.URL+"/kem/encapsulate", "application/json", bytes.NewReader(encReq))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("POST /kem/encapsulate failed: %v", err)
	}
	var encResp struct {
		CiphertextHex   string `json:"ciphertext_hex"`
		SharedSecretHex string `json:"shared_secret_hex"`
	}
	_ = json.NewDecoder(res.Body).Decode(&encResp)
	if encResp.CiphertextHex == "" || encResp.SharedSecretHex == "" {
		t.Fatalf("invalid encapsulate response")
	}

	// 4. POST /kem/decapsulate
	decReq, _ := json.Marshal(map[string]any{
		"session_id": sess.SessionID,
		"ciphertext": encResp.CiphertextHex,
	})
	res, err = client.Post(server.URL+"/kem/decapsulate", "application/json", bytes.NewReader(decReq))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("POST /kem/decapsulate failed: %v", err)
	}
	var decResp struct {
		SharedSecretHex string `json:"shared_secret_hex"`
		Status          string `json:"status"`
	}
	_ = json.NewDecoder(res.Body).Decode(&decResp)
	if decResp.SharedSecretHex != encResp.SharedSecretHex {
		t.Fatalf("decapsulated secret does not match encapsulated secret: enc=%s, dec=%s", encResp.SharedSecretHex, decResp.SharedSecretHex)
	}

	// 5. POST /sign
	signReq, _ := json.Marshal(map[string]any{
		"key_id": "broker-edge-pqc-root",
		"voucher": map[string]any{
			"lease_id": "lease-rest-test-01",
			"tenant":   "tenant-alpha",
		},
	})
	res, err = client.Post(server.URL+"/sign", "application/json", bytes.NewReader(signReq))
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /sign failed: %v", err)
	}
	var sig VoucherSignature
	_ = json.NewDecoder(res.Body).Decode(&sig)

	// 6. POST /verify
	verifyReq, _ := json.Marshal(map[string]any{
		"signature": sig,
		"voucher": map[string]any{
			"lease_id": "lease-rest-test-01",
			"tenant":   "tenant-alpha",
		},
	})
	res, err = client.Post(server.URL+"/verify", "application/json", bytes.NewReader(verifyReq))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("POST /verify failed: %v", err)
	}

	// 7. GET /entropy and POST /entropy/harvest
	res, _ = client.Get(server.URL + "/entropy")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /entropy failed")
	}
	res, _ = client.Post(server.URL+"/entropy/harvest?bytes=64", "application/json", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /entropy/harvest failed")
	}

	// 8. GET /stats
	res, _ = client.Get(server.URL + "/stats")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /stats failed")
	}

	// 9. POST /reset
	res, _ = client.Post(server.URL+"/reset", "application/json", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /reset failed")
	}
}

func TestPQC_DoSBodyBounding(t *testing.T) {
	cfg := DefaultPQCConfig()
	engine := NewPQCEngine(cfg)
	handler := NewPQCHandler(engine)
	server := httptest.NewServer(handler)
	defer server.Close()

	// 100KB payload exceeding 64KB limit
	oversized := make([]byte, 100*1024)
	res, err := server.Client().Post(server.URL+"/kem/keypair", "application/json", bytes.NewReader(oversized))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for oversized payload, got %d", res.StatusCode)
	}
}

func TestPQC_HighConcurrency40Goroutines(t *testing.T) {
	cfg := DefaultPQCConfig()
	engine := NewPQCEngine(cfg)
	engine.Start()
	defer engine.Stop()

	var wg sync.WaitGroup
	workers := 40
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			alg := AlgorithmMLKEM768
			if id%2 == 1 {
				alg = AlgorithmMLKEM1024
			}

			// 1. Session keypair
			sess, err := engine.CreateKEMSession(alg, true)
			if err != nil {
				t.Errorf("worker %d: session error: %v", id, err)
				return
			}

			// 2. Encapsulate
			enc, _, err := engine.EncapsulateAgainstPublicKey(alg, sess.ServerPublicKey, true)
			if err != nil {
				t.Errorf("worker %d: enc error: %v", id, err)
				return
			}

			// 3. Decapsulate
			decSecret, _, err := engine.DecapsulateSession(sess.SessionID, hex.EncodeToString(enc.Ciphertext))
			if err != nil {
				t.Errorf("worker %d: dec error: %v", id, err)
				return
			}
			if !ConstantTimeSecretEqual(enc.SharedSecret, decSecret) {
				t.Errorf("worker %d: secret mismatch", id)
				return
			}

			// 4. Voucher signing & verification
			voucher := map[string]any{"worker_id": id, "nonce": fmt.Sprintf("nonce-%d", id)}
			sig, err := engine.SignVoucher("broker-edge-pqc-root", voucher)
			if err != nil {
				t.Errorf("worker %d: sign error: %v", id, err)
				return
			}
			if !engine.VerifyVoucher(voucher, sig) {
				t.Errorf("worker %d: verify failed", id)
				return
			}
		}(i)
	}

	wg.Wait()

	stats := engine.GetStats()
	if stats.TotalKeyExchanges != uint64(workers) {
		t.Fatalf("expected %d key exchanges, got %d", workers, stats.TotalKeyExchanges)
	}
	if stats.TotalSignaturesVerified != uint64(workers) {
		t.Fatalf("expected %d verified signatures, got %d", workers, stats.TotalSignaturesVerified)
	}
}
