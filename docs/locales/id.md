<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <a href="./zh-CN.md">🇨🇳 中文</a> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <strong>🇮🇩 Bahasa Indonesia</strong>
</p>

# ⚡ CF-Lease-Broker

**Penggabungan sumber daya multi-akun Cloudflare & broker sewa token dinamis untuk Workers, KV, dan R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-mulai-cepat)

<sub>Dibuat oleh **kLauncher1** · Penggabungan sumber daya edge ketersediaan tinggi, brankas rahasia, dan penyewaan tenant dinamis.</sub>

</div>

---

## ⚡ Fitur Utama (Highlights)

- **Rotator Token Edge Otonom (Autonomous Edge Token Rotator)**: Pemantauan siklus hidup proaktif untuk voucher sewa dan token penyewa yang memicu pembaruan latar belakang pada 80% masa berlaku, dengan validitas token ganda tanpa downtime selama masa tenggang 30 detik (`internal/rotator/rotator.go`).
- **Mesh Pencabutan Token Berbasis Gossip (<5ms)**: Buku besar tombstone terdesentralisasi dengan pencarian $O(1)$, tombstone bertanda tangan HMAC-SHA256, penomoran urutan monotonik, sinkronisasi status anti-entropi, dan siaran peer-to-peer antar node klaster dalam waktu kurang dari 5 milidetik (`internal/rotator/mesh.go`).
- **Atestasi Perangkat Keras TPM 2.0 untuk Pembaruan Terdelegasi**: Verifikasi integritas platform kriptografi untuk permintaan pembaruan terdelegasi, nonce tantangan sekali pakai untuk mencegah serangan replay, verifikasi tag HMAC waktu-konstan, dan validasi digest PCR (`internal/rotator/attest.go`).
- **Radar Kesehatan Edge Cerdas & Perutean Geografis Anycast**: Pemeriksaan sintetis bersamaan terhadap Point of Presence (PoP) Cloudflare global, pelacakan latensi rata-rata bergerak eksponensial (EMA, `alpha=0.25`), jitter, dan skor kesehatan gabungan, dipadukan dengan perutean Anycast sub-milidetik dan deteksi anomali BGP route flap (`internal/radar/`).
- **Migrasi Sewa Proaktif Otonom**: Memindahkan sewa microservice aktif secara dinamis dari PoP edge yang mengalami degradasi ke alternatif siaga yang sehat sebelum terjadi kehilangan paket data atau batas waktu soket jaringan.
- **Optimalisasi Biaya Otonom & Buku Besar Penagihan Multi-Tenant**: Pengukuran komputasi, permintaan, dan penyimpanan granular dengan presisi mikro-sen ($1.00 = 1.000.000 mikro-sen) berdasarkan tarif resmi Cloudflare di Workers, KV, dan R2, serta migrasi beban kerja otomatis dari batas berbayar ke akun tingkat gratis siaga (`internal/billing/`).
- **Ekspor Keuangan Standar OpenCost 1.0 & Multi-Format**: Ekspor jurnal penagihan bawaan dalam format RFC 4180 CSV, JSON baris baru (JSONL), dan spesifikasi standar OpenCost 1.0 (`/api/billing/ledger?format=opencost`).
- **Mutual TLS (mTLS) Zero-Trust & Verifikasi Sertifikat Klien**: Pemeriksaan rantai sertifikat klien X.509 yang ketat, verifikasi daftar pencabutan sertifikat (CRL), penegakan jendela validitas, pencocokan SAN wildcard (`*.domain.com`), dan kebijakan akses proksi terbalik upstream (`internal/mtls/verifier.go`).
- **Otoritas Sertifikat Efemeral (Ephemeral CA) & Mesh Peering**: Pembuatan sertifikat root ECDSA P-256 dalam memori tanpa dependensi eksternal, penerbitan sertifikat klien dinamis, dan token peering antarklaster yang ditandatangani secara kriptografis (`internal/mtls/ca.go`).
- **Atestasi TPM 2.0 & Penyimpanan Sertifikat Windows**: Memanfaatkan Windows CNG (`ncrypt.dll`) dan Microsoft Platform Crypto Provider untuk memverifikasi modul perangkat keras TPM 2.0 dan memeriksa penyimpanan sertifikat sistem Windows (`crypt32.dll`) (`internal/mtls/tpm_windows.go`).
- **Mesin Konsensus Raft Ketersediaan Tinggi**: Klaster konsensus multi-node dengan pengatur waktu pemilihan acak (150-300ms), pemilihan pemimpin kuorum mayoritas, jurnal Write-Ahead Log (WAL) dengan verifikasi SHA-256, dan penemuan peer dinamis melalui DNS SRV (`net.LookupSRV`) (`internal/raft/`).
- **Kompilator Virtual & Pengemasan Edge Worker**: Minifier dan validator leksikal dalam memori berkecepatan tinggi untuk skrip JavaScript/Wasm dengan penghapusan komentar, verifikasi sintaksis, dan hashing konten SHA-256 (`internal/compiler/bundle.go`).
- **Injeksi Rute Dinamis Multi-Zona**: Kompilator rute wildcard berkinerja tinggi untuk memetakan domain arbitrer (`*.domain.com/path/*`) dengan bobot prioritas dan tabel injeksi rute dinamis (`internal/compiler/router.go`).
- **Saluran Streaming Langsung Zero-Copy**: Streaming respons edge latensi rendah menggunakan buffer pool 64KB yang telah dialokasikan sebelumnya (`sync.Pool`), pembersihan fragmen langsung (`http.Flusher`), dan deteksi pemutusan klien untuk mencegah kebocoran soket (`internal/compiler/stream.go`).
- **Artefak Mandiri Multi-Arsitektur**: Saluran kompilasi silang yang menghasilkan biner mandiri untuk `linux/amd64`, `linux/arm64`, `darwin/arm64`, dan `windows/amd64` dengan verifikasi checksum SHA-256 otomatis (`scripts/build_artifacts.mjs`).
- **Pemuatan Ulang Cepat Tanpa Downtime (Hot Reloading)**: Sinkronisasi dinamis kredensial dan pool akun melalui pemantauan perubahan file dan sinyal sistem operasi (`SIGHUP`) tanpa me-restart proses.
- **Agregasi Kuota Multi-Akun**: Menggabungkan beberapa akun gratis dan berbayar Cloudflare ke dalam klaster komputasi virtual terpadu untuk melampaui batas harian akun tunggal.
- **Brankas Rahasia Produksi**: Enkripsi amplop saat istirahat menggunakan Windows DPAPI (`CryptProtectData`) dan AES-256-GCM lintas platform dengan pembersihan memori aman (`Zeroize`).
- **Autentikasi HS256 JWT & RBAC**: Mesin token HMAC-SHA256 dengan verifikasi waktu-konstan dan daftar tolak pencabutan JTI yang aman dari thread.
- **Gateway R2 Virtual Kompatibel S3**: Titik akhir protokol S3 REST (`/s3/{bucket}/{key...}`) dengan pemecahan otomatis 2MB dan verifikasi ETag komposit.
- **Cache KV Terpartisi Multi-Akun**: Lapisan penyimpanan KV thread-safe dengan hashing konsisten FNV, caching memori LRU, dan kedaluwarsa TTL.
- **Pengekspor Metrik Prometheus 0.0.4**: Titik akhir standar `/metrics` yang mengekspor jumlah sewa, kuota akun, dan telemetri proksi tanpa dependensi tambahan.
- **Pembatasan Laju Jendela Geser & Masa Percobaan**: Pelacak laju permintaan dalam memori dengan mesin status backoff eksponensial untuk akun yang terkena pembatasan.
- **Webhook Peringatan Multi-Saluran**: Notifikasi kaya untuk Discord, Slack, dan format JSON pada ambang batas 80%, 95%, dan 100%.
- **Pencegatan Kesalahan 429 & Failover Otomatis**: Mendeteksi kesalahan 429 Too Many Requests dan gangguan gateway secara real-time, mengalihkan permintaan ke akun sehat berikutnya dalam pool.
- **Proksi Terbalik Transparan**: Meneruskan lalu lintas melalui `/proxy/{service}/*` dengan percobaan ulang muatan buffer dan pembersihan header hop-by-hop (RFC 7230).
- **Penyewaan Sumber Daya Dinamis (Vouchers)**: Menerbitkan token API dengan cakupan khusus berumur pendek (`lse_...`) untuk microservice internal atau pengujian otomatis.
- **Penerapan Biner Tunggal**: Dasbor manajemen bertema Glassmorphism modern disematkan langsung ke dalam biner Go melalui `//go:embed`.
- **Jejak Memori Ringan**: Berjalan terus menerus dengan penggunaan kurang dari **12 MB RAM** tanpa dependensi runtime eksternal.

