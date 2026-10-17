<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <a href="./zh-CN.md">🇨🇳 中文</a> · <strong>🇪🇸 Español</strong> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**Agrupación de recursos multicuenta de Cloudflare y agente dinámico de arrendamiento de tokens para Workers, KV y R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-inicio-rápido)

<sub>Desarrollado por **kLauncher1** · Agrupación de recursos perimetrales de alta disponibilidad, almacén de secretos y arrendamiento dinámico de inquilinos.</sub>

</div>

---

## ⚡ Aspectos Destacados (Highlights)

- **Sandbox Wasm Perimetral y Límite Físico de Memoria de 32MB**: Memoria lineal administrada y segura de WebAssembly particionada en páginas de 64 KiB (`WasmPageSize = 65536`), imponiendo estrictamente un techo físico de 32 MiB (`HardCeilingMemoryPages = 512`), protección de acceso fuera de límites, limpieza de memoria por puesta a cero (Zeroization) al desmontar y seguimiento de picos de asignación (`internal/sandbox/memory.go`).
- **Contabilidad Determinista de Gas y Bloqueo de Bucles**: Presupuesto de ejecución por instrucción con medición determinista de gas y contabilización de pasos, deteniendo de inmediato las iteraciones infinitas descontroladas tras agotar el presupuesto de gas sin congelar el agente host (`internal/sandbox/gas.go`).
- **Aislamiento de Fallos del Host y Recuperación Tolerante a Pánico**: Recuperación diferida envuelve las ejecuciones de cargas invitadas, atrapando fallos del cliente (violaciones de memoria SIGSEGV, pánicos de ejecución no controlados, trampas de falta de gas y sobreasignaciones OOM) garantizando que el agente host se mantenga 100% estable, respaldado por un búfer anular de fallos (conserva hasta 500 informes recientes) y umbrales progresivos de cuarentena (`internal/sandbox/engine.go`).
- **Enrutamiento de Paquetes eBPF HTTP/3 con Cero Sobrecarga**: Envío de sockets a nivel de kernel omitiendo la pila TCP del espacio de usuario mediante sockmap eBPF y ganchos XDP en Linux, logrando latencias de despacho inferiores a 10 microsegundos (< 10µs, medido ~0.26µs) para cargas de proxy perimetral (`internal/steering/engine.go`).
- **Inspección de Cabeceras de Paquetes UDP/QUIC RFC 9000**: Inspección sin asignación en el heap que analiza cabeceras QUIC Long (Initial, 0-RTT, Handshake, Retry) y Short (1-RTT), extrayendo identificadores de conexión de destino y origen (DCID/SCID) y enrutando el tráfico directamente a los endpoints óptimos de Workers basados en reglas de prefijo CID y balanceo por menor número de conexiones (`internal/steering/quic_parser.go`).
- **Bypass de Kernel Multiplataforma y Alternativa en Espacio de Usuario**: Arquitectura multiplataforma que ofrece ganchos eBPF sockmap nativos en Linux y despacho rápido de copia cero en el espacio de usuario en Windows y macOS con latencia submicrosegundo y 100% de cobertura de pruebas (`internal/steering/`).
- **Rotador Autónomo de Tokens Perimetrales**: Supervisión proactiva del ciclo de vida de cupones de arrendamiento y tokens de inquilinos que activa renovaciones en segundo plano al alcanzar el 80% de validez, con vigencia dual de tokens sin tiempo de inactividad durante un período de gracia de 30 segundos (`internal/rotator/rotator.go`).
- **Malla de Revocación de Tokens Basada en Gossip Menor a 5ms**: Registro descentralizado de lápidas de revocación con búsquedas $O(1)$, lápidas firmadas con HMAC-SHA256, numeración secuencial monótona, sincronización de estado antientropía y difusión peer-to-peer con propagación inferior a 5ms en el clúster (`internal/rotator/mesh.go`).
- **Atestación de Hardware TPM 2.0 para Actualización Delegada**: Verificación criptográfica de integridad de plataforma para solicitudes delegadas de actualización de tokens, números de un solo uso (nonces) de desafío para evitar ataques de repetición, validación de etiquetas HMAC en tiempo constante y verificación de resúmenes de Registros de Configuración de Plataforma (PCR) (`internal/rotator/attest.go`).
- **Radar Inteligente de Salud Perimetral y Enrutamiento Geográfico Anycast**: Sondeo sintético concurrente de puntos de presencia (PoPs) globales de Cloudflare, seguimiento de latencia por media móvil exponencial (EMA, `alpha=0.25`), fluctuación (jitter) y puntajes de salud compuestos, combinado con enrutamiento geográfico Anycast submilisegundo (< 1ms) y detección de anomalías de oscilación de rutas BGP (`internal/radar/`).
- **Migración Autónoma Preventiva de Arrendamientos**: Transfiere dinámicamente arrendamientos activos de microservicios desde PoPs perimetrales degradados hacia alternativas saludables en espera antes de que ocurran pérdidas de paquetes o tiempos de espera de sockets.
- **Optimización Autónoma de Costos y Libro Contable Multinquilino**: Medición granular de cómputo, solicitudes y almacenamiento con atribución de costos en microcentavos ($1.00 = 1,000,000 microcentavos) basada en las tarifas oficiales de Cloudflare para Workers, KV y R2, junto con migración autónoma de cargas de trabajo hacia cuentas gratuitas en espera para evitar sobrecostos (`internal/billing/`).
- **Exportación Financiera Multiformato y OpenCost 1.0**: Soporte integrado de exportación de registros contables en RFC 4180 CSV, JSON delimitado por saltos de línea (JSONL) y formato estándar OpenCost 1.0 (`/api/billing/ledger?format=opencost`) para ingeniería financiera perimetral.
- **TLS Mutuo de Confianza Cero (mTLS) y Verificación de Certificados de Cliente**: Verificación integral de cadenas de certificados de cliente X.509, comprobación de listas de revocación (CRL), aplicación de ventanas de validez, coincidencia de Nombres Alternativos del Sujeto (SAN) comodín (`*.domain.com`) y políticas de acceso de emisor/nombre común para rutas de proxy inverso (`internal/mtls/verifier.go`).
- **Autoridad de Certificación Efímera (CA) y Malla de Emparejamiento**: Generación en memoria sin dependencias de certificados raíz ECDSA P-256, emisión dinámica de certificados de cliente, seguimiento de números de serie CRL y tokens de emparejamiento interclúster firmados criptográficamente (`internal/mtls/ca.go`).
- **Atestación de Hardware TPM 2.0 y Almacén de Certificados de Windows**: Utiliza la Criptografía de Próxima Generación de Windows (CNG) `ncrypt.dll` para consultar la presencia de TPM 2.0 en Microsoft Platform Crypto Provider, inspeccionar el almacén de certificados del sistema (`crypt32.dll`) y generar mediciones PCR deterministas (`internal/mtls/tpm_windows.go`).
- **Motor de Consenso Raft de Alta Disponibilidad**: Clúster de consenso Raft multinodo con temporizadores de elección aleatorios (150-300ms), elección de líder por quórum mayoritario, registro Write-Ahead Log (WAL) con indexación monótona y verificación SHA-256, y descubrimiento dinámico de pares por DNS SRV (`net.LookupSRV`) sin dependencias externas (`internal/raft/`).
- **Compilador Virtual y Empaquetador de Workers Perimetrales**: Minificador léxico y validador en memoria ultrarrápido para paquetes JavaScript/Wasm con eliminación de comentarios, verificación de sintaxis y hash de contenido SHA-256 (`internal/compiler/bundle.go`).
- **Inyección Dinámica de Rutas Multizona**: Compilador de rutas comodín de alto rendimiento que coincide con dominios arbitrarios (`*.domain.com/path/*`) con ponderación de prioridad y tablas de inyección dinámica de rutas (`internal/compiler/router.go`).
- **Canal de Transmisión Directa de Copia Cero**: Transmisión de respuestas perimetrales de baja latencia utilizando grupos de búferes de 64KB preasignados (`sync.Pool`), vaciado inmediato de fragmentos (`http.Flusher`) y detección de desconexión del cliente (`internal/compiler/stream.go`).
- **Artefactos Independientes Multiarquitectura**: Canal de compilación cruzada que empaqueta ejecutables independientes optimizados para `linux/amd64`, `linux/arm64`, `darwin/arm64` y `windows/amd64` con generación automática de sumas de verificación SHA-256 (`scripts/build_artifacts.mjs`).
- **Recarga en Caliente sin Tiempo de Inactividad**: Recarga dinámica de configuración y credenciales mediante supervisión de archivos y señales del SO (`SIGHUP` en POSIX) que sincroniza inquilinos agrupados y el almacén de secretos sin reiniciar procesos.
- **Agregación de Cuotas Multicuenta**: Agrupa múltiples cuentas gratuitas y profesionales de Cloudflare en un clúster de cómputo virtual unificado, superando los límites diarios de solicitudes individuales.
- **Almacén de Secretos de Producción**: Cifrado envolvente en reposo mediante Windows DPAPI (`CryptProtectData`) y AES-256-GCM multiplataforma con limpieza segura de memoria (`Zeroize`).
- **Autenticación HS256 JWT y RBAC**: Motor de autenticación administrativa y de microservicios con tokens HMAC-SHA256, verificación en tiempo constante y listas de bloqueo JTI seguras para subprocesos.
- **Pasarela Virtual R2 Compatible con S3**: Endpoints con protocolo REST de S3 (`/s3/{bucket}/{key...}`) que distribuyen el almacenamiento de objetos en depósitos R2 agrupados con fragmentación automática de 2MB y verificación de integridad SHA-256.
- **Caché KV Particionada Multicuenta**: Capa de almacenamiento KV particionada segura para subprocesos con hashing FNV consistente, almacenamiento en caché LRU y desalojo por TTL.
- **Exportador de Métricas Prometheus 0.0.4**: Endpoint estándar `/metrics` que exporta recuentos de arrendamientos, cuotas de inquilinos, rechazos de límite de tasa y telemetría de proxy sin dependencias externas.
- **Límite de Tasa por Ventana Deslizante y Periodo de Observación Automático**: Rastreador de tasa en memoria por ventana deslizante con tolerancia a ráfagas y máquina de estados de período de prueba con retroceso exponencial.
- **Webhooks Multicanal de Cuotas**: Notificaciones enriquecidas en Discord, Slack Block Kit y JSON genérico disparadas a umbrales del 80% (Advertencia), 95% (Crítico) y 100% (Agotado) con amortiguación de 10 minutos.
- **Intercepción Automática de Límites de Tasa y Conmutación por Error**: Intercepta errores HTTP 429 y de pasarela en tiempo real, redirigiendo solicitudes en curso a la siguiente cuenta saludable del grupo sin interrupciones.
- **Proxy Inverso Transparente**: Reenvía tráfico fluidamente mediante `/proxy/{service}/*` con reintentos de cuerpo almacenado en búfer y eliminación de cabeceras salto a salto RFC 7230.
- **Arrendamiento Dinámico de Recursos (Cupones)**: Emite tokens API de corta duración con ámbito delimitado (`lse_...`) para microservicios internos, llamadores externos o suites de pruebas.
- **Despliegue en un Solo Ejecutable**: El panel de administración web con estética glassmorphism está incrustado directamente en el binario Go mediante `//go:embed`.
- **Huella de Memoria Ligera**: Se ejecuta de forma persistente consumiendo menos de **12 MB de RAM** sin dependencias externas en tiempo de ejecución.

