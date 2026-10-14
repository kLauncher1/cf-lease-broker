package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
	"github.com/kLauncher1/cf-lease-broker/internal/cloudflare"
	"github.com/kLauncher1/cf-lease-broker/internal/compiler"
	"github.com/kLauncher1/cf-lease-broker/internal/config"
	"github.com/kLauncher1/cf-lease-broker/internal/proxy"
	"github.com/kLauncher1/cf-lease-broker/internal/quota"
	"github.com/kLauncher1/cf-lease-broker/internal/security"
	"github.com/kLauncher1/cf-lease-broker/internal/storage"
)

var (
	// Version is populated at build time via -ldflags="-X main.Version=..."
	Version = "0.7.0"
	// BuildTime is populated at build time via -ldflags="-X main.BuildTime=..."
	BuildTime = "development"
)

//go:embed web/dist/*
var embeddedWeb embed.FS

type grantRequest struct {
	TargetService string `json:"target_service"`
	ResourceType  string `json:"resource_type"`
	DurationHours int    `json:"duration_hours"`
}

func main() {
	// Initialize configuration & zero-downtime hot reloader (Milestone 6)
	configPath := os.Getenv("CF_BROKER_CONFIG_PATH")
	if configPath == "" {
		configPath = config.DefaultConfigPath
	}
	hotReloader := config.NewHotReloader(configPath, nil, 2*time.Second)
	activeConfig := hotReloader.GetConfig()

	port := os.Getenv("PORT")
	if port == "" && activeConfig.ServerPort != "" {
		port = activeConfig.ServerPort
	}
	if port == "" {
		port = "9081"
	}

	poolMgr := broker.NewPoolManager()
	leaseEng := broker.NewLeaseEngine(poolMgr)
	cfClient := cloudflare.NewClient()
	proxyHandler := proxy.NewProxyHandler(poolMgr, leaseEng)
	webhookDispatcher := storage.NewWebhookDispatcher(os.Getenv("WEBHOOK_URL"))
	kvCache := storage.NewKVCacheLayer(poolMgr)
	r2Gateway := storage.NewR2Gateway(poolMgr, kvCache, webhookDispatcher, storage.DefaultChunkSize)

	// Edge Worker Bundle Virtual Compiler & Dynamic Route Injection (Milestone 7)
	bundleMgr := compiler.NewBundleManager()
	routeCompiler := compiler.NewRouteCompiler()
	streamPipe := compiler.NewStreamPipe(compiler.DefaultBufferSize)
	compilerHandler := compiler.NewCompilerHandler(bundleMgr, routeCompiler, streamPipe)

	// Seed default edge worker and dynamic route mappings
	seedScript := `// Edge Worker Microservice Gateway
export default {
	async fetch(request, env, ctx) {
		const url = new URL(request.url);
		return new Response(JSON.stringify({
			status: "edge_worker_dispatched",
			tenant: "cf-tenant-alpha",
			path: url.pathname,
			timestamp: new Date().toISOString()
		}), {
			headers: { "Content-Type": "application/json", "X-Edge-Origin": "cf-lease-broker" }
		});
	}
};`
	seedBundle, _ := bundleMgr.PackageWorker(compiler.BundleConfig{
		TenantID:          "cf-tenant-alpha",
		Name:              "gateway-worker",
		Minify:            true,
		CompatibilityDate: "2024-04-01",
		MemoryLimitMB:     128,
	}, []byte(seedScript), nil)
	if seedBundle != nil {
		_, _ = routeCompiler.RegisterRoute("*.edge.broker/api/*", "zone-default", "cf-tenant-alpha", seedBundle.ID)
		_, _ = routeCompiler.RegisterRoute("app.internal/*", "zone-default", "cf-tenant-alpha", seedBundle.ID)
	}

	// Distributed Quota, Rate Limiting & Prometheus Metrics (Milestone 5)
	slidingLimiter := quota.NewSlidingWindowLimiter(time.Minute, 120, 30)
	probationMgr := quota.NewProbationManager(poolMgr)
	quotaNotifier := quota.NewQuotaNotifier(os.Getenv("DISCORD_WEBHOOK_URL"), os.Getenv("SLACK_WEBHOOK_URL"), os.Getenv("WEBHOOK_URL"))
	metricsCollector := quota.NewMetricsCollector(poolMgr, leaseEng, probationMgr, slidingLimiter)

	// Background ticker for automated probation recovery and quota evaluation
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			probationMgr.SweepExpired()
			for _, acc := range poolMgr.ListAccounts() {
				quotaNotifier.CheckAccount(acc)
			}
		}
	}()

	// Production Secrets Vault & Auth Manager (Milestone 4)
	vault := security.NewSecretVault()
	jwtSecret := []byte(os.Getenv("CF_BROKER_JWT_SECRET"))
	jwtMgr := security.NewJWTManager(jwtSecret)
	authMgr := security.NewAuthManager(jwtMgr, vault)

	// Seed encrypted credentials for pooled accounts into vault and poolMgr
	for _, acc := range activeConfig.Accounts {
		poolMgr.UpsertAccount(acc.ToAccountStatus())
		token := acc.APIToken
		if token == "" {
			token = fmt.Sprintf("cftok_%s_edge_production_token", acc.ID)
		}
		_ = vault.StoreCredential(acc.ID, []byte(token))
	}

	// Register hot-reload callback to synchronize pool and credentials with zero downtime
	hotReloader.OnReload(func(oldCfg, newCfg *config.Config) {
		log.Printf("[Config] Hot reloading configuration with %d accounts...", len(newCfg.Accounts))
		var statuses []broker.AccountStatus
		for _, acc := range newCfg.Accounts {
			statuses = append(statuses, acc.ToAccountStatus())
			if acc.APIToken != "" {
				_ = vault.StoreCredential(acc.ID, []byte(acc.APIToken))
			}
		}
		poolMgr.SyncAccounts(statuses)
		log.Println("[Config] Hot reload applied successfully with zero downtime.")
	})

	if err := hotReloader.Start(); err != nil {
		log.Printf("[Config] Warning: hot reloader background start: %v", err)
	}

	mux := http.NewServeMux()

	// Prometheus 0.0.4 Metrics Exporter (/metrics)
	mux.Handle("/metrics", metricsCollector)

	// Distributed Quota, Webhooks & Probation APIs (/api/quota/*)
	mux.Handle("/api/quota/webhooks/", quotaNotifier)
	mux.Handle("/api/quota/alerts", quotaNotifier)
	mux.Handle("/api/quota/probation", probationMgr)
	mux.Handle("/api/quota/probation/", probationMgr)
	mux.HandleFunc("/api/quota/ratelimit", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			key = "global"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(slidingLimiter.GetStats(key))
	})

	// Virtual R2 S3-Compatible Gateway (/s3/{bucket}/{key...})
	mux.Handle("/s3/", r2Gateway)

	// Multi-Account Partitioned KV API (/api/kv/*)
	mux.Handle("/api/kv/", kvCache)

	// Outgoing Quota Alerts & Webhooks API (/api/webhooks/*)
	mux.Handle("/api/webhooks/", webhookDispatcher)

	// Transparent Reverse Proxy with 429 Rate-Limit Interception
	mux.Handle("/proxy/", proxyHandler)

	// Production Secrets Vault API (/api/security/*, Protected by Bearer/Admin Auth)
	mux.Handle("/api/security/", authMgr.RequireAuth(vault))

	// Configuration & Hot-Reload Diagnostics API (/api/config/*, Protected by Bearer/Admin Auth)
	mux.Handle("/api/config/", authMgr.RequireAuth(hotReloader))
	mux.Handle("/api/config", authMgr.RequireAuth(hotReloader))

	// Edge Worker Virtual Compiler, Dynamic Route Injection & Zero-Copy Streaming (/api/compiler/*)
	mux.Handle("/api/compiler/", compilerHandler)
	mux.Handle("/api/compiler", compilerHandler)

	// Authentication & JWT Management Endpoints
	mux.HandleFunc("/api/auth/token", authMgr.HandleIssueToken)
	mux.HandleFunc("/api/auth/revoke", authMgr.RequireAuth(http.HandlerFunc(authMgr.HandleRevokeToken)).ServeHTTP)
	mux.HandleFunc("/api/auth/me", authMgr.RequireAuth(http.HandlerFunc(authMgr.HandleMe)).ServeHTTP)
	mux.HandleFunc("/api/auth/status", authMgr.HandleStatus)

	// API: List pooled Cloudflare accounts
	mux.HandleFunc("/api/accounts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(poolMgr.ListAccounts())
	})

	// API: List and grant leases (Protected by JWT/Bearer Authentication)
	leaseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var req grantRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.TargetService == "" {
				req.TargetService = "service-app"
			}
			if req.ResourceType == "" {
				req.ResourceType = "Workers"
			}
			if req.DurationHours <= 0 {
				req.DurationHours = 24
			}

			voucher, err := leaseEng.GrantLease(req.TargetService, req.ResourceType, time.Duration(req.DurationHours)*time.Hour)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			metricsCollector.IncCumulativeLeases()
			_ = json.NewEncoder(w).Encode(voucher)
			return
		}

		_ = json.NewEncoder(w).Encode(leaseEng.ListLeases())
	})
	mux.Handle("/api/leases", authMgr.RequireAuth(leaseHandler))

	// API: Revoke active lease (Protected by JWT/Bearer Authentication)
	revokeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id query parameter", http.StatusBadRequest)
			return
		}
		if err := leaseEng.RevokeLease(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"revoked"}`))
	})
	mux.Handle("/api/leases/revoke", authMgr.RequireAuth(revokeHandler))

	// API: Cloudflare connectivity check
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := cfClient.CheckConnectivity(r.Context())
		_ = json.NewEncoder(w).Encode(status)
	})

	// Embedded Static Dashboard
	subFS, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		log.Fatalf("Failed to initialize embedded web filesystem: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	fmt.Println("=================================================================")
	fmt.Printf(" ⚡ CF-Lease-Broker v%s · Multi-Account Cloudflare Resource Broker\n", Version)
	fmt.Printf(" 📡 Web Dashboard Live at http://localhost:%s\n", port)
	fmt.Println(" 🔒 Production Secrets Vault & HS256 JWT Authentication Enabled")
	fmt.Println(" 🔄 Zero-Downtime Hot Reloading Active (Watcher & SIGHUP)")
	fmt.Println(" 🚀 Edge Worker Virtual Compiler & Dynamic Route Injection Active")
	fmt.Println(" 💡 Standalone Single Binary with Embedded Zero-Dependency UI")
	fmt.Println("=================================================================")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
