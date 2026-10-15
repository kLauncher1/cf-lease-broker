package billing

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const (
	defaultMaxEntries = 10000
)

// BillingMeter provides thread-safe real-time metering and micro-cent cost attribution.
type BillingMeter struct {
	mu                          sync.RWMutex
	rates                       CloudflareRates
	maxEntries                  int
	entries                     []CostEntry
	tenants                     map[string]*TenantSummary
	accounts                    map[string]*AccountCostSummary
	serviceTotals               map[string]int64
	totalClusterCostMicroCents  int64
	totalClusterSavingsMicroCents int64
	totalClusterRequests        int64
	totalClusterBytes           int64
	totalClusterCpuMs           int64
	currentDay                  string
}

// NewBillingMeter constructs a new initialized BillingMeter.
func NewBillingMeter(rates CloudflareRates) *BillingMeter {
	now := time.Now().UTC()
	return &BillingMeter{
		rates:         rates,
		maxEntries:    defaultMaxEntries,
		entries:       make([]CostEntry, 0, 1024),
		tenants:       make(map[string]*TenantSummary),
		accounts:      make(map[string]*AccountCostSummary),
		serviceTotals: make(map[string]int64),
		currentDay:    now.Format("2006-01-02"),
	}
}

// SetRates safely updates active pricing rates.
func (m *BillingMeter) SetRates(rates CloudflareRates) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rates = rates
}

// GetRates returns a copy of current pricing rates.
func (m *BillingMeter) GetRates() CloudflareRates {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rates
}

// RegisterAccount initializes or updates an account tier profile.
func (m *BillingMeter) RegisterAccount(accountID string, tier TierType, monthlyBudgetMicroCents int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, exists := m.accounts[accountID]
	if !exists {
		acc = &AccountCostSummary{
			AccountID:              accountID,
			Tier:                   tier,
			MonthlyBudgetMicroCents: monthlyBudgetMicroCents,
			FreeWorkersRemaining:   m.rates.DailyFreeWorkersRequests,
			FreeKVReadsRemaining:   m.rates.DailyFreeKVReads,
			LastUpdated:            time.Now().UTC(),
		}
		m.accounts[accountID] = acc
	} else {
		acc.Tier = tier
		if monthlyBudgetMicroCents > 0 {
			acc.MonthlyBudgetMicroCents = monthlyBudgetMicroCents
		}
		acc.LastUpdated = time.Now().UTC()
	}
}

// RecordUsage attributes micro-cent cost to a consumption record and stores it in the ledger.
func (m *BillingMeter) RecordUsage(record UsageRecord) CostEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.checkDailyRollLocked()

	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	if record.TenantID == "" {
		record.TenantID = "default"
	}
	if record.AccountID == "" {
		record.AccountID = "primary"
	}

	acc, exists := m.accounts[record.AccountID]
	if !exists {
		acc = &AccountCostSummary{
			AccountID:            record.AccountID,
			Tier:                 TierPaidStandard,
			FreeWorkersRemaining: m.rates.DailyFreeWorkersRequests,
			FreeKVReadsRemaining: m.rates.DailyFreeKVReads,
			LastUpdated:          record.Timestamp,
		}
		m.accounts[record.AccountID] = acc
	}

	unitRate, cost, savings := m.calculateCostLocked(record, acc)

	entryID := generateEntryID()
	entry := CostEntry{
		ID:                  entryID,
		Timestamp:           record.Timestamp,
		TenantID:            record.TenantID,
		AccountID:           record.AccountID,
		Service:             record.Service,
		ResourceType:        record.ResourceType,
		Quantity:            record.Quantity,
		UnitRateMicroCents:  unitRate,
		TotalCostMicroCents: cost,
		Currency:            "USD",
	}

	// Append to bounded ledger journal
	if len(m.entries) >= m.maxEntries {
		// Evict oldest 10% to prevent excessive re-allocations
		evictCount := m.maxEntries / 10
		if evictCount < 1 {
			evictCount = 1
		}
		m.entries = append(m.entries[:0], m.entries[evictCount:]...)
	}
	m.entries = append(m.entries, entry)

	// Update tenant aggregate
	tenant, tExists := m.tenants[record.TenantID]
	if !tExists {
		tenant = &TenantSummary{
			TenantID:         record.TenantID,
			ServiceBreakdown: make(map[string]int64),
			LastActive:       record.Timestamp,
		}
		m.tenants[record.TenantID] = tenant
	}
	tenant.TotalCostMicroCents += cost
	if record.ResourceType == "requests" {
		tenant.TotalRequests += record.Quantity
	}
	if record.ResourceType == "cpu_ms" {
		tenant.TotalCpuMs += record.Quantity
	}
	tenant.ServiceBreakdown[record.Service] += cost
	tenant.LastActive = record.Timestamp
	if tenant.BudgetLimitMicroCents > 0 && tenant.TotalCostMicroCents >= tenant.BudgetLimitMicroCents {
		tenant.IsOverBudget = true
	}

	// Update account aggregate
	acc.TotalCostMicroCents += cost
	acc.EstimatedSavingsMicroCents += savings
	acc.LastUpdated = record.Timestamp

	// Update cluster totals
	m.totalClusterCostMicroCents += cost
	m.totalClusterSavingsMicroCents += savings
	if record.ResourceType == "requests" {
		m.totalClusterRequests += record.Quantity
	}
	if record.ResourceType == "cpu_ms" {
		m.totalClusterCpuMs += record.Quantity
	}
	m.serviceTotals[record.Service] += cost

	return entry
}

