package enclave

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestEnclaveMemory_AllocationAndIsolation(t *testing.T) {
	pool := NewEnclaveMemoryPool(1, 4) // 1MB total, 4KB pages = 256 pages

	t1Pages, err := pool.Allocate("tenant-alpha", 2)
	if err != nil {
		t.Fatalf("Failed to allocate pages for tenant-alpha: %v", err)
	}
	if len(t1Pages) != 2 {
		t.Fatalf("Expected 2 pages, got %d", len(t1Pages))
	}

	t2Pages, err := pool.Allocate("tenant-beta", 2)
	if err != nil {
		t.Fatalf("Failed to allocate pages for tenant-beta: %v", err)
	}

	for _, p1 := range t1Pages {
		for _, p2 := range t2Pages {
			if p1 == p2 {
				t.Fatalf("Isolation violation: tenant-alpha and beta share page %d", p1)
			}
		}
	}

	totalBytes, allocBytes, pageCount, allocPages := pool.Stats()
	if pageCount != 256 || allocPages != 4 {
		t.Fatalf("Unexpected stats: pageCount=%d allocPages=%d", pageCount, allocPages)
	}
	if totalBytes != 1024*1024 || allocBytes != 4*4096 {
		t.Fatalf("Unexpected byte stats: total=%d alloc=%d", totalBytes, allocBytes)
	}
}

func TestEnclaveMemory_BoundsChecking(t *testing.T) {
	pool := NewEnclaveMemoryPool(1, 4)
	pages, err := pool.Allocate("tenant-alpha", 1)
	if err != nil {
		t.Fatalf("Allocation failed: %v", err)
	}
	pid := pages[0]

	// Write out of bounds
	hugeData := make([]byte, 5000)
	if err := pool.Write("tenant-alpha", pid, 0, hugeData); err != ErrOutOfBounds {
		t.Fatalf("Expected ErrOutOfBounds for large write, got: %v", err)
	}

	// Write negative offset
	if err := pool.Write("tenant-alpha", pid, -1, []byte("test")); err != ErrOutOfBounds {
		t.Fatalf("Expected ErrOutOfBounds for negative offset, got: %v", err)
	}

	// Cross-tenant write rejection
	if err := pool.Write("tenant-beta", pid, 0, []byte("data")); err == nil {
		t.Fatal("Expected error for cross-tenant unauthorized write")
	}

	// Read out of bounds
	if _, err := pool.Read("tenant-alpha", pid, 4000, 200); err != ErrOutOfBounds {
		t.Fatalf("Expected ErrOutOfBounds for read beyond page size, got: %v", err)
	}
}

func TestEnclaveMemory_ZeroizationOnFree(t *testing.T) {
	pool := NewEnclaveMemoryPool(1, 4)
	pages, _ := pool.Allocate("tenant-alpha", 1)
	pid := pages[0]

	secretData := []byte("confidential-cf-lease-token-super-secret")
	if err := pool.Write("tenant-alpha", pid, 100, secretData); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	readBack, err := pool.Read("tenant-alpha", pid, 100, len(secretData))
	if err != nil || string(readBack) != string(secretData) {
		t.Fatalf("Read back failed or mismatch: %v", err)
	}

	// Free page and verify underlying raw memory is scrubbed
	if err := pool.Free("tenant-alpha", []int{pid}); err != nil {
		t.Fatalf("Free failed: %v", err)
	}

	offset := pid * pool.pageSize
	for i := offset; i < offset+pool.pageSize; i++ {
		if pool.rawMemory[i] != 0 {
			t.Fatalf("Zeroization failed: byte at offset %d is %d, expected 0", i, pool.rawMemory[i])
		}
	}
}

func TestEnclaveAttestation_QuoteGeneration(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewAttestationEngine(cfg)

	quote, err := engine.GenerateQuote("tenant=cf-alpha;scope=workers", "nonce-12345")
	if err != nil {
		t.Fatalf("GenerateQuote failed: %v", err)
	}

	if !strings.HasPrefix(quote.QuoteID, "quote-") {
		t.Fatalf("Unexpected QuoteID: %s", quote.QuoteID)
	}
	if quote.Nonce != "nonce-12345" {
		t.Fatalf("Nonce mismatch: %s", quote.Nonce)
	}
	if quote.Signature == "" {
		t.Fatal("Empty signature on quote")
	}
	if quote.Measurement.MRENCLAVE == "" || quote.Measurement.MRSIGNER == "" {
		t.Fatal("Empty measurement digests in quote")
	}
}

