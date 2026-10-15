package billing

import (
	"time"
)

// TierType defines the subscription billing tier of a Cloudflare account.
type TierType string

const (
	TierFree           TierType = "free"
	TierPaidStandard   TierType = "paid_standard"
	TierPaidEnterprise TierType = "paid_enterprise"
)

// CloudflareRates defines Cloudflare resource rates in micro-cents.
// 1 USD = 100 cents = 1,000,000 micro-cents.
// 1 cent = 10,000 micro-cents.
type CloudflareRates struct {
	// Workers pricing
	WorkersPerMillionRequestsMicroCents int64 `json:"workers_per_million_requests_micro_cents"` // $0.30/1M = 300,000 micro-cents
	WorkersPerMillionCpuMsMicroCents    int64 `json:"workers_per_million_cpu_ms_micro_cents"`    // $0.02/1M ms = 20,000 micro-cents
	DailyFreeWorkersRequests            int64 `json:"daily_free_workers_requests"`               // 100,000 requests/day

	// KV pricing
	KVReadsPerMillionMicroCents  int64 `json:"kv_reads_per_million_micro_cents"`  // $0.50/1M = 500,000 micro-cents
	KVWritesPerMillionMicroCents int64 `json:"kv_writes_per_million_micro_cents"` // $5.00/1M = 5,000,000 micro-cents
	DailyFreeKVReads             int64 `json:"daily_free_kv_reads"`               // 100,000 reads/day

	// R2 pricing
	R2StoragePerGBMonthMicroCents int64 `json:"r2_storage_per_gb_month_micro_cents"` // $0.015/GB-mo = 15,000 micro-cents
	R2ClassAPerMillionMicroCents  int64 `json:"r2_class_a_per_million_micro_cents"`  // $4.50/1M = 4,500,000 micro-cents
	R2ClassBPerMillionMicroCents  int64 `json:"r2_class_b_per_million_micro_cents"`  // $0.36/1M = 360,000 micro-cents
	DailyFreeR2StorageBytes       int64 `json:"daily_free_r2_storage_bytes"`         // 10 GB (10 * 1024^3)

	// Bandwidth pricing (Cloudflare egress is $0)
	EgressBandwidthPerGBMicroCents int64 `json:"egress_bandwidth_per_gb_micro_cents"` // 0 micro-cents
}

// DefaultCloudflareRates returns standard Cloudflare pricing rates in micro-cents.
func DefaultCloudflareRates() CloudflareRates {
	return CloudflareRates{
		WorkersPerMillionRequestsMicroCents: 300000,
		WorkersPerMillionCpuMsMicroCents:    20000,
		DailyFreeWorkersRequests:            100000,
		KVReadsPerMillionMicroCents:         500000,
		KVWritesPerMillionMicroCents:        5000000,
		DailyFreeKVReads:                    100000,
		R2StoragePerGBMonthMicroCents:       15000,
		R2ClassAPerMillionMicroCents:        4500000,
		R2ClassBPerMillionMicroCents:        360000,
		DailyFreeR2StorageBytes:             10 * 1024 * 1024 * 1024,
		EgressBandwidthPerGBMicroCents:      0,
	}
}

// UsageRecord represents an individual measured consumption event.
type UsageRecord struct {
	TenantID     string    `json:"tenant_id"`
	AccountID    string    `json:"account_id"`
	Service      string    `json:"service"`       // "workers", "kv", "r2", "proxy"
	ResourceType string    `json:"resource_type"` // "requests", "cpu_ms", "kv_read", "kv_write", "r2_class_a", "r2_class_b", "r2_storage", "bytes_egress"
	Quantity     int64     `json:"quantity"`
	DurationMs   int64     `json:"duration_ms"`
	Timestamp    time.Time `json:"timestamp"`
}

// CostEntry represents an audited, cost-attributed line item in the billing ledger.
type CostEntry struct {
	ID                  string    `json:"id"`
	Timestamp           time.Time `json:"timestamp"`
	TenantID            string    `json:"tenant_id"`
	AccountID           string    `json:"account_id"`
	Service             string    `json:"service"`
	ResourceType        string    `json:"resource_type"`
	Quantity            int64     `json:"quantity"`
	UnitRateMicroCents  int64     `json:"unit_rate_micro_cents"`
	TotalCostMicroCents int64     `json:"total_cost_micro_cents"`
	Currency            string    `json:"currency"`
}

// TenantSummary aggregates usage and attributed costs for a single tenant.
type TenantSummary struct {
	TenantID            string           `json:"tenant_id"`
	TotalCostMicroCents int64            `json:"total_cost_micro_cents"`
	TotalRequests       int64            `json:"total_requests"`
	TotalCpuMs          int64            `json:"total_cpu_ms"`
	TotalBytes          int64            `json:"total_bytes"`
	ServiceBreakdown    map[string]int64 `json:"service_breakdown"` // service -> cost_micro_cents
	BudgetLimitMicroCents int64          `json:"budget_limit_micro_cents"`
	IsOverBudget        bool             `json:"is_over_budget"`
	LastActive          time.Time        `json:"last_active"`
}

// AccountCostSummary aggregates usage and cost for an underlying Cloudflare pooled account.
type AccountCostSummary struct {
	AccountID              string    `json:"account_id"`
	Tier                   TierType  `json:"tier"`
	TotalCostMicroCents    int64     `json:"total_cost_micro_cents"`
	MonthlyBudgetMicroCents int64    `json:"monthly_budget_micro_cents"`
	WorkersRequestsCount   int64     `json:"workers_requests_count"`
	FreeWorkersRemaining   int64     `json:"free_workers_remaining"`
	KVReadsCount           int64     `json:"kv_reads_count"`
	FreeKVReadsRemaining   int64     `json:"free_kv_reads_remaining"`
	EstimatedSavingsMicroCents int64 `json:"estimated_savings_micro_cents"`
	LastUpdated            time.Time `json:"last_updated"`
}

// MigrationRule governs automated workload migration to prevent paid overages.
type MigrationRule struct {
	OverageThresholdPercent   float64 `json:"overage_threshold_percent"`    // e.g. 80.0
	MaxAllowedOverageMicroCents int64 `json:"max_allowed_overage_micro_cents"` // threshold to trigger migration
	PreferFreeTierStandby     bool    `json:"prefer_free_tier_standby"`
	AutoMigrateOnOverage      bool    `json:"auto_migrate_on_overage"`
}

// DefaultMigrationRule returns standard cost optimization thresholds.
func DefaultMigrationRule() MigrationRule {
	return MigrationRule{
		OverageThresholdPercent:   80.0,
		MaxAllowedOverageMicroCents: 5000000, // $5.00
		PreferFreeTierStandby:     true,
		AutoMigrateOnOverage:      true,
	}
}

// MigrationEvent records an automated workload or lease rerouting action.
type MigrationEvent struct {
	ID                     string    `json:"id"`
	Timestamp              time.Time `json:"timestamp"`
	SourceAccountID        string    `json:"source_account_id"`
	TargetAccountID        string    `json:"target_account_id"`
	TenantID               string    `json:"tenant_id"`
	Reason                 string    `json:"reason"`
	LeasesMigrated         int       `json:"leases_migrated"`
	EstimatedSavingsMicroCents int64 `json:"estimated_savings_micro_cents"`
	Status                 string    `json:"status"` // "completed", "failed"
}