// RecordProxyRequest is a convenience method for proxy request telemetry.
func (m *BillingMeter) RecordProxyRequest(tenantID, accountID, service string, cpuMs, bytesIn, bytesOut int64) {
	now := time.Now().UTC()
	// Record HTTP request count
	m.RecordUsage(UsageRecord{
		TenantID:     tenantID,
		AccountID:    accountID,
		Service:      service,
		ResourceType: "requests",
		Quantity:     1,
		DurationMs:   cpuMs,
		Timestamp:    now,
	})

	// Record compute CPU ms if positive
	if cpuMs > 0 {
		m.RecordUsage(UsageRecord{
			TenantID:     tenantID,
			AccountID:    accountID,
			Service:      service,
			ResourceType: "cpu_ms",
			Quantity:     cpuMs,
			DurationMs:   cpuMs,
			Timestamp:    now,
		})
	}

	// Record egress bytes
	totalBytes := bytesIn + bytesOut
	if totalBytes > 0 {
		m.mu.Lock()
		m.totalClusterBytes += totalBytes
		if t, ok := m.tenants[tenantID]; ok {
			t.TotalBytes += totalBytes
		}
		m.mu.Unlock()
	}
}

// calculateCostLocked computes the unit rate, cost, and savings for a record.
func (m *BillingMeter) calculateCostLocked(record UsageRecord, acc *AccountCostSummary) (unitRate int64, cost int64, savings int64) {
	qty := record.Quantity
	if qty <= 0 {
		return 0, 0, 0
	}

	switch record.ResourceType {
	case "requests":
		unitRate = m.rates.WorkersPerMillionRequestsMicroCents
		acc.WorkersRequestsCount += qty
		if acc.Tier == TierFree {
			if acc.FreeWorkersRemaining > 0 {
				freeUsed := qty
				if freeUsed > acc.FreeWorkersRemaining {
					freeUsed = acc.FreeWorkersRemaining
				}
				acc.FreeWorkersRemaining -= freeUsed
				savings = (freeUsed * unitRate) / 1000000
				billable := qty - freeUsed
				cost = (billable * unitRate) / 1000000
			} else {
				cost = (qty * unitRate) / 1000000
			}
		} else {
			cost = (qty * unitRate) / 1000000
		}

	case "cpu_ms":
		unitRate = m.rates.WorkersPerMillionCpuMsMicroCents
		cost = (qty * unitRate) / 1000000

	case "kv_read":
		unitRate = m.rates.KVReadsPerMillionMicroCents
		acc.KVReadsCount += qty
		if acc.Tier == TierFree {
			if acc.FreeKVReadsRemaining > 0 {
				freeUsed := qty
				if freeUsed > acc.FreeKVReadsRemaining {
					freeUsed = acc.FreeKVReadsRemaining
				}
				acc.FreeKVReadsRemaining -= freeUsed
				savings = (freeUsed * unitRate) / 1000000
				billable := qty - freeUsed
				cost = (billable * unitRate) / 1000000
			} else {
				cost = (qty * unitRate) / 1000000
			}
		} else {
			cost = (qty * unitRate) / 1000000
		}

	case "kv_write":
		unitRate = m.rates.KVWritesPerMillionMicroCents
		cost = (qty * unitRate) / 1000000

	case "r2_class_a":
		unitRate = m.rates.R2ClassAPerMillionMicroCents
		cost = (qty * unitRate) / 1000000

	case "r2_class_b":
		unitRate = m.rates.R2ClassBPerMillionMicroCents
		cost = (qty * unitRate) / 1000000

	case "r2_storage":
		unitRate = m.rates.R2StoragePerGBMonthMicroCents
		// qty is in GB-hours, pro-rated to month (720h)
		cost = (qty * unitRate) / 720

	default:
		unitRate = 0
		cost = 0
	}

	return unitRate, cost, savings
}