---

## 🚀 Mulai Cepat (Quick Start)

### 1. Unduh dan Jalankan

```bash
# Klon repositori
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# Jalankan langsung
go run .
```

Konsol manajemen akan aktif di:
👉 **`http://localhost:9081`**

### 2. Kompilasi Silang Artefak Mandiri Multi-Arsitektur

```bash
# Buat biner mandiri untuk linux/amd64, linux/arm64, darwin/arm64, dan windows/amd64
node scripts/build_artifacts.mjs --clean

# Verifikasi checksum kriptografi SHA-256
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Arsitektur & Penggabungan Sumber Daya (Architecture)

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

## 🔒 Manajemen Rahasia & Autentikasi

### 1. Brankas Enkripsi Amplop (`internal/security/vault.go`)
- **Integrasi Windows DPAPI**: Pada sistem Windows, rahasia dilindungi oleh Windows DPAPI (`CryptProtectData`) di tingkat sistem operasi (`dpapi:<base64>`).
- **Amplop AES-256-GCM**: Pada sistem operasi lain, data dienkripsi dengan AES-256-GCM terotentikasi menggunakan nonce acak 96-bit unik.
- **Pembersihan Memori Aman**: Buffer memori yang berisi data sensitif segera dikosongkan setelah pemrosesan menggunakan pola `Zeroize`.

### 2. Middleware JWT & RBAC (`internal/security/middleware.go`)
- **Token Standar HS256**: Mengeluarkan dan memverifikasi token HMAC-SHA256 dengan pemeriksaan waktu-konstan untuk mencegah serangan analisis waktu.
- **Daftar Pencabutan Instan**: Mendukung pembatalan token seketika berdasarkan ID JTI (`POST /api/auth/revoke`).
- **Perlindungan Rute Administratif**: Titik akhir manajemen membutuhkan tajuk otorisasi yang valid `Authorization: Bearer <token>`.

```bash
# 1. Terbitkan token JWT operator
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. Panggil titik akhir sewa terlindungi
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 Gateway R2 Virtual Kompatibel S3 (`/s3/{bucket}/{key...}`)

