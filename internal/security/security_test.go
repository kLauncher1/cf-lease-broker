package security

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDPAPIRoundtrip(t *testing.T) {
	if !isDPAPIAvailable() {
		t.Skip("DPAPI not supported on this platform, skipping native DPAPI test")
	}

	secret := []byte("cf_token_super_secret_production_key_12345")
	encrypted, err := dpapiEncrypt(secret)
	if err != nil {
		t.Fatalf("dpapiEncrypt failed: %v", err)
	}

	if bytes.Equal(encrypted, secret) {
		t.Fatal("ciphertext must not match plaintext")
	}

	decrypted, err := dpapiDecrypt(encrypted)
	if err != nil {
		t.Fatalf("dpapiDecrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, secret) {
		t.Fatalf("decrypted data %q != original %q", decrypted, secret)
	}

	// Empty payload error handling
	if _, err := dpapiEncrypt(nil); err == nil {
		t.Fatal("expected error on encrypting empty plaintext")
	}
	if _, err := dpapiDecrypt(nil); err == nil {
		t.Fatal("expected error on decrypting empty ciphertext")
	}
}

func TestVaultAESGCMRoundtrip(t *testing.T) {
	vault := NewSecretVault()

	secret := []byte("cloudflare_api_token_alpha_zone_xyz")
	envelope, err := vault.Encrypt(secret, "aes-gcm")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if !strings.HasPrefix(envelope, "aes-gcm:") {
		t.Fatalf("expected aes-gcm prefix, got: %s", envelope)
	}

	decrypted, err := vault.Decrypt(envelope)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, secret) {
		t.Fatalf("decrypted %q != original %q", decrypted, secret)
	}
}

func TestVaultCiphertextTampering(t *testing.T) {
	vault := NewSecretVault()

	secret := []byte("integrity_check_secret")
	envelope, err := vault.Encrypt(secret, "aes-gcm")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Tamper with the last byte
	tampered := envelope[:len(envelope)-2] + "AA"
	_, err = vault.Decrypt(tampered)
	if err == nil {
		t.Fatal("expected decryption to fail on tampered ciphertext")
	}
}

func TestZeroize(t *testing.T) {
	buffer := []byte("sensitive_cloudflare_api_key")
	Zeroize(buffer)

	for i, b := range buffer {
		if b != 0 {
			t.Fatalf("expected zeroed byte at %d, got %v", i, b)
		}
	}
}

func TestJWTSignAndVerify(t *testing.T) {
	secret := []byte("super_jwt_signing_key_32_bytes_ok")
	jm := NewJWTManager(secret)

	now := time.Now().UTC()
	claims := JWTClaims{
		Sub:    "service-gateway",
		Role:   "operator",
		Scopes: []string{"leases:grant", "leases:read"},
		Iat:    now.Unix(),
		Exp:    now.Add(1 * time.Hour).Unix(),
		Jti:    "tok_test_123",
	}

	tokenStr, err := jm.SignToken(claims)
	if err != nil {
		t.Fatalf("SignToken failed: %v", err)
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	verified, err := jm.VerifyToken(tokenStr)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if verified.Sub != claims.Sub || verified.Role != claims.Role || verified.Jti != claims.Jti {
		t.Fatalf("claims mismatch: %+v != %+v", verified, claims)
	}
}

func TestJWTExpiration(t *testing.T) {
	secret := []byte("test_secret_for_exp")
	jm := NewJWTManager(secret)

	past := time.Now().UTC().Add(-2 * time.Hour)
	claims := JWTClaims{
		Sub:  "expired-client",
		Role: "service",
		Iat:  past.Add(-1 * time.Hour).Unix(),
		Exp:  past.Unix(),
		Jti:  "tok_expired",
	}

	tokenStr, err := jm.SignToken(claims)
	if err != nil {
		t.Fatalf("SignToken failed: %v", err)
	}

	_, err = jm.VerifyToken(tokenStr)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expiration error, got: %v", err)
	}
}

