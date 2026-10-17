<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <strong>🇨🇳 中文</strong> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**面向 Workers、KV 与 R2 的多账号 Cloudflare 资源池化与动态令牌租赁代理**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-快速开始)

<sub>开发者：**kLauncher1** · 高可用边缘资源池化、保密凭据金库与动态多租户租赁系统</sub>

</div>

---

## ⚡ 核心亮点 (Highlights)

- **自主边缘令牌轮换器**：主动监控租赁凭证与租户令牌生命周期，在有效期达到 80% 时自动触发后台续订；提供 30 秒交接宽限期内双令牌并行生效机制，保障零停机无缝切换 (`internal/rotator/rotator.go`)。
- **5ms 以内基于 Gossip 的令牌撤销网络**：支持 $O(1)$ 复杂度撤销查询的去中心化墓碑账本，具备 HMAC-SHA256 签名墓碑、单调递增序列号、反熵状态同步与点对点广播，实现集群节点间 5ms 以内极速传播 (`internal/rotator/mesh.go`)。
- **委托刷新硬件级 TPM 2.0 远程证明**：针对委托令牌刷新请求执行平台完整性密码学验证，提供防重放的一次性挑战随机数、恒定时间 HMAC 标签校验以及平台配置寄存器 (PCR) 摘要核对 (`internal/rotator/attest.go`)。
- **智能边缘健康雷达与 Anycast 地理路由**：对全球 Cloudflare 边缘节点 (PoP) 执行并发合成探测，实时跟踪指数移动平均 (EMA) 延迟 (`alpha=0.25`)、抖动与综合健康指数，结合 1ms 以内的 Anycast 地理路由及 BGP 路由震荡异常检测 (`internal/radar/`)。
- **自主前置式租赁迁移**：在发生网络丢包或套接字超时之前，动态将处于性能劣化 PoP 的活跃微服务租赁平滑迁移至健康的备用节点。
- **自主成本优化与多租户计费账本**：依据官方 Cloudflare 计费模型对 Workers、KV 与 R2 资源进行微美分级别（$1.00 = 1,000,000 微美分）精细计量，当付费账号面临超额风险时自动将负载无缝切流至空闲免费层账号 (`internal/billing/`)。
- **OpenCost 1.0 与多格式财务凭单导出**：支持开箱即用的 RFC 4180 CSV、换行符分隔 JSON (JSONL) 以及标准 OpenCost 1.0 规范格式导出 (`/api/billing/ledger?format=opencost`)，满足企业边缘云财务工程需求。
- **零信任双向 TLS (mTLS) 与客户端证书校验**：支持全面的 X.509 客户端证书链校验、证书吊销列表 (CRL) 黑名单核查、有效期窗口强制执行、通配符使用者可选名称 (SAN) 匹配 (`*.domain.com`) 以及上游反向代理路由的颁发者策略控制 (`internal/mtls/verifier.go`)。
- **临时证书颁发机构 (CA) 与对等网络**：零依赖纯内存 ECDSA P-256 根证书生成、动态客户端证书签发、CRL 序列号追踪以及经密码学签名的跨代理集群对等认证令牌 (`internal/mtls/ca.go`)。
- **硬件 TPM 2.0 证明与 Windows 证书存储**：借助 Windows CNG (`ncrypt.dll`) 查询 Microsoft Platform Crypto Provider 以验证硬件 TPM 2.0，检查 Windows 系统证书存储 (`crypt32.dll`) 并生成确定性 PCR 测量报价 (`internal/mtls/tpm_windows.go`)。
- **高可用 Raft 状态机与分布式集群**：基于随机选举定时器 (150-300ms) 的多节点 Raft 共识集群，具备多数派法定人数选举、包含单调索引与 SHA-256 校验的预写日志 (WAL)，以及零外部依赖的 DNS SRV 动态节点发现 (`net.LookupSRV`) (`internal/raft/`)。
- **边缘 Worker 虚拟编译器与打包**：专为 JavaScript/Wasm 边缘 Worker 包设计的内存级语法压缩器与校验器，支持注释消除、语法合法性校验及 SHA-256 内容哈希指纹 (`internal/compiler/bundle.go`)。
- **多区域动态路由注入**：高性能通配符路由编译器，支持任意域名匹配 (`*.domain.com/path/*`)，具备权重评分与动态路由注入能力 (`internal/compiler/router.go`)。
- **零拷贝直接流式管道**：利用预分配 64KB 缓冲区池 (`sync.Pool`)、实时数据块推送 (`http.Flusher`) 及客户端断开连接检测，实现超低延迟边缘响应流传输 (`internal/compiler/stream.go`)。
- **多架构独立可执行文件构建**：支持交叉编译构建适用于 `linux/amd64`、`linux/arm64`、`darwin/arm64` 与 `windows/amd64` 的轻量独立二进制文件，并自动生成 SHA-256 校验和 (`scripts/build_artifacts.mjs`)。
- **零停机热重载**：通过文件修改监听与系统信号 (POSIX 的 `SIGHUP`) 动态更新租户池与凭据保险箱，无需重启进程或中断现有连接。
- **多账号配额聚合**：将多个免费/专业版 Cloudflare 账号整合为单一虚拟计算集群，突破单个账号每日请求上限。
- **企业级凭据金库**：静态数据采用 Windows DPAPI (`CryptProtectData`) 与跨平台 AES-256-GCM 信封加密，结合内存即时归零清扫 (`Zeroize`)，杜绝凭据泄漏。
- **HS256 JWT 鉴权与 RBAC**：基于 HMAC-SHA256 的身份认证引擎，具备常量时间签名比对（防时序侧信道攻击）与线程安全 JTI 撤销黑名单。
- **虚拟 R2 S3 兼容网关**：标准 S3 REST 协议端点 (`/s3/{bucket}/{key...}`)，支持 2MB 自动分块、条带化分发存储至多个 R2 存储桶，具备完整复合 ETag 校验。
- **多账号分区 KV 缓存**：采用一致性 FNV 哈希、LRU 内存缓存与 TTL 过期的线程安全分区 KV 存储层。
- **Prometheus 0.0.4 指标导出**：原生标准 `/metrics` 抓取端点，零外部依赖输出租赁计数、租户配额、限流拒绝统计与代理遥测数据。
- **滑动窗口限流与自动试用期隔离**：具备子桶内存滑动窗口限流器，为受限流账号提供指数退避惩罚试用状态机。
- **多渠道配额 Webhook 告警**：在 80%（警告）、95%（危险）与 100%（耗尽）阈值触发 Discord Embed、Slack Block Kit 和自定义 JSON 告警，内置 10 分钟防抖机制。
- **429 拦截与自动故障转移**：实时截获 HTTP 429 Too Many Requests 及网关错误，毫秒级将传输中请求自动重试并转移至池内下一个健康账号。
- **透明反向代理**：通过 `/proxy/{service}/*` 透明转发流量，支持请求体流式缓冲重试与 RFC 7230 逐跳标头清理。
- **动态资源租赁（凭证）**：面向微服务、第三方调用方或集成测试签发短生命周期、指定权限范围的 API 令牌与端点 (`lse_...`)。
- **单二进制部署**：基于 `//go:embed` 技术将 Cloudflare 风格的毛玻璃管理后台仪表盘完整嵌入单个可执行文件中。
- **极小内存开销**：常驻运行内存持续保持在 **12 MB 以下**，不依赖任何外部运行时。

