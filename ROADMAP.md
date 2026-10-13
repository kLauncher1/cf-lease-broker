# Product Roadmap

This document outlines the milestones and engineering priorities for `cf-lease-broker`.

---

## Release: v0.1.0 (Foundation)
- [x] Multi-tenant Cloudflare account pool manager with real-time quota telemetry.
- [x] Dynamic lease engine issuing scoped resource vouchers (`lse_...`) and tokens.
- [x] Immediate lease revocation and failover rerouting logic.
- [x] Embedded Cloudflare-themed glassmorphism dashboard (`web/dist/index.html`).
- [x] Full unit test coverage for pool and lease engine routines.

---

## Release: v0.2.0 (Automated Rate-Limit Interception & Failover)
- [x] Direct Cloudflare API reverse proxy intercepting 429 Too Many Requests responses.
- [x] Automatic zero-downtime hot-swap of accounts with buffered request retries.
- [x] Route pattern `/proxy/{service}/*` with transparent endpoint dispatching.
- [x] Broker telemetry headers (`X-CF-Broker-Account`, `X-CF-Broker-Attempts`).
- [x] RFC 7230 hop-by-hop header sanitization and stream multiplexing.
- [x] Interactive proxy route tester integrated into embedded dashboard.

---

## Release: v0.3.0 (KV & R2 Multi-Account Virtualization)
- [x] Virtual R2 S3-compatible gateway (`/s3/{bucket}/{key...}`) pooling storage buckets across accounts.
- [x] Unified KV read/write cache partitioner with consistent hashing and TTL eviction.
- [x] Automatic 2MB chunk striping with SHA-256 integrity and HTTP Range request support.
- [x] Outgoing Discord/Slack webhook notifications on account quota exhaustion (80%, 95%, 100%).
- [x] Interactive S3 storage explorer & KV cache inspector in embedded dashboard.

---

## Milestone 4: v0.4.0 (Production Secrets Management & Authentication)
- [ ] DPAPI and environment variable encryption for Cloudflare API keys at rest.
- [ ] JWT / Bearer token authentication for administrative `/api/leases` endpoints.
- [ ] Mutual TLS (mTLS) client certificate verification for zero-trust microservice communication.

---

## Milestone 5: v0.5.0 (Distributed Quota Synchronization & Real-time Webhooks)
- [ ] Distributed sliding-window rate limit tracking and automated account recovery probation.
- [ ] Prometheus metrics exporter endpoint (`/metrics`) exposing lease counts, latency percentiles, and tenant health.
- [ ] Zero-downtime hot reloading of account credentials configuration via SIGHUP/file watcher.

---

## Milestone 6: v1.0.0 (Production Hardening & Cross-Platform Distribution)
- [ ] Multi-architecture standalone binary packaging (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`).
- [ ] Multi-node Raft consensus state machine for regional cluster synchronization.
- [ ] Formal cryptographic audit and automated fuzz testing pipeline.
