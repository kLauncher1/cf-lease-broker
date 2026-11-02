package scrubber

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IPReputationTracker maintains stateful IP behavioral metrics, reputation scoring, and challenge verification.
type IPReputationTracker struct {
	mu            sync.RWMutex
	records       map[string]*IPReputationRecord
	bannedIPs     map[string]banEntry
	usedNonces    map[string]time.Time
	hmacSecret    []byte
	challengeTTL  time.Duration
	defaultBanTTL time.Duration
	lastPrune     time.Time
}

type banEntry struct {
	until  time.Time
	reason string
}

// NewIPReputationTracker constructs a new thread-safe reputation tracker.
func NewIPReputationTracker(secret []byte, challengeTTL, defaultBanTTL time.Duration) *IPReputationTracker {
	if len(secret) == 0 {
		secret = []byte("default-cf-broker-reputation-secret-key-32b")
	}
	if challengeTTL <= 0 {
		challengeTTL = 5 * time.Minute
	}
	if defaultBanTTL <= 0 {
		defaultBanTTL = 15 * time.Minute
	}

	return &IPReputationTracker{
		records:       make(map[string]*IPReputationRecord),
		bannedIPs:     make(map[string]banEntry),
		usedNonces:    make(map[string]time.Time),
		hmacSecret:    secret,
		challengeTTL:  challengeTTL,
		defaultBanTTL: defaultBanTTL,
		lastPrune:     time.Now(),
	}
}

// GetOrCreateRecord retrieves or initializes an IP reputation record.
func (t *IPReputationTracker) GetOrCreateRecord(ip string) *IPReputationRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.getOrCreateLocked(ip)
}

func (t *IPReputationTracker) getOrCreateLocked(ip string) *IPReputationRecord {
	if rec, exists := t.records[ip]; exists {
		return rec
	}
	now := time.Now()
	rec := &IPReputationRecord{
		IP:        ip,
		Score:     100.0,
		Level:     ReputationClean,
		FirstSeen: now,
		LastSeen:  now,
	}
	t.records[ip] = rec
	return rec
}

// RecordEvent applies score adjustments based on observed traffic behavior.
func (t *IPReputationTracker) RecordEvent(ip string, attack AttackType, penalty float64, signature string, policy TenantScrubberPolicy) *IPReputationRecord {
	t.mu.Lock()
	defer t.mu.Unlock()

	rec := t.getOrCreateLocked(ip)
	now := time.Now()
	rec.LastSeen = now
	rec.TotalRequests++

	if penalty > 0 {
		rec.Score -= penalty
		rec.TotalDrops++
		if rec.Score < 0.0 {
			rec.Score = 0.0
		}
		if signature != "" {
			rec.AnomalousSignatures = appendUniqueString(rec.AnomalousSignatures, signature, 10)
		}
	} else if penalty < 0 {
		// Negative penalty indicates reward/recovery
		rec.Score += (-penalty)
		if rec.Score > 100.0 {
			rec.Score = 100.0
		}
	}

	// Update reputation level
	rec.Level = computeLevel(rec.Score)

	// Auto-ban if score falls below policy DropThreshold
	if policy.StrictBanning && rec.Score <= policy.DropThreshold {
		until := now.Add(t.defaultBanTTL)
		t.bannedIPs[ip] = banEntry{
			until:  until,
			reason: fmt.Sprintf("automatic quarantine: reputation score fell to %.1f (attack: %s)", rec.Score, attack),
		}
		rec.BannedUntil = until
		rec.BanReason = t.bannedIPs[ip].reason
		rec.Level = ReputationBanned
	}

	return rec
}

// RecordGoodBehavior increases reputation score for successful clean requests.
func (t *IPReputationTracker) RecordGoodBehavior(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rec := t.getOrCreateLocked(ip)
	rec.LastSeen = time.Now()
	rec.TotalRequests++
	rec.Score += 1.0
	if rec.Score > 100.0 {
		rec.Score = 100.0
	}
	rec.Level = computeLevel(rec.Score)
}

// IsBanned checks if an IP is currently quarantined in the ban list.
func (t *IPReputationTracker) IsBanned(ip string) (bool, string, time.Time) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entry, exists := t.bannedIPs[ip]
	if !exists {
		return false, "", time.Time{}
	}

	if time.Now().After(entry.until) {
		// Expired ban will be lazily or sweep cleared
		return false, "", time.Time{}
	}

	return true, entry.reason, entry.until
}

// BanIP manually or programmatically adds an IP to the quarantine ban list.
func (t *IPReputationTracker) BanIP(ip string, duration time.Duration, reason string) {
	if duration <= 0 {
		duration = t.defaultBanTTL
	}
	until := time.Now().Add(duration)

	t.mu.Lock()
	defer t.mu.Unlock()

	t.bannedIPs[ip] = banEntry{until: until, reason: reason}
	rec := t.getOrCreateLocked(ip)
	rec.BannedUntil = until
	rec.BanReason = reason
	rec.Score = 0.0
	rec.Level = ReputationBanned
}

