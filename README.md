<div align="center">

# ⚡ CF-Lease-Broker

**Multi-account Cloudflare resource pooling & dynamic token lease broker for Workers, KV, and R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-quick-start)

<sub>Crafted by **kLauncher1** · High-availability edge resource pooling, secrets vault, and dynamic tenant leasing.</sub>

</div>

---

## ⚡ Highlights

- **Autonomous Cost Optimization & Multi-Tenant Billing Ledger**: Granular compute, request, and storage metering with micro-cent cost attribution ($1.00 = 1,000,000 micro-cents) based on official Cloudflare pricing rates across Workers, KV, and R2, combined with autonomous workload migration away from paid tier overages to standby free tier accounts (`internal/billing/`).
- **OpenCost 1.0 & Multi-Format Financial Journals**: Out-of-the-box billing journal exports in RFC 4180 CSV, newline-delimited JSON (JSONL), and standard OpenCost 1.0 specification formats (`/api/billing/ledger?format=opencost`) for enterprise edge cloud financial engineering.
- **Zero-Trust Mutual TLS (mTLS) & Client Certificate Verification**: Comprehensive X.509 client certificate chain verification, certificate revocation list (CRL) denylist checking, validity window enforcement, wildcard Subject Alternative Name (SAN) matching (`*.domain.com`), and common name / issuer access policies for upstream reverse proxy routes (`internal/mtls/verifier.go`).
- **Ephemeral Certificate Authority (CA) & Peering Mesh**: Zero-dependency in-memory ECDSA P-256 root certificate generation, dynamic client certificate issuance, CRL serial tracking, and cryptographically signed inter-broker cluster peering tokens (`internal/mtls/ca.go`).
- **Hardware-Backed TPM 2.0 Attestation & Windows Certificate Store**: Leverages Windows Cryptographic Next Generation (CNG) `ncrypt.dll` to query Microsoft Platform Crypto Provider for hardware TPM 2.0 presence, inspect the Windows system certificate store (`crypt32.dll`), and generate deterministic platform configuration register (PCR) quote measurements (`internal/mtls/tpm_windows.go`).
- **High-Availability Raft Consensus Engine**: Multi-node Raft consensus cluster with randomized election timers (150-300ms), majority quorum leader election, Write-Ahead Log (WAL) journal with monotonic indexing and SHA-256 verification, and DNS SRV dynamic peer discovery (`net.LookupSRV`) with zero external dependencies (`internal/raft/`).
- **Edge Worker Virtual Compiler & Packaging**: Fast in-memory lexical minifier and validator for JavaScript/Wasm Edge Worker bundles with comment stripping, syntax verification, and SHA-256 content hashing (`internal/compiler/bundle.go`).
- **Multi-Zone Dynamic Route Injection**: High-performance wildcard route compiler matching arbitrary domains (`*.domain.com/path/*`) with priority weighting and dynamic route injection tables (`internal/compiler/router.go`).
- **Zero-Copy Direct Streaming Pipeline**: Low-latency edge response streaming utilizing pre-allocated 64KB buffer pools (`sync.Pool`), immediate chunk flushing (`http.Flusher`), and client disconnection detection (`internal/compiler/stream.go`).
- **Multi-Architecture Standalone Artifacts**: Cross-compilation pipeline packaging stripped standalone executables for `linux/amd64`, `linux/arm64`, `darwin/arm64`, and `windows/amd64` with automated SHA-256 integrity checksum generation (`scripts/build_artifacts.mjs`).
- **Zero-Downtime Hot Reloading**: Dynamic configuration and credentials reloading via file modification watching and OS signals (`SIGHUP` on POSIX) that synchronizes pooled tenants and the secrets vault without process restarts.
- **Multi-Account Quota Aggregation**: Pools multiple free/pro Cloudflare accounts into a unified virtual compute cluster, overcoming single-account daily request boundaries.
- **Production Secrets Vault**: Envelope encryption at rest using Windows DPAPI (`CryptProtectData`) and cross-platform AES-256-GCM with memory zeroization (`Zeroize`).
- **HS256 JWT Authentication & RBAC**: Microservice and administrative authentication engine with HMAC-SHA256 tokens, constant-time verification, and thread-safe JTI revocation denylists.
- **Virtual R2 S3-Compatible Gateway**: S3 REST protocol endpoints (`/s3/{bucket}/{key...}`) that stripe object storage across pooled R2 buckets with automatic 2MB chunking and SHA-256 integrity verification.
- **Multi-Account Partitioned KV Cache**: Thread-safe partitioned KV storage layer with consistent FNV hashing, LRU memory caching, and TTL eviction.
- **Prometheus 0.0.4 Metrics Exporter**: Native standard Prometheus `/metrics` scraper endpoint exporting lease counts, tenant quotas, rate limit rejections, and proxy telemetry with zero external dependencies.
- **Sliding-Window Rate Limiting & Automated Probation**: Sub-bucketed in-memory sliding window rate tracker with burst allowance and exponential backoff probation state machine for rate-limited accounts.
- **Multi-Channel Quota Webhooks**: Rich Discord embeds, Slack Block Kit alerts, and generic JSON notifications triggered at 80% (Warning), 95% (Critical), and 100% (Exhausted) thresholds with 10-minute debouncing.
- **Automated Rate-Limit Interception & Zero-Downtime Failover**: Intercepts HTTP 429 Too Many Requests and gateway errors in real time, automatically rerouting inflight requests to the next healthy account in the pool.
- **Transparent Reverse Proxy**: Forward traffic seamlessly via `/proxy/{service}/*` with buffered body retries and RFC 7230 hop-by-hop header stripping.
- **Dynamic Resource Leasing (Vouchers)**: Issues short-lived, scoped API tokens and endpoints (`lse_...`) for internal microservices, third-party callers, or test suites.
- **Single Executable Deployment**: The Cloudflare-themed glassmorphism management dashboard is baked directly into the Go binary via `//go:embed`.
- **Lightweight Memory Footprint**: Runs persistently under **12 MB of RAM** with zero external runtime dependencies.

