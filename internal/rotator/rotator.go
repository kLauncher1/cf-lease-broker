package rotator

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// TokenRotator orchestrates background lifecycle renewals and token handovers.
type TokenRotator struct {
	mu             sync.RWMutex
	config         RotatorConfig
	mesh           *RevocationMesh
	leaseEngine    *broker.LeaseEngine
	verifier       *AttestationVerifier
	tokens         map[string]*RotatedToken // keyed by TokenID
	tokenKeyIndex  map[string]string        // tokenKey -> TokenID
	graceTokens    map[string]*RotatedToken // previousTokenKey -> RotatedToken
	events         []RenewalEvent
	running        bool
	stopChan       chan struct{}
	lastSweepAt    time.Time
	renewedCount   int
	revokedCount   int
}

// NewTokenRotator creates an autonomous token rotation engine.
func NewTokenRotator(
	cfg RotatorConfig,
	mesh *RevocationMesh,
	leaseEngine *broker.LeaseEngine,
	verifier *AttestationVerifier,
) *TokenRotator {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 2 * time.Second
	}
	if cfg.RenewalThresholdRatio <= 0 || cfg.RenewalThresholdRatio >= 1.0 {
		cfg.RenewalThresholdRatio = 0.80
	}
	if cfg.GracePeriod <= 0 {
		cfg.GracePeriod = 30 * time.Second
	}
	if verifier == nil {
		verifier = NewAttestationVerifier(nil)
	}

	tr := &TokenRotator{
		config:        cfg,
		mesh:          mesh,
		leaseEngine:   leaseEngine,
		verifier:      verifier,
		tokens:        make(map[string]*RotatedToken),
		tokenKeyIndex: make(map[string]string),
		graceTokens:   make(map[string]*RotatedToken),
		events:        make([]RenewalEvent, 0, 100),
		stopChan:      make(chan struct{}),
	}

	if mesh != nil {
		mesh.OnRevoke(func(tombstone RevocationTombstone) {
			tr.handleMeshRevocation(tombstone)
		})
	}

	return tr
}

// Start launches the automated background renewal ticker.
func (tr *TokenRotator) Start() {
	tr.mu.Lock()
	if tr.running {
		tr.mu.Unlock()
		return
	}
	tr.running = true
	tr.stopChan = make(chan struct{})
	tr.mu.Unlock()

	go tr.runLoop()
}

// Stop cleanly shuts down the background rotation worker.
func (tr *TokenRotator) Stop() {
	tr.mu.Lock()
	if !tr.running {
		tr.mu.Unlock()
		return
	}
	tr.running = false
	close(tr.stopChan)
	tr.mu.Unlock()
}

// RegisterVoucher tracks an existing lease voucher under the automated rotator.
func (tr *TokenRotator) RegisterVoucher(voucher broker.LeaseVoucher) *RotatedToken {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	totalTTL := voucher.ExpiresAt.Sub(voucher.IssuedAt)
	if totalTTL <= 0 {
		totalTTL = 24 * time.Hour
	}
	thresholdDuration := time.Duration(float64(totalTTL) * tr.config.RenewalThresholdRatio)
	renewAt := voucher.IssuedAt.Add(thresholdDuration)

	tokenID := "tok_" + generateRandomHex(8)
	rt := &RotatedToken{
		TokenID:       tokenID,
		LeaseID:       voucher.LeaseID,
		AccountID:     voucher.AccountID,
		TenantID:      voucher.AccountID,
		TargetService: voucher.TargetService,
		TokenKey:      voucher.TokenKey,
		IssuedAt:      voucher.IssuedAt,
		ExpiresAt:     voucher.ExpiresAt,
		RenewAt:       renewAt,
		State:         TokenStateActive,
		RenewalCount:  0,
	}

	tr.tokens[tokenID] = rt
	tr.tokenKeyIndex[rt.TokenKey] = tokenID
	return rt
}

// CheckAndRenew evaluates all tracked tokens, renewing those past their threshold.
func (tr *TokenRotator) CheckAndRenew() []RenewalEvent {
	tr.mu.Lock()
	now := time.Now().UTC()
	tr.lastSweepAt = now

	// Clean up elapsed grace tokens
	for key, tok := range tr.graceTokens {
		if now.After(tok.GraceUntil) {
			delete(tr.graceTokens, key)
		}
	}

	if !tr.config.AutoRenewEnabled {
		tr.mu.Unlock()
		return nil
	}

	toRenew := make([]*RotatedToken, 0)
	for _, tok := range tr.tokens {
		if tok.State == TokenStateActive && now.After(tok.RenewAt) {
			if tr.mesh != nil && (tr.mesh.IsRevoked(tok.TokenID) || tr.mesh.IsRevoked(tok.LeaseID)) {
				tok.State = TokenStateRevoked
				continue
			}
			toRenew = append(toRenew, tok)
		}
	}
	tr.mu.Unlock()

	events := make([]RenewalEvent, 0, len(toRenew))
	for _, tok := range toRenew {
		ev, err := tr.RenewToken(tok.TokenID, "automated_background_renewal")
		if err == nil && ev != nil {
			events = append(events, *ev)
		}
	}
	return events
}

