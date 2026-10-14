package quota

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

func TestSlidingWindowLimiterAllowAndDeny(t *testing.T) {
	// Window: 100ms, MaxRequests: 3, Burst: 1 -> Total capacity = 4
	limiter := NewSlidingWindowLimiter(100*time.Millisecond, 3, 1)

	// First 4 requests must pass
	for i := 1; i <= 4; i++ {
		allowed, count, _ := limiter.Allow("tenant-a")
		if !allowed {
			t.Fatalf("expected request %d to be allowed", i)
		}
		if count != int64(i) {
			t.Fatalf("expected count %d, got %d", i, count)
		}
	}

	// 5th request exceeds limit + burst, must be denied
	allowed, count, resetRemaining := limiter.Allow("tenant-a")
	if allowed {
		t.Fatalf("expected 5th request to be denied, but was allowed")
	}
	if count != 4 {
		t.Fatalf("expected count to stay at 4, got %d", count)
	}
	if resetRemaining <= 0 {
		t.Fatalf("expected positive reset remaining duration, got %v", resetRemaining)
	}

	// Wait for window to expire
	time.Sleep(120 * time.Millisecond)

	// Now a new request must be allowed
	allowedAfter, countAfter, _ := limiter.Allow("tenant-a")
	if !allowedAfter {
		t.Fatalf("expected request after window reset to be allowed")
	}
	if countAfter != 1 {
		t.Fatalf("expected count to reset to 1, got %d", countAfter)
	}
}

func TestSlidingWindowLimiterStatsAndPrune(t *testing.T) {
	limiter := NewSlidingWindowLimiter(200*time.Millisecond, 5, 2)

	limiter.AllowN("user-1", 3)
	stats := limiter.GetStats("user-1")
	if stats.CurrentCount != 3 {
		t.Fatalf("expected current count 3, got %d", stats.CurrentCount)
	}
	if stats.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", stats.Limit)
	}
	if stats.BurstRemaining != 2 {
		t.Fatalf("expected burst remaining 2, got %d", stats.BurstRemaining)
	}

	// Consume burst
	limiter.AllowN("user-1", 3) // total 6 -> uses 1 burst
	stats2 := limiter.GetStats("user-1")
	if stats2.BurstRemaining != 1 {
		t.Fatalf("expected burst remaining 1, got %d", stats2.BurstRemaining)
	}

	// Test PruneIdle
	time.Sleep(50 * time.Millisecond)
	pruned := limiter.PruneIdle(20 * time.Millisecond)
	if pruned != 1 {
		t.Fatalf("expected 1 key to be pruned, got %d", pruned)
	}
}