Broker menyediakan titik akhir yang kompatibel dengan API AWS S3:
1. **Pemecahan Objek (Striping)**: Muatan yang melebihi 2MB dibagi secara otomatis menjadi blok berurutan (`chk_<hash>_<index>`) dan disebarkan ke seluruh akun yang tersedia.
2. **Checksum Integritas**: Setiap fragmen menyertakan hash SHA-256 terpisah dan objek menerima ETag komposit standar.
3. **Permintaan Rentang Byte (HTTP Range)**: Dukungan penuh untuk tajuk `Range: bytes=start-end` untuk streaming video dan unduhan lanjutan.
4. **Daftar Objek**: Sesuai dengan format respons XML standar `ListObjectsV2`.

```bash
# 1. Simpan objek ke dalam pool R2
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. Ambil objek kembali
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. Permintaan rentang byte parsial
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf
```

---

## 🔄 Proksi Terbalik Transparan dengan Failover Otomatis (`/proxy/{service}/*`)

1. **Pemilihan Akun Optimal**: Permintaan diarahkan ke akun dengan sisa kuota terbesar (`SelectOptimalAccount`).
2. **Buffer Muatan Permintaan**: Menyimpan data permintaan hingga 10MB dalam memori agar percobaan ulang dapat berjalan tanpa kehilangan data.
3. **Pencegatan Kode 429**: Jika akun merespons dengan HTTP 429 atau kesalahan 502/503/504, akun ditandai tidak sehat dan permintaan dialihkan ke akun berikutnya.
4. **Pembersihan Header**: Menghapus header per-lompatan (hop-by-hop) sesuai standar RFC 7230 pada kedua arah.
5. **Header Telemetri**: Menambahkan header diagnostik `X-CF-Broker-Account` dan `X-CF-Broker-Attempts`.