// RenewToken rotates a token, generating a new token key and placing the old key in grace period.
func (tr *TokenRotator) RenewToken(tokenID, reason string) (*RenewalEvent, error) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	tok, exists := tr.tokens[tokenID]
	if !exists {
		return nil, errors.New("token not found")
	}

	if tok.State == TokenStateRevoked {
		return nil, errors.New("cannot renew revoked token")
	}

	now := time.Now().UTC()
	startNano := time.Now().UnixNano()

	oldKey := tok.TokenKey
	newKey := "cftok_rot_" + generateRandomHex(16)

	// Calculate new lifetime
	duration := tok.ExpiresAt.Sub(tok.IssuedAt)
	if duration <= 0 {
		duration = 24 * time.Hour
	}

	tok.PreviousTokenKey = oldKey
	tok.TokenKey = newKey
	tok.GraceUntil = now.Add(tr.config.GracePeriod)
	tok.IssuedAt = now
	tok.ExpiresAt = now.Add(duration)
	tok.RenewAt = now.Add(time.Duration(float64(duration) * tr.config.RenewalThresholdRatio))
	tok.RenewalCount++
	tok.LastRenewedAt = now
	tok.State = TokenStateActive

	// Update lookups
	delete(tr.tokenKeyIndex, oldKey)
	tr.tokenKeyIndex[newKey] = tok.TokenID
	tr.graceTokens[oldKey] = tok
	tr.renewedCount++

	// Synchronize with lease engine if associated
	if tr.leaseEngine != nil && tok.LeaseID != "" {
		if voucher, found := tr.leaseEngine.GetLease(tok.LeaseID); found {
			voucher.TokenKey = newKey
			voucher.ExpiresAt = tok.ExpiresAt
			tr.leaseEngine.ApplyVoucher(*voucher)
		}
	}

	durationMs := (time.Now().UnixNano() - startNano) / 1e6
	event := RenewalEvent{
		EventID:     "ev_" + generateRandomHex(6),
		TokenID:     tok.TokenID,
		LeaseID:     tok.LeaseID,
		OldTokenKey: oldKey,
		NewTokenKey: newKey,
		RenewedAt:   now,
		DurationMs:  durationMs,
		Reason:      reason,
		Attested:    tok.AttestationHash != "",
	}
	tr.events = append(tr.events, event)
	if len(tr.events) > 100 {
		tr.events = tr.events[len(tr.events)-50:]
	}

	return &event, nil
}

// RevokeToken marks a token as permanently revoked, cleans grace keys, and notifies the mesh.
func (tr *TokenRotator) RevokeToken(tokenID, reason string) error {
	tr.mu.Lock()
	tok, exists := tr.tokens[tokenID]
	if !exists {
		tr.mu.Unlock()
		return errors.New("token not found")
	}

	tok.State = TokenStateRevoked
	delete(tr.tokenKeyIndex, tok.TokenKey)
	if tok.PreviousTokenKey != "" {
		delete(tr.graceTokens, tok.PreviousTokenKey)
	}
	tr.revokedCount++
	leaseID := tok.LeaseID
	tr.mu.Unlock()

	if tr.mesh != nil {
		tr.mesh.Revoke(leaseID, tokenID, reason)
	}
	if tr.leaseEngine != nil && leaseID != "" {
		_ = tr.leaseEngine.RevokeLease(leaseID)
	}

	return nil
}

// ProcessDelegatedRefresh executes an attested renewal on behalf of an edge node.
func (tr *TokenRotator) ProcessDelegatedRefresh(req *DelegatedRefreshRequest) (*DelegatedRefreshResponse, error) {
	if req == nil {
		return &DelegatedRefreshResponse{Success: false, Error: "nil request"}, errors.New("nil request")
	}

	valid, reason := tr.verifier.VerifyDelegationQuote(req)
	if !valid {
		return &DelegatedRefreshResponse{
			Success:            false,
			TokenID:            req.TokenID,
			AttestationVerdict: reason,
			Error:              "attestation verification failed: " + reason,
		}, fmt.Errorf("attestation verification failed: %s", reason)
	}

	_, err := tr.RenewToken(req.TokenID, "delegated_hardware_attested_refresh")
	if err != nil {
		return &DelegatedRefreshResponse{
			Success:            false,
			TokenID:            req.TokenID,
			AttestationVerdict: reason,
			Error:              err.Error(),
		}, err
	}

	tr.mu.Lock()
	if tok, ok := tr.tokens[req.TokenID]; ok {
		tok.AttestationHash = tr.verifier.ComputeAttestationHash(req.HardwareQuote, req.PCRDigest)
	}
	tokCopy := *tr.tokens[req.TokenID]
	tr.mu.Unlock()

	return &DelegatedRefreshResponse{
		Success:            true,
		TokenID:            tokCopy.TokenID,
		NewTokenKey:        tokCopy.TokenKey,
		ExpiresAt:          tokCopy.ExpiresAt,
		AttestationVerdict: "Hardware Attestation Succeeded (" + reason + ")",
	}, nil
}