---

## 🚀 Inicio Rápido

### 1. Descargar y Ejecutar

```bash
# Clonar el repositorio
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# Ejecutar directamente
go run .
```

La consola de administración estará disponible en:
👉 **`http://localhost:9081`**

### 2. Compilación Cruzada de Artefactos Independientes Multiarquitectura

```bash
# Compilar binarios independientes para linux/amd64, linux/arm64, darwin/arm64, windows/amd64
node scripts/build_artifacts.mjs --clean

# Verificar sumas de comprobación criptográficas SHA-256
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Arquitectura y Agrupación de Recursos

```text
┌────────────────────────────────────────────────────────────────────────┐
│                            cf-lease-broker                             │
│                         Un Solo Ejecutable Go                          │
├───────────────────┬───────────────────┬────────────────────────────────┤
│   internal/raft   │  internal/broker  │       internal/storage         │
│ • Núcleo Raft     │ • Grupo de Cuentas│ • Pasarela Virtual S3 (/s3/...)│
│ • Registro WAL    │ • Arrendamientos  │ • Fragmentador en Bandas 2MB   │
│ • Pares DNS SRV   │ • Balance de Cuota│ • Caché KV Particionada        │
│ • Sincronía FSM   │ • Conmutación Auto│ • Webhooks Multicanal          │
├───────────────────┼───────────────────┼────────────────────────────────┤
│   internal/mtls   │ internal/security │       internal/compiler        │
│ • CA Efímera      │ • Windows DPAPI   │ • Minificador Virtual JS/Wasm  │
│ • Cert X509 Clnt  │ • AES-256-GCM     │ • Compilador Rutas Multizona   │
│ • Atestación TPM  │ • HS256 JWT/RBAC  │ • Tubería Flujo 64KB Cero-Copia│
│ • Tokens de Pares │ • Limpieza Memoria│ • Repositorio Paquetes Inquilin│
├───────────────────┴───────────────────┴────────────────────────────────┤
│                         internal/quota & config                        │
│  • Prometheus (/metrics) • Ventana Deslizante • Recarga en Caliente    │
├────────────────────────────────────────────────────────────────────────┤
│                         Motor HTTP net/http                            │
│   • /api/mtls/* (Cert/Verif/TPM)       • /api/compiler/*               │
│   • /raft/* (Voto/Anexo/Estado)        • /proxy/{svc}/* (Guarda mTLS)  │
│   • /metrics         • /api/auth/*     • /s3/{bkt}/{key}               │
│   • /api/accounts    • /api/security   • /api/config/* (Guarda Auth)   │
├────────────────────────────────────────────────────────────────────────┤
│                       Panel de Control Web Integrado                   │
│                     (UI Glassmorphism vía //go:embed)                  │
│                     web/dist/index.html -> Puerto 9081                 │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 🔒 Gestión de Secretos y Autenticación

`cf-lease-broker` proporciona almacenamiento seguro de secretos y autenticación de nivel empresarial sin dependencias de terceros:

### 1. Bóveda con Cifrado Envolvente (`internal/security/vault.go`)
- **Integración con Windows DPAPI**: En sistemas Windows, los secretos se protegen mediante Windows DPAPI (`CryptProtectData`), asegurando las claves API con credenciales a nivel del SO (`dpapi:<base64>`).
- **Envoltorio AES-256-GCM**: En plataformas que no son Windows (o cuando se especifica), los secretos se cifran mediante AES-256-GCM autenticado con valores nonce aleatorios únicos de 96 bits (`aes-gcm:<nonce>:<ciphertext>`).
- **Limpieza de Memoria**: Los búferes de bytes confidenciales se limpian inmediatamente después de su uso mediante el patrón `Zeroize` (`for i := range b { b[i] = 0 }`).

### 2. Middleware JWT y RBAC (`internal/security/middleware.go`)
- **Tokens Estándar HS256**: Emite y verifica JWT HMAC-SHA256 con verificación de firmas en tiempo constante (`crypto/subtle.ConstantTimeCompare`) para prevenir ataques de canal lateral temporales.
- **Lista de Denegación por Revocación**: Permite la revocación inmediata de tokens por JTI (`POST /api/auth/revoke`) con barrido automático por expiración TTL.
- **APIs Administrativas Protegidas**: Los endpoints de administración (`/api/leases`, `/api/leases/revoke` y `/api/security/*`) requieren una cabecera válida `Authorization: Bearer <token>` o claves maestras de arranque.

```bash
# 1. Emitir un token JWT de operador
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. Llamar al endpoint protegido de arrendamientos con token Bearer
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 Pasarela Virtual R2 Compatible con S3 (`/s3/{bucket}/{key...}`)

El agente expone endpoints estándar compatibles con AWS S3 que permiten a los SDK de S3, microservicios y herramientas CLI almacenar objetos en cuentas agrupadas de Cloudflare R2 sin exceder las cuotas individuales de almacenamiento de los inquilinos:

### Capacidades Principales:
1. **Fragmentación en Bandas**: Las cargas entrantes que superan los 2MB se dividen automáticamente en bloques secuenciales (`chk_<hash>_<index>`) y se distribuyen entre las cuentas saludables del grupo.
2. **Sumas de Verificación de Integridad**: Cada fragmento incluye una suma de verificación individual SHA-256, y los objetos reciben ETags compuestos estándar compatibles con subidas multiparte.
3. **Solicitudes HTTP Range**: Admite cabeceras `Range: bytes=start-end` para transmisión de video, reanudación de descargas y lecturas parciales de archivos grandes.
4. **ListObjectsV2**: Cumple con el formato estándar de respuesta XML S3 `ListBucketResult`.

### Ejemplos con CLI S3 y Curl:

```bash
# 1. Almacenar un objeto en depósitos R2 agrupados
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. Recuperar el objeto
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. Solicitud de rango parcial de bytes
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf

# 4. Listar objetos del depósito (XML S3)
curl http://localhost:9081/s3/assets
```

---

## 🔄 Proxy Inverso Transparente con Conmutación por Error (`/proxy/{service}/*`)

El intermediario proporciona un proxy inverso perimetral transparente en `/proxy/{service}/*`. Los emisores dirigen sus solicitudes HTTP a este endpoint sin necesidad de saber qué cuenta de inquilino posee actualmente cuota disponible.

### Cómo Funciona la Conmutación por Error:
1. **Resolución del Destino**: El `{service}` entrante y la subruta se resuelven contra la cuenta agrupada con la mayor cuota restante (`SelectOptimalAccount`).
2. **Almacenamiento en Búfer de la Carga**: Los cuerpos de las solicitudes se almacenan en búfer en memoria (hasta 10MB) mediante lectores de flujo para que los reintentos puedan reproducir la carga completa sin pérdida de datos.
3. **Intercepción de Códigos 429 y Errores de Pasarela**: Si la cuenta de destino responde con HTTP `429 Too Many Requests`, `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout` o experimenta una caída de conexión:
   - El agente marca inmediatamente a ese inquilino como no saludable mediante `TriggerFailover(accountID)`.
   - La solicitud se reintenta automáticamente contra la siguiente cuenta saludable del grupo.
4. **Depuración Salto a Salto**: Las cabeceras salto a salto RFC 7230 (`Connection`, `Keep-Alive`, `Upgrade`, `TE`, `Trailers`, etc.) se eliminan en ambas direcciones.
5. **Cabeceras de Telemetría**: Las respuestas exitosas incluyen cabeceras de inspección:
   - `X-CF-Broker-Account: {account_id}` (inquilino que procesó la solicitud)
   - `X-CF-Broker-Attempts: {count}` (número de intentos requeridos)

---

## 🔄 Recarga en Caliente sin Tiempo de Inactividad (`internal/config/`)

`cf-lease-broker` admite la rotación dinámica de credenciales y la actualización de grupos de cuentas sin reiniciar el binario ni interrumpir las conexiones HTTP activas:

### Mecánica de Recarga en Caliente:
1. **Bucle de Supervisión de Archivos**: Sondea periódicamente el archivo de configuración (`CF_BROKER_CONFIG_PATH` o `config.json`) y evalúa los cambios de contenido mediante resúmenes deterministas SHA-256 para evitar recargas espurias.
2. **Manejo de Señales POSIX**: Captura `SIGHUP` (Unix/Linux/macOS) para activar la validación y recarga sincrónica e inmediata de la configuración.
3. **Intercambio de Estado Atómico y Seguridad de Reserva**: Si un archivo recargado contiene JSON no válido, límites negativos o IDs de inquilinos duplicados, la recarga falla de forma segura con un registro de error, preservando la configuración activa intacta.
4. **Sincronización de Credenciales y Grupo**: Los inquilinos recargados con éxito se sincronizan directamente con `PoolManager` (`SyncAccounts`) y se cifran en `SecretVault` (`StoreCredential`).

### Formato del Archivo de Configuración (`config.json`):
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

## 🚀 Compilador Virtual de Workers Perimetrales e Inyección Dinámica de Rutas

El intermediario cuenta con un motor integrado de compilación y enrutamiento para scripts de Workers perimetrales multiinquilino y módulos WebAssembly:

### 1. Minificación Léxica y Empaquetado en Memoria (`internal/compiler/bundle.go`)
- **Tokenizador Léxico**: Elimina comentarios de una línea (`//`) y multilínea (`/* */`), suprime espacios en blanco redundantes, normaliza la separación de puntuación y valida las firmas de exportación de módulos de Cloudflare Worker (`export default { fetch(...) }`, `addEventListener('fetch')`).
- **Validación de WebAssembly**: Verifica las cabeceras mágicas de 4 bytes (`\x00asm`) y la conformidad de versión antes de empaquetar inquilinos.
- **Hash de Contenido**: Genera resúmenes SHA-256 deterministas para el control de versiones y el seguimiento de auditoría de los paquetes de inquilinos.

### 2. Compilador de Rutas Comodín Multizona (`internal/compiler/router.go`)
- **Mapeo de Dominios Arbitrarios**: Asigna endpoints en subdominios comodín (`*.domain.com/*`), hosts exactos (`api.domain.com/v1/*`) o reglas de reserva global (`*/*`).
- **Trie Ponderado por Prioridad**: Evalúa el host y la ruta de la solicitud entrante con puntuación de especificidad (host exacto > subdominio comodín > global; prefijo de ruta más largo > prefijo corto), enrutando el tráfico dinámicamente sin recargas de configuración.

### 3. Canal de Transmisión Directa de Copia Cero (`internal/compiler/stream.go`)
- **Grupo de Búferes**: Utiliza segmentos de bytes de 64KB preasignados (`sync.Pool`) para eliminar asignaciones en el montículo durante transferencias proxy de alto rendimiento.
- **Vaciado Inmediato de Sockets**: Aprovecha `http.Flusher` para transmitir fragmentos de respuesta a los clientes en tiempo real mientras desactiva el almacenamiento intermedio mediante `X-Accel-Buffering: no`.
- **Detección de Cancelación**: Se vincula a `context.Context` para cancelar inmediatamente los flujos de Workers ascendentes ante desconexiones de clientes, evitando fugas de sockets.

---

## 🤝 Máquina de Estados Raft de Alta Disponibilidad para Clústeres de Agentes Distribuidos

`cf-lease-broker` integra un motor de consenso Raft sin dependencias (`internal/raft/`) que permite la agrupación de agentes en múltiples regiones, la replicación de estados de arrendamiento sin pérdida de datos y la conmutación por error automática del líder:

### Arquitectura Principal y Primitivas de Consenso:
1. **Elección de Líder por Quórum y Temporizadores Aleatorios**: Los nodos transitan entre los estados Seguidor (Follower), Candidato (Candidate) y Líder (Leader). Los tiempos de espera de elección aleatorios (150–300 ms) evitan bloqueos por división de votos, mientras que un tic de latido de 50 ms mantiene la autoridad del líder en quórums mayoritarios `((N/2) + 1)`.
2. **Registro WAL de Escritura Anticipada (`internal/raft/journal.go`)**: Aplica una indexación de transacciones monótona con sumas de verificación de integridad SHA-256. La consistencia del registro del seguidor se valida antes de anexar, truncando automáticamente entradas conflictivas no confirmadas. Admite persistencia opcional en disco de solo adición (`raft_journal.wal`).
3. **Descubrimiento Dinámico de Pares mediante DNS SRV (`internal/raft/discovery.go`)**: Resuelve automáticamente los pares del clúster a través de registros DNS SRV RFC 2782 (`_raft._tcp.<domain>`) con degradación elegante a asignaciones estáticas de entorno (`CF_RAFT_PEERS`) y registro manual en tiempo de ejecución (`POST /raft/peers`).
4. **Máquina de Estados Finitos (FSM) del Intermediario (`internal/raft/fsm_broker.go`)**: Replica comandos de transacción de manera determinista en todos los nodos:
   - `GRANT_LEASE`: Replica cupones de arrendamiento activos en el clúster.
   - `REVOKE_LEASE`: Propaga la revocación del arrendamiento a todos los motores pares.
   - `SYNC_ACCOUNT`: Sincroniza perfiles de inquilinos y límites de cuota.
   - `UPDATE_QUOTA`: Propaga incrementos diarios de solicitudes de Workers, deltas de almacenamiento R2 y recuentos de lectura KV.
   - `NOOP`: Sondas de barrera de consenso para comprobaciones de preparación del clúster.

### Flujo de Topología del Clúster:

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

### Ejemplos de Curl para el Clúster Raft:

```bash
# 1. Inspeccionar el estado del nodo local y la topología del clúster
curl http://localhost:9081/raft/status

