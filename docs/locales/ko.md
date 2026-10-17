<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <strong>🇰🇷 한국어</strong> · <a href="./zh-CN.md">🇨🇳 中文</a> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**Workers, KV, R2를 위한 다중 계정 Cloudflare 리소스 풀링 및 동적 토큰 임대 브로커**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-빠른-시작)

<sub>제작자: **kLauncher1** · 고가용성 엣지 리소스 풀링, 보안 시크릿 금고 및 동적 테넌트 임대 시스템</sub>

</div>

---

## ⚡ 주요 특징 (Highlights)

- **엣지 Wasm 샌드박스 및 32MB 메모리 물리 상한**: 64 KiB 페이지(`WasmPageSize = 65536`)로 분할된 안전한 관리형 WebAssembly 선형 메모리, 32 MiB 물리적 상한선(`HardCeilingMemoryPages = 512`) 강제, 범위 초과 접근 보호, 인스턴스 해제 시 제로화(Zeroization) 스크러빙 및 최대 메모리 할당 추적 (`internal/sandbox/memory.go`).
- **결정론적 가스 계측 및 무한 루프 트랩**: 명령어 단위 실행 예산 관리, 결정론적 가스 측정 및 스텝 회계, 가스 고갈 시 호스트 브로커의 중단 없이 무한 루프를 즉각 강제 종료 (`internal/sandbox/gas.go`).
- **호스트 충돌 격리 및 장애 허용 패닉 복구**: 게스트 워크로드 실행을 감싸는 지연 복구(defer recover), 게스트 결함(SIGSEGV 메모리 위반, 런타임 패닉, 가스 고갈 트랩, OOM 초과 할당) 격리 트랩으로 호스트 브로커 100% 안정성 보장, 순환 링 버퍼 충돌 저널(최대 500개 충돌 보고서 보관) 및 단계적 테넌트 격리 임계값 (`internal/sandbox/engine.go`).
- **오버헤드 제로 HTTP/3 eBPF 패킷 스티어링**: Linux 상에서 eBPF sockmap 및 XDP 훅을 통해 유저스페이스 TCP 스택을 우회하는 커널 레벨 소켓 디스패칭, 엣지 프록시 워크로드에 대해 10마이크로초 미만(< 10µs, 실측 ~0.26µs)의 디스패치 레이턴시 달성 (`internal/steering/engine.go`).
- **RFC 9000 UDP/QUIC 패킷 헤더 정밀 검사**: QUIC Long(Initial, 0-RTT, Handshake, Retry) 및 Short(1-RTT) 헤더를 파싱하는 제로 힙 패킷 검사, 대상 및 출발지 연결 ID(DCID/SCID) 추출, CID 접두사 규칙 및 최소 연결 로드 밸런싱을 기반으로 최적의 워커 엔드포인트로 트래픽 직접 전달 (`internal/steering/quic_parser.go`).
- **크로스 플랫폼 커널 바이패스 및 유저스페이스 폴백**: Linux에서는 네이티브 eBPF sockmap 훅을 제공하고, Windows 및 macOS에서는 마이크로초 미만의 레이턴시와 100% 테스트 커버리지를 보장하는 제로 카피 유저스페이스 고속 경로 디스패치를 제공하는 원활한 크로스 플랫폼 아키텍처 (`internal/steering/`).
- **자율 엣지 토큰 로테이터**: 임대 바우처와 테넌트 토큰 수명주기를 능동 모니터링하여 유효 기간 80% 도달 시 백그라운드 갱신 트리거, 30초 인계 유예 기간 동안 무중단 이중 토큰 유효성 보장 (`internal/rotator/rotator.go`).
- **5ms 미만 가십 기반 토큰 해지 메시**: $O(1)$ 해지 조회를 지원하는 탈중앙화 툼스톤 원장, HMAC-SHA256 서명 툼스톤, 단조 증가 시퀀스 번호, 안티 엔트로피 상태 동기화 및 P2P 브로드캐스트로 클러스터 노드 간 5ms 미만 전파 달성 (`internal/rotator/mesh.go`).
- **위임 갱신을 위한 하드웨어 기반 TPM 2.0 원격 증명**: 위임 토큰 갱신 요청에 대한 플랫폼 무결성 암호화 검증, 재생 공격을 차단하는 1회용 챌린지 난수, 상수 시간 HMAC 태그 검증 및 플랫폼 구성 레지스터(PCR) 다이제스트 검증 (`internal/rotator/attest.go`).
- **지능형 엣지 헬스 레이더 및 애니캐스트 지리 라우팅**: 전 세계 Cloudflare 엣지 거점(PoP)에 대한 동시성 합성 프로빙, 지수 이동 평균(EMA) 지연 시간(`alpha=0.25`), 지터 및 종합 헬스 점수 추적, 1ms 미만의 애니캐스트 지리 라우팅 및 BGP 경로 플랩 이상 징후 탐지 결합 (`internal/radar/`).
- **자율 선제적 임대 마이그레이션**: 네트워크 패킷 손실이나 소켓 타임아웃이 발생하기 전에 성능이 저하되는 엣지 PoP에서 정상적인 대기 PoP로 활성 및 처리 중인 마이크로서비스 임대를 동적으로 자동 이전.
- **자율 비용 최적화 및 다중 테넌트 청구 원장**: Workers, KV, R2 공식 Cloudflare 요금제를 기반으로 마이크로센트 단위($1.00 = 1,000,000 마이크로센트) 연산/요청/스토리지 정밀 계측, 유료 플랜 초과 요금 위험 시 유휴 무료 티어 계정으로 워크로드 자동 마이그레이션 (`internal/billing/`).
- **OpenCost 1.0 및 다중 포맷 재무 원장 내보내기**: 엔터프라이즈 엣지 클라우드 재무 엔지니어링을 위해 RFC 4180 CSV, JSONL 및 표준 OpenCost 1.0 사양 포맷(`/api/billing/ledger?format=opencost`)으로 청구 원장 즉시 내보내기 지원.
- **제로 트러스트 상호 TLS (mTLS) 및 클라이언트 인증서 검증**: X.509 클라이언트 인증서 체인 검증, 인증서 해지 목록(CRL) 거부 목록 확인, 유효 기간 검증, 와일드카드 주체 대체 이름(SAN) 매칭(`*.domain.com`) 및 업스트림 리버스 프록시 경로에 대한 발급자 접근 정책 적용 (`internal/mtls/verifier.go`).
- **에페머럴 인증 기관(CA) 및 피어링 메시**: 제로 의존성 인메모리 ECDSA P-256 루트 인증서 생성, 동적 클라이언트 인증서 발급, CRL 일련번호 추적 및 암호화 서명된 브로커 간 피어링 토큰 (`internal/mtls/ca.go`).
- **하드웨어 TPM 2.0 원격 증명 및 Windows 인증서 저장소**: Windows CNG(`ncrypt.dll`)를 활용하여 Microsoft Platform Crypto Provider의 하드웨어 TPM 2.0 탑재를 검증하고, Windows 시스템 인증서 저장소(`crypt32.dll`) 점검 및 PCR 인용 측정값 생성 (`internal/mtls/tpm_windows.go`).
- **고가용성 Raft 합의 엔진**: 무작위 선거 타이머(150~300ms) 기반 멀티 노드 Raft 합의 클러스터, 과반 정족수 리더 선출, 단조 인덱싱 및 SHA-256 검증이 포함된 WAL(Write-Ahead Log) 저널, 제로 외부 의존성 DNS SRV 동적 피어 탐색 (`net.LookupSRV`) (`internal/raft/`).
- **엣지 워커 가상 컴파일러 및 패키징**: 주석 제거, 문법 검증 및 SHA-256 콘텐츠 해싱이 포함된 JavaScript/Wasm 엣지 워커 번들용 고속 인메모리 어휘 축소기 및 검증기 (`internal/compiler/bundle.go`).
- **다중 영역 동적 라우트 주입**: 우선순위 가중치 및 동적 라우트 주입 테이블을 통해 임의 도메인(`*.domain.com/path/*`)을 매칭하는 고성능 와일드카드 라우트 컴파일러 (`internal/compiler/router.go`).
- **제로 카피 다이렉트 스트리밍 파이프라인**: 사전 할당된 64KB 버퍼 풀(`sync.Pool`), 즉각적인 청크 플러시(`http.Flusher`) 및 클라이언트 연결 해제 감지를 활용한 초저지연 엣지 응답 스트리밍 (`internal/compiler/stream.go`).
- **다중 아키텍처 독립 실행형 바이너리**: SHA-256 무결성 체크섬 자동 생성과 함께 `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`용 단독 바이너리를 빌드하는 크로스 컴파일 파이프라인 (`scripts/build_artifacts.mjs`).
- **무중단 핫 리로딩**: 프로세스 재시작 없이 파일 수정 감지 및 OS 시그널(POSIX의 `SIGHUP`)을 통해 풀링된 테넌트와 시크릿 금고를 동적으로 실시간 갱신.
- **다중 계정 쿼터 어그리게이션**: 여러 무료/프로 Cloudflare 계정을 단일 가상 컴퓨트 클러스터로 통합하여 단일 계정 일일 요청 한계를 극복.
- **프로덕션 시크릿 금고**: Windows DPAPI(`CryptProtectData`) 및 크로스 플랫폼 AES-256-GCM 봉투 암호화와 메모리 스크러빙(`Zeroize`)을 적용하여 저장 데이터 완전 보호.
- **HS256 JWT 인증 및 RBAC**: 타이밍 공격을 방어하는 상수 시간 검증 및 스레드 안전 JTI 해지 거부 목록이 포함된 HMAC-SHA256 기반 인증 엔진.
- **가상 R2 S3 호환 게이트웨이**: 자동 2MB 청크 분할 및 SHA-256 무결성 검증을 통해 풀링된 R2 버킷에 오브젝트를 분산 저장하는 S3 REST 프로토콜 엔드포인트(`/s3/{bucket}/{key...}`).
- **다중 계정 파티셔닝 KV 캐시**: FNV 일관된 해싱, LRU 메모리 캐싱 및 TTL 만료가 탑재된 스레드 안전 파티셔닝 KV 스토리지 레이어.
- **Prometheus 0.0.4 메트릭 익스포터**: 제로 외부 의존성으로 임대 건수, 테넌트 쿼터, 레이트 리밋 거부 및 프록시 텔레메트리를 내보내는 표준 `/metrics` 엔드포인트.
- **슬라이딩 윈도우 레이트 리미팅 및 자동 보호 관찰**: 서브 버킷 인메모리 슬라이딩 윈도우 기반 트래픽 제어 및 속도 제한 계정에 대한 지수 백오프 보호 관찰 상태 머신.
- **다채널 쿼터 웹훅**: 80%(경고), 95%(위험), 100%(소진) 임계값에서 10분 디바운싱을 거쳐 디스코드 임베드, 슬랙 블록 키트 및 JSON 알림 발송.
- **레이트 리밋 자동 가로채기 및 무중단 장애 조치**: HTTP 429 및 게이트웨이 오류 발생 시 실시간으로 요청을 가로채 풀 내 다음 정상 계정으로 무중단 자동 재라우팅.
- **투명 리버스 프록시**: 버퍼링된 바디 재시도 및 RFC 7230 홉 바이 홉 헤더 제거를 통해 `/proxy/{service}/*`로 트래픽을 완벽히 전달.
- **동적 리소스 임대 (바우처)**: 내부 마이크로서비스, 서드파티 호출자 또는 테스트 스위트를 위한 단기 범위 한정 API 토큰 및 엔드포인트(`lse_...`) 발급.
- **단일 바이너리 배포**: `//go:embed`를 통해 Cloudflare 테마의 글래스모피즘 관리 대시보드가 단일 Go 바이너리에 내장.
- **초경량 메모리 점유율**: 외부 런타임 종속성 없이 지속적으로 **12 MB 이하의 RAM**으로 상시 구동.

