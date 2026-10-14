package quota

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// MetricsCollector accumulates broker telemetry and exports standard Prometheus exposition format.
type MetricsCollector struct {
	mu sync.RWMutex

	poolMgr     *broker.PoolManager
	leaseEng    *broker.LeaseEngine
	probation   *ProbationManager
	limiter     *SlidingWindowLimiter

	requestsTotal      map[string]int64 // key: service|status|account
	rateLimitHitsTotal map[string]int64 // key: account
	failoversTotal     map[string]int64 // key: account
	webhooksTotal      map[string]int64 // key: channel|status

	cumulativeLeases   int64
}

// NewMetricsCollector constructs a Prometheus metrics exporter backed by live broker services.
func NewMetricsCollector(
	poolMgr *broker.PoolManager,
	leaseEng *broker.LeaseEngine,
	probation *ProbationManager,
	limiter *SlidingWindowLimiter,
) *MetricsCollector {
	return &MetricsCollector{
		poolMgr:            poolMgr,
		leaseEng:           leaseEng,
		probation:          probation,
		limiter:            limiter,
		requestsTotal:      make(map[string]int64),
		rateLimitHitsTotal: make(map[string]int64),
		failoversTotal:     make(map[string]int64),
		webhooksTotal:      make(map[string]int64),
	}
}

// IncCumulativeLeases increments the lifetime granted lease counter.
func (mc *MetricsCollector) IncCumulativeLeases() {
	atomic.AddInt64(&mc.cumulativeLeases, 1)
}

// RecordRequest tracks a completed reverse proxy or gateway HTTP request.
func (mc *MetricsCollector) RecordRequest(service, status, account string) {
	if service == "" {
		service = "unknown"
	}
	if status == "" {
		status = "200"
	}
	if account == "" {
		account = "unassigned"
	}

	key := fmt.Sprintf("%s|%s|%s", service, status, account)

	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.requestsTotal[key]++
}

