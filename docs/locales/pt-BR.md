<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <a href="./zh-CN.md">🇨🇳 中文</a> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <strong>🇧🇷 Português</strong> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**Agrupamento de recursos multiconexão Cloudflare e corretor dinâmico de concessão de tokens para Workers, KV e R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-início-rápido)

<sub>Desenvolvido por **kLauncher1** · Agrupamento de recursos de borda de alta disponibilidade, cofre de segredos e concessão dinâmica para múltiplos locatários.</sub>

</div>

---

## ⚡ Destaques (Highlights)

- **Rotacionador Autônomo de Tokens de Borda**: Monitoramento proativo do ciclo de vida de vouchers de concessão e tokens de locatário que aciona renovações em segundo plano com 80% de validade, garantindo validade de token duplo sem tempo de inatividade durante um período de carência de 30 segundos (`internal/rotator/rotator.go`).
- **Malha de Revogação de Tokens Baseada em Gossip (<5ms)**: Livro-razão descentralizado de lápides (tombstones) com buscas $O(1)$, lápides assinadas com HMAC-SHA256, numeração sequencial monotônica, sincronização de estado anti-entropia e transmissão ponto a ponto em menos de 5ms entre os nós do cluster (`internal/rotator/mesh.go`).
- **Atestação de Hardware TPM 2.0 para Atualização Delegada**: Verificação criptográfica de integridade de plataforma para solicitações de atualização delegadas, nonces de desafio de uso único contra ataques de repetição e validação de digests de PCR em tempo constante (`internal/rotator/attest.go`).
- **Radar Inteligente de Saúde de Borda e Roteamento Geográfico Anycast**: Sondagem sintética concorrente de Pontos de Presença (PoPs) globais da Cloudflare, rastreamento de latência média móvel exponencial (EMA, `alpha=0.25`), jitter e índices de integridade, combinado com roteamento Anycast submilissegundo e detecção de oscilação de rota BGP (`internal/radar/`).
- **Migração Preventiva Autônoma de Concessões**: Realoca proativamente as concessões ativas e em trânsito de PoPs degradados para instâncias íntegras em espera antes que ocorram perdas de pacotes ou timeouts de socket.
- **Otimização Autônoma de Custos e Livro-Razão Multilocatário**: Medição granular de computação, requisições e armazenamento com precisão de microcentavos ($1,00 = 1.000.000 microcentavos) com base nas tarifas oficiais da Cloudflare para Workers, KV e R2, aliada à migração autônoma de sobretaxas para contas gratuitas de reserva (`internal/billing/`).
- **OpenCost 1.0 e Exportação Financeira Multiformato**: Exportação nativa de diários de faturamento nos padrões RFC 4180 CSV, JSON delimitado por quebra de linha (JSONL) e na especificação padrão OpenCost 1.0 (`/api/billing/ledger?format=opencost`).
- **Mutual TLS (mTLS) Zero-Trust e Verificação de Certificados de Cliente**: Verificação estrita de cadeias de certificados X.509, checagem de listas de revogação (CRL), imposição de janelas de validade, correspondência de SANs curinga (`*.domain.com`) e proteção de proxy reverso upstream (`internal/mtls/verifier.go`).
- **Autoridade de Certificação (CA) Efêmera e Malha de Emparelhamento**: Geração em memória sem dependências de certificado raiz ECDSA P-256, emissão dinâmica de certificados de cliente e tokens de emparelhamento intercluster assinados criptograficamente (`internal/mtls/ca.go`).
- **Atestação TPM 2.0 e Repositório de Certificados do Windows**: Utiliza a interface Windows CNG (`ncrypt.dll`) e o Microsoft Platform Crypto Provider para inspecionar o TPM 2.0 de hardware e o repositório de certificados do sistema (`crypt32.dll`) (`internal/mtls/tpm_windows.go`).
- **Mecanismo de Consenso Raft de Alta Disponibilidade**: Cluster distribuído com temporizadores aleatórios de eleição (150-300ms), quórum por maioria, registro de transações Write-Ahead Log (WAL) com verificação SHA-256 e descoberta dinâmica via DNS SRV (`internal/raft/`).
- **Compilador Virtual de Edge Workers e Empacotamento**: Minificador e validador léxico em memória de alta velocidade para scripts JavaScript/Wasm com remoção de comentários, verificação sintática e hash de conteúdo SHA-256 (`internal/compiler/bundle.go`).
- **Injeção de Rotas Dinâmicas Multizona**: Compilador de rotas com suporte a curingas (`*.domain.com/path/*`) com pontuação de especificidade e tabelas de injeção dinâmica de rotas (`internal/compiler/router.go`).
- **Pipeline de Streaming Direto Zero-Copy**: Transmissão de respostas de baixa latência usando pools de buffers pré-alocados de 64KB (`sync.Pool`), descarga imediata de pacotes (`http.Flusher`) e detecção de desconexão de clientes para evitar vazamento de sockets (`internal/compiler/stream.go`).
- **Artefatos Independentes Multi-Arquitetura**: Pipeline de compilação cruzada gerando executáveis autônomos para `linux/amd64`, `linux/arm64`, `darwin/arm64` e `windows/amd64` com hashes de integridade SHA-256 automáticos (`scripts/build_artifacts.mjs`).
- **Hot-Reloading Sem Tempo de Inatividade**: Recarregamento dinâmico de credenciais e contas via monitoramento de arquivos e sinais do sistema operacional (`SIGHUP`) sem reiniciar o processo.
- **Agregação de Cotas Multiconexão**: Consolida múltiplas contas gratuitas e pagas da Cloudflare em um cluster virtual unificado para superar limites individuais diários.
- **Cofre de Segredos para Produção**: Criptografia de envelope em repouso usando Windows DPAPI (`CryptProtectData`) e AES-256-GCM multiplataforma com higienização de memória (`Zeroize`).
- **Autenticação HS256 JWT e RBAC**: Emissão de tokens HMAC-SHA256 com validação em tempo constante e listas de revogação imediata por JTI.
- **Gateway R2 Virtual Compatível com S3**: Endpoints compatíveis com a API REST do AWS S3 (`/s3/{bucket}/{key...}`) com fragmentação automática de 2MB e validação composta de integridade ETag.
- **Cache KV Particionado Multiconexão**: Camada de armazenamento KV segura para concorrência com hashing consistente FNV, cache em memória LRU e expiração por TTL.
- **Exportador de Métricas Prometheus 0.0.4**: Endpoint padrão `/metrics` que exporta contagem de concessões, cotas de contas e telemetria de tráfego.
- **Limitação de Taxa com Janela Deslizante e Período de Provação**: Controlador de tráfego em memória com máquina de estados de recuo exponencial para contas temporariamente restringidas.
- **Webhooks de Alerta Multicanal**: Notificações ricas para Discord, Slack e JSON nos limites de 80%, 95% e 100%.
- **Interceptação Automática de Erros 429 e Failover**: Intercepta respostas HTTP 429 e falhas de gateway em tempo real, redirecionando o tráfego instantaneamente para a próxima conta saudável.
- **Proxy Reverso Transparente**: Encaminhamento direto de tráfego via `/proxy/{service}/*` com novas tentativas de corpo e limpeza de cabeçalhos de salto (RFC 7230).
- **Concessão Dinâmica de Recursos (Vouchers)**: Emissão de tokens de API delimitados e de curta duração (`lse_...`) para microsserviços internos ou suítes de teste.
- **Implantação em Binário Único**: Interface de gerenciamento Glassmorphism incorporada diretamente no binário Go via `//go:embed`.
- **Baixo Consumo de Memória**: Opera de forma contínua consumindo menos de **12 MB de RAM** sem dependências de tempo de execução externas.