func TestJWTTamperedSignature(t *testing.T) {
	secret := []byte("test_secret_for_tampering")
	jm := NewJWTManager(secret)

	claims := JWTClaims{
		Sub:  "client-tamper",
		Role: "service",
		Exp:  time.Now().UTC().Add(1 * time.Hour).Unix(),
	}

	tokenStr, err := jm.SignToken(claims)
	if err != nil {
		t.Fatalf("SignToken failed: %v", err)
	}

	parts := strings.Split(tokenStr, ".")
	tampered := parts[0] + "." + parts[1] + ".invalidSig"
	_, err = jm.VerifyToken(tampered)
	if err == nil {
		t.Fatal("expected error on tampered signature")
	}
}

func TestJWTRevocationList(t *testing.T) {
	secret := []byte("test_secret_for_revocation")
	jm := NewJWTManager(secret)

	claims := JWTClaims{
		Sub:  "service-to-revoke",
		Role: "service",
		Jti:  "tok_revoke_999",
		Exp:  time.Now().UTC().Add(1 * time.Hour).Unix(),
	}

	tokenStr, err := jm.SignToken(claims)
	if err != nil {
		t.Fatalf("SignToken failed: %v", err)
	}

	// Verify passes initially
	if _, err := jm.VerifyToken(tokenStr); err != nil {
		t.Fatalf("expected token valid, got: %v", err)
	}

	// Revoke JTI
	if err := jm.RevokeToken(claims.Jti, time.Now().UTC().Add(1*time.Hour)); err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	if !jm.IsRevoked(claims.Jti) {
		t.Fatal("expected IsRevoked to return true")
	}

	// Verify must fail
	_, err = jm.VerifyToken(tokenStr)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked error, got: %v", err)
	}
}

