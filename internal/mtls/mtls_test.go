package mtls

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
	"github.com/kLauncher1/cf-lease-broker/internal/proxy"
)

func TestEphemeralCA_InitAndIssue(t *testing.T) {
	ca, err := NewEphemeralCA("test-root-ca", 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create EphemeralCA: %v", err)
	}

	rootPEM := ca.GetRootCertPEM()
	if !strings.Contains(string(rootPEM), "-----BEGIN CERTIFICATE-----") {
		t.Fatalf("expected valid PEM root certificate, got: %s", string(rootPEM))
	}

	sans := []string{"edge.broker.local", "127.0.0.1"}
	resp, err := ca.IssueClientCert("edge-worker-01", sans, 2*time.Hour)
	if err != nil {
		t.Fatalf("failed to issue client cert: %v", err)
	}

	if resp.SerialNumber == "" {
		t.Errorf("expected non-empty serial number")
	}
	if !strings.Contains(resp.CertificatePEM, "-----BEGIN CERTIFICATE-----") {
		t.Errorf("expected client cert PEM")
	}
	if !strings.Contains(resp.PrivateKeyPEM, "-----BEGIN EC PRIVATE KEY-----") {
		t.Errorf("expected client private key PEM")
	}
	if resp.Fingerprint == "" {
		t.Errorf("expected SHA-256 fingerprint")
	}
}

func TestEphemeralCA_CertRevocation(t *testing.T) {
	ca, err := NewEphemeralCA("revocation-ca", 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create CA: %v", err)
	}

	resp, err := ca.IssueClientCert("node-client", nil, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to issue client cert: %v", err)
	}

	if ca.IsRevoked(resp.SerialNumber) {
		t.Errorf("newly issued cert should not be revoked")
	}

	if err := ca.RevokeCertificate(resp.SerialNumber); err != nil {
		t.Fatalf("failed to revoke cert: %v", err)
	}

	if !ca.IsRevoked(resp.SerialNumber) {
		t.Errorf("expected serial %s to be marked as revoked", resp.SerialNumber)
	}
	if !ca.IsRevoked(strings.ToUpper(resp.SerialNumber)) {
		t.Errorf("revocation lookup should be case-insensitive")
	}
}

func TestEphemeralCA_PeeringTokenSignAndVerify(t *testing.T) {
	ca, err := NewEphemeralCA("peering-ca", 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create CA: %v", err)
	}

	token, err := ca.GeneratePeeringToken("broker-node-1", "cluster-us-east", "primary", 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate peering token: %v", err)
	}

	valid, reason := ca.VerifyPeeringToken(token)
	if !valid {
		t.Fatalf("expected peering token to be valid, got reason: %s", reason)
	}

	// Tampered token test
	tampered := *token
	tampered.Role = "unauthorized_role"
	valid, reason = ca.VerifyPeeringToken(&tampered)
	if valid {
		t.Errorf("tampered peering token must be rejected")
	}
	if reason != "signature mismatch" {
		t.Errorf("expected 'signature mismatch', got: %s", reason)
	}

	// Expired token test
	expired := *token
	expired.ExpiresAt = time.Now().Add(-10 * time.Minute).Unix()
	valid, reason = ca.VerifyPeeringToken(&expired)
	if valid || reason != "token expired" {
		t.Errorf("expected 'token expired', got: %s (valid=%t)", reason, valid)
	}
}

func TestCertVerifier_ValidAndRevokedCert(t *testing.T) {
	ca, _ := NewEphemeralCA("verifier-ca", 24*time.Hour)
	policy := VerificationPolicy{
		RequireClientCert: true,
		EnforceExpiry:     true,
	}
	verifier := NewCertVerifier(ca, policy)

	resp, err := ca.IssueClientCert("tenant-alpha", []string{"alpha.workers.dev"}, 2*time.Hour)
	if err != nil {
		t.Fatalf("failed to issue cert: %v", err)
	}

	res := verifier.VerifyPEM([]byte(resp.CertificatePEM))
	if !res.Valid {
		t.Fatalf("expected valid verification, got: %s", res.Reason)
	}
	if res.Certificate == nil || res.Certificate.SubjectCommonName != "tenant-alpha" {
		t.Errorf("unexpected cert details: %+v", res.Certificate)
	}

	// Revoke certificate
	_ = ca.RevokeCertificate(resp.SerialNumber)
	resRevoked := verifier.VerifyPEM([]byte(resp.CertificatePEM))
	if resRevoked.Valid {
		t.Errorf("revoked certificate must fail verification")
	}
	if !strings.Contains(resRevoked.Reason, "revoked") {
		t.Errorf("expected reason to mention revocation, got: %s", resRevoked.Reason)
	}
}