func TestSlidingWindowConcurrency(t *testing.T) {
	limiter := NewSlidingWindowLimiter(500*time.Millisecond, 100, 50)
	var wg sync.WaitGroup
	allowedCount := int64(0)
	var mu sync.Mutex

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				allowed, _, _ := limiter.Allow("concurrent-key")
				if allowed {
					mu.Lock()
					allowedCount++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if allowedCount != 150 {
		t.Fatalf("expected exactly 150 allowed requests under 150 capacity, got %d", allowedCount)
	}
}

func TestProbationLifecycleAndAutoRecovery(t *testing.T) {
	poolMgr := broker.NewPoolManager()
	pm := NewProbationManager(poolMgr)

	// Initially healthy
	state, remaining := pm.CheckState("cf-tenant-alpha")
	if state != StateHealthy || remaining != 0 {
		t.Fatalf("expected healthy state initially, got %s, remaining %v", state, remaining)
	}

	// Record failure with short base duration for testing
	pm.baseDuration = 50 * time.Millisecond
	pm.maxDuration = 200 * time.Millisecond
	dur := pm.RecordFailure("cf-tenant-alpha", "429 Too Many Requests")
	if dur != 50*time.Millisecond {
		t.Fatalf("expected 50ms probation duration, got %v", dur)
	}

	state, rem := pm.CheckState("cf-tenant-alpha")
	if state != StateProbation || rem <= 0 {
		t.Fatalf("expected probation state, got %s, rem %v", state, rem)
	}

	// Wait for probation to elapse
	time.Sleep(70 * time.Millisecond)

	// CheckState triggers auto-recovery
	stateAfter, remAfter := pm.CheckState("cf-tenant-alpha")
	if stateAfter != StateHealthy || remAfter != 0 {
		t.Fatalf("expected auto-recovered healthy state, got %s, remaining %v", stateAfter, remAfter)
	}

	// Manual Recover
	pm.RecordFailure("cf-tenant-bravo", "synthetic error")
	pm.Recover("cf-tenant-bravo")
	stateB, _ := pm.CheckState("cf-tenant-bravo")
	if stateB != StateHealthy {
		t.Fatalf("expected manual recover to set healthy, got %s", stateB)
	}

	// MarkExhausted
	pm.MarkExhausted("cf-tenant-charlie", "quota 100% exceeded")
	stateC, _ := pm.CheckState("cf-tenant-charlie")
	if stateC != StateExhausted {
		t.Fatalf("expected exhausted state, got %s", stateC)
	}
}

func TestProbationExponentialBackoff(t *testing.T) {
	poolMgr := broker.NewPoolManager()
	pm := NewProbationManager(poolMgr)
	pm.baseDuration = 10 * time.Millisecond
	pm.maxDuration = 80 * time.Millisecond
	pm.multiplier = 2.0

	// 1st failure: 10ms
	d1 := pm.RecordFailure("tenant-x", "fail 1")
	if d1 != 10*time.Millisecond {
		t.Fatalf("expected 10ms, got %v", d1)
	}

	// 2nd failure: 20ms
	d2 := pm.RecordFailure("tenant-x", "fail 2")
	if d2 != 20*time.Millisecond {
		t.Fatalf("expected 20ms, got %v", d2)
	}

	// 3rd failure: 40ms
	d3 := pm.RecordFailure("tenant-x", "fail 3")
	if d3 != 40*time.Millisecond {
		t.Fatalf("expected 40ms, got %v", d3)
	}

	// 4th failure: 80ms
	d4 := pm.RecordFailure("tenant-x", "fail 4")
	if d4 != 80*time.Millisecond {
		t.Fatalf("expected 80ms, got %v", d4)
	}

	// 5th failure: capped at 80ms
	d5 := pm.RecordFailure("tenant-x", "fail 5")
	if d5 != 80*time.Millisecond {
		t.Fatalf("expected capped at 80ms, got %v", d5)
	}
}

func TestProbationSweepExpired(t *testing.T) {
	poolMgr := broker.NewPoolManager()
	pm := NewProbationManager(poolMgr)
	pm.baseDuration = 30 * time.Millisecond

	pm.RecordFailure("cf-tenant-alpha", "rate limited")
	time.Sleep(50 * time.Millisecond)

	recovered := pm.SweepExpired()
	if len(recovered) != 1 || recovered[0] != "cf-tenant-alpha" {
		t.Fatalf("expected cf-tenant-alpha to be swept, got %v", recovered)
	}
}

func TestProbationHTTPHandlers(t *testing.T) {
	poolMgr := broker.NewPoolManager()
	pm := NewProbationManager(poolMgr)

	// 1. GET /api/quota/probation
	req1 := httptest.NewRequest(http.MethodGet, "/api/quota/probation", nil)
	w1 := httptest.NewRecorder()
	pm.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w1.Code)
	}

	// 2. POST /api/quota/probation/trigger?id=cf-tenant-alpha
	req2 := httptest.NewRequest(http.MethodPost, "/api/quota/probation/trigger?id=cf-tenant-alpha", nil)
	w2 := httptest.NewRecorder()
	pm.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w2.Code)
	}
	var trigResp map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &trigResp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if trigResp["status"] != "probation_triggered" {
		t.Fatalf("expected probation_triggered, got %v", trigResp["status"])
	}

	// 3. POST /api/quota/probation/recover?id=cf-tenant-alpha
	req3 := httptest.NewRequest(http.MethodPost, "/api/quota/probation/recover?id=cf-tenant-alpha", nil)
	w3 := httptest.NewRecorder()
	pm.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w3.Code)
	}
}

func TestNotifierThresholdEvaluationAndCooldown(t *testing.T) {
	notifier := NewQuotaNotifier("", "", "")

	// 1. Under 80% (75%) -> returns nil
	a1 := notifier.EvaluateThreshold("tenant-1", "Workers", 75000, 100000)
	if a1 != nil {
		t.Fatalf("expected no alert under 80%%, got %+v", a1)
	}

	// 2. 82% -> WARNING
	a2 := notifier.EvaluateThreshold("tenant-1", "Workers", 82000, 100000)
	if a2 == nil || a2.Level != "WARNING" {
		t.Fatalf("expected WARNING alert at 82%%, got %+v", a2)
	}

	// 3. Immediate repeat check -> suppressed by cooldown
	a2Repeat := notifier.EvaluateThreshold("tenant-1", "Workers", 83000, 100000)
	if a2Repeat != nil {
		t.Fatalf("expected duplicate warning to be suppressed by cooldown, got %+v", a2Repeat)
	}

	// 4. 96% -> CRITICAL (higher tier triggers)
	a3 := notifier.EvaluateThreshold("tenant-1", "Workers", 96000, 100000)
	if a3 == nil || a3.Level != "CRITICAL" {
		t.Fatalf("expected CRITICAL alert at 96%%, got %+v", a3)
	}

	// 5. 100% -> EXHAUSTED
	a4 := notifier.EvaluateThreshold("tenant-1", "Workers", 100000, 100000)
	if a4 == nil || a4.Level != "EXHAUSTED" {
		t.Fatalf("expected EXHAUSTED alert at 100%%, got %+v", a4)
	}

	// Check history
	history := notifier.GetHistory()
	if len(history) != 3 {
		t.Fatalf("expected 3 recorded alerts in history, got %d", len(history))
	}
}

