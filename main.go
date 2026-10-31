package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/billing"
	"github.com/kLauncher1/cf-lease-broker/internal/broker"
	"github.com/kLauncher1/cf-lease-broker/internal/cloudflare"
	"github.com/kLauncher1/cf-lease-broker/internal/compiler"
	"github.com/kLauncher1/cf-lease-broker/internal/config"
	"github.com/kLauncher1/cf-lease-broker/internal/mesh"
	"github.com/kLauncher1/cf-lease-broker/internal/mtls"
	"github.com/kLauncher1/cf-lease-broker/internal/proxy"
	"github.com/kLauncher1/cf-lease-broker/internal/quota"
	"github.com/kLauncher1/cf-lease-broker/internal/radar"
	"github.com/kLauncher1/cf-lease-broker/internal/raft"
	"github.com/kLauncher1/cf-lease-broker/internal/replication"
	"github.com/kLauncher1/cf-lease-broker/internal/rotator"
	"github.com/kLauncher1/cf-lease-broker/internal/sandbox"
	"github.com/kLauncher1/cf-lease-broker/internal/security"
	"github.com/kLauncher1/cf-lease-broker/internal/steering"
	"github.com/kLauncher1/cf-lease-broker/internal/storage"
	"github.com/kLauncher1/cf-lease-broker/internal/telemetry"
	"github.com/kLauncher1/cf-lease-broker/internal/transcoder"
)