# 2. Consultar la salud de los pares y la latencia de ida y vuelta
curl http://localhost:9081/raft/peers

# 3. Registrar manualmente un nuevo nodo par
curl -X POST http://localhost:9081/raft/peers \
  -H "Content-Type: application/json" \
  -d '{"id": "node-2", "address": "http://127.0.0.1:9082"}'

# 4. Proponer una transacción de máquina de estados al clúster
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

# 5. Inspeccionar entradas WAL replicadas y sumas de verificación SHA-256
curl http://localhost:9081/raft/journal
```

---

## 🛡️ TLS Mutuo de Confianza Cero (mTLS) y Atestación de Hardware

`cf-lease-broker` incorpora un motor completo de TLS mutuo (mTLS) de confianza cero, una Autoridad de Certificación (CA) Efímera dinámica en memoria y atestación de integridad TPM 2.0 respaldada por hardware:

### 1. Autoridad de Certificación Efímera y Emisión Dinámica (`internal/mtls/ca.go`)
- **Ancla Raíz en Memoria**: Inicializa dinámicamente una CA raíz ECDSA P-256 (`x509.IsCA = true`) sin herramientas externas ni dependencias de OpenSSL.
- **Certificados de Cliente y Pares**: Emite certificados X.509 de cliente y de pares de corta duración y firmados criptográficamente, con Nombres Comunes, ventanas de validez y Nombres Alternativos del Sujeto (SAN) personalizados.
- **Revocación CRL**: Mantiene una lista de revocación de certificados (CRL) rápida y segura para subprocesos, indexada por números de serie hexadecimales con búsquedas de denegación $O(1)$.
- **Vales de Emparejamiento Firmados**: Genera y valida tokens de emparejamiento ECDSA-SHA256 para comunicación interclúster de confianza cero.

### 2. Verificación de Certificados de Cliente y Aplicación de Políticas (`internal/mtls/verifier.go`)
- **Verificación de Cadena de Confianza**: Valida las cadenas de certificados X.509 entrantes contra el grupo de la CA raíz (`x509.CertPool`).
- **Aplicación de Políticas**: Aplica coincidencia de SAN comodín (`*.edge.cluster.local`, bloques CIDR), Nombres Comunes permitidos, Emisores permitidos y ventanas estrictas de caducidad de certificados.
- **Extracción Transparente de Cabeceras Perimetrales**: Extrae certificados de conexiones TLS directas (`r.TLS.PeerCertificates`) o de cabeceras estándar de reenvío perimetral (`X-Client-Cert-Pem`, `X-Forwarded-Client-Cert`, `X-Client-Cert-Der`).
- **Protección del Proxy Inverso**: Cuando `CF_REQUIRE_MTLS=true` está habilitado, todas las solicitudes a través de `/proxy/{service}/*` deben presentar un certificado de cliente verificado.

### 3. Atestación de Hardware TPM 2.0 (`internal/mtls/tpm_windows.go`)
- **Integración con Windows CNG**: Consulta la Criptografía de Próxima Generación de Microsoft (`ncrypt.dll`) para el "Microsoft Platform Crypto Provider" para verificar la presencia de TPM 2.0.
- **Almacén de Certificados del Sistema Windows**: Inspecciona y valida los almacenes de certificados de máquina/usuario mediante `crypt32.dll` (`CertOpenSystemStoreW`).
- **Cotizaciones de Plataforma**: Genera resúmenes deterministas de Registros de Configuración de Plataforma (PCR) y mediciones de claves de respaldo para registros de auditoría.

### Ejemplos de Curl para mTLS y TPM:

```bash
# 1. Inspeccionar el estado de mTLS, la CA Raíz activa y el estado de atestación TPM
curl http://localhost:9081/api/mtls/status

# 2. Descargar el certificado de la CA Raíz en formato PEM
curl http://localhost:9081/api/mtls/ca/cert -o root-ca.pem

# 3. Emitir un certificado de cliente efímero
curl -X POST http://localhost:9081/api/mtls/ca/issue \
  -H "Content-Type: application/json" \
  -d '{
    "common_name": "worker-edge-alpha",
    "sans": ["*.edge.broker.local", "127.0.0.1"],
    "validity_hours": 24
  }'

# 4. Verificar un certificado PEM X.509 contra la política activa
curl -X POST http://localhost:9081/api/mtls/verify \
  -H "Content-Type: application/json" \
  -d '{"certificate_pem": "-----BEGIN CERTIFICATE-----\n..."}'

# 5. Revocar un número de serie de certificado
curl -X POST http://localhost:9081/api/mtls/revoke \
  -H "Content-Type: application/json" \
  -d '{"serial_number": "7b8a1c9d2e"}'

# 6. Consultar cotización de atestación de hardware TPM 2.0
curl http://localhost:9081/api/mtls/tpm/attest
```

---

## 💰 Optimización Autónoma de Costos y Libro Contable Multinquilino

`cf-lease-broker` proporciona un motor de facturación multinquilino y un optimizador de costos de alta precisión diseñados para rastrear el consumo hasta microcentavos y prevenir sobrecostos en la nube de forma autónoma:

### 1. Atribución Granular de Costos en Microcentavos (`internal/billing/meter.go`)
- **Precisión de Microcentavos**: Realiza el seguimiento del uso de recursos con aritmética entera de microcentavos ($1.00 = 1,000,000 de microcentavos) para eliminar por completo desviaciones por redondeo en coma flotante.
- **Modelo Completo de Precios de Cloudflare**: Precalibrado con las tarifas oficiales de Cloudflare:
  - Solicitudes de Workers: $0.30 por 1M de solicitudes (300,000 microcentavos)
  - Duración de CPU de Workers: $0.02 por 1M de milisegundos de CPU (20,000 microcentavos)
  - Lecturas KV: $0.50 por 1M de lecturas (500,000 microcentavos)
  - Escrituras KV: $5.00 por 1M de escrituras (5,000,000 microcentavos)
  - Almacenamiento R2: $0.015 por GB-mes (15,000 microcentavos)
  - Operaciones Clase A de R2: $4.50 por 1M de operaciones (4,500,000 microcentavos)
  - Operaciones Clase B de R2: $0.36 por 1M de operaciones (360,000 microcentavos)
  - Ancho de Banda Saliente: $0.00 (garantía de cero costo de salida de Cloudflare)
- **Asignación Diaria de Nivel Gratuito**: Realiza el seguimiento de las concesiones gratuitas diarias (100,000 solicitudes de Workers y 100,000 lecturas KV gratuitas por cuenta) con renovación automática a medianoche UTC, atribuyendo un costo de $0 mientras acumula el ahorro estimado.

### 2. Migración Autónoma de Cargas de Trabajo (`internal/billing/optimizer.go`)
- **Detección Proactiva de Excesos**: Audita continuamente el uso de las cuentas frente a umbrales de exceso configurables (por defecto 80%).
- **Priorización de Nivel Gratuito en Espera**: Cuando una cuenta de pago o muy utilizada se acerca a su cuota o límite de presupuesto, el optimizador redirige el tráfico de forma autónoma o establece un período de prueba de enfriamiento, dirigiendo arrendamientos y tráfico de proxy posteriores a cuentas gratuitas en espera saludables con cuotas diarias sin usar.
- **Seguimiento de Ahorros Acumulados**: Cuantifica e informa los microcentavos exactos ahorrados al desviar el tráfico de los sobrecostos de nivel de pago.

### 3. OpenCost 1.0 y Exportación Financiera Multiformato (`internal/billing/journal.go`)
- **Especificación OpenCost 1.0**: Exporta asignaciones de facturación de inquilinos coincidentes con la especificación de cómputo perimetral OpenCost (`/api/billing/ledger?format=opencost`).
- **Exportación CSV RFC 4180**: Genera registros de facturación separados por comas para análisis en hojas de cálculo y auditorías financieras (`/api/billing/ledger?format=csv`).
- **JSON Delimitado por Saltos de Línea (JSONL)**: Transmite registros estructurados para ingestión en BigQuery, Datadog o Elasticsearch (`/api/billing/ledger?format=jsonl`).

### Ejemplos de Curl para Facturación y Costos:

```bash
# 1. Consultar resumen de costos del clúster y desglose por inquilino
curl http://localhost:9081/api/billing/summary

# 2. Exportar libro contable de facturación en formato JSON OpenCost 1.0
curl http://localhost:9081/api/billing/ledger?format=opencost

# 3. Descargar diario de facturación en CSV RFC 4180
curl http://localhost:9081/api/billing/ledger?format=csv -o ledger.csv

# 4. Activar evaluación de migración de cargas de trabajo a petición
curl -X POST http://localhost:9081/api/billing/optimize

# 5. Inspeccionar historial reciente de migraciones autónomas y ahorros
curl http://localhost:9081/api/billing/migrations

# 6. Ingerir evento de uso de recursos personalizado de inquilino
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

## 🛰️ Radar Inteligente de Salud Perimetral y Enrutamiento Geográfico Anycast

`cf-lease-broker` cuenta con un motor autónomo de Radar de Salud Perimetral que monitorea los Puntos de Presencia (PoPs) globales de Cloudflare, detecta anomalías de red antes de que ocurra pérdida de paquetes y resuelve rutas Anycast óptimas con precisión submilisegundo:

### 1. Sondeo Sintético Perimetral y Telemetría (`internal/radar/probe.go`)
- **Media Móvil Exponencial (EMA)**: Suaviza las observaciones de sondeo de alta frecuencia (`alpha=0.25`) para evitar fluctuaciones de ruta durante oscilaciones momentáneas.
- **Puntuación de Fluctuación y Pérdida**: Calcula la fluctuación estadística y las tasas de pérdida de paquetes, generando índices compuestos de salud (0-100) por PoP.
- **Despacho Concurrente**: Grupos acotados de subprocesos sondean endpoints perimetrales simultáneamente sin agotar los recursos.

### 2. Enrutamiento Geográfico Anycast Submilisegundo (`internal/radar/router.go`)
- **Distancia Haversine**: Calcula la distancia esférica de círculo máximo exacta entre las coordenadas del cliente y las ubicaciones de los PoPs perimetrales.
- **Puntuación Compuesta**: Optimiza considerando distancia geográfica, EMA de latencia, fluctuación y estado de salud operativo.
- **Afinidad de Inquilino**: Vincula arrendamientos de inquilinos a sus PoPs regionales óptimos manteniendo una resolución de ruta submilisegundo.

### 3. Detección de Anomalías de Ruta BGP y Migración Proactiva
- **Detección de Oscilación de Ruta**: Marca desviaciones de latencia que superan los 35 ms o 1.8 veces el EMA base como anomalías críticas.
- **Evacuación Preventiva**: Migra automáticamente los arrendamientos activos asignados a PoPs degradados a alternativas saludables en espera antes de que los clientes experimenten pérdidas de paquetes o caídas de conexión.

### Ejemplos de Curl para el Radar:

```bash
# 1. Consultar estado del radar y resumen de telemetría global
curl http://localhost:9081/api/radar/status

# 2. Listar todos los PoPs monitoreados en Asia Pacífico
curl "http://localhost:9081/api/radar/pops?region=Asia%20Pacific"

# 3. Resolver el PoP perimetral óptimo para una ubicación (Seúl, KR)
curl -X POST http://localhost:9081/api/radar/route \
  -H "Content-Type: application/json" \
  -d '{"lat": 37.5665, "lon": 126.9780, "country": "KR"}'

# 4. Activar sondeo sintético concurrente en todos los PoPs
curl -X POST http://localhost:9081/api/radar/probe \
  -H "Content-Type: application/json" \
  -d '{"concurrency": 10}'

# 5. Inspeccionar oscilaciones de ruta BGP y migraciones autónomas recientes
curl http://localhost:9081/api/radar/events
```

---

## 🔄 Rotador Autónomo de Tokens Perimetrales y Malla de Revocación

`cf-lease-broker` proporciona un motor autónomo de rotación de tokens perimetrales, una malla de revocación de alta velocidad basada en gossip y atestación TPM 2.0 respaldada por hardware para operaciones de actualización delegada:

### 1. Renovación Autónoma Proactiva de Tokens (`internal/rotator/rotator.go`)
- **Activación por Umbral Proactivo**: Evalúa automáticamente los tokens de arrendamiento activos y activa renovaciones en segundo plano cuando un token alcanza el 80% de su ciclo de vida, evitando desconexiones de clientes perimetrales.
- **Período de Gracia de Traspaso sin Inactividad**: Conserva las claves de tokens anteriores en un mapeo de gracia en memoria (por defecto 30 segundos), permitiendo que las solicitudes en tránsito y los proxies perimetrales realicen la transición sin caídas ni errores HTTP 401.
- **Sincronización de Vales de Arrendamiento**: Sincroniza las claves de tokens renovadas y los vencimientos extendidos con `internal/broker/lease.go` en tiempo real.

### 2. Malla de Revocación Gossip Menor a 5ms (`internal/rotator/mesh.go`)
- **Libro Descentralizado de Lápidas**: Almacena lápidas de revocación firmadas criptográficamente con complejidad de búsqueda $O(1)$ (`IsRevoked(id)`).
- **Propagación Sub-5ms**: Transmite directamente lápidas a los nodos del clúster en menos de 5 milisegundos.
- **Verificación Antimanipulación HMAC-SHA256**: Cada lápida está firmada con un número de secuencia monótono y se valida con comprobaciones de igualdad en tiempo constante.
- **Sincronización de Estado Antientropía**: Reconcilia automáticamente lápidas faltantes durante los intercambios gossip entre pares.

### 3. Atestación de Hardware TPM 2.0 para Actualización Delegada (`internal/rotator/attest.go`)
- **Nonces de Desafío de Un Solo Uso**: Genera nonces de desafío criptográficos con seguimiento de vencimiento, rechazando estrictamente ataques de repetición.
- **Verificación de Integridad de Plataforma**: Valida los resúmenes del Registro de Configuración de Plataforma (PCR) y las cotizaciones TPM 2.0 antes de emitir tokens de arrendamiento renovados para solicitudes delegadas.

### Ejemplos de Curl para Rotador y Malla:

```bash
# 1. Consultar resumen del rotador, tokens activos y latencia de la malla
curl http://localhost:9081/api/rotator/status

# 2. Listar todos los tokens administrados en rotación
curl http://localhost:9081/api/rotator/tokens

# 3. Activar manualmente la rotación a petición de un token
curl -X POST http://localhost:9081/api/rotator/renew \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "operator_manual_trigger"}'

# 4. Transmitir revocación instantánea en la malla gossip (<5ms)
curl -X POST http://localhost:9081/api/rotator/revoke \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "security_audit_invalidation"}'

# 5. Solicitar nonce de desafío de atestación de hardware de un solo uso
curl http://localhost:9081/api/rotator/challenge

# 6. Realizar actualización delegada de token con cotización TPM de hardware
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

# 7. Listar lápidas de revocación activas
curl http://localhost:9081/api/rotator/tombstones
```

---

## 📦 Limitador de Memoria de Sandbox Wasm Perimetral y Aislamiento de Caídas

Las cargas de trabajo de workers perimetrales que se ejecutan en implementaciones de agentes multiinquilino requieren límites de recursos absolutos y aislamiento de fallos para evitar que scripts rebeldes, fugas de memoria o excepciones no controladas degraden el agente host o filtren datos entre inquilinos.

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

### Capacidades Principales de la Arquitectura

1. **Límite Físico Estricto de 32 MiB**: La memoria lineal de WebAssembly está particionada en páginas de 64 KiB (`WasmPageSize = 65536`). Incluso si la política de un inquilino solicita más, el agente limita estrictamente la memoria lineal a 512 páginas (32 MiB, `HardCeilingMemoryPages`). Cualquier asignación que exceda este límite superior se rechaza con `ErrMemoryHardCeilingExceeded` y se registra como una intercepción OOM.
2. **Contabilidad Determinista de Gas**: Las instrucciones descuentan pesos predefinidos (operación básica: 1, aritmética: 2, acceso a memoria: 10, paso de bucle: 5, llamada: 20, crecimiento de memoria: 100/página). Si el gas se agota, la ejecución se detiene deterministamente con `ErrGasExhausted`, rompiendo bucles infinitos sin colgar el hilo del agente.
3. **Aislamiento de Pánicos y Fallos**: La ejecución del huésped se envuelve en lógica de recuperación diferida (`defer func() { if r := recover(); ... }`). Los fallos graves (p. ej., desreferencias de puntero nulo SIGSEGV simuladas o pánicos en tiempo de ejecución) son interceptados, generando un `CrashReport` aislado con seguimiento de pila mientras el agente host sigue atendiendo tráfico sin interrupción.
4. **Registro de Caídas y Cuarentena Progresiva**: Un búfer anular circular seguro para subprocesos retiene los últimos 500 informes de caídas con marcas de tiempo, motivos y estados de memoria. Si un inquilino supera su umbral configurado de caídas consecutivas (por defecto: 5), se coloca automáticamente en estado `QUARANTINED` hasta que un operador emita un restablecimiento.
5. **Limpieza por Puesta a Cero (Zeroization)**: Al desmontar o restablecer una instancia, todos los fragmentos de memoria lineal se borran con bytes cero (`subtle.ConstantTimeCompare`), evitando la remanencia de datos entre inquilinos y la fuga de tokens.

### Uso y Comandos de Ejemplo

```bash
# 1. Inspeccionar estado del supervisor de sandbox y métricas agregadas
curl http://localhost:9081/api/sandbox/status

# 2. Ejecutar una carga de cómputo en el sandbox Wasm del huésped
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "compute",
    "gas_budget": 100000
  }'

# 3. Probar trampa determinista de bucle infinito (se detiene por falta de gas)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "infinite_loop",
    "gas_budget": 5000
  }'

# 4. Probar cumplimiento del límite físico de 32MB (rechazo por OOM)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "memory_alloc",
    "requested_pages": 600
  }'

# 5. Probar aislamiento de pánico/fallo del huésped (host 100% estable)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "panic_fault"
  }'

# 6. Listar informes recientes de caídas y seguimientos de pila recuperados
curl "http://localhost:9081/api/sandbox/crashes?limit=10"

# 7. Registrar o actualizar política personalizada de cuota de sandbox
curl -X POST http://localhost:9081/api/sandbox/tenants \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "max_memory_pages": 256,
    "gas_limit": 2000000,
    "max_crash_threshold": 5
  }'

# 8. Restablecer instancia y limpiar cuarentena
curl -X POST http://localhost:9081/api/sandbox/reset \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant-edge-prod", "instance_id": "inst-1"}'
```

---

## 📡 Referencia de API REST

| Endpoint | Método | Autenticación | Descripción |
| :--- | :---: | :---: | :--- |
| `/api/sandbox/status` | `GET` | Público | Estado del supervisor de sandbox, instancias activas, OOM y recuperaciones de pánico |
| `/api/sandbox/execute` | `POST` | Público | Ejecuta cargas de trabajo Wasm huéspedes bajo estrictos límites de memoria y gas |
| `/api/sandbox/tenants` | `GET/POST` | Público | Inspecciona o actualiza políticas de aislamiento y cuotas de sandbox de inquilinos |
| `/api/sandbox/crashes` | `GET` | Público | Registro anular de caídas que lista fallos capturados y seguimientos de pila |
| `/api/sandbox/crashes/clear` | `POST` | Público | Vacía el búfer anular del registro de caídas en memoria |
| `/api/sandbox/reset` | `POST` | Público | Restablece instancias caídas o en cuarentena al estado IDLE listo |
| `/api/steering/status` | `GET` | Público | Estado de despacho eBPF HTTP/3, modo activo y estadísticas de rendimiento |
| `/api/steering/endpoints` | `GET/POST/DELETE` | Público | Lista, registra o elimina endpoints de workers ascendentes para despacho directo |
| `/api/steering/rules` | `GET/POST/DELETE` | Público | Lista, añade o elimina reglas de enrutamiento por prefijo de ID de conexión QUIC |
| `/api/steering/simulate` | `POST` | Público | Simula inspección de cabeceras UDP/QUIC y mide latencia de despacho en microsegundos |
| `/api/steering/stats` | `GET` | Público | Telemetría detallada de despacho en microsegundos y ratio sub-10µs |
| `/api/rotator/status` | `GET` | Público | Estado del rotador, tokens activos, renovaciones y latencia de propagación de malla |
| `/api/rotator/tokens` | `GET` | Público | Lista todos los tokens activos bajo gestión de rotación |
| `/api/rotator/renew` | `POST` | Público | Activa rotación inmediata y renovación de claves de tokens |
| `/api/rotator/revoke` | `POST` | Público | Emite revocación instantánea de tokens/arrendamientos en la malla gossip |
| `/api/rotator/gossip` | `POST` | Público | Intercambio gossip entre pares para lápidas y membresía de malla |
| `/api/rotator/delegate` | `POST` | Público | Actualización delegada de tokens que requiere atestación TPM de hardware |
| `/api/rotator/challenge` | `GET` | Público | Genera nonce de desafío de un solo uso para atestación de hardware |
| `/api/rotator/tombstones` | `GET` | Público | Lista lápidas de revocación firmadas en el libro contable local |
| `/api/rotator/peers` | `GET/POST` | Público | Lista pares de malla o registra un nuevo nodo par gossip |
| `/api/rotator/events` | `GET` | Público | Registro de auditoría de eventos recientes de rotación y revocación |
| `/api/radar/status` | `GET` | Público | Estado del radar, PoPs monitoreados, latencia global y recuentos de migración |
| `/api/radar/pops` | `GET` | Público | Lista PoPs perimetrales monitoreados con estadísticas (`?region=...`) |
| `/api/radar/probe` | `POST` | Público | Activa sondeo sintético para un PoP específico (`?colo=...`) o todos |
| `/api/radar/route` | `GET/POST` | Público | Resolutor de rutas Anycast submilisegundo (`?lat=...&lon=...&country=...`) |
| `/api/radar/events` | `GET` | Público | Anomalías recientes de rutas BGP y diarios de migración autónoma |
| `/api/radar/migrate` | `POST` | Público | Evacua arrendamientos activos de un PoP degradado específico |
| `/api/billing/summary` | `GET` | Público | Resumen de costos del clúster, ahorros y agregados por inquilino/cuenta |
| `/api/billing/ledger` | `GET` | Público | Exportación de libro (`?format=json|csv|jsonl|opencost&tenant=...&limit=...`) |
| `/api/billing/meter` | `POST` | Público | Ingiere y atribuye registros de consumo de recursos |
| `/api/billing/optimize` | `POST` | Público | Activa evaluación de optimización para migración autónoma de cargas |
| `/api/billing/migrations` | `GET` | Público | Inspecciona historial reciente de migraciones autónomas y ahorros totales |
| `/api/billing/rates` | `GET/POST` | Público | Consulta o actualiza tarifas de precios oficiales de Cloudflare en microcentavos |
| `/api/billing/reset` | `POST` | Público | Limpia el libro de facturación en memoria y los datos agregados |
| `/api/mtls/status` | `GET` | Público | Inspecciona estadísticas de CA Raíz activa, política y atestación TPM 2.0 |
| `/api/mtls/ca/cert` | `GET` | Público | Descarga el certificado público de la CA Raíz en formato PEM |
| `/api/mtls/ca/issue` | `POST` | Público | Emite certificados efímeros de cliente/pares con SANs y validez personalizada |
| `/api/mtls/verify` | `POST` | Público | Evalúa un certificado PEM X.509 contra la CA activa y política de seguridad |
| `/api/mtls/revoke` | `POST` | Público | Revoca un número de serie de certificado y lo añade a la lista negra CRL |
| `/api/mtls/tpm/attest` | `GET` | Público | Consulta cotización de proveedor TPM 2.0 Windows y almacén de certificados |
| `/api/mtls/peer-token` | `POST` | Público | Genera token de pares firmado con ECDSA para sincronización interclúster |
| `/api/mtls/peer-token/verify` | `POST` | Público | Verifica criptográficamente el token de pares de sincronización interclúster |
| `/api/mtls/policy` | `POST/PUT` | Público | Actualiza la política de verificación de certificados de cliente activa |
| `/raft/status` | `GET` | Público | Inspecciona rol de nodo local, término, ID líder, índice commit y telemetría |
| `/raft/peers` | `GET/POST` | Público | Inspecciona pares del clúster y latencia; registra nodos remotos manualmente |
| `/raft/propose` | `POST` | Público | Propone comandos (`GRANT_LEASE`, `REVOKE_LEASE`, `UPDATE_QUOTA`, `SYNC_ACCOUNT`) |
| `/raft/journal` | `GET` | Público | Instantánea de todas las entradas WAL replicadas con sumas SHA-256 |
| `/raft/vote` | `POST` | Público | Endpoint RPC de votación para elecciones Raft de candidatos |
| `/raft/append` | `POST` | Público | Endpoint RPC de latidos Raft y replicación de entradas de registro |
| `/metrics` | `GET` | Público | Endpoint estándar de métricas Prometheus 0.0.4 en texto plano |
| `/api/compiler/compile` | `POST` | Público | Compila y minifica código JS de Worker, retornando ratio y estado sintáctico |
| `/api/compiler/workers` | `GET` | Público | Lista paquetes de workers desplegados y sus hashes de contenido SHA-256 |
| `/api/compiler/workers` | `POST` | Público | Empaqueta y despliega un nuevo paquete de Worker perimetral para un inquilino |
| `/api/compiler/workers` | `DELETE` | Público | Despliega o desinstala un paquete de Worker (`?id=bundle-...`) |
| `/api/compiler/routes` | `GET` | Público | Lista rutas dinámicas registradas ordenadas por prioridad descendente |
| `/api/compiler/routes` | `POST` | Público | Registra nuevo patrón de ruta dinámico (`pattern`, `zone_id`, `tenant_id`, `worker_id`) |
| `/api/compiler/routes` | `DELETE` | Público | Elimina enlace de ruta dinámico (`?id=route-...`) |
| `/api/compiler/routes/match` | `POST` | Público | Prueba coincidencia de patrones de ruta URL (`host`, `path`) |
| `/api/compiler/stream-test` | `GET` | Público | Endpoint de prueba de rendimiento de transmisión de copia cero (`?size_mb=1..50`) |
| `/api/config/status` | `GET` | **Bearer** | Telemetría de recarga en caliente y lista de cuentas saneada (tokens ocultos) |
| `/api/config/reload` | `POST` | **Bearer** | Activa recarga en caliente inmediata y sin caídas de configuración desde disco |
| `/api/quota/alerts` | `GET` | Público | Historial registrado de notificaciones de umbral de cuota |
| `/api/quota/webhooks/config` | `GET/POST` | Público | Inspecciona o actualiza URLs de webhooks de Discord/Slack |
| `/api/quota/webhooks/test` | `POST` | Público | Envía notificación de prueba de umbral de cuota (80%/95%/100%) |
| `/api/quota/probation` | `GET` | Público | Registros actuales de prueba y temporizadores para cuentas agrupadas |
| `/api/quota/probation/recover` | `POST` | Público | Restaura manualmente una cuenta desde el estado de prueba (`?id=...`) |
| `/api/quota/probation/trigger` | `POST` | Público | Simula activación de prueba por límite de tasa 429 (`?id=...`) |
| `/api/quota/ratelimit` | `GET` | Público | Telemetría del limitador de ventana deslizante (`?key=...`) |
| `/api/auth/token` | `POST` | Público / Key | Emite token portador JWT HS256 con alcance delimitado |
| `/api/auth/revoke` | `POST` | Bearer | Revoca un token JWT mediante su identificador JTI |
| `/api/auth/me` | `GET` | Bearer | Verifica e inspecciona las declaraciones activas del llamador |
| `/api/auth/status` | `GET` | Público | Inspecciona proveedores criptográficos y estado de autenticación |
| `/api/security/encrypt` | `POST` | Bearer | Cifra carga útil de secretos en envoltorio DPAPI o AES-GCM |
| `/api/security/decrypt` | `POST` | Bearer | Prueba descifrado de ida y vuelta de envoltorios de seguridad |
| `/api/security/status` | `GET` | Bearer | Inspecciona el motor de la bóveda de secretos y el proveedor activo |
| `/s3/{bucket}/{key...}` | `PUT/GET/HEAD/DELETE` | Público | Pasarela de almacenamiento S3 virtual con fragmentación en bandas |
| `/s3/{bucket}` | `GET` | Público | Listado XML S3 ListObjectsV2 para el depósito especificado |
| `/api/kv/{ns}/{key}` | `GET/PUT/DELETE` | Público | Endpoint de caché KV particionada multicuenta |
| `/api/kv/stats` | `GET` | Público | Eficiencia de caché, tasa de aciertos y métricas de partición |
| `/proxy/{service}/*` | `ANY` | Público | Proxy inverso transparente con conmutación por error e intercepción 429 |
| `/api/accounts` | `GET` | Público | Instantánea de todas las cuentas Cloudflare agrupadas y sus límites |
| `/api/leases` | `GET` | **Bearer** | Lista todos los arrendamientos de recursos activos y revocados |
| `/api/leases` | `POST` | **Bearer** | Otorga nuevo arrendamiento dinámico (`target_service`, `resource_type`) |
| `/api/leases/revoke` | `POST` | **Bearer** | Invalida y revoca inmediatamente un cupón de arrendamiento (`?id=lse_...`) |
| `/api/health` | `GET` | Público | Verifica latencia y conectividad con la API pública de Cloudflare |

---

## 🗺️ Hoja de Ruta (Roadmap)

Consulte [ROADMAP.md](../../ROADMAP.md) para conocer los objetivos de los hitos y las funciones planificadas.

---

## 🔒 Seguridad (Security)

Para divulgaciones de vulnerabilidades y procedimientos de reporte, consulte [SECURITY.md](../../SECURITY.md).

---

## 📄 Licencia (License)

Licenciado bajo la [Licencia Apache 2.0](../../LICENSE).
