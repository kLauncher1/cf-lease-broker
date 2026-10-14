package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
	"github.com/kLauncher1/cf-lease-broker/internal/security"
)

func TestDefaultConfigValidation(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig() should pass validation, got: %v", err)
	}

	if len(cfg.Accounts) != 3 {
		t.Fatalf("expected 3 default accounts, got %d", len(cfg.Accounts))
	}

	for _, acc := range cfg.Accounts {
		st := acc.ToAccountStatus()
		if st.ID != acc.ID {
			t.Errorf("expected status ID %s, got %s", acc.ID, st.ID)
		}
		if !st.IsHealthy {
			t.Errorf("expected default account %s to be healthy", acc.ID)
		}
	}
}

func TestParseConfigValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "invalid json",
			json:    `{invalid-json`,
			wantErr: "failed to parse config JSON",
		},
		{
			name:    "empty accounts list",
			json:    `{"accounts":[]}`,
			wantErr: "declare at least one account",
		},
		{
			name:    "empty account ID",
			json:    `{"accounts":[{"id":"","alias":"test","worker_limit":1000}]}`,
			wantErr: "empty ID",
		},
		{
			name:    "duplicate account ID",
			json:    `{"accounts":[{"id":"dup","alias":"a1","worker_limit":1000},{"id":"dup","alias":"a2","worker_limit":2000}]}`,
			wantErr: "duplicate account ID",
		},
		{
			name:    "negative worker limit",
			json:    `{"accounts":[{"id":"acc-1","alias":"a1","worker_limit":-10}]}`,
			wantErr: "negative worker_limit",
		},
		{
			name:    "negative r2 storage",
			json:    `{"accounts":[{"id":"acc-1","alias":"a1","worker_limit":100,"r2_storage_mb":-5.5}]}`,
			wantErr: "negative r2_storage_mb",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tc.json))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !containsSubstring(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestConfigCloneIsolation(t *testing.T) {
	orig := DefaultConfig()
	cloned := orig.Clone()

	if cloned == nil {
		t.Fatal("cloned config is nil")
	}

	// Mutate cloned values
	cloned.ServerPort = "9999"
	cloned.Accounts[0].ID = "mutated-id"
	f := false
	cloned.Accounts[0].IsHealthy = &f

	// Verify original is untouched
	if orig.ServerPort == "9999" {
		t.Errorf("mutating clone modified original ServerPort")
	}
	if orig.Accounts[0].ID == "mutated-id" {
		t.Errorf("mutating clone modified original Account ID")
	}
	if *orig.Accounts[0].IsHealthy != true {
		t.Errorf("mutating clone modified original IsHealthy")
	}
}

func TestHotReloadManualTrigger(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialCfg := DefaultConfig()
	if err := WriteInitialConfig(configPath, initialCfg); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	hr := NewHotReloader(configPath, initialCfg, time.Hour)

	var reloadTriggered bool
	var newAccountsCount int
	hr.OnReload(func(oldCfg, newCfg *Config) {
		reloadTriggered = true
		newAccountsCount = len(newCfg.Accounts)
	})

	// Add a 4th account to disk
	updatedCfg := initialCfg.Clone()
	tVal := true
	updatedCfg.Accounts = append(updatedCfg.Accounts, AccountConfig{
		ID:          "cf-tenant-delta",
		Alias:       "Edge Cluster Canary",
		Plan:        "Enterprise (1M/day)",
		WorkerLimit: 1000000,
		APIToken:    "cftok_cf-tenant-delta_canary",
		IsHealthy:   &tVal,
	})

	rawUpdated, err := json.Marshal(updatedCfg)
	if err != nil {
		t.Fatalf("failed to marshal updated config: %v", err)
	}
	if err := os.WriteFile(configPath, rawUpdated, 0600); err != nil {
		t.Fatalf("failed to write updated config file: %v", err)
	}

	// Trigger manual reload
	reloaded, err := hr.Reload()
	if err != nil {
		t.Fatalf("manual reload failed: %v", err)
	}

	if !reloadTriggered {
		t.Errorf("expected OnReload callback to be executed")
	}
	if newAccountsCount != 4 {
		t.Errorf("expected 4 accounts in callback, got %d", newAccountsCount)
	}
	if len(reloaded.Accounts) != 4 {
		t.Errorf("expected 4 accounts in reloaded config, got %d", len(reloaded.Accounts))
	}
	if hr.GetStats().ReloadCount != 1 {
		t.Errorf("expected reload_count to be 1, got %d", hr.GetStats().ReloadCount)
	}
}

func TestHotReloadFileWatcherPolling(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialCfg := DefaultConfig()
	if err := WriteInitialConfig(configPath, initialCfg); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// 50ms fast polling for test
	hr := NewHotReloader(configPath, initialCfg, 50*time.Millisecond)

	reloadCh := make(chan struct{}, 1)
	hr.OnReload(func(oldCfg, newCfg *Config) {
		select {
		case reloadCh <- struct{}{}:
		default:
		}
	})

	if err := hr.Start(); err != nil {
		t.Fatalf("failed to start hot reloader: %v", err)
	}
	defer hr.Stop()

	// Update config on disk
	time.Sleep(100 * time.Millisecond)
	updatedCfg := initialCfg.Clone()
	updatedCfg.Accounts[0].WorkerLimit = 250000
	rawUpdated, err := json.Marshal(updatedCfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, rawUpdated, 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Wait for automatic poll detection
	select {
	case <-reloadCh:
		// Reload succeeded via background watcher
		active := hr.GetConfig()
		if active.Accounts[0].WorkerLimit != 250000 {
			t.Errorf("expected reloaded limit 250000, got %d", active.Accounts[0].WorkerLimit)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background file watcher reload")
	}
}

func TestHotReloadInvalidFilePreservesActiveConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialCfg := DefaultConfig()
	if err := WriteInitialConfig(configPath, initialCfg); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	hr := NewHotReloader(configPath, initialCfg, time.Hour)

	var errorCallbackFired bool
	hr.OnError(func(err error) {
		errorCallbackFired = true
	})

	// Overwrite with corrupted JSON syntax
	if err := os.WriteFile(configPath, []byte(`{corrupt_json:!!`), 0600); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	_, err := hr.Reload()
	if err == nil {
		t.Fatal("expected reload to fail with corrupt JSON, got nil")
	}

	if !errorCallbackFired {
		t.Errorf("expected OnError callback to fire")
	}

	// Zero-downtime invariant: active config must remain intact!
	active := hr.GetConfig()
	if active == nil || len(active.Accounts) != 3 {
		t.Fatalf("active configuration was corrupted after failed reload attempt!")
	}
	if hr.GetStats().FailedReloads != 1 {
		t.Errorf("expected failed_reloads 1, got %d", hr.GetStats().FailedReloads)
	}
}

func TestHotReloadPoolAndVaultSync(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	poolMgr := broker.NewPoolManager()
	vault := security.NewSecretVault()

	initialCfg := DefaultConfig()
	if err := WriteInitialConfig(configPath, initialCfg); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	hr := NewHotReloader(configPath, initialCfg, time.Hour)

	// Wire sync callback
	hr.OnReload(func(oldCfg, newCfg *Config) {
		var statuses []broker.AccountStatus
		for _, acc := range newCfg.Accounts {
			statuses = append(statuses, acc.ToAccountStatus())
			if acc.APIToken != "" {
				_ = vault.StoreCredential(acc.ID, []byte(acc.APIToken))
			}
		}
		poolMgr.SyncAccounts(statuses)
	})

	// Add brand-new account with token
	updatedCfg := initialCfg.Clone()
	tVal := true
	updatedCfg.Accounts = append(updatedCfg.Accounts, AccountConfig{
		ID:          "cf-tenant-delta",
		Alias:       "Canary Delta",
		Plan:        "Enterprise Tier",
		WorkerLimit: 500000,
		APIToken:    "cftok_super_secret_delta_token",
		IsHealthy:   &tVal,
	})

	data, _ := json.Marshal(updatedCfg)
	_ = os.WriteFile(configPath, data, 0600)

	if _, err := hr.Reload(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	// Verify PoolManager received new account
	deltaAcc, err := poolMgr.GetAccount("cf-tenant-delta")
	if err != nil {
		t.Fatalf("expected cf-tenant-delta in pool manager: %v", err)
	}
	if deltaAcc.WorkerLimit != 500000 {
		t.Errorf("expected worker limit 500000, got %d", deltaAcc.WorkerLimit)
	}

	// Verify SecretVault stored and can decrypt credential
	decryptedSecret, err := vault.RetrieveDecryptedSecret("cf-tenant-delta")
	if err != nil {
		t.Fatalf("failed to retrieve credential from vault: %v", err)
	}
	defer security.Zeroize(decryptedSecret)

	if string(decryptedSecret) != "cftok_super_secret_delta_token" {
		t.Errorf("expected decrypted secret 'cftok_super_secret_delta_token', got '%s'", string(decryptedSecret))
	}
}

func TestHotReloadHTTPHandlers(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialCfg := DefaultConfig()
	_ = WriteInitialConfig(configPath, initialCfg)

	hr := NewHotReloader(configPath, initialCfg, time.Hour)

	// 1. GET /api/config/status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/config/status", nil)
	rrStatus := httptest.NewRecorder()
	hr.ServeHTTP(rrStatus, reqStatus)

	if rrStatus.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET /api/config/status, got %d", rrStatus.Code)
	}

	var statusResp struct {
		Stats    HotReloadStats     `json:"stats"`
		Accounts []SanitizedAccount `json:"accounts"`
	}
	if err := json.Unmarshal(rrStatus.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("failed to unmarshal status JSON: %v", err)
	}
	if len(statusResp.Accounts) != 3 {
		t.Errorf("expected 3 sanitized accounts, got %d", len(statusResp.Accounts))
	}
	// Verify raw tokens are NOT in response (sanitization check)
	rawBody := rrStatus.Body.String()
	if containsSubstring(rawBody, "cftok_") {
		t.Errorf("security violation: raw api_token leaked in HTTP status response: %s", rawBody)
	}

	// 2. POST /api/config/reload
	reqReload := httptest.NewRequest(http.MethodPost, "/api/config/reload", nil)
	rrReload := httptest.NewRecorder()
	hr.ServeHTTP(rrReload, reqReload)

	if rrReload.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for POST /api/config/reload, got %d", rrReload.Code)
	}

	// 3. POST /api/config/reload with broken file
	_ = os.WriteFile(configPath, []byte(`{invalid`), 0600)
	reqReloadBad := httptest.NewRequest(http.MethodPost, "/api/config/reload", nil)
	rrReloadBad := httptest.NewRecorder()
	hr.ServeHTTP(rrReloadBad, reqReloadBad)

	if rrReloadBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid reload, got %d", rrReloadBad.Code)
	}

	// 4. Unknown endpoint
	req404 := httptest.NewRequest(http.MethodGet, "/api/config/unknown", nil)
	rr404 := httptest.NewRecorder()
	hr.ServeHTTP(rr404, req404)

	if rr404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", rr404.Code)
	}
}

func TestHotReloaderConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialCfg := DefaultConfig()
	_ = WriteInitialConfig(configPath, initialCfg)

	hr := NewHotReloader(configPath, initialCfg, time.Hour)

	var wg sync.WaitGroup
	workers := 20
	iterations := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = hr.GetConfig()
				_ = hr.GetStats()
				if j%10 == 0 {
					_, _ = hr.Reload()
				}
			}
		}(i)
	}

	wg.Wait()
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && searchSubstr(s, sub)))
}

func searchSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