---

## 🚀 Início Rápido (Quick Start)

### 1. Baixar e Executar

```bash
# Clonar o repositório
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# Executar diretamente
go run .
```

O console de gerenciamento estará disponível em:
👉 **`http://localhost:9081`**

### 2. Compilação Cruzada de Artefatos Multi-Arquitetura

```bash
# Compilar binários autônomos para linux/amd64, linux/arm64, darwin/arm64 e windows/amd64
node scripts/build_artifacts.mjs --clean

# Verificar os checksums criptográficos SHA-256
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Arquitetura e Agrupamento de Recursos (Architecture)

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

## 🔒 Gerenciamento de Segredos e Autenticação

### 1. Cofre de Criptografia de Envelope (`internal/security/vault.go`)
- **Integração Windows DPAPI**: Em sistemas Windows, os segredos são resguardados via DPAPI (`CryptProtectData`) no nível do sistema operacional (`dpapi:<base64>`).
- **Envelope AES-256-GCM**: Em outros sistemas, a proteção é realizada por meio de cifra autenticada AES-256-GCM com nonces aleatórios de 96 bits.
- **Higienização de Memória**: Buffers contendo credenciais são imediatamente zerados após a operação através do padrão `Zeroize`.

### 2. Middleware JWT e RBAC (`internal/security/middleware.go`)
- **Tokens Padronizados HS256**: Validação em tempo constante contra ataques de temporização (`crypto/subtle.ConstantTimeCompare`).
- **Lista de Revogação Imediata**: Cancelamento de tokens por identificador JTI (`POST /api/auth/revoke`) com limpeza por TTL.
- **Rotas Administrativas Protegidas**: Endpoints de configuração exigem cabeçalho `Authorization: Bearer <token>`.

```bash
# 1. Emitir token JWT de operador
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. Consultar concessões ativas com o token de autorização
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 Gateway R2 Virtual Compatível com S3 (`/s3/{bucket}/{key...}`)

