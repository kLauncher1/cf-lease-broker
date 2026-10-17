<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <strong>🇰🇷 한국어</strong> · <a href="./zh-CN.md">🇨🇳 中文</a> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**다중 계정 Cloudflare 리소스 풀링 및 Workers, KV, R2 동적 토큰 임대 브로커**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-빠른-시작)

<sub>제작자: **kLauncher1** · 고가용성 엣지 리소스 풀링, 보안 시크릿 금고 및 동적 테넌트 임대 시스템</sub>

</div>

---

## ⚡ 주요 특징 (Highlights)

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
# 저장소 복제
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# 소스코드에서 직접 실행
go run .
```

관리 콘솔은 다음 주소에서 즉시 접속 가능합니다:
👉 **`http://localhost:9081`**

### 2. 다중 아키텍처 독립 바이너리 크로스 컴파일

```bash
# linux/amd64, linux/arm64, darwin/arm64, windows/amd64 단독 바이너리 빌드
node scripts/build_artifacts.mjs --clean

# SHA-256 암호화 체크섬 검증
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ 아키텍처 및 리소스 풀링

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

## 🔒 시크릿 관리 및 인증

`cf-lease-broker`는 외부 서드파티 라이브러리 없이 자체적으로 엔터프라이즈급 시크릿 저장소와 인증을 제공합니다:

### 1. 봉투 암호화 금고 (`internal/security/vault.go`)
- **Windows DPAPI 연동**: Windows 환경에서는 Windows DPAPI(`CryptProtectData`)를 사용하여 OS 자격증명 기반으로 API 키를 암호화 보관합니다 (`dpapi:<base64>`).
- **AES-256-GCM 봉투**: 비 Windows 플랫폼에서는 96비트 고유 랜덤 난수가 결합된 인증 암호화 AES-256-GCM을 적용합니다 (`aes-gcm:<nonce>:<ciphertext>`).
- **메모리 스크러빙**: 민감한 바이트 버퍼는 사용 후 즉시 `Zeroize` 패턴을 통해 메모리에서 완전 소거됩니다 (`for i := range b { b[i] = 0 }`).

### 2. JWT 및 RBAC 미들웨어 (`internal/security/middleware.go`)
- **HS256 표준 토큰**: 부채널 타이밍 공격을 방어하기 위해 상수 시간 서명 검증(`crypto/subtle.ConstantTimeCompare`)을 사용하는 HMAC-SHA256 JWT 발급 및 검증.
- **해지 거부 목록**: JTI 기반의 즉시 토큰 해지(`POST /api/auth/revoke`) 및 TTL 자동 정리 지원.
- **보호된 관리 API**: 관리용 엔드포인트(`/api/leases`, `/api/leases/revoke`, `/api/security/*`)는 유효한 `Authorization: Bearer <token>` 또는 관리자 부트스트랩 키를 요구합니다.

```bash
# 1. 오퍼레이터 JWT 토큰 발급
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. 발급된 Bearer 토큰으로 보호된 임대 목록 엔드포인트 호출
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 가상 R2 S3 호환 게이트웨이 (`/s3/{bucket}/{key...}`)

표준 AWS S3 호환 엔드포인트를 제공하여 기존 S3 SDK, 마이크로서비스, CLI 도구가 개별 테넌트 한도를 초과하지 않고 풀링된 R2 계정에 오브젝트를 분산 저장할 수 있습니다:

### 핵심 기능:
1. **스트라이프 청크 분할**: 2MB를 초과하는 수신 페이로드는 순차 블록(`chk_<hash>_<index>`)으로 자동 분할되어 정상 계정들에 분산 저장됩니다.
2. **무결성 체크섬**: 각 청크는 개별 SHA-256 체크섬을 가지며, 오브젝트는 표준 멀티파트 호환 복합 ETag를 수신합니다.
3. **HTTP Range 요청**: 비디오 스트리밍, 다운로드 이어받기 및 대용량 에셋 부분 읽기를 위한 `Range: bytes=start-end` 헤더 완벽 지원.
4. **ListObjectsV2**: 표준 S3 XML `ListBucketResult` 응답 규격을 준수합니다.

### S3 CLI 및 Curl 예시:

```bash
# 1. 풀링된 R2 버킷에 오브젝트 분산 저장
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. 오브젝트 다운로드 인출
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. 부분 바이트 범위 요청
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf

# 4. 버킷 오브젝트 목록 조회 (S3 XML)
curl http://localhost:9081/s3/assets
```

---

## 🔄 투명 페일오버 리버스 프록시 (`/proxy/{service}/*`)

`/proxy/{service}/*`에서 투명한 엣지 리버스 프록시를 제공합니다. 호출자는 어느 테넌트 계정에 쿼터가 남아있는지 알 필요 없이 이 엔드포인트로 HTTP 요청을 보냅니다.

### 장애 조치(Failover) 동작 원리:
1. **대상 결정**: 수신된 요청의 `{service}`와 하위 경로는 잔여 쿼터가 가장 많은 풀 계정으로 라우팅됩니다 (`SelectOptimalAccount`).
2. **페이로드 버퍼링**: 요청 바디는 스트리밍 리더를 통해 메모리에 최대 10MB까지 버퍼링되어, 재시도 시 데이터 유실 없이 페이로드를 완벽히 재전송합니다.
3. **429 및 게이트웨이 에러 가로채기**: 대상 계정이 HTTP `429 Too Many Requests`, `502`, `503`, `504` 또는 연결 끊김으로 응답할 경우:
   - 해당 테넌트를 즉시 비정상으로 표시합니다 (`TriggerFailover(accountID)`).
   - 풀 내의 다음 정상 계정으로 요청을 즉각 자동 재전송합니다.
4. **홉 바이 홉 헤더 정리**: RFC 7230 홉 바이 홉 헤더(`Connection`, `Keep-Alive`, `Upgrade`, `TE` 등)는 양방향에서 안전하게 제거됩니다.
5. **텔레메트리 헤더**: 성공적인 응답에는 요청을 처리한 테넌트(`X-CF-Broker-Account`)와 시도 횟수(`X-CF-Broker-Attempts`) 헤더가 포함됩니다.

---

## 🔄 무중단 핫 리로딩 (`internal/config/`)

바이너리 재시작이나 활성 연결 끊김 없이 자격증명 로테이션과 계정 풀 변경을 지원합니다:

### 핫 리로드 메커니즘:
1. **파일 감시 루프**: 설정 파일(`CF_BROKER_CONFIG_PATH` 또는 `config.json`)을 주기적으로 폴링하고 SHA-256 다이제스트를 대조하여 불필요한 재로드를 방지합니다.
2. **POSIX 시그널 처리**: `SIGHUP`(Unix/Linux/macOS)을 감지하여 즉시 동기식 설정 검증 및 리로드를 수행합니다.
3. **원자적 상태 교체 및 안전 폴백**: 리로드된 파일에 잘못된 JSON, 음수 한도 또는 중복 ID가 포함된 경우 활성 설정을 그대로 유지하며 안전하게 실패합니다.
4. **자격증명 및 풀 동기화**: 정상 리로드된 계정은 `PoolManager`(`SyncAccounts`) 및 `SecretVault`(`StoreCredential`)로 즉시 동기화됩니다.

---

## 🚀 엣지 워커 가상 컴파일러 및 동적 라우트 주입

다중 테넌트 Edge Worker 스크립트 및 WebAssembly 모듈을 위한 컴파일 및 라우팅 엔진을 탑재하였습니다:

### 1. 인메모리 어휘 축소 및 패키징 (`internal/compiler/bundle.go`)
- **어휘 토크나이저**: 단일행/다중행 주석 제거, 불필요한 공백 압축 및 Cloudflare Worker 모듈 내보내기 서명(`export default { fetch(...) }`) 유효성 검증.
- **WebAssembly 검증**: 패키징 전 4바이트 매직 헤더(`\x00asm`) 및 버전 규격을 확인합니다.
- **콘텐츠 해싱**: 테넌트 번들 버전 관리 및 감사 추적을 위한 결정론적 SHA-256 다이제스트 생성.

### 2. 다중 영역 와일드카드 라우트 컴파일러 (`internal/compiler/router.go`)
- **임의 도메인 매핑**: 와일드카드 서브도메인(`*.domain.com/*`), 정확한 호스트(`api.domain.com/v1/*`) 또는 글로벌 폴백 규칙(`*/*`)을 매핑합니다.
- **우선순위 트라이(Trie)**: 정확한 호스트 > 와일드카드 > 글로벌 순으로 점수를 매겨 설정 리로드 없이 트래픽을 동적으로 라우팅합니다.

### 3. 제로 카피 다이렉트 스트리밍 (`internal/compiler/stream.go`)
- **버퍼 풀**: 대용량 프록시 전송 중 힙 할당을 방지하기 위해 64KB 바이트 슬라이스(`sync.Pool`)를 재사용합니다.
- **소켓 즉시 플러시**: `http.Flusher`를 활용하여 응답 청크를 실시간 스트리밍하며, `X-Accel-Buffering: no`로 중간 캐싱을 차단합니다.
- **연결 해제 감지**: 클라이언트 연결 종료 시 업스트림 스트림을 즉시 종료하여 소켓 누수를 방지합니다.

---

## 🤝 고가용성 Raft 분산 합의 엔진

제로 의존성 Raft 합의 엔진(`internal/raft/`)을 내장하여 멀티 리전 클러스터링, 무손실 임대 상태 복제 및 자동 리더 장애 조치를 지원합니다:

### 합의 핵심 요소:
1. **과반 정족수 선거**: Follower, Candidate, Leader 상태 전환. 무작위 선거 타임아웃(150~300ms)으로 스플릿 브레인을 방지하고 50ms 하트비트로 권위를 유지합니다.
2. **WAL 저널 (`internal/raft/journal.go`)**: SHA-256 무결성 검증이 포함된 단조 트랜잭션 인덱싱. 충돌하는 미커밋 항목을 자동 정리하며 선택적으로 디스크 파일(`raft_journal.wal`) 저장을 지원합니다.
3. **DNS SRV 동적 피어 탐색 (`internal/raft/discovery.go`)**: RFC 2782 DNS SRV 레코드(`_raft._tcp.<domain>`)를 통해 피어를 자동 탐색하며 환경 변수 매핑(`CF_RAFT_PEERS`)으로 유연하게 폴백합니다.
4. **브로커 FSM (`internal/raft/fsm_broker.go`)**: `GRANT_LEASE`, `REVOKE_LEASE`, `SYNC_ACCOUNT`, `UPDATE_QUOTA` 명령을 모든 노드에 결정론적으로 복제합니다.

---

## 🛡️ 제로 트러스트 상호 TLS (mTLS) 및 하드웨어 원격 증명

### 1. 에페머럴 인증 기관 및 동적 발급 (`internal/mtls/ca.go`)
- 외부 OpenSSL 없이 메모리 상에서 ECDSA P-256 루트 CA(`x509.IsCA = true`)를 동적 부트스트래핑합니다.
- 커스텀 Common Name, SAN 및 유효 기간을 갖는 단기 X.509 클라이언트 인증서를 발급합니다.
- 스레드 안전한 CRL(인증서 해지 목록)을 통해 $O(1)$ 속도로 해지 여부를 조회합니다.

### 2. 클라이언트 인증서 검증 (`internal/mtls/verifier.go`)
- 수신된 X.509 클라이언트 인증서 체인을 루트 CA 풀과 대조 검증합니다.
- 와일드카드 SAN 매칭(`*.edge.cluster.local`), 허용된 Common Name, 발급자 정책을 강제합니다.
- `CF_REQUIRE_MTLS=true` 활성화 시 `/proxy/{service}/*` 통과 전 반드시 인증서 검증을 통과해야 합니다.

### 3. 하드웨어 TPM 2.0 원격 증명 (`internal/mtls/tpm_windows.go`)
- Windows CNG(`ncrypt.dll`)를 통해 Microsoft Platform Crypto Provider의 TPM 2.0 탑재를 확인합니다.
- `crypt32.dll`로 시스템 인증서 저장소를 점검하고 플랫폼 PCR 인용 다이제스트를 생성합니다.

---

## 💰 자율 비용 최적화 및 다중 테넌트 청구 원장

### 1. 마이크로센트 정밀 계측 (`internal/billing/meter.go`)
- 부동소수점 오차를 원천 배제하기 위해 정수 마이크로센트($1.00 = 1,000,000 마이크로센트)로 사용량을 추적합니다.
- Workers 요청당 $0.30/1M, CPU 시간당 $0.02/1M ms, KV 읽기 $0.50/1M, 쓰기 $5.00/1M, R2 스토리지 GB월당 $0.015 등 공식 요율이 사전 캘리브레이션되어 있습니다.
- 계정별 일일 무료 제공량(무료 요청 10만 건, 무료 KV 읽기 10만 건)을 UTC 자정 롤오버로 추적하며 절감 비용을 산출합니다.

### 2. 자율 워크로드 마이그레이션 (`internal/billing/optimizer.go`)
- 사용량이 임계치(기본 80%)에 근접하면 유료 계정의 초과 요금을 방지하기 위해 유휴 무료 티어 계정으로 트래픽을 선제 이전합니다.

### 3. OpenCost 1.0 및 재무 원장 내보내기 (`internal/billing/journal.go`)
- OpenCost 표준 규격(`/api/billing/ledger?format=opencost`), RFC 4180 CSV, JSONL 포맷 내보내기를 기본 지원합니다.

---

## 🛰️ 지능형 엣지 헬스 레이더 및 애니캐스트 지리 라우팅

### 1. 합성 엣지 프로빙 (`internal/radar/probe.go`)
- 지수 이동 평균(EMA, `alpha=0.25`)으로 지연 시간 노이즈를 스무딩하여 경로 요동을 방지합니다.
- 통계적 지터와 패킷 손실률을 종합하여 PoP별 0~100 헬스 인덱스를 산출합니다.

### 2. 애니캐스트 지리 라우팅 (`internal/radar/router.go`)
- Haversine 공식을 통해 클라이언트 좌표와 PoP 간 구면 거리를 정밀 계산하고 밀리초 미만 속도로 최적 경로를 결정합니다.

### 3. BGP 경로 이상 탐지 및 사전 이전
- 기준선 대비 35ms 이상 또는 1.8배 지연 시 치명적 이상으로 분류하여, 연결 장애가 발생하기 전 활성 임대를 대기 PoP로 자율 대피시킵니다.

---

## 🔄 자율 엣지 토큰 로테이터 및 해지 메시

### 1. 자율 선제적 토큰 갱신 (`internal/rotator/rotator.go`)
- 토큰 유효 기간의 80% 도달 시 백그라운드 갱신을 실행하며, 30초 유예 기간 동안 이전 토큰과 새 토큰을 동시 유효 처리하여 엣지 클라이언트 단절을 0% 방지합니다.

### 2. 5ms 미만 가십 해지 메시 (`internal/rotator/mesh.go`)
- HMAC-SHA256 서명된 툼스톤을 P2P 가십 메시로 5ms 내에 클러스터 전역에 브로드캐스트합니다.

### 3. 하드웨어 TPM 2.0 위임 갱신 (`internal/rotator/attest.go`)
- 단일 사용 챌린지 난수와 하드웨어 PCR 다이제스트를 검증한 후 위임된 토큰 갱신을 승인합니다.

---

## 📡 REST API 레퍼런스

| 엔드포인트 | 메서드 | 인증 | 설명 |
| :--- | :---: | :---: | :--- |
| `/api/rotator/status` | `GET` | 공개 | 로테이터 상태, 활성 토큰, 갱신 및 메시 전파 지연 시간 조회 |
| `/api/rotator/tokens` | `GET` | 공개 | 로테이션 관리 대상 활성 토큰 목록 |
| `/api/rotator/renew` | `POST` | 공개 | 토큰 키 즉시 로테이션 및 갱신 트리거 |
| `/api/rotator/revoke` | `POST` | 공개 | 가십 메시를 통한 즉각적인 토큰/임대 해지 브로드캐스트 |
| `/api/radar/status` | `GET` | 공개 | 레이더 엔진 상태, 추적 중인 PoP 및 마이그레이션 이력 |
| `/api/radar/route` | `GET/POST` | 공개 | 서브 밀리초 Anycast 경로 분석기 (`?lat=...&lon=...&country=...`) |
| `/api/billing/summary` | `GET` | 공개 | 클러스터 비용 요약, 절감액 및 테넌트 집계 |
| `/api/billing/ledger` | `GET` | 공개 | 원장 내보내기 (`?format=json\|csv\|jsonl\|opencost`) |
| `/api/mtls/status` | `GET` | 공개 | 활성 루트 CA 상태, 정책 및 TPM 2.0 원격 증명 점검 |
| `/api/mtls/ca/cert` | `GET` | 공개 | PEM 포맷 루트 CA 인증서 다운로드 |
| `/api/mtls/ca/issue` | `POST` | 공개 | SAN 및 유효 기간이 지정된 단기 클라이언트 인증서 발급 |
| `/api/mtls/verify` | `POST` | 공개 | X.509 인증서 PEM 유효성 및 보안 정책 검증 |
| `/raft/status` | `GET` | 공개 | 로컬 노드 역할, 임기, 리더 ID 및 클러스터 텔레메트리 |
| `/metrics` | `GET` | 공개 | Prometheus 0.0.4 표준 메트릭 스크래퍼 |
| `/api/compiler/compile` | `POST` | 공개 | JS 워커 소스 컴파일 및 어휘 압축 수행 |
| `/s3/{bucket}/{key...}` | `PUT/GET/HEAD/DELETE` | 공개 | 2MB 청크 분할 가상 S3 호환 스토리지 게이트웨이 |
| `/proxy/{service}/*` | `ANY` | 공개 | 429 장애 조치 기능이 탑재된 투명 리버스 프록시 |
| `/api/accounts` | `GET` | 공개 | 풀링된 Cloudflare 계정 및 한도 스냅샷 |
| `/api/leases` | `GET/POST` | **Bearer** | 활성 임대 목록 조회 및 신규 동적 임대 발급 |
| `/api/leases/revoke` | `POST` | **Bearer** | 임대 바우처 즉시 무효화 및 해지 |
| `/api/health` | `GET` | 공개 | Cloudflare 공용 엣지 API 연결성 및 지연 시간 검증 |

---

## 🗺️ 로드맵 (Roadmap)

마일스톤 목표 및 개발 계획은 [ROADMAP.md](../../ROADMAP.md)를 참조하십시오.

---

## 🔒 보안 (Security)

취약점 보고 절차 및 보안 정책은 [SECURITY.md](../../SECURITY.md)를 참조하십시오.

---

## 📄 라이선스 (License)

본 프로젝트는 [Apache License 2.0](../../LICENSE) 하에 배포됩니다.