func TestEnclaveAttestation_QuoteVerificationSuccess(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewAttestationEngine(cfg)

	quote, err := engine.GenerateQuote("auth-data-token", "nonce-unique-abc")
	if err != nil {
		t.Fatalf("Quote gen failed: %v", err)
	}

	policy := QuoteVerificationPolicy{
		AllowedSigners:      []string{cfg.SignerKey},
		MinISVSVN:           1,
		AllowedEnclaveTypes: []EnclaveType{EnclaveTypeSGX, EnclaveTypeSEV, EnclaveTypeEmulated},
		MaxQuoteAgeSec:      300,
	}

	if err := engine.VerifyQuote(quote, policy); err != nil {
		t.Fatalf("Expected quote verification success, got: %v", err)
	}
}

func TestEnclaveAttestation_AntiReplayNonce(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewAttestationEngine(cfg)

	quote, _ := engine.GenerateQuote("payload", "nonce-replay-target")
	policy := cfg.DefaultPolicy

	// First verification succeeds
	if err := engine.VerifyQuote(quote, policy); err != nil {
		t.Fatalf("Initial verification failed: %v", err)
	}

	// Replay verification fails
	if err := engine.VerifyQuote(quote, policy); err != ErrNonceReplayed {
		t.Fatalf("Expected ErrNonceReplayed, got: %v", err)
	}
}

func TestEnclaveAttestation_QuoteTamperingDetection(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewAttestationEngine(cfg)
	policy := cfg.DefaultPolicy

	// Tamper with measurement
	quote1, _ := engine.GenerateQuote("original-data", "nonce-tamper-meas")
	tamperedQuote := *quote1
	tamperedQuote.Measurement.MRENCLAVE = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := engine.VerifyQuote(&tamperedQuote, policy); err != ErrQuoteVerificationFailed {
		t.Fatalf("Expected ErrQuoteVerificationFailed on tampered MRENCLAVE, got: %v", err)
	}

	// Tamper with signature
	quote2, _ := engine.GenerateQuote("original-data", "nonce-tamper-sig")
	tamperedSigQuote := *quote2
	tamperedSigQuote.Signature = "deadbeef" + tamperedSigQuote.Signature[8:]
	if err := engine.VerifyQuote(&tamperedSigQuote, policy); err != ErrQuoteVerificationFailed {
		t.Fatalf("Expected ErrQuoteVerificationFailed on tampered signature, got: %v", err)
	}
}

func TestEnclaveAttestation_PolicyEnforcement(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewAttestationEngine(cfg)

	// Strict SVN failure
	quote1, _ := engine.GenerateQuote("data", "nonce-policy-svn")
	strictSVNPolicy := cfg.DefaultPolicy
	strictSVNPolicy.MinISVSVN = 99
	if err := engine.VerifyQuote(quote1, strictSVNPolicy); err == nil || !strings.Contains(err.Error(), "ISVSVN") {
		t.Fatalf("Expected ISVSVN policy failure, got: %v", err)
	}

	// Signer whitelist rejection
	quote2, _ := engine.GenerateQuote("data", "nonce-policy-signer")
	untrustedSignerPolicy := cfg.DefaultPolicy
	untrustedSignerPolicy.AllowedSigners = []string{"untrusted-signer-xyz"}
	if err := engine.VerifyQuote(quote2, untrustedSignerPolicy); err == nil || !strings.Contains(err.Error(), "untrusted MRSIGNER") {
		t.Fatalf("Expected untrusted MRSIGNER error, got: %v", err)
	}
}