func TestCertVerifier_UntrustedRoot(t *testing.T) {
	ca1, _ := NewEphemeralCA("trusted-ca", 24*time.Hour)
	ca2, _ := NewEphemeralCA("alien-ca", 24*time.Hour)

	verifier := NewCertVerifier(ca1, VerificationPolicy{AllowSelfSigned: false})

	alienResp, _ := ca2.IssueClientCert("alien-client", nil, 1*time.Hour)
	res := verifier.VerifyPEM([]byte(alienResp.CertificatePEM))
	if res.Valid {
		t.Errorf("certificate signed by untrusted root must be rejected")
	}
	if !strings.Contains(res.Reason, "trust chain verification failed") {
		t.Errorf("expected trust chain failure, got: %s", res.Reason)
	}
}

func TestCertVerifier_SANAndCNPolicy(t *testing.T) {
	ca, _ := NewEphemeralCA("san-ca", 24*time.Hour)
	verifier := NewCertVerifier(ca, VerificationPolicy{
		AllowedSANs:        []string{"*.edge.cluster.local", "10.0.0.0/8"},
		AllowedCommonNames: []string{"edge-service-*"},
	})

	// Match DNS wildcard
	resp1, _ := ca.IssueClientCert("edge-service-auth", []string{"auth.edge.cluster.local"}, 1*time.Hour)
	res1 := verifier.VerifyPEM([]byte(resp1.CertificatePEM), VerificationPolicy{
		AllowedSANs:        []string{"*.edge.cluster.local"},
		AllowedCommonNames: []string{"edge-service-auth"},
	})
	if !res1.Valid {
		t.Errorf("expected matching wildcard SAN to pass, got: %s", res1.Reason)
	}

	// Mismatched SAN
	resp2, _ := ca.IssueClientCert("edge-service-auth", []string{"untrusted.domain.com"}, 1*time.Hour)
	res2 := verifier.VerifyPEM([]byte(resp2.CertificatePEM), VerificationPolicy{
		AllowedSANs: []string{"*.edge.cluster.local"},
	})
	if res2.Valid {
		t.Errorf("mismatched SAN should be rejected")
	}
	if !strings.Contains(res2.Reason, "do not match allowed patterns") {
		t.Errorf("expected SAN mismatch reason, got: %s", res2.Reason)
	}

	// Mismatched Common Name
	res3 := verifier.VerifyPEM([]byte(resp1.CertificatePEM), VerificationPolicy{
		AllowedCommonNames: []string{"forbidden-service"},
	})
	if res3.Valid {
		t.Errorf("mismatched CommonName should be rejected")
	}
}

