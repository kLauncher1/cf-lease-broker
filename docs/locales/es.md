<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <a href="./zh-CN.md">🇨🇳 中文</a> · <strong>🇪🇸 Español</strong> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**Agrupación de recursos multicuenta de Cloudflare y agente de arrendamiento dinámico de tokens para Workers, KV y R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-inicio-rápido)

<sub>Creado por **kLauncher1** · Agrupación de recursos perimetrales de alta disponibilidad, bóveda de secretos y arrendamiento dinámico multiinquilino.</sub>

</div>

---

## ⚡ Puntos Destacados (Highlights)

- **Rotador Autónomo de Tokens en el Perímetro**: Supervisión proactiva del ciclo de vida de cupones y tokens de inquilino que activa renovaciones en segundo plano al alcanzar el 80% de validez, garantizando compatibilidad con doble token durante un período de gracia de 30 segundos sin tiempo de inactividad (`internal/rotator/rotator.go`).
- **Malla de Revocación de Tokens Basada en Gossip (<5ms)**: Libro mayor descentralizado de lápidas (tombstones) con búsquedas de revocación en tiempo constante $O(1)$, lápidas firmadas con HMAC-SHA256, numeración secuencial monótona, sincronización de estado anti-entropía y difusión punto a punto a través de los nodos del clúster (`internal/rotator/mesh.go`).
- **Atestación de Hardware TPM 2.0 para Actualización Delegada**: Verificación criptográfica de la integridad de la plataforma para solicitudes delegadas de renovación de tokens, nonces de desafío de un solo uso para prevenir ataques de repetición y validación de resúmenes PCR (`internal/rotator/attest.go`).
- **Radar Inteligente de Salud Perimetral y Enrutamiento Geográfico Anycast**: Sondeo sintético concurrente de los puntos de presencia (PoP) de Cloudflare a nivel global, seguimiento de latencia con media móvil exponencial (EMA, `alpha=0.25`), cálculo de jitter e índices de salud, junto con enrutamiento Anycast submilimétrico y detección de aleteo de rutas BGP (`internal/radar/`).
- **Migración Preventiva Autónoma de Arrendamientos**: Reubica dinámicamente los arrendamientos de microservicios activos y en tránsito lejos de PoPs con degradación hacia alternativas en espera antes de que ocurran pérdidas de paquetes o tiempos de espera de sockets.
- **Optimización Autónoma de Costos y Libro Mayor de Facturación Multiinquilino**: Medición granular de cómputo, peticiones y almacenamiento con precisión de microcentavos ($1.00 = 1,000,000 microcentavos) basada en las tarifas oficiales de Cloudflare, reubicando cargas de trabajo hacia cuentas gratuitas en espera cuando se avecinan sobrecostos (`internal/billing/`).
- **Exportación Financiera en Formato OpenCost 1.0**: Generación nativa de diarios de facturación en formatos RFC 4180 CSV, JSON delimitado por saltos de línea (JSONL) y especificación estándar OpenCost 1.0 (`/api/billing/ledger?format=opencost`).
- **Mutual TLS (mTLS) Zero-Trust y Verificación de Certificados de Cliente**: Verificación rigurosa de cadenas de certificados X.509, listas de revocación (CRL), ventanas de validez, coincidencia de nombres alternativos del sujeto (SAN) con comodines (`*.domain.com`) y políticas de acceso de emisor (`internal/mtls/verifier.go`).
- **Autoridad de Certificación (CA) Efímera y Malla de Emparejamiento**: Generación en memoria sin dependencias de certificados raíz ECDSA P-256, emisión dinámica de certificados de cliente y tokens de emparejamiento inter-clúster firmados criptográficamente (`internal/mtls/ca.go`).
- **Atestación TPM 2.0 en Hardware y Almacén de Certificados de Windows**: Utiliza Windows CNG (`ncrypt.dll`) y el proveedor criptográfico de la plataforma para verificar el módulo TPM 2.0 y generar cotizaciones de medición PCR (`internal/mtls/tpm_windows.go`).
- **Motor de Consenso Raft de Alta Disponibilidad**: Clúster distribuido con temporizadores de elección aleatorios (150-300ms), quórum por mayoría absoluta, registro Write-Ahead Log (WAL) con suma de comprobación SHA-256 y descubrimiento dinámico mediante DNS SRV (`net.LookupSRV`) (`internal/raft/`).
- **Compilador Virtual de Edge Workers y Empaquetado**: Minificador y validador léxico en memoria para paquetes JavaScript/Wasm con eliminación de comentarios, validación sintáctica y huella digital de contenido SHA-256 (`internal/compiler/bundle.go`).
- **Inyección de Rutas Dinámicas Multizona**: Compilador de rutas comodín de alto rendimiento para mapear dominios arbitrarios (`*.domain.com/path/*`) con ponderación por prioridad (`internal/compiler/router.go`).
- **Tubería de Transmisión Directa Zero-Copy**: Transmisión de respuestas de bajísima latencia mediante grupos de búfer preasignados de 64KB (`sync.Pool`), vaciado inmediato de fragmentos (`http.Flusher`) y detección de desconexión del cliente (`internal/compiler/stream.go`).
- **Binarios Autónomos Multi-Arquitectura**: Flujo de compilación cruzada que empaqueta ejecutables independientes para `linux/amd64`, `linux/arm64`, `darwin/arm64` y `windows/amd64` con sumas de comprobación criptográficas SHA-256 (`scripts/build_artifacts.mjs`).
- **Recarga en Caliente sin Interrupciones**: Sincronización en tiempo real de cuentas e inquilinos mediante monitoreo de modificaciones de archivos y señales del sistema operativo (`SIGHUP` en POSIX) sin reiniciar el proceso.
- **Agregación de Cuotas Multicuenta**: Combina múltiples cuentas gratuitas y profesionales de Cloudflare en un clúster de cómputo virtual unificado.
- **Bóveda Segura de Secretos**: Cifrado en reposo mediante Windows DPAPI (`CryptProtectData`) y AES-256-GCM con limpieza segura de memoria (`Zeroize`).
- **Autenticación HS256 JWT y RBAC**: Motor de tokens HMAC-SHA256 con verificación en tiempo constante y listas de revocación JTI seguras para hilos.
- **Pasarela Virtual R2 Compatible con S3**: Puntos de conexión S3 REST (`/s3/{bucket}/{key...}`) con partición automática de 2MB y comprobación ETag compuesta.
- **Caché KV Particionada Multicuenta**: Almacenamiento seguro para hilos con dispersión consistente FNV, caché en memoria LRU y desalojo por TTL.
- **Exportador de Métricas Prometheus 0.0.4**: Punto de conexión estándar `/metrics` para raspar cuotas, estados de arrendamiento y telemetría de proxy.
- **Limitador de Tasa por Ventana Deslizante y Periodo de Prueba**: Control de tráfico en memoria con máquina de estados de retroceso exponencial para cuentas limitadas.
- **Notificaciones Webhook Multicanal**: Alertas enriquecidas para Discord, Slack y JSON en umbrales del 80%, 95% y 100%.
- **Conmutación por Error Automática ante Códigos 429**: Interceptación en tiempo real de errores 429 Too Many Requests con reintento instantáneo en la siguiente cuenta disponible del grupo.
- **Proxy Inverso Transparente**: Enrutamiento directo mediante `/proxy/{service}/*` con reintentos de cuerpo almacenado y despojo de encabezados salto a salto (RFC 7230).
- **Arrendamiento Dinámico de Recursos**: Emisión de tokens de API y endpoints acotados de corta duración (`lse_...`).
- **Implementación en un Solo Binario**: Panel de administración web integrado en el propio binario de Go mediante `//go:embed`.
- **Bajo Consumo de Memoria**: Funciona de forma continua con menos de **12 MB de RAM** sin dependencias externas.

