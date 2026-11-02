package enclave

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// NonceTracker maintains an anti-replay cache of used attestation nonces.
type NonceTracker struct {
	mu     sync.RWMutex
	nonces map[string]time.Time
}

// NewNonceTracker constructs a thread-safe nonce registry.
func NewNonceTracker() *NonceTracker {
	return &NonceTracker{
		nonces: make(map[string]time.Time),
	}
}

// CheckAndRecord verifies the nonce has not been observed and records it with TTL.
func (nt *NonceTracker) CheckAndRecord(nonce string, ttl time.Duration) bool {
	if nonce == "" {
		return false
	}
	nt.mu.Lock()
	defer nt.mu.Unlock()

	now := time.Now()
	// Clean expired entries on write
	for n, exp := range nt.nonces {
		if now.After(exp) {
			delete(nt.nonces, n)
		}
	}

	if _, exists := nt.nonces[nonce]; exists {
		return false // Replay detected
	}

	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	nt.nonces[nonce] = now.Add(ttl)
	return true
}

// HasNonce checks whether a nonce is currently recorded.
func (nt *NonceTracker) HasNonce(nonce string) bool {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	exp, exists := nt.nonces[nonce]
	return exists && time.Now().Before(exp)
}

// AttestationEngine generates and verifies remote attestation quotes.
type AttestationEngine struct {
	mu              sync.RWMutex
	enclaveType     EnclaveType
	signerKey       []byte
	signerKeyID     string
	mrenclave       string
	mrsigner        string
	sevLaunchDigest string
	isvProdID       uint16
	isvSVN          uint16
	nonceTracker    *NonceTracker
	quoteCache      map[string]*AttestationQuote
}

// NewAttestationEngine creates a quote generator and verifier.
func NewAttestationEngine(cfg EnclaveConfig) *AttestationEngine {
	// Derive synthetic hardware measurements from signer key and config
	mrsignerHash := sha256.Sum256([]byte(cfg.SignerKey))
	mrenclaveHash := sha256.Sum256([]byte("enclave-code-cf-lease-broker-v0.24.0"))
	sevDigest := sha256.Sum256([]byte("amd-sev-guest-firmware-digest-2026"))

	signerKeyBytes := sha256.Sum256([]byte(cfg.HardwareRootKey + ":" + cfg.SignerKey))

	return &AttestationEngine{
		enclaveType:     cfg.EnclaveType,
		signerKey:       signerKeyBytes[:],
		signerKeyID:     cfg.SignerKey,
		mrenclave:       hex.EncodeToString(mrenclaveHash[:]),
		mrsigner:        hex.EncodeToString(mrsignerHash[:]),
		sevLaunchDigest: hex.EncodeToString(sevDigest[:]),
		isvProdID:       101,
		isvSVN:          2,
		nonceTracker:    NewNonceTracker(),
		quoteCache:      make(map[string]*AttestationQuote),
	}
}

// GenerateQuote crafts a tamper-evident hardware attestation quote.
func (ae *AttestationEngine) GenerateQuote(userData, nonce string) (*AttestationQuote, error) {
	if nonce == "" {
		rb := make([]byte, 16)
		if _, err := rand.Read(rb); err != nil {
			return nil, err
		}
		nonce = hex.EncodeToString(rb)
	}

	userDataHash := sha256.Sum256([]byte(userData))
	userDataHashHex := hex.EncodeToString(userDataHash[:])

	qidBytes := make([]byte, 16)
	if _, err := rand.Read(qidBytes); err != nil {
		return nil, err
	}
	quoteID := "quote-" + hex.EncodeToString(qidBytes)

	now := time.Now().UTC()

	meas := MeasurementRegister{
		MRENCLAVE:       ae.mrenclave,
		MRSIGNER:        ae.mrsigner,
		SEVLaunchDigest: ae.sevLaunchDigest,
		ISVProdID:       ae.isvProdID,
		ISVSVN:          ae.isvSVN,
	}

	// Sign payload: quoteID || enclaveType || nonce || userDataHash || mrenclave || mrsigner || isvSVN || timestamp
	sigPayload := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%d",
		quoteID, ae.enclaveType, nonce, userDataHashHex,
		meas.MRENCLAVE, meas.MRSIGNER, meas.ISVSVN, now.Unix(),
	)

	mac := hmac.New(sha256.New, ae.signerKey)
	mac.Write([]byte(sigPayload))
	sigHex := hex.EncodeToString(mac.Sum(nil))

	report := fmt.Sprintf("TCB_LEVEL=0x01;CPUSVN=0x0402;ATTRIBUTES=0x07;HW=%s", ae.enclaveType)

	quote := &AttestationQuote{
		QuoteID:        quoteID,
		EnclaveType:    ae.enclaveType,
		Timestamp:      now,
		Nonce:          nonce,
		UserData:       userData,
		UserDataHash:   userDataHashHex,
		Measurement:    meas,
		TCBVersion:     1,
		Signature:      sigHex,
		PublicKey:      ae.signerKeyID,
		HardwareReport: report,
	}

	ae.mu.Lock()
	ae.quoteCache[quoteID] = quote
	ae.mu.Unlock()

	return quote, nil
}