---

## 🚀 빠른 시작

### 1. 다운로드 및 실행

```bash
# 리포지토리 클론
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# 직접 실행
go run .
```

관리 콘솔 접속 주소:
👉 **`http://localhost:9081`**

### 2. 다중 아키텍처 독립 실행형 바이너리 크로스 컴파일

```bash
# linux/amd64, linux/arm64, darwin/arm64, windows/amd64용 단독 바이너리 빌드
node scripts/build_artifacts.mjs --clean

# 암호화 SHA-256 체크섬 검증
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ 아키텍처 및 리소스 풀링

```text
┌────────────────────────────────────────────────────────────────────────┐
│                            cf-lease-broker                             │
│                         단일 Go 실행 바이너리                          │
├───────────────────┬───────────────────┬────────────────────────────────┤
│   internal/raft   │  internal/broker  │       internal/storage         │
│ • Raft 노드 코어  │ • 계정 풀         │ • 가상 S3 게이트웨이 (/s3/...) │
│ • WAL 저널        │ • 동적 임대       │ • 2MB 스트라이프 청커          │
│ • DNS SRV 피어링  │ • 쿼터 밸런서     │ • 파티셔닝 KV 캐시 레이어      │
│ • 브로커 FSM 동기 │ • 자동 장애 조치  │ • 다채널 웹훅                  │
├───────────────────┼───────────────────┼────────────────────────────────┤
│   internal/mtls   │ internal/security │       internal/compiler        │
│ • 에페머럴 CA     │ • Windows DPAPI   │ • 가상 JS Minifier / Wasm      │
│ • 클라이언트 인증 │ • AES-256-GCM     │ • 다중 영역 라우트 컴파일러    │
│ • TPM 2.0 원격증명│ • HS256 JWT / RBAC│ • 64KB 제로 카피 스트림 파이프 │
│ • 피어링 토큰     │ • 메모리 제로화   │ • 테넌트 번들 저장소           │
├───────────────────┴───────────────────┴────────────────────────────────┤
│                         internal/quota & config                        │
│  • Prometheus (/metrics) • 슬라이딩 윈도우 • 무중단 핫 리로딩          │
├────────────────────────────────────────────────────────────────────────┤
│                         net/http HTTP 엔진                             │
│   • /api/mtls/* (인증/검증/TPM)        • /api/compiler/*               │
│   • /raft/* (투표/추가/상태)           • /proxy/{svc}/* (mTLS 보호)    │
│   • /metrics         • /api/auth/*     • /s3/{bkt}/{key}               │
│   • /api/accounts    • /api/security   • /api/config/* (인증 보호)     │
├────────────────────────────────────────────────────────────────────────┤
│                         내장형 웹 대시보드                             │
│                   (글래스모피즘 UI via //go:embed)                     │
│                  web/dist/index.html -> Port 9081                      │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 🔒 시크릿 관리 및 인증

`cf-lease-broker`는 외부 서드파티 종속성 없이 엔터프라이즈급 시크릿 저장소와 인증을 즉시 제공합니다:

### 1. 봉투 암호화 금고 (`internal/security/vault.go`)
- **Windows DPAPI 연동**: Windows 시스템에서는 Windows DPAPI(`CryptProtectData`)를 통해 OS 레벨 자격증명(`dpapi:<base64>`)으로 API 키를 보호합니다.
- **AES-256-GCM 봉투**: 비 Windows 플랫폼(또는 지정 시)에서는 고유한 96비트 무작위 nonce를 사용하는 인증된 AES-256-GCM(`aes-gcm:<nonce>:<ciphertext>`)으로 시크릿을 암호화합니다.
- **메모리 스크러빙**: 민감한 바이트 버퍼는 사용 직후 `Zeroize` 메모리 스크러빙 패턴(`for i := range b { b[i] = 0 }`)을 통해 메모리에서 즉시 소거됩니다.

### 2. JWT 및 RBAC 미들웨어 (`internal/security/middleware.go`)
- **HS256 표준 토큰**: 타이밍 부채널 공격을 방지하기 위해 상수 시간 서명 검증(`crypto/subtle.ConstantTimeCompare`)을 적용하여 HMAC-SHA256 JWT를 발급 및 검증합니다.
- **해지 거부 목록**: 자동 TTL 만료 정리와 함께 JTI별 즉각적인 토큰 해지(`POST /api/auth/revoke`)를 지원합니다.
- **보호된 관리자 API**: 관리 엔드포인트(`/api/leases`, `/api/leases/revoke`, `/api/security/*`)는 유효한 `Authorization: Bearer <token>` 또는 루트 관리자 부트스트랩 키를 요구합니다.

```bash
# 1. 운영자 JWT 토큰 발급
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. 발급받은 Bearer 토큰으로 보호된 임대 엔드포인트 호출
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 가상 R2 S3 호환 게이트웨이 (`/s3/{bucket}/{key...}`)

브로커는 개별 테넌트의 저장소 쿼터를 초과하지 않고도 풀링된 Cloudflare R2 계정 전반에 오브젝트를 저장할 수 있도록 표준 AWS S3 호환 엔드포인트를 제공합니다:

### 핵심 기능:
1. **스트라이프 청킹**: 2MB를 초과하는 수신 페이로드는 순차 블록(`chk_<hash>_<index>`)으로 자동 분할되어 풀 내 정상 계정에 고르게 분산 저장됩니다.
2. **무결성 체크섬**: 각 청크는 개별 SHA-256 체크섬을 포함하며, 전체 오브젝트는 표준 멀티파트 호환 복합 ETag를 부여받습니다.
3. **HTTP Range 요청**: 비디오 스트리밍, 다운로드 이어받기 및 대용량 에셋의 부분 읽기를 위한 `Range: bytes=start-end` 헤더를 지원합니다.
4. **ListObjectsV2**: 표준 S3 XML `ListBucketResult` 응답 규격을 준수합니다.

### S3 CLI 및 Curl 예시:

```bash
# 1. 풀링된 R2 버킷에 오브젝트 저장
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. 오브젝트 다운로드
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. 부분 바이트 범위(Range) 요청
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf

# 4. 버킷 오브젝트 목록 조회 (S3 XML)
curl http://localhost:9081/s3/assets
```

---

## 🔄 투명 페일오버 리버스 프록시 (`/proxy/{service}/*`)

브로커는 `/proxy/{service}/*`에서 투명한 엣지 리버스 프록시를 제공합니다. 호출자는 어떤 테넌트 계정에 가용 쿼터가 남아있는지 알 필요 없이 이 엔드포인트로 HTTP 요청을 전송하기만 하면 됩니다.

### 장애 조치(Failover) 작동 원리:
1. **대상 결정**: 수신 요청의 `{service}`와 하위 경로는 가용 쿼터가 가장 많이 남아있는 풀링 계정(`SelectOptimalAccount`)으로 매핑됩니다.
2. **페이로드 버퍼링**: 재시도 시 데이터 유실 없이 전체 페이로드를 다시 전송할 수 있도록 스트리밍 리더를 사용하여 요청 본문을 메모리에 버퍼링(최대 10MB)합니다.
3. **429 및 게이트웨이 오류 가로채기**: 대상 계정이 HTTP `429 Too Many Requests`, `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout`으로 응답하거나 연결이 끊어지는 경우:
   - 브로커는 `TriggerFailover(accountID)`를 통해 해당 테넌트를 즉시 비정상 상태로 표시합니다.
   - 요청은 풀 내의 다음 정상 계정으로 무중단 자동 재시도됩니다.
4. **홉 바이 홉(Hop-by-Hop) 헤더 정리**: RFC 7230 홉 바이 홉 헤더(`Connection`, `Keep-Alive`, `Upgrade`, `TE`, `Trailers` 등)를 양방향 모두에서 자동 제거합니다.
5. **텔레메트리 헤더 반환**: 성공 응답에는 요청 처리 메타데이터가 헤더에 포함됩니다:
   - `X-CF-Broker-Account: {account_id}` (요청을 처리한 테넌트)
   - `X-CF-Broker-Attempts: {count}` (시도된 요청 횟수)

---

## 🔄 무중단 핫 리로딩 (`internal/config/`)

`cf-lease-broker`는 바이너리를 재시작하거나 활성 HTTP 연결을 끊지 않고도 동적 자격증명 로테이션과 계정 풀 갱신을 지원합니다:

### 핫 리로드 메커니즘:
1. **파일 감시 루프**: 설정 파일(`CF_BROKER_CONFIG_PATH` 또는 `config.json`)을 주기적으로 폴링하고, SHA-256 다이제스트를 통해 실제 내용 변경을 확인하여 불필요한 재로드를 방지합니다.
2. **POSIX 시그널 처리**: Unix/Linux/macOS에서 `SIGHUP` 시그널을 수신하면 즉시 동기식 설정 검증 및 리로드를 트리거합니다.
3. **원자적 상태 교체 및 안전 폴백**: 새로 읽어온 파일에 유효하지 않은 JSON, 음수 제한값, 중복 테넌트 ID가 포함된 경우 기존 설정을 100% 온전하게 유지하며 에러 로그와 함께 안전하게 실패 처리됩니다.
4. **자격증명 및 풀 동기화**: 검증을 통과한 테넌트는 `PoolManager`(`SyncAccounts`)와 동기화되며 `SecretVault`(`StoreCredential`)에 즉시 암호화 저장됩니다.

### 설정 파일 포맷 (`config.json`):
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

## 🚀 엣지 워커 가상 컴파일러 및 동적 라우트 주입

브로커는 멀티 테넌트 엣지 워커 스크립트와 WebAssembly 모듈을 위한 가상 컴파일 및 라우팅 엔진을 탑재하고 있습니다:

### 1. 인메모리 어휘 축소 및 패키징 (`internal/compiler/bundle.go`)
- **어휘 토크나이저**: 단일 행(`//`) 및 다중 행(`/* */`) 주석을 제거하고 공백을 정규화하며, Cloudflare Worker 모듈 익스포트 시그니처(`export default { fetch(...) }`, `addEventListener('fetch')`)를 검증합니다.
- **WebAssembly 검증**: 패키징 전 4바이트 매직 헤더(`\x00asm`) 및 버전 규격 준수를 검사합니다.
- **콘텐츠 해싱**: 번들 버전 관리 및 감사 추적을 위해 결정론적 SHA-256 다이제스트를 생성합니다.

### 2. 다중 영역 와일드카드 라우트 컴파일러 (`internal/compiler/router.go`)
- **임의 도메인 매핑**: 와일드카드 서브도메인(`*.domain.com/*`), 완전 일치 호스트(`api.domain.com/v1/*`), 글로벌 폴백 규칙(`*/*`) 전반에 엔드포인트를 매핑합니다.
- **우선순위 가중치 트라이**: 호스트와 경로의 구체성 점수(완전 일치 호스트 > 와일드카드 서브도메인 > 글로벌, 긴 경로 접두사 > 짧은 접두사)를 평가하여 설정 재로드 없이 트래픽을 동적으로 라우팅합니다.

### 3. 제로 카피 다이렉트 스트리밍 파이프라인 (`internal/compiler/stream.go`)
- **버퍼 풀**: 대용량 프록시 전송 중 힙 할당을 방지하기 위해 사전 할당된 64KB 슬라이스(`sync.Pool`)를 재사용합니다.
- **소켓 즉시 플러시**: `http.Flusher`를 활용하여 응답 청크를 실시간 스트리밍하며, `X-Accel-Buffering: no`로 중간 캐싱을 차단합니다.
- **연결 해제 감지**: `context.Context`와 연동하여 클라이언트 연결 종료 시 업스트림 스트림을 즉시 종료함으로써 소켓 누수를 원천 차단합니다.

---

## 🤝 고가용성 Raft 분산 합의 엔진

`cf-lease-broker`는 멀티 리전 브로커 클러스터링, 무손실 임대 상태 복제 및 자동 리더 장애 조치를 지원하는 제로 의존성 Raft 합의 엔진(`internal/raft/`)을 내장하고 있습니다:

### 핵심 아키텍처 및 합의 원리:
1. **정족수 리더 선출 및 무작위 타이머**: 노드는 Follower, Candidate, Leader 상태로 전환됩니다. 무작위 선거 타임아웃(150~300ms)으로 스플릿 보트 데드락을 방지하며, 50ms 하트비트 티커로 과반 정족수 `((N/2) + 1)` 전반에 리더 권한을 유지합니다.
2. **WAL 저널 (`internal/raft/journal.go`)**: SHA-256 무결성 체크섬이 포함된 단조 트랜잭션 인덱싱을 강제합니다. 추가 전 팔로워 로그 일관성을 검증하며 충돌하는 미커밋 항목을 자동 정리합니다. 디스크 추가 전용 파일(`raft_journal.wal`) 저장을 지원합니다.
3. **DNS SRV 동적 피어 탐색 (`internal/raft/discovery.go`)**: RFC 2782 DNS SRV 레코드(`_raft._tcp.<domain>`)를 통해 클러스터 피어를 자동 확인하며 환경 변수(`CF_RAFT_PEERS`) 및 수동 등록(`POST /raft/peers`)으로 유연하게 폴백합니다.
4. **브로커 유한 상태 머신 (FSM) (`internal/raft/fsm_broker.go`)**: 트랜잭션 명령을 모든 노드에 결정론적으로 복제합니다:
   - `GRANT_LEASE`: 클러스터 전역에 활성 임대 바우처 복제.
   - `REVOKE_LEASE`: 모든 피어 엔진에 임대 해지 전파.
   - `SYNC_ACCOUNT`: 테넌트 프로필 및 쿼터 한도 동기화.
   - `UPDATE_QUOTA`: 일일 워커 요청 증분, R2 스토리지 델타 및 KV 읽기 수 전파.
   - `NOOP`: 클러스터 준비 상태 확인을 위한 합의 배리어 프로브.

### 클러스터 토폴로지 흐름:

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

### Raft 클러스터 Curl 예시:

```bash
# 1. 로컬 노드 상태 및 클러스터 토폴로지 점검
curl http://localhost:9081/raft/status

