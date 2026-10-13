<div align="center">

# ⚡ CF-Lease-Broker

**Multi-account Cloudflare resource pooling & dynamic token lease broker for Workers, KV, and R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-quick-start)

<sub>Crafted by **kLauncher1** · High-availability edge resource pooling and dynamic tenant leasing.</sub>

</div>

---

## ⚡ Highlights

- **Multi-Account Quota Aggregation**: Pools multiple free/pro Cloudflare accounts into a unified virtual compute cluster, overcoming single-account daily request boundaries.
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

### 2. Build Single Binary

```bash
go build -ldflags="-s -w" -o cf-lease-broker .
./cf-lease-broker
```

---

## 🏛️ Architecture & Resource Pooling

```text
┌────────────────────────────────────────────────────────────────────────┐
│                            cf-lease-broker                             │
│                         Single Go Executable                           │
├──────────────────────────┬──────────────────────────┬──────────────────┤
│   internal/broker        │      internal/proxy      │ internal/cloud...│
│   • Multi-Tenant Pool    │   • Transparent Proxy    │ • Edge Verify    │
│   • Dynamic Vouchers     │   • 429 Interception     │ • Latency Probe  │
│   • Quota Balancer       │   • Inflight Retry Engine│                  │
├──────────────────────────┴──────────────────────────┴──────────────────┤
│                         net/http HTTP Engine                           │
│   • /api/accounts   • /api/leases   • /api/health   • /proxy/{svc}/*   │
├────────────────────────────────────────────────────────────────────────┤
│                         Embedded Web Dashboard                         │
│                   (Glassmorphism UI via //go:embed)                    │
│                  web/dist/index.html -> Port 9081                      │
└────────────────────────────────────────────────────────────────────────┘
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

### Example Proxy Call:

```bash
# Transparently forward GET to auth-gateway on the optimal account
curl -i http://localhost:9081/proxy/auth-gateway/v1/verify

# HTTP/1.1 200 OK
# X-CF-Broker-Account: cf-tenant-charlie
# X-CF-Broker-Attempts: 1
```

If `cf-tenant-charlie` hits a rate limit, the caller receives the exact same response from `cf-tenant-bravo` with `X-CF-Broker-Attempts: 2` and zero client-side errors.

---

## 📡 REST API Reference

| Endpoint | Method | Description |
| :--- | :---: | :--- |
| `/proxy/{service}/*` | `ANY` | Transparent failover reverse proxy to Cloudflare Workers with 429 interception |
| `/api/accounts` | `GET` | Snapshot of all pooled Cloudflare accounts, limits, and usage |
| `/api/leases` | `GET` | List all active and revoked resource leases |
| `/api/leases` | `POST` | Grant a new dynamic lease (`target_service`, `resource_type`, `duration_hours`) |
| `/api/leases/revoke` | `POST` | Immediately invalidate and revoke a lease voucher (`?id=lse_...`) |
| `/api/health` | `GET` | Verifies latency and connectivity to Cloudflare's public API edge |

### Grant Lease Request Example
```bash
curl -X POST http://localhost:9081/api/leases \
  -H "Content-Type: application/json" \
  -d '{"target_service": "auth-gateway", "resource_type": "Workers", "duration_hours": 24}'
```

---

## 🗺️ Roadmap

See [ROADMAP.md](./ROADMAP.md) for milestone goals and planned features.

---

## 🔒 Security

For vulnerability disclosures and reporting procedures, please refer to [SECURITY.md](./SECURITY.md).

---

## 📄 License

Licensed under the [Apache License 2.0](./LICENSE).