// VerifyQuote verifies an attestation quote against root keys, freshness, replay, and policy.
func (ae *AttestationEngine) VerifyQuote(quote *AttestationQuote, policy QuoteVerificationPolicy) error {
	if quote == nil {
		return ErrQuoteVerificationFailed
	}

	// 1. Replay check: ensure nonce is unique and not already consumed
	if !ae.nonceTracker.CheckAndRecord(quote.Nonce, time.Duration(policy.MaxQuoteAgeSec)*time.Second) {
		return ErrNonceReplayed
	}

	// 2. Freshness check
	age := time.Since(quote.Timestamp).Seconds()
	maxAge := float64(policy.MaxQuoteAgeSec)
	if maxAge <= 0 {
		maxAge = 300 // 5 minutes default
	}
	if age > maxAge || age < -30 { // Allow 30s clock skew
		return ErrQuoteExpired
	}

	// 3. Enclave Type verification
	if len(policy.AllowedEnclaveTypes) > 0 {
		allowed := false
		for _, t := range policy.AllowedEnclaveTypes {
			if t == quote.EnclaveType {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: enclave type %s not allowed by policy", ErrInvalidPolicy, quote.EnclaveType)
		}
	}
	if policy.RequireHardwareSGX && quote.EnclaveType != EnclaveTypeSGX {
		return fmt.Errorf("%w: Intel SGX hardware required", ErrInvalidPolicy)
	}

	// 4. Security Version Number (ISVSVN) check
	if quote.Measurement.ISVSVN < policy.MinISVSVN {
		return fmt.Errorf("%w: ISVSVN %d below required minimum %d", ErrInvalidPolicy, quote.Measurement.ISVSVN, policy.MinISVSVN)
	}

	// 5. Signer identity (MRSIGNER) check
	if len(policy.AllowedSigners) > 0 {
		signerAllowed := false
		for _, s := range policy.AllowedSigners {
			sHash := sha256.Sum256([]byte(s))
			if quote.Measurement.MRSIGNER == hex.EncodeToString(sHash[:]) || quote.PublicKey == s {
				signerAllowed = true
				break
			}
		}
		if !signerAllowed {
			return fmt.Errorf("%w: untrusted MRSIGNER %s", ErrInvalidPolicy, quote.Measurement.MRSIGNER)
		}
	}

	// 6. Cryptographic signature check
	sigPayload := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%d",
		quote.QuoteID, quote.EnclaveType, quote.Nonce, quote.UserDataHash,
		quote.Measurement.MRENCLAVE, quote.Measurement.MRSIGNER,
		quote.Measurement.ISVSVN, quote.Timestamp.Unix(),
	)

	expectedMac := hmac.New(sha256.New, ae.signerKey)
	expectedMac.Write([]byte(sigPayload))
	expectedSig := hex.EncodeToString(expectedMac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(quote.Signature), []byte(expectedSig)) != 1 {
		return ErrQuoteVerificationFailed
	}

	// 7. Verify user data integrity if user data provided
	if quote.UserData != "" {
		expectedDataHash := sha256.Sum256([]byte(quote.UserData))
		expectedHashHex := hex.EncodeToString(expectedDataHash[:])
		if subtle.ConstantTimeCompare([]byte(quote.UserDataHash), []byte(expectedHashHex)) != 1 {
			return ErrQuoteVerificationFailed
		}
	}

	return nil
}

// GetCachedQuote retrieves a previously generated quote by ID.
func (ae *AttestationEngine) GetCachedQuote(id string) (*AttestationQuote, bool) {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	q, ok := ae.quoteCache[id]
	return q, ok
}