---

## 🚀 Quick Start

### 1. Download & Run

```bash
# Clone the repository
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# Run directly
go run .
```

The management console will be live at:
👉 **`http://localhost:9081`**

### 2. Cross-Compile Standalone Multi-Architecture Artifacts

```bash
# Build standalone binaries for linux/amd64, linux/arm64, darwin/arm64, windows/amd64
node scripts/build_artifacts.mjs --clean

# Verify cryptographic SHA-256 checksums
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Architecture & Resource Pooling

```text
┌────────────────────────────────────────────────────────────────────────┐
│                            cf-lease-broker                             │
│                         Single Go Executable                           │
├───────────────────┬───────────────────┬────────────────────────────────┤
│   internal/raft   │  internal/broker  │       internal/storage         │
│ • Raft Node Core  │ • Account Pool    │ • Virtual S3 Gateway (/s3/...) │
│ • WAL Journal     │ • Dynamic Leases  │ • 2MB Striped Chunker          │
│ • DNS SRV Peering │ • Quota Balancer  │ • Partitioned KV Cache Layer   │
│ • Broker FSM Sync │ • Auto Failover   │ • Multi-Channel Webhooks       │
├───────────────────┼───────────────────┼────────────────────────────────┤
│   internal/mtls   │ internal/security │       internal/compiler        │
│ • Ephemeral CA    │ • Windows DPAPI   │ • Virtual JS Minifier / Wasm   │
│ • Client Cert X509│ • AES-256-GCM     │ • Multi-Zone Route Compiler    │
│ • TPM 2.0 Attest  │ • HS256 JWT / RBAC│ • 64KB Zero-Copy Stream Pipe   │
│ • Peering Tokens  │ • Zeroize Memory  │ • Tenant Bundle Repository     │
├───────────────────┴───────────────────┴────────────────────────────────┤
│                         internal/quota & config                        │
│  • Prometheus (/metrics) • Sliding Window • Zero-Downtime Hot Reload   │
├────────────────────────────────────────────────────────────────────────┤
│                         net/http HTTP Engine                           │
│   • /api/mtls/* (Cert/Verify/TPM)      • /api/compiler/*               │
│   • /raft/* (Vote/Append/Status)       • /proxy/{svc}/* (mTLS Guarded) │
│   • /metrics         • /api/auth/*     • /s3/{bkt}/{key}               │
│   • /api/accounts    • /api/security   • /api/config/* (Auth Guarded)  │
├────────────────────────────────────────────────────────────────────────┤
│                         Embedded Web Dashboard                         │
│                   (Glassmorphism UI via //go:embed)                    │
│                  web/dist/index.html -> Port 9081                      │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 🔒 Secrets Management & Authentication

`cf-lease-broker` provides enterprise-grade secret storage and authentication out of the box with zero third-party dependencies:

### 1. Envelope Encryption Vault (`internal/security/vault.go`)
- **Windows DPAPI Integration**: On Windows systems, secrets are protected via Windows DPAPI (`CryptProtectData`), securing API keys with OS-level credentials (`dpapi:<base64>`).
- **AES-256-GCM Envelope**: On non-Windows platforms (or when specified), secrets are encrypted using authenticated AES-256-GCM with unique 96-bit random nonces (`aes-gcm:<nonce>:<ciphertext>`).
- **Memory Scrubbing**: Sensitive byte buffers are immediately wiped after use using the `Zeroize` memory scrubbing pattern (`for i := range b { b[i] = 0 }`).

### 2. JWT & RBAC Middleware (`internal/security/middleware.go`)
- **HS256 Standard Tokens**: Issues and verifies HMAC-SHA256 JWTs with constant-time signature verification (`crypto/subtle.ConstantTimeCompare`) to prevent timing side-channel attacks.
- **Revocation Denylist**: Supports immediate token revocation by JTI (`POST /api/auth/revoke`) with automatic TTL expiration sweep.
- **Guarded Administrative APIs**: Administrative endpoints (`/api/leases`, `/api/leases/revoke`, and `/api/security/*`) require valid `Authorization: Bearer <token>` or root admin bootstrap keys.

```bash
# 1. Issue an Operator JWT Token
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. Call Protected Leases Endpoint with Bearer Token
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 Virtual R2 S3-Compatible Gateway (`/s3/{bucket}/{key...}`)

The broker exposes standard AWS S3-compatible endpoints allowing existing S3 SDKs, microservices, and CLI tools to store objects across pooled Cloudflare R2 accounts without exceeding individual tenant storage quotas:

### Core Capabilities:
1. **Striped Chunking**: Inbound payloads exceeding 2MB are automatically split into sequence blocks (`chk_<hash>_<index>`) and distributed across healthy accounts in the pool.
2. **Integrity Checksums**: Each chunk includes an individual SHA-256 checksum, and objects receive standard multi-part compatible composite ETags.
3. **HTTP Range Requests**: Supports `Range: bytes=start-end` headers for video streaming, resume downloads, and large asset partial reads.
4. **ListObjectsV2**: Conforms to standard S3 XML `ListBucketResult` responses.

### S3 CLI & Curl Examples:

```bash
# 1. Store an object across pooled R2 buckets
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. Retrieve object
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. Partial byte range request
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf

# 4. List bucket objects (S3 XML)
curl http://localhost:9081/s3/assets
```

---

## 🔄 Transparent Failover Reverse Proxy (`/proxy/{service}/*`)

The broker provides a transparent edge reverse proxy at `/proxy/{service}/*`. Callers point their HTTP requests to this endpoint without needing to know which tenant account currently holds available quota.

### How Failover Works:
1. **Target Resolution**: The incoming request `{service}` and subpath are resolved against the pooled account with the highest remaining quota (`SelectOptimalAccount`).
2. **Payload Buffering**: Request bodies are buffered in memory (up to 10MB) using streaming readers so that retries can replay the complete payload without data loss.
3. **429 / Gateway Interception**: If the target account responds with HTTP `429 Too Many Requests`, `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout`, or encounters a connection drop:
   - The broker immediately marks that tenant as unhealthy via `TriggerFailover(accountID)`.
   - The request is automatically retried against the next healthy account in the pool.
4. **Hop-by-Hop Sanitization**: RFC 7230 hop-by-hop headers (`Connection`, `Keep-Alive`, `Upgrade`, `TE`, `Trailers`, etc.) are stripped in both directions.
5. **Telemetry Headers**: Successful responses include inspection headers:
   - `X-CF-Broker-Account: {account_id}` (tenant that fulfilled the request)
   - `X-CF-Broker-Attempts: {count}` (number of attempts required)

---

## 🔄 Zero-Downtime Hot Reloading (`internal/config/`)

`cf-lease-broker` supports dynamic credential rotation and account pool updates without restarting the binary or dropping active HTTP connections:

### Hot Reload Mechanics:
1. **File Watcher Loop**: Periodically polls the configuration file (`CF_BROKER_CONFIG_PATH` or `config.json`) and evaluates content changes via SHA-256 deterministic digests to avoid spurious reloads.
2. **POSIX Signal Handling**: Catches `SIGHUP` (Unix/Linux/macOS) to trigger immediate synchronous configuration validation and reloads.
3. **Atomic State Swap & Fallback Safety**: If a reloaded file contains invalid JSON, negative limits, or duplicate tenant IDs, the reload fails safely with an error log while preserving the active configuration untouched.
4. **Credential & Pool Sync**: Successfully reloaded tenants are synchronized directly with `PoolManager` (`SyncAccounts`) and encrypted into `SecretVault` (`StoreCredential`).

### Configuration File Format (`config.json`):
```json
{
  "server_port": "9081",
  "version": "0.6.0",
  "accounts": [
    {
      "id": "cf-tenant-alpha",
      "alias": "Edge Cluster Primary",
      "plan": "Free Tier (100k/day)",
      "worker_limit": 100000,
      "api_token": "cftok_prod_alpha_secret"
    },
    {
      "id": "cf-tenant-bravo",
      "alias": "Edge Cluster Secondary",
      "plan": "Free Tier (100k/day)",
      "worker_limit": 100000,
      "api_token": "cftok_prod_bravo_secret"
    }
  ]
}
```

---

## 🚀 Edge Worker Virtual Compiler & Dynamic Route Injection

The broker features an integrated compilation and routing engine for multi-tenant Edge Worker scripts and WebAssembly modules:

### 1. In-Memory Lexical Minification & Packaging (`internal/compiler/bundle.go`)
- **Lexical Tokenizer**: Strips single-line (`//`) and multi-line (`/* */`) comments, eliminates redundant whitespace, normalizes punctuation spacing, and validates Cloudflare Worker module export signatures (`export default { fetch(...) }`, `addEventListener('fetch')`).
- **WebAssembly Validation**: Verifies 4-byte magic headers (`\x00asm`) and version compliance before tenant packaging.
- **Content Hashing**: Generates deterministic SHA-256 digests for tenant bundle versioning and audit trails.

### 2. Multi-Zone Wildcard Route Compiler (`internal/compiler/router.go`)
- **Arbitrary Domain Mapping**: Maps endpoints across wildcard subdomains (`*.domain.com/*`), exact hosts (`api.domain.com/v1/*`), or global fallback rules (`*/*`).
- **Priority-Weighted Trie**: Evaluates incoming request host and path with specificity scoring (exact host > wildcard subdomain > global; longer path prefix > short prefix), routing traffic dynamically without configuration reloads.

### 3. Zero-Copy Direct Streaming Pipeline (`internal/compiler/stream.go`)
- **Buffer Pool**: Utilizes pre-allocated 64KB byte slices (`sync.Pool`) to eliminate heap allocations during high-throughput proxy transfers.
- **Immediate Socket Flushing**: Leverages `http.Flusher` to stream response chunks to clients in real time while disabling intermediary caching via `X-Accel-Buffering: no`.
- **Cancellation Detection**: Hooks into `context.Context` to terminate upstream worker streams immediately upon client disconnects, preventing socket leakages.

---

## 🤝 High-Availability Raft State Machine for Distributed Broker Clusters

`cf-lease-broker` embeds a zero-dependency Raft consensus engine (`internal/raft/`) enabling multi-region broker clustering, zero-data-loss lease state replication, and automatic leader failover:

### Core Architecture & Consensus Primitives:
1. **Quorum Leader Election & Randomized Timers**: Nodes transition across Follower, Candidate, and Leader states. Randomized election timeouts (150–300ms) prevent split-vote deadlocks, while a 50ms heartbeat ticker sustains leader authority across majority quorums `((N/2) + 1)`.
2. **Write-Ahead Log (WAL) Journal (`internal/raft/journal.go`)**: Enforces monotonic transaction indexing with SHA-256 integrity checksums. Follower log consistency is validated before appending, automatically truncating conflicting uncommitted entries. Supports optional append-only disk persistence (`raft_journal.wal`).
3. **Dynamic Peer Discovery via DNS SRV (`internal/raft/discovery.go`)**: Automatically resolves cluster peers via RFC 2782 DNS SRV records (`_raft._tcp.<domain>`) with graceful fallback to static environment mappings (`CF_RAFT_PEERS`) and runtime manual registration (`POST /raft/peers`).
4. **Broker Finite State Machine (`internal/raft/fsm_broker.go`)**: Replicates transaction commands deterministically across all nodes:
   - `GRANT_LEASE`: Replicates active lease vouchers across the cluster.
   - `REVOKE_LEASE`: Propagates lease revocation to all peer engines.
   - `SYNC_ACCOUNT`: Synchronizes tenant profiles and quota limits.
   - `UPDATE_QUOTA`: Propagates incremental daily worker requests, R2 storage deltas, and KV read counts.
   - `NOOP`: Consensus barrier probes for cluster readiness checks.

### Cluster Topology Flow:

```text
 Client / Microservice
        │
        ▼ (POST /raft/propose or /api/leases)