var (
	// Version is populated at build time via -ldflags="-X main.Version=..."
	Version = "0.18.0"
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
	// Initialize configuration & zero-downtime hot reloader
	configPath := os.Getenv("CF_BROKER_CONFIG_PATH")
	if configPath == "" {
		configPath = config.DefaultConfigPath
	}
	hotReloader := config.NewHotReloader(configPath, nil, 2*time.Second)
	activeConfig := hotReloader.GetConfig()

	port := os.Getenv("PORT")
	if port == "" { port = activeConfig.ServerPort }
	if port == "" { port = "9081" }

	poolMgr := broker.NewPoolManager()
	leaseEng := broker.NewLeaseEngine(poolMgr)
	cfClient := cloudflare.NewClient()
	proxyHandler := proxy.NewProxyHandler(poolMgr, leaseEng)
	webhookDispatcher := storage.NewWebhookDispatcher(os.Getenv("WEBHOOK_URL"))
	kvCache := storage.NewKVCacheLayer(poolMgr)
	r2Gateway := storage.NewR2Gateway(poolMgr, kvCache, webhookDispatcher, storage.DefaultChunkSize)

	// Edge Worker virtual compiler and dynamic route injector
	bundleMgr := compiler.NewBundleManager()
	routeCompiler := compiler.NewRouteCompiler()
	streamPipe := compiler.NewStreamPipe(compiler.DefaultBufferSize)
	compilerHandler := compiler.NewCompilerHandler(bundleMgr, routeCompiler, streamPipe)

	seedScript := `export default { async fetch(req) { return new Response(JSON.stringify({status:"edge_worker_dispatched",tenant:"cf-tenant-alpha",time:new Date().toISOString()}),{headers:{"Content-Type":"application/json","X-Edge-Origin":"cf-lease-broker"}}); } };`
	seedBundle, _ := bundleMgr.PackageWorker(compiler.BundleConfig{
		TenantID: "cf-tenant-alpha", Name: "gateway-worker", Minify: true, CompatibilityDate: "2024-04-01", MemoryLimitMB: 128,
	}, []byte(seedScript), nil)
	if seedBundle != nil {
		_, _ = routeCompiler.RegisterRoute("*.edge.broker/api/*", "zone-default", "cf-tenant-alpha", seedBundle.ID)
		_, _ = routeCompiler.RegisterRoute("app.internal/*", "zone-default", "cf-tenant-alpha", seedBundle.ID)
	}

	// Distributed quota, rate limiting & Prometheus metrics
	slidingLimiter := quota.NewSlidingWindowLimiter(time.Minute, 120, 30)
	probationMgr := quota.NewProbationManager(poolMgr)
	quotaNotifier := quota.NewQuotaNotifier(os.Getenv("DISCORD_WEBHOOK_URL"), os.Getenv("SLACK_WEBHOOK_URL"), os.Getenv("WEBHOOK_URL"))
	metricsCollector := quota.NewMetricsCollector(poolMgr, leaseEng, probationMgr, slidingLimiter)

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

	// Production secrets vault & auth manager
	vault := security.NewSecretVault()
	jwtSecret := []byte(os.Getenv("CF_BROKER_JWT_SECRET"))
	jwtMgr := security.NewJWTManager(jwtSecret)
	authMgr := security.NewAuthManager(jwtMgr, vault)

	// High-availability Raft consensus engine
	raftNodeID := os.Getenv("CF_RAFT_NODE_ID")
	if raftNodeID == "" { raftNodeID = "broker-node-" + port }
	raftDiscoveryDomain := os.Getenv("CF_RAFT_DISCOVERY_DOMAIN")
	raftDataDir := os.Getenv("CF_RAFT_DATA_DIR")
	peerMap := make(map[string]string)
	if peersRaw := os.Getenv("CF_RAFT_PEERS"); peersRaw != "" {
		for _, p := range strings.Split(peersRaw, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if k, v, ok := strings.Cut(p, "="); ok {
				peerMap[k] = v
			} else {
				peerMap[p] = p
			}
		}
	}

	raftJournal, err := raft.NewJournal(raftDataDir)
	if err != nil {
		raftJournal, _ = raft.NewJournal("")
	}
	raftDiscovery := raft.NewPeerDiscovery(raftNodeID, raftDiscoveryDomain, peerMap)
	brokerFSM := raft.NewBrokerFSM(poolMgr, leaseEng)
	raftCfg := raft.DefaultNodeConfig(raftNodeID)
	raftCfg.DataDir = raftDataDir
	raftCfg.DiscoveryDomain = raftDiscoveryDomain
	raftNode := raft.NewRaftNode(raftCfg, raftJournal, raftDiscovery, brokerFSM)
	raftNode.Start()
	defer raftNode.Stop()
	raftHandler := raft.NewRaftHandler(raftNode, raftJournal, raftDiscovery)

	// Zero-trust mutual TLS (mTLS) & ephemeral CA mesh
	ephemeralCA, err := mtls.NewEphemeralCA("cf-lease-broker-root-ca", 365*24*time.Hour)
	if err != nil {
		log.Fatalf("Failed to initialize mTLS Ephemeral CA: %v", err)
	}
	mtlsPolicy := mtls.VerificationPolicy{
		RequireClientCert: false,
		EnforceExpiry:     true,
		AllowedSANs:        []string{"*.edge.broker.local", "*.workers.dev", "127.0.0.1"},
	}
	certVerifier := mtls.NewCertVerifier(ephemeralCA, mtlsPolicy)
	mtlsHandler := mtls.NewMTLSHandler(ephemeralCA, certVerifier)

	if os.Getenv("CF_REQUIRE_MTLS") == "true" {
		proxyHandler.CertVerifier = func(r *http.Request) error {
			cert, err := certVerifier.ExtractCertFromRequest(r)
			if err != nil {
				return err
			}
			res := certVerifier.VerifyCertificate(cert)
			if !res.Valid {
				return fmt.Errorf("client certificate rejected: %s", res.Reason)
			}
			return nil
		}
		log.Println("Zero-Trust mTLS client certificate verification enforced on edge proxy")
	}

	// Cost optimization & multi-tenant billing ledger
	billingMeter := billing.NewBillingMeter(billing.DefaultCloudflareRates())
	for _, acc := range activeConfig.Accounts {
		tier := billing.TierPaidStandard
		if strings.Contains(strings.ToLower(acc.Plan), "free") {
			tier = billing.TierFree
		}
		billingMeter.RegisterAccount(acc.ID, tier, 10000000)
	}
	costOptimizer := billing.NewCostOptimizer(billingMeter, poolMgr, billing.DefaultMigrationRule())
	billingHandler := billing.NewBillingHandler(billingMeter, costOptimizer)
	proxyHandler.SetUsageRecorder(func(tenantID, accountID, service string, cpuMs, bytesIn, bytesOut int64) {
		billingMeter.RecordProxyRequest(tenantID, accountID, service, cpuMs, bytesIn, bytesOut)
	})

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			_ = costOptimizer.EvaluateWorkloadMigration()
		}
	}()

	// Intelligent Edge Health Radar & Anycast geo-routing
	probeEngine := radar.NewProbeEngine(radar.DefaultPoPList())
	radarRouter := radar.NewRadarRouter(probeEngine)
	radarHandler := radar.NewRadarHandler(radarRouter, probeEngine)
	probeEngine.Start(60 * time.Second)
	defer probeEngine.Stop()

	proxyHandler.SetColoResolver(func(r *http.Request) (string, float64) {
		country := r.Header.Get("CF-IPCountry")
		if country == "" {
			country = r.Header.Get("X-Country-Code")
		}
		loc := radar.GeoPoint{}
		if country != "" {
			if geo, found := radar.CountryToGeo(country); found {
				loc = geo
			}
		}
		tenantID := r.Header.Get("X-Tenant-ID")
		var rec radar.RouteRecommendation
		if tenantID != "" {
			rec = radarRouter.ResolveTenantColo(tenantID, loc)
		} else {
			rec = radarRouter.ResolveOptimalColo(loc, false)
		}
		return rec.Colo, rec.DistanceKm
	})

	// Autonomous Edge Token Rotator & Revocation Mesh
	rotatorMesh := rotator.NewRevocationMesh("broker-node-"+port, []byte(os.Getenv("CF_BROKER_GOSSIP_SECRET")))
	attestVerifier := rotator.NewAttestationVerifier([]byte(os.Getenv("CF_BROKER_ATTEST_SECRET")))
	tokenRotator := rotator.NewTokenRotator(rotator.DefaultRotatorConfig(), rotatorMesh, leaseEng, attestVerifier)
	tokenRotator.Start()
	defer tokenRotator.Stop()
	rotatorHandler := rotator.NewRotatorHandler(tokenRotator, rotatorMesh, attestVerifier)

	// Zero-overhead HTTP/3 eBPF packet steering
	steeringEngine := steering.NewPacketSteeringEngine(steering.DefaultSteeringConfig())
	for _, ep := range []steering.WorkerEndpoint{
		{ID: "worker-edge-iad", Colo: "IAD", Address: "10.0.1.10:443", Weight: 100},
		{ID: "worker-edge-lhr", Colo: "LHR", Address: "10.0.2.10:443", Weight: 100},
		{ID: "worker-edge-nrt", Colo: "NRT", Address: "10.0.3.10:443", Weight: 100},
	} { _ = steeringEngine.RegisterEndpoint(&ep) }
	_ = steeringEngine.AddRule(&steering.SteeringRule{ID: "rule-na", CIDPrefix: "cf01", TargetEndpointID: "worker-edge-iad", Priority: 10, Description: "North America CID prefix"})
	_ = steeringEngine.AddRule(&steering.SteeringRule{ID: "rule-eu", CIDPrefix: "cf02", TargetEndpointID: "worker-edge-lhr", Priority: 10, Description: "Europe CID prefix"})
	steeringHandler := steering.NewSteeringHandler(steeringEngine)

	proxyHandler.SetSteeringResolver(func(r *http.Request) (string, float64, string) {
		dec, err := steeringEngine.SteerPacket([]byte(r.URL.Path), r.RemoteAddr, r.Host)
		if err == nil && dec != nil && dec.TargetEndpoint != nil {
			return string(dec.Mode), dec.DispatchLatencyUs, dec.TargetEndpoint.ID
		}
		return string(steering.SteeringModeUserspace), 0.35, "worker-edge-iad"
	})

	// Edge Wasm Sandbox Memory Limiter & Crash Isolation
	sandboxEngine := sandbox.NewSandboxEngine(sandbox.DefaultSandboxConfig())
	sandboxHandler := sandbox.NewSandboxHandler(sandboxEngine)
	proxyHandler.SetSandboxResolver(func(r *http.Request) (string, int) {
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID == "" {
			tenantID = "default"
		}
		if inst, err := sandboxEngine.GetOrCreateInstance(tenantID, "inst-0"); err == nil {
			return string(inst.Status), inst.Memory.Pages()
		}
		return string(sandbox.StatusIdle), sandbox.DefaultInitialMemoryPages
	})

	// Multi-Region Anycast Edge Mesh & Dynamic Peer Sharding
	meshNodeID := os.Getenv("CF_MESH_NODE_ID")
	if meshNodeID == "" { meshNodeID = "broker-iad-" + port }
	meshRegion := os.Getenv("CF_MESH_REGION")
	if meshRegion == "" { meshRegion = "iad" }
	meshSecret := []byte(os.Getenv("CF_MESH_SECRET"))
	if len(meshSecret) == 0 { meshSecret = []byte("cf-lease-mesh-default-secret-key") }
	meshCfg := mesh.DefaultMeshConfig(meshNodeID, meshRegion, "127.0.0.1:"+port)
	meshCfg.SecretKey = meshSecret
	meshEngine := mesh.NewMeshEngine(meshCfg)
	for _, p := range []mesh.PeerNode{
		{ID: "broker-lhr-01", Address: "10.0.2.10:9081", Region: "lhr", Vnodes: 128, Status: mesh.NodeStatusActive},
		{ID: "broker-nrt-01", Address: "10.0.3.10:9081", Region: "nrt", Vnodes: 128, Status: mesh.NodeStatusActive},
		{ID: "broker-sfo-01", Address: "10.0.4.10:9081", Region: "sfo", Vnodes: 128, Status: mesh.NodeStatusActive},
	} { _ = meshEngine.RegisterPeer(p) }
	meshEngine.Start()
	defer meshEngine.Stop()
	meshHandler := mesh.NewMeshHandler(meshEngine)

	proxyHandler.SetMeshAffinityResolver(func(r *http.Request) (string, string, string, bool) {
		clientRegion := r.Header.Get("CF-IPCountry")
		if clientRegion == "" { clientRegion = r.Header.Get("X-Region") }
		if clientRegion == "" { clientRegion = "iad" }
		shardKey := r.Header.Get("X-Tenant-ID")
		if shardKey == "" { shardKey = r.URL.Path }
		if dec, err := meshEngine.ResolveAffinity(clientRegion, shardKey); err == nil && dec != nil {
			return dec.SelectedNode.ID, dec.SelectedNode.Region, shardKey, dec.IsLocalServe
		}
		return meshNodeID, meshRegion, shardKey, true
	})

	// Distributed eBPF Telemetry Pipeline & Anomaly Self-Healing Engine
	telemetryEngine := telemetry.NewTelemetryEngine(telemetry.DefaultTelemetryConfig())
	for _, acc := range activeConfig.Accounts {
		telemetryEngine.RegisterTarget(acc.ID, "127.0.0.1:443", "tcp")
	}
	telemetryEngine.RegisterTarget("default", "127.0.0.1:443", "tcp")
	telemetryEngine.Start(context.Background())
	defer telemetryEngine.Stop()
	telemetryHandler := telemetry.NewTelemetryHandler(telemetryEngine)

	// Adaptive Zero-Copy Compression & Brotli/Zstandard Edge Transcoder
	transcoderEngine := transcoder.NewTranscoderEngine(transcoder.DefaultTranscoderConfig())
	transcoderHandler := transcoder.NewTranscoderHandler(transcoderEngine)

	proxyHandler.SetCircuitChecker(func(accountID string) bool {
		return telemetryEngine.GetCircuitState(accountID) == telemetry.CircuitTripped
	})
	proxyHandler.SetTelemetryResolver(func(r *http.Request) (string, float64, float64, bool) {
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID == "" { tenantID = "default" }
		state := telemetryEngine.GetCircuitState(tenantID)
		return string(state), 14.85, 0.0015, state == telemetry.CircuitTripped || state == telemetry.CircuitHalfOpen
	})
	proxyHandler.SetTranscoderResolver(func(r *http.Request, respHeader http.Header, body []byte) ([]byte, string, float64) {
		dec := transcoderEngine.InspectPayload(r.Header.Get("Accept-Encoding"), respHeader.Get("Content-Type"), int64(len(body)))
		if !dec.ShouldCompress { return body, "", 0 }
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID == "" { tenantID = "default" }
		comp, err := transcoderEngine.TranscodeBytes(body, dec, tenantID)
		if err != nil || len(comp) >= len(body) { return body, "", 0 }
		return comp, string(dec.Algorithm), float64(len(body)-len(comp)) / float64(len(body)) * 100.0
	})

	// Key-Value Geo-Replication Mesh & Eventual Consistency Engine
	replEngine := replication.NewGeoReplicationEngine(replication.ReplicationConfig{LocalNodeID: "node-edge-local", Region: "ord"})
	replEngine.Start()
	defer replEngine.Stop()
	replHandler := replication.NewReplicationHandler(replEngine)
	proxyHandler.SetReplicationResolver(func(r *http.Request) (string, string) {
		ctx := replication.ExtractSessionContext(r)
		stats := replEngine.Stats()
		if ctx != nil && ctx.RequestedConsistency != "" {
			return string(ctx.RequestedConsistency), stats.CurrentClock.String()
		}
		return "eventual", stats.CurrentClock.String()
	})

	// Seed encrypted credentials into vault and poolMgr
	for _, acc := range activeConfig.Accounts {
		poolMgr.UpsertAccount(acc.ToAccountStatus())
		token := acc.APIToken
		if token == "" {
			token = fmt.Sprintf("cftok_%s_edge_production_token", acc.ID)
		}
		_ = vault.StoreCredential(acc.ID, []byte(token))
	}

	hotReloader.OnReload(func(oldCfg, newCfg *config.Config) {
		var statuses []broker.AccountStatus
		for _, acc := range newCfg.Accounts {
			telemetryEngine.RegisterTarget(acc.ID, "127.0.0.1:443", "tcp")
			statuses = append(statuses, acc.ToAccountStatus())
			if acc.APIToken != "" { _ = vault.StoreCredential(acc.ID, []byte(acc.APIToken)) }
			tier := billing.TierPaidStandard
			if strings.Contains(strings.ToLower(acc.Plan), "free") { tier = billing.TierFree }
			billingMeter.RegisterAccount(acc.ID, tier, 10000000)
		}
		poolMgr.SyncAccounts(statuses)
	})

	if err := hotReloader.Start(); err != nil {
		log.Printf("[Config] Warning: hot reloader background start: %v", err)
	}

	mux := http.NewServeMux()

	regPair := func(path string, h http.Handler) {
		mux.Handle(path+"/", h)
		mux.Handle(path, h)
	}

	mux.Handle("/metrics", metricsCollector)
	mux.Handle("/api/quota/webhooks/", quotaNotifier)
	mux.Handle("/api/quota/alerts", quotaNotifier)
	regPair("/api/quota/probation", probationMgr)
	mux.HandleFunc("/api/quota/ratelimit", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" { key = "global" }
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(slidingLimiter.GetStats(key))
	})

	mux.Handle("/s3/", r2Gateway)
	mux.Handle("/api/kv/", kvCache)
	mux.Handle("/api/webhooks/", webhookDispatcher)
	mux.Handle("/proxy/", proxyHandler)
	mux.Handle("/api/security/", authMgr.RequireAuth(vault))
	regPair("/api/config", authMgr.RequireAuth(hotReloader))
	regPair("/api/compiler", compilerHandler)
	regPair("/raft", raftHandler)
	regPair("/api/mtls", mtlsHandler)
	regPair("/api/billing", billingHandler)
	regPair("/api/radar", radarHandler)
	regPair("/api/rotator", rotatorHandler)
	regPair("/api/steering", steeringHandler)
	regPair("/api/sandbox", sandboxHandler)
	regPair("/api/mesh", meshHandler)
	regPair("/api/telemetry", telemetryHandler)
	regPair("/api/transcoder", transcoderHandler)
	regPair("/api/replication", replHandler)

	mux.HandleFunc("/api/auth/token", authMgr.HandleIssueToken)
	mux.HandleFunc("/api/auth/revoke", authMgr.RequireAuth(http.HandlerFunc(authMgr.HandleRevokeToken)).ServeHTTP)
	mux.HandleFunc("/api/auth/me", authMgr.RequireAuth(http.HandlerFunc(authMgr.HandleMe)).ServeHTTP)
	mux.HandleFunc("/api/auth/status", authMgr.HandleStatus)
	mux.HandleFunc("/api/accounts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(poolMgr.ListAccounts())
	})

	leaseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var req grantRequest
			if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.TargetService == "" { req.TargetService = "service-app" }
			if req.ResourceType == "" { req.ResourceType = "Workers" }
			if req.DurationHours <= 0 { req.DurationHours = 24 }

			voucher, err := leaseEng.GrantLease(req.TargetService, req.ResourceType, time.Duration(req.DurationHours)*time.Hour)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			tokenRotator.RegisterVoucher(*voucher)
			if raftNode.Status().Role == raft.RoleLeader {
				vBytes, _ := json.Marshal(voucher)
				_, _ = raftNode.Propose(raft.CmdGrantLease, vBytes)
			}
			rec := radarRouter.ResolveOptimalColo(radar.GeoPoint{}, false)
			radarRouter.AssignLeaseColo(voucher.LeaseID, req.TargetService, rec.Colo)
			metricsCollector.IncCumulativeLeases()
			_ = json.NewEncoder(w).Encode(voucher)
			return
		}
		_ = json.NewEncoder(w).Encode(leaseEng.ListLeases())
	})
	mux.Handle("/api/leases", authMgr.RequireAuth(leaseHandler))

	revokeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" { http.Error(w, "missing id query parameter", http.StatusBadRequest); return }
		if err := tokenRotator.RevokeToken(id, "operator_api_revocation"); err != nil {
			if err := leaseEng.RevokeLease(id); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			rotatorMesh.Revoke(id, "", "operator_api_revocation")
		}
		if raftNode.Status().Role == raft.RoleLeader {
			rBytes, _ := json.Marshal(raft.RevokeLeasePayload{LeaseID: id})
			_, _ = raftNode.Propose(raft.CmdRevokeLease, rBytes)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"revoked"}`))
	})
	mux.Handle("/api/leases/revoke", authMgr.RequireAuth(revokeHandler))

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfClient.CheckConnectivity(r.Context()))
	})

	subFS, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil { log.Fatalf("Failed to initialize embedded web filesystem: %v", err) }
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	banner := "=================================================================\n" +
		fmt.Sprintf(" ⚡ CF-Lease-Broker v%s · Multi-Account Cloudflare Resource Broker\n", Version) +
		fmt.Sprintf(" 📡 Web Dashboard Live at http://localhost:%s\n", port) +
		" 🔒 Vault/JWT · 🔄 HotReload · 🚀 Compiler · 🤝 Raft · 🛡️ mTLS · 💰 Billing\n" +
		" 🛰️ Radar · 🔄 Rotator · ⚡ HTTP/3 eBPF · 📦 Wasm Sandbox · 🌐 Mesh · 🩺 Telemetry\n" +
		" 🗜️ Adaptive Compression · 🌐 Key-Value Geo-Replication Active (/api/replication/*)\n" +
		" 💡 Standalone Single Binary with Embedded Zero-Dependency UI\n" +
		"=================================================================\n"
	fmt.Print(banner)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