// RecordRateLimitHit tracks an intercepted 429 status or sliding-window rejection.
func (mc *MetricsCollector) RecordRateLimitHit(account string) {
	if account == "" {
		account = "global"
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.rateLimitHitsTotal[account]++
}

// RecordFailover tracks a triggered failover event for a tenant account.
func (mc *MetricsCollector) RecordFailover(account string) {
	if account == "" {
		account = "unknown"
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.failoversTotal[account]++
}

// RecordWebhookDispatch tracks an outgoing webhook attempt.
func (mc *MetricsCollector) RecordWebhookDispatch(channel, status string) {
	key := fmt.Sprintf("%s|%s", channel, status)
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.webhooksTotal[key]++
}

// FormatPrometheus generates standard Prometheus 0.0.4 text representation.
func (mc *MetricsCollector) FormatPrometheus() string {
	var b strings.Builder

	// Header declaration
	b.WriteString("# CF-Lease-Broker Prometheus Telemetry Exporter\n\n")

	// 1. Account counts by status
	b.WriteString("# HELP cf_broker_accounts_total Total number of pooled Cloudflare accounts by state\n")
	b.WriteString("# TYPE cf_broker_accounts_total gauge\n")

	healthyCount := 0
	probationCount := 0
	exhaustedCount := 0

	var accounts []broker.AccountStatus
	if mc.poolMgr != nil {
		accounts = mc.poolMgr.ListAccounts()
	}

	for _, acc := range accounts {
		if !acc.IsHealthy {
			if !acc.ProbationUntil.IsZero() {
				probationCount++
			} else {
				exhaustedCount++
			}
		} else {
			healthyCount++
		}
	}
	fmt.Fprintf(&b, "cf_broker_accounts_total{status=\"healthy\"} %d\n", healthyCount)
	fmt.Fprintf(&b, "cf_broker_accounts_total{status=\"probation\"} %d\n", probationCount)
	fmt.Fprintf(&b, "cf_broker_accounts_total{status=\"exhausted\"} %d\n", exhaustedCount)
	b.WriteString("\n")

	// 2. Probation accounts gauge
	b.WriteString("# HELP cf_broker_probation_accounts_total Number of accounts currently undergoing rate-limit probation\n")
	b.WriteString("# TYPE cf_broker_probation_accounts_total gauge\n")
	fmt.Fprintf(&b, "cf_broker_probation_accounts_total %d\n\n", probationCount)

	// 3. Active & cumulative leases
	activeLeases := 0
	if mc.leaseEng != nil {
		activeLeases = len(mc.leaseEng.ListLeases())
	}
	b.WriteString("# HELP cf_broker_leases_active Currently active resource vouchers\n")
	b.WriteString("# TYPE cf_broker_leases_active gauge\n")
	fmt.Fprintf(&b, "cf_broker_leases_active %d\n\n", activeLeases)

	cumulative := atomic.LoadInt64(&mc.cumulativeLeases)
	if cumulative < int64(activeLeases) {
		cumulative = int64(activeLeases)
	}
	b.WriteString("# HELP cf_broker_leases_total Cumulative total of granted resource vouchers\n")
	b.WriteString("# TYPE cf_broker_leases_total counter\n")
	fmt.Fprintf(&b, "cf_broker_leases_total %d\n\n", cumulative)

	// 4. Quota utilization ratios per account and resource
	b.WriteString("# HELP cf_broker_quota_usage_ratio Current tenant quota utilization ratio (0.0 to 1.0)\n")
	b.WriteString("# TYPE cf_broker_quota_usage_ratio gauge\n")
	for _, acc := range accounts {
		// Worker ratio
		var workerRatio float64
		if acc.WorkerLimit > 0 {
			workerRatio = float64(acc.WorkerUsed) / float64(acc.WorkerLimit)
		}
		fmt.Fprintf(&b, "cf_broker_quota_usage_ratio{account=\"%s\",resource=\"workers\"} %.4f\n", acc.ID, workerRatio)

		// R2 ratio (10GB limit)
		r2Ratio := acc.R2StorageMB / 10000.0
		fmt.Fprintf(&b, "cf_broker_quota_usage_ratio{account=\"%s\",resource=\"r2\"} %.4f\n", acc.ID, r2Ratio)

		// KV ratio (100k limit)
		kvRatio := float64(acc.KVReadCount) / 100000.0
		fmt.Fprintf(&b, "cf_broker_quota_usage_ratio{account=\"%s\",resource=\"kv\"} %.4f\n", acc.ID, kvRatio)
	}
	b.WriteString("\n")

	// 5. Rate limit hits
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	b.WriteString("# HELP cf_broker_rate_limit_hits_total Number of rate limit rejections or 429 status encounters\n")
	b.WriteString("# TYPE cf_broker_rate_limit_hits_total counter\n")
	if len(mc.rateLimitHitsTotal) == 0 {
		b.WriteString("cf_broker_rate_limit_hits_total{account=\"none\"} 0\n")
	} else {
		keys := sortedKeys(mc.rateLimitHitsTotal)
		for _, acc := range keys {
			fmt.Fprintf(&b, "cf_broker_rate_limit_hits_total{account=\"%s\"} %d\n", acc, mc.rateLimitHitsTotal[acc])
		}
	}
	b.WriteString("\n")

	// 6. Failovers total
	b.WriteString("# HELP cf_broker_account_failovers_total Total failovers triggered per account\n")
	b.WriteString("# TYPE cf_broker_account_failovers_total counter\n")
	if len(mc.failoversTotal) == 0 {
		b.WriteString("cf_broker_account_failovers_total{account=\"none\"} 0\n")
	} else {
		keys := sortedKeys(mc.failoversTotal)
		for _, acc := range keys {
			fmt.Fprintf(&b, "cf_broker_account_failovers_total{account=\"%s\"} %d\n", acc, mc.failoversTotal[acc])
		}
	}
	b.WriteString("\n")

	// 7. Requests total
	b.WriteString("# HELP cf_broker_requests_total Total HTTP requests handled through broker gateway\n")
	b.WriteString("# TYPE cf_broker_requests_total counter\n")
	if len(mc.requestsTotal) == 0 {
		b.WriteString("cf_broker_requests_total{account=\"none\",service=\"none\",status=\"200\"} 0\n")
	} else {
		keys := sortedKeys(mc.requestsTotal)
		for _, k := range keys {
			parts := strings.Split(k, "|")
			if len(parts) == 3 {
				fmt.Fprintf(&b, "cf_broker_requests_total{service=\"%s\",status=\"%s\",account=\"%s\"} %d\n",
					parts[0], parts[1], parts[2], mc.requestsTotal[k])
			}
		}
	}
	b.WriteString("\n")

	// 8. Webhooks dispatched
	b.WriteString("# HELP cf_broker_webhooks_dispatched_total Total webhooks dispatched by channel and outcome\n")
	b.WriteString("# TYPE cf_broker_webhooks_dispatched_total counter\n")
	if len(mc.webhooksTotal) == 0 {
		b.WriteString("cf_broker_webhooks_dispatched_total{channel=\"none\",status=\"none\"} 0\n")
	} else {
		keys := sortedKeys(mc.webhooksTotal)
		for _, k := range keys {
			parts := strings.Split(k, "|")
			if len(parts) == 2 {
				fmt.Fprintf(&b, "cf_broker_webhooks_dispatched_total{channel=\"%s\",status=\"%s\"} %d\n",
					parts[0], parts[1], mc.webhooksTotal[k])
			}
		}
	}

	return b.String()
}

// ServeHTTP writes the Prometheus 0.0.4 exposition text to the client.
func (mc *MetricsCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(mc.FormatPrometheus()))
}

func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