func TestEnclaveSealing_RoundtripAndTamper(t *testing.T) {
	sealer := NewSecretSealer("hardware-root-key-secret", "mrenclave-digest", "mrsigner-digest")

	originalSecret := []byte("cf-api-token-bearer-secret-key-1234567890")

	// Seal with MRSIGNER policy
	blobMRSigner, err := sealer.Seal(originalSecret, SealPolicyMRSigner, "token-key")
	if err != nil {
		t.Fatalf("Seal with MRSIGNER failed: %v", err)
	}

	unsealed, err := sealer.Unseal(blobMRSigner)
	if err != nil {
		t.Fatalf("Unseal failed: %v", err)
	}
	if string(unsealed) != string(originalSecret) {
		t.Fatalf("Unsealed secret mismatch: got %s, want %s", string(unsealed), string(originalSecret))
	}

	// Seal with MRENCLAVE policy
	blobMREnclave, err := sealer.Seal(originalSecret, SealPolicyMREnclave, "token-key-2")
	if err != nil {
		t.Fatalf("Seal with MRENCLAVE failed: %v", err)
	}

	unsealed2, err := sealer.Unseal(blobMREnclave)
	if err != nil {
		t.Fatalf("Unseal MRENCLAVE failed: %v", err)
	}
	if string(unsealed2) != string(originalSecret) {
		t.Fatal("Unsealed secret mismatch on MRENCLAVE policy")
	}

	// Tamper detection: alter ciphertext byte
	tamperedBlob := *blobMRSigner
	rawCipher, _ := hex.DecodeString(tamperedBlob.Ciphertext)
	rawCipher[0] ^= 0xFF
	tamperedBlob.Ciphertext = hex.EncodeToString(rawCipher)

	if _, err := sealer.Unseal(&tamperedBlob); err == nil {
		t.Fatal("Expected unseal error on tampered ciphertext")
	}
}

func TestEnclaveExecution_ConfidentialWorker(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewEnclaveEngine(cfg)
	engine.Start(context.Background())
	defer engine.Stop()

	// Execute sha256 task
	input := []byte("hello-enclave-confidential-compute")
	res, err := engine.ExecuteConfidentialTask("tenant-omega", "sha256", input)
	if err != nil {
		t.Fatalf("ExecuteConfidentialTask failed: %v", err)
	}
	if res.Output == "" || res.OutputHash == "" {
		t.Fatalf("Incomplete execution result: %+v", res)
	}

	// Verify page was cleanly freed after execution
	_, allocBytes, _, allocPages := engine.MemoryPool().Stats()
	if allocPages != 0 || allocBytes != 0 {
		t.Fatalf("Memory leak: expected 0 allocated pages, got %d", allocPages)
	}
}

func TestEnclaveHandler_RESTEndpoints(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewEnclaveEngine(cfg)
	handler := NewEnclaveHandler(engine)

	// 1. Status GET
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/enclave/status", nil)
	wStatus := httptest.NewRecorder()
	handler.ServeHTTP(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("GET /api/enclave/status failed: %d", wStatus.Code)
	}

	// 2. Quote POST
	quotePayload := `{"user_data":"test-token","nonce":"rest-nonce-1"}`
	reqQuote := httptest.NewRequest(http.MethodPost, "/api/enclave/quote", strings.NewReader(quotePayload))
	wQuote := httptest.NewRecorder()
	handler.ServeHTTP(wQuote, reqQuote)
	if wQuote.Code != http.StatusOK {
		t.Fatalf("POST /api/enclave/quote failed: %d", wQuote.Code)
	}

	var quote AttestationQuote
	if err := json.Unmarshal(wQuote.Body.Bytes(), &quote); err != nil {
		t.Fatalf("Failed to parse quote: %v", err)
	}

	// 3. Verify POST
	verifyReq := map[string]interface{}{"quote": quote}
	vBytes, _ := json.Marshal(verifyReq)
	reqVerify := httptest.NewRequest(http.MethodPost, "/api/enclave/verify", bytes.NewReader(vBytes))
	wVerify := httptest.NewRecorder()
	handler.ServeHTTP(wVerify, reqVerify)
	if wVerify.Code != http.StatusOK {
		t.Fatalf("POST /api/enclave/verify failed: %d", wVerify.Code)
	}

	// 4. Seal & Unseal POST
	sealReq := `{"tenant_id":"t1","plaintext":"cloud-secret-data","policy":"mrsigner","key_id":"k1"}`
	reqSeal := httptest.NewRequest(http.MethodPost, "/api/enclave/seal", strings.NewReader(sealReq))
	wSeal := httptest.NewRecorder()
	handler.ServeHTTP(wSeal, reqSeal)
	if wSeal.Code != http.StatusOK {
		t.Fatalf("POST /api/enclave/seal failed: %d", wSeal.Code)
	}

	var blob SealedBlob
	_ = json.Unmarshal(wSeal.Body.Bytes(), &blob)

	unsealReq := map[string]interface{}{"tenant_id": "t1", "blob": blob}
	uBytes, _ := json.Marshal(unsealReq)
	reqUnseal := httptest.NewRequest(http.MethodPost, "/api/enclave/unseal", bytes.NewReader(uBytes))
	wUnseal := httptest.NewRecorder()
	handler.ServeHTTP(wUnseal, reqUnseal)
	if wUnseal.Code != http.StatusOK {
		t.Fatalf("POST /api/enclave/unseal failed: %d", wUnseal.Code)
	}

	// 5. Execute POST
	execReq := `{"tenant_id":"t1","task_type":"sha256","input":"edge-data"}`
	reqExec := httptest.NewRequest(http.MethodPost, "/api/enclave/execute", strings.NewReader(execReq))
	wExec := httptest.NewRecorder()
	handler.ServeHTTP(wExec, reqExec)
	if wExec.Code != http.StatusOK {
		t.Fatalf("POST /api/enclave/execute failed: %d", wExec.Code)
	}

	// 6. Stats GET & Reset POST
	reqStats := httptest.NewRequest(http.MethodGet, "/api/enclave/stats", nil)
	wStats := httptest.NewRecorder()
	handler.ServeHTTP(wStats, reqStats)
	if wStats.Code != http.StatusOK {
		t.Fatalf("GET /api/enclave/stats failed: %d", wStats.Code)
	}

	reqReset := httptest.NewRequest(http.MethodPost, "/api/enclave/reset", nil)
	wReset := httptest.NewRecorder()
	handler.ServeHTTP(wReset, reqReset)
	if wReset.Code != http.StatusOK {
		t.Fatalf("POST /api/enclave/reset failed: %d", wReset.Code)
	}
}

