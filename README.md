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
- **Dynamic Resource Leasing (Vouchers)**: Issues short-lived, scoped API tokens and endpoints (`lse_...`) for internal microservices, third-party callers, or test suites.
- **Zero-Downtime Auto-Failover**: Monitors HTTP 429 rate limits across tenant accounts and automatically reroutes active traffic leases to healthy accounts in the pool.
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
┌────────────────────────────────────────────────────────┐
│                    cf-lease-broker                     │
│                 Single Go Executable                   │
├──────────────────────────┬─────────────────────────────┤
│   internal/broker        │      internal/cloudflare    │
│   • Multi-Tenant Pool    │      • Edge Verification    │
│   • Dynamic Vouchers     │      • Health & Latency     │
├──────────────────────────┴─────────────────────────────┤
│                 net/http HTTP Engine                   │
│   • /api/accounts        • /api/leases                 │
├────────────────────────────────────────────────────────┤
│                 Embedded Web Dashboard                 │
│           (React 19 + TypeScript Glassmorphism)        │
│          //go:embed web/dist/* -> Port 9081            │
└────────────────────────────────────────────────────────┘
```

---

## 📡 REST API Reference

| Endpoint | Method | Description |
| :--- | :---: | :--- |
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
