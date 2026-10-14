package quota

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// QuotaAlert represents an evaluated threshold violation event.
type QuotaAlert struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	AccountID   string    `json:"account_id"`
	Resource    string    `json:"resource"`
	Current     float64   `json:"current"`
	Limit       float64   `json:"limit"`
	Percent     float64   `json:"percent"`
	Level       string    `json:"level"` // "WARNING" (80%), "CRITICAL" (95%), "EXHAUSTED" (100%)
	Message     string    `json:"message"`
	DeliveredTo []string  `json:"delivered_to,omitempty"`
}

// WebhookConfig contains outbound webhook targets.
type WebhookConfig struct {
	DiscordURL string `json:"discord_url,omitempty"`
	SlackURL   string `json:"slack_url,omitempty"`
	GenericURL string `json:"generic_url,omitempty"`
}

// QuotaNotifier manages multi-channel alert delivery with deduplication and cooldown.
type QuotaNotifier struct {
	mu         sync.RWMutex
	config     WebhookConfig
	history    []QuotaAlert
	cooldowns  map[string]time.Time
	httpClient *http.Client
}

// NewQuotaNotifier initializes a quota notifier with optional initial webhook URLs.
func NewQuotaNotifier(discordURL, slackURL, genericURL string) *QuotaNotifier {
	return &QuotaNotifier{
		config: WebhookConfig{
			DiscordURL: discordURL,
			SlackURL:   slackURL,
			GenericURL: genericURL,
		},
		history:    make([]QuotaAlert, 0, 100),
		cooldowns:  make(map[string]time.Time),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// UpdateWebhooks dynamically sets outbound notification destinations.
func (qn *QuotaNotifier) UpdateWebhooks(cfg WebhookConfig) {
	qn.mu.Lock()
	defer qn.mu.Unlock()
	qn.config = cfg
}

// GetConfig returns the active webhook configuration with sensitive tokens masked.
func (qn *QuotaNotifier) GetConfig() map[string]string {
	qn.mu.RLock()
	defer qn.mu.RUnlock()

	mask := func(u string) string {
		if u == "" {
			return ""
		}
		if len(u) <= 24 {
			return "configured (masked)"
		}
		return u[:16] + "..." + u[len(u)-6:]
	}

	return map[string]string{
		"discord_url": mask(qn.config.DiscordURL),
		"slack_url":   mask(qn.config.SlackURL),
		"generic_url": mask(qn.config.GenericURL),
	}
}

// EvaluateThreshold inspects resource usage against 80%, 95%, and 100% boundary rules.
func (qn *QuotaNotifier) EvaluateThreshold(accountID, resource string, current, limit float64) *QuotaAlert {
	if limit <= 0 {
		return nil
	}

	pct := (current / limit) * 100.0
	var level string
	if pct >= 100.0 {
		level = "EXHAUSTED"
	} else if pct >= 95.0 {
		level = "CRITICAL"
	} else if pct >= 80.0 {
		level = "WARNING"
	} else {
		return nil
	}

	qn.mu.Lock()
	defer qn.mu.Unlock()

	cdKey := fmt.Sprintf("%s:%s:%s", accountID, resource, level)
	lastSent, exists := qn.cooldowns[cdKey]
	if exists && time.Since(lastSent) < 10*time.Minute {
		return nil // Suppress duplicate alert during cooldown
	}
	qn.cooldowns[cdKey] = time.Now().UTC()

	alert := QuotaAlert{
		ID:        fmt.Sprintf("alert_%d", time.Now().UnixNano()),
		Timestamp: time.Now().UTC(),
		AccountID: accountID,
		Resource:  resource,
		Current:   current,
		Limit:     limit,
		Percent:   pct,
		Level:     level,
		Message:   fmt.Sprintf("[%s] %s on %s reached %.1f%% (%.1f / %.1f)", level, resource, accountID, pct, current, limit),
	}

	delivered := qn.dispatchSync(alert)
	alert.DeliveredTo = delivered

	qn.history = append(qn.history, alert)
	if len(qn.history) > 100 {
		qn.history = qn.history[1:]
	}

	return &alert
}

// CheckAccount audits all resource vectors (Workers, R2, KV) for a pooled account.
func (qn *QuotaNotifier) CheckAccount(acc broker.AccountStatus) []QuotaAlert {
	var triggered []QuotaAlert

	// 1. Worker daily requests
	if acc.WorkerLimit > 0 {
		if a := qn.EvaluateThreshold(acc.ID, "Workers Requests", float64(acc.WorkerUsed), float64(acc.WorkerLimit)); a != nil {
			triggered = append(triggered, *a)
		}
	}

	// 2. R2 Storage (10 GB free tier boundary)
	const r2LimitMB = 10000.0
	if a := qn.EvaluateThreshold(acc.ID, "R2 Storage (MB)", acc.R2StorageMB, r2LimitMB); a != nil {
		triggered = append(triggered, *a)
	}

	// 3. KV Read Operations (100k free tier boundary)
	const kvReadLimit = 100000.0
	if a := qn.EvaluateThreshold(acc.ID, "KV Read Count", float64(acc.KVReadCount), kvReadLimit); a != nil {
		triggered = append(triggered, *a)
	}

	return triggered
}

func (qn *QuotaNotifier) dispatchSync(alert QuotaAlert) []string {
	var channels []string

	// Discord Webhook Dispatch
	if qn.config.DiscordURL != "" {
		if err := qn.sendDiscord(qn.config.DiscordURL, alert); err == nil {
			channels = append(channels, "discord")
		}
	}

	// Slack Webhook Dispatch
	if qn.config.SlackURL != "" {
		if err := qn.sendSlack(qn.config.SlackURL, alert); err == nil {
			channels = append(channels, "slack")
		}
	}

	// Generic JSON Webhook Dispatch
	if qn.config.GenericURL != "" {
		if err := qn.sendGeneric(qn.config.GenericURL, alert); err == nil {
			channels = append(channels, "generic")
		}
	}

	return channels
}

// BuildDiscordPayload formats a Discord embed message for quota alerts.
func BuildDiscordPayload(alert QuotaAlert) ([]byte, error) {
	// Color codes: Amber for 80%, Red for 95%, Dark Red for 100%
	color := 15105570 // Orange
	if alert.Level == "CRITICAL" {
		color = 15158332 // Red
	} else if alert.Level == "EXHAUSTED" {
		color = 10038562 // Dark Red
	}

	payload := map[string]any{
		"username":   "Cloudflare Lease Broker",
		"avatar_url": "https://raw.githubusercontent.com/kLauncher1/cf-lease-broker/main/web/public/cf-logo.png",
		"embeds": []map[string]any{
			{
				"title":       fmt.Sprintf("⚠️ Quota %s Threshold Triggered", alert.Level),
				"description": alert.Message,
				"color":       color,
				"fields": []map[string]any{
					{"name": "Account", "value": alert.AccountID, "inline": true},
					{"name": "Resource", "value": alert.Resource, "inline": true},
					{"name": "Utilization", "value": fmt.Sprintf("%.1f%%", alert.Percent), "inline": true},
					{"name": "Current / Limit", "value": fmt.Sprintf("%.0f / %.0f", alert.Current, alert.Limit), "inline": false},
				},
				"footer": map[string]any{
					"text": "CF-Lease-Broker Quota Guardian",
				},
				"timestamp": alert.Timestamp.Format(time.RFC3339),
			},
		},
	}
	return json.Marshal(payload)
}

// BuildSlackPayload formats a Slack Block Kit message for quota alerts.
func BuildSlackPayload(alert QuotaAlert) ([]byte, error) {
	emoji := "⚠️"
	if alert.Level == "CRITICAL" {
		emoji = "🚨"
	} else if alert.Level == "EXHAUSTED" {
		emoji = "🛑"
	}

	payload := map[string]any{
		"text": fmt.Sprintf("%s [%s] Quota alert on %s: %s", emoji, alert.Level, alert.AccountID, alert.Message),
		"blocks": []map[string]any{
			{
				"type": "header",
				"text": map[string]any{
					"type": "plain_text",
					"text": fmt.Sprintf("%s Cloudflare Quota Alert: %s", emoji, alert.Level),
				},
			},
			{
				"type": "section",
				"fields": []map[string]any{
					{"type": "mrkdwn", "text": fmt.Sprintf("*Account:*\n`%s`", alert.AccountID)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Resource:*\n%s", alert.Resource)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Usage:*\n*%.1f%%*", alert.Percent)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Current / Limit:*\n%.0f / %.0f", alert.Current, alert.Limit)},
				},
			},
			{
				"type": "context",
				"elements": []map[string]any{
					{"type": "mrkdwn", "text": fmt.Sprintf("Triggered at %s · Automated Lease Broker", alert.Timestamp.Format(time.RFC1123))},
				},
			},
		},
	}
	return json.Marshal(payload)
}

func (qn *QuotaNotifier) sendDiscord(url string, alert QuotaAlert) error {
	body, err := BuildDiscordPayload(alert)
	if err != nil {
		return err
	}
	return qn.postJSON(url, body)
}

func (qn *QuotaNotifier) sendSlack(url string, alert QuotaAlert) error {
	body, err := BuildSlackPayload(alert)
	if err != nil {
		return err
	}
	return qn.postJSON(url, body)
}

func (qn *QuotaNotifier) sendGeneric(url string, alert QuotaAlert) error {
	body, err := json.Marshal(alert)
	if err != nil {
		return err
	}
	return qn.postJSON(url, body)
}

func (qn *QuotaNotifier) postJSON(url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := qn.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook responded with HTTP %d", resp.StatusCode)
	}
	return nil
}

// GetHistory returns copies of all recorded alert events.
func (qn *QuotaNotifier) GetHistory() []QuotaAlert {
	qn.mu.RLock()
	defer qn.mu.RUnlock()

	res := make([]QuotaAlert, len(qn.history))
	copy(res, qn.history)
	return res
}

// TriggerSyntheticAlert dispatches a mock alert for integration and dashboard testing.
func (qn *QuotaNotifier) TriggerSyntheticAlert(accountID, resource, level string, percent float64) QuotaAlert {
	qn.mu.Lock()
	defer qn.mu.Unlock()

	alert := QuotaAlert{
		ID:        fmt.Sprintf("alert_sim_%d", time.Now().UnixNano()),
		Timestamp: time.Now().UTC(),
		AccountID: accountID,
		Resource:  resource,
		Current:   percent * 1000,
		Limit:     100000,
		Percent:   percent,
		Level:     level,
		Message:   fmt.Sprintf("[%s] Synthetic %s threshold simulation on %s (%.1f%%)", level, resource, accountID, percent),
	}

	delivered := qn.dispatchSync(alert)
	alert.DeliveredTo = delivered

	qn.history = append(qn.history, alert)
	if len(qn.history) > 100 {
		qn.history = qn.history[1:]
	}

	return alert
}

// ServeHTTP provides the REST management API for `/api/quota/webhooks/*` and `/api/quota/alerts`.
func (qn *QuotaNotifier) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.URL.Path == "/api/quota/alerts" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(qn.GetHistory())

	case r.URL.Path == "/api/quota/webhooks/config" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(qn.GetConfig())

	case r.URL.Path == "/api/quota/webhooks/config" && r.Method == http.MethodPost:
		var cfg WebhookConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		qn.UpdateWebhooks(cfg)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "updated",
			"config": qn.GetConfig(),
		})

	case r.URL.Path == "/api/quota/webhooks/test" && r.Method == http.MethodPost:
		var req struct {
			AccountID string  `json:"account_id"`
			Resource  string  `json:"resource"`
			Level     string  `json:"level"`
			Percent   float64 `json:"percent"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.AccountID == "" {
			req.AccountID = "cf-tenant-alpha"
		}
		if req.Resource == "" {
			req.Resource = "Workers Requests"
		}
		if req.Level == "" {
			req.Level = "WARNING"
		}
		if req.Percent <= 0 {
			req.Percent = 85.0
		}

		alert := qn.TriggerSyntheticAlert(req.AccountID, req.Resource, req.Level, req.Percent)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "dispatched",
			"alert":  alert,
		})

	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}