func TestCertVerifier_ExtractFromHeader(t *testing.T) {
	ca, _ := NewEphemeralCA("header-ca", 24*time.Hour)
	verifier := NewCertVerifier(ca, VerificationPolicy{})

	resp, _ := ca.IssueClientCert("header-client", nil, 1*time.Hour)

	// Test 1: X-Client-Cert-Pem
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.Header.Set("X-Client-Cert-Pem", url.PathEscape(resp.CertificatePEM))
	cert1, err := verifier.ExtractCertFromRequest(req1)
	if err != nil {
		t.Fatalf("failed to extract cert from X-Client-Cert-Pem: %v", err)
	}
	if cert1.Subject.CommonName != "header-client" {
		t.Errorf("expected 'header-client', got '%s'", cert1.Subject.CommonName)
	}

	// Test 2: X-Forwarded-Client-Cert (Envoy format)
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	escapedPEM := url.QueryEscape(resp.CertificatePEM)
	req2.Header.Set("X-Forwarded-Client-Cert", `By=spiffe://cluster/ns/default/sa/edge;Cert="`+escapedPEM+`";Chain=""`)
	cert2, err := verifier.ExtractCertFromRequest(req2)
	if err != nil {
		t.Fatalf("failed to extract cert from X-Forwarded-Client-Cert: %v", err)
	}
	if cert2.Subject.CommonName != "header-client" {
		t.Errorf("expected 'header-client', got '%s'", cert2.Subject.CommonName)
	}

	// Test 3: X-Client-Cert-Der
	block, _ := pem.Decode([]byte(resp.CertificatePEM))
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.Header.Set("X-Client-Cert-Der", base64.StdEncoding.EncodeToString(block.Bytes))
	cert3, err := verifier.ExtractCertFromRequest(req3)
	if err != nil {
		t.Fatalf("failed to extract cert from X-Client-Cert-Der: %v", err)
	}
	if cert3.Subject.CommonName != "header-client" {
		t.Errorf("expected 'header-client', got '%s'", cert3.Subject.CommonName)
	}
}

func TestMTLSMiddleware_RequireClientCert(t *testing.T) {
	ca, _ := NewEphemeralCA("mw-ca", 24*time.Hour)
	verifier := NewCertVerifier(ca, VerificationPolicy{RequireClientCert: true})

	var extractedCN string
	targetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if details, ok := ClientCertFromContext(r.Context()); ok {
			extractedCN = details.SubjectCommonName
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("access_granted"))
	})

	protected := RequireClientCert(verifier)(targetHandler)

	// Case 1: Missing certificate returns 403 Forbidden
	reqNoCert := httptest.NewRequest(http.MethodGet, "/secure", nil)
	rrNoCert := httptest.NewRecorder()
	protected.ServeHTTP(rrNoCert, reqNoCert)
	if rrNoCert.Code != http.StatusForbidden {
		t.Errorf("expected HTTP 403 for missing cert, got: %d", rrNoCert.Code)
	}

	// Case 2: Valid certificate passes and injects context
	resp, _ := ca.IssueClientCert("service-worker-alpha", nil, 1*time.Hour)
	reqValid := httptest.NewRequest(http.MethodGet, "/secure", nil)
	reqValid.Header.Set("X-Client-Cert-Pem", resp.CertificatePEM)
	rrValid := httptest.NewRecorder()
	protected.ServeHTTP(rrValid, reqValid)

	if rrValid.Code != http.StatusOK {
		t.Errorf("expected HTTP 200 for valid cert, got: %d (%s)", rrValid.Code, rrValid.Body.String())
	}
	if extractedCN != "service-worker-alpha" {
		t.Errorf("expected context CN 'service-worker-alpha', got: '%s'", extractedCN)
	}
	if rrValid.Header().Get("X-Client-Cert-Verified") != "true" {
		t.Errorf("expected X-Client-Cert-Verified header")
	}
}

func TestMTLSMiddleware_PeeringAuth(t *testing.T) {
	ca, _ := NewEphemeralCA("peer-mw-ca", 24*time.Hour)
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("peer_rpc_ok"))
	})
	protected := PeeringAuth(ca)(target)

	// Missing token -> 401
	req1 := httptest.NewRequest(http.MethodPost, "/peer/sync", nil)
	rr1 := httptest.NewRecorder()
	protected.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401, got: %d", rr1.Code)
	}

	// Valid token -> 200
	token, _ := ca.GeneratePeeringToken("node-alpha", "cluster-1", "leader", 1*time.Hour)
	tokenBytes, _ := json.Marshal(token)

	req2 := httptest.NewRequest(http.MethodPost, "/peer/sync", nil)
	req2.Header.Set("X-Broker-Peering-Token", string(tokenBytes))
	rr2 := httptest.NewRecorder()
	protected.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got: %d (%s)", rr2.Code, rr2.Body.String())
	}
	if rr2.Header().Get("X-Broker-Peer-ID") != "node-alpha" {
		t.Errorf("expected peer ID header")
	}
}