┌───────────────────────────────────────┐
│           Leader Broker Node          │
│  ┌───────────────┐ ┌────────────────┐ │
│  │  WAL Journal  │ │   Broker FSM   │ │
│  └───────┬───────┘ └────────────────┘ │
└──────────┼────────────────────────────┘
           │ AppendEntries (50ms Heartbeat / Log Replication)
     ┌─────┴────────────────┐
     ▼                      ▼
┌──────────────────┐  ┌──────────────────┐
│ Follower Node 2  │  │ Follower Node 3  │
│ (Quorum Ack 2/3) │  │ (Quorum Ack 3/3) │
└──────────────────┘  └──────────────────┘
```

### Raft Cluster Curl Examples:

```bash
# 1. Inspect local node status and cluster topology
curl http://localhost:9081/raft/status

# 2. Query peer health and round-trip latency
curl http://localhost:9081/raft/peers

# 3. Manually register a new peer node
curl -X POST http://localhost:9081/raft/peers \
  -H "Content-Type: application/json" \
  -d '{"id": "node-2", "address": "http://127.0.0.1:9082"}'

# 4. Propose a state machine transaction to the cluster
curl -X POST http://localhost:9081/raft/propose \
  -H "Content-Type: application/json" \
  -d '{
    "type": "GRANT_LEASE",
    "data": {
      "lease_id": "lse_edge_distributed_01",
      "account_id": "cf-tenant-alpha",
      "target_service": "auth-service",
      "resource_type": "Workers"
    }
  }'

