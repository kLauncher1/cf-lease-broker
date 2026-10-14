package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ReloadCallback is triggered whenever a valid configuration hot reload occurs.
type ReloadCallback func(oldCfg, newCfg *Config)

// ReloadErrorCallback is triggered when a reload attempt fails validation or I/O.
type ReloadErrorCallback func(err error)

// HotReloadStats conveys operational telemetry regarding configuration reloads.
type HotReloadStats struct {
	ConfigPath          string    `json:"config_path"`
	ReloadCount         int64     `json:"reload_count"`
	FailedReloads       int64     `json:"failed_reloads"`
	LastReloadAt        time.Time `json:"last_reload_at,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
	AccountsCount       int       `json:"accounts_count"`
	PollIntervalSeconds float64   `json:"poll_interval_seconds"`
	IsRunning           bool      `json:"is_running"`
}

// SanitizedAccount represents account metadata safe for export over HTTP.
type SanitizedAccount struct {
	ID          string  `json:"id"`
	Alias       string  `json:"alias"`
	Plan        string  `json:"plan"`
	WorkerLimit int64   `json:"worker_limit"`
	WorkerUsed  int64   `json:"worker_used"`
	R2StorageMB float64 `json:"r2_storage_mb"`
	KVReadCount int64   `json:"kv_read_count"`
	IsHealthy   bool    `json:"is_healthy"`
	HasToken    bool    `json:"has_token"`
}

// HotReloader provides zero-downtime hot reloading of credentials and configuration.
type HotReloader struct {
	mu             sync.RWMutex
	configPath     string
	activeConfig   *Config
	lastModTime    time.Time
	lastHash       string
	lastReloadAt   time.Time
	reloadCount    int64
	failedReloads  int64
	lastError      string
	pollInterval   time.Duration
	callbacks      []ReloadCallback
	errorCallbacks []ReloadErrorCallback
	stopChan       chan struct{}
	sigChan        chan os.Signal
	running        bool
}

// NewHotReloader initializes a hot reloader with an optional initial config and polling interval.
func NewHotReloader(configPath string, initial *Config, pollInterval time.Duration) *HotReloader {
	if configPath == "" {
		configPath = DefaultConfigPath
	}
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	if initial == nil {
		initial = DefaultConfig()
	}

	hr := &HotReloader{
		configPath:   configPath,
		activeConfig: initial.Clone(),
		pollInterval: pollInterval,
		stopChan:     make(chan struct{}),
	}

	// If file exists at instantiation, attempt to load initial state from disk
	if info, err := os.Stat(configPath); err == nil {
		if data, err := os.ReadFile(configPath); err == nil {
			if parsed, err := ParseConfig(data); err == nil {
				hr.activeConfig = parsed
				hr.lastModTime = info.ModTime()
				hr.lastHash = ComputeHash(data)
			}
		}
	}

	return hr
}

// OnReload registers a callback invoked whenever the configuration changes.
func (hr *HotReloader) OnReload(cb ReloadCallback) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.callbacks = append(hr.callbacks, cb)
}

// OnError registers a callback invoked whenever a reload attempt fails.
func (hr *HotReloader) OnError(cb ReloadErrorCallback) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.errorCallbacks = append(hr.errorCallbacks, cb)
}

// GetConfig returns a deep copy of the active configuration.
func (hr *HotReloader) GetConfig() *Config {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return hr.activeConfig.Clone()
}

// Start begins background file system polling and OS signal listening.
func (hr *HotReloader) Start() error {
	hr.mu.Lock()
	if hr.running {
		hr.mu.Unlock()
		return errors.New("hot reloader is already running")
	}
	hr.running = true
	hr.stopChan = make(chan struct{})
	hr.mu.Unlock()

	// 1. File modification polling loop
	go func() {
		ticker := time.NewTicker(hr.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-hr.stopChan:
				return
			case <-ticker.C:
				_, _ = hr.checkFileAndReload(false)
			}
		}
	}()

	// 2. OS Signal listener loop (e.g., SIGHUP on POSIX)
	sigs := reloadSignals()
	if len(sigs) > 0 {
		hr.sigChan = make(chan os.Signal, 1)
		signal.Notify(hr.sigChan, sigs...)

		go func() {
			for {
				select {
				case <-hr.stopChan:
					signal.Stop(hr.sigChan)
					return
				case <-hr.sigChan:
					_, _ = hr.Reload()
				}
			}
		}()
	}

	return nil
}

// Stop cleanly terminates the background watcher and signal listener routines.
func (hr *HotReloader) Stop() {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	if !hr.running {
		return
	}
	hr.running = false
	close(hr.stopChan)
	if hr.sigChan != nil {
		signal.Stop(hr.sigChan)
	}
}

// Reload performs an immediate synchronous check and reload of the configuration file.
func (hr *HotReloader) Reload() (*Config, error) {
	return hr.checkFileAndReload(true)
}

// checkFileAndReload checks if the file has changed and atomically applies the new configuration.
func (hr *HotReloader) checkFileAndReload(force bool) (*Config, error) {
	hr.mu.RLock()
	filePath := hr.configPath
	lastHash := hr.lastHash
	lastMod := hr.lastModTime
	hr.mu.RUnlock()

	info, err := os.Stat(filePath)
	if err != nil {
		if !force {
			return nil, nil // Silently ignore missing file during background polling
		}
		reloadErr := fmt.Errorf("configuration file not found: %w", err)
		hr.recordFailure(reloadErr)
		return nil, reloadErr
	}

	// Skip if modification time has not advanced and not forcing
	if !force && !info.ModTime().After(lastMod) {
		return nil, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		reloadErr := fmt.Errorf("failed to read configuration file: %w", err)
		hr.recordFailure(reloadErr)
		return nil, reloadErr
	}

	newHash := ComputeHash(data)
	if !force && newHash == lastHash {
		hr.mu.Lock()
		hr.lastModTime = info.ModTime()
		hr.mu.Unlock()
		return nil, nil
	}

	newCfg, err := ParseConfig(data)
	if err != nil {
		reloadErr := fmt.Errorf("failed to validate reloaded config: %w", err)
		hr.recordFailure(reloadErr)
		return nil, reloadErr
	}

	hr.mu.Lock()
	oldCfg := hr.activeConfig
	hr.activeConfig = newCfg
	hr.lastModTime = info.ModTime()
	hr.lastHash = newHash
	hr.lastReloadAt = time.Now().UTC()
	hr.reloadCount++
	hr.lastError = ""
	cbs := make([]ReloadCallback, len(hr.callbacks))
	copy(cbs, hr.callbacks)
	hr.mu.Unlock()

	// Invoke callbacks outside the critical section to prevent deadlock
	for _, cb := range cbs {
		cb(oldCfg, newCfg)
	}

	return newCfg.Clone(), nil
}

// recordFailure records an error occurrence and dispatches registered error callbacks.
func (hr *HotReloader) recordFailure(err error) {
	hr.mu.Lock()
	hr.failedReloads++
	hr.lastError = err.Error()
	errCbs := make([]ReloadErrorCallback, len(hr.errorCallbacks))
	copy(errCbs, hr.errorCallbacks)
	hr.mu.Unlock()

	for _, cb := range errCbs {
		cb(err)
	}
}

// GetStats returns telemetry metrics on reload frequency, failures, and active accounts.
func (hr *HotReloader) GetStats() HotReloadStats {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	accCount := 0
	if hr.activeConfig != nil {
		accCount = len(hr.activeConfig.Accounts)
	}

	return HotReloadStats{
		ConfigPath:          hr.configPath,
		ReloadCount:         hr.reloadCount,
		FailedReloads:       hr.failedReloads,
		LastReloadAt:        hr.lastReloadAt,
		LastError:           hr.lastError,
		AccountsCount:       accCount,
		PollIntervalSeconds: hr.pollInterval.Seconds(),
		IsRunning:           hr.running,
	}
}

// ServeHTTP provides REST diagnostic endpoints under /api/config/*.
func (hr *HotReloader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/config")
	path = strings.TrimPrefix(path, "/")
	w.Header().Set("Content-Type", "application/json")

	switch {
	case (path == "status" || path == "") && r.Method == http.MethodGet:
		stats := hr.GetStats()
		cfg := hr.GetConfig()

		var accounts []SanitizedAccount
		if cfg != nil {
			for _, acc := range cfg.Accounts {
				h := true
				if acc.IsHealthy != nil {
					h = *acc.IsHealthy
				}
				accounts = append(accounts, SanitizedAccount{
					ID:          acc.ID,
					Alias:       acc.Alias,
					Plan:        acc.Plan,
					WorkerLimit: acc.WorkerLimit,
					WorkerUsed:  acc.WorkerUsed,
					R2StorageMB: acc.R2StorageMB,
					KVReadCount: acc.KVReadCount,
					IsHealthy:   h,
					HasToken:    acc.APIToken != "",
				})
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"stats":    stats,
			"accounts": accounts,
		})

	case (path == "reload") && r.Method == http.MethodPost:
		newCfg, err := hr.Reload()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "reload_failed",
				"error":  err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "reloaded",
			"reload_count": hr.GetStats().ReloadCount,
			"accounts":     len(newCfg.Accounts),
		})

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"endpoint not found"}`))
	}
}

// WriteInitialConfig persists a default configuration file if the target does not exist.
func WriteInitialConfig(targetPath string, cfg *Config) error {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil && !os.IsExist(err) {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0600)
}
