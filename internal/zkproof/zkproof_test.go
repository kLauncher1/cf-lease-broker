package zkproof

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestProofIdentityDirect(t *testing.T) {
	wScalar, _ := RandomScalar()
	bScalar, _ := RandomScalar()
	comPoint := PointAdd(ScalarMult(G1, wScalar), ScalarMult(H1, bScalar))

	rw, _ := RandomScalar()
	rb, _ := RandomScalar()
	ptA := PointAdd(ScalarMult(G1, rw), ScalarMult(H1, rb))

	c, _ := RandomScalar()

	zw := modOrder(new(big.Int).Add(rw, modOrder(new(big.Int).Mul(c, wScalar))))
	zb := modOrder(new(big.Int).Add(rb, modOrder(new(big.Int).Mul(c, bScalar))))

	ptB := ScalarMult(G1, zw)
	ptC := ScalarMult(H1, zb)

	lhs := PointAdd(ptB, ptC)
	rhs := PointAdd(ptA, ScalarMult(comPoint, c))

	if lhs.X.Cmp(rhs.X) != 0 || lhs.Y.Cmp(rhs.Y) != 0 {
		t.Fatalf("Identity failed!\nLHS=(%s, %s)\nRHS=(%s, %s)",
			lhs.X.Text(16), lhs.Y.Text(16), rhs.X.Text(16), rhs.Y.Text(16))
	}
}

func TestCurveAndFieldArithmetic(t *testing.T) {
	if !G1.IsOnCurve() {
		t.Fatal("G1 generator must lie on curve")
	}
	if !H1.IsOnCurve() {
		t.Fatal("H1 generator must lie on curve")
	}

	// Point Doubling
	g2 := PointAdd(G1, G1)
	if !g2.IsOnCurve() {
		t.Fatal("2*G1 must lie on curve")
	}
	if g2.Infinity {
		t.Fatal("2*G1 must not be infinity")
	}

	// Scalar multiplication equality: [2]G1 == G1 + G1
	g2Scalar := ScalarMult(G1, big.NewInt(2))
	if g2.X.Cmp(g2Scalar.X) != 0 || g2.Y.Cmp(g2Scalar.Y) != 0 {
		t.Fatalf("ScalarMult [2]G1 mismatch: got (%s, %s), want (%s, %s)",
			g2Scalar.X.Text(16), g2Scalar.Y.Text(16), g2.X.Text(16), g2.Y.Text(16))
	}

	// Order modulus check: [OrderModulus]G1 == Infinity
	infP := ScalarMult(G1, OrderModulus)
	if !infP.Infinity {
		t.Fatalf("[r]G1 should be Infinity, got (%s, %s)", infP.X.Text(16), infP.Y.Text(16))
	}
	inf := InfinityPoint()
	addInf := PointAdd(G1, inf)
	if addInf.X.Cmp(G1.X) != 0 || addInf.Y.Cmp(G1.Y) != 0 {
		t.Fatal("G1 + Infinity must equal G1")
	}

	// Negation: P + (-P) == Infinity
	negY := new(big.Int).Sub(FieldModulus, G1.Y)
	negP := Point{X: G1.X, Y: negY, Infinity: false}
	shouldBeInf := PointAdd(G1, negP)
	if !shouldBeInf.Infinity {
		t.Fatal("P + (-P) must yield Infinity")
	}
}

func TestWorkerIsolationProofLifecycle(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())

	stmt := ProofStatement{
		StatementID:        "stmt_worker_01",
		Type:               StatementWorkerIsolation,
		TenantID:           "tenant_alpha",
		WorkerHash:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		GasLimit:           5000000,
		MemoryCeilingPages: 512,
	}

	// 1. Generate valid proof
	proof, err := engine.ProveWorkerIsolation(stmt, "dev_secret_witness_token_xyz", 3500000, 256)
	if err != nil {
		t.Fatalf("ProveWorkerIsolation failed: %v", err)
	}
	if proof.ProofID == "" || proof.A.X == "" || proof.Commitment == "" {
		t.Fatal("proof fields must not be empty")
	}

	// 2. Verify proof
	res, err := engine.Verify(proof)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected proof to be valid, reason: %s", res.Reason)
	}

	// 3. Witness violation: gas exceeds ceiling
	_, err = engine.ProveWorkerIsolation(stmt, "dev_secret_witness_token_xyz", 6000000, 256)
	if err == nil {
		t.Fatal("expected error when witness gas exceeds statement limit")
	}

	// 4. Witness violation: memory pages exceed ceiling
	_, err = engine.ProveWorkerIsolation(stmt, "dev_secret_witness_token_xyz", 3500000, 1024)
	if err == nil {
		t.Fatal("expected error when witness memory pages exceed statement limit")
	}
}

