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

## Release: v0.5.0 (Distributed Quota Synchronization & Real-time Webhooks)
- [x] Multi-channel Discord/Slack/Custom webhook notifications on tenant quota thresholds (80%, 95%, 100%) with 10-minute debouncing.
- [x] Sub-bucketed sliding-window rate limit tracking with burst allowance and memory reclamation.
- [x] Automated account recovery probation with exponential backoff and cooldown auto-restoration.
- [x] Prometheus 0.0.4 metrics exporter endpoint (`/metrics`) exposing lease counts, quotas, rate limit hits, and proxy telemetry.
- [x] Embedded console quota monitor, probation manager, and webhook diagnostic test runner.

---

## Release: v0.6.0 (Cross-Platform Standalone Artifacts & CI/CD Pipeline)
- [x] Multi-architecture standalone binary packaging (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`).
- [x] GitHub Actions release workflow with automated binary signing and SHA-256 checksum generation.
- [x] Zero-downtime hot reloading of account credentials configuration via file watcher and signals.

---

## Release: v0.7.0 (Edge Worker Bundle Virtual Compiler & Dynamic Route Injection)
- [x] On-the-fly JavaScript/Wasm bundle minification and tenant packaging pipeline.
- [x] Multi-zone route pattern compiler mapping arbitrary domain endpoints to tenant clusters.
- [x] Zero-copy direct streaming pipeline for oversized HTTP responses.

---

## Milestone 8: v1.0.0 (High-Availability Raft State Machine for Distributed Broker Clusters)
- [ ] Multi-node Raft consensus layer for cross-region lease broker synchronization.
- [ ] Automatic leader election and peer discovery via DNS SRV records.
- [ ] Distributed transaction journal with zero-data-loss failover guarantees.

---

## Milestone 9: v1.1.0 (Zero-Trust Mutual TLS & Upstream Verification)
- [ ] Mutual TLS (mTLS) client certificate verification for upstream edge reverse proxy routes.
- [ ] Ephemeral CA token generation for inter-broker cluster peering.
- [ ] Hardware-backed TPM 2.0 key attestation and Windows certificate store integration.

---

## Milestone 10: v1.2.0 (Autonomous Cost Optimization & Billing Ledger)
- [ ] Granular per-lease compute and bandwidth metering with micro-cent cost attribution.
- [ ] Automated workload migration away from paid tier overages to standby free tier accounts.
- [ ] Exportable billing journals in CSV, JSONL, and OpenCost specification formats.

---

## Milestone 11: v1.3.0 (Intelligent Edge Health Radar & Anycast Routing)
- [ ] Synthetic probe pinging all 330+ Cloudflare edge PoPs for real-time latency map construction.
- [ ] Sub-millisecond geographic tenant selection prioritizing nearest healthy Cloudflare colo.
- [ ] BGP route change detection and dynamic lease migration before network packet loss occurs.

---

## Milestone 12: v1.4.0 (Adaptive Zero-Copy Compression & Transcoding)
- [ ] Dynamic payload inspection selecting optimal compression algorithm (Brotli, Zstandard, gzip) based on client Accept-Encoding.
- [ ] Real-time byte streaming through pre-allocated compressor buffers with zero intermediate heap allocations.
- [ ] Dynamic dictionary training for tenant API JSON payloads reducing bandwidth by up to 85%.

---

## Milestone 13: v1.5.0 (Edge Key-Value Geo-Replication Mesh)
- [ ] Multi-master asynchronous KV replication across active broker nodes with vector clock conflict resolution.
- [ ] CRDT primitives for distributed counter and set synchronization across edge clusters.
- [ ] Read-your-writes and monotonic-read consistency guarantees for regional tenant workers.