// checkDailyRollLocked resets daily free allowances upon UTC date turnover.
func (m *BillingMeter) checkDailyRollLocked() {
	today := time.Now().UTC().Format("2006-01-02")
	if today != m.currentDay {
		m.currentDay = today
		for _, acc := range m.accounts {
			if acc.Tier == TierFree {
				acc.FreeWorkersRemaining = m.rates.DailyFreeWorkersRequests
				acc.FreeKVReadsRemaining = m.rates.DailyFreeKVReads
			}
		}
	}
}

// GetTenantSummary returns a tenant summary copy if found.
func (m *BillingMeter) GetTenantSummary(tenantID string) (*TenantSummary, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, false
	}
	res := *t
	res.ServiceBreakdown = make(map[string]int64, len(t.ServiceBreakdown))
	for k, v := range t.ServiceBreakdown {
		res.ServiceBreakdown[k] = v
	}
	return &res, true
}

// GetAllTenants returns copies of all tenant summaries.
func (m *BillingMeter) GetAllTenants() map[string]*TenantSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]*TenantSummary, len(m.tenants))
	for k, v := range m.tenants {
		copyItem := *v
		copyItem.ServiceBreakdown = make(map[string]int64, len(v.ServiceBreakdown))
		for sk, sv := range v.ServiceBreakdown {
			copyItem.ServiceBreakdown[sk] = sv
		}
		res[k] = &copyItem
	}
	return res
}

// GetAccountCost returns an account summary copy if found.
func (m *BillingMeter) GetAccountCost(accountID string) (*AccountCostSummary, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	acc, ok := m.accounts[accountID]
	if !ok {
		return nil, false
	}
	copyAcc := *acc
	return &copyAcc, true
}

// GetAllAccounts returns copies of all account summaries.
func (m *BillingMeter) GetAllAccounts() map[string]*AccountCostSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]*AccountCostSummary, len(m.accounts))
	for k, v := range m.accounts {
		copyAcc := *v
		res[k] = &copyAcc
	}
	return res
}

// GetClusterTotals returns aggregate cluster metrics.
func (m *BillingMeter) GetClusterTotals() (cost int64, savings int64, reqs int64, bytes int64, cpuMs int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalClusterCostMicroCents, m.totalClusterSavingsMicroCents, m.totalClusterRequests, m.totalClusterBytes, m.totalClusterCpuMs
}

// GetRecentEntries returns up to limit recent ledger entries, optionally filtered by tenant.
func (m *BillingMeter) GetRecentEntries(limit int, tenantFilter string) []CostEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.entries) {
		limit = len(m.entries)
	}

	result := make([]CostEntry, 0, limit)
	for i := len(m.entries) - 1; i >= 0 && len(result) < limit; i-- {
		e := m.entries[i]
		if tenantFilter == "" || e.TenantID == tenantFilter {
			result = append(result, e)
		}
	}
	return result
}

// Reset clears all in-memory ledger state and aggregates.
func (m *BillingMeter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = m.entries[:0]
	m.tenants = make(map[string]*TenantSummary)
	m.accounts = make(map[string]*AccountCostSummary)
	m.serviceTotals = make(map[string]int64)
	m.totalClusterCostMicroCents = 0
	m.totalClusterSavingsMicroCents = 0
	m.totalClusterRequests = 0
	m.totalClusterBytes = 0
	m.totalClusterCpuMs = 0
}

func generateEntryID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("cost_%d", time.Now().UnixNano())
	}
	return "cost_" + hex.EncodeToString(b)
}
