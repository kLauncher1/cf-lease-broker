package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/billing"
	"github.com/kLauncher1/cf-lease-broker/internal/broker"
	"github.com/kLauncher1/cf-lease-broker/internal/cloudflare"
	"github.com/kLauncher1/cf-lease-broker/internal/compiler"
	"github.com/kLauncher1/cf-lease-broker/internal/config"
	"github.com/kLauncher1/cf-lease-broker/internal/enclave"
	"github.com/kLauncher1/cf-lease-broker/internal/federation"
	"github.com/kLauncher1/cf-lease-broker/internal/mesh"
	"github.com/kLauncher1/cf-lease-broker/internal/mtls"
	"github.com/kLauncher1/cf-lease-broker/internal/pqc"
	"github.com/kLauncher1/cf-lease-broker/internal/proxy"
	"github.com/kLauncher1/cf-lease-broker/internal/quota"
	"github.com/kLauncher1/cf-lease-broker/internal/radar"
	"github.com/kLauncher1/cf-lease-broker/internal/raft"
	"github.com/kLauncher1/cf-lease-broker/internal/replication"
	"github.com/kLauncher1/cf-lease-broker/internal/rotator"
	"github.com/kLauncher1/cf-lease-broker/internal/sandbox"
	"github.com/kLauncher1/cf-lease-broker/internal/scrubber"
	"github.com/kLauncher1/cf-lease-broker/internal/security"
	"github.com/kLauncher1/cf-lease-broker/internal/steering"
	"github.com/kLauncher1/cf-lease-broker/internal/storage"
	"github.com/kLauncher1/cf-lease-broker/internal/telemetry"
	"github.com/kLauncher1/cf-lease-broker/internal/transcoder"
	"github.com/kLauncher1/cf-lease-broker/internal/wasmdedup"
	"github.com/kLauncher1/cf-lease-broker/internal/zkproof"
)

// ServerContext encapsulates runtime engine dependencies for HTTP routing.
type ServerContext struct {
	poolMgr           *broker.PoolManager
	leaseEng          *broker.LeaseEngine
	cfClient          *cloudflare.Client
	proxyHandler      *proxy.ProxyHandler
	authMgr           *security.AuthManager
	vault             *security.SecretVault
	hotReloader       *config.HotReloader
	compilerHandler   *compiler.CompilerHandler
	metricsCollector  *quota.MetricsCollector
	slidingLimiter    *quota.SlidingWindowLimiter
	probationMgr      *quota.ProbationManager
	quotaNotifier     *quota.QuotaNotifier
	raftHandler       *raft.RaftHandler
	raftNode          *raft.RaftNode
	mtlsHandler       *mtls.MTLSHandler
	billingHandler    *billing.BillingHandler
	radarHandler      *radar.RadarHandler
	radarRouter       *radar.RadarRouter
	rotatorHandler    *rotator.RotatorHandler
	tokenRotator      *rotator.TokenRotator
	rotatorMesh       *rotator.RevocationMesh
	steeringHandler   *steering.SteeringHandler
	sandboxHandler    *sandbox.SandboxHandler
	meshHandler       *mesh.MeshHandler
	telemetryHandler  *telemetry.TelemetryHandler
	transcoderHandler *transcoder.TranscoderHandler
	replHandler       *replication.ReplicationHandler
	fedHandler        *federation.FederationHandler
	zkHandler         *zkproof.ZKProofHandler
	pqcHandler        *pqc.PQCHandler
	dedupHandler      *wasmdedup.DedupHandler
	scrubberHandler   *scrubber.ScrubberHandler
	enclaveHandler    *enclave.EnclaveHandler
	r2Gateway         *storage.R2Gateway
	kvCache           *storage.KVCacheLayer
	webhookDispatcher *storage.WebhookDispatcher
}