O broker disponibiliza endpoints compatíveis com o padrão S3:
1. **Fragmentação em Faixas (Striping)**: Payloads superiores a 2MB são divididos em blocos indexados (`chk_<hash>_<index>`) e distribuídos pelo pool de contas.
2. **Hashes de Verificação**: Cada bloco contém seu hash individual SHA-256, gerando ETags compostos compatíveis com uploads multipartes.
3. **Requisições de Intervalo (HTTP Range)**: Suporte completo ao cabeçalho `Range: bytes=start-end` para streaming e downloads parciais.
4. **Listagem de Objetos**: Respostas compatíveis com o esquema XML padrão do S3 `ListObjectsV2`.

```bash
# 1. Armazenar objeto no pool R2
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. Recuperar objeto armazenado
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. Solicitação de intervalo de bytes
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf
```

---

## 🔄 Proxy Reverso Transparente com Failover Automático (`/proxy/{service}/*`)

1. **Seleção de Rota Ideal**: O tráfego recebido é encaminhado à conta com o maior volume de cota restante (`SelectOptimalAccount`).
2. **Buffer de Carga Útil**: Armazena em memória requisições de até 10MB para garantir reenvios automáticos sem perda de dados.
3. **Interceptação de Erros 429**: Caso a conta retorne HTTP 429 ou falhas 502/503/504, o broker aciona `TriggerFailover` e encaminha a requisição para a próxima conta funcional.
4. **Higienização de Cabeçalhos**: Exclusão bilateral de cabeçalhos de salto conforme norma RFC 7230.
5. **Telemetria de Tráfego**: Inserção de cabeçalhos explicativos `X-CF-Broker-Account` e `X-CF-Broker-Attempts`.

---

## 🔄 Recarregamento em Quente Sem Interrupção (`internal/config/`)

1. **Varredura de Arquivos**: Avaliação de modificações por meio de hashes SHA-256 para evitar recargas desnecessárias.
2. **Tratamento de Sinais POSIX**: Captura do sinal `SIGHUP` para aplicação síncrona imediata da nova configuração.
3. **Substituição Atômica com Segurança**: Arquivos com erros de sintaxe JSON são rejeitados mantendo a configuração ativa intacta.

---

## 🚀 Compilador Virtual de Edge Workers e Injeção de Rotas

1. **Minificação Léxica em Memória**: Remoção de comentários e validação de assinaturas de módulos Cloudflare Worker.
2. **Compilador de Rotas Multizona**: Associação de padrões com curinga (`*.domain.com/*`) baseada em pontuação de especificidade.
3. **Streaming Zero-Copy**: Utilização de pools de memória de 64KB com `http.Flusher` para vazão máxima sem sobrecarga de heap.

---

## 🤝 Máquina de Estados Raft Distribuída de Alta Disponibilidade

1. **Eleição com Quórum por Maioria**: Temporizadores entre 150 e 300ms e batimentos cardíacos a cada 50ms.
2. **Registro Write-Ahead Log (WAL)**: Indexação rigorosa e verificação de integridade via SHA-256.
3. **Descoberta via DNS SRV**: Resolução dinâmica de membros do cluster através de registros RFC 2782 (`net.LookupSRV`).
4. **FSM do Broker**: Replicação determinística de ações `GRANT_LEASE`, `REVOKE_LEASE`, `SYNC_ACCOUNT` e `UPDATE_QUOTA`.

---

## 🛡️ Segurança mTLS e Atestação de Plataforma TPM 2.0

1. **Autoridade Certificadora Efêmera**: Geração de certificado raiz ECDSA P-256 e emissão dinâmica de certificados de cliente.
2. **Validação de Cadeia e Políticas**: Verificação de certificados X.509 em conformidade com as regras de SAN e emissor.
3. **Atestação TPM 2.0 via Windows CNG**: Consulta à API `ncrypt.dll` para obtenção de medições PCR e auditoria de plataforma.

---

## 💰 Otimização Autônoma de Custos e Contabilidade de Locatários

