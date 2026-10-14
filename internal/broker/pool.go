package broker

import (
	"errors"
	"sync"
	"time"
)

// AccountStatus represents a pooled Cloudflare account and its usage quota.
type AccountStatus struct {
	ID             string    `json:"id"`
	Alias          string    `json:"alias"`
	Plan           string    `json:"plan"`
	WorkerLimit    int64     `json:"worker_limit"`
	WorkerUsed     int64     `json:"worker_used"`
	R2StorageMB    float64   `json:"r2_storage_mb"`
	KVReadCount    int64     `json:"kv_read_count"`
	IsHealthy      bool      `json:"is_healthy"`
	LastFailoverAt time.Time `json:"last_failover_at,omitempty"`
	ProbationUntil time.Time `json:"probation_until,omitempty"`
}

// PoolManager coordinates multiple Cloudflare tenant accounts and dynamic failover.
type PoolManager struct {
	mu       sync.RWMutex
	accounts map[string]*AccountStatus
}

// NewPoolManager initializes a pool manager with default cluster profiles.
func NewPoolManager() *PoolManager {
	pm := &PoolManager{
		accounts: make(map[string]*AccountStatus),
	}

	// Pre-seed mock/standby tenant profiles for out-of-the-box operation
	pm.accounts["cf-tenant-alpha"] = &AccountStatus{
		ID:          "cf-tenant-alpha",
		Alias:       "Edge Cluster Primary",
		Plan:        "Free Tier (100k/day)",
		WorkerLimit: 100000,
		WorkerUsed:  14200,
		R2StorageMB: 340.5,
		KVReadCount: 8900,
		IsHealthy:   true,
	}
	pm.accounts["cf-tenant-bravo"] = &AccountStatus{
		ID:          "cf-tenant-bravo",
		Alias:       "Edge Cluster Secondary",
		Plan:        "Free Tier (100k/day)",
		WorkerLimit: 100000,
		WorkerUsed:  3210,
		R2StorageMB: 120.0,
		KVReadCount: 1540,
		IsHealthy:   true,
	}
	pm.accounts["cf-tenant-charlie"] = &AccountStatus{
		ID:          "cf-tenant-charlie",
		Alias:       "Edge Cluster Standby",
		Plan:        "Free Tier (100k/day)",
		WorkerLimit: 100000,
		WorkerUsed:  850,
		R2StorageMB: 50.0,
		KVReadCount: 420,
		IsHealthy:   true,
	}

	return pm
}

// ListAccounts returns current snapshots of all pooled Cloudflare accounts.
func (pm *PoolManager) ListAccounts() []AccountStatus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	res := make([]AccountStatus, 0, len(pm.accounts))
	for _, acc := range pm.accounts {
		res = append(res, *acc)
	}
	return res
}

// SelectOptimalAccount picks the healthy account with the highest available quota.
func (pm *PoolManager) SelectOptimalAccount() (*AccountStatus, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now().UTC()
	var candidate *AccountStatus
	var maxAvailable int64 = -1

	for _, acc := range pm.accounts {
		// Auto-recover account if probation period has elapsed
		if !acc.IsHealthy && !acc.ProbationUntil.IsZero() && now.After(acc.ProbationUntil) {
			acc.IsHealthy = true
			acc.ProbationUntil = time.Time{}
		}

		if !acc.IsHealthy {
			continue
		}
		available := acc.WorkerLimit - acc.WorkerUsed
		if available > maxAvailable {
			maxAvailable = available
			candidate = acc
		}
	}

	if candidate == nil || maxAvailable <= 0 {
		return nil, errors.New("no healthy Cloudflare accounts with available quota")
	}
	return candidate, nil
}

// SetAccountProbation places an account into probation until a future timestamp.
func (pm *PoolManager) SetAccountProbation(accountID string, duration time.Duration) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	acc, exists := pm.accounts[accountID]
	if !exists {
		return errors.New("account not found")
	}

	acc.IsHealthy = false
	acc.ProbationUntil = time.Now().UTC().Add(duration)
	acc.LastFailoverAt = time.Now().UTC()
	return nil
}

// RecoverAccount restores an account to healthy operational status immediately.
func (pm *PoolManager) RecoverAccount(accountID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	acc, exists := pm.accounts[accountID]
	if !exists {
		return errors.New("account not found")
	}

	acc.IsHealthy = true
	acc.ProbationUntil = time.Time{}
	return nil
}

// TriggerFailover marks an account as unhealthy and records a failover timestamp.
func (pm *PoolManager) TriggerFailover(accountID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	acc, exists := pm.accounts[accountID]
	if !exists {
		return errors.New("account not found")
	}

	acc.IsHealthy = false
	acc.LastFailoverAt = time.Now().UTC()
	return nil
}

// ListHealthyAccountIDs returns IDs of all currently healthy accounts.
func (pm *PoolManager) ListHealthyAccountIDs() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var ids []string
	for id, acc := range pm.accounts {
		if acc.IsHealthy {
			ids = append(ids, id)
		}
	}
	return ids
}

// UpdateR2Storage adjusts the recorded R2 storage consumption for an account.
func (pm *PoolManager) UpdateR2Storage(accountID string, deltaMB float64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	acc, exists := pm.accounts[accountID]
	if !exists {
		return errors.New("account not found")
	}

	acc.R2StorageMB += deltaMB
	if acc.R2StorageMB < 0 {
		acc.R2StorageMB = 0
	}
	return nil
}

// IncrementKVRead increments the KV read counter for an account.
func (pm *PoolManager) IncrementKVRead(accountID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	acc, exists := pm.accounts[accountID]
	if !exists {
		return errors.New("account not found")
	}

	acc.KVReadCount++
	return nil
}

// IncrementWorkerUsed increments the Worker daily request counter.
func (pm *PoolManager) IncrementWorkerUsed(accountID string, delta int64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	acc, exists := pm.accounts[accountID]
	if !exists {
		return errors.New("account not found")
	}

	acc.WorkerUsed += delta
	return nil
}

// GetAccount returns a copy of the specified account status.
func (pm *PoolManager) GetAccount(accountID string) (*AccountStatus, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	acc, exists := pm.accounts[accountID]
	if !exists {
		return nil, errors.New("account not found")
	}
	cpy := *acc
	return &cpy, nil
}