// buildMux constructs and binds all HTTP endpoints and middleware.
func buildMux(s *ServerContext) *http.ServeMux {
	mux := http.NewServeMux()

	regPair := func(path string, h http.Handler) {
		mux.Handle(path+"/", h)
		mux.Handle(path, h)
	}

	mux.Handle("/metrics", s.metricsCollector)
	mux.Handle("/api/quota/webhooks/", s.quotaNotifier)
	mux.Handle("/api/quota/alerts", s.quotaNotifier)
	regPair("/api/quota/probation", s.probationMgr)
	mux.HandleFunc("/api/quota/ratelimit", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			key = "global"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.slidingLimiter.GetStats(key))
	})

	mux.Handle("/s3/", s.r2Gateway)
	mux.Handle("/api/kv/", s.kvCache)
	mux.Handle("/api/webhooks/", s.webhookDispatcher)
	mux.Handle("/proxy/", s.proxyHandler)
	mux.Handle("/api/security/", s.authMgr.RequireAuth(s.vault))

	for path, h := range map[string]http.Handler{
		"/api/config":      s.authMgr.RequireAuth(s.hotReloader),
		"/api/compiler":    s.compilerHandler,
		"/raft":            s.raftHandler,
		"/api/mtls":        s.mtlsHandler,
		"/api/billing":     s.billingHandler,
		"/api/radar":       s.radarHandler,
		"/api/rotator":     s.rotatorHandler,
		"/api/steering":    s.steeringHandler,
		"/api/sandbox":     s.sandboxHandler,
		"/api/mesh":        s.meshHandler,
		"/api/telemetry":   s.telemetryHandler,
		"/api/transcoder":  s.transcoderHandler,
		"/api/replication": s.replHandler,
		"/api/federation":  s.fedHandler,
		"/api/zkproof":     s.zkHandler,
		"/api/pqc":         s.pqcHandler,
		"/api/wasmdedup":   s.dedupHandler,
		"/api/scrubber":    s.scrubberHandler,
		"/api/enclave":     s.enclaveHandler,
	} {
		regPair(path, h)
	}

	mux.HandleFunc("/api/auth/token", s.authMgr.HandleIssueToken)
	mux.HandleFunc("/api/auth/revoke", s.authMgr.RequireAuth(http.HandlerFunc(s.authMgr.HandleRevokeToken)).ServeHTTP)
	mux.HandleFunc("/api/auth/me", s.authMgr.RequireAuth(http.HandlerFunc(s.authMgr.HandleMe)).ServeHTTP)
	mux.HandleFunc("/api/auth/status", s.authMgr.HandleStatus)
	mux.HandleFunc("/api/accounts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.poolMgr.ListAccounts())
	})

	leaseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var req grantRequest
			if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
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

			voucher, err := s.leaseEng.GrantLease(req.TargetService, req.ResourceType, time.Duration(req.DurationHours)*time.Hour)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			s.tokenRotator.RegisterVoucher(*voucher)
			if s.raftNode.Status().Role == raft.RoleLeader {
				vBytes, _ := json.Marshal(voucher)
				_, _ = s.raftNode.Propose(raft.CmdGrantLease, vBytes)
			}
			rec := s.radarRouter.ResolveOptimalColo(radar.GeoPoint{}, false)
			s.radarRouter.AssignLeaseColo(voucher.LeaseID, req.TargetService, rec.Colo)
			s.metricsCollector.IncCumulativeLeases()
			_ = json.NewEncoder(w).Encode(voucher)
			return
		}
		_ = json.NewEncoder(w).Encode(s.leaseEng.ListLeases())
	})
	mux.Handle("/api/leases", s.authMgr.RequireAuth(leaseHandler))

	revokeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id query parameter", http.StatusBadRequest)
			return
		}
		if err := s.tokenRotator.RevokeToken(id, "operator_api_revocation"); err != nil {
			if err := s.leaseEng.RevokeLease(id); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			s.rotatorMesh.Revoke(id, "", "operator_api_revocation")
		}
		if s.raftNode.Status().Role == raft.RoleLeader {
			rBytes, _ := json.Marshal(raft.RevokeLeasePayload{LeaseID: id})
			_, _ = s.raftNode.Propose(raft.CmdRevokeLease, rBytes)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"revoked"}`))
	})
	mux.Handle("/api/leases/revoke", s.authMgr.RequireAuth(revokeHandler))

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.cfClient.CheckConnectivity(r.Context()))
	})

	subFS, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		log.Fatalf("Failed to initialize embedded web filesystem: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	return mux
}