func TestQuotaAttestationAnonymousProof(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())

	stmt := ProofStatement{
		StatementID:  "stmt_quota_01",
		Type:         StatementQuotaAttestation,
		TenantID:     "tenant_beta",
		CurrentUsage: 750000,
		MaxQuota:     1000000,
	}

	// Generate proof showing usage <= quota without revealing secret Cloudflare API key
	apiKeySecret := "cf_api_key_secret_live_0987654321fedcba"
	proof, err := engine.ProveQuotaAttestation(stmt, apiKeySecret, 750000, nil)
	if err != nil {
		t.Fatalf("ProveQuotaAttestation failed: %v", err)
	}

	// Verify proof passes without needing the secret API key
	res, err := engine.Verify(proof)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected quota proof to be valid, reason: %s", res.Reason)
	}

	// Over-quota witness rejection at prover stage
	_, err = engine.ProveQuotaAttestation(stmt, apiKeySecret, 1200000, nil)
	if err == nil {
		t.Fatal("expected error when witness usage exceeds max quota")
	}
}

func TestProofTamperingDetection(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())

	stmt := ProofStatement{
		StatementID:        "stmt_tamper_test",
		Type:               StatementWorkerIsolation,
		TenantID:           "tenant_gamma",
		WorkerHash:         "abc123hash",
		GasLimit:           1000000,
		MemoryCeilingPages: 128,
	}

	origProof, err := engine.ProveWorkerIsolation(stmt, "witness_secret", 500000, 64)
	if err != nil {
		t.Fatalf("ProveWorkerIsolation failed: %v", err)
	}

	// Case 1: Tampered point A
	tamperedA := *origProof
	tamperedA.ProofID = "zkp_tampered_a"
	tamperedA.Nullifier = "nullifier_tampered_a"
	tamperedA.A = PointToProofPoint(PointAdd(G1, G1)) // Replace A with 2*G1
	res, _ := engine.Verify(&tamperedA)
	if res.Valid {
		t.Fatal("verification must fail when proof point A is tampered")
	}

	// Case 2: Tampered public inputs
	tamperedInputs := *origProof
	tamperedInputs.ProofID = "zkp_tampered_inputs"
	tamperedInputs.Nullifier = "nullifier_tampered_inputs"
	tamperedInputs.PublicInputs = []string{"0x1234", "0x5678", "0x9abc", "0xdef0"}
	res, _ = engine.Verify(&tamperedInputs)
	if res.Valid {
		t.Fatal("verification must fail when public inputs are tampered")
	}

	// Case 3: Tampered commitment
	tamperedCom := *origProof
	tamperedCom.ProofID = "zkp_tampered_com"
	tamperedCom.Nullifier = "nullifier_tampered_com"
	tamperedCom.Commitment = "0x01,0x02"
	res, _ = engine.Verify(&tamperedCom)
	if res.Valid {
		t.Fatal("verification must fail when commitment point is tampered")
	}
}

func TestAntiReplayNullifierTracking(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())

	stmt := ProofStatement{
		StatementID:        "stmt_replay_test",
		Type:               StatementWorkerIsolation,
		TenantID:           "tenant_delta",
		WorkerHash:         "replay_hash",
		GasLimit:           2000000,
		MemoryCeilingPages: 256,
	}

	proof, err := engine.ProveWorkerIsolation(stmt, "replay_secret", 1000000, 128)
	if err != nil {
		t.Fatalf("ProveWorkerIsolation failed: %v", err)
	}

	// First verification succeeds
	res1, err := engine.Verify(proof)
	if err != nil || !res1.Valid {
		t.Fatalf("first verification should pass: %v", err)
	}

	// Replay verification with identical nullifier MUST fail
	res2, err := engine.Verify(proof)
	if err == nil || res2.Valid {
		t.Fatal("replayed proof with consumed nullifier must be rejected")
	}
	if err != ErrProofReplayDetected {
		t.Fatalf("expected ErrProofReplayDetected, got: %v", err)
	}

	// Nullifiers count check
	if engine.Verifier().NullifiersCount() != 1 {
		t.Fatalf("expected 1 tracked nullifier, got: %d", engine.Verifier().NullifiersCount())
	}
}

