<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <a href="./zh-CN.md">🇨🇳 中文</a> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <strong>🇧🇷 Português</strong> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**Agrupamento de recursos multiconas da Cloudflare e agente dinâmico de locação de tokens para Workers, KV e R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-início-rápido)

<sub>Desenvolvido por **kLauncher1** · Agrupamento de recursos de borda de alta disponibilidade, cofre de segredos e locação dinâmica de inquilinos.</sub>

</div>

---

## ⚡ Destaques (Highlights)

- **Sandbox Wasm de Borda e Teto Físico de Memória de 32MB**: Memória linear gerenciada e segura de WebAssembly particionada em páginas de 64 KiB (`WasmPageSize = 65536`), impondo estritamente um teto físico de 32 MiB (`HardCeilingMemoryPages = 512`), proteção de acesso fora dos limites, limpeza por zeramento (Zeroization) na desmontagem e rastreamento de picos de alocação (`internal/sandbox/memory.go`).
- **Contabilidade Determinística de Gás e Bloqueio de Loops**: Orçamento de execução por instrução com medição determinística de gás e contabilização de passos, interrompendo imediatamente iterações infinitas após o esgotamento do gás sem congelar a thread do broker hospedeiro (`internal/sandbox/gas.go`).
- **Isolamento de Falhas do Host e Recuperação Tolerante a Pânico**: Recuperação adiada envolve as execuções de cargas convidadas, capturando falhas (violações de memória SIGSEGV, pânicos de execução não tratados, armadilhas de falta de gás e OOM) garantindo que o broker host permaneça 100% estável, apoiado por um buffer anelar de falhas (retém até 500 relatórios recentes) e limites progressivos de quarentena (`internal/sandbox/engine.go`).
- **Roteamento de Pacotes eBPF HTTP/3 com Sobrecarga Zero**: Envio de sockets no nível do kernel ignorando a pilha TCP do espaço do usuário através de sockmap eBPF e ganchos XDP no Linux, atingindo latências de despacho inferiores a 10 microssegundos (< 10µs, medido ~0.26µs) para cargas de proxy de borda (`internal/steering/engine.go`).
- **Inspeção de Cabeçalhos de Pacotes UDP/QUIC RFC 9000**: Inspeção sem alocação no heap analisando cabeçalhos QUIC Long (Initial, 0-RTT, Handshake, Retry) e Short (1-RTT), extraindo identificadores de conexão de destino e origem (DCID/SCID) e roteando o tráfego diretamente para os endpoints ideais de Workers com base em regras de prefixo CID e menor número de conexões (`internal/steering/quic_parser.go`).
- **Bypass de Kernel Multiplataforma e Alternativa em Espaço do Usuário**: Arquitetura multiplataforma fornecendo ganchos sockmap eBPF nativos no Linux e despacho rápido de cópia zero no espaço do usuário no Windows e macOS com latência submicrossegundo e 100% de cobertura de testes (`internal/steering/`).
- **Rotacionador Autônomo de Tokens de Borda**: Monitoramento proativo do ciclo de vida de vouchers de locação e tokens de inquilinos acionando renovações em segundo plano ao atingir 80% de validade, com vigência dupla de tokens sem tempo de inatividade durante um período de carência de 30 segundos (`internal/rotator/rotator.go`).
- **Malha de Revogação de Tokens Baseada em Gossip Inferior a 5ms**: Registro descentralizado de lápides de revogação com consultas $O(1)$, lápides assinadas com HMAC-SHA256, numeração sequencial monótona, sincronização de estado anti-entropia e difusão ponto a ponto propagada em menos de 5ms no cluster (`internal/rotator/mesh.go`).
- **Atestação de Hardware TPM 2.0 para Atualização Delegada**: Verificação criptográfica de integridade de plataforma para solicitações delegadas de atualização de tokens, nonces de desafio de uso único evitando ataques de repetição, validação de tags HMAC em tempo constante e verificação de resumos de Registros de Configuração de Plataforma (PCR) (`internal/rotator/attest.go`).
- **Radar Inteligente de Saúde de Borda e Roteamento Geográfico Anycast**: Sondagem sintética concorrente de pontos de presença (PoPs) globais da Cloudflare, rastreamento de latência por média móvel exponencial (EMA, `alpha=0.25`), jitter e pontuações compostas de integridade, combinado com roteamento geográfico Anycast submilissegundo (< 1ms) e detecção de anomalias de oscilação de rotas BGP (`internal/radar/`).
- **Migração Autônoma Preventiva de Locações**: Transfere dinamicamente locações ativas de microsserviços a partir de PoPs degradados para alternativas saudáveis em espera antes que ocorram perdas de pacotes ou timeouts de sockets.
- **Otimização Autônoma de Custos e Livro-Razão Multilocatário**: Medição granular de computação, solicitações e armazenamento com atribuição de custos em microcentavos ($1.00 = 1.000.000 microcentavos) com base nas taxas oficiais da Cloudflare para Workers, KV e R2, juntamente com migração autônoma de cargas para contas gratuitas em espera para evitar sobrecustos (`internal/billing/`).
- **Exportação Financeira Multiformato e OpenCost 1.0**: Suporte integrado de exportação de lançamentos contábeis em RFC 4180 CSV, JSON delimitado por quebras de linha (JSONL) e formato padrão OpenCost 1.0 (`/api/billing/ledger?format=opencost`) para engenharia financeira de borda.
- **TLS Mútuo de Confiança Zero (mTLS) e Verificação de Certificados de Cliente**: Verificação abrangente da cadeia de confiança de certificados X.509 de clientes, consulta a listas de revogação (CRL), imposição de janelas de validade, correspondência de Nomes Alternativos de Sujeito (SAN) curinga (`*.domain.com`) e políticas de acesso de emissor/nome comum para rotas de proxy reverso (`internal/mtls/verifier.go`).
- **Autoridade de Certificação Efêmera (CA) e Malha de Emparelhamento**: Geração em memória sem dependências de certificados raiz ECDSA P-256, emissão dinâmica de certificados de cliente, rastreamento de números de série CRL e tokens de emparelhamento intercluster assinados criptograficamente (`internal/mtls/ca.go`).
- **Atestação de Hardware TPM 2.0 e Repositório de Certificados do Windows**: Utiliza a Criptografia de Próxima Geração do Windows (CNG) `ncrypt.dll` para consultar a presença de TPM 2.0 no Microsoft Platform Crypto Provider, inspecionar o repositório de certificados do sistema (`crypt32.dll`) e gerar medições PCR determinísticas (`internal/mtls/tpm_windows.go`).
- **Motor de Consenso Raft de Alta Disponibilidade**: Cluster de consenso Raft multinó com temporizadores de eleição aleatórios (150-300ms), eleição de líder por quórum majoritário, registro Write-Ahead Log (WAL) com indexação monótona e verificação SHA-256, e descoberta dinâmica de pares por DNS SRV (`net.LookupSRV`) sem dependências externas (`internal/raft/`).
- **Compilador Virtual e Empacotador de Workers de Borda**: Minificador lexical e validador em memória ultrarrápido para pacotes JavaScript/Wasm com eliminação de comentários, verificação sintática e hash de conteúdo SHA-256 (`internal/compiler/bundle.go`).
- **Injeção Dinâmica de Rotas Multizona**: Compilador de rotas curinga de alto desempenho correspondendo a domínios arbitrários (`*.domain.com/path/*`) com ponderação de prioridade e tabelas de injeção dinâmica de rotas (`internal/compiler/router.go`).
- **Canal de Transmissão Direta de Cópia Zero**: Transmissão de respostas de borda de baixa latência utilizando pools de buffers de 64KB pré-alocados (`sync.Pool`), descarga imediata de fragmentos (`http.Flusher`) e detecção de desconexão de clientes (`internal/compiler/stream.go`).
- **Artefatos Autônomos Multiarquitetura**: Pipeline de compilação cruzada que empacota executáveis autônomos otimizados para `linux/amd64`, `linux/arm64`, `darwin/arm64` e `windows/amd64` com geração automática de somas de verificação SHA-256 (`scripts/build_artifacts.mjs`).
- **Recarregamento a Quente sem Tempo de Inatividade**: Recarregamento dinâmico de configurações e credenciais via monitoramento de arquivos e sinais do SO (`SIGHUP` em POSIX) sincronizando inquilinos agrupados e o cofre de segredos sem reiniciar processos.
- **Agregação de Cotas Multicontas**: Agrupa múltiplas contas gratuitas e pagas da Cloudflare em um cluster computacional virtual unificado, superando limites diários de requisições individuais.
- **Cofre de Segredos para Produção**: Criptografia de envelope em repouso via Windows DPAPI (`CryptProtectData`) e AES-256-GCM multiplataforma com limpeza segura de memória (`Zeroize`).
- **Autenticação HS256 JWT e RBAC**: Motor de autenticação administrativa e de microsserviços com tokens HMAC-SHA256, verificação em tempo constante e listas de bloqueio JTI protegidas contra concorrência.
- **Gateway Virtual R2 Compatível com S3**: Endpoints com protocolo REST S3 (`/s3/{bucket}/{key...}`) distribuindo o armazenamento de objetos em buckets R2 agrupados com fragmentação automática em blocos de 2MB e verificação de integridade SHA-256.
- **Cache KV Particionado Multicontas**: Camada de armazenamento KV particionada segura para threads com hashing consistente FNV, armazenamento em cache LRU e despejo por TTL.
- **Exportador de Métricas Prometheus 0.0.4**: Endpoint padrão `/metrics` exportando contagens de locações, cotas de inquilinos, rejeições de limite de taxa e telemetria de proxy sem dependências externas.
- **Limite de Taxa por Janela Deslizante e Período Probatório Automático**: Rastreador de taxa em memória por janela deslizante com tolerância a rajadas e máquina de estados probatória com recuo exponencial.
- **Webhooks Multicanais de Cotas**: Notificações ricas no Discord, Slack Block Kit e JSON genérico acionadas em limites de 80% (Aviso), 95% (Crítico) e 100% (Esgotado) com amortecimento de 10 minutos.
- **Interceptação Automática de Limites de Taxa e Failover**: Intercepta erros HTTP 429 e de gateway em tempo real, redirecionando requisições em andamento para a próxima conta saudável do grupo sem interrupções.
- **Proxy Reverso Transparente**: Encaminha tráfego fluidamente via `/proxy/{service}/*` com novas tentativas de corpo armazenado em buffer e remoção de cabeçalhos salto a salto RFC 7230.
- **Locação Dinâmica de Recursos (Vouchers)**: Emite tokens de API de curta duração com escopo delimitado (`lse_...`) para microsserviços internos, chamadores externos ou suítes de testes.
- **Implantação em um Único Executável**: O painel de administração web com estética glassmorphism é incorporado diretamente no binário Go via `//go:embed`.
- **Uso de Memória Leve**: Executa persistentemente consumindo menos de **12 MB de RAM** sem dependências externas em tempo de execução.