# 2. 피어 헬스 및 왕복 지연 시간(RTT) 쿼리
curl http://localhost:9081/raft/peers

# 3. 새로운 피어 노드 수동 등록
curl -X POST http://localhost:9081/raft/peers \
  -H "Content-Type: application/json" \
  -d '{"id": "node-2", "address": "http://127.0.0.1:9082"}'

# 4. 클러스터에 상태 머신 트랜잭션 제안
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

# 5. 복제된 WAL 저널 항목 및 SHA-256 체크섬 확인
curl http://localhost:9081/raft/journal
```

---

## 🛡️ 제로 트러스트 상호 TLS (mTLS) 및 하드웨어 원격 증명

`cf-lease-broker`는 완벽한 제로 트러스트 상호 TLS (mTLS) 엔진, 동적 인메모리 에페머럴 인증 기관 (CA) 및 하드웨어 기반 TPM 2.0 원격 증명을 통합 제공합니다:

### 1. 에페머럴 인증 기관 및 동적 발급 (`internal/mtls/ca.go`)
- **인메모리 루트 앵커**: 외부 도구 없이 메모리에서 ECDSA P-256 루트 CA(`x509.IsCA = true`)를 동적으로 부트스트래핑합니다.
- **클라이언트 및 피어 인증서**: 커스텀 Common Name, 유효 기간 및 SAN이 포함된 단기 X.509 인증서를 동적 발급합니다.
- **CRL 해지 관리**: 16진수 일련번호로 색인화된 스레드 안전 인증서 해지 목록(CRL)을 유지하여 O(1) 거부 목록 조회를 지원합니다.
- **서명된 피어링 바우처**: 브로커 간 제로 트러스트 통신을 위한 ECDSA-SHA256 피어링 토큰을 발급하고 검증합니다.

### 2. 클라이언트 인증서 검증 및 정책 강제 (`internal/mtls/verifier.go`)
- **트러스트 체인 검증**: 수신된 X.509 클라이언트 인증서를 루트 CA 풀(`x509.CertPool`)과 대조 검증합니다.
- **정책 강제**: 와일드카드 SAN 매칭(`*.edge.cluster.local`, CIDR 블록), 허용된 Common Name, 허용된 발급자 및 엄격한 만료 기간을 강제합니다.
- **투명한 엣지 헤더 추출**: 직접 TLS 연결(`r.TLS.PeerCertificates`) 또는 엣지 전달 헤더(`X-Client-Cert-Pem`, `X-Forwarded-Client-Cert`, `X-Client-Cert-Der`)에서 인증서를 추출합니다.
- **리버스 프록시 보호**: `CF_REQUIRE_MTLS=true` 활성화 시 `/proxy/{service}/*` 통과 전 반드시 인증서 검증을 거쳐야 합니다.

### 3. 하드웨어 TPM 2.0 원격 증명 (`internal/mtls/tpm_windows.go`)
- **Windows CNG 연동**: Microsoft Cryptographic Next Generation(`ncrypt.dll`)을 통해 Microsoft Platform Crypto Provider의 TPM 2.0 탑재를 확인합니다.
- **Windows 시스템 인증서 저장소**: `crypt32.dll`(`CertOpenSystemStoreW`)로 머신/사용자 인증서 저장소를 검사하고 유효성을 확인합니다.
- **플랫폼 인용 (Quote)**: 감사 추적을 위해 결정론적 플랫폼 구성 레지스터(PCR) 다이제스트 및 보증 키 측정값을 생성합니다.

### mTLS 및 TPM Curl 예시:

```bash
# 1. mTLS 상태, 활성 루트 CA 및 TPM 증명 상태 점검
curl http://localhost:9081/api/mtls/status

# 2. PEM 포맷 루트 CA 공개 인증서 다운로드
curl http://localhost:9081/api/mtls/ca/cert -o root-ca.pem

# 3. 에페머럴 클라이언트 인증서 발급
curl -X POST http://localhost:9081/api/mtls/ca/issue \
  -H "Content-Type: application/json" \
  -d '{
    "common_name": "worker-edge-alpha",
    "sans": ["*.edge.broker.local", "127.0.0.1"],
    "validity_hours": 24
  }'

# 4. 보안 정책에 따른 X.509 PEM 인증서 검증
curl -X POST http://localhost:9081/api/mtls/verify \
  -H "Content-Type: application/json" \
  -d '{"certificate_pem": "-----BEGIN CERTIFICATE-----\n..."}'

# 5. 인증서 일련번호 해지
curl -X POST http://localhost:9081/api/mtls/revoke \
  -H "Content-Type: application/json" \
  -d '{"serial_number": "7b8a1c9d2e"}'

# 6. 하드웨어 TPM 2.0 원격 증명 인용 쿼리
curl http://localhost:9081/api/mtls/tpm/attest
```

---

## 💰 자율 비용 최적화 및 다중 테넌트 청구 원장

`cf-lease-broker`는 사용량을 마이크로센트 단위까지 정밀 추적하고 클라우드 초과 요금을 사전에 방지하도록 설계된 고정밀 멀티 테넌트 청구 엔진과 비용 최적화기를 탑재하고 있습니다:

### 1. 세분화된 마이크로센트 비용 귀속 (`internal/billing/meter.go`)
- **마이크로센트 정밀도**: 부동소수점 오차를 완전히 배제하기 위해 정수 마이크로센트($1.00 = 1,000,000 마이크로센트)로 리소스 사용량을 추적합니다.
- **공식 Cloudflare 요금 모델 완벽 적용**:
  - Workers 요청: 100만 요청당 $0.30 (300,000 마이크로센트)
  - Workers CPU 시간: 100만 CPU ms당 $0.02 (20,000 마이크로센트)
  - KV 읽기: 100만 읽기당 $0.50 (500,000 마이크로센트)
  - KV 쓰기: 100만 쓰기당 $5.00 (5,000,000 마이크로센트)
  - R2 스토리지: GB-월당 $0.015 (15,000 마이크로센트)
  - R2 Class A 작업: 100만 작업당 $4.50 (4,500,000 마이크로센트)
  - R2 Class B 작업: 100만 작업당 $0.36 (360,000 마이크로센트)
  - 아웃바운드 트래픽: $0.00 (Cloudflare 제로 아웃바운드 보장)
- **일일 무료 제공량 추적**: 계정별 일일 무료 제공량(무료 요청 10만 건, 무료 KV 읽기 10만 건)을 UTC 자정 롤오버로 추적하여 $0 비용 처리와 함께 누적 절감액을 산출합니다.

### 2. 자율 워크로드 마이그레이션 (`internal/billing/optimizer.go`)
- **선제적 초과 감지**: 계정 사용량을 설정된 임계치(기본 80%)와 상시 대조 감사합니다.
- **대기 무료 티어 우선순위**: 유료 계정이 쿼터나 예산 상한선에 도달하면 최적화 엔진이 자율적으로 트래픽을 재라우팅하거나 보호 관찰 쿨다운을 적용하여, 사용하지 않은 일일 쿼터가 남아있는 정상적인 무료 티어 계정으로 트래픽을 이전합니다.
- **누적 절감액 보고**: 유료 초과 요금을 방어함으로써 절감된 정밀 마이크로센트 금액을 실시간 계산하고 보고합니다.

### 3. OpenCost 1.0 및 다중 포맷 재무 내보내기 (`internal/billing/journal.go`)
- **OpenCost 1.0 규격**: OpenCost 엣지 컴퓨팅 사양과 직접 일치하는 형식으로 테넌트 청구 내역을 내보냅니다 (`/api/billing/ledger?format=opencost`).
- **RFC 4180 CSV 내보내기**: 스프레드시트 분석 및 재무 감사를 위한 CSV 파일을 생성합니다 (`/api/billing/ledger?format=csv`).
- **줄바꿈 구분 JSON (JSONL)**: BigQuery, Datadog 또는 Elasticsearch 수집용 스트리밍 로그를 생성합니다 (`/api/billing/ledger?format=jsonl`).

### 청구 및 비용 Curl 예시:

```bash
# 1. 클러스터 전체 비용 요약 및 테넌트별 내역 쿼리
curl http://localhost:9081/api/billing/summary

# 2. OpenCost 1.0 JSON 포맷으로 청구 원장 내보내기
curl http://localhost:9081/api/billing/ledger?format=opencost

# 3. RFC 4180 CSV 청구 저널 다운로드
curl http://localhost:9081/api/billing/ledger?format=csv -o ledger.csv

# 4. 온디맨드 워크로드 마이그레이션 평가 트리거
curl -X POST http://localhost:9081/api/billing/optimize

# 5. 최근 자율 마이그레이션 이력 및 절감액 확인
curl http://localhost:9081/api/billing/migrations

# 6. 커스텀 테넌트 리소스 사용 이벤트 수집
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

## 🛰️ 지능형 엣지 헬스 레이더 및 애니캐스트 지리 라우팅

`cf-lease-broker`는 전 세계 Cloudflare 거점(PoP)을 모니터링하고 패킷 손실이 발생하기 전에 네트워크 이상 징후를 감지하며 밀리초 미만의 정밀도로 최적의 Anycast 경로를 확인하는 자율 엣지 헬스 레이더 엔진을 탑재하고 있습니다:

### 1. 합성 엣지 프로빙 및 텔레메트리 (`internal/radar/probe.go`)
- **지수 이동 평균 (EMA)**: 일시적 지터로 인한 경로 요동을 방지하기 위해 고주파 프로브 관측값을 스무딩(`alpha=0.25`) 처리합니다.
- **지터 및 손실률 산출**: 통계적 지터와 패킷 손실 비율을 계산하여 PoP별 0~100 복합 헬스 인덱스를 산출합니다.
- **동시성 디스패치**: 리소스 고갈 없이 엣지 엔드포인트를 동시에 핑 테스트하는 제한된 워커 풀을 사용합니다.

### 2. 서브 밀리초 Anycast 지리 라우팅 (`internal/radar/router.go`)
- **Haversine 거리 계산**: 클라이언트 좌표와 엣지 PoP 위치 간의 대원 구면 거리를 정밀하게 계산합니다.
- **종합 점수화**: 지리적 거리, 지연 시간 EMA, 지터 및 운영 헬스 상태를 종합적으로 최적화합니다.
- **테넌트 친화도 (Affinity)**: 서브 밀리초 수준의 라우트 결정을 유지하면서 테넌트 임대를 최적의 지역 PoP에 바인딩합니다.

### 3. BGP 경로 이상 탐지 및 선제적 마이그레이션
- **경로 플랩 탐지**: 기준선 EMA 대비 35ms 이상 또는 1.8배 이상의 지연 시간 편차를 치명적 이상으로 분류합니다.
- **선제적 대피**: 클라이언트 연결에서 패킷 손실이나 소켓 타임아웃이 발생하기 전에 성능이 저하된 PoP에 할당된 활성 임대를 대기 중인 정상 PoP로 자동 이전합니다.

### Radar Curl 예시:

```bash
# 1. 레이더 엔진 상태 및 글로벌 텔레메트리 요약 조회
curl http://localhost:9081/api/radar/status

# 2. 아시아 태평양 지역에서 모니터링 중인 전체 PoP 목록 조회
curl "http://localhost:9081/api/radar/pops?region=Asia%20Pacific"

# 3. 특정 클라이언트 위치(서울, 대한민국)에 대한 최적 엣지 PoP 분석
curl -X POST http://localhost:9081/api/radar/route \
  -H "Content-Type: application/json" \
  -d '{"lat": 37.5665, "lon": 126.9780, "country": "KR"}'

# 4. 전체 엣지 PoP에 대한 동시성 합성 프로빙 트리거
curl -X POST http://localhost:9081/api/radar/probe \
  -H "Content-Type: application/json" \
  -d '{"concurrency": 10}'

# 5. 최근 BGP 경로 플랩 및 자율 마이그레이션 이벤트 조회
curl http://localhost:9081/api/radar/events
```

---

## 🔄 자율 엣지 토큰 로테이터 및 해지 메시

`cf-lease-broker`는 자율 엣지 토큰 로테이션 엔진, 초고속 가십 기반 해지 메시 및 위임된 갱신 작업을 위한 하드웨어 기반 TPM 2.0 원격 증명을 제공합니다:

### 1. 자율 선제적 토큰 갱신 (`internal/rotator/rotator.go`)
- **선제적 임계값 트리거**: 활성 임대 토큰을 지속 평가하여 토큰 유효 기간의 80%에 도달하면 백그라운드 갱신을 실행, 엣지 클라이언트 단절을 원천 방지합니다.
- **무중단 인계 유예 기간**: 인메모리 유예 기간 매핑(기본 30초)에 이전 토큰 키를 보관하여 처리 중인 요청과 엣지 프록시가 연결 끊김이나 HTTP 401 오류 없이 원활히 전환되도록 보장합니다.
- **임대 바우처 동기화**: 갱신된 토큰 키와 연장된 만료 시간을 `internal/broker/lease.go`와 실시간 동기화합니다.

### 2. 5ms 미만 가십 해지 메시 (`internal/rotator/mesh.go`)
- **탈중앙화 툼스톤 원장**: 암호화 서명된 해지 툼스톤을 $O(1)$ 조회 복잡도(`IsRevoked(id)`)로 저장합니다.
- **5ms 미만 전파**: 5밀리초 미만 내에 클러스터 노드 전반으로 툼스톤을 즉시 브로드캐스트합니다.
- **HMAC-SHA256 위변조 방지**: 모든 툼스톤은 단조 시퀀스 번호로 서명되며 상수 시간 비교를 통해 무결성을 검증합니다.
- **안티 엔트로피 상태 동기화**: 피어 간 가십 핸드셰이크 중 누락된 툼스톤을 자동 복구합니다.

### 3. 위임 갱신을 위한 하드웨어 TPM 2.0 원격 증명 (`internal/rotator/attest.go`)
- **단일 사용 챌린지 난수**: 만료 시간이 추적되는 암호화 챌린지 난수를 생성하여 재생 공격을 엄격히 차단합니다.
- **플랫폼 무결성 검증**: 위임된 갱신 요청에 대해 하드웨어 플랫폼 구성 레지스터(PCR) 다이제스트 및 TPM 2.0 인용을 검증한 후 토큰을 발급합니다.

### Rotator 및 메시 Curl 예시:

```bash
# 1. 로테이터 요약, 활성 토큰 및 메시 지연 시간 조회
curl http://localhost:9081/api/rotator/status

# 2. 관리 대상 전체 로테이션 토큰 목록 조회
curl http://localhost:9081/api/rotator/tokens

# 3. 특정 토큰에 대한 온디맨드 로테이션 수동 트리거
curl -X POST http://localhost:9081/api/rotator/renew \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "operator_manual_trigger"}'

# 4. 가십 메시를 통한 즉각적인 해지 전파 (<5ms)
curl -X POST http://localhost:9081/api/rotator/revoke \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "security_audit_invalidation"}'

# 5. 1회용 하드웨어 원격 증명 챌린지 난수 요청
curl http://localhost:9081/api/rotator/challenge

# 6. 하드웨어 TPM 인용을 사용한 위임 토큰 갱신 실행
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

# 7. 활성 해지 툼스톤 목록 확인
curl http://localhost:9081/api/rotator/tombstones
```

---

## 📦 엣지 Wasm 샌드박스 메모리 제한기 및 충돌 격리

멀티 테넌트 브로커 환경에서 실행되는 엣지 워커 워크로드는 악성 스크립트, 메모리 누수 또는 처리되지 않은 예외가 호스트 브로커를 다운시키거나 테넌트 간 데이터를 유출하는 것을 방지하기 위해 엄격한 리소스 경계와 오류 격리를 필요로 합니다.

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

### 핵심 아키텍처 역량

1. **엄격한 32 MiB 물리적 상한선**: WebAssembly 선형 메모리는 64 KiB 페이지(`WasmPageSize = 65536`)로 분할 관리됩니다. 테넌트 정책이 더 높은 값을 요청하더라도 브로커는 최대 512페이지(32 MiB, `HardCeilingMemoryPages`)로 제한합니다. 이 상한을 초과하는 할당은 `ErrMemoryHardCeilingExceeded`와 함께 즉시 거부되며 OOM 차단으로 기록됩니다.
2. **결정론적 가스 계측**: 명령어는 사전 정의된 가중치(기본 연산: 1, 산술: 2, 메모리 접근: 10, 루프 단계: 5, 호출: 20, 메모리 증가: 페이지당 100)에 따라 가스를 차감합니다. 가스가 소진되면 실행은 `ErrGasExhausted`와 함께 결정론적으로 중단되어 브로커 스레드를 멈추지 않고 무한 루프를 차단합니다.
3. **패닉 및 오류 격리**: 게스트 실행은 지연 복구 로직(`defer func() { if r := recover(); ... }`)으로 감싸집니다. 심각한 오류(예: SIGSEGV 널 포인터 참조 또는 런타임 패닉)가 발생하면 스택 트레이스가 포함된 독립된 `CrashReport`를 생성하여 기록하며, 호스트 브로커는 어떠한 중단 없이 계속 정상 트래픽을 처리합니다.
4. **충돌 저널 및 단계적 격리**: 스레드 안전 순환 링 버퍼에 타임스탬프, 원인, 메모리 상태가 포함된 최근 500개의 충돌 보고서를 유지합니다. 테넌트가 설정된 연속 충돌 임계값(기본: 5회)을 초과하면 운영자가 리셋하기 전까지 자동으로 `QUARANTINED`(격리) 상태로 전환됩니다.
5. **제로화 스크러빙 (Zeroization)**: 인스턴스 해제 또는 리셋 시 모든 선형 메모리 슬라이스를 0 바이트로 덮어써 초기화(`subtle.ConstantTimeCompare`)함으로써 테넌트 간 잔류 데이터 및 토큰 유출을 원천 방지합니다.

### 사용법 및 예제 명령어

```bash
# 1. 샌드박스 슈퍼바이저 상태 및 집계 메트릭 확인
curl http://localhost:9081/api/sandbox/status

# 2. 게스트 Wasm 샌드박스에서 연산 워크로드 실행
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "compute",
    "gas_budget": 100000
  }'

# 3. 결정론적 무한 루프 트랩 테스트 (가스 소진 시 즉각 중단)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "infinite_loop",
    "gas_budget": 5000
  }'

# 4. 32MB 물리적 상한선 강제 테스트 (OOM 거부)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "memory_alloc",
    "requested_pages": 600
  }'

# 5. 게스트 패닉/결함 격리 테스트 (호스트 브로커 100% 정상 유지)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "panic_fault"
  }'

# 6. 최근 충돌 저널 보고서 및 복구된 스택 트레이스 조회
curl "http://localhost:9081/api/sandbox/crashes?limit=10"

# 7. 테넌트 샌드박스 격리 정책 및 쿼터 등록/업데이트
curl -X POST http://localhost:9081/api/sandbox/tenants \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "max_memory_pages": 256,
    "gas_limit": 2000000,
    "max_crash_threshold": 5
  }'

# 8. 인스턴스 리셋 및 격리 상태 해제
curl -X POST http://localhost:9081/api/sandbox/reset \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant-edge-prod", "instance_id": "inst-1"}'
```

---

## 📡 REST API 레퍼런스

| 엔드포인트 | 메서드 | 인증 | 설명 |
| :--- | :---: | :---: | :--- |
| `/api/sandbox/status` | `GET` | 공개 | 샌드박스 슈퍼바이저 상태, 활성 인스턴스, OOM 차단 및 패닉 복구 메트릭 |
| `/api/sandbox/execute` | `POST` | 공개 | 엄격한 메모리 제한 및 가스 계측 하에 게스트 Wasm 워크로드 실행 |
| `/api/sandbox/tenants` | `GET/POST` | 공개 | 테넌트 샌드박스 격리 정책 및 쿼터 조회 또는 업데이트 |
| `/api/sandbox/crashes` | `GET` | 공개 | 포착된 결함 및 복구된 스택 트레이스를 기록하는 순환 충돌 저널 |
| `/api/sandbox/crashes/clear` | `POST` | 공개 | 인메모리 충돌 저널 링 버퍼 초기화 |
| `/api/sandbox/reset` | `POST` | 공개 | 충돌하거나 격리된 샌드박스 인스턴스를 새로운 IDLE 상태로 리셋 |
| `/api/steering/status` | `GET` | 공개 | HTTP/3 eBPF 스티어링 상태, 활성 모드 및 처리량 통계 |
| `/api/steering/endpoints` | `GET/POST/DELETE` | 공개 | 다이렉트 스티어링용 업스트림 워커 엔드포인트 목록 조회, 등록 또는 삭제 |
| `/api/steering/rules` | `GET/POST/DELETE` | 공개 | QUIC 연결 ID(CID) 접두사 라우팅 규칙 목록 조회, 추가 또는 삭제 |
| `/api/steering/simulate` | `POST` | 공개 | UDP/QUIC 패킷 헤더 검사 시뮬레이션 및 마이크로초 디스패치 레이턴시 측정 |
| `/api/steering/stats` | `GET` | 공개 | 정밀 마이크로초 디스패치 텔레메트리 및 10µs 미만 비율 통계 |
| `/api/rotator/status` | `GET` | 공개 | 로테이터 상태, 활성 토큰, 갱신 및 메시 전파 지연 시간 조회 |
| `/api/rotator/tokens` | `GET` | 공개 | 로테이션 관리 대상 활성 토큰 목록 조회 |
| `/api/rotator/renew` | `POST` | 공개 | 즉각적인 토큰 키 로테이션 및 갱신 트리거 |
| `/api/rotator/revoke` | `POST` | 공개 | 가십 메시를 통한 즉각적인 토큰/임대 해지 브로드캐스트 |
| `/api/rotator/gossip` | `POST` | 공개 | 툼스톤 및 메시 멤버십 교환을 위한 피어 가십 엔드포인트 |
| `/api/rotator/delegate` | `POST` | 공개 | 하드웨어 TPM 원격 증명을 요구하는 위임 토큰 갱신 |
| `/api/rotator/challenge` | `GET` | 공개 | 하드웨어 원격 증명용 1회용 챌린지 난수 생성 |
| `/api/rotator/tombstones` | `GET` | 공개 | 로컬 원장에 저장된 서명된 해지 툼스톤 목록 |
| `/api/rotator/peers` | `GET/POST` | 공개 | 메시 피어 목록 조회 또는 신규 가십 피어 노드 등록 |
| `/api/rotator/events` | `GET` | 공개 | 최근 로테이션 및 해지 이벤트 감사 로그 |
| `/api/radar/status` | `GET` | 공개 | 레이더 엔진 상태, 추적 중인 PoP, 글로벌 지연 시간 및 마이그레이션 건수 |
| `/api/radar/pops` | `GET` | 공개 | 텔레메트리 통계가 포함된 모니터링 엣지 PoP 목록 (`?region=...`) |
| `/api/radar/probe` | `POST` | 공개 | 특정 지역(`?colo=...`) 또는 전체 PoP에 대한 합성 프로빙 트리거 |
| `/api/radar/route` | `GET/POST` | 공개 | 서브 밀리초 Anycast 경로 분석기 (`?lat=...&lon=...&country=...`) |
| `/api/radar/events` | `GET` | 공개 | 최근 BGP 경로 이상 징후 및 자율 마이그레이션 저널 |
| `/api/radar/migrate` | `POST` | 공개 | 성능이 저하된 특정 지역에서 활성 임대 강제 대피 |
| `/api/billing/summary` | `GET` | 공개 | 클러스터 비용 요약, 절감액 및 테넌트/계정 집계 |
| `/api/billing/ledger` | `GET` | 공개 | 원장 내보내기 (`?format=json|csv|jsonl|opencost&tenant=...&limit=...`) |
| `/api/billing/meter` | `POST` | 공개 | 리소스 소비 기록 수집 및 비용 귀속 |
| `/api/billing/optimize` | `POST` | 공개 | 자율 워크로드 마이그레이션 평가 트리거 |
| `/api/billing/migrations` | `GET` | 공개 | 최근 워크로드 마이그레이션 이력 및 총 절감액 확인 |
| `/api/billing/rates` | `GET/POST` | 공개 | 활성 Cloudflare 마이크로센트 요금표 조회 또는 업데이트 |
| `/api/billing/reset` | `POST` | 공개 | 인메모리 청구 원장 및 집계 데이터 초기화 |
| `/api/mtls/status` | `GET` | 공개 | 활성 루트 CA 상태, 정책 및 하드웨어 TPM 2.0 원격 증명 점검 |
| `/api/mtls/ca/cert` | `GET` | 공개 | PEM 포맷 루트 CA 공개 인증서 다운로드 |
| `/api/mtls/ca/issue` | `POST` | 공개 | SAN 및 커스텀 유효 기간이 지정된 에페머럴 클라이언트/피어 인증서 발급 |
| `/api/mtls/verify` | `POST` | 공개 | 활성 CA 신뢰 풀 및 보안 정책에 따라 X.509 PEM 인증서 평가 |
| `/api/mtls/revoke` | `POST` | 공개 | 인증서 일련번호를 해지하고 CRL 거부 목록에 추가 |
| `/api/mtls/tpm/attest` | `GET` | 공개 | Windows TPM 2.0 플랫폼 공급자 인용 및 시스템 인증서 저장소 조회 |
| `/api/mtls/peer-token` | `POST` | 공개 | 브로커 클러스터 간 동기화를 위한 ECDSA 서명 피어링 토큰 생성 |
| `/api/mtls/peer-token/verify` | `POST` | 공개 | 브로커 클러스터 간 피어링 토큰 암호화 검증 |
| `/api/mtls/policy` | `POST/PUT` | 공개 | 활성 클라이언트 인증서 검증 정책 업데이트 |
| `/raft/status` | `GET` | 공개 | 로컬 노드 역할, 임기, 리더 ID, 커밋 인덱스 및 클러스터 텔레메트리 점검 |
| `/raft/peers` | `GET/POST` | 공개 | 클러스터 피어 및 왕복 지연 시간 점검, 원격 노드 수동 등록 |
| `/raft/propose` | `POST` | 공개 | 상태 머신 명령 제안 (`GRANT_LEASE`, `REVOKE_LEASE`, `UPDATE_QUOTA`, `SYNC_ACCOUNT`) |
| `/raft/journal` | `GET` | 공개 | SHA-256 체크섬이 포함된 복제된 WAL 저널 항목 전체 스냅샷 |
| `/raft/vote` | `POST` | 공개 | 후보자 투표를 위한 Raft 선거 투표 엔드포인트 |
| `/raft/append` | `POST` | 공개 | Raft 하트비트 및 로그 복제 RPC 엔드포인트 |
| `/metrics` | `GET` | 공개 | 표준 Prometheus 0.0.4 텍스트 포맷 메트릭 스크래퍼 |
| `/api/compiler/compile` | `POST` | 공개 | JavaScript 워커 소스를 컴파일 및 어휘 압축하여 압축률과 문법 상태 반환 |
| `/api/compiler/workers` | `GET` | 공개 | 배포된 모든 워커 번들 및 SHA-256 콘텐츠 해시 목록 |
| `/api/compiler/workers` | `POST` | 공개 | 테넌트용 신규 엣지 워커 번들 패키징 및 배포 |
| `/api/compiler/workers` | `DELETE` | 공개 | 워커 번들 배포 해제 (`?id=bundle-...`) |
| `/api/compiler/routes` | `GET` | 공개 | 우선순위 내림차순으로 정렬된 등록 동적 라우트 목록 |
| `/api/compiler/routes` | `POST` | 공개 | 신규 동적 라우트 패턴 등록 (`pattern`, `zone_id`, `tenant_id`, `worker_id`) |
| `/api/compiler/routes` | `DELETE` | 공개 | 동적 라우트 바인딩 제거 (`?id=route-...`) |
| `/api/compiler/routes/match` | `POST` | 공개 | URL 라우트 패턴 일치 테스트 (`host`, `path`) |
| `/api/compiler/stream-test` | `GET` | 공개 | 제로 카피 스트리밍 벤치마크 엔드포인트 (`?size_mb=1..50`) |
| `/api/config/status` | `GET` | **Bearer** | 핫 리로드 텔레메트리 통계 및 마스킹된 계정 목록 (토큰 마스킹) |
| `/api/config/reload` | `POST` | **Bearer** | 디스크로부터 설정 파일의 무중단 핫 리로드 즉시 트리거 |
| `/api/quota/alerts` | `GET` | 공개 | 기록된 쿼터 임계값 알림 히스토리 |
| `/api/quota/webhooks/config` | `GET/POST` | 공개 | 디스코드/슬랙 웹훅 URL 점검 또는 업데이트 |
| `/api/quota/webhooks/test` | `POST` | 공개 | 테스트 쿼터 임계값 알림 발송 (80%/95%/100%) |
| `/api/quota/probation` | `GET` | 공개 | 풀링된 계정의 현재 보호 관찰 기록 및 타이머 |
| `/api/quota/probation/recover` | `POST` | 공개 | 보호 관찰 중인 계정 수동 복구 (`?id=...`) |
| `/api/quota/probation/trigger` | `POST` | 공개 | 429 레이트 리밋 보호 관찰 트리거 시뮬레이션 (`?id=...`) |
| `/api/quota/ratelimit` | `GET` | 공개 | 슬라이딩 윈도우 리미터 텔레메트리 (`?key=...`) |
| `/api/auth/token` | `POST` | 공개 / Key | 범위 한정 HS256 JWT Bearer 토큰 발급 |
| `/api/auth/revoke` | `POST` | Bearer | JTI 기준 JWT 토큰 즉시 해지 |
| `/api/auth/me` | `GET` | Bearer | 활성 호출자 클레임 검증 및 확인 |
| `/api/auth/status` | `GET` | 공개 | 암호화 공급자 및 인증 엔진 상태 확인 |
| `/api/security/encrypt` | `POST` | Bearer | 시크릿 페이로드를 DPAPI 또는 AES-GCM 봉투로 암호화 |
| `/api/security/decrypt` | `POST` | Bearer | 왕복 봉투 복호화 테스트 |
| `/api/security/status` | `GET` | Bearer | 시크릿 금고 엔진 및 활성 공급자 점검 |
| `/s3/{bucket}/{key...}` | `PUT/GET/HEAD/DELETE` | 공개 | 청크 스트라이핑 기능이 포함된 S3 호환 가상 스토리지 게이트웨이 |
| `/s3/{bucket}` | `GET` | 공개 | 버킷 대상 S3 ListObjectsV2 XML 목록 조회 |
| `/api/kv/{ns}/{key}` | `GET/PUT/DELETE` | 공개 | 다중 계정 파티셔닝 KV 캐싱 엔드포인트 |
| `/api/kv/stats` | `GET` | 공개 | 캐시 효율, 히트율 및 테넌트 파티션 메트릭 |
| `/proxy/{service}/*` | `ANY` | 공개 | 429 가로채기 기능이 탑재된 투명 페일오버 리버스 프록시 |
| `/api/accounts` | `GET` | 공개 | 풀링된 Cloudflare 전체 계정 및 한도 스냅샷 |
| `/api/leases` | `GET` | **Bearer** | 활성 및 해지된 전체 리소스 임대 목록 조회 |
| `/api/leases` | `POST` | **Bearer** | 신규 동적 임대 부여 (`target_service`, `resource_type`) |
| `/api/leases/revoke` | `POST` | **Bearer** | 임대 바우처 즉각 무효화 및 해지 (`?id=lse_...`) |
| `/api/health` | `GET` | 공개 | Cloudflare 공용 API 엣지와의 레이턴시 및 연결성 검증 |

---

## 🗺️ 로드맵 (Roadmap)

마일스톤 목표 및 계획된 기능은 [ROADMAP.md](../../ROADMAP.md)를 참조하십시오.

---

## 🔒 보안 (Security)

취약점 공개 및 보고 절차는 [SECURITY.md](../../SECURITY.md)를 참조하십시오.

---

## 📄 라이선스 (License)

[Apache License 2.0](../../LICENSE) 하에 라이선스가 부여됩니다.
