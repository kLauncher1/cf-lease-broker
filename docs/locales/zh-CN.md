<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <strong>🇨🇳 中文</strong> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**面向 Workers、KV 与 R2 的多账户 Cloudflare 资源池化与动态令牌租赁代理**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-快速开始)

<sub>由 **kLauncher1** 构建 · 高可用边缘资源池化、密钥库与动态租户租赁系统</sub>

</div>

---

## ⚡ 核心亮点 (Highlights)

- **边缘 Wasm 沙箱与 32MB 内存硬上限**：以 64 KiB 页面(`WasmPageSize = 65536`)划分的安全受控 WebAssembly 线性内存，强制执行严格的 32 MiB 物理硬上限(`HardCeilingMemoryPages = 512`)、越界访问保护、实例销毁时零化清除(Zeroization)及峰值分配追踪 (`internal/sandbox/memory.go`)。
- **确定性 Gas 计量与循环拦截**：按指令执行预算管理，具备确定性 Gas 计量与步骤核算，在 Gas 耗尽时立即终止失控的无限循环，且绝不挂起宿主代理 (`internal/sandbox/gas.go`)。
- **宿主崩溃隔离与容错恐慌恢复**：以延迟恢复(defer recover)包装客户机工作负载执行，捕获客户机错误(SIGSEGV 内存越界、未处理运行时 panic、Gas 耗尽陷阱及 OOM 超额分配)，确保宿主代理 100% 稳定，由循环环形缓冲区崩溃日志(保留最多 500 条近期崩溃报告)和渐进式租户隔离阈值强力支持 (`internal/sandbox/engine.go`)。
- **零开销 HTTP/3 eBPF 数据包调度**：在 Linux 上通过 eBPF sockmap 和 XDP 钩子在内核级进行套接字分发，绕过用户空间 TCP 协议栈，为边缘代理工作负载实现亚 10 微秒(< 10µs，实测约 0.26µs)的分发延迟 (`internal/steering/engine.go`)。
- **RFC 9000 UDP/QUIC 数据包头部深度检查**：零堆分配数据包解析，支持 QUIC Long(Initial, 0-RTT, Handshake, Retry)与 Short(1-RTT)报头，提取目的与源连接 ID(DCID/SCID)，基于 CID 前缀规则与最小连接负载均衡直接调度流量至最优 Worker 端点 (`internal/steering/quic_parser.go`)。
- **跨平台内核旁路与用户空间回退**：无缝跨平台架构，在 Linux 上提供原生 eBPF sockmap 钩子，在 Windows 和 macOS 上提供零拷贝用户空间快速路径分发，具备微秒级延迟与 100% 测试覆盖率 (`internal/steering/`)。
- **自主边缘令牌轮换器**：主动监控租赁凭证与租户令牌生命周期，在有效期达到 80% 时触发后台自动续签，并在 30 秒交接宽限期内提供零停机双令牌有效性 (`internal/rotator/rotator.go`)。
- **低于 5ms 的基于 Gossip 令牌撤销网格**：去中心化墓碑账本，支持 $O(1)$ 复杂度的撤销查询、HMAC-SHA256 签名墓碑、单调递增序列号、反熵状态同步与点对点广播，在代理集群节点间实现低于 5ms 的快速传播 (`internal/rotator/mesh.go`)。
- **用于委托刷新的硬件级 TPM 2.0 证明**：针对委托令牌刷新请求的加密平台完整性验证、防止重放攻击的单次使用质询随机数、常数时间 HMAC 标签校验以及平台配置寄存器(PCR)摘要验证 (`internal/rotator/attest.go`)。
- **智能边缘健康雷达与 Anycast 地理路由**：对全球 Cloudflare 边缘接入点(PoP)进行并发合成探测，追踪指数移动平均(EMA)延迟(`alpha=0.25`)、抖动与综合健康评分，结合亚毫秒级(< 1ms) Anycast 地理路由与 BGP 路由摆动异常检测 (`internal/radar/`)。
- **自主先发制人租赁迁移**：在发生网络丢包或套接字超时之前，动态将处于降级边缘 PoP 的活跃微服务租赁迁移至健康的备用节点。
- **自主成本优化与多租户计费账本**：基于 Workers、KV 和 R2 的官方 Cloudflare 定价标准，以微美分($1.00 = 1,000,000 微美分)进行细粒度算力、请求和存储计量，并在超出付费阈值时自动将工作负载迁移至备用免费账户 (`internal/billing/`)。
- **OpenCost 1.0 与多格式财务账本导出**：开箱即用支持导出 RFC 4180 CSV、换行符分隔 JSON(JSONL) 以及标准 OpenCost 1.0 规范格式(`/api/billing/ledger?format=opencost`)，满足企业级边缘云财务工程需求。
- **零信任相互 TLS (mTLS) 与客户端证书验证**：全面的 X.509 客户端证书链验证、证书撤销列表(CRL)黑名单检查、有效期窗口强制执行、通配符使用者可选名称(SAN)匹配(`*.domain.com`)以及针对上游反向代理路由的颁发者访问策略 (`internal/mtls/verifier.go`)。
- **临时证书颁发机构 (CA) 与对等网格**：零依赖内存级 ECDSA P-256 根证书生成、动态客户端证书签发、CRL 序列号追踪以及经加密签名的跨代理集群对等互联令牌 (`internal/mtls/ca.go`)。
- **硬件级 TPM 2.0 证明与 Windows 证书存储区**：利用 Windows 下一代加密技术(CNG) `ncrypt.dll` 查询 Microsoft Platform Crypto Provider 以验证硬件 TPM 2.0 的存在，检查 Windows 系统证书存储区(`crypt32.dll`)并生成确定性平台配置寄存器(PCR)测量报告 (`internal/mtls/tpm_windows.go`)。
- **高可用 Raft 一致性引擎**：基于随机选举定时器(150~300ms)的多节点 Raft 一致性集群、多数派法定人数领导者选举、具备单调递增索引和 SHA-256 验证的预写式日志(WAL)账本，以及零外部依赖的 DNS SRV 动态节点发现 (`net.LookupSRV`) (`internal/raft/`)。
- **边缘 Worker 虚拟编译器与打包**：用于 JavaScript/Wasm 边缘 Worker 脚本包的快速内存词法压缩与验证器，支持注释清除、语法校验和 SHA-256 内容哈希 (`internal/compiler/bundle.go`)。
- **多区域动态路由注入**：高性能通配符路由编译器，支持匹配任意域名(`*.domain.com/path/*`)，具备优先级权重评估与动态路由注入表 (`internal/compiler/router.go`)。
- **零拷贝直接流式传输管道**：低延迟边缘响应流式传输，利用预分配的 64KB 缓冲池(`sync.Pool`)、即时数据块刷新(`http.Flusher`)及客户端断开检测 (`internal/compiler/stream.go`)。
- **多架构独立运行产物**：跨平台编译流水线，自动构建去除符号信息的精简独立可执行文件，支持 `linux/amd64`、`linux/arm64`、`darwin/arm64` 和 `windows/amd64`，并自动生成 SHA-256 完整性校验和 (`scripts/build_artifacts.mjs`)。
- **零停机热重载**：通过文件修改监听和操作系统信号(POSIX 上的 `SIGHUP`)实现动态配置与凭据重载，在无需重启进程的情况下同步池化租户和密钥库。
- **多账户配额聚合**：将多个 Cloudflare 免费/专业版账户聚合为统一的虚拟计算集群，突破单个账户的每日请求上限。
- **生产级密钥存储库**：静态信封加密，在 Windows 上使用 DPAPI(`CryptProtectData`)，跨平台支持 AES-256-GCM 及内存清零(`Zeroize`)。
- **HS256 JWT 认证与 RBAC**：微服务与管理认证引擎，基于 HMAC-SHA256 令牌，具备常数时间签名比对与线程安全的 JTI 撤销黑名单。
- **虚拟 R2 S3 兼容网关**：S3 REST 协议端点(`/s3/{bucket}/{key...}`)，将对象存储分片条带化存储至池化 R2 存储桶中，具备自动 2MB 分块与 SHA-256 完整性校验。
- **多账户分区 KV 缓存**：线程安全的分区 KV 存储层，具备一致性 FNV 哈希、LRU 内存缓存与 TTL 过期清除机制。
- **Prometheus 0.0.4 指标导出器**：原生标准 Prometheus `/metrics` 抓取端点，零外部依赖导出租赁计数、租户配额、速率限制拒绝与代理遥测。
- **滑动窗口速率限制与自动观察期**：基于细分内存桶的滑动窗口速率跟踪器，具备突发容差及针对受限账户的指数退避观察状态机。
- **多渠道配额 Webhook 告警**：在 80%(警告)、95%(严重)和 100%(耗尽)阈值下触发丰富的 Discord Embeds、Slack Block Kit 以及通用 JSON 通知，具备 10 分钟防抖机制。
- **自动限流拦截与零停机故障转移**：实时拦截 HTTP 429 请求过多及网关错误，自动将正在处理的请求无缝重新路由至资源池中的下一个健康账户。
- **透明反向代理**：通过 `/proxy/{service}/*` 无缝转发流量，支持缓冲请求体重试及符合 RFC 7230 的逐跳头部过滤。
- **动态资源租赁 (凭证)**：为内部微服务、第三方调用方或测试套件签发短寿命、受限范围的 API 令牌与端点(`lse_...`)。
- **单可执行文件部署**：通过 `//go:embed` 将 Cloudflare 主题的毛玻璃质感管理面板直接内嵌到单个 Go 二进制文件中。
- **轻量级内存占用**：常驻运行内存持续保持在 **12 MB RAM 以下**，零外部运行时依赖。

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