# 5. Inspect replicated WAL journal entries and SHA-256 checksums
curl http://localhost:9081/raft/journal
```

---

## 🛡️ Zero-Trust Mutual TLS (mTLS) & Hardware Attestation

`cf-lease-broker` incorporates a complete zero-trust mutual TLS (mTLS) engine, dynamic in-memory Ephemeral Certificate Authority (CA), and hardware-backed TPM 2.0 attestation:

### 1. Ephemeral Certificate Authority & Dynamic Issuance (`internal/mtls/ca.go`)
- **In-Memory Root Anchor**: Dynamically bootstraps an ECDSA P-256 root Certificate Authority (`x509.IsCA = true`) without external tools or OpenSSL dependencies.
- **Client & Peer Certificates**: Issues short-lived, cryptographically signed X.509 client and peer certificates with custom Common Names, validity windows, and Subject Alternative Names (SANs).
- **CRL Revocation**: Maintains a fast, thread-safe Certificate Revocation List (CRL) indexed by hexadecimal serial numbers with O(1) denylist lookups.
- **Signed Peering Vouchers**: Generates and validates ECDSA-SHA256 peering tokens for zero-trust inter-broker cluster communication.

### 2. Client Certificate Verification & Policy Enforcement (`internal/mtls/verifier.go`)
- **Trust Chain Verification**: Validates inbound X.509 client certificates against the root CA pool (`x509.CertPool`).
- **Policy Enforcement**: Enforces wildcard SAN matching (`*.edge.cluster.local`, CIDR blocks), allowed Common Names, allowed Issuers, and strict certificate expiration windows.
- **Transparent Edge Header Extraction**: Extracts certificates from direct TLS connections (`r.TLS.PeerCertificates`) or standard edge forwarding headers (`X-Client-Cert-Pem`, `X-Forwarded-Client-Cert`, `X-Client-Cert-Der`).
- **Reverse Proxy Guarding**: When `CF_REQUIRE_MTLS=true` is enabled, all requests through `/proxy/{service}/*` must present a verified client certificate.

### 3. Hardware TPM 2.0 Attestation (`internal/mtls/tpm_windows.go`)
- **Windows CNG Integration**: Queries Microsoft Cryptographic Next Generation (`ncrypt.dll`) for the "Microsoft Platform Crypto Provider" to verify TPM 2.0 presence.
- **Windows System Certificate Store**: Inspects and validates machine/user certificate stores via `crypt32.dll` (`CertOpenSystemStoreW`).
- **Platform Quotes**: Generates deterministic Platform Configuration Register (PCR) digests and endorsement key measurements for audit trails.

### mTLS & TPM Curl Examples:

```bash
# 1. Inspect mTLS status, active Root CA, and TPM attestation state
curl http://localhost:9081/api/mtls/status

# 2. Download the Root CA certificate in PEM format
curl http://localhost:9081/api/mtls/ca/cert -o root-ca.pem

# 3. Issue an ephemeral client certificate
curl -X POST http://localhost:9081/api/mtls/ca/issue \
  -H "Content-Type: application/json" \
  -d '{
    "common_name": "worker-edge-alpha",
    "sans": ["*.edge.broker.local", "127.0.0.1"],
    "validity_hours": 24
  }'

# 4. Verify an X.509 PEM certificate against active policy
curl -X POST http://localhost:9081/api/mtls/verify \
  -H "Content-Type: application/json" \
  -d '{"certificate_pem": "-----BEGIN CERTIFICATE-----\n..."}'

# 5. Revoke a certificate serial number
curl -X POST http://localhost:9081/api/mtls/revoke \
  -H "Content-Type: application/json" \
  -d '{"serial_number": "7b8a1c9d2e"}'

# 6. Query hardware TPM 2.0 attestation quote
curl http://localhost:9081/api/mtls/tpm/attest
```

---

## 💰 Autonomous Cost Optimization & Multi-Tenant Billing Ledger

`cf-lease-broker` provides a high-precision, multi-tenant billing engine and cost optimizer designed to track consumption down to micro-cents and autonomously prevent cloud billing overages:

### 1. Granular Micro-Cent Cost Attribution (`internal/billing/meter.go`)
- **Micro-Cent Precision**: Tracks resource usage with integer micro-cent math ($1.00 = 1,000,000 micro-cents) to completely eliminate floating-point rounding drift.
- **Full Cloudflare Pricing Model**: Pre-calibrated with official Cloudflare rates:
  - Workers Requests: $0.30 per 1M requests (300,000 micro-cents)
  - Workers CPU Duration: $0.02 per 1M CPU milliseconds (20,000 micro-cents)
  - KV Reads: $0.50 per 1M reads (500,000 micro-cents)
  - KV Writes: $5.00 per 1M writes (5,000,000 micro-cents)
  - R2 Storage: $0.015 per GB-month (15,000 micro-cents)
  - R2 Class A Operations: $4.50 per 1M operations (4,500,000 micro-cents)
  - R2 Class B Operations: $0.36 per 1M operations (360,000 micro-cents)
  - Egress Bandwidth: $0.00 (Cloudflare zero egress guarantee)
- **Daily Free-Tier Allowance**: Tracks daily free allowances (100,000 free Workers requests and 100,000 free KV reads per free account) with automatic UTC midnight rollover, attributing $0 cost while accumulating estimated savings.

### 2. Autonomous Workload Migration (`internal/billing/optimizer.go`)
- **Proactive Overage Detection**: Continuously audits account usage against configurable overage thresholds (default 80%).
- **Standby Free Tier Prioritization**: When a paid or heavily utilized account approaches its quota or budget ceiling, the optimizer autonomously re-routes traffic or sets cooldown probation, directing subsequent leases and proxy traffic to healthy standby free tier accounts with unused daily quotas.
- **Cumulative Savings Tracking**: Quantifies and reports the exact micro-cents saved by steering traffic away from paid tier overages.

### 3. OpenCost 1.0 & Multi-Format Financial Export (`internal/billing/journal.go`)
- **OpenCost 1.0 Specification**: Exports tenant billing allocations directly matching the OpenCost edge compute specification (`/api/billing/ledger?format=opencost`).
- **RFC 4180 CSV Export**: Generates comma-separated billing records for spreadsheet analysis and finance audits (`/api/billing/ledger?format=csv`).
- **Newline-Delimited JSON (JSONL)**: Streams structured log records for ingestion into BigQuery, Datadog, or Elasticsearch (`/api/billing/ledger?format=jsonl`).

### Billing & Cost Curl Examples:

```bash
# 1. Query cluster-wide cost summary and tenant breakdown
curl http://localhost:9081/api/billing/summary

# 2. Export billing ledger in OpenCost 1.0 JSON format
curl http://localhost:9081/api/billing/ledger?format=opencost

# 3. Download RFC 4180 CSV billing journal
curl http://localhost:9081/api/billing/ledger?format=csv -o ledger.csv

# 4. Trigger on-demand workload migration evaluation
curl -X POST http://localhost:9081/api/billing/optimize

# 5. Inspect recent autonomous migration history and savings
curl http://localhost:9081/api/billing/migrations

# 6. Ingest custom tenant usage event
curl -X POST http://localhost:9081/api/billing/meter \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "analytics-tenant",
    "account_id": "cf-tenant-alpha",
    "service": "workers",
    "resource_type": "requests",
    "quantity": 25000,
    "duration_ms": 120
  }'
```

---

## 📡 REST API Reference

| Endpoint | Method | Auth | Description |
| :--- | :---: | :---: | :--- |
| `/api/billing/summary` | `GET` | Public | Cluster cost summary, savings, and tenant/account aggregates |
| `/api/billing/ledger` | `GET` | Public | Ledger export (`?format=json|csv|jsonl|opencost&tenant=...&limit=...`) |
| `/api/billing/meter` | `POST` | Public | Ingest and attribute resource consumption record |
| `/api/billing/optimize` | `POST` | Public | Trigger autonomous workload migration evaluation |
| `/api/billing/migrations` | `GET` | Public | Inspect recent workload migration history and total savings |
| `/api/billing/rates` | `GET/POST` | Public | Query or update active Cloudflare pricing rates in micro-cents |
| `/api/billing/reset` | `POST` | Public | Clear in-memory billing ledger and aggregates |
| `/api/mtls/status` | `GET` | Public | Inspect active Root CA stats, policy, and hardware TPM 2.0 attestation |
| `/api/mtls/ca/cert` | `GET` | Public | Download Root CA public certificate in PEM format |
| `/api/mtls/ca/issue` | `POST` | Public | Issue an ephemeral client/peer certificate with SANs and custom validity |
| `/api/mtls/verify` | `POST` | Public | Evaluate an X.509 certificate PEM against active CA trust and security policy |
| `/api/mtls/revoke` | `POST` | Public | Revoke a certificate serial number and add to CRL denylist |
| `/api/mtls/tpm/attest` | `GET` | Public | Query Windows TPM 2.0 platform provider quote and system cert store |
| `/api/mtls/peer-token` | `POST` | Public | Generate ECDSA-signed peering token for inter-broker cluster synchronization |
| `/api/mtls/peer-token/verify` | `POST` | Public | Cryptographically verify inter-broker cluster peering token |
| `/api/mtls/policy` | `POST/PUT` | Public | Update active client certificate verification policy |
| `/raft/status` | `GET` | Public | Inspect local node role, term, leader ID, commit index, and cluster telemetry |
| `/raft/peers` | `GET/POST` | Public | Inspect cluster peers and latency; manually register remote node |
| `/raft/propose` | `POST` | Public | Propose a state machine command (`GRANT_LEASE`, `REVOKE_LEASE`, `UPDATE_QUOTA`, `SYNC_ACCOUNT`) |
| `/raft/journal` | `GET` | Public | Snapshot of all replicated WAL journal entries with SHA-256 checksums |
| `/raft/vote` | `POST` | Public | Raft election ballot endpoint for candidate votes |
| `/raft/append` | `POST` | Public | Raft heartbeat and log replication RPC endpoint |
| `/metrics` | `GET` | Public | Standard Prometheus 0.0.4 text format metrics scraper |
| `/api/compiler/compile` | `POST` | Public | Compiles & minifies JavaScript worker source, returning compression ratio and syntax status |
| `/api/compiler/workers` | `GET` | Public | List all deployed worker bundles and SHA-256 content hashes |
| `/api/compiler/workers` | `POST` | Public | Package and deploy a new Edge Worker bundle for a tenant |
| `/api/compiler/workers` | `DELETE` | Public | Undeploy a worker bundle (`?id=bundle-...`) |
| `/api/compiler/routes` | `GET` | Public | List registered dynamic routes sorted by priority descending |
| `/api/compiler/routes` | `POST` | Public | Register new dynamic route pattern (`pattern`, `zone_id`, `tenant_id`, `worker_id`) |
| `/api/compiler/routes` | `DELETE` | Public | Remove dynamic route binding (`?id=route-...`) |
| `/api/compiler/routes/match` | `POST` | Public | Test URL route pattern matching (`host`, `path`) |
| `/api/compiler/stream-test` | `GET` | Public | Zero-copy streaming benchmark endpoint (`?size_mb=1..50`) |
| `/api/config/status` | `GET` | **Bearer** | Hot-reload telemetry stats and sanitized accounts list (redacted tokens) |
| `/api/config/reload` | `POST` | **Bearer** | Triggers immediate zero-downtime hot reload of configuration from disk |
| `/api/quota/alerts` | `GET` | Public | Logged quota threshold notifications history |
| `/api/quota/webhooks/config` | `GET/POST` | Public | Inspect or update Discord/Slack webhook URLs |
| `/api/quota/webhooks/test` | `POST` | Public | Dispatches test quota threshold notification (80%/95%/100%) |
| `/api/quota/probation` | `GET` | Public | Current probation records and timers for pooled accounts |
| `/api/quota/probation/recover` | `POST` | Public | Manually restore an account from probation (`?id=...`) |
| `/api/quota/probation/trigger` | `POST` | Public | Simulate 429 rate limit probation trigger (`?id=...`) |
| `/api/quota/ratelimit` | `GET` | Public | Sliding window limiter telemetry (`?key=...`) |
| `/api/auth/token` | `POST` | Public / Key | Issue scoped HS256 JWT bearer token |
| `/api/auth/revoke` | `POST` | Bearer | Revoke a JWT token by JTI |
| `/api/auth/me` | `GET` | Bearer | Verify and inspect active caller claims |
| `/api/auth/status` | `GET` | Public | Inspect cryptographic providers and auth status |
| `/api/security/encrypt` | `POST` | Bearer | Encrypt secret payload into DPAPI or AES-GCM envelope |
| `/api/security/decrypt` | `POST` | Bearer | Test round-trip envelope decryption |
| `/api/security/status` | `GET` | Bearer | Inspect secret vault engine and active provider |
| `/s3/{bucket}/{key...}` | `PUT/GET/HEAD/DELETE` | Public | S3-compatible virtual storage gateway with chunk striping |
| `/s3/{bucket}` | `GET` | Public | S3 ListObjectsV2 XML listing for bucket |
| `/api/kv/{ns}/{key}` | `GET/PUT/DELETE` | Public | Multi-account partitioned KV caching endpoint |
| `/api/kv/stats` | `GET` | Public | Cache efficiency, hit rate, and tenant partition metrics |
| `/proxy/{service}/*` | `ANY` | Public | Transparent failover reverse proxy with 429 interception |
| `/api/accounts` | `GET` | Public | Snapshot of all pooled Cloudflare accounts and limits |
| `/api/leases` | `GET` | **Bearer** | List all active and revoked resource leases |
| `/api/leases` | `POST` | **Bearer** | Grant a new dynamic lease (`target_service`, `resource_type`) |
| `/api/leases/revoke` | `POST` | **Bearer** | Immediately invalidate and revoke a lease voucher (`?id=lse_...`) |
| `/api/health` | `GET` | Public | Verifies latency and connectivity to Cloudflare's public API edge |

---

## 🗺️ Roadmap

See [ROADMAP.md](./ROADMAP.md) for milestone goals and planned features.

---

## 🔒 Security

For vulnerability disclosures and reporting procedures, please refer to [SECURITY.md](./SECURITY.md).

---

## 📄 License

Licensed under the [Apache License 2.0](./LICENSE).