func TestAuthMiddlewareStaticKey(t *testing.T) {
	vault := NewSecretVault()
	jwtMgr := NewJWTManager(nil)
	authMgr := NewAuthManager(jwtMgr, vault)

	reached := false
	handler := authMgr.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || claims.Role != "admin" {
			t.Fatal("expected admin claims for static admin key")
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Request with valid admin key in Authorization Bearer
	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("Authorization", "Bearer "+authMgr.adminAPIKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !reached {
		t.Fatalf("expected 200 OK, got: %d", rec.Code)
	}

	// Request with missing token
	reqNoAuth := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	recNoAuth := httptest.NewRecorder()
	reached = false
	handler.ServeHTTP(recNoAuth, reqNoAuth)
	if recNoAuth.Code != http.StatusUnauthorized || reached {
		t.Fatalf("expected 401 Unauthorized, got: %d", recNoAuth.Code)
	}
}

func TestAuthMiddlewareJWTBearer(t *testing.T) {
	vault := NewSecretVault()
	jwtMgr := NewJWTManager([]byte("jwt_secret_token_12345"))
	authMgr := NewAuthManager(jwtMgr, vault)

	token, err := jwtMgr.SignToken(JWTClaims{
		Sub:  "test-worker",
		Role: "service",
		Exp:  time.Now().UTC().Add(1 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("SignToken failed: %v", err)
	}

	var callerRole string
	handler := authMgr.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := ClaimsFromContext(r.Context())
		callerRole = claims.Role
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || callerRole != "service" {
		t.Fatalf("expected 200 OK with role service, got code %d, role %s", rec.Code, callerRole)
	}
}

func TestAuthMiddlewareRoleEnforcement(t *testing.T) {
	vault := NewSecretVault()
	jwtMgr := NewJWTManager(nil)
	authMgr := NewAuthManager(jwtMgr, vault)

	adminToken, _ := jwtMgr.SignToken(JWTClaims{Sub: "admin-user", Role: "admin", Exp: time.Now().UTC().Add(time.Hour).Unix()})
	operatorToken, _ := jwtMgr.SignToken(JWTClaims{Sub: "operator-user", Role: "operator", Exp: time.Now().UTC().Add(time.Hour).Unix()})
	serviceToken, _ := jwtMgr.SignToken(JWTClaims{Sub: "service-user", Role: "service", Exp: time.Now().UTC().Add(time.Hour).Unix()})

	adminOnlyHandler := authMgr.RequireRole("operator")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 1. Service user should be forbidden (403)
	reqSvc := httptest.NewRequest(http.MethodGet, "/api/admin-only", nil)
	reqSvc.Header.Set("Authorization", "Bearer "+serviceToken)
	recSvc := httptest.NewRecorder()
	adminOnlyHandler.ServeHTTP(recSvc, reqSvc)
	if recSvc.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for service role, got: %d", recSvc.Code)
	}

	// 2. Operator user should pass (200)
	reqOp := httptest.NewRequest(http.MethodGet, "/api/admin-only", nil)
	reqOp.Header.Set("Authorization", "Bearer "+operatorToken)
	recOp := httptest.NewRecorder()
	adminOnlyHandler.ServeHTTP(recOp, reqOp)
	if recOp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for operator role, got: %d", recOp.Code)
	}

	// 3. Admin user should always pass (universal access)
	reqAdm := httptest.NewRequest(http.MethodGet, "/api/admin-only", nil)
	reqAdm.Header.Set("Authorization", "Bearer "+adminToken)
	recAdm := httptest.NewRecorder()
	adminOnlyHandler.ServeHTTP(recAdm, reqAdm)
	if recAdm.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin role, got: %d", recAdm.Code)
	}
}

func TestAuthManagerHTTPHandlers(t *testing.T) {
	vault := NewSecretVault()
	jwtMgr := NewJWTManager(nil)
	authMgr := NewAuthManager(jwtMgr, vault)

	// 1. Test POST /api/auth/token
	body := `{"sub":"unit-service","role":"service","duration_hours":12}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", strings.NewReader(body))
	rec := httptest.NewRecorder()
	authMgr.HandleIssueToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HandleIssueToken expected 200, got: %d (%s)", rec.Code, rec.Body.String())
	}

	var tokenResp struct {
		Token  string    `json:"token"`
		Claims JWTClaims `json:"claims"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tokenResp); err != nil {
		t.Fatalf("failed to decode token response: %v", err)
	}
	if tokenResp.Token == "" || tokenResp.Claims.Sub != "unit-service" {
		t.Fatalf("unexpected token response: %+v", tokenResp)
	}

	// 2. Test GET /api/auth/status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	recStatus := httptest.NewRecorder()
	authMgr.HandleStatus(recStatus, reqStatus)
	if recStatus.Code != http.StatusOK {
		t.Fatalf("HandleStatus expected 200, got: %d", recStatus.Code)
	}

	// 3. Test POST /api/auth/revoke
	revokeBody := `{"jti":"` + tokenResp.Claims.Jti + `","duration_hours":2}`
	reqRevoke := httptest.NewRequest(http.MethodPost, "/api/auth/revoke", strings.NewReader(revokeBody))
	recRevoke := httptest.NewRecorder()
	authMgr.HandleRevokeToken(recRevoke, reqRevoke)
	if recRevoke.Code != http.StatusOK {
		t.Fatalf("HandleRevokeToken expected 200, got: %d", recRevoke.Code)
	}

	// 4. Verify token is now invalid
	if _, err := jwtMgr.VerifyToken(tokenResp.Token); err == nil {
		t.Fatal("expected token to be revoked")
	}
}

func TestVaultCredentialStoreAndRetrieve(t *testing.T) {
	vault := NewSecretVault()

	accountID := "cf-tenant-alpha"
	secretToken := []byte("cftok_alpha_secret_zone_token_value")

	if err := vault.StoreCredential(accountID, secretToken); err != nil {
		t.Fatalf("StoreCredential failed: %v", err)
	}

	creds := vault.ListCredentials()
	if len(creds) != 1 || creds[0].AccountID != accountID {
		t.Fatalf("unexpected credentials list: %+v", creds)
	}

	retrieved, err := vault.RetrieveDecryptedSecret(accountID)
	if err != nil {
		t.Fatalf("RetrieveDecryptedSecret failed: %v", err)
	}
	defer Zeroize(retrieved)

	if !bytes.Equal(retrieved, secretToken) {
		t.Fatalf("retrieved %q != original %q", retrieved, secretToken)
	}
}