---

## 🚀 Início Rápido

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

### 2. Compilação Cruzada de Artefatos Autônomos Multiarquitetura

```bash
# Compilar binários autônomos para linux/amd64, linux/arm64, darwin/arm64, windows/amd64
node scripts/build_artifacts.mjs --clean

# Verificar somas de verificação criptográficas SHA-256
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Arquitetura e Agrupamento de Recursos

```text
┌────────────────────────────────────────────────────────────────────────┐
│                            cf-lease-broker                             │
│                         Executável Go Único                            │
├───────────────────┬───────────────────┬────────────────────────────────┤
│   internal/raft   │  internal/broker  │       internal/storage         │
│ • Núcleo Raft     │ • Pool de Contas  │ • Gateway Virtual S3 (/s3/...) │
│ • Registro WAL    │ • Locações Dinâm. │ • Fragmentador em Faixas 2MB   │
│ • Pares DNS SRV   │ • Balanço de Cota │ • Cache KV Particionado        │
│ • Sincronia FSM   │ • Failover Auto   │ • Webhooks Multicanais         │
├───────────────────┼───────────────────┼────────────────────────────────┤
│   internal/mtls   │ internal/security │       internal/compiler        │
│ • CA Efêmera      │ • Windows DPAPI   │ • Minificador Virt. JS / Wasm  │
│ • Cert X509 Clnt  │ • AES-256-GCM     │ • Compilador Rotas Multizona   │
│ • Atestação TPM   │ • HS256 JWT/RBAC  │ • Tubo de Fluxo Zero-Cópia 64KB│
│ • Tokens de Pares │ • Limpeza Memória │ • Repositório Pacotes Inquilino│
├───────────────────┴───────────────────┴────────────────────────────────┤
│                         internal/quota & config                        │
│  • Prometheus (/metrics) • Janela Deslizante • Recarga a Quente        │
├────────────────────────────────────────────────────────────────────────┤
│                         Motor HTTP net/http                            │
│   • /api/mtls/* (Cert/Verif/TPM)       • /api/compiler/*               │
│   • /raft/* (Voto/Anexo/Status)        • /proxy/{svc}/* (Guarda mTLS)  │
│   • /metrics         • /api/auth/*     • /s3/{bkt}/{key}               │
│   • /api/accounts    • /api/security   • /api/config/* (Guarda Auth)   │
├────────────────────────────────────────────────────────────────────────┤
│                       Painel de Controle Web Embutido                  │
│                     (UI Glassmorphism via //go:embed)                  │
│                     web/dist/index.html -> Porta 9081                  │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 🔒 Gerenciamento de Segredos e Autenticação

O `cf-lease-broker` fornece armazenamento seguro de segredos e autenticação de nível empresarial prontos para uso, com zero dependências de terceiros:

### 1. Cofre com Criptografia de Envelope (`internal/security/vault.go`)
- **Integração com Windows DPAPI**: Em sistemas Windows, os segredos são protegidos via Windows DPAPI (`CryptProtectData`), protegendo chaves de API com credenciais do SO (`dpapi:<base64>`).
- **Envelope AES-256-GCM**: Em plataformas não Windows (ou quando especificado), os segredos são criptografados usando AES-256-GCM autenticado com nonces aleatórios exclusivos de 96 bits (`aes-gcm:<nonce>:<ciphertext>`).
- **Limpeza de Memória**: Buffers de bytes sensíveis são imediatamente apagados após o uso utilizando o padrão `Zeroize` (`for i := range b { b[i] = 0 }`).

### 2. Middleware JWT e RBAC (`internal/security/middleware.go`)
- **Tokens Padrão HS256**: Emite e valida JWTs HMAC-SHA256 com verificação de assinatura em tempo constante (`crypto/subtle.ConstantTimeCompare`) para evitar ataques de temporização de canal lateral.
- **Lista de Bloqueio por Revogação**: Permite a revogação imediata de tokens por JTI (`POST /api/auth/revoke`) com varredura automática por expiração TTL.
- **APIs Administrativas Protegidas**: Endpoints de administração (`/api/leases`, `/api/leases/revoke` e `/api/security/*`) exigem cabeçalho válido `Authorization: Bearer <token>` ou chaves mestras de inicialização.

```bash
# 1. Emitir um token JWT de operador
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. Chamar o endpoint protegido de locações com token Bearer
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 Gateway Virtual R2 Compatível com S3 (`/s3/{bucket}/{key...}`)

O intermediário expõe endpoints compatíveis com o padrão AWS S3, permitindo que SDKs S3, microsserviços e ferramentas CLI armazenem objetos em contas agrupadas da Cloudflare R2 sem exceder as cotas de armazenamento individuais dos inquilinos:

### Recursos Principais:
1. **Fragmentação em Faixas**: Cargas úteis recebidas com mais de 2MB são divididas automaticamente em blocos sequenciais (`chk_<hash>_<index>`) e distribuídas entre contas saudáveis do pool.
2. **Somas de Verificação de Integridade**: Cada pedaço inclui uma soma de verificação individual SHA-256, e os objetos recebem ETags compostos padrão compatíveis com envios multipartes.
3. **Solicitações HTTP Range**: Suporta cabeçalhos `Range: bytes=start-end` para streaming de vídeo, retomada de downloads e leituras parciais de arquivos grandes.
4. **ListObjectsV2**: Em conformidade com o formato padrão de resposta XML S3 `ListBucketResult`.

### Exemplos com CLI S3 e Curl:

```bash
# 1. Armazenar um objeto em buckets R2 agrupados
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. Recuperar o objeto
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. Solicitação de intervalo parcial de bytes
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf

# 4. Listar objetos do bucket (XML S3)
curl http://localhost:9081/s3/assets
```

---

## 🔄 Proxy Reverso Transparente com Failover (`/proxy/{service}/*`)

O intermediário fornece um proxy reverso de borda transparente em `/proxy/{service}/*`. Os chamadores direcionam requisições HTTP para este endpoint sem precisar saber qual conta de inquilino possui cota disponível no momento.

### Como Funciona o Failover:
1. **Resolução de Destino**: O `{service}` e subcaminho de entrada são resolvidos contra a conta do pool com maior cota restante (`SelectOptimalAccount`).
2. **Armazenamento de Carga Útil**: Os corpos das requisições são armazenados em buffer de memória (até 10MB) via leitores de fluxo para que tentativas possam reproduzir a carga completa sem perda de dados.
3. **Interceptação de Erros 429 e Gateway**: Se a conta de destino responder com HTTP `429 Too Many Requests`, `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout` ou queda de conexão:
   - O broker imediatamente marca o inquilino como não saudável via `TriggerFailover(accountID)`.
   - A solicitação é repetida automaticamente na próxima conta saudável do pool.
4. **Sanitização Salto a Salto**: Cabeçalhos salto a salto RFC 7230 (`Connection`, `Keep-Alive`, `Upgrade`, `TE`, `Trailers`, etc.) são eliminados em ambas as direções.
5. **Cabeçalhos de Telemetria**: Respostas bem-sucedidas incluem cabeçalhos de inspeção úteis:
   - `X-CF-Broker-Account: {account_id}` (inquilino que atendeu à requisição)
   - `X-CF-Broker-Attempts: {count}` (número de tentativas necessárias)

---

## 🔄 Recarregamento a Quente sem Tempo de Inatividade (`internal/config/`)

O `cf-lease-broker` suporta rotação dinâmica de credenciais e atualização de pools de contas sem reiniciar o binário ou interromper conexões HTTP ativas:

### Mecânica de Recarregamento a Quente:
1. **Loop de Monitoramento de Arquivos**: Consulta periodicamente o arquivo de configuração (`CF_BROKER_CONFIG_PATH` ou `config.json`) e avalia alterações via resumos determinísticos SHA-256 para evitar recargas espúrias.
2. **Tratamento de Sinais POSIX**: Captura o sinal `SIGHUP` (Unix/Linux/macOS) para disparar a validação síncrona e o recarregamento imediato da configuração.
3. **Troca de Estado Atômica e Segurança de Fallback**: Se o arquivo recarregado contiver JSON inválido, limites negativos ou IDs duplicados, a recarga falha com segurança com log de erro, mantendo a configuração ativa intacta.
4. **Sincronização de Credenciais e Pool**: Inquilinos recarregados com sucesso são sincronizados diretamente com `PoolManager` (`SyncAccounts`) e criptografados no `SecretVault` (`StoreCredential`).

### Formato do Arquivo de Configuração (`config.json`):
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

## 🚀 Compilador Virtual de Workers de Borda e Injeção Dinâmica de Rotas

O intermediário possui um mecanismo integrado de compilação e roteamento para scripts de Workers de borda multilocatários e módulos WebAssembly:

### 1. Minificação Lexical e Empacotamento em Memória (`internal/compiler/bundle.go`)
- **Tokenizador Lexical**: Remove comentários de linha única (`//`) e múltiplas linhas (`/* */`), elimina espaços em branco redundantes, normaliza espaçamento de pontuação e valida assinaturas de exportação de módulos Cloudflare Worker (`export default { fetch(...) }`, `addEventListener('fetch')`).
- **Validação WebAssembly**: Verifica cabeçalhos mágicos de 4 bytes (`\x00asm`) e conformidade de versão antes do empacotamento do inquilino.
- **Hash de Conteúdo**: Gera resumos SHA-256 determinísticos para controle de versão e trilhas de auditoria dos pacotes de inquilinos.

### 2. Compilador de Rotas Curinga Multizona (`internal/compiler/router.go`)
- **Mapeamento de Domínio Arbitrário**: Mapeia endpoints em subdomínios curinga (`*.domain.com/*`), hosts exatos (`api.domain.com/v1/*`) ou regras globais de fallback (`*/*`).
- **Trie Ponderada por Prioridade**: Avalia o host e caminho de entrada com pontuação de especificidade (host exato > subdomínio curinga > global; prefixo longo > curto), roteando tráfego dinamicamente sem recargas de configuração.

### 3. Canal de Transmissão Direta de Cópia Zero (`internal/compiler/stream.go`)
- **Pool de Buffers**: Utiliza fatias de bytes pré-alocadas de 64KB (`sync.Pool`) para eliminar alocações no heap durante transferências proxy de alto rendimento.
- **Descarga Imediata de Sockets**: Aproveita `http.Flusher` para transmitir pedaços de resposta para clientes em tempo real enquanto desativa o armazenamento intermediário via `X-Accel-Buffering: no`.
- **Detecção de Cancelamento**: Vincula-se a `context.Context` para encerrar imediatamente os fluxos de Workers upstream após desconexões de clientes, evitando vazamento de sockets.

---

## 🤝 Máquina de Estados Raft de Alta Disponibilidade para Clusters Distribuídos

O `cf-lease-broker` incorpora um mecanismo de consenso Raft sem dependências (`internal/raft/`) permitindo agrupamento de brokers multirregião, replicação de locações sem perda de dados e failover automático de líder:

### Arquitetura Principal e Primitivas de Consenso:
1. **Eleição de Líder por Quórum e Temporizadores Aleatórios**: Os nós transitam entre os estados Follower, Candidate e Leader. Os tempos limite de eleição aleatórios (150–300ms) evitam bloqueios por votos divididos, enquanto um batimento de 50ms mantém a autoridade do líder no quórum majoritário `((N/2) + 1)`.
2. **Registro Write-Ahead Log (WAL) (`internal/raft/journal.go`)**: Impõe indexação monotônica de transações com somas de verificação SHA-256. A consistência do registro dos seguidores é validada antes de anexar, truncando automaticamente entradas não confirmadas em conflito. Suporte a persistência opcional em disco em modo somente anexação (`raft_journal.wal`).
3. **Descoberta Dinâmica de Pares via DNS SRV (`internal/raft/discovery.go`)**: Resolve automaticamente os pares do cluster através de registros DNS SRV RFC 2782 (`_raft._tcp.<domain>`) com fallback elegante para mapeamentos estáticos de ambiente (`CF_RAFT_PEERS`) e registro manual em tempo de execução (`POST /raft/peers`).
4. **Máquina de Estados Finitos (FSM) do Broker (`internal/raft/fsm_broker.go`)**: Replica comandos transacionais deterministicamente em todos os nós:
   - `GRANT_LEASE`: Replica vouchers de locação ativos em todo o cluster.
   - `REVOKE_LEASE`: Propaga a revogação de locação para todos os nós pares.
   - `SYNC_ACCOUNT`: Sincroniza perfis de inquilinos e limites de cota.
   - `UPDATE_QUOTA`: Propaga incrementos diários de solicitações de Workers, deltas de armazenamento R2 e leituras KV.
   - `NOOP`: Sondas de barreira de consenso para verificações de prontidão do cluster.

### Fluxo de Topologia do Cluster:

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

### Exemplos de Curl para o Cluster Raft:

```bash
# 1. Inspecionar o status do nó local e a topologia do cluster
curl http://localhost:9081/raft/status