管理控制台访问地址：
👉 **`http://localhost:9081`**

### 2. 跨平台编译多架构独立运行产物

```bash
# 构建适用于 linux/amd64, linux/arm64, darwin/arm64, windows/amd64 的独立二进制文件
node scripts/build_artifacts.mjs --clean

# 校验加密 SHA-256 校验和
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ 架构与资源池化

```text
┌────────────────────────────────────────────────────────────────────────┐
│                            cf-lease-broker                             │
│                           单个 Go 可执行文件                           │
├───────────────────┬───────────────────┬────────────────────────────────┤
│   internal/raft   │  internal/broker  │       internal/storage         │
│ • Raft 节点核心   │ • 账户资源池      │ • 虚拟 S3 网关 (/s3/...)       │
│ • WAL 预写日志    │ • 动态租约管理    │ • 2MB 条带化分块器             │
│ • DNS SRV 对等发现│ • 配额均衡器      │ • 分区 KV 缓存层               │
│ • 代理 FSM 同步   │ • 自动故障转移    │ • 多渠道 Webhook 告警          │
├───────────────────┼───────────────────┼────────────────────────────────┤
│   internal/mtls   │ internal/security │       internal/compiler        │
│ • 临时 CA 机构    │ • Windows DPAPI   │ • 虚拟 JS 压缩器 / Wasm        │
│ • 客户端 X509 认证│ • AES-256-GCM     │ • 多区域通配符路由编译器       │
│ • TPM 2.0 硬件证明│ • HS256 JWT / RBAC│ • 64KB 零拷贝流式传输管道      │
│ • 对等互联令牌    │ • 内存清零抹除    │ • 租户脚本包存储库             │
├───────────────────┴───────────────────┴────────────────────────────────┤
│                         internal/quota & config                        │
│  • Prometheus (/metrics) • 滑动窗口限流 • 零停机热重载                 │
├────────────────────────────────────────────────────────────────────────┤
│                         net/http HTTP 引擎                             │
│   • /api/mtls/* (证书/验证/TPM)        • /api/compiler/*               │
│   • /raft/* (投票/追加/状态)           • /proxy/{svc}/* (mTLS 防护)    │
│   • /metrics         • /api/auth/*     • /s3/{bkt}/{key}               │
│   • /api/accounts    • /api/security   • /api/config/* (认证保护)      │
├────────────────────────────────────────────────────────────────────────┤
│                            内嵌 Web 仪表板                             │
│                    (基于 //go:embed 的毛玻璃 UI)                       │
│                   web/dist/index.html -> 端口 9081                     │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 🔒 密钥管理与身份验证

`cf-lease-broker` 开箱即用提供企业级机密存储与身份验证，零第三方依赖：

### 1. 信封加密密钥库 (`internal/security/vault.go`)
- **Windows DPAPI 集成**：在 Windows 系统上，通过 Windows DPAPI(`CryptProtectData`)利用操作系统级凭据(`dpapi:<base64>`)保护 API 密钥。
- **AES-256-GCM 信封**：在非 Windows 平台（或明确指定时），使用带有唯一 96 位随机 Nonce 的带认证 AES-256-GCM(`aes-gcm:<nonce>:<ciphertext>`)加密机密。
- **内存擦除**：敏感字节缓冲区在使用后立即通过 `Zeroize` 内存擦除模式(`for i := range b { b[i] = 0 }`)彻底清零。

### 2. JWT 与 RBAC 中间件 (`internal/security/middleware.go`)
- **HS256 标准令牌**：签发并验证 HMAC-SHA256 JWT，具备常数时间签名比较(`crypto/subtle.ConstantTimeCompare`)，有效抵御时序侧信道攻击。
- **撤销黑名单**：支持按 JTI 立即撤销令牌(`POST /api/auth/revoke`)，并配备自动 TTL 过期清理机制。
- **受保护的管理 API**：管理端点(`/api/leases`、`/api/leases/revoke` 和 `/api/security/*`)均要求有效的 `Authorization: Bearer <token>` 或根管理员引导密钥。

```bash
# 1. 签发操作员 JWT 令牌
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. 携带 Bearer 令牌调用受保护的租赁端点
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 虚拟 R2 S3 兼容网关 (`/s3/{bucket}/{key...}`)

代理提供标准 AWS S3 兼容端点，允许现有 S3 SDK、微服务和 CLI 工具跨池化 Cloudflare R2 账户存储对象，突破单个租户的存储配额限制：

### 核心功能：
1. **条带化分块**：超过 2MB 的入站载荷将自动分割为有序分块(`chk_<hash>_<index>`)并分发至资源池内的健康账户中。
2. **完整性校验**：每个数据块均包含独立的 SHA-256 校验和，完整对象可获得标准分段上传兼容的复合 ETag。
3. **HTTP Range 请求**：支持 `Range: bytes=start-end` 头部，实现视频点播流式传输、断点续传及大文件部分读取。
4. **ListObjectsV2**：符合标准 S3 XML `ListBucketResult` 响应格式。

### S3 CLI 与 Curl 示例：

```bash
# 1. 跨池化 R2 存储桶存储对象
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. 获取对象
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. 部分字节范围(Range)请求
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf

# 4. 列出存储桶对象 (S3 XML)
curl http://localhost:9081/s3/assets
```

---

## 🔄 透明故障转移反向代理 (`/proxy/{service}/*`)

代理在 `/proxy/{service}/*` 处提供透明的边缘反向代理。调用方只需将 HTTP 请求发送到此端点，无需关心当前由哪个租户账户承载可用配额。

### 故障转移工作机制：
1. **目标解析**：入站请求的 `{service}` 与子路径将被解析至剩余配额最多的池化账户(`SelectOptimalAccount`)。
2. **载荷缓冲**：使用流式读取器将请求体缓冲在内存中（最多 10MB），以便重试时可无损重放完整载荷。
3. **429 与网关错误拦截**：若目标账户返回 HTTP `429 Too Many Requests`、`502 Bad Gateway`、`503 Service Unavailable`、`504 Gateway Timeout` 或遇到网络断开连接：
   - 代理立即通过 `TriggerFailover(accountID)` 将该租户标记为不健康状态。
   - 请求将自动重试并切换至资源池中的下一个健康账户。
4. **逐跳标头过滤**：双向自动清除符合 RFC 7230 规范的逐跳标头(`Connection`、`Keep-Alive`、`Upgrade`、`TE`、`Trailers` 等)。
5. **遥测标头**：成功响应将包含便于排查的元数据标头：
   - `X-CF-Broker-Account: {account_id}` (实际处理请求的租户账户)
   - `X-CF-Broker-Attempts: {count}` (尝试请求的次数)

---

## 🔄 零停机热重载 (`internal/config/`)

`cf-lease-broker` 支持在不重启进程或断开活跃 HTTP 连接的情况下动态轮换凭证与更新账户池：

### 热重载机制：
1. **文件监听循环**：定期轮询配置文件(`CF_BROKER_CONFIG_PATH` 或 `config.json`)，并通过 SHA-256 确定性摘要比对内容变更，避免无效重载。
2. **POSIX 信号处理**：捕获 `SIGHUP` 信号（Unix/Linux/macOS）立即触发同步配置校验与重新加载。
3. **原子状态切换与安全回退**：若重载文件包含非法 JSON、负数值限制或重复租户 ID，重载将安全失败并记录错误日志，原有活动配置不受任何影响。
4. **凭据与池同步**：成功重载的租户将直接同步至 `PoolManager`(`SyncAccounts`) 并加密持久化至 `SecretVault`(`StoreCredential`)。

### 配置文件格式 (`config.json`)：
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

## 🚀 边缘 Worker 虚拟编译器与动态路由注入

代理内置针对多租户边缘 Worker 脚本与 WebAssembly 模块的轻量编译与动态路由引擎：

### 1. 内存词法压缩与打包 (`internal/compiler/bundle.go`)
- **词法分词器**：剥除单行(`//`)与多行(`/* */`)注释，消除多余空白符，规范标点间距，并严格校验 Cloudflare Worker 模块导出签名(`export default { fetch(...) }`, `addEventListener('fetch')`)。
- **WebAssembly 校验**：在打包租户模块前严格验证 4 字节魔数头(`\x00asm`)与版本规范。
- **内容哈希**：生成确定性 SHA-256 摘要，用于租户脚本包版本管理与审计跟踪。

### 2. 多区域通配符路由编译器 (`internal/compiler/router.go`)
- **任意域名映射**：支持跨通配符子域(`*.domain.com/*`)、精确主机(`api.domain.com/v1/*`)或全局回退规则(`*/*`)映射端点。
- **优先级权重前缀树**：评估入站请求主机与路径的匹配特异性评分（精确主机 > 通配符子域 > 全局；长路径前缀 > 短前缀），无需重载配置即可动态调度流量。

### 3. 零拷贝直接流式传输管道 (`internal/compiler/stream.go`)
- **预分配缓冲池**：利用预分配的 64KB 字节切片池(`sync.Pool`)彻底杜绝高吞吐代理传输中的堆内存反复分配。
- **即时套接字刷新**：利用 `http.Flusher` 实时将响应数据块直接推送至客户端，并通过 `X-Accel-Buffering: no` 禁用任何中间层缓冲。
- **取消中断检测**：深度挂钩 `context.Context`，在客户端意外断开连接时立即终止上游 Worker 数据流，杜绝套接字句柄泄漏。

---

## 🤝 高可用 Raft 分布式一致性引擎

`cf-lease-broker` 内嵌零依赖的 Raft 一致性引擎(`internal/raft/`)，支持多区域代理集群化、零数据丢失租约状态复制及自动化领导者故障转移：

### 核心架构与一致性基语：
1. **多数派领导者选举与随机定时器**：节点在 Follower、Candidate 与 Leader 状态间平滑切换。通过 150~300ms 随机选举超时彻底杜绝分票死锁，并通过 50ms 心跳滴答器维持多数派法定人数 `((N/2) + 1)` 的领导权威。
2. **预写日志 (WAL) 账本 (`internal/raft/journal.go`)**：强制实施带有 SHA-256 完整性校验和的单调事务索引。追加前验证跟随者日志一致性，自动截断冲突的未提交条目。支持可选的仅追加磁盘持久化(`raft_journal.wal`)。
3. **基于 DNS SRV 的动态对等发现 (`internal/raft/discovery.go`)**：通过 RFC 2782 DNS SRV 记录(`_raft._tcp.<domain>`)自动解析集群对等节点，并优雅回退至静态环境变量映射(`CF_RAFT_PEERS`)或运行时手动注册(`POST /raft/peers`)。
4. **代理有限状态机 (FSM) (`internal/raft/fsm_broker.go`)**：在所有节点上确定性复制事务指令：
   - `GRANT_LEASE`: 在集群内同步复制活跃租赁凭证。
   - `REVOKE_LEASE`: 向所有对等引擎广播租约撤销事件。
   - `SYNC_ACCOUNT`: 同步租户配置档案与配额限额。
   - `UPDATE_QUOTA`: 传播每日 Worker 请求增量、R2 存储变动与 KV 读取计数。
   - `NOOP`: 用于集群就绪度探测的一致性屏障探针。

### 集群拓扑结构流向：

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

### Raft 集群 Curl 示例：

```bash
# 1. 检查本地节点状态与集群拓扑
curl http://localhost:9081/raft/status