---

## 🔄 Pemuatan Ulang Cepat Tanpa Gangguan (`internal/config/`)

1. **Pemantauan File**: Meninjau file konfigurasi secara berkala dan memvalidasi perubahan menggunakan hash SHA-256.
2. **Penanganan Sinyal POSIX**: Menangkap sinyal `SIGHUP` untuk memicu pembaruan konfigurasi sinkron seketika.
3. **Penggantian Aman**: File dengan kesalahan format JSON akan ditolak secara aman sehingga konfigurasi aktif tetap terjaga.

---

## 🚀 Kompilator Virtual Edge Worker & Injeksi Rute

1. **Minifikasi Leksikal Memori**: Menghapus komentar dan memverifikasi tanda tangan modul Cloudflare Worker.
2. **Kompilator Rute Multi-Zona**: Memetakan pola wildcard (`*.domain.com/*`) berdasarkan skor spesifisitas.
3. **Streaming Langsung Zero-Copy**: Menggunakan buffer 64KB dan `http.Flusher` untuk transfer data instan dengan jejak memori minimal.

---

## 🤝 Konsensus Raft Terdistribusi Ketersediaan Tinggi

1. **Pemilihan Pemimpin Mayoritas**: Pengatur waktu acak antara 150 hingga 300ms dan heartbeat setiap 50ms.
2. **Jurnal Write-Ahead Log (WAL)**: Pengindeksan monotonik dengan verifikasi integritas hash SHA-256.
3. **Penemuan Peer DNS SRV**: Menemukan anggota klaster secara dinamis melalui catatan RFC 2782 (`net.LookupSRV`).
4. **FSM Broker**: Replikasi deterministik perintah `GRANT_LEASE`, `REVOKE_LEASE`, `SYNC_ACCOUNT`, dan `UPDATE_QUOTA`.

---

## 🛡️ Keamanan mTLS & Atestasi Perangkat Keras TPM 2.0

1. **Otoritas Sertifikat Efemeral**: Pembuatan kunci root ECDSA P-256 dalam memori dan penerbitan sertifikat klien secara langsung.
2. **Verifikasi Sertifikat & Kebijakan SAN**: Memeriksa sertifikat X.509 terhadap aturan rantai kepercayaan dan batas validitas.
3. **Atestasi Perangkat Keras TPM 2.0**: Memeriksa platform Windows CNG `ncrypt.dll` untuk menghasilkan kutipan pengukuran PCR.

---

## 💰 Optimasi Biaya Otonom & Pembukuan Tenant

1. **Perhitungan Presisi Mikro-Sen**: Pelacakan biaya akurat ($1.00 = 1.000.000 mikro-sen) untuk menghindari bias pembulatan.
2. **Pengalihan Beban Kerja Otomatis**: Mengarahkan lalu lintas ke akun gratis cadangan saat mendekati batas akun berbayar.
3. **Ekspor OpenCost 1.0 & CSV/JSONL**: Menyediakan laporan pembukuan standar untuk audit keuangan.

---

## 🛰️ Radar Kesehatan Edge & Perutean Anycast

1. **Pemeriksaan Sintetis Berkala**: Perhitungan latensi EMA (`alpha=0.25`) dan pemantauan jitter jaringan.
2. **Perutean Geografis Anycast Sub-Milidetik**: Menggunakan rumus Haversine untuk memilih PoP terdekat dengan afinitas tenant.
3. **Deteksi Anomali BGP**: Memindahkan sewa aktif secara proaktif sebelum koneksi jaringan terputus.

