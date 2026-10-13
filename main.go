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

	mux := http.NewServeMux()

	// API: List pooled Cloudflare accounts
	mux.HandleFunc("/api/accounts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(poolMgr.ListAccounts())
	})

	// API: List and grant leases
	mux.HandleFunc("/api/leases", func(w http.ResponseWriter, r *http.Request) {
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

	// API: Revoke active lease
	mux.HandleFunc("/api/leases/revoke", func(w http.ResponseWriter, r *http.Request) {
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
	fmt.Println(" 💡 Standalone Single Binary with Embedded Zero-Dependency UI")
	fmt.Println("=================================================================")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
