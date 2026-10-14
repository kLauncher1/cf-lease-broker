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

## Release: v0.4.0 (Production Secrets Management & Authentication)
- [x] Hardware/OS-backed Windows DPAPI and cross-platform AES-256-GCM envelope encryption at rest.
- [x] HS256 JWT / Bearer token authentication engine with constant-time signature verification.
- [x] Role-Based Access Control (RBAC) middleware guarding administrative `/api/leases` and `/api/security` endpoints.
- [x] Ephemeral key derivation and in-memory credential scrubbing (`Zeroize`) preventing memory dumps.
- [x] Interactive Secrets Vault and JWT session inspector embedded into web console.

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

---

## Milestone 7: v1.1.0 (Zero-Trust Mutual TLS & Upstream Verification)
- [ ] Mutual TLS (mTLS) client certificate verification for upstream edge reverse proxy routes.
- [ ] Ephemeral CA token generation for inter-broker cluster peering.
- [ ] Hardware-backed TPM 2.0 key attestation and Windows certificate store integration.

---

## Milestone 8: v1.2.0 (Autonomous Cost Optimization & Billing Ledger)
- [ ] Granular per-lease compute and bandwidth metering with micro-cent cost attribution.
- [ ] Automated workload migration away from paid tier overages to standby free tier accounts.
- [ ] Exportable billing journals in CSV, JSONL, and OpenCost specification formats.

---

## Milestone 9: v1.3.0 (Intelligent Edge Health Radar & Anycast Routing)
- [ ] Synthetic probe pinging all 330+ Cloudflare edge PoPs for real-time latency map construction.
- [ ] Sub-millisecond geographic tenant selection prioritizing nearest healthy Cloudflare colo.
- [ ] BGP route change detection and dynamic lease migration before network packet loss occurs.
