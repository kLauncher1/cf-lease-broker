package billing

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

const (
	defaultMaxMigrations = 1000
)

// CostOptimizer detects workload overages and autonomously migrates traffic to cost-optimal standby accounts.
type CostOptimizer struct {
	mu                     sync.RWMutex
	meter                  *BillingMeter
	pool                   *broker.PoolManager
	rule                   MigrationRule
	migrations             []MigrationEvent
	totalSavingsMicroCents int64
}

// NewCostOptimizer constructs an initialized CostOptimizer.
func NewCostOptimizer(meter *BillingMeter, pool *broker.PoolManager, rule MigrationRule) *CostOptimizer {
	return &CostOptimizer{
		meter:      meter,
		pool:       pool,
		rule:       rule,
		migrations: make([]MigrationEvent, 0, 128),
	}
}

// GetRule returns the active migration rule configuration.
func (o *CostOptimizer) GetRule() MigrationRule {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.rule
}

// SetRule updates the migration rule configuration.
func (o *CostOptimizer) SetRule(rule MigrationRule) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.rule = rule
}

// GetTotalSavings returns the total cumulative micro-cents saved by optimizer migrations.
func (o *CostOptimizer) GetTotalSavings() int64 {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.totalSavingsMicroCents
}

// GetRecentMigrations returns up to limit recent migration events in reverse chronological order.
func (o *CostOptimizer) GetRecentMigrations(limit int) []MigrationEvent {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if limit <= 0 || limit > len(o.migrations) {
		limit = len(o.migrations)
	}

	res := make([]MigrationEvent, 0, limit)
	for i := len(o.migrations) - 1; i >= 0 && len(res) < limit; i-- {
		res = append(res, o.migrations[i])
	}
	return res
}

// EvaluateWorkloadMigration audits all accounts for overages and executes migrations when necessary.
func (o *CostOptimizer) EvaluateWorkloadMigration() []MigrationEvent {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.pool == nil {
		return nil
	}

	accounts := o.pool.ListAccounts()
	if len(accounts) < 2 {
		return nil
	}

	meterAccounts := o.meter.GetAllAccounts()
	rates := o.meter.GetRates()

	// 1. Identify candidate target accounts with remaining capacity (standby accounts)
	type candidate struct {
		account        broker.AccountStatus
		tier           TierType
		availableQuota int64
		freeRemaining  int64
	}

	var standbyFree []candidate
	var standbyOthers []candidate

	for _, acc := range accounts {
		if !acc.IsHealthy {
			continue
		}
		avail := acc.WorkerLimit - acc.WorkerUsed
		if avail <= 0 {
			continue
		}

		tier := TierPaidStandard
		var freeRemaining int64 = 0
		if mAcc, ok := meterAccounts[acc.ID]; ok {
			tier = mAcc.Tier
			freeRemaining = mAcc.FreeWorkersRemaining
		}

		c := candidate{
			account:        acc,
			tier:           tier,
			availableQuota: avail,
			freeRemaining:  freeRemaining,
		}

		if tier == TierFree && (freeRemaining > 0 || avail > 1000) {
			standbyFree = append(standbyFree, c)
		} else {
			standbyOthers = append(standbyOthers, c)
		}
	}

	// 2. Identify over-utilized accounts requiring migration
	var events []MigrationEvent

	for _, acc := range accounts {
		if !acc.IsHealthy || acc.WorkerLimit <= 0 {
			continue
		}

		usagePct := (float64(acc.WorkerUsed) / float64(acc.WorkerLimit)) * 100.0
		needsMigration := false
		reason := ""

		if usagePct >= o.rule.OverageThresholdPercent {
			needsMigration = true
			reason = fmt.Sprintf("High usage threshold reached (%.1f%% >= %.1f%%)", usagePct, o.rule.OverageThresholdPercent)
		} else {
			// Check if account is paid standard while standby free tier accounts are available
			mAcc, ok := meterAccounts[acc.ID]
			if ok && mAcc.Tier != TierFree && len(standbyFree) > 0 && acc.WorkerUsed > 500 {
				needsMigration = true
				reason = fmt.Sprintf("Paid tier account active while %d standby free accounts have zero cost allowance", len(standbyFree))
			}
		}

		if !needsMigration {
			continue
		}

		// Select best target standby account
		var target *candidate
		if o.rule.PreferFreeTierStandby && len(standbyFree) > 0 {
			for i := range standbyFree {
				if standbyFree[i].account.ID != acc.ID {
					target = &standbyFree[i]
					break
				}
			}
		}

		if target == nil && len(standbyOthers) > 0 {
			for i := range standbyOthers {
				if standbyOthers[i].account.ID != acc.ID {
					target = &standbyOthers[i]
					break
				}
			}
		}

		if target == nil || target.account.ID == acc.ID {
			continue
		}

		// Calculate estimated savings: 1,000 requests shifted from paid/overage to free
		estSavings := (1000 * rates.WorkersPerMillionRequestsMicroCents) / 1000000
		if estSavings < 100 {
			estSavings = 100
		}

		event := MigrationEvent{
			ID:                         generateMigrationID(),
			Timestamp:                  time.Now().UTC(),
			SourceAccountID:            acc.ID,
			TargetAccountID:            target.account.ID,
			TenantID:                   "cluster",
			Reason:                     reason,
			LeasesMigrated:             1,
			EstimatedSavingsMicroCents: estSavings,
			Status:                     "completed",
		}

		if o.rule.AutoMigrateOnOverage {
			// Place source account in short cooldown probation so the pool routes traffic to target
			_ = o.pool.SetAccountProbation(acc.ID, 3*time.Minute)
		}

		// Record event
		if len(o.migrations) >= defaultMaxMigrations {
			o.migrations = append(o.migrations[:0], o.migrations[1:]...)
		}
		o.migrations = append(o.migrations, event)
		o.totalSavingsMicroCents += estSavings
		events = append(events, event)
	}

	return events
}

// TriggerManualMigration forces a specific account-to-account workload migration.
func (o *CostOptimizer) TriggerManualMigration(sourceID, targetID, tenantID, reason string) (MigrationEvent, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if sourceID == "" || targetID == "" {
		return MigrationEvent{}, fmt.Errorf("source and target account IDs must not be empty")
	}
	if sourceID == targetID {
		return MigrationEvent{}, fmt.Errorf("source and target account cannot be identical")
	}

	rates := o.meter.GetRates()
	estSavings := (5000 * rates.WorkersPerMillionRequestsMicroCents) / 1000000

	event := MigrationEvent{
		ID:                         generateMigrationID(),
		Timestamp:                  time.Now().UTC(),
		SourceAccountID:            sourceID,
		TargetAccountID:            targetID,
		TenantID:                   tenantID,
		Reason:                     reason,
		LeasesMigrated:             1,
		EstimatedSavingsMicroCents: estSavings,
		Status:                     "completed",
	}

	if o.pool != nil {
		_ = o.pool.SetAccountProbation(sourceID, 5*time.Minute)
	}

	if len(o.migrations) >= defaultMaxMigrations {
		o.migrations = append(o.migrations[:0], o.migrations[1:]...)
	}
	o.migrations = append(o.migrations, event)
	o.totalSavingsMicroCents += estSavings

	return event, nil
}

func generateMigrationID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("mig_%d", time.Now().UnixNano())
	}
	return "mig_" + hex.EncodeToString(b)
}