func TestEnclaveHandler_BodySizeBoundingDoS(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewEnclaveEngine(cfg)
	handler := NewEnclaveHandler(engine)

	// Payload larger than 64KB
	hugePayload := strings.Repeat("A", 70*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/enclave/quote", strings.NewReader(hugePayload))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	// Should fail with 400 Bad Request due to truncation of malformed JSON
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request on oversized payload, got: %d", w.Code)
	}
}

func TestEnclave_HighConcurrency40Goroutines(t *testing.T) {
	cfg := DefaultEnclaveConfig()
	engine := NewEnclaveEngine(cfg)
	engine.Start(context.Background())
	defer engine.Stop()

	var wg sync.WaitGroup
	workers := 40
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			tenant := fmt.Sprintf("tenant-%d", workerID)

			// 1. Generate quote
			quote, err := engine.GenerateQuote("token-data", fmt.Sprintf("nonce-%d", workerID))
			if err != nil {
				t.Errorf("Worker %d: GenerateQuote failed: %v", workerID, err)
				return
			}

			// 2. Verify quote
			if err := engine.VerifyQuote(quote, nil); err != nil {
				t.Errorf("Worker %d: VerifyQuote failed: %v", workerID, err)
				return
			}

			// 3. Seal secret
			secret := []byte(fmt.Sprintf("secret-payload-tenant-%d", workerID))
			blob, err := engine.SealSecret(tenant, secret, SealPolicyMRSigner, "key-1")
			if err != nil {
				t.Errorf("Worker %d: SealSecret failed: %v", workerID, err)
				return
			}

			// 4. Unseal secret
			unsealed, err := engine.UnsealSecret(tenant, blob)
			if err != nil || string(unsealed) != string(secret) {
				t.Errorf("Worker %d: UnsealSecret failed: %v", workerID, err)
				return
			}

			// 5. Execute confidential task
			res, err := engine.ExecuteConfidentialTask(tenant, "sha256", secret)
			if err != nil || res.Output == "" {
				t.Errorf("Worker %d: ExecuteConfidentialTask failed: %v", workerID, err)
				return
			}
		}()
	}

	wg.Wait()
	stats := engine.GetStats()
	if stats.QuotesGenerated != int64(workers) || stats.SecretsSealed != int64(workers) {
		t.Fatalf("Mismatch in concurrent stats: %+v", stats)
	}
}