# 2. Consultar integridade dos pares e latência de ida e volta (RTT)
curl http://localhost:9081/raft/peers

# 3. Registrar manualmente um novo nó par
curl -X POST http://localhost:9081/raft/peers \
  -H "Content-Type: application/json" \
  -d '{"id": "node-2", "address": "http://127.0.0.1:9082"}'

# 4. Propor uma transação de máquina de estados para o cluster
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

# 5. Inspecionar entradas replicadas do diário WAL e somas SHA-256
curl http://localhost:9081/raft/journal
```

---

## 🛡️ TLS Mútuo de Confiança Zero (mTLS) e Atestação de Hardware

O `cf-lease-broker` incorpora um motor completo de TLS mútuo (mTLS) de confiança zero, uma Autoridade de Certificação (CA) Efêmera dinâmica em memória e atestação de integridade TPM 2.0 por hardware:

### 1. Autoridade de Certificação Efêmera e Emissão Dinâmica (`internal/mtls/ca.go`)
- **Âncora Raiz em Memória**: Inicializa dinamicamente uma CA raiz ECDSA P-256 (`x509.IsCA = true`) sem ferramentas externas ou dependências de OpenSSL.
- **Certificados de Cliente e Pares**: Emite certificados X.509 de curta duração para clientes e pares, assinados criptograficamente, com Nomes Comuns personalizados, janelas de validade e SANs.
- **Revogação CRL**: Mantém uma lista de revogação de certificados (CRL) rápida e segura para threads, indexada por números de série hexadecimais com pesquisas $O(1)$.
- **Vouchers de Emparelhamento Assinados**: Gera e valida tokens de emparelhamento ECDSA-SHA256 para comunicação intercluster de confiança zero.

### 2. Verificação de Certificados de Cliente e Políticas (`internal/mtls/verifier.go`)
- **Verificação da Cadeia de Confiança**: Valida cadeias de certificados X.509 recebidas contra o pool da CA raiz (`x509.CertPool`).
- **Aplicação de Políticas**: Impõe correspondência de SANs curinga (`*.edge.cluster.local`, blocos CIDR), Nomes Comuns permitidos, Emissores permitidos e janelas rígidas de expiração.
- **Extração Transparente de Cabeçalhos**: Extrai certificados de conexões TLS directas (`r.TLS.PeerCertificates`) ou cabeçalhos padrão de encaminhamento (`X-Client-Cert-Pem`, `X-Forwarded-Client-Cert`, `X-Client-Cert-Der`).
- **Proteção do Proxy Reverso**: Quando `CF_REQUIRE_MTLS=true` está ativo, todas as requisições via `/proxy/{service}/*` devem apresentar um certificado de cliente verificado.

### 3. Atestação de Hardware TPM 2.0 (`internal/mtls/tpm_windows.go`)
- **Integração Windows CNG**: Consulta a Criptografia de Próxima Geração da Microsoft (`ncrypt.dll`) para o "Microsoft Platform Crypto Provider" para verificar a presença de TPM 2.0.
- **Repositório de Certificados do Windows**: Inspeciona e valida repositórios de certificados de máquina/usuário via `crypt32.dll` (`CertOpenSystemStoreW`).
- **Medições de Plataforma**: Gera resumos determinísticos do Registro de Configuração de Plataforma (PCR) e medições de chaves de endosso para trilhas de auditoria.

### Exemplos de Curl para mTLS e TPM:

```bash
# 1. Inspecionar status do mTLS, CA Raiz ativa e estado de atestação TPM
curl http://localhost:9081/api/mtls/status

# 2. Baixar o certificado da CA Raiz em formato PEM
curl http://localhost:9081/api/mtls/ca/cert -o root-ca.pem

# 3. Emitir um certificado de cliente efêmero
curl -X POST http://localhost:9081/api/mtls/ca/issue \
  -H "Content-Type: application/json" \
  -d '{
    "common_name": "worker-edge-alpha",
    "sans": ["*.edge.broker.local", "127.0.0.1"],
    "validity_hours": 24
  }'

# 4. Verificar um certificado PEM X.509 contra a política ativa
curl -X POST http://localhost:9081/api/mtls/verify \
  -H "Content-Type: application/json" \
  -d '{"certificate_pem": "-----BEGIN CERTIFICATE-----\n..."}'

# 5. Revogar um número de série de certificado
curl -X POST http://localhost:9081/api/mtls/revoke \
  -H "Content-Type: application/json" \
  -d '{"serial_number": "7b8a1c9d2e"}'

# 6. Consultar cotação de atestação de hardware TPM 2.0
curl http://localhost:9081/api/mtls/tpm/attest
```

---

## 💰 Otimização Autônoma de Custos e Livro-Razão Multilocatário

O `cf-lease-broker` fornece um mecanismo de faturamento multilocatário e otimizador de custos de alta precisão projetado para rastrear o consumo até microcentavos e evitar cobranças excessivas na nuvem:

### 1. Atribuição Granular de Custos em Microcentavos (`internal/billing/meter.go`)
- **Precisão de Microcentavos**: Rastreia uso de recursos com matemática inteira de microcentavos ($1.00 = 1.000.000 microcentavos) para eliminar desvios de arredondamento de ponto flutuante.
- **Tabela Completa de Preços da Cloudflare**: Pré-calibrado com as tarifas oficiais da Cloudflare:
  - Solicitações Workers: $0.30 por 1M de requisições (300.000 microcentavos)
  - Duração de CPU Workers: $0.02 por 1M de milissegundos de CPU (20.000 microcentavos)
  - Leituras KV: $0.50 por 1M de leituras (500.000 microcentavos)
  - Gravações KV: $5.00 por 1M de gravações (5.000.000 microcentavos)
  - Armazenamento R2: $0.015 por GB-mês (15.000 microcentavos)
  - Operações Classe A R2: $4.50 por 1M de operações (4.500.000 microcentavos)
  - Operações Classe B R2: $0.36 por 1M de operações (360.000 microcentavos)
  - Largura de Banda de Saída: $0.00 (garantia de custo zero de saída da Cloudflare)
- **Franquia Diária do Nível Gratuito**: Rastreia franquias gratuitas diárias (100.000 requisições Workers e 100.000 leituras KV por conta) com renovação automática à meia-noite UTC, atribuindo custo $0 enquanto acumula a economia estimada.

### 2. Migração Autônoma de Cargas de Trabalho (`internal/billing/optimizer.go`)
- **Detecção Proativa de Excesso**: Audita continuamente o uso de contas em relação a limites configuráveis (80% por padrão).
- **Priorização do Nível Gratuito em Espera**: Quando uma conta paga se aproxima de sua cota ou teto de orçamento, o otimizador redireciona o tráfego ou impõe período de observação, direcionando locações posteriores a contas gratuitas saudáveis com cotas não utilizadas.
- **Rastreamento de Economia Acumulada**: Quantifica e relata os microcentavos exatos economizados ao evitar sobretaxas do nível pago.

### 3. OpenCost 1.0 e Exportação Financeira Multiformato (`internal/billing/journal.go`)
- **Especificação OpenCost 1.0**: Exporta alocações de faturamento diretamente compatíveis com a especificação de computação de borda OpenCost (`/api/billing/ledger?format=opencost`).
- **Exportação CSV RFC 4180**: Gera registros de faturamento separados por vírgula para análise em planilhas e auditorias contábeis (`/api/billing/ledger?format=csv`).
- **JSON Delimitado por Quebras de Linha (JSONL)**: Transmite fluxos de registros estruturados para ingestão no BigQuery, Datadog ou Elasticsearch (`/api/billing/ledger?format=jsonl`).

### Exemplos de Curl para Faturamento e Custos:

```bash
# 1. Consultar resumo de custos do cluster e detalhamento por locatário
curl http://localhost:9081/api/billing/summary

# 2. Exportar livro-razão no formato JSON OpenCost 1.0
curl http://localhost:9081/api/billing/ledger?format=opencost

# 3. Baixar diário de faturamento em CSV RFC 4180
curl http://localhost:9081/api/billing/ledger?format=csv -o ledger.csv

# 4. Acionar avaliação manual de migração de cargas de trabalho
curl -X POST http://localhost:9081/api/billing/optimize

# 5. Inspecionar histórico recente de migrações autônomas e economia
curl http://localhost:9081/api/billing/migrations

# 6. Ingerir evento personalizado de consumo de locatário
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

## 🛰️ Radar Inteligente de Saúde de Borda e Roteamento Anycast

O `cf-lease-broker` possui um motor autônomo de Radar de Saúde de Borda que monitora Pontos de Presença (PoPs) globais da Cloudflare, detecta anomalias de rede antes de perdas de pacotes e resolve rotas Anycast ideais com precisão submilissegundo:

### 1. Sondagem Sintética de Borda e Telemetria (`internal/radar/probe.go`)
- **Média Móvel Exponencial (EMA)**: Suaviza observações de sondagem de alta frequência (`alpha=0.25`) para evitar flutuações de rotas durante oscilações momentâneas.
- **Pontuação de Jitter e Perda**: Calcula jitter estatístico e taxas de perda de pacotes, gerando índices compostos de saúde (0-100) por PoP.
- **Despacho Concorrente**: Pools delimitados de threads sondam endpoints de borda simultaneamente sem sobrecarregar o sistema.

### 2. Roteamento Geográfico Anycast Submilissegundo (`internal/radar/router.go`)
- **Distância Haversine**: Calcula a distância esférica de grande círculo exata entre coordenadas de clientes e localizações de PoPs de borda.
- **Pontuação Composta**: Otimiza considerando distância geográfica, EMA de latência, jitter e disponibilidade operacional.
- **Afinidade de Inquilino**: Associa locações de inquilinos a seus PoPs regionais ideais mantendo resolução de rota submilissegundo.

### 3. Detecção de Anomalias de Rota BGP e Migração Proativa
- **Detecção de Oscilação de Rota**: Sinaliza desvios de latência superiores a 35ms ou 1,8x o EMA base como anomalias críticas.
- **Evacuação Preventiva**: Migra automaticamente locações ativas atribuídas a PoPs degradados para alternativas saudáveis antes que ocorram quedas de conexão.

### Exemplos de Curl para o Radar:

```bash
# 1. Consultar status do radar e resumo da telemetria global
curl http://localhost:9081/api/radar/status

# 2. Listar todos os PoPs monitorados na região Ásia-Pacífico
curl "http://localhost:9081/api/radar/pops?region=Asia%20Pacific"

# 3. Resolver o PoP de borda ideal para uma localização (Seul, KR)
curl -X POST http://localhost:9081/api/radar/route \
  -H "Content-Type: application/json" \
  -d '{"lat": 37.5665, "lon": 126.9780, "country": "KR"}'

# 4. Acionar sondagem sintética concorrente em todos os PoPs
curl -X POST http://localhost:9081/api/radar/probe \
  -H "Content-Type: application/json" \
  -d '{"concurrency": 10}'

# 5. Inspecionar oscilações de rota BGP e migrações autônomas recentes
curl http://localhost:9081/api/radar/events
```

---

## 🔄 Rotacionador Autônomo de Tokens de Borda e Malha de Revogação

O `cf-lease-broker` fornece um mecanismo autônomo de rotação de tokens, uma malha de revogação de alta velocidade baseada em protocolo Gossip e atestação de hardware TPM 2.0 para atualizações delegadas:

### 1. Renovação Autônoma Proativa de Tokens (`internal/rotator/rotator.go`)
- **Acionamento Proativo por Limiar**: Avalia automaticamente tokens de locação ativos e aciona renovações em segundo plano quando um token atinge 80% de sua vida útil, evitando desconexões.
- **Período de Carência sem Quedas**: Retém chaves de tokens anteriores em mapeamento de carência em memória (padrão 30 segundos), permitindo que requisições em trânsito façam a transição sem erros HTTP 401.
- **Sincronização de Vouchers de Locação**: Sincroniza chaves de tokens renovadas e expirações estendidas com `internal/broker/lease.go` em tempo real.

### 2. Malha de Revogação Gossip Sub-5ms (`internal/rotator/mesh.go`)
- **Livro-Razão Descentralizado de Lápides**: Armazena lápides de revogação criptograficamente assinadas com consultas $O(1)$ (`IsRevoked(id)`).
- **Propagação em Menos de 5ms**: Transmite diretamente lápides aos nós do cluster em menos de 5 milissegundos.
- **Verificação Inviolável HMAC-SHA256**: Cada lápide é assinada com um número de sequência monótono e validada com verificações de igualdade em tempo constante.
- **Sincronização de Estado Anti-Entropia**: Reconcilia automaticamente lápides ausentes durante apertos de mão periódicos Gossip entre pares.

### 3. Atestação de Hardware TPM 2.0 para Atualização Delegada (`internal/rotator/attest.go`)
- **Nonces de Desafio de Uso Único**: Gera nonces de desafio criptográficos com expiração rastreada, rejeitando estritamente ataques de repetição.
- **Verificação de Integridade de Plataforma**: Valida resumos PCR e cotações TPM 2.0 antes de emitir tokens renovados para solicitações delegadas.

### Exemplos de Curl para Rotacionador e Malha:

```bash
# 1. Consultar resumo do rotacionador, tokens ativos e latência da malha
curl http://localhost:9081/api/rotator/status

# 2. Listar todos os tokens gerenciados sob rotação
curl http://localhost:9081/api/rotator/tokens

# 3. Acionar manualmente a rotação sob demanda para um token
curl -X POST http://localhost:9081/api/rotator/renew \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "operator_manual_trigger"}'

# 4. Transmitir revogação instantânea na malha gossip (<5ms)
curl -X POST http://localhost:9081/api/rotator/revoke \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "security_audit_invalidation"}'

# 5. Solicitar nonce de desafio para atestação de hardware de uso único
curl http://localhost:9081/api/rotator/challenge

# 6. Executar atualização delegada com cotação TPM de hardware
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

# 7. Listar lápides de revogação ativas
curl http://localhost:9081/api/rotator/tombstones
```

---

## 📦 Limitador de Memória do Sandbox Wasm de Borda e Isolamento de Falhas

As cargas de trabalho de workers de borda em execução em implantações de brokers multilocatários exigem limites rígidos de recursos e isolamento de falhas para evitar que scripts maliciosos, vazamentos de memória ou exceções não tratadas degradem o broker host ou vazem dados entre inquilinos.

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

### Recursos Arquiteturais Fundamentais

1. **Teto Físico Rígido de 32 MiB**: A memória linear do WebAssembly é particionada em páginas de 64 KiB (`WasmPageSize = 65536`). Mesmo se a política do inquilino solicitar mais, o broker limita rigorosamente a memória linear a 512 páginas (32 MiB, `HardCeilingMemoryPages`). Qualquer alocação que exceda este teto é rejeitada com `ErrMemoryHardCeilingExceeded` e registrada como interceptação OOM.
2. **Contabilidade Determinística de Gás**: As instruções debitam pesos predefinidos (operação básica: 1, aritmética: 2, acesso à memória: 10, passo de loop: 5, chamada: 20, expansão de memória: 100/página). Quando o gás se esgota, a execução é interrompida deterministicamente com `ErrGasExhausted`, quebrando loops infinitos sem travar a thread do broker.
3. **Isolamento de Pânico e Falhas**: A execução do código convidado é envolvida em lógica de recuperação adiada (`defer func() { if r := recover(); ... }`). Falhas severas (por exemplo, desreferenciamento de ponteiro nulo SIGSEGV simulado ou pânico em tempo de execução) são interceptadas, gerando um `CrashReport` isolado com rastreamento de pilha enquanto o broker host continua atendendo o tráfego sem interrupção.
4. **Diário de Falhas e Quarentena Progressiva**: Um buffer de anel circular seguro para threads retém os últimos 500 relatórios de falha com carimbos de data/hora, motivos e estados de memória. Se um inquilino exceder o limite configurado de falhas consecutivas (padrão: 5), ele é colocado automaticamente no status `QUARANTINED` até que um operador execute a reinicialização.
5. **Limpeza por Zeramento (Zeroization)**: Ao desmontar ou redefinir uma instância, todas as fatias de memória linear são limpas com bytes zero (`subtle.ConstantTimeCompare`), impedindo a permanência de dados entre inquilinos e vazamento de tokens.

### Uso e Comandos de Exemplo

```bash
# 1. Inspecionar status do supervisor de sandbox e métricas agregadas
curl http://localhost:9081/api/sandbox/status

# 2. Executar carga computacional no sandbox Wasm convidado
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "compute",
    "gas_budget": 100000
  }'

# 3. Testar armadilha de loop infinito (interrompe no esgotamento de gás)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "infinite_loop",
    "gas_budget": 5000
  }'

# 4. Testar aplicação do teto físico de 32MB (rejeição por OOM)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "memory_alloc",
    "requested_pages": 600
  }'

# 5. Testar isolamento de pânico/falha (broker host permanece 100% estável)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "panic_fault"
  }'

# 6. Listar relatórios recentes de falhas e rastreamentos recuperados
curl "http://localhost:9081/api/sandbox/crashes?limit=10"

# 7. Registrar ou atualizar política de cota de sandbox de inquilino
curl -X POST http://localhost:9081/api/sandbox/tenants \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "max_memory_pages": 256,
    "gas_limit": 2000000,
    "max_crash_threshold": 5
  }'

# 8. Reinicializar instância e limpar quarentena
curl -X POST http://localhost:9081/api/sandbox/reset \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant-edge-prod", "instance_id": "inst-1"}'
```

---

## 📡 Referência da API REST

| Endpoint | Método | Autenticação | Descrição |
| :--- | :---: | :---: | :--- |
| `/api/sandbox/status` | `GET` | Público | Status do supervisor de sandbox, instâncias ativas, OOM e recuperações de pânico |
| `/api/sandbox/execute` | `POST` | Público | Executa cargas de trabalho Wasm convidadas sob limites estritos de memória e gás |
| `/api/sandbox/tenants` | `GET/POST` | Público | Inspeciona ou atualiza políticas de isolamento de sandbox e cotas de inquilinos |
| `/api/sandbox/crashes` | `GET` | Público | Diário anelar de falhas listando erros capturados e rastreamentos de pilha |
| `/api/sandbox/crashes/clear` | `POST` | Público | Limpa o buffer de anel do diário de falhas na memória |
| `/api/sandbox/reset` | `POST` | Público | Redefine instâncias com falha ou em quarentena para o estado inicial IDLE |
| `/api/steering/status` | `GET` | Público | Status de direcionamento eBPF HTTP/3, modo ativo e métricas de taxa de transferência |
| `/api/steering/endpoints` | `GET/POST/DELETE` | Público | Lista, registra ou remove endpoints de Workers upstream para despacho direto |
| `/api/steering/rules` | `GET/POST/DELETE` | Público | Lista, adiciona ou remove regras de roteamento por prefixo de ID de conexão QUIC |
| `/api/steering/simulate` | `POST` | Público | Simula inspeção de cabeçalhos UDP/QUIC e mede a latência de despacho em microssegundos |
| `/api/steering/stats` | `GET` | Público | Telemetria detalhada de despacho em microssegundos e proporção sub-10µs |
| `/api/rotator/status` | `GET` | Público | Status do rotacionador, tokens ativos, renovações e latência de propagação da malha |
| `/api/rotator/tokens` | `GET` | Público | Lista todos os tokens ativos sob gerenciamento de rotação |
| `/api/rotator/renew` | `POST` | Público | Dispara rotação imediata e renovação da chave de token |
| `/api/rotator/revoke` | `POST` | Público | Propaga revogação instantânea de token/locação através da malha gossip |
| `/api/rotator/gossip` | `POST` | Público | Ponto de troca gossip entre pares para lápides e adesão à malha |
| `/api/rotator/delegate` | `POST` | Público | Atualização delegada de tokens exigindo atestação de hardware TPM |
| `/api/rotator/challenge` | `GET` | Público | Gera nonce de desafio de uso único para atestação de hardware |
| `/api/rotator/tombstones` | `GET` | Público | Lista lápides de revogação assinadas no livro-razão local |
| `/api/rotator/peers` | `GET/POST` | Público | Lista pares da malha ou registra um novo nó de rede gossip |
| `/api/rotator/events` | `GET` | Público | Log de auditoria de eventos recentes de rotação e revogação |
| `/api/radar/status` | `GET` | Público | Status do radar, PoPs rastreados, latência global e contagens de migração |
| `/api/radar/pops` | `GET` | Público | Lista PoPs de borda monitorados com estatísticas de telemetria (`?region=...`) |
| `/api/radar/probe` | `POST` | Público | Dispara sondagem sintética para um PoP específico (`?colo=...`) ou todos |
| `/api/radar/route` | `GET/POST` | Público | Resolvedor de rotas Anycast submilissegundo (`?lat=...&lon=...&country=...`) |
| `/api/radar/events` | `GET` | Público | Anomalias recentes de rotas BGP e diários de migração autônoma |
| `/api/radar/migrate` | `POST` | Público | Evacua locações ativas atribuídas a um PoP degradado específico |
| `/api/billing/summary` | `GET` | Público | Resumo de custos do cluster, economias e agregados por inquilino/conta |
| `/api/billing/ledger` | `GET` | Público | Exportação do livro-razão (`?format=json|csv|jsonl|opencost&tenant=...&limit=...`) |
| `/api/billing/meter` | `POST` | Público | Ingere e atribui registros de consumo de recursos |
| `/api/billing/optimize` | `POST` | Público | Aciona avaliação de migração autônoma de cargas de trabalho |
| `/api/billing/migrations` | `GET` | Público | Inspeciona histórico recente de migrações autônomas e economia total |
| `/api/billing/rates` | `GET/POST` | Público | Consulta ou atualiza tabela de tarifas oficiais da Cloudflare em microcentavos |
| `/api/billing/reset` | `POST` | Público | Limpa o livro de faturamento em memória e dados agregados |
| `/api/mtls/status` | `GET` | Público | Inspeciona estatísticas da CA Raiz ativa, políticas e atestação TPM 2.0 |
| `/api/mtls/ca/cert` | `GET` | Público | Baixa o certificado público da CA Raiz no formato PEM |
| `/api/mtls/ca/issue` | `POST` | Público | Emite certificados efêmeros de cliente/pares com SANs e validade personalizada |
| `/api/mtls/verify` | `POST` | Público | Avalia certificado PEM X.509 em relação à CA confiável e políticas ativas |
| `/api/mtls/revoke` | `POST` | Público | Revoga número de série de certificado e adiciona à lista negra CRL |
| `/api/mtls/tpm/attest` | `GET` | Público | Consulta cotação do provedor TPM 2.0 Windows e repositório de certificados |
| `/api/mtls/peer-token` | `POST` | Público | Gera token de pares assinado com ECDSA para sincronização intercluster |
| `/api/mtls/peer-token/verify` | `POST` | Público | Verifica criptograficamente token de pares de sincronização intercluster |
| `/api/mtls/policy` | `POST/PUT` | Público | Atualiza a política de verificação de certificados de cliente ativa |
| `/raft/status` | `GET` | Público | Inspeciona função do nó local, termo, ID líder, índice commit e telemetria |
| `/raft/peers` | `GET/POST` | Público | Inspeciona pares do cluster e latência; registra nós remotos manualmente |
| `/raft/propose` | `POST` | Público | Propõe comando (`GRANT_LEASE`, `REVOKE_LEASE`, `UPDATE_QUOTA`, `SYNC_ACCOUNT`) |
| `/raft/journal` | `GET` | Público | Instantâneo de todas as entradas WAL replicadas com somas SHA-256 |
| `/raft/vote` | `POST` | Público | Endpoint RPC de votação para eleições Raft de candidatos |
| `/raft/append` | `POST` | Público | Endpoint RPC de batimentos cardíacos Raft e replicação de entradas de log |
| `/metrics` | `GET` | Público | Ponto de coleta de métricas em formato texto padrão Prometheus 0.0.4 |
| `/api/compiler/compile` | `POST` | Público | Compila e minifica código-fonte JS de Worker, retornando taxa e status sintático |
| `/api/compiler/workers` | `GET` | Público | Lista todos os pacotes de Workers implantados e seus hashes SHA-256 |
| `/api/compiler/workers` | `POST` | Público | Empacota e implanta novo pacote de Worker de borda para um inquilino |
| `/api/compiler/workers` | `DELETE` | Público | Desimplanta um pacote de Worker (`?id=bundle-...`) |
| `/api/compiler/routes` | `GET` | Público | Lista rotas dinâmicas registradas ordenadas por prioridade decrescente |
| `/api/compiler/routes` | `POST` | Público | Registra novo padrão de rota dinâmico (`pattern`, `zone_id`, `tenant_id`, `worker_id`) |
| `/api/compiler/routes` | `DELETE` | Público | Remove vínculo de rota dinâmica (`?id=route-...`) |
| `/api/compiler/routes/match` | `POST` | Público | Testa correspondência de padrões de rota de URL (`host`, `path`) |
| `/api/compiler/stream-test` | `GET` | Público | Endpoint de benchmark de transmissão direta zero-cópia (`?size_mb=1..50`) |
| `/api/config/status` | `GET` | **Bearer** | Telemetria de recarregamento a quente e lista de contas sanitizada (tokens ocultos) |
| `/api/config/reload` | `POST` | **Bearer** | Aciona recarregamento imediato sem tempo de inatividade a partir do disco |
| `/api/quota/alerts` | `GET` | Público | Histórico registrado de notificações de limites de cota |
| `/api/quota/webhooks/config` | `GET/POST` | Público | Inspeciona ou atualiza URLs de webhooks do Discord/Slack |
| `/api/quota/webhooks/test` | `POST` | Público | Dispara notificação de teste de limite de cota (80%/95%/100%) |
| `/api/quota/probation` | `GET` | Público | Registros atuais de observação probatória e temporizadores de contas |
| `/api/quota/probation/recover` | `POST` | Público | Restaura manualmente uma conta a partir do estado de observação (`?id=...`) |
| `/api/quota/probation/trigger` | `POST` | Público | Simula disparo de observação probatória por limite 429 (`?id=...`) |
| `/api/quota/ratelimit` | `GET` | Público | Telemetria do limitador de taxa por janela deslizante (`?key=...`) |
| `/api/auth/token` | `POST` | Público / Key | Emite token Bearer JWT HS256 com escopo delimitado |
| `/api/auth/revoke` | `POST` | Bearer | Revoga imediatamente um token JWT pelo seu identificador JTI |
| `/api/auth/me` | `GET` | Bearer | Verifica e inspeciona declarações ativas do chamador no token |
| `/api/auth/status` | `GET` | Público | Inspeciona provedores criptográficos e status do mecanismo de autenticação |
| `/api/security/encrypt` | `POST` | Bearer | Criptografa carga de segredos em envelope DPAPI ou AES-GCM |
| `/api/security/decrypt` | `POST` | Bearer | Testa decodificação bidirecional de envelopes de segurança |
| `/api/security/status` | `GET` | Bearer | Inspeciona mecanismo do cofre de segredos e provedor ativo |
| `/s3/{bucket}/{key...}` | `PUT/GET/HEAD/DELETE` | Público | Gateway de armazenamento virtual compatível com S3 com fragmentação em faixas |
| `/s3/{bucket}` | `GET` | Público | Listagem XML ListObjectsV2 compatível com S3 para o bucket especificado |
| `/api/kv/{ns}/{key}` | `GET/PUT/DELETE` | Público | Endpoint de cache KV particionado com hashing consistente |
| `/api/kv/stats` | `GET` | Público | Eficiência de cache, taxa de acerto e métricas por inquilino |
| `/proxy/{service}/*` | `ANY` | Público | Proxy reverso transparente com failover e interceptação de 429 |
| `/api/accounts` | `GET` | Público | Instantâneo de todas as contas Cloudflare agrupadas e limites restantes |
| `/api/leases` | `GET` | **Bearer** | Lista todas as locações de recursos ativas e revogadas |
| `/api/leases` | `POST` | **Bearer** | Concede e emite nova locação dinâmica (`target_service`, `resource_type`) |
| `/api/leases/revoke` | `POST` | **Bearer** | Invalida e revoga imediatamente um voucher de locação (`?id=lse_...`) |
| `/api/health` | `GET` | Público | Verifica latência e conectividade com a API pública da Cloudflare |

---

## 🗺️ Roteiro (Roadmap)

Consulte o [ROADMAP.md](../../ROADMAP.md) para metas de marcos e recursos planejados.

---

## 🔒 Segurança (Security)

Para divulgações de vulnerabilidades e procedimentos de denúncia, consulte o [SECURITY.md](../../SECURITY.md).

---

## 📄 Licença (License)

Licenciado sob a [Apache License 2.0](../../LICENSE).
