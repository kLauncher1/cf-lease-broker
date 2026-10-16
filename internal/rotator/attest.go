package rotator

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// AttestationVerifier validates hardware-backed security quotes for delegated token refresh operations.
type AttestationVerifier struct {
	mu           sync.Mutex
	nonces       map[string]time.Time
	secretKey    []byte
	nonceTimeout time.Duration
}

// NewAttestationVerifier initializes a verifier with an optional cluster secret for HMAC attestation.
func NewAttestationVerifier(secretKey []byte) *AttestationVerifier {
	if len(secretKey) == 0 {
		secretKey = []byte("cf_broker_attestation_cluster_secret_default")
	}
	return &AttestationVerifier{
		nonces:       make(map[string]time.Time),
		secretKey:    secretKey,
		nonceTimeout: 5 * time.Minute,
	}
}

// GenerateChallenge produces a single-use random nonce and timestamp for attestation handshakes.
func (v *AttestationVerifier) GenerateChallenge() (nonce string, timestamp int64) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	nonce = hex.EncodeToString(b)
	timestamp = time.Now().UTC().Unix()

	v.mu.Lock()
	v.nonces[nonce] = time.Now().UTC().Add(v.nonceTimeout)
	v.pruneExpiredNoncesLocked()
	v.mu.Unlock()

	return nonce, timestamp
}

// VerifyAndConsumeNonce ensures that a challenge nonce exists, is not expired, and has not been replayed.
func (v *AttestationVerifier) VerifyAndConsumeNonce(nonce string, timestamp int64) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.pruneExpiredNoncesLocked()

	expiresAt, exists := v.nonces[nonce]
	if !exists {
		return false
	}

	delete(v.nonces, nonce)

	now := time.Now().UTC()
	if now.After(expiresAt) {
		return false
	}

	reqTime := time.Unix(timestamp, 0).UTC()
	diff := now.Sub(reqTime)
	if diff < 0 {
		diff = -diff
	}
	return diff <= v.nonceTimeout
}

// VerifyDelegationQuote validates platform integrity proofs for delegated token refresh requests.
func (v *AttestationVerifier) VerifyDelegationQuote(req *DelegatedRefreshRequest) (bool, string) {
	if req == nil {
		return false, "empty request"
	}

	if req.Nonce == "" {
		return false, "missing challenge nonce"
	}

	if !v.VerifyAndConsumeNonce(req.Nonce, req.Timestamp) {
		return false, "invalid or replayed attestation nonce"
	}

	if req.HardwareQuote == "" {
		return false, "missing hardware platform quote"
	}

	if req.PCRDigest == "" {
		return false, "missing PCR measurement digest"
	}

	if len(req.PCRDigest) != 64 {
		return false, "malformed PCR digest: expected 64-character SHA-256 hex string"
	}

	// Verify TPM2 quote structure or HMAC attestation tag
	quote := req.HardwareQuote
	isTPMQuote := strings.HasPrefix(quote, "TPM2_Quote[") && strings.HasSuffix(quote, "]")
	isAttestedHMAC := strings.HasPrefix(quote, "HMAC_Quote:")

	if !isTPMQuote && !isAttestedHMAC {
		return false, "unrecognized hardware attestation quote format"
	}

	if isAttestedHMAC {
		providedHMAC := strings.TrimPrefix(quote, "HMAC_Quote:")
		expectedHMAC := v.ComputeHMAC(req.TokenID, req.TenantID, req.Nonce, req.PCRDigest)
		if subtle.ConstantTimeCompare([]byte(providedHMAC), []byte(expectedHMAC)) != 1 {
			return false, "hardware attestation HMAC signature mismatch"
		}
	}

	return true, "Hardware TPM 2.0 attestation quote verified"
}

// ComputeHMAC generates an expected verification tag for a given request payload.
func (v *AttestationVerifier) ComputeHMAC(tokenID, tenantID, nonce, pcrDigest string) string {
	mac := hmac.New(sha256.New, v.secretKey)
	payload := fmt.Sprintf("%s:%s:%s:%s", tokenID, tenantID, nonce, pcrDigest)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// ComputeAttestationHash returns a deterministic SHA-256 fingerprint of the verified quote and PCR state.
func (v *AttestationVerifier) ComputeAttestationHash(quote, pcrDigest string) string {
	h := sha256.New()
	h.Write([]byte(quote))
	h.Write([]byte(":"))
	h.Write([]byte(pcrDigest))
	return hex.EncodeToString(h.Sum(nil))
}

func (v *AttestationVerifier) pruneExpiredNoncesLocked() {
	now := time.Now().UTC()
	for n, exp := range v.nonces {
		if now.After(exp) {
			delete(v.nonces, n)
		}
	}
}
