package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// JWTClaims holds standard and application-specific claims.
type JWTClaims struct {
	Sub    string   `json:"sub"`
	Role   string   `json:"role"`
	Scopes []string `json:"scopes,omitempty"`
	Iat    int64    `json:"iat"`
	Exp    int64    `json:"exp"`
	Jti    string   `json:"jti"`
	Aud    string   `json:"aud,omitempty"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// JWTManager issues, verifies, and invalidates HMAC-SHA256 tokens.
type JWTManager struct {
	mu           sync.RWMutex
	secret       []byte
	revokedToken map[string]time.Time
}

// NewJWTManager creates an HMAC-SHA256 JWT manager.
func NewJWTManager(secret []byte) *JWTManager {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	return &JWTManager{
		secret:       secret,
		revokedToken: make(map[string]time.Time),
	}
}

// SignToken generates a base64url signed HS256 JWT string.
func (jm *JWTManager) SignToken(claims JWTClaims) (string, error) {
	if claims.Sub == "" {
		return "", errors.New("sub claim is required")
	}
	if claims.Jti == "" {
		claims.Jti = "tok_" + GenerateRandomHex(12)
	}
	if claims.Iat == 0 {
		claims.Iat = time.Now().UTC().Unix()
	}
	if claims.Exp == 0 {
		claims.Exp = time.Now().UTC().Add(24 * time.Hour).Unix()
	}
	if claims.Role == "" {
		claims.Role = "service"
	}

	header := jwtHeader{
		Alg: "HS256",
		Typ: "JWT",
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("failed to marshal header: %w", err)
	}

	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal claims: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsBytes)

	signingInput := headerB64 + "." + claimsB64

	jm.mu.RLock()
	sec := jm.secret
	jm.mu.RUnlock()

	mac := hmac.New(sha256.New, sec)
	mac.Write([]byte(signingInput))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sigB64, nil
}

// VerifyToken validates JWT format, HMAC signature, expiration, and revocation status.
func (jm *JWTManager) VerifyToken(tokenStr string) (*JWTClaims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed jwt: expected 3 dot-separated segments")
	}

	signingInput := parts[0] + "." + parts[1]
	providedSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("invalid signature base64 encoding")
	}

	jm.mu.RLock()
	sec := jm.secret
	jm.mu.RUnlock()

	mac := hmac.New(sha256.New, sec)
	mac.Write([]byte(signingInput))
	expectedSig := mac.Sum(nil)

	// Constant-time signature verification prevents timing attacks
	if !hmac.Equal(providedSig, expectedSig) {
		return nil, errors.New("invalid signature or token tampered")
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid claims base64 encoding")
	}

	var claims JWTClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claims: %w", err)
	}

	now := time.Now().UTC().Unix()
	if claims.Exp > 0 && now > claims.Exp {
		return nil, errors.New("token has expired")
	}

	if jm.IsRevoked(claims.Jti) {
		return nil, errors.New("token has been revoked")
	}

	return &claims, nil
}

// RevokeToken marks a token JTI as revoked until its expiration.
func (jm *JWTManager) RevokeToken(jti string, expiresAt time.Time) error {
	if jti == "" {
		return errors.New("jti is required for revocation")
	}

	jm.mu.Lock()
	defer jm.mu.Unlock()

	// Clean up stale entries while holding lock
	now := time.Now().UTC()
	for id, exp := range jm.revokedToken {
		if now.After(exp) {
			delete(jm.revokedToken, id)
		}
	}

	jm.revokedToken[jti] = expiresAt
	return nil
}

// IsRevoked checks whether a JTI is on the revocation list.
func (jm *JWTManager) IsRevoked(jti string) bool {
	if jti == "" {
		return false
	}

	jm.mu.RLock()
	defer jm.mu.RUnlock()

	exp, found := jm.revokedToken[jti]
	if !found {
		return false
	}
	if time.Now().UTC().After(exp) {
		return false
	}
	return true
}

// RevokedCount returns number of active revoked tokens.
func (jm *JWTManager) RevokedCount() int {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return len(jm.revokedToken)
}

// GenerateRandomHex produces a cryptographically secure hex string.
func GenerateRandomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
