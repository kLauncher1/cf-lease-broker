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
	"github.com/kLauncher1/cf-lease-broker/internal/proxy"
	"github.com/kLauncher1/cf-lease-broker/internal/security"
	"github.com/kLauncher1/cf-lease-broker/internal/storage"
)

//go:embed web/dist/*
var embeddedWeb embed.FS

type grantRequest struct {
	TargetService string `json:"target_service"`
	ResourceType  string `json:"resource_type"`
	DurationHours int    `json:"duration_hours"`
}

func main() {
	port := os.Getenv("PORT")
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

	// Production Secrets Vault & Auth Manager (Milestone 4)
	vault := security.NewSecretVault()
	jwtSecret := []byte(os.Getenv("CF_BROKER_JWT_SECRET"))
	jwtMgr := security.NewJWTManager(jwtSecret)
	authMgr := security.NewAuthManager(jwtMgr, vault)

	// Seed encrypted credentials for pooled accounts into vault
	for _, acc := range poolMgr.ListAccounts() {
		mockKey := fmt.Sprintf("cftok_%s_edge_production_token", acc.ID)
		_ = vault.StoreCredential(acc.ID, []byte(mockKey))
	}

	mux := http.NewServeMux()

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
	fmt.Println(" ⚡ CF-Lease-Broker · Multi-Account Cloudflare Resource Broker")
	fmt.Printf(" 📡 Web Dashboard Live at http://localhost:%s\n", port)
	fmt.Println(" 🔒 Production Secrets Vault & HS256 JWT Authentication Enabled")
	fmt.Println(" 💡 Standalone Single Binary with Embedded Zero-Dependency UI")
	fmt.Println("=================================================================")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