---

## 🔄 Rotator Token Otonom & Mesh Pencabutan Gossip

1. **Pembaruan Proaktif**: Memperbarui token pada 80% masa pakai dengan masa tenggang transisi 30 detik.
2. **Penyebaran Berbasis Gossip (<5ms)**: Penyiaran tombstone pencabutan ke seluruh klaster dalam waktu kurang dari 5ms.
3. **Pembaruan Terdelegasi TPM 2.0**: Verifikasi nonce tantangan dan digest PCR untuk permintaan pembaruan terdelegasi.

---

## 📡 Referensi REST API

| Titik Akhir | Metode | Autentikasi | Deskripsi |
| :--- | :---: | :---: | :--- |
| `/api/rotator/status` | `GET` | Publik | Status rotator, token aktif, dan latensi propagasi mesh |
| `/api/rotator/tokens` | `GET` | Publik | Daftar seluruh token yang berada di bawah pengelolaan rotasi |
| `/api/rotator/renew` | `POST` | Publik | Meminta rotasi langsung dan pembaruan kunci token |
| `/api/rotator/revoke` | `POST` | Publik | Menyiarkan pencabutan token instan melalui mesh gossip |
| `/api/radar/status` | `GET` | Publik | Status mesin radar, PoP yang dipantau, dan latensi global |
| `/api/radar/route` | `GET/POST`| Publik | Penentu rute geografis Anycast sub-milidetik |
| `/api/billing/summary` | `GET` | Publik | Ringkasan biaya klaster dan total penghematan yang dicapai |
| `/api/billing/ledger` | `GET` | Publik | Unduh buku besar penagihan (`?format=json\|csv\|jsonl\|opencost`) |
| `/api/mtls/status` | `GET` | Publik | Status CA efemeral, kebijakan mTLS, dan laporan TPM 2.0 |
| `/api/mtls/ca/cert` | `GET` | Publik | Unduh sertifikat publik root CA dalam format PEM |
| `/api/mtls/ca/issue` | `POST` | Publik | Penerbitan sertifikat klien sementara dengan opsi SAN |
| `/api/mtls/verify` | `POST` | Publik | Validasi sertifikat X.509 terhadap kebijakan keamanan |
| `/raft/status` | `GET` | Publik | Status node Raft lokal, peran klaster, dan indeks jurnal |
| `/metrics` | `GET` | Publik | Pengikis metrik standar format Prometheus 0.0.4 |
| `/api/compiler/compile`| `POST` | Publik | Kompilasi, analisis sintaksis, dan minifikasi kode Worker |
| `/s3/{bucket}/{key...}`| `PUT/GET` | Publik | Titik masuk penyimpanan objek virtual kompatibel AWS S3 |
| `/proxy/{service}/*` | `ANY` | Publik | Proksi terbalik transparan dengan pencegatan kode 429 |
| `/api/accounts` | `GET` | Publik | Ringkasan akun Cloudflare yang digabungkan dan sisa kuota |
| `/api/leases` | `GET/POST` | **Bearer** | Melihat daftar sewa aktif atau memberikan sewa baru |
| `/api/leases/revoke` | `POST` | **Bearer** | Membatalkan dan mencabut voucher sewa seketika |
| `/api/health` | `GET` | Publik | Memeriksa konektivitas ke API publik Cloudflare |

---

## 🗺️ Peta Jalan Pengembangan (Roadmap)

Silakan merujuk ke dokumen [ROADMAP.md](../../ROADMAP.md) untuk melihat tahapan pengembangan selanjutnya.

---

## 🔒 Keamanan (Security)

Untuk prosedur pelaporan kerentanan secara bertanggung jawab, silakan lihat [SECURITY.md](../../SECURITY.md).

---

## 📄 Lisensi (License)

Didistribusikan di bawah lisensi [Apache License 2.0](../../LICENSE).