// ValidateTokenKey checks if a token key is active or within its valid handover grace period.
func (tr *TokenRotator) ValidateTokenKey(tokenKey string) (tokenID string, valid bool) {
	if tokenKey == "" {
		return "", false
	}

	tr.mu.RLock()
	defer tr.mu.RUnlock()

	now := time.Now().UTC()

	// 1. Check primary active token keys
	if id, exists := tr.tokenKeyIndex[tokenKey]; exists {
		if tok, ok := tr.tokens[id]; ok {
			if tok.State == TokenStateActive && now.Before(tok.ExpiresAt) {
				if tr.mesh == nil || (!tr.mesh.IsRevoked(tok.TokenID) && !tr.mesh.IsRevoked(tok.LeaseID)) {
					return tok.TokenID, true
				}
			}
		}
	}

	// 2. Check handover grace period for previous token keys
	if tok, inGrace := tr.graceTokens[tokenKey]; inGrace {
		if tok.State != TokenStateRevoked && now.Before(tok.GraceUntil) {
			if tr.mesh == nil || (!tr.mesh.IsRevoked(tok.TokenID) && !tr.mesh.IsRevoked(tok.LeaseID)) {
				return tok.TokenID, true
			}
		}
	}

	return "", false
}

// GetToken returns a copy of a tracked token.
func (tr *TokenRotator) GetToken(tokenID string) (*RotatedToken, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	tok, found := tr.tokens[tokenID]
	if !found {
		return nil, false
	}
	cp := *tok
	return &cp, true
}

// ListTokens returns a snapshot of all managed tokens.
func (tr *TokenRotator) ListTokens() []RotatedToken {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	res := make([]RotatedToken, 0, len(tr.tokens))
	for _, tok := range tr.tokens {
		res = append(res, *tok)
	}
	return res
}

// GetRecentEvents returns up to N recent rotation events.
func (tr *TokenRotator) GetRecentEvents(limit int) []RenewalEvent {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	if limit <= 0 || limit > len(tr.events) {
		limit = len(tr.events)
	}
	res := make([]RenewalEvent, limit)
	start := len(tr.events) - limit
	copy(res, tr.events[start:])
	return res
}

// GetSummary compiles live telemetry metrics for the rotator and mesh.
func (tr *TokenRotator) GetSummary() RotatorSummary {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	activeCount := 0
	for _, t := range tr.tokens {
		if t.State == TokenStateActive {
			activeCount++
		}
	}

	peerCount := 0
	avgLatency := 1.5
	if tr.mesh != nil {
		peerCount = len(tr.mesh.ListPeers())
		avgLatency = tr.mesh.GetAveragePropagationLatencyMs()
	}

	return RotatorSummary{
		ActiveTokens:            activeCount,
		RenewedCount:            tr.renewedCount,
		RevokedCount:            tr.revokedCount,
		MeshPeersCount:          peerCount,
		AvgPropagationLatencyMs: avgLatency,
		AutoRenewEnabled:        tr.config.AutoRenewEnabled,
		CheckIntervalSec:        tr.config.CheckInterval.Seconds(),
		GracePeriodSec:          tr.config.GracePeriod.Seconds(),
		LastSweepAt:             tr.lastSweepAt,
	}
}

func (tr *TokenRotator) handleMeshRevocation(tombstone RevocationTombstone) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	for _, tok := range tr.tokens {
		if tok.LeaseID == tombstone.LeaseID || tok.TokenID == tombstone.TokenID {
			tok.State = TokenStateRevoked
			delete(tr.tokenKeyIndex, tok.TokenKey)
			if tok.PreviousTokenKey != "" {
				delete(tr.graceTokens, tok.PreviousTokenKey)
			}
			tr.revokedCount++
		}
	}
}

func (tr *TokenRotator) runLoop() {
	ticker := time.NewTicker(tr.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tr.stopChan:
			return
		case <-ticker.C:
			tr.CheckAndRenew()
		}
	}
}

func generateRandomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