func TestTPMAttestation_Query(t *testing.T) {
	status := CheckTPMAttestation()
	if status.Provider == "" {
		t.Errorf("expected non-empty TPM provider")
	}
	if status.PCRDigest == "" {
		t.Errorf("expected non-empty PCR digest")
	}
	if status.PlatformQuote == "" {
		t.Errorf("expected non-empty platform quote")
	}
}

func TestMTLSHandler_HTTPEndpoints(t *testing.T) {
	ca, _ := NewEphemeralCA("handler-test-ca", 24*time.Hour)
	verifier := NewCertVerifier(ca, VerificationPolicy{})
	handler := NewMTLSHandler(ca, verifier)

	// 1. GET /api/mtls/status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/mtls/status", nil)
	rrStatus := httptest.NewRecorder()
	handler.ServeHTTP(rrStatus, reqStatus)
	if rrStatus.Code != http.StatusOK {
		t.Errorf("GET /api/mtls/status failed: %d", rrStatus.Code)
	}

	// 2. GET /api/mtls/ca/cert
	reqCACert := httptest.NewRequest(http.MethodGet, "/api/mtls/ca/cert", nil)
	rrCACert := httptest.NewRecorder()
	handler.ServeHTTP(rrCACert, reqCACert)
	if rrCACert.Code != http.StatusOK || !strings.Contains(rrCACert.Body.String(), "BEGIN CERTIFICATE") {
		t.Errorf("GET /api/mtls/ca/cert failed: %s", rrCACert.Body.String())
	}

	// 3. POST /api/mtls/ca/issue
	issueBody, _ := json.Marshal(IssueCertRequest{
		CommonName:    "worker-node-edge",
		SANs:          []string{"edge.worker.local"},
		ValidityHours: 12,
	})
	reqIssue := httptest.NewRequest(http.MethodPost, "/api/mtls/ca/issue", bytes.NewReader(issueBody))
	rrIssue := httptest.NewRecorder()
	handler.ServeHTTP(rrIssue, reqIssue)
	if rrIssue.Code != http.StatusOK {
		t.Fatalf("POST /api/mtls/ca/issue failed: %d (%s)", rrIssue.Code, rrIssue.Body.String())
	}
	var issued IssueCertResponse
	_ = json.Unmarshal(rrIssue.Body.Bytes(), &issued)

	// 4. POST /api/mtls/verify
	verifyBody, _ := json.Marshal(VerifyCertRequest{
		CertificatePEM: issued.CertificatePEM,
	})
	reqVerify := httptest.NewRequest(http.MethodPost, "/api/mtls/verify", bytes.NewReader(verifyBody))
	rrVerify := httptest.NewRecorder()
	handler.ServeHTTP(rrVerify, reqVerify)
	if rrVerify.Code != http.StatusOK {
		t.Errorf("POST /api/mtls/verify failed: %d", rrVerify.Code)
	}
	var vRes VerificationResult
	_ = json.Unmarshal(rrVerify.Body.Bytes(), &vRes)
	if !vRes.Valid {
		t.Errorf("expected certificate to verify successfully: %s", vRes.Reason)
	}

	// 5. POST /api/mtls/revoke
	revokeBody, _ := json.Marshal(RevokeCertRequest{
		SerialNumber: issued.SerialNumber,
	})
	reqRevoke := httptest.NewRequest(http.MethodPost, "/api/mtls/revoke", bytes.NewReader(revokeBody))
	rrRevoke := httptest.NewRecorder()
	handler.ServeHTTP(rrRevoke, reqRevoke)
	if rrRevoke.Code != http.StatusOK {
		t.Errorf("POST /api/mtls/revoke failed: %d", rrRevoke.Code)
	}

	// 6. GET /api/mtls/tpm/attest
	reqTPM := httptest.NewRequest(http.MethodGet, "/api/mtls/tpm/attest", nil)
	rrTPM := httptest.NewRecorder()
	handler.ServeHTTP(rrTPM, reqTPM)
	if rrTPM.Code != http.StatusOK {
		t.Errorf("GET /api/mtls/tpm/attest failed: %d", rrTPM.Code)
	}

	// 7. POST /api/mtls/peer-token & verify
	pTokenBody := []byte(`{"broker_id":"node-1","cluster_id":"us-east","role":"replica","ttl_minutes":30}`)
	reqPT := httptest.NewRequest(http.MethodPost, "/api/mtls/peer-token", bytes.NewReader(pTokenBody))
	rrPT := httptest.NewRecorder()
	handler.ServeHTTP(rrPT, reqPT)
	if rrPT.Code != http.StatusOK {
		t.Fatalf("POST /api/mtls/peer-token failed: %d", rrPT.Code)
	}
	var pToken PeeringToken
	_ = json.Unmarshal(rrPT.Body.Bytes(), &pToken)

	reqPTVer := httptest.NewRequest(http.MethodPost, "/api/mtls/peer-token/verify", bytes.NewReader(rrPT.Body.Bytes()))
	rrPTVer := httptest.NewRecorder()
	handler.ServeHTTP(rrPTVer, reqPTVer)
	if rrPTVer.Code != http.StatusOK || !strings.Contains(rrPTVer.Body.String(), `"valid":true`) {
		t.Errorf("POST /api/mtls/peer-token/verify failed: %s", rrPTVer.Body.String())
	}
}

