package broker

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// LeaseVoucher represents a granted temporary resource lease.
type LeaseVoucher struct {
	LeaseID       string    `json:"lease_id"`
	AccountID     string    `json:"account_id"`
	TargetService string    `json:"target_service"`
	ResourceType  string    `json:"resource_type"`
	TokenKey      string    `json:"token_key"`
	EndpointURL   string    `json:"endpoint_url"`
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	IsRevoked     bool      `json:"is_revoked"`
}

// LeaseEngine manages the lifecycle of dynamic resource leases.
type LeaseEngine struct {
	mu          sync.RWMutex
	poolManager *PoolManager
	leases      map[string]*LeaseVoucher
}

// NewLeaseEngine initializes a lease engine coupled to a pool manager.
func NewLeaseEngine(pm *PoolManager) *LeaseEngine {
	return &LeaseEngine{
		poolManager: pm,
		leases:      make(map[string]*LeaseVoucher),
	}
}

// GrantLease allocates a temporary lease against the optimal pooled account.
func (le *LeaseEngine) GrantLease(targetService, resourceType string, duration time.Duration) (*LeaseVoucher, error) {
	optimalAcc, err := le.poolManager.SelectOptimalAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to allocate lease: %w", err)
	}

	leaseID := "lse_" + generateRandomHex(6)
	tokenKey := "cftok_" + generateRandomHex(16)
	endpoint := fmt.Sprintf("https://%s.%s.workers.dev", targetService, optimalAcc.ID)

	now := time.Now().UTC()
	voucher := &LeaseVoucher{
		LeaseID:       leaseID,
		AccountID:     optimalAcc.ID,
		TargetService: targetService,
		ResourceType:  resourceType,
		TokenKey:      tokenKey,
		EndpointURL:   endpoint,
		IssuedAt:      now,
		ExpiresAt:     now.Add(duration),
		IsRevoked:     false,
	}

	le.mu.Lock()
	le.leases[leaseID] = voucher
	le.mu.Unlock()

	return voucher, nil
}

// RevokeLease immediately invalidates an active lease.
func (le *LeaseEngine) RevokeLease(leaseID string) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	voucher, exists := le.leases[leaseID]
	if !exists {
		return errors.New("lease voucher not found")
	}

	voucher.IsRevoked = true
	return nil
}

// ListLeases returns current active and expired leases.
func (le *LeaseEngine) ListLeases() []LeaseVoucher {
	le.mu.RLock()
	defer le.mu.RUnlock()

	res := make([]LeaseVoucher, 0, len(le.leases))
	for _, l := range le.leases {
		res = append(res, *l)
	}
	return res
}

// GetLease retrieves an active or expired voucher by ID.
func (le *LeaseEngine) GetLease(leaseID string) (*LeaseVoucher, bool) {
	le.mu.RLock()
	defer le.mu.RUnlock()

	voucher, exists := le.leases[leaseID]
	if !exists {
		return nil, false
	}
	cp := *voucher
	return &cp, true
}

// ValidateVoucher performs constant-time validation of a lease token key and checks expiration/revocation.
func (le *LeaseEngine) ValidateVoucher(leaseID, tokenKey string) bool {
	le.mu.RLock()
	defer le.mu.RUnlock()

	voucher, exists := le.leases[leaseID]
	if !exists || voucher.IsRevoked {
		return false
	}

	if time.Now().UTC().After(voucher.ExpiresAt) {
		return false
	}

	if subtle.ConstantTimeCompare([]byte(voucher.TokenKey), []byte(tokenKey)) != 1 {
		return false
	}

	return true
}

// ApplyVoucher records a replicated lease voucher in the local state.
func (le *LeaseEngine) ApplyVoucher(voucher LeaseVoucher) {
	le.mu.Lock()
	defer le.mu.Unlock()
	le.leases[voucher.LeaseID] = &voucher
}

func generateRandomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
