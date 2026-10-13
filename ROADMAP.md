# Product Roadmap

This document outlines the milestones and engineering priorities for `cf-lease-broker`.

---

## Current Release: v0.1.0 (Foundation)
- [x] Multi-tenant Cloudflare account pool manager with real-time quota telemetry.
- [x] Dynamic lease engine issuing scoped resource vouchers (`lse_...`) and tokens.
- [x] Immediate lease revocation and failover rerouting logic.
- [x] Embedded Cloudflare-themed glassmorphism dashboard (`web/dist/index.html`).
- [x] Full unit test coverage for pool and lease engine routines.

---

## Milestone 2: v0.2.0 (Automated Rate-Limit Interception)
- [ ] Direct Cloudflare API reverse proxy intercepting 429 Too Many Requests responses.
- [ ] Automatic zero-downtime hot-swap of API tokens without client reconfiguration.
- [ ] Prometheus metrics endpoint (`/metrics`) exposing active lease counts and tenant health.

---

## Milestone 3: v0.3.0 (KV & R2 Multi-Account Virtualization)
- [ ] Virtual R2 S3-compatible gateway pooling storage buckets across accounts.
- [ ] Unified KV read/write cache partitioner.
- [ ] Outgoing Discord/Slack webhook notifications on account quota exhaustion.

---

## Milestone 4: v1.0.0 (Production Hardening)
- [ ] Vault / AWS Secrets Manager backend for storing Cloudflare API keys encrypted at rest.
- [ ] Mutual TLS (mTLS) client verification for administrative broker endpoints.
- [ ] Cross-platform release binaries (Linux amd64/arm64, Windows, macOS).