func TestDiscordAndSlackPayloadFormatting(t *testing.T) {
	alert := QuotaAlert{
		ID:        "alt-test-1",
		Timestamp: time.Now().UTC(),
		AccountID: "cf-tenant-alpha",
		Resource:  "Workers Requests",
		Current:   95000,
		Limit:     100000,
		Percent:   95.0,
		Level:     "CRITICAL",
		Message:   "Workers usage reached 95%",
	}

	// Discord payload verification
	discordBytes, err := BuildDiscordPayload(alert)
	if err != nil {
		t.Fatalf("failed to build discord payload: %v", err)
	}
	var discordMap map[string]any
	if err := json.Unmarshal(discordBytes, &discordMap); err != nil {
		t.Fatalf("invalid discord json: %v", err)
	}
	embeds, ok := discordMap["embeds"].([]any)
	if !ok || len(embeds) == 0 {
		t.Fatalf("expected embeds array in discord payload")
	}

	// Slack payload verification
	slackBytes, err := BuildSlackPayload(alert)
	if err != nil {
		t.Fatalf("failed to build slack payload: %v", err)
	}
	var slackMap map[string]any
	if err := json.Unmarshal(slackBytes, &slackMap); err != nil {
		t.Fatalf("invalid slack json: %v", err)
	}
	blocks, ok := slackMap["blocks"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("expected blocks array in slack payload")
	}
}

func TestNotifierHTTPHandlers(t *testing.T) {
	notifier := NewQuotaNotifier("", "", "")

	// 1. GET /api/quota/webhooks/config
	req1 := httptest.NewRequest(http.MethodGet, "/api/quota/webhooks/config", nil)
	w1 := httptest.NewRecorder()
	notifier.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w1.Code)
	}

	// 2. POST /api/quota/webhooks/config
	cfgBody := `{"discord_url":"https://discord.com/api/webhooks/12345/abcdefg"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/quota/webhooks/config", strings.NewReader(cfgBody))
	w2 := httptest.NewRecorder()
	notifier.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w2.Code)
	}

	// 3. POST /api/quota/webhooks/test
	testBody := `{"account_id":"cf-tenant-alpha","resource":"Workers Requests","level":"WARNING","percent":82.5}`
	req3 := httptest.NewRequest(http.MethodPost, "/api/quota/webhooks/test", strings.NewReader(testBody))
	w3 := httptest.NewRecorder()
	notifier.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w3.Code)
	}

	// 4. GET /api/quota/alerts
	req4 := httptest.NewRequest(http.MethodGet, "/api/quota/alerts", nil)
	w4 := httptest.NewRecorder()
	notifier.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w4.Code)
	}
	var alerts []QuotaAlert
	if err := json.Unmarshal(w4.Body.Bytes(), &alerts); err != nil {
		t.Fatalf("invalid json alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 synthetic alert in history, got %d", len(alerts))
	}
}

func TestPrometheusMetricsExport(t *testing.T) {
	poolMgr := broker.NewPoolManager()
	leaseEng := broker.NewLeaseEngine(poolMgr)
	probation := NewProbationManager(poolMgr)
	limiter := NewSlidingWindowLimiter(time.Minute, 60, 10)

	metrics := NewMetricsCollector(poolMgr, leaseEng, probation, limiter)

	// Record some telemetry
	metrics.IncCumulativeLeases()
	metrics.RecordRequest("workers", "200", "cf-tenant-alpha")
	metrics.RecordRateLimitHit("cf-tenant-alpha")
	metrics.RecordFailover("cf-tenant-alpha")
	metrics.RecordWebhookDispatch("discord", "success")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on /metrics, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected text/plain content type, got %s", ct)
	}

	body := w.Body.String()

	requiredSubstrings := []string{
		"# HELP cf_broker_accounts_total",
		"# TYPE cf_broker_accounts_total gauge",
		"cf_broker_accounts_total{status=\"healthy\"}",
		"# HELP cf_broker_leases_active",
		"# HELP cf_broker_leases_total",
		"# HELP cf_broker_quota_usage_ratio",
		"# HELP cf_broker_rate_limit_hits_total",
		"# HELP cf_broker_account_failovers_total",
		"# HELP cf_broker_requests_total",
		"# HELP cf_broker_webhooks_dispatched_total",
		"cf_broker_requests_total{service=\"workers\",status=\"200\",account=\"cf-tenant-alpha\"} 1",
		"cf_broker_rate_limit_hits_total{account=\"cf-tenant-alpha\"} 1",
		"cf_broker_webhooks_dispatched_total{channel=\"discord\",status=\"success\"} 1",
	}

	for _, sub := range requiredSubstrings {
		if !strings.Contains(body, sub) {
			t.Fatalf("Prometheus output missing expected string: %s\nFull body:\n%s", sub, body)
		}
	}
}
