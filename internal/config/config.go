package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// DefaultConfigPath is the fallback location for tenant credentials configuration.
const DefaultConfigPath = "config.json"

// AccountConfig defines credentials and quota limits for a single pooled Cloudflare tenant.
type AccountConfig struct {
	ID          string  `json:"id"`
	Alias       string  `json:"alias"`
	Plan        string  `json:"plan"`
	WorkerLimit int64   `json:"worker_limit"`
	WorkerUsed  int64   `json:"worker_used,omitempty"`
	R2StorageMB float64 `json:"r2_storage_mb,omitempty"`
	KVReadCount int64   `json:"kv_read_count,omitempty"`
	APIToken    string  `json:"api_token"`
	SecretKey   string  `json:"secret_key,omitempty"`
	IsHealthy   *bool   `json:"is_healthy,omitempty"`
}

// ToAccountStatus converts AccountConfig into the broker's runtime AccountStatus.
func (a AccountConfig) ToAccountStatus() broker.AccountStatus {
	healthy := true
	if a.IsHealthy != nil {
		healthy = *a.IsHealthy
	}
	return broker.AccountStatus{
		ID:          a.ID,
		Alias:       a.Alias,
		Plan:        a.Plan,
		WorkerLimit: a.WorkerLimit,
		WorkerUsed:  a.WorkerUsed,
		R2StorageMB: a.R2StorageMB,
		KVReadCount: a.KVReadCount,
		IsHealthy:   healthy,
	}
}

// Config represents the application and credentials configuration schema.
type Config struct {
	ServerPort          string          `json:"server_port,omitempty"`
	Version             string          `json:"version,omitempty"`
	JWTSecret           string          `json:"jwt_secret,omitempty"`
	MasterKey           string          `json:"master_key,omitempty"`
	WebhookURL          string          `json:"webhook_url,omitempty"`
	DiscordWebhookURL   string          `json:"discord_webhook_url,omitempty"`
	SlackWebhookURL     string          `json:"slack_webhook_url,omitempty"`
	PollIntervalSeconds int             `json:"poll_interval_seconds,omitempty"`
	Accounts            []AccountConfig `json:"accounts"`
}

// Validate verifies that the configuration structure contains no logical inconsistencies.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("configuration cannot be nil")
	}
	if len(c.Accounts) == 0 {
		return errors.New("configuration must declare at least one account")
	}

	seen := make(map[string]bool)
	for i, acc := range c.Accounts {
		cleanID := strings.TrimSpace(acc.ID)
		if cleanID == "" {
			return fmt.Errorf("account at index %d has empty ID", i)
		}
		if seen[cleanID] {
			return fmt.Errorf("duplicate account ID '%s' detected", cleanID)
		}
		seen[cleanID] = true

		if acc.WorkerLimit < 0 {
			return fmt.Errorf("account '%s' has negative worker_limit: %d", cleanID, acc.WorkerLimit)
		}
		if acc.R2StorageMB < 0 {
			return fmt.Errorf("account '%s' has negative r2_storage_mb: %f", cleanID, acc.R2StorageMB)
		}
	}
	return nil
}

// Clone creates a deep copy of the configuration structure.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	cp := *c
	if c.Accounts != nil {
		cp.Accounts = make([]AccountConfig, len(c.Accounts))
		for i, acc := range c.Accounts {
			cp.Accounts[i] = acc
			if acc.IsHealthy != nil {
				h := *acc.IsHealthy
				cp.Accounts[i].IsHealthy = &h
			}
		}
	}
	return &cp
}

// ComputeHash calculates a deterministic SHA-256 hex digest of raw configuration bytes.
func ComputeHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ParseConfig decodes and validates a configuration from raw JSON bytes.
func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return &cfg, nil
}

// LoadConfigFile loads and parses a configuration file from the specified path.
func LoadConfigFile(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file '%s': %w", filePath, err)
	}
	return ParseConfig(data)
}

// DefaultConfig provides an out-of-the-box configuration for local development.
func DefaultConfig() *Config {
	t := true
	return &Config{
		ServerPort:          "9081",
		Version:             "0.6.0",
		PollIntervalSeconds: 2,
		Accounts: []AccountConfig{
			{
				ID:          "cf-tenant-alpha",
				Alias:       "Edge Cluster Primary",
				Plan:        "Free Tier (100k/day)",
				WorkerLimit: 100000,
				WorkerUsed:  14200,
				R2StorageMB: 340.5,
				KVReadCount: 8900,
				APIToken:    "cftok_cf-tenant-alpha_edge_production_token",
				IsHealthy:   &t,
			},
			{
				ID:          "cf-tenant-bravo",
				Alias:       "Edge Cluster Secondary",
				Plan:        "Free Tier (100k/day)",
				WorkerLimit: 100000,
				WorkerUsed:  3210,
				R2StorageMB: 120.0,
				KVReadCount: 1540,
				APIToken:    "cftok_cf-tenant-bravo_edge_production_token",
				IsHealthy:   &t,
			},
			{
				ID:          "cf-tenant-charlie",
				Alias:       "Edge Cluster Standby",
				Plan:        "Free Tier (100k/day)",
				WorkerLimit: 100000,
				WorkerUsed:  850,
				R2StorageMB: 50.0,
				KVReadCount: 420,
				APIToken:    "cftok_cf-tenant-charlie_edge_production_token",
				IsHealthy:   &t,
			},
		},
	}
}
