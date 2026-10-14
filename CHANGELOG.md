# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.8.0] - 2026-10-14

### Added
- **High-Availability Raft Consensus Engine**: Implemented `internal/raft/node.go` and `internal/raft/types.go` providing a zero-external-dependency Raft consensus engine with randomized election timers (150-300ms), 50ms leader heartbeats, majority quorum voting, automatic leader failover, and candidate step-down on encountering higher terms.
- **Write-Ahead Log (WAL) Transaction Journal**: Implemented `internal/raft/journal.go` featuring monotonic log entry indexing, conflicting entry truncation, commit index progression, SHA-256 integrity checksum verification, and optional persistent disk synchronization (`raft_journal.wal`).
- **Dynamic Peer Discovery via DNS SRV Records**: Implemented `internal/raft/discovery.go` querying DNS SRV records (`net.LookupSRV`) with static address fallback, runtime manual peer registration, and real-time round-trip latency tracking.
- **Concrete Broker Finite State Machine (BrokerFSM)**: Implemented `internal/raft/fsm_broker.go` deterministically applying replicated cluster commands (`GRANT_LEASE`, `REVOKE_LEASE`, `SYNC_ACCOUNT`, `UPDATE_QUOTA`, `NOOP`) across `broker.PoolManager` and `broker.LeaseEngine`, alongside state snapshotting and hydration routines.
- **Raft Consensus REST API & HTTP Transport**: Implemented `internal/raft/handler.go` and mounted `/raft/*` routes in `main.go`, exposing `/raft/vote`, `/raft/append`, `/raft/status`, `/raft/propose`, `/raft/peers`, and `/raft/journal` endpoints.
- **Interactive Glassmorphism Raft Consensus Dashboard Panel**: Enhanced `web/dist/index.html` with an interactive cluster topology monitor, real-time node role / term badge, leader status tracker, command proposer with pre-configured command templates, WAL journal inspector, and DNS SRV peer status table.
- **Comprehensive Raft & Cluster Unit Test Suite**: Added 8 exhaustive unit and multi-node cluster tests in `internal/raft/raft_test.go` covering journal append and checksum integrity, follower truncation, WAL disk persistence and recovery, DNS SRV static fallback, BrokerFSM command execution and snapshots, single-node leader bootstrapping, 3-node cluster election, quorum replication, leader failover, and HTTP endpoints.

### Changed
- Mounted `/raft/` routes on HTTP mux and initialized `RaftNode`, `Journal`, `PeerDiscovery`, and `BrokerFSM` with environment variable overrides (`CF_RAFT_NODE_ID`, `CF_RAFT_PEERS`, `CF_RAFT_DISCOVERY_DOMAIN`, `CF_RAFT_DATA_DIR`) in `main.go`.
- Extended `internal/broker/lease.go` with `ApplyVoucher` method to support deterministic state replication from follower nodes.
- Linked lease grant and revocation endpoints (`/api/leases`) to propose cluster-wide state machine transactions when running as the active Raft leader.
- Bumped application version to `0.8.0` across manifests (`package.json`, `web/package.json`) and security policy (`SECURITY.md`).

---

## [0.7.0] - 2026-10-14

### Added
- **Edge Worker Virtual Compiler & Bundle Packager**: Implemented `internal/compiler/bundle.go` featuring a zero-dependency lexical minifier for JavaScript Edge Worker scripts that strips comments, normalizes whitespace and punctuation, detects standard module exports (`export default { fetch(...) }`, `addEventListener('fetch')`), validates WebAssembly binary headers (`\x00asm` v1), and generates deterministic SHA-256 digests for tenant bundle cataloging.
- **Multi-Zone Wildcard Route Compiler**: Implemented `internal/compiler/router.go` providing dynamic URL route compilation and matching across arbitrary domains, wildcard subdomains (`*.domain.com/*`), exact hosts, and path prefixes with specificity-based priority scoring.
- **Zero-Copy Direct Response Streaming Pipeline**: Implemented `internal/compiler/stream.go` utilizing pre-allocated 64KB buffer pools (`sync.Pool`), immediate socket flushes via `http.Flusher`, and client disconnection cancellation detection (`context.Context`), eliminating heap allocation overhead during high-volume proxy responses.
- **Compiler & Dynamic Route REST Endpoints**: Implemented `internal/compiler/handler.go` and wired `/api/compiler/*` into `main.go`, exposing `/compile`, `/workers` (CRUD), `/routes` (CRUD & pattern matching), and synthetic streaming benchmark endpoints (`/stream-test`).
- **Interactive Compiler Console in Web Dashboard**: Enhanced `web/dist/index.html` with an interactive script minifier, live dynamic route injector, pattern match tester, and real-time zero-copy streaming benchmark runner.
- **Compiler & Router Unit Test Suite**: Added 9 exhaustive unit tests in `internal/compiler/compiler_test.go` covering JavaScript lexical minification, Wasm header validation, bundle repository lifecycle, route priority scoring, zero-copy streaming integrity, cancellation handling, and thread-safe concurrency.