1. **Cálculo Preciso em Microcentavos**: Aferição exata de custos ($1,00 = 1.000.000 microcentavos) eliminando desvios de arredondamento.
2. **Desvio Preventivo de Cargas**: Redirecionamento instantâneo para contas gratuitas de reserva antes do esgotamento das cotas contratadas.
3. **Exportação OpenCost 1.0 e CSV/JSONL**: Emissão padronizada de demonstrativos de consumo para relatórios contábeis.

---

## 🛰️ Radar de Saúde de Borda e Roteamento Anycast

1. **Monitoramento Sintético Contínuo**: Cálculo de latência via EMA (`alpha=0.25`) e registro de jitter por localidade.
2. **Roteamento Anycast com Resolução Submilissegundo**: Estimativa de distância por fórmula de Haversine para melhor seleção de PoP.
3. **Detecção de Oscilações BGP**: Transferência antecipada de concessões ativas em nós degradados antes da ocorrência de quedas de rede.

---

## 🔄 Rotacionador Autônomo de Tokens e Malha Gossip

1. **Renovação Antecipada**: Emissão de novos tokens ao atingir 80% da vigência, com tolerância transitória de 30 segundos.
2. **Disseminação por Gossip (<5ms)**: Propagação instantânea de lápides de revogação para todos os membros do cluster.
3. **Delegação Chancelada por TPM 2.0**: Validação de nonces de uso único e digests de PCR em solicitações delegadas de atualização.

---

## 📡 Referência da API REST

| Endpoint | Método | Autenticação | Descrição |
| :--- | :---: | :---: | :--- |
| `/api/rotator/status` | `GET` | Pública | Estado geral do rotacionador, tokens vigentes e latência da malha |
| `/api/rotator/tokens` | `GET` | Pública | Listagem dos tokens sob gestão de renovação |
| `/api/rotator/renew` | `POST` | Pública | Solicitação de renovação manual imediata de um token |
| `/api/rotator/revoke` | `POST` | Pública | Propagação de revogação de token através da malha gossip |
| `/api/radar/status` | `GET` | Pública | Indicadores do radar, PoPs avaliados e latências globais |
| `/api/radar/route` | `GET/POST`| Pública | Resolução Anycast submilissegundo para localização geográfica |
| `/api/billing/summary` | `GET` | Pública | Síntese de despesas do cluster e economia total obtida |
| `/api/billing/ledger` | `GET` | Pública | Download do diário contábil (`?format=json\|csv\|jsonl\|opencost`) |
| `/api/mtls/status` | `GET` | Pública | Diagnóstico da CA efêmera, regras de segurança e prova TPM 2.0 |
| `/api/mtls/ca/cert` | `GET` | Pública | Obtenção do certificado raiz público no formato PEM |
| `/api/mtls/ca/issue` | `POST` | Pública | Fornecimento de certificado de cliente de curta validade |
| `/api/mtls/verify` | `POST` | Pública | Verificação de certificado X.509 contra as diretrizes vigentes |
| `/raft/status` | `GET` | Pública | Indicadores do nó Raft local, papel desempenhado e índice de log |
| `/metrics` | `GET` | Pública | Coleta de telemetria no padrão Prometheus 0.0.4 |
| `/api/compiler/compile`| `POST` | Pública | Compilação, análise léxica e minificação de scripts Worker |
| `/s3/{bucket}/{key...}`| `PUT/GET` | Pública | Ponto de acesso para armazenamento virtual compatível com S3 |
| `/proxy/{service}/*` | `ANY` | Pública | Proxy reverso transparente com reenvio em respostas 429 |
| `/api/accounts` | `GET` | Pública | Relação de contas Cloudflare cadastradas e seus saldos de cota |
| `/api/leases` | `GET/POST` | **Bearer** | Consulta às concessões ativas ou registro de uma nova concessão |
| `/api/leases/revoke` | `POST` | **Bearer** | Anulação instantânea de um voucher de concessão |
| `/api/health` | `GET` | Pública | Verificação de conectividade com a API pública da Cloudflare |

---

## 🗺️ Roteiro de Desenvolvimento (Roadmap)

Consulte o documento [ROADMAP.md](../../ROADMAP.md) para detalhes sobre as próximas fases e metas estabelecidas.

---

## 🔒 Segurança (Security)

Para esclarecimentos e reporte responsável de vulnerabilidades, consulte [SECURITY.md](../../SECURITY.md).

---

## 📄 Licença (License)

Disponibilizado sob a licença [Apache License 2.0](../../LICENSE).
