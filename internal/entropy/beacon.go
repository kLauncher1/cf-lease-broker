package entropy

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// GenesisBeaconHash is the deterministic predecessor hash for Round 1 of the beacon.
	GenesisBeaconHash = "0000000000000000000000000000000000000000000000000000000000000000"
	defaultMaxHistory = 128
)

// QuantumEntropyBeacon manages an immutable, cryptographically chained sequence of entropy rounds.
type QuantumEntropyBeacon struct {
	mu             sync.RWMutex
	currentRound   uint64
	previousHash   string
	attestationKey []byte
	maxClockSkew   time.Duration
	history        []BeaconRound
	maxHistory     int
}

// NewQuantumEntropyBeacon initializes a new quantum entropy beacon starting from genesis.
func NewQuantumEntropyBeacon(attestationKey []byte, maxClockSkew time.Duration) *QuantumEntropyBeacon {
	if len(attestationKey) == 0 {
		attestationKey = []byte("cf-beacon-attestation-master-key-default")
	}
	if maxClockSkew <= 0 {
		maxClockSkew = 30 * time.Second
	}

	return &QuantumEntropyBeacon{
		currentRound:   0,
		previousHash:   GenesisBeaconHash,
		attestationKey: append([]byte(nil), attestationKey...),
		maxClockSkew:   maxClockSkew,
		history:        make([]BeaconRound, 0, defaultMaxHistory),
		maxHistory:     defaultMaxHistory,
	}
}

// computeOutputHash creates a deterministic SHA-256 link binding round, predecessor hash, and entropy.
func computeOutputHash(round uint64, prevHash string, entropyBytes []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(fmt.Sprintf("%d|%s|%s", round, prevHash, hex.EncodeToString(entropyBytes))))
	return hex.EncodeToString(h.Sum(nil))
}

// signBeaconPayload computes an HMAC-SHA256 attestation over the canonical round representation.
func signBeaconPayload(key []byte, round uint64, prevHash, outHash string, ts time.Time, minBits float64) string {
	mac := hmac.New(sha256.New, key)
	payload := fmt.Sprintf("%d|%s|%s|%s|%.2f", round, prevHash, outHash, ts.UTC().Format(time.RFC3339Nano), minBits)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// NextRound increments the beacon to Round N, chaining it to Round N-1 with attestation signing.
func (b *QuantumEntropyBeacon) NextRound(entropyBytes []byte, source EntropySource, minBits float64) (*BeaconRound, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(entropyBytes) == 0 {
		return nil, errors.New("beacon: cannot generate round with empty entropy")
	}

	nextRoundNum := b.currentRound + 1
	now := time.Now().UTC()
	outHash := computeOutputHash(nextRoundNum, b.previousHash, entropyBytes)
	sig := signBeaconPayload(b.attestationKey, nextRoundNum, b.previousHash, outHash, now, minBits)

	round := BeaconRound{
		Round:          nextRoundNum,
		Timestamp:      now,
		PreviousHash:   b.previousHash,
		OutputHash:     outHash,
		Signature:      sig,
		EntropyMinBits: minBits,
		Source:         source,
	}

	// Advance chain
	b.currentRound = nextRoundNum
	b.previousHash = outHash

	// Append to bounded history
	b.history = append(b.history, round)
	if len(b.history) > b.maxHistory {
		b.history = b.history[len(b.history)-b.maxHistory:]
	}

	return &round, nil
}

// GetCurrentRound returns the most recent BeaconRound or nil if genesis.
func (b *QuantumEntropyBeacon) GetCurrentRound() *BeaconRound {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.history) == 0 {
		return nil
	}
	latest := b.history[len(b.history)-1]
	return &latest
}

// GetRound searches history for a specific round number.
func (b *QuantumEntropyBeacon) GetRound(round uint64) (*BeaconRound, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for i := len(b.history) - 1; i >= 0; i-- {
		if b.history[i].Round == round {
			r := b.history[i]
			return &r, nil
		}
	}
	return nil, fmt.Errorf("beacon: round %d not found in active history", round)
}

// GetHistory returns up to limit recent rounds in chronological order.
func (b *QuantumEntropyBeacon) GetHistory(limit int) []BeaconRound {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 || limit > len(b.history) {
		limit = len(b.history)
	}

	start := len(b.history) - limit
	out := make([]BeaconRound, limit)
	copy(out, b.history[start:])
	return out
}

// VerifyRound performs cryptographic peer validation on a round, enforcing hash chain and clock skew.
func (b *QuantumEntropyBeacon) VerifyRound(round *BeaconRound, expectedPrevHash string, verificationKey []byte) (bool, error) {
	if round == nil {
		return false, errors.New("beacon: round cannot be nil")
	}
	if round.Round == 0 {
		return false, errors.New("beacon: round number must be >= 1")
	}

	// 1. Clock skew validation
	now := time.Now().UTC()
	diff := now.Sub(round.Timestamp)
	if diff < 0 {
		diff = -diff
	}
	if diff > b.maxClockSkew {
		return false, fmt.Errorf("beacon: timestamp skew %v exceeds maximum allowable %v", diff, b.maxClockSkew)
	}

	// 2. Hash chain integrity check
	if expectedPrevHash != "" && round.PreviousHash != expectedPrevHash {
		return false, fmt.Errorf("beacon: previous hash mismatch; expected %s, got %s", expectedPrevHash, round.PreviousHash)
	}

	// 3. HMAC-SHA256 signature verification in constant time
	key := verificationKey
	if len(key) == 0 {
		key = b.attestationKey
	}

	expectedSig := signBeaconPayload(key, round.Round, round.PreviousHash, round.OutputHash, round.Timestamp, round.EntropyMinBits)
	if subtle.ConstantTimeCompare([]byte(round.Signature), []byte(expectedSig)) != 1 {
		return false, errors.New("beacon: cryptographic signature verification failed (tampered round or bad key)")
	}

	return true, nil
}