---

## 🚀 Inicio Rápido

### 1. Descarga y Ejecución

```bash
# Clonar el repositorio
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# Ejecutar directamente con Go
go run .
```

La consola de administración estará disponible en:
👉 **`http://localhost:9081`**

### 2. Compilación Cruzada Multi-Arquitectura

```bash
# Compilar binarios independientes para linux/amd64, linux/arm64, darwin/arm64, windows/amd64
node scripts/build_artifacts.mjs --clean

# Verificar sumas de comprobación SHA-256
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Arquitectura y Agrupación de Recursos

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

## 🔒 Gestión de Secretos y Autenticación

### 1. Bóveda con Cifrado de Sobre (`internal/security/vault.go`)
- **Integración con Windows DPAPI**: Protege las claves API utilizando las credenciales del sistema operativo (`dpapi:<base64>`).
- **Cifrado AES-256-GCM**: En plataformas POSIX, aplica cifrado autenticado AES-256-GCM con vector de inicialización único de 96 bits (`aes-gcm:<nonce>:<ciphertext>`).
- **Limpieza de Memoria**: Limpieza inmediata de los búferes de memoria en uso mediante la técnica `Zeroize`.

### 2. Autenticación JWT y Middleware RBAC (`internal/security/middleware.go`)
- **Tokens HS256**: Generación y verificación de tokens JWT con validación de firmas en tiempo constante para neutralizar ataques de canal lateral.
- **Revocación Inmediata**: Revocación de credenciales por identificador JTI (`POST /api/auth/revoke`).

---

## 🪣 Pasarela Virtual R2 Compatible con S3 (`/s3/{bucket}/{key...}`)

Expone una interfaz compatible con el protocolo S3 de AWS:
1. **Fraccionamiento en Bloques**: Las cargas superiores a 2MB se dividen en fragmentos correlativos distribuidos entre las distintas cuentas de Cloudflare.
2. **Sumas de Integridad**: Cada bloque cuenta con su propio resumen SHA-256 y un ETag compuesto representativo.
3. **Peticiones por Rangos**: Soporte para la cabecera `Range: bytes=start-end` para descargas interrumpidas y vídeo en continuo.

---

## 🔄 Proxy Inverso Transparente con Conmutación por Error (`/proxy/{service}/*`)

1. **Resolución Inteligente**: Reenvía la petición a la cuenta que posea mayor margen de cuota libre (`SelectOptimalAccount`).
2. **Reintentos de Cargas Útiles**: Almacena en memoria intermedia hasta 10MB para reintentar la transmisión de datos sin pérdida ante caídas.
3. **Respuesta Rápida ante Bloqueos 429**: Enruta la petición a otra cuenta disponible de inmediato si la cuenta activa recibe un código 429 o error 5xx de pasarela.

---

## 🚀 Compilador Virtual de Workers y Enrutamiento Dinámico

1. **Minificación en Memoria**: Eliminación de comentarios y espacios innecesarios con validación de firmas de exportación en Workers.
2. **Árbol de Enrutamiento Multizona**: Reglas de enrutamiento basadas en comodines jerárquicos.
3. **Transmisión de Datos Zero-Copy**: Reducción de la huella en memoria mediante el reciclaje de fragmentos de 64KB (`sync.Pool`).

---

## 🤝 Consenso Distribuido Raft para Clústeres de Alta Disponibilidad

1. **Elección por Mayoría**: Nodos con conmutación dinámica entre los estados Follower, Candidate y Leader.
2. **Diario WAL Persistente**: Registro de operaciones determinista firmado con SHA-256.
3. **Máquina de Estados Unificada**: Propagación sincrónica de concesiones de arrendamiento, cuotas y revocaciones.

---

## 🛡️ mTLS Zero-Trust y Atestación de Hardware

1. **Emisión Dinámica de Certificados**: CA raíz ECDSA P-256 en memoria para expedir certificados a clientes y pares del clúster.
2. **Inspección de Certificados**: Coincidencia de SANs comodín y validación estricta de periodos de expiración.
3. **Atestación TPM 2.0**: Comprobación del proveedor criptográfico de Windows y lectura de registros PCR.

---

## 💰 Optimización de Costes y Contabilidad

1. **Precisión Milimétrica en Microcentavos**: Contabilidad interna en valores enteros para erradicar desviaciones por redondeo.
2. **Desvío Preventivo de Cargas**: Transferencia autónoma de flujos de trabajo hacia cuentas de nivel gratuito al superar el umbral fijado.
3. **Exportación Financiera Universal**: Soporte de formatos OpenCost 1.0, CSV y JSONL.

---

## 🛰️ Radar de Salud Perimetral y Enrutamiento Anycast

1. **Sondeo Sintético**: Análisis continuo de latencias con filtrado EMA para eludir fluctuaciones espurias.
2. **Cálculo Geográfico Preciso**: Estimación de distancias ortodrómicas mediante la fórmula de Haversine para seleccionar el PoP idóneo en milisegundos.
3. **Protección frente a Inestabilidad BGP**: Detección de saltos súbitos de latencia para evacuar conexiones activas de forma transparente.

---

## 🔄 Rotador de Credenciales y Malla Gossip

1. **Renovación Anticipada**: Petición de nuevas claves al consumirse el 80% del tiempo de validez, manteniendo un periodo de gracia de 30 segundos.
2. **Propagación Instantánea por Gossip**: Transmisión de revocaciones en menos de 5 milisegundos a todos los nodos participantes.

---

## 📡 Referencia de la API REST

| Endpoint | Método | Autenticación | Descripción |
| :--- | :---: | :---: | :--- |
| `/api/rotator/status` | `GET` | Pública | Estado del rotador, tokens en seguimiento y latencia de propagación |
| `/api/rotator/tokens` | `GET` | Pública | Relación de tokens sujetos a rotación automática |
| `/api/rotator/renew` | `POST` | Pública | Solicitud de renovación forzada de un token |
| `/api/rotator/revoke` | `POST` | Pública | Notificación de anulación de un token a través de la malla gossip |
| `/api/radar/status` | `GET` | Pública | Resumen de telemetría y estado de los PoPs examinados |
| `/api/radar/route` | `GET/POST`| Pública | Resolución de rutas geográficas Anycast submilimétricas |
| `/api/billing/summary` | `GET` | Pública | Balance de costes, ahorro generado y cómputo por inquilino |
| `/api/billing/ledger` | `GET` | Pública | Extracción de registros de gastos (`?format=json\|csv\|jsonl\|opencost`) |
| `/api/mtls/status` | `GET` | Pública | Comprobación de la CA efímera y de la prueba TPM 2.0 |
| `/api/mtls/ca/cert` | `GET` | Pública | Obtención del certificado raíz en formato PEM |
| `/api/mtls/ca/issue` | `POST` | Pública | Creación de certificados de cliente de vigencia reducida |
| `/api/mtls/verify` | `POST` | Pública | Comprobación de la validez de un certificado X.509 |
| `/raft/status` | `GET` | Pública | Datos de consenso, rol en el clúster e índice de confirmación |
| `/metrics` | `GET` | Pública | Métrica adaptada al recopilador estándar de Prometheus 0.0.4 |
| `/api/compiler/compile`| `POST` | Pública | Compilación y comprobación de sintaxis de scripts de Worker |
| `/s3/{bucket}/{key...}`| `PUT/GET` | Pública | Interfaz virtual de objetos compatible con AWS S3 |
| `/proxy/{service}/*` | `ANY` | Pública | Enrutamiento transparente con gestión de errores 429 |
| `/api/accounts` | `GET` | Pública | Inventario de cuentas de Cloudflare y consumo de cuotas |
| `/api/leases` | `GET/POST` | **Bearer** | Consulta de arrendamientos activos o adjudicación de uno nuevo |
| `/api/leases/revoke` | `POST` | **Bearer** | Anulación fulminante de un arrendamiento |
| `/api/health` | `GET` | Pública | Comprobación de la disponibilidad de la API pública de Cloudflare |

---

## 🗺️ Hoja de Ruta (Roadmap)

Consulte el documento [ROADMAP.md](../../ROADMAP.md) para conocer las siguientes fases planificadas.

---

## 🔒 Seguridad (Security)

Para la notificación responsable de vulnerabilidades, remítase a [SECURITY.md](../../SECURITY.md).

---

## 📄 Licencia (License)

Distribuido bajo los términos de la licencia [Apache License 2.0](../../LICENSE).
