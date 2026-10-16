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

## Release: v0.8.0 (High-Availability Raft State Machine for Distributed Broker Clusters)
- [x] Multi-node Raft consensus engine with randomized election timers (150-300ms) and majority quorum voting.
- [x] Write-Ahead Log (WAL) transaction journal with monotonic indexing and SHA-256 integrity verification.
- [x] Automatic peer discovery via DNS SRV records with static fallback and latency tracking.
- [x] Broker state machine replicating lease grants, revocations, account profiles, and quota changes.
- [x] Full REST API endpoints (`/raft/*`) and interactive glassmorphism cluster monitor panel.
- [x] Comprehensive unit and multi-node failover test suites with zero external dependencies.

---

## Release: v0.9.0 (Zero-Trust Mutual TLS & Upstream Verification)
- [x] Mutual TLS (mTLS) client certificate verification for upstream edge reverse proxy routes.
- [x] Ephemeral CA token generation for inter-broker cluster peering.
- [x] Hardware-backed TPM 2.0 key attestation and Windows certificate store integration.

---

## Release: v0.10.0 (Autonomous Cost Optimization & Billing Ledger)
- [x] Granular per-lease compute and bandwidth metering with micro-cent cost attribution.
- [x] Automated workload migration away from paid tier overages to standby free tier accounts.
- [x] Exportable billing journals in CSV, JSONL, and OpenCost specification formats.

---

## Release: v0.11.0 (Intelligent Edge Health Radar & Anycast Routing)
- [x] Synthetic probe pinging all 330+ Cloudflare edge PoPs for real-time latency map construction.
- [x] Sub-millisecond geographic tenant selection prioritizing nearest healthy Cloudflare colo.
- [x] BGP route change detection and dynamic lease migration before network packet loss occurs.

---

## Release: v0.12.0 (Adaptive Zero-Copy Compression & Transcoding)
- [ ] Dynamic payload inspection selecting optimal compression algorithm (Brotli, Zstandard, gzip) based on client Accept-Encoding.
- [ ] Real-time byte streaming through pre-allocated compressor buffers with zero intermediate heap allocations.
- [ ] Dynamic dictionary training for tenant API JSON payloads reducing bandwidth by up to 85%.

---

## Release: v0.13.0 (Edge Key-Value Geo-Replication Mesh)
- [ ] Multi-master asynchronous KV replication across active broker nodes with vector clock conflict resolution.
- [ ] CRDT primitives for distributed counter and set synchronization across edge clusters.
- [ ] Read-your-writes and monotonic-read consistency guarantees for regional tenant workers.

---

## Release: v0.14.0 (Multi-Cluster Gateway Federation & WAN Mesh)
- [ ] Cross-cloud and multi-datacenter broker federation over secure WAN tunnels.
- [ ] Dynamic boundary route advertisement with WireGuard overlay mesh.
- [ ] Global tenant lease routing with localized data sovereignty enforcement.

---

## Release: v0.15.0 (Zero-Knowledge Edge Proof Verifier)
- [ ] Cryptographic verification of tenant worker isolation and quota attestations without revealing underlying API credentials.
- [ ] Succinct non-interactive zero-knowledge proofs for trustless edge verification.

---

## Release: v0.16.0 (Autonomous Multi-Tenant eBPF DDoS Scrubbing & Edge Mitigation)
- [ ] Line-rate XDP packet filter dropping volumetric SYN floods and amplification attacks at zero CPU cost.
- [ ] Stateful IP reputation scoring and automatic challenge token issuance for anomalous client streams.
- [ ] Microsecond-scale mitigation telemetry export to edge monitoring mesh.

---

## Release: v0.17.0 (Edge Hardware Secure Enclave Intel SGX & AMD SEV Memory Attestation)
- [ ] Hardware-isolated secure memory enclave shielding cryptographic keys from host OS compromise.
- [ ] Remote attestation quotes cryptographically verified during inter-broker lease peering.
- [ ] Confidential computing runtime for zero-trust worker bundle execution.

---

## Release: v0.18.0 (Quantum Entropy Beacon & Hardware RNG Attestation)
- [ ] Quantum entropy harvesting from CPU TRNG hardware pipelines and jitter entropy sources.
- [ ] NIST SP 800-90B continuous health testing and cryptographic randomness extraction for lease tokens.
- [ ] Zero-trust entropy attestation broadcast across regional broker mesh instances.

---

## Release: v0.19.0 (Dynamic Line-Rate BGP Anycast Prefix Broadcaster)
- [ ] Autonomous BGP session peering with upstream Tier-1 transit providers for custom Anycast IP prefixes.
- [ ] Line-rate prefix withdrawal and traffic diversion within 5 milliseconds of PoP partition.
- [ ] Dynamic Community tagging routing tenant requests based on real-time transit pricing and peering agreements.