### Changed
- Mounted `/api/compiler/` routes on HTTP mux and initialized default edge worker and wildcard route mappings during application bootstrap in `main.go`.
- Bumped application version to `0.7.0` across manifests (`package.json`, `web/package.json`) and security policy (`SECURITY.md`).

---

## [0.6.0] - 2026-10-14

### Added
- **Multi-Architecture Standalone Cross-Compilation Pipeline**: Built `scripts/build_artifacts.mjs` providing automated standalone packaging across four target architectures (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`) with binary size optimization, stripped symbols (`-s -w`), and SHA-256 / SHA-512 cryptographic checksum manifest generation (`dist/checksums.sha256`, `dist/artifacts_manifest.json`).
- **Automated GitHub Actions CI & Release Workflows**: Created `.github/workflows/release.yml` with automated tag-based releases, cryptographic binary checksum verification, and asset uploads; expanded `.github/workflows/ci.yml` with a multi-architecture cross-compilation matrix covering all target platforms.
- **Zero-Downtime Hot Reloading of Tenant Credentials**: Implemented `internal/config/config.go` and `internal/config/hot_reload.go` supporting background file watching with deterministic content hashing, cross-platform signal dispatching (SIGHUP on POSIX), and thread-safe dynamic synchronization with `broker.PoolManager` and `security.SecretVault` without service interruption.
- **Configuration & Hot Reload REST Endpoints**: Registered `/api/config/status` and `/api/config/reload` under administrative Bearer/JWT authentication in `main.go`, with automatic credential token redaction (`HasToken: true`) in diagnostic responses.
- **Config & Hot Reload Unit Test Suite**: Added 9 comprehensive unit tests in `internal/config/config_test.go` covering schema validation, clone isolation, manual reloads, file watcher polling, bad config recovery, pool/vault credential sync, HTTP endpoints, and high-concurrency thread safety.

### Changed
- Added `UpsertAccount`, `SyncAccounts`, and `RemoveAccount` methods to `internal/broker/pool.go` enabling non-disruptive runtime quota updates.
- Integrated `config.HotReloader` into `main.go` bootstrap sequence, reading `CF_BROKER_CONFIG_PATH` and binding hot-reload events to the pool manager and vault.
- Version bumped to `0.6.0` across project manifests (`package.json`, `web/package.json`) and security policy (`SECURITY.md`).

---

## [0.5.0] - 2026-10-14

### Added
- **Distributed Quota Synchronization & Real-time Webhooks**: Built `internal/quota/notifier.go` providing multi-channel notifications for Discord, Slack, and generic webhook endpoints when tenant resources reach 80% (Warning), 95% (Critical), and 100% (Exhausted) thresholds with 10-minute debouncing cooldowns.
- **Sliding-Window Rate Limiter**: Implemented `internal/quota/sliding_window.go` providing sub-bucketed in-memory timestamp rate limiting with configurable window durations, request limits, burst allowances, and automatic idle key memory reclamation.
- **Automated Account Recovery Probation**: Added `internal/quota/probation.go` managing tenant failure states with exponential backoff probation intervals (30s base up to 5m ceiling) and automated health recovery upon cooldown expiration.
- **Prometheus 0.0.4 Metrics Exporter**: Implemented `internal/quota/metrics.go` exporting standard Prometheus exposition format at `/metrics` with zero external dependencies, exposing account health states, active/cumulative leases, request telemetry, quota utilization ratios, rate limit rejections, and failover counts.
- **Embedded Quota & Telemetry Console**: Enhanced `web/dist/index.html` with real-time Prometheus scraper view, interactive 429 probation trigger/recovery controls, sliding-window limiter statistics, and multi-channel webhook configuration and testing panels.
- **Quota & Metrics Unit Test Suite**: Added 11 exhaustive unit tests in `internal/quota/quota_test.go` covering sliding-window concurrency, probation backoff and auto-recovery, Discord/Slack payload formatting, and Prometheus metrics generation.

### Changed
- Integrated Prometheus `/metrics` scraper and `/api/quota/*` management endpoints into `main.go`.
- Added automated background ticker in `main.go` for periodic probation sweep and tenant threshold evaluation.
- Added `ProbationUntil`, `SetAccountProbation`, and `RecoverAccount` methods to `internal/broker/pool.go`.

---

## [0.4.0] - 2026-10-14

### Added
- **Windows DPAPI Cryptographic Engine**: Built `internal/security/dpapi_windows.go` invoking OS-native `CryptProtectData` and `CryptUnprotectData` via `syscall` to encrypt Cloudflare API keys and account credentials at rest.
- **Cross-Platform AES-256-GCM Secret Vault**: Implemented `internal/security/vault.go` providing authenticated envelope encryption (`dpapi:...` and `aes-gcm:...`), master key derivation from environment variables (`CF_BROKER_MASTER_KEY`), and in-memory credential scrubbing (`Zeroize`).
- **HS256 JWT Authentication Engine**: Added `internal/security/jwt.go` generating and verifying RFC 7519 JSON Web Tokens with HMAC-SHA256 signatures, constant-time signature comparison, and thread-safe JTI denylist revocation.
- **Role-Based Access Control Middleware**: Implemented `internal/security/middleware.go` (`RequireAuth` and `RequireRole`) protecting administrative `/api/leases`, `/api/leases/revoke`, `/api/security/*`, and `/api/auth/*` routes with support for both dynamic Bearer JWTs and static bootstrap admin keys.
- **Interactive Security & Token Console**: Enhanced embedded web dashboard (`web/dist/index.html`) with real-time JWT token issuance, session authentication injection, live DPAPI/AES-GCM secret encryptor, and security posture telemetry.
- **Security Unit Test Suite**: Added 13 comprehensive unit tests in `internal/security/security_test.go` covering DPAPI round-trip, AES-GCM encryption, ciphertext tampering detection, buffer zeroization, JWT expiry, signature tampering, revocation denylist, and RBAC middleware enforcement.

### Changed
- Protected administrative endpoints (`/api/leases`, `/api/leases/revoke`, and `/api/security/*`) with `RequireAuth` middleware in `main.go`.
- Pre-seeded multi-account credentials into the encrypted vault during application bootstrap.

---

## [0.3.0] - 2026-10-14

### Added
- **Virtual R2 S3-Compatible Gateway**: Full S3 REST protocol implementation (`/s3/{bucket}/{key...}`) supporting `PUT`, `GET` (with HTTP `Range: bytes=start-end` support), `HEAD`, `DELETE`, and `ListObjectsV2` (`GET /s3/{bucket}`).
- **Striped Object Chunk Engine**: Built `internal/storage/chunker.go` to divide large file streams into configurable 2MB blocks, calculating SHA-256 block checksums and composite ETags while distributing blocks across healthy Cloudflare tenants in the pool.
- **Multi-Account Partitioned KV Cache**: Added `internal/storage/kv_cache.go` providing thread-safe key-value caching and metadata manifest storage partitioned across accounts via consistent FNV hashing with TTL eviction.
- **Quota Alert Webhook Engine**: Implemented `internal/storage/webhook.go` providing threshold monitors for 80%, 95%, and 100% tenant quota consumption with Slack and Discord webhook formatting and debounce cooldowns.
- **Interactive S3 Storage & KV Explorer**: Integrated real-time S3 object uploader, manifest inspector, and KV cache hit rate telemetry into the embedded dashboard (`web/dist/index.html`).
- **Storage Unit Test Suite**: Added `internal/storage/storage_test.go` verifying chunking, byte-range reassembly, KV cache partitioning, S3 XML serialization, and path traversal protections.

### Changed
- Registered `/s3/`, `/api/kv/`, and `/api/webhooks/` handlers in `main.go`.
- Added quota accounting helpers (`UpdateR2Storage`, `IncrementKVRead`, `IncrementWorkerUsed`, `ListHealthyAccountIDs`) to `PoolManager`.

---

## [0.2.0] - 2026-10-13

### Added
- **Automated Rate-Limit Interception**: Implemented `internal/proxy/proxy.go` providing transparent reverse proxying for `/proxy/{service}/*` with active HTTP 429 status detection.
- **Zero-Downtime Failover Engine**: Buffered request payload pipeline (up to 10MB) allowing transparent automatic retries across pooled Cloudflare tenants upon detecting 429, 502, 503, 504 status codes or network timeouts.
- **Hop-by-Hop Header Sanitization**: RFC 7230 compliant filtering of hop-by-hop headers (`Connection`, `Keep-Alive`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailers`, `Transfer-Encoding`, `Upgrade`) and dynamically declared connection tokens.
- **Broker Telemetry Headers**: Added `X-CF-Broker-Account` and `X-CF-Broker-Attempts` response headers to expose routing transparency and attempt counts to upstream consumers.
- **Interactive Route Tester UI**: Added a transparent failover proxy status panel and live endpoint verification card in the embedded web dashboard (`web/dist/index.html`).
- **Comprehensive Proxy Test Suite**: Unit tests in `internal/proxy/proxy_test.go` verifying transparent GET/POST proxying, automated 429 failover, full pool exhaustion, hop-by-hop stripping, and path validation.

### Changed
- Integrated reverse proxy handler registration (`/proxy/`) into `main.go`.

---

## [0.1.0] - 2026-10-13

### Added
- **Multi-Tenant Account Pool Manager**: Thread-safe Cloudflare tenant manager (`internal/broker/pool.go`) with quota tracking and health state management.
- **Dynamic Lease Engine**: Voucher allocation service (`internal/broker/lease.go`) issuing temporary scoped credentials and service endpoint bindings.
- **Edge Connectivity Checker**: Native Cloudflare edge latency and health verification engine (`internal/cloudflare/client.go`).
- **Embedded Web Console**: Single-binary dashboard with Cloudflare-themed glassmorphism interface packaged via Go `//go:embed`.
- **Zero-Dependency Architecture**: Pure standard library implementation with zero external runtime dependencies.
