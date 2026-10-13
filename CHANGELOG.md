# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
