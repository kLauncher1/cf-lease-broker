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
│  internal/broker  │  internal/proxy   │       internal/storage         │
│  • Account Pool   │  • Reverse Proxy  │ • Virtual S3 Gateway (/s3/...) │
│  • Dynamic Leases │  • 429 Failover   │ • 2MB Striped Chunker          │
│  • Quota Balancer │  • Payload Buffer │ • Partitioned KV Cache Layer   │
├───────────────────┼───────────────────┼────────────────────────────────┤
│  internal/quota   │ internal/security │        internal/config         │
│  • Prometheus     │ • Windows DPAPI   │ • Zero-Downtime Hot Reload     │
│  • Sliding-Window │ • AES-256-GCM     │ • File Watcher & SIGHUP Sync   │
│  • Auto-Probation │ • HS256 JWT / RBAC│ • Dynamic Credential Injection │
│  • Webhook Alerts │ • Zeroize Memory  │ • Token Redaction Engine       │
├───────────────────┴───────────────────┴────────────────────────────────┤
│                         net/http HTTP Engine                           │
│   • /metrics         • /api/quota/*   • /s3/{bkt}/{key}                │
│   • /proxy/{svc}/*   • /api/accounts  • /api/leases (Auth Guarded)     │
│   • /api/auth/*      • /api/security  • /api/config/* (Auth Guarded)   │
│   • /api/health                                                        │
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

## 📡 REST API Reference

| Endpoint | Method | Auth | Description |
| :--- | :---: | :---: | :--- |
| `/metrics` | `GET` | Public | Standard Prometheus 0.0.4 text format metrics scraper |
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