func TestVectorAcceleratorBatchVerification(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())

	// Generate 6 distinct proofs
	proofs := make([]*ZKProof, 6)
	for i := 0; i < 6; i++ {
		stmt := ProofStatement{
			StatementID:        "stmt_batch_" + string(rune('a'+i)),
			Type:               StatementWorkerIsolation,
			TenantID:           "tenant_batch",
			WorkerHash:         "hash_batch",
			GasLimit:           1000000,
			MemoryCeilingPages: 128,
		}
		p, err := engine.ProveWorkerIsolation(stmt, "batch_secret_"+string(rune('a'+i)), 500000, 64)
		if err != nil {
			t.Fatalf("failed to generate proof %d: %v", i, err)
		}
		proofs[i] = p
	}

	// Batch verify with vector accelerator
	results, err := engine.VerifyBatch(proofs)
	if err != nil {
		t.Fatalf("VerifyBatch failed: %v", err)
	}
	if len(results) != len(proofs) {
		t.Fatalf("expected %d results, got: %d", len(proofs), len(results))
	}

	for i, r := range results {
		if !r.Valid {
			t.Fatalf("proof %d in batch failed: reason %s", i, r.Reason)
		}
		if !r.VectorAccelerated {
			t.Fatalf("proof %d should be marked vector_accelerated", i)
		}
	}

	stats := engine.Accelerator().GetStats()
	if stats.TotalBatchRuns == 0 || stats.TotalProofsBatched != 6 {
		t.Fatalf("unexpected vector stats: %+v", stats)
	}
}

func TestBoundaryIntegrityProof(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())

	stmt := ProofStatement{
		StatementID: "stmt_boundary_01",
		Type:        StatementBoundaryIntegrity,
		TenantID:    "tenant_omega",
	}

	proof, err := engine.ProveBoundaryIntegrity(stmt, "inter_broker_shared_secret_key")
	if err != nil {
		t.Fatalf("ProveBoundaryIntegrity failed: %v", err)
	}

	res, err := engine.Verify(proof)
	if err != nil || !res.Valid {
		t.Fatalf("boundary integrity proof verification failed: %v, reason: %s", err, res.Reason)
	}
}

func TestRESTEndpointsAndHTTPHandler(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())
	handler := NewZKProofHandler(engine)

	// 1. GET /api/zkproof/status
	req := httptest.NewRequest(http.MethodGet, "/api/zkproof/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /status failed: status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "zero_knowledge_edge_proof_verifier") {
		t.Fatal("GET /status missing subsystem name")
	}

	// 2. GET /api/zkproof/keys
	req = httptest.NewRequest(http.MethodGet, "/api/zkproof/keys", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /keys failed: status %d", w.Code)
	}

	// 3. POST /api/zkproof/prove/isolation
	provePayload := map[string]interface{}{
		"statement": map[string]interface{}{
			"statement_id":         "http_stmt_01",
			"type":                 "worker_isolation",
			"tenant_id":            "tenant_http",
			"worker_hash":          "worker_http_hash",
			"gas_limit":            2000000,
			"memory_ceiling_pages": 256,
		},
		"witness_secret": "http_witness_token",
		"gas_witness":    1000000,
		"mem_witness":    128,
	}
	body, _ := json.Marshal(provePayload)
	req = httptest.NewRequest(http.MethodPost, "/api/zkproof/prove/isolation", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /prove/isolation failed: status %d, body: %s", w.Code, w.Body.String())
	}

	var generatedProof ZKProof
	if err := json.NewDecoder(w.Body).Decode(&generatedProof); err != nil {
		t.Fatalf("failed to decode proof: %v", err)
	}

	// 4. POST /api/zkproof/verify
	proofBody, _ := json.Marshal(generatedProof)
	req = httptest.NewRequest(http.MethodPost, "/api/zkproof/verify", bytes.NewReader(proofBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /verify failed: status %d, body: %s", w.Code, w.Body.String())
	}

	// 5. POST /api/zkproof/reset
	req = httptest.NewRequest(http.MethodPost, "/api/zkproof/reset", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /reset failed: status %d", w.Code)
	}
}

func TestBodySizeBoundingDoSDefense(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())
	handler := NewZKProofHandler(engine)

	// Oversized payload > 64KB (e.g. 100KB)
	hugePayload := strings.Repeat("A", 100*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/zkproof/verify", strings.NewReader(hugePayload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Must be rejected with 400 Bad Request
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for >64KB body, got: %d", w.Code)
	}
}

func TestHighConcurrency40Goroutines(t *testing.T) {
	engine := NewZKProofEngine(DefaultVerifierConfig())

	var wg sync.WaitGroup
	workers := 40
	errChan := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			stmt := ProofStatement{
				StatementID:        fmt.Sprintf("concurrent_stmt_%d", idx),
				Type:               StatementWorkerIsolation,
				TenantID:           "tenant_concurrent",
				WorkerHash:         "hash_concurrent",
				GasLimit:           5000000,
				MemoryCeilingPages: 512,
			}
			p, err := engine.ProveWorkerIsolation(stmt, "concurrent_secret", 2000000, 256)
			if err != nil {
				errChan <- err
				return
			}
			res, err := engine.Verify(p)
			if err != nil || !res.Valid {
				errChan <- err
				return
			}
		}(i)
	}
	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			t.Fatalf("concurrency test encountered error: %v", err)
		}
	}

	stats := engine.GetStats()
	if stats.TotalProofsVerified != uint64(workers) {
		t.Fatalf("expected %d verified proofs, got: %d", workers, stats.TotalProofsVerified)
	}
}