func TestMTLS_Concurrency(t *testing.T) {
	ca, _ := NewEphemeralCA("concurrent-ca", 24*time.Hour)
	verifier := NewCertVerifier(ca, VerificationPolicy{})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cn := "worker-" + hex.EncodeToString([]byte{byte(id)})
			resp, err := ca.IssueClientCert(cn, []string{"*.cluster.local"}, 2*time.Hour)
			if err != nil {
				t.Errorf("goroutine %d failed to issue: %v", id, err)
				return
			}
			res := verifier.VerifyPEM([]byte(resp.CertificatePEM))
			if !res.Valid {
				t.Errorf("goroutine %d cert failed verification: %s", id, res.Reason)
			}
			if id%3 == 0 {
				_ = ca.RevokeCertificate(resp.SerialNumber)
				if !ca.IsRevoked(resp.SerialNumber) {
					t.Errorf("goroutine %d failed revocation", id)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestProxy_MTLSIntegration(t *testing.T) {
	poolMgr := broker.NewPoolManager()
	leaseEng := broker.NewLeaseEngine(poolMgr)
	proxyHandler := proxy.NewProxyHandler(poolMgr, leaseEng)

	ca, _ := NewEphemeralCA("proxy-mtls-ca", 24*time.Hour)
	verifier := NewCertVerifier(ca, VerificationPolicy{RequireClientCert: true})

	proxyHandler.CertVerifier = func(r *http.Request) error {
		cert, err := verifier.ExtractCertFromRequest(r)
		if err != nil {
			return err
		}
		res := verifier.VerifyCertificate(cert)
		if !res.Valid {
			return errorsNew(res.Reason)
		}
		return nil
	}

	// 1. Request without cert -> 403
	req1 := httptest.NewRequest(http.MethodGet, "/proxy/service-app/data", nil)
	rr1 := httptest.NewRecorder()
	proxyHandler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden without cert, got: %d", rr1.Code)
	}

	// 2. Request with valid client cert -> passes cert check (will attempt backend routing)
	clientCert, _ := ca.IssueClientCert("edge-reverse-proxy-client", nil, 1*time.Hour)
	req2 := httptest.NewRequest(http.MethodGet, "/proxy/service-app/data", nil)
	req2.Header.Set("X-Client-Cert-Pem", clientCert.CertificatePEM)
	rr2 := httptest.NewRecorder()
	proxyHandler.ServeHTTP(rr2, req2)
	// It should NOT be 403 Forbidden because cert is valid!
	if rr2.Code == http.StatusForbidden {
		t.Errorf("expected cert to be accepted, got 403: %s", rr2.Body.String())
	}
}

// helper for error creation in test
type testError string

func (e testError) Error() string { return string(e) }
func errorsNew(msg string) error  { return testError(msg) }
