# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
