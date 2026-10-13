package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// AlertEvent describes an individual quota threshold trigger or failover alert.
type AlertEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	TenantID  string    `json:"tenant_id"`
	Metric    string    `json:"metric"`
	Current   float64   `json:"current"`
	Limit     float64   `json:"limit"`
	Percent   float64   `json:"percent"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// WebhookDispatcher manages quota threshold evaluations and delivers webhook alerts.
type WebhookDispatcher struct {
	mu         sync.RWMutex
	webhookURL string
	alerts     []AlertEvent
	cooldowns  map[string]time.Time // key: tenantID:metric:level
	client     *http.Client
}

// NewWebhookDispatcher creates a new alert dispatcher.
func NewWebhookDispatcher(webhookURL string) *WebhookDispatcher {
	return &WebhookDispatcher{
		webhookURL: webhookURL,
		alerts:     make([]AlertEvent, 0, 50),
		cooldowns:  make(map[string]time.Time),
		client:     &http.Client{Timeout: 5 * time.Second},
	}
}

// SetWebhookURL dynamically configures the outbound destination URL.
func (wd *WebhookDispatcher) SetWebhookURL(url string) {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	wd.webhookURL = url
}

// CheckAccount evaluates account quotas against 80%, 95%, and 100% threshold rules.
func (wd *WebhookDispatcher) CheckAccount(acc broker.AccountStatus) []AlertEvent {
	var triggered []AlertEvent

	// Check Worker Usage
	if acc.WorkerLimit > 0 {
		pct := (float64(acc.WorkerUsed) / float64(acc.WorkerLimit)) * 100.0
		if alert := wd.evaluateThreshold(acc.ID, "Workers Requests", float64(acc.WorkerUsed), float64(acc.WorkerLimit), pct); alert != nil {
			triggered = append(triggered, *alert)
		}
	}

	// Check R2 Storage (assume 10,000 MB / 10GB free tier boundary per account)
	const r2LimitMB = 10000.0
	r2Pct := (acc.R2StorageMB / r2LimitMB) * 100.0
	if alert := wd.evaluateThreshold(acc.ID, "R2 Storage (MB)", acc.R2StorageMB, r2LimitMB, r2Pct); alert != nil {
		triggered = append(triggered, *alert)
	}

	for _, a := range triggered {
		_ = wd.Dispatch(a)
	}

	return triggered
}

func (wd *WebhookDispatcher) evaluateThreshold(tenantID, metric string, cur, lim, pct float64) *AlertEvent {
	level := ""
	if pct >= 100.0 {
		level = "EXHAUSTED"
	} else if pct >= 95.0 {
		level = "CRITICAL"
	} else if pct >= 80.0 {
		level = "WARNING"
	}

	if level == "" {
		return nil
	}

	wd.mu.Lock()
	defer wd.mu.Unlock()

	cdKey := fmt.Sprintf("%s:%s:%s", tenantID, metric, level)
	lastSent, exists := wd.cooldowns[cdKey]
	if exists && time.Since(lastSent) < 10*time.Minute {
		return nil // Suppress duplicate notification during cooldown window
	}

	wd.cooldowns[cdKey] = time.Now()
	event := AlertEvent{
		ID:        fmt.Sprintf("alt_%d", time.Now().UnixNano()),
		Timestamp: time.Now().UTC(),
		TenantID:  tenantID,
		Metric:    metric,
		Current:   cur,
		Limit:     lim,
		Percent:   pct,
		Level:     level,
		Message:   fmt.Sprintf("[%s] %s on %s reached %.1f%% (%.1f / %.1f)", level, metric, tenantID, pct, cur, lim),
	}

	wd.alerts = append(wd.alerts, event)
	if len(wd.alerts) > 50 {
		wd.alerts = wd.alerts[1:]
	}

	return &event
}

// Dispatch posts formatted notifications to external endpoints (Discord/Slack compatible).
func (wd *WebhookDispatcher) Dispatch(alert AlertEvent) error {
	wd.mu.RLock()
	targetURL := wd.webhookURL
	wd.mu.RUnlock()

	if targetURL == "" {
		return nil // Webhook URL not set; stored in in-memory audit log
	}

	payload := map[string]interface{}{
		"text": alert.Message,
		"embeds": []map[string]interface{}{
			{
				"title":       fmt.Sprintf("Cloudflare Broker Quota Alert: %s", alert.Level),
				"description": alert.Message,
				"color":       15958048, // Amber/Orange
				"fields": []map[string]interface{}{
					{"name": "Tenant", "value": alert.TenantID, "inline": true},
					{"name": "Metric", "value": alert.Metric, "inline": true},
					{"name": "Usage", "value": fmt.Sprintf("%.1f%%", alert.Percent), "inline": true},
				},
				"timestamp": alert.Timestamp.Format(time.RFC3339),
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := wd.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook delivery failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
	}
	return nil
}

// RecentAlerts returns a slice of logged threshold notifications.
func (wd *WebhookDispatcher) RecentAlerts() []AlertEvent {
	wd.mu.RLock()
	defer wd.mu.RUnlock()

	res := make([]AlertEvent, len(wd.alerts))
	copy(res, wd.alerts)
	return res
}

// ServeHTTP provides the API endpoint for `/api/webhooks/*`.
func (wd *WebhookDispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.URL.Path == "/api/webhooks/test" && r.Method == http.MethodPost {
		testAlert := AlertEvent{
			ID:        fmt.Sprintf("alt_test_%d", time.Now().Unix()),
			Timestamp: time.Now().UTC(),
			TenantID:  "cf-tenant-alpha",
			Metric:    "Workers Requests",
			Current:   85000,
			Limit:     100000,
			Percent:   85.0,
			Level:     "WARNING",
			Message:   "[WARNING] Synthetic test alert generated via REST endpoint",
		}
		_ = wd.Dispatch(testAlert)
		wd.mu.Lock()
		wd.alerts = append(wd.alerts, testAlert)
		wd.mu.Unlock()

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "dispatched",
			"alert":  testAlert,
		})
		return
	}

	_ = json.NewEncoder(w).Encode(wd.RecentAlerts())
}