// UnbanIP releases an IP address from active quarantine.
func (t *IPReputationTracker) UnbanIP(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, existed := t.bannedIPs[ip]
	delete(t.bannedIPs, ip)

	if rec, exists := t.records[ip]; exists {
		rec.BannedUntil = time.Time{}
		rec.BanReason = ""
		rec.Score = 75.0 // Reset to baseline suspicious-cleared score
		rec.Level = ReputationSuspicious
	}
	return existed
}

// ListBans returns all active non-expired banned IPs.
func (t *IPReputationTracker) ListBans() map[string]banEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	out := make(map[string]banEntry)
	for ip, b := range t.bannedIPs {
		if now.Before(b.until) {
			out[ip] = b
		}
	}
	return out
}

// IssueChallenge creates an HMAC-SHA256 authenticated challenge token for anomalous clients.
func (t *IPReputationTracker) IssueChallenge(clientIP, tenantID string) (*ChallengeToken, error) {
	now := time.Now()
	expiresAt := now.Add(t.challengeTTL)

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	sig := t.computeSignature(clientIP, tenantID, nonce, expiresAt.UnixMilli())
	tokenID := fmt.Sprintf("%s:%s:%d:%s", clientIP, tenantID, expiresAt.UnixMilli(), nonce)

	t.mu.Lock()
	rec := t.getOrCreateLocked(clientIP)
	rec.ChallengesIssued++
	t.mu.Unlock()

	return &ChallengeToken{
		TokenID:   tokenID,
		ClientIP:  clientIP,
		TenantID:  tenantID,
		IssuedAt:  now,
		ExpiresAt: expiresAt,
		Nonce:     nonce,
		Signature: sig,
		Verified:  false,
	}, nil
}

// VerifyChallenge validates client challenge submission and rewards reputation if valid.
func (t *IPReputationTracker) VerifyChallenge(tokenStr, providedSig string) (bool, error) {
	parts := strings.Split(tokenStr, ":")
	if len(parts) != 4 {
		return false, ErrChallengeInvalid
	}

	clientIP := parts[0]
	tenantID := parts[1]
	expiresMilli, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return false, ErrChallengeInvalid
	}
	nonce := parts[3]

	now := time.Now()
	if now.UnixMilli() > expiresMilli {
		return false, ErrChallengeExpired
	}

	expectedSig := t.computeSignature(clientIP, tenantID, nonce, expiresMilli)
	if subtle.ConstantTimeCompare([]byte(expectedSig), []byte(providedSig)) != 1 {
		return false, ErrChallengeInvalid
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Anti-replay check
	if _, alreadyUsed := t.usedNonces[nonce]; alreadyUsed {
		return false, ErrChallengeReplayed
	}
	t.usedNonces[nonce] = time.UnixMilli(expiresMilli)

	// Reward client IP for solving challenge
	rec := t.getOrCreateLocked(clientIP)
	rec.ChallengesSolved++
	rec.Score += 30.0
	if rec.Score > 100.0 {
		rec.Score = 100.0
	}
	rec.Level = computeLevel(rec.Score)

	// If IP was auto-banned due to score, unban it if recovered above threshold
	if rec.Score > 50.0 {
		delete(t.bannedIPs, clientIP)
		rec.BannedUntil = time.Time{}
		rec.BanReason = ""
	}

	return true, nil
}

// computeSignature derives HMAC-SHA256 signature for challenge token verification.
func (t *IPReputationTracker) computeSignature(ip, tenant, nonce string, expiresMilli int64) string {
	mac := hmac.New(sha256.New, t.hmacSecret)
	payload := fmt.Sprintf("%s|%s|%s|%d", ip, tenant, nonce, expiresMilli)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// SweepExpired purges outdated nonces, expired bans, and decays stale records.
func (t *IPReputationTracker) SweepExpired() {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	// 1. Purge expired bans
	for ip, b := range t.bannedIPs {
		if now.After(b.until) {
			delete(t.bannedIPs, ip)
		}
	}

	// 2. Purge expired nonces
	for nonce, exp := range t.usedNonces {
		if now.After(exp) {
			delete(t.usedNonces, nonce)
		}
	}

	// 3. Purge or decay stale IP records older than 24 hours
	for ip, rec := range t.records {
		if now.Sub(rec.LastSeen) > 24*time.Hour && now.After(rec.BannedUntil) {
			delete(t.records, ip)
		}
	}
	t.lastPrune = now
}

// Reset clears all in-memory states.
func (t *IPReputationTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = make(map[string]*IPReputationRecord)
	t.bannedIPs = make(map[string]banEntry)
	t.usedNonces = make(map[string]time.Time)
}

func computeLevel(score float64) ReputationLevel {
	if score >= 80.0 {
		return ReputationClean
	}
	if score > 50.0 {
		return ReputationSuspicious
	}
	if score >= 20.0 {
		return ReputationAnomalous
	}
	return ReputationBanned
}

func appendUniqueString(slice []string, val string, maxItems int) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	if len(slice) >= maxItems {
		slice = slice[1:]
	}
	return append(slice, val)
}