---

## 🚀 快速开始

### 1. 下载并运行

```bash
# 克隆仓库
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# 直接运行
go run .
```

管理控制台即刻运行于：
👉 **`http://localhost:9081`**

### 2. 交叉编译多架构独立二进制

```bash
# 构建适用于 linux/amd64, linux/arm64, darwin/arm64, windows/amd64 的独立执行文件
node scripts/build_artifacts.mjs --clean

# 校验 SHA-256 密码学哈希值
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ 架构设计与资源池化

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

## 🔒 凭据管理与身份鉴权

### 1. 信封加密金库 (`internal/security/vault.go`)
- **Windows DPAPI**：在 Windows 系统上，自动调用 Windows DPAPI (`CryptProtectData`) 将 API Key 绑定至操作系统用户账户 (`dpapi:<base64>`)。
- **AES-256-GCM 信封**：在非 Windows 平台上，使用携带 96 位独立随机 Nonce 的认证加密算法 AES-256-GCM (`aes-gcm:<nonce>:<ciphertext>`)。
- **内存擦除**：敏感字节切片在使用完毕后立即通过 `Zeroize` 模式覆盖清理 (`for i := range b { b[i] = 0 }`)。

### 2. JWT 与 RBAC 中间件 (`internal/security/middleware.go`)
- **HS256 标准令牌**：签发与验证 HMAC-SHA256 JWT，使用恒定时间签名比对 (`crypto/subtle.ConstantTimeCompare`) 防御侧信道攻击。
- **即时撤销机制**：支持依据 JTI 立即撤销令牌 (`POST /api/auth/revoke`)，内置后台 TTL 自动清扫。
- **管理路由受保护**：管理端点 (`/api/leases`, `/api/leases/revoke`, `/api/security/*`) 强制要求合法的 Bearer Token。

---

## 🪣 虚拟 R2 S3 兼容网关 (`/s3/{bucket}/{key...}`)

向客户端暴露完全兼容 AWS S3 的接口，现有 S3 SDK 与工具可在免修改前提下分发存储海量对象：

1. **条带化分块**：大于 2MB 的负载被自动拆解为有序数据块 (`chk_<hash>_<index>`) 分散存入不同账号下的 R2 存储桶。
2. **完整性校验**：每个数据块配备专属 SHA-256 校验和，提供标准的复合 ETag。
3. **HTTP Range 支持**：支持 `Range: bytes=start-end` 请求，满足大文件断点续传与流式视频加载需求。

---

## 🔄 透明故障转移反向代理 (`/proxy/{service}/*`)

1. **自动寻优**：智能选择当前可用配额最多的账号作为路由目标 (`SelectOptimalAccount`)。
2. **流式缓冲重试**：最大缓冲 10MB 请求体，确保触发 429 或连接错误时可无损重放。
3. **故障隔离**：当远端响应 429、502、503、504 时，立即将故障账号置入惩罚期并瞬间切流至健康账号。

---

## 🚀 边缘 Worker 虚拟编译器与动态路由

1. **词法精简打包**：自动去除代码注释与多余空白，验证 Worker 标准入口。
2. **多区域路由树**：支持精确域名、通配符子域及全局降级规则匹配。
3. **零拷贝数据流**：借助 64KB 缓冲区池与直接套接字推送 (`http.Flusher`)，实现低延迟流式吞吐。

---

## 🤝 高可用 Raft 状态机与分布式共识

1. **多数派仲裁**：通过 150-300ms 随机定时器防止选票瓜分，以多数派仲裁维持集群强一致性。
2. **WAL 预写日志**：记录单调事务并进行 SHA-256 签名，支持持久化存储 (`raft_journal.wal`)。
3. **状态机复制**：集群全网同步租赁签发、令牌撤销、租户限额与增量配额变更。

---

## 🛡️ 零信任 mTLS 与硬件 TPM 证明

1. **动态证书签发**：内置 ECDSA P-256 根 CA，随时签发短期客户端证书与对等通信令牌。
2. **双向证书验证**：对入站连接强制验证证书链、通配符 SAN 及有效期。
3. **TPM 2.0 证明**：利用 Windows CNG 提取硬件 TPM 平台配置寄存器 (PCR) 报价。

---

## 💰 自主成本优化与账本

1. **微美分计费**：采用绝对无浮点舍入误差的微美分整数运算实时计量请求与计算消耗。
2. **预防超额切换**：在付费账号达到配额阈值（默认 80%）前，主动调度流量至闲置的免费账号。
3. **多格式导出**：提供符合 OpenCost 1.0 规范的 JSON 以及标准 CSV/JSONL 报表。

---

## 🛰️ 智能边缘健康雷达

1. **合成探测**：利用 EMA 算法平滑延迟指标，评估全网 PoP 真实服务质量。
2. **Anycast 最优解**：基于半正矢公式 (Haversine) 精准计算客户端与边缘物理距离，亚毫秒级输出最佳路线。
3. **BGP 震荡自愈**：当监测到路径延迟骤增超过 35ms 时，提前将长连接和微服务会话迁往备用节点。

---

## 🔄 边缘令牌轮换与 Gossip 撤销

1. **前置轮换**：令牌到期 80% 时自动后台续期，伴随 30 秒双向兼容宽限期。
2. **Gossip 撤销网**：在 5 毫秒内将带防篡改签名的撤销墓碑广播至整个集群。

---

## 📡 REST API 核心接口

| 接口端点 | 方法 | 权限 | 功能描述 |
| :--- | :---: | :---: | :--- |
| `/api/rotator/status` | `GET` | 公开 | 令牌轮换状态、活跃令牌统计与网络延迟 |
| `/api/rotator/tokens` | `GET` | 公开 | 查看受管理的所有轮换令牌列表 |
| `/api/rotator/renew` | `POST` | 公开 | 立即触发指定令牌的主动续订与轮换 |
| `/api/rotator/revoke` | `POST` | 公开 | 通过 Gossip 网络广播吊销令牌凭证 |
| `/api/radar/status` | `GET` | 公开 | 雷达探测状态、受监控 PoP 统计及迁移次数 |
| `/api/radar/route` | `GET/POST`| 公开 | 亚毫秒 Anycast 路由寻优计算器 |
| `/api/billing/summary` | `GET` | 公开 | 集群成本汇总、开销节省额与租户统计 |
| `/api/billing/ledger` | `GET` | 公开 | 账本财务数据导出 (`?format=json\|csv\|jsonl\|opencost`) |
| `/api/mtls/status` | `GET` | 公开 | 根 CA 运行状态、安全策略与 TPM 2.0 硬件状态 |
| `/api/mtls/ca/cert` | `GET` | 公开 | 下载 PEM 格式根 CA 证书 |
| `/api/mtls/ca/issue` | `POST` | 公开 | 签发携带指定 SAN 与有效期的客户端证书 |
| `/api/mtls/verify` | `POST` | 公开 | 针对现有证书链与安全策略执行合规校验 |
| `/raft/status` | `GET` | 公开 | 查看本节点角色、任期、Leader ID 及同步位点 |
| `/metrics` | `GET` | 公开 | Prometheus 0.0.4 标准监控指标抓取入口 |
| `/api/compiler/compile`| `POST` | 公开 | 在线编译压缩 JS Worker 代码 |
| `/s3/{bucket}/{key...}`| `PUT/GET` | 公开 | 虚拟 S3 兼容对象存储网关 |
| `/proxy/{service}/*` | `ANY` | 公开 | 具备 429 故障转移特性的反向代理通道 |
| `/api/accounts` | `GET` | 公开 | 池化 Cloudflare 账号列表与实时配额状态 |
| `/api/leases` | `GET/POST` | **Bearer** | 查询活跃租赁或动态签发新资源租约 |
| `/api/leases/revoke` | `POST` | **Bearer** | 立即废止指定的租赁凭证 |
| `/api/health` | `GET` | 公开 | 检查与 Cloudflare 公共 API 边缘的连通性 |

---

## 🗺️ 开发路线 (Roadmap)

详细的里程碑规划请参见 [ROADMAP.md](../../ROADMAP.md)。

---

## 🔒 安全规范 (Security)

漏洞报告渠道与披露指南请参见 [SECURITY.md](../../SECURITY.md)。

---

## 📄 开源许可证 (License)

本项目依据 [Apache License 2.0](../../LICENSE) 许可证开源。