# 2. 查询对等节点健康状况与往返延迟(RTT)
curl http://localhost:9081/raft/peers

# 3. 手动注册新的对等节点
curl -X POST http://localhost:9081/raft/peers \
  -H "Content-Type: application/json" \
  -d '{"id": "node-2", "address": "http://127.0.0.1:9082"}'

# 4. 向集群提议状态机事务
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

# 5. 查看已复制的 WAL 日志条目及 SHA-256 校验和
curl http://localhost:9081/raft/journal
```

---

## 🛡️ 零信任相互 TLS (mTLS) 与硬件证明

`cf-lease-broker` 集成了完备的零信任相互 TLS (mTLS) 引擎、动态内存临时证书颁发机构 (CA) 以及基于硬件的 TPM 2.0 完整性证明：

### 1. 临时证书颁发机构与动态签发 (`internal/mtls/ca.go`)
- **内存根信任锚**：无需外部工具或 OpenSSL 依赖，在内存中动态引导生成 ECDSA P-256 根证书颁发机构(`x509.IsCA = true`)。
- **客户端与对等证书**：签发具有自定义通用名称(CN)、有效期窗口及使用者可选名称(SAN)的短寿命加密 X.509 客户端与对等证书。
- **CRL 撤销管理**：维护按十六进制序列号索引的线程安全证书撤销列表(CRL)，实现 $O(1)$ 黑名单命中查询。
- **已签名对等凭证**：签发并校验用于代理间零信任对等同步的 ECDSA-SHA256 对等令牌。

### 2. 客户端证书验证与策略强制 (`internal/mtls/verifier.go`)
- **信任链验证**：对照根 CA 证书池(`x509.CertPool`)校验入站 X.509 客户端证书链。
- **策略强制**：强制实施通配符 SAN 匹配(`*.edge.cluster.local`、CIDR 块)、允许的通用名称、允许的颁发者以及严格的证书过期时间窗口。
- **透明边缘标头提取**：支持从直接 TLS 连接(`r.TLS.PeerCertificates`)或标准边缘转发标头(`X-Client-Cert-Pem`、`X-Forwarded-Client-Cert`、`X-Client-Cert-Der`)中提取证书。
- **反向代理防护**：启用 `CF_REQUIRE_MTLS=true` 时，所有途经 `/proxy/{service}/*` 的请求均必须出示经合规验证的客户端证书。

### 3. 硬件级 TPM 2.0 证明 (`internal/mtls/tpm_windows.go`)
- **Windows CNG 集成**：通过微软下一代加密技术(`ncrypt.dll`)查询 Microsoft Platform Crypto Provider，确认硬件 TPM 2.0 的存在。
- **Windows 系统证书存储区**：通过 `crypt32.dll`(`CertOpenSystemStoreW`)检查并验证机器/用户证书存储区。
- **平台度量报告**：生成确定性平台配置寄存器(PCR)摘要与背书密钥测量值，提供防篡改审计跟踪。

### mTLS 与 TPM Curl 示例：

```bash
# 1. 查看 mTLS 状态、活动根 CA 及 TPM 证明状态
curl http://localhost:9081/api/mtls/status

# 2. 下载 PEM 格式的根 CA 公共证书
curl http://localhost:9081/api/mtls/ca/cert -o root-ca.pem

# 3. 签发临时客户端证书
curl -X POST http://localhost:9081/api/mtls/ca/issue \
  -H "Content-Type: application/json" \
  -d '{
    "common_name": "worker-edge-alpha",
    "sans": ["*.edge.broker.local", "127.0.0.1"],
    "validity_hours": 24
  }'

# 4. 根据活动安全策略评估 X.509 PEM 证书
curl -X POST http://localhost:9081/api/mtls/verify \
  -H "Content-Type: application/json" \
  -d '{"certificate_pem": "-----BEGIN CERTIFICATE-----\n..."}'

# 5. 撤销证书序列号
curl -X POST http://localhost:9081/api/mtls/revoke \
  -H "Content-Type: application/json" \
  -d '{"serial_number": "7b8a1c9d2e"}'

# 6. 查询硬件 TPM 2.0 证明报价
curl http://localhost:9081/api/mtls/tpm/attest
```

---

## 💰 自主成本优化与多租户计费账本

`cf-lease-broker` 提供高精度的多租户计费引擎与成本优化器，支持精细到微美分的资源计量，自主防止云端账单超额扣费：

### 1. 细粒度微美分成本归因 (`internal/billing/meter.go`)
- **微美分精度**：以整数微美分($1.00 = 1,000,000 微美分)追踪资源消耗，彻底规避浮点数舍入偏差。
- **完整的 Cloudflare 定价模型**：预置官方最新资费标准：
  - Workers 请求量：每 100 万次请求 $0.30 (300,000 微美分)
  - Workers CPU 时长：每 100 万毫秒 CPU 时间 $0.02 (20,000 微美分)
  - KV 读取：每 100 万次读取 $0.50 (500,000 微美分)
  - KV 写入：每 100 万次写入 $5.00 (5,000,000 微美分)
  - R2 存储空间：每月每 GB $0.015 (15,000 微美分)
  - R2 A 类操作：每 100 万次操作 $4.50 (4,500,000 微美分)
  - R2 B 类操作：每 100 万次操作 $0.36 (360,000 微美分)
  - 出站流量带宽：$0.00 (Cloudflare 零出站流量费承诺)
- **每日免费配额抵扣**：跟踪每个免费账户的每日免费额度（10 万次免费 Workers 请求和 10 万次免费 KV 读取），在 UTC 午夜自动滚动重置，在归因 $0 成本的同时精准统计预估节省金额。

### 2. 自主工作负载迁移 (`internal/billing/optimizer.go`)
- **前瞻性超额检测**：对照可配置的阈值（默认 80%）持续审计账户使用率。
- **备用免费层优先**：当付费账户接近配额或预算上限时，优化器自动重定向流量或设置冷却观察期，将后续租约与代理流量调度至具备未用完每日免费配额的健康备用账户。
- **累计节省金额追踪**：量化并实时报告因避免付费层超额而节省的微美分精确数额。

### 3. OpenCost 1.0 与多格式财务导出 (`internal/billing/journal.go`)
- **OpenCost 1.0 规范**：以直接符合 OpenCost 边缘计算规范的格式导出租户账单分配 (`/api/billing/ledger?format=opencost`)。
- **RFC 4180 CSV 导出**：生成逗号分隔的账单记录，便于电子表格分析与财务核算 (`/api/billing/ledger?format=csv`)。
- **换行符分隔 JSON (JSONL)**：流式输出结构化日志记录，便于直接摄取到 BigQuery、Datadog 或 Elasticsearch (`/api/billing/ledger?format=jsonl`)。

### 计费与成本 Curl 示例：

```bash
# 1. 查询集群级成本汇总与租户明细
curl http://localhost:9081/api/billing/summary

# 2. 导出 OpenCost 1.0 JSON 格式的计费账本
curl http://localhost:9081/api/billing/ledger?format=opencost

# 3. 下载 RFC 4180 CSV 计费日志文件
curl http://localhost:9081/api/billing/ledger?format=csv -o ledger.csv

# 4. 手动触发即时工作负载迁移评估
curl -X POST http://localhost:9081/api/billing/optimize

# 5. 查看近期自主迁移历史记录与节省金额
curl http://localhost:9081/api/billing/migrations

# 6. 摄入自定义租户资源使用事件
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

## 🛰️ 智能边缘健康雷达与 Anycast 地理路由

`cf-lease-broker` 内置自主边缘健康雷达引擎，实时监控全球 Cloudflare 接入点(PoP)，在丢包发生前捕捉网络异常，并以亚毫秒级精度解析最优 Anycast 路径：

### 1. 合成边缘探测与遥测 (`internal/radar/probe.go`)
- **指数移动平均 (EMA)**：平滑高频探测采样(`alpha=0.25`)，防止偶发抖动引发路由颠簸。
- **延迟抖动与丢包评分**：计算统计学抖动与丢包比率，为每个 PoP 生成 0~100 的综合健康指数。
- **受控并发调度**：利用有限容量的 Worker 池并发探测边缘端点，避免系统资源耗尽。

### 2. 亚毫秒级 Anycast 地理路由 (`internal/radar/router.go`)
- **Haversine 球面距离**：精确计算客户端坐标与边缘 PoP 节点间的大圆球面距离。
- **复合权衡评分**：跨地理距离、延迟 EMA、抖动及运行健康状态进行多维度综合寻优。
- **租户亲和性**：将租户租赁绑定至其最佳区域 PoP，同时保持亚毫秒级的快速路由解析。

### 3. BGP 路由异常检测与主动规避
- **路由摆动检测**：将超出基线 EMA 35ms 或达到 1.8 倍以上的延迟跳变标记为关键异常。
- **先发制人撤离**：在客户端连接感知到网络丢包或连接超时之前，自动将分配给降级 PoP 的活跃租赁疏散至健康的备用节点。

### 雷达 Curl 示例：

```bash
# 1. 查询雷达状态与全球遥测概览
curl http://localhost:9081/api/radar/status

# 2. 列出亚太地区受监控的所有边缘 PoP
curl "http://localhost:9081/api/radar/pops?region=Asia%20Pacific"

# 3. 为指定客户端位置(首尔, KR)解析最佳边缘 PoP
curl -X POST http://localhost:9081/api/radar/route \
  -H "Content-Type: application/json" \
  -d '{"lat": 37.5665, "lon": 126.9780, "country": "KR"}'

# 4. 对所有边缘 PoP 触发并发合成探测
curl -X POST http://localhost:9081/api/radar/probe \
  -H "Content-Type: application/json" \
  -d '{"concurrency": 10}'

# 5. 查看近期 BGP 路由摆动与自主迁移事件
curl http://localhost:9081/api/radar/events
```

---

## 🔄 自主边缘令牌轮换器与撤销网格

`cf-lease-broker` 提供了自主边缘令牌轮换引擎、高速基于 Gossip 协议的撤销网格，以及用于委托刷新操作的硬件级 TPM 2.0 证明支持：

### 1. 自主前瞻性令牌续期 (`internal/rotator/rotator.go`)
- **主动阈值触发**：持续评估活跃租约令牌，当令牌生命周期达到其有效期的 80% 时自动触发后台续期，避免边缘客户端因令牌失效而中断。
- **零停机平滑交接宽限期**：在内存宽限期映射中保留前代令牌密钥（默认 30 秒），允许传输中的并发请求无感知平滑过渡，杜绝 HTTP 401 报错。
- **租约凭单实时同步**：将更新后的令牌密钥与延长后的过期时间实时同步回 `internal/broker/lease.go`。

### 2. 亚 5ms Gossip 撤销网格 (`internal/rotator/mesh.go`)
- **去中心化墓碑账本**：存储经过加密签名的撤销墓碑，支持 $O(1)$ 的快速命中查询(`IsRevoked(id)`)。
- **亚 5ms 极速传播**：在 5 毫秒内将撤销墓碑直接广播并同步至集群内的所有节点。
- **HMAC-SHA256 防篡改校验**：每个墓碑均由单调序列号签名，并通过常数时间比对校验完整性。
- **反熵状态对齐**：在节点间定期进行的 Gossip 握手中自动补齐缺失的墓碑记录。

### 3. 用于委托刷新的硬件 TPM 2.0 证明 (`internal/rotator/attest.go`)
- **单次使用质询随机数**：生成具备有效期的加密质询随机数，严格杜绝重放攻击。
- **平台完整性核验**：在为委托刷新请求颁发续签令牌前，严格核查硬件平台配置寄存器(PCR)摘要与 TPM 2.0 证明报价。

### 轮换器与网格 Curl 示例：

```bash
# 1. 查询轮换器概况、活动令牌与网格延迟
curl http://localhost:9081/api/rotator/status

# 2. 列出受轮换管理的所有活动令牌
curl http://localhost:9081/api/rotator/tokens

# 3. 手动触发特定令牌的即时轮换续签
curl -X POST http://localhost:9081/api/rotator/renew \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "operator_manual_trigger"}'

# 4. 在 Gossip 网格中秒级广播紧急撤销 (<5ms)
curl -X POST http://localhost:9081/api/rotator/revoke \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "security_audit_invalidation"}'

# 5. 获取单次使用硬件证明质询随机数
curl http://localhost:9081/api/rotator/challenge

# 6. 携带硬件 TPM 证明执行委托令牌续签
curl -X POST http://localhost:9081/api/rotator/delegate \
  -H "Content-Type: application/json" \
  -d '{
    "token_id": "tok_12345678",
    "delegated_by_node": "edge-worker-node-1",
    "nonce": "<nonce_from_challenge>",
    "hardware_quote": "TPM2_Quote[PCR0-7=1234567890abcdef,EK=abcdef1234567890]",
    "pcr_digest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "timestamp": 1776300000
  }'

# 7. 查看活动撤销墓碑列表
curl http://localhost:9081/api/rotator/tombstones
```

---

## 📦 边缘 Wasm 沙箱内存限制器与崩溃隔离

在多租户代理部署中运行的边缘 Worker 工作负载必须建立绝对的资源边界与故障隔离机制，防止恶意脚本、内存泄漏或未捕获的异常损坏宿主代理或在租户之间泄露数据。

```
┌────────────────────────────────────────────────────────────────────────┐
│                   Edge Wasm Sandbox Supervisor Engine                  │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   Guest Worker Bytecode (Wasm / Simulated Bytecode)                    │
│         │                                                              │
│         ▼                                                              │
│   ┌──────────────────────────────────────────────────────────────┐     │
│   │ Safe Managed Linear Memory (64 KiB Pages)                    │     │
│   │  • Tenant Quota (e.g. 16MB)  • Hard Ceiling: 32MB (512p)     │     │
│   │  • Bounds Checking           • Teardown Zeroization Scrub    │     │
│   └──────────────────────────────────────────────────────────────┘     │
│         │                                                              │
│         ▼                                                              │
│   ┌──────────────────────────────────────────────────────────────┐     │
│   │ Deterministic Gas Accountant & Loop Step Counter             │     │
│   │  • Basic Op: 1 gas           • Math: 2 gas                   │     │
│   │  • Memory: 10 gas            • Branch/Loop: 5 gas            │     │
│   │  • Loop Steps Trap: Deterministic Halt on Budget Exhaustion  │     │
│   └──────────────────────────────────────────────────────────────┘     │
│         │                                                              │
│         ▼                                                              │
│   ┌──────────────────────────────────────────────────────────────┐     │
│   │ Fault-Tolerant Panic Isolation Guard (defer recover)         │     │
│   │  • SIGSEGV / Memory Violations Trapped                       │     │
│   │  • Host Broker Process 100% Intact & Healthy                 │     │
│   │  • Circular Ring-Buffer Crash Journal (500 Entries)          │     │
│   │  • Progressive Tenant Quarantine Threshold                   │     │
│   └──────────────────────────────────────────────────────────────┘     │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

### 核心架构能力

1. **严格的 32 MiB 物理硬上限**：WebAssembly 线性内存按 64 KiB 页面(`WasmPageSize = 65536`)划分。即使租户策略申请更多内存，代理也会将线性内存严格限制在 512 页以内(32 MiB，`HardCeilingMemoryPages`)。任何超过此上限的分配都会被拒绝并返回 `ErrMemoryHardCeilingExceeded`，记录为 OOM 拦截。
2. **确定性 Gas 计量**：指令根据预定义权重扣减 Gas（基础操作：1，算术：2，内存访问：10，循环步长：5，调用：20，内存增长：每页 100）。若 Gas 耗尽，执行将以 `ErrGasExhausted` 确定性终止，在不挂起代理线程的前提下打断死循环。
3. **恐慌与故障隔离**：客户机代码执行均包装在延迟恢复机制中(`defer func() { if r := recover(); ... }`)。严重错误（如模拟 SIGSEGV 空指针解引用或运行时 panic）将被捕获并格式化为包含堆栈跟踪的独立 `CrashReport`，宿主代理则持续平稳对外提供服务。
4. **崩溃日志与渐进式隔离**：线程安全的循环环形缓冲区记录最近 500 条崩溃报告（附带时间戳、诱因和内存状态）。若租户连续崩溃次数超过预设阈值（默认：5 次），将自动转入 `QUARANTINED`（隔离）状态，直至管理员手动重置。
5. **内存清零擦除 (Zeroization)**：在实例销毁或重置时，所有线性内存切片均以 0 字节彻底覆写清除(`subtle.ConstantTimeCompare`)，杜绝跨租户数据残留与敏感令牌泄漏。

### 使用方法与命令示例

```bash
# 1. 查看沙箱管理器状态与聚合指标
curl http://localhost:9081/api/sandbox/status

# 2. 在客户机 Wasm 沙箱中执行计算任务
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "compute",
    "gas_budget": 100000
  }'

# 3. 测试确定性无限循环拦截 (Gas 耗尽即刻停止)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "infinite_loop",
    "gas_budget": 5000
  }'

# 4. 测试 32MB 物理硬上限强制执行 (OOM 拒绝)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "memory_alloc",
    "requested_pages": 600
  }'

# 5. 测试客户机恐慌/故障隔离 (宿主代理保持 100% 稳定运行)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "panic_fault"
  }'

# 6. 列出最近崩溃报告及已恢复的堆栈追踪
curl "http://localhost:9081/api/sandbox/crashes?limit=10"

# 7. 注册或更新自定义租户沙箱配额策略
curl -X POST http://localhost:9081/api/sandbox/tenants \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "max_memory_pages": 256,
    "gas_limit": 2000000,
    "max_crash_threshold": 5
  }'

# 8. 重置实例并清除隔离状态
curl -X POST http://localhost:9081/api/sandbox/reset \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant-edge-prod", "instance_id": "inst-1"}'
```

---

## 📡 REST API 参考

| 端点 | 方法 | 认证 | 说明 |
| :--- | :---: | :---: | :--- |
| `/api/sandbox/status` | `GET` | 公开 | 沙箱监管器状态、活动实例、OOM 拦截次数及恐慌恢复指标 |
| `/api/sandbox/execute` | `POST` | 公开 | 在严格内存限制和 Gas 计量下执行客户机 Wasm 工作负载 |
| `/api/sandbox/tenants` | `GET/POST` | 公开 | 查看或更新租户沙箱隔离策略与配额限额 |
| `/api/sandbox/crashes` | `GET` | 公开 | 记录捕获故障及堆栈跟踪的循环崩溃日志 |
| `/api/sandbox/crashes/clear` | `POST` | 公开 | 清空内存中的崩溃日志环形缓冲区 |
| `/api/sandbox/reset` | `POST` | 公开 | 将崩溃或隔离的沙箱实例重置为全新的 IDLE 就绪状态 |
| `/api/steering/status` | `GET` | 公开 | HTTP/3 eBPF 调度状态、活动运行模式及吞吐量统计 |
| `/api/steering/endpoints` | `GET/POST/DELETE` | 公开 | 查看、注册或移除用于直接调度的上游 Worker 端点 |
| `/api/steering/rules` | `GET/POST/DELETE` | 公开 | 查看、添加或删除 QUIC 连接 ID(CID) 前缀路由规则 |
| `/api/steering/simulate` | `POST` | 公开 | 模拟 UDP/QUIC 数据包头解析并实测微秒级调度延迟 |
| `/api/steering/stats` | `GET` | 公开 | 细粒度微秒调度遥测及低于 10µs 延迟所占比例 |
| `/api/rotator/status` | `GET` | 公开 | 轮换器状态、活动令牌数、续签计数及网格传播延迟 |
| `/api/rotator/tokens` | `GET` | 公开 | 列出受轮换管理的所有活动令牌 |
| `/api/rotator/renew` | `POST` | 公开 | 立即触发特定令牌轮换与密钥续期 |
| `/api/rotator/revoke` | `POST` | 公开 | 跨 Gossip 网格极速广播令牌/租约即时撤销通知 |
| `/api/rotator/gossip` | `POST` | 公开 | 用于交换墓碑与维护网格成员资格的对等 Gossip 端点 |
| `/api/rotator/delegate` | `POST` | 公开 | 要求硬件 TPM 完整性证明的委托令牌刷新接口 |
| `/api/rotator/challenge` | `GET` | 公开 | 生成用于硬件证明校验的单次使用质询随机数 |
| `/api/rotator/tombstones` | `GET` | 公开 | 列出本地账本中存储的带签名撤销墓碑 |
| `/api/rotator/peers` | `GET/POST` | 公开 | 查看对等网格节点列表或手动注册新的 Gossip 节点 |
| `/api/rotator/events` | `GET` | 公开 | 近期轮换与撤销安全事件审计日志 |
| `/api/radar/status` | `GET` | 公开 | 雷达引擎状态、跟踪 PoP 数、全局平均延迟与迁移次数 |
| `/api/radar/pops` | `GET` | 公开 | 列出受监控的边缘 PoP 及遥测指标 (`?region=...`) |
| `/api/radar/probe` | `POST` | 公开 | 针对特定节点(`?colo=...`)或所有 PoP 触发合成探测 |
| `/api/radar/route` | `GET/POST` | 公开 | 亚毫秒级 Anycast 最佳地理路由解析器 (`?lat=...&lon=...&country=...`) |
| `/api/radar/events` | `GET` | 公开 | 近期 BGP 路由摆动异常与自主迁移流水账本 |
| `/api/radar/migrate` | `POST` | 公开 | 紧急疏散指定异常 PoP 上的所有活跃微服务租赁 |
| `/api/billing/summary` | `GET` | 公开 | 集群成本汇总、预估累计节省及按租户/账户聚合数据 |
| `/api/billing/ledger` | `GET` | 公开 | 账本导出 (`?format=json|csv|jsonl|opencost&tenant=...&limit=...`) |
| `/api/billing/meter` | `POST` | 公开 | 摄取并归因单条资源消耗记录 |
| `/api/billing/optimize` | `POST` | 公开 | 手动触发自主工作负载迁移优化评估 |
| `/api/billing/migrations` | `GET` | 公开 | 查阅近期工作负载迁移历史流水与累计节省明细 |
| `/api/billing/rates` | `GET/POST` | 公开 | 查询或更新活动的 Cloudflare 微美分官方费率表 |
| `/api/billing/reset` | `POST` | 公开 | 清空内存计费账本与聚合统计数据 |
| `/api/mtls/status` | `GET` | 公开 | 查看当前活动根 CA 指标、安全策略与硬件 TPM 2.0 证明 |
| `/api/mtls/ca/cert` | `GET` | 公开 | 下载 PEM 格式的根 CA 公共数字证书 |
| `/api/mtls/ca/issue` | `POST` | 公开 | 签发带 SAN 扩展与自定义有效期的临时客户端/对等证书 |
| `/api/mtls/verify` | `POST` | 公开 | 根据活动 CA 信任池与安全策略评估 X.509 PEM 证书 |
| `/api/mtls/revoke` | `POST` | 公开 | 撤销指定证书序列号并将其追加至 CRL 吊销黑名单 |
| `/api/mtls/tpm/attest` | `GET` | 公开 | 查询 Windows TPM 2.0 平台证明报价及系统证书库状态 |
| `/api/mtls/peer-token` | `POST` | 公开 | 生成用于跨代理集群安全同步的 ECDSA 签名对等令牌 |
| `/api/mtls/peer-token/verify` | `POST` | 公开 | 校验跨代理集群对等令牌的密码学签名有效性 |
| `/api/mtls/policy` | `POST/PUT` | 公开 | 动态更新入站客户端证书的安全校验策略 |
| `/raft/status` | `GET` | 公开 | 查看本地节点角色、任期、Leader ID、提交索引及集群遥测 |
| `/raft/peers` | `GET/POST` | 公开 | 查看集群对等节点与往返延迟；手动登记远端节点 |
| `/raft/propose` | `POST` | 公开 | 提议状态机指令 (`GRANT_LEASE`, `REVOKE_LEASE`, `UPDATE_QUOTA`, `SYNC_ACCOUNT`) |
| `/raft/journal` | `GET` | 公开 | 包含 SHA-256 校验和的已复制 WAL 预写日志快照 |
| `/raft/vote` | `POST` | 公开 | 候选人拉票时的 Raft 选举投票 RPC 端点 |
| `/raft/append` | `POST` | 公开 | Raft 心跳与日志条目复制 RPC 端点 |
| `/metrics` | `GET` | 公开 | 标准 Prometheus 0.0.4 纯文本格式指标抓取端点 |
| `/api/compiler/compile` | `POST` | 公开 | 编译并压缩 JavaScript Worker 源码，返回压缩比与语法状态 |
| `/api/compiler/workers` | `GET` | 公开 | 列出所有已部署的 Worker 脚本包及其 SHA-256 内容哈希 |
| `/api/compiler/workers` | `POST` | 公开 | 为指定租户打包并发布新的边缘 Worker 脚本包 |
| `/api/compiler/workers` | `DELETE` | 公开 | 卸载指定的 Worker 脚本包 (`?id=bundle-...`) |
| `/api/compiler/routes` | `GET` | 公开 | 按优先级降序展示已注册的所有动态路由规则 |
| `/api/compiler/routes` | `POST` | 公开 | 注册新的动态路由模式 (`pattern`, `zone_id`, `tenant_id`, `worker_id`) |
| `/api/compiler/routes` | `DELETE` | 公开 | 移除指定的动态路由绑定 (`?id=route-...`) |
| `/api/compiler/routes/match` | `POST` | 公开 | 测试指定 URL 请求与路由表的模式匹配 (`host`, `path`) |
| `/api/compiler/stream-test` | `GET` | 公开 | 零拷贝流式传输基准性能测试端点 (`?size_mb=1..50`) |
| `/api/config/status` | `GET` | **Bearer** | 热重载遥测指标及已脱敏的账户列表 (已隐藏敏感令牌) |
| `/api/config/reload` | `POST` | **Bearer** | 触发立即从磁盘无损热重载最新配置文件 |
| `/api/quota/alerts` | `GET` | 公开 | 查看历史记录中的配额阈值报警通知列表 |
| `/api/quota/webhooks/config` | `GET/POST` | 公开 | 查看或更新 Discord/Slack Webhook 告警地址 |
| `/api/quota/webhooks/test` | `POST` | 公开 | 手动发送测试配额阈值告警通知 (80%/95%/100%) |
| `/api/quota/probation` | `GET` | 公开 | 池化账户当前的观察期限制记录与解封倒计时 |
| `/api/quota/probation/recover` | `POST` | 公开 | 手动解除指定账户的限流观察期状态 (`?id=...`) |
| `/api/quota/probation/trigger` | `POST` | 公开 | 模拟触发 429 速率限制观察期降级逻辑 (`?id=...`) |
| `/api/quota/ratelimit` | `GET` | 公开 | 滑动窗口速率限制器遥测数据 (`?key=...`) |
| `/api/auth/token` | `POST` | 公开 / Key | 签发受限范围的 HS256 JWT Bearer 身份认证令牌 |
| `/api/auth/revoke` | `POST` | Bearer | 通过 JTI 标识立即作废指定的 JWT 令牌 |
| `/api/auth/me` | `GET` | Bearer | 校验并查看当前调用方的 Token Claims 载荷明细 |
| `/api/auth/status` | `GET` | 公开 | 查看底层密码学提供程序与认证中枢运行状态 |
| `/api/security/encrypt` | `POST` | Bearer | 将明文密钥封装加密为 DPAPI 或 AES-GCM 安全信封 |
| `/api/security/decrypt` | `POST` | Bearer | 验证安全信封的双向解密往返合规性 |
| `/api/security/status` | `GET` | Bearer | 检查机密存储库引擎及当前活跃的加密提供者 |
| `/s3/{bucket}/{key...}` | `PUT/GET/HEAD/DELETE` | 公开 | 支持条带化分块存储的 S3 兼容虚拟对象网关 |
| `/s3/{bucket}` | `GET` | 公开 | 针对指定存储桶的 S3 ListObjectsV2 XML 列表响应 |
| `/api/kv/{ns}/{key}` | `GET/PUT/DELETE` | 公开 | 多账户一致性哈希分区的 KV 键值缓存读写端点 |
| `/api/kv/stats` | `GET` | 公开 | 缓存命中率、运行效率及租户分区存储统计 |
| `/proxy/{service}/*` | `ANY` | 公开 | 具备实时 429 拦截重试能力的透明故障转移反向代理 |
| `/api/accounts` | `GET` | 公开 | 查看所有已池化的 Cloudflare 账户状态与剩余额度快照 |
| `/api/leases` | `GET` | **Bearer** | 列出当前所有活跃及已注销的资源租约凭单 |
| `/api/leases` | `POST` | **Bearer** | 动态分配并签发新的资源租约 (`target_service`, `resource_type`) |
| `/api/leases/revoke` | `POST` | **Bearer** | 立即宣告作废并注销指定的资源租约凭单 (`?id=lse_...`) |
| `/api/health` | `GET` | 公开 | 实时校验与 Cloudflare 官方 API 边缘节点的通信延迟及连通性 |

---

## 🗺️ 路线图 (Roadmap)

有关里程碑目标与后续功能规划，请参阅 [ROADMAP.md](../../ROADMAP.md)。

---

## 🔒 安全性 (Security)

有关安全漏洞披露流程与报告指南，请参阅 [SECURITY.md](../../SECURITY.md)。

---

## 📄 许可证 (License)

本项目基于 [Apache License 2.0](../../LICENSE) 开源协议授权。
