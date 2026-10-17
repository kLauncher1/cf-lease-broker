<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <a href="./zh-CN.md">🇨🇳 中文</a> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <a href="./fr.md">🇫🇷 Français</a> · <strong>🇮🇩 Bahasa Indonesia</strong>
</p>

# ⚡ CF-Lease-Broker

**Penggabungan sumber daya multi-akun Cloudflare dan pialang sewa token dinamis untuk Workers, KV, dan R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-mulai-cepat)

<sub>Dikembangkan oleh **kLauncher1** · Penggabungan sumber daya tepi ketersediaan tinggi, brankas rahasia, dan sewa penyewa dinamis.</sub>

</div>

---

## ⚡ Sorotan Utama (Highlights)

- **Sandbox Wasm Tepi & Batas Fisik Memori 32MB**: Memori linear WebAssembly terkelola yang aman dipartisi dalam halaman 64 KiB (`WasmPageSize = 65536`), memberlakukan batas fisik ketat 32 MiB (`HardCeilingMemoryPages = 512`), perlindungan akses di luar batas, pembersihan nol (Zeroization) saat pelepasan, dan pelacakan lonjakan alokasi (`internal/sandbox/memory.go`).
- **Akuntansi Gas Deterministik & Penghentian Loop**: Anggaran eksekusi per instruksi dengan pengukuran gas deterministik dan penghitungan langkah, segera menghentikan loop tak terbatas setelah anggaran gas habis tanpa membekukan thread broker host (`internal/sandbox/gas.go`).
- **Isolasi Kegagalan Host & Pemulihan Toleran Panik**: Pemulihan tertunda membungkus eksekusi beban kerja tamu, menangkap kegagalan (pelanggaran memori SIGSEGV, kepanikan runtime yang tidak tertangani, perangkap kehabisan gas, dan OOM) menjaga broker host 100% stabil, didukung buffer cincin sirkular (hingga 500 laporan terbaru) dan ambang batas karantina progresif (`internal/sandbox/engine.go`).
- **Perutean Paket HTTP/3 eBPF Tanpa Beban Tambahan**: Pengiriman soket tingkat kernel melewati tumpukan TCP ruang pengguna melalui eBPF sockmap dan hook XDP di Linux, mencapai latensi pengiriman di bawah 10 mikrodetik (< 10µs, terukur ~0.26µs) untuk beban kerja proksi tepi (`internal/steering/engine.go`).
- **Inspeksi Header Paket UDP/QUIC RFC 9000**: Inspeksi tanpa alokasi heap yang mengurai header QUIC Long (Initial, 0-RTT, Handshake, Retry) dan Short (1-RTT), mengekstrak ID koneksi tujuan dan sumber (DCID/SCID), serta merutekan lalu lintas langsung ke titik akhir Workers optimal berdasarkan aturan prefiks CID dan koneksi paling sedikit (`internal/steering/quic_parser.go`).
- **Bypass Kernel Lintas Platform & Fallback Ruang Pengguna**: Arsitektur lintas platform yang menyediakan hook eBPF sockmap asli di Linux dan pengiriman cepat salinan nol di ruang pengguna pada Windows dan macOS dengan latensi sub-mikrodetik dan cakupan uji 100% (`internal/steering/`).
- **Pemutar Token Tepi Otonom**: Pemantauan proaktif siklus hidup voucher sewa dan token penyewa yang memicu pembaruan latar belakang saat mencapai 80% masa berlaku, dengan validitas token ganda tanpa downtime selama masa tenggang 30 detik (`internal/rotator/rotator.go`).
- **Jaring Pencabutan Token Berbasis Gossip Sub-5ms**: Buku besar nisan pencabutan terdesentralisasi dengan pencarian $O(1)$, nisan bertanda tangan HMAC-SHA256, penomoran urutan monoton, sinkronisasi status anti-entropi, dan penyebaran peer-to-peer di bawah 5ms ke seluruh klaster (`internal/rotator/mesh.go`).
- **Atestasi Perangkat Keras TPM 2.0 untuk Pembaruan Didelegasikan**: Verifikasi integritas platform kriptografis untuk permintaan pembaruan token yang didelegasikan, nonce tantangan sekali pakai mencegah serangan pemutaran ulang, validasi tag HMAC waktu konstan, dan verifikasi ringkasan PCR (`internal/rotator/attest.go`).
- **Radar Kesehatan Tepi Cerdas & Perutean Geo Anycast**: Penyelidikan sintetis bersamaan dari PoP global Cloudflare, pelacakan latensi rata-rata bergerak eksponensial (EMA, `alpha=0.25`), jitter, dan skor kesehatan komposit yang dikombinasikan dengan perutean Anycast sub-milidetik (< 1ms) dan deteksi anomali BGP (`internal/radar/`).
- **Migrasi Sewa Otonom Pencegahan**: Mentransfer sewa aktif layanan mikro secara dinamis dari PoP tepi yang terdegradasi ke alternatif siaga yang sehat sebelum terjadi kehilangan paket atau batas waktu soket.
- **Optimalisasi Biaya Otonom & Buku Besar Penagihan Multi-Penyewa**: Pengukuran terperinci komputasi, permintaan, dan penyimpanan dengan atribusi biaya mikro-sen ($1.00 = 1.000.000 mikro-sen) berdasarkan tarif resmi Cloudflare untuk Workers, KV, dan R2, bersama dengan migrasi beban kerja otonom ke akun gratis siaga untuk menghindari pembengkakan biaya (`internal/billing/`).
- **Ekspor Keuangan Multi-Format & OpenCost 1.0**: Dukungan bawaan ekspor jurnal penagihan dalam RFC 4180 CSV, JSONL, dan format standar OpenCost 1.0 (`/api/billing/ledger?format=opencost`) untuk rekayasa keuangan tepi.
- **Mutual TLS Zero-Trust (mTLS) & Verifikasi Sertifikat Klien**: Verifikasi rantai kepercayaan sertifikat klien X.509 yang komprehensif, pemeriksaan daftar pencabutan (CRL), penegakan jendela validitas, pencocokan SAN wildcard (`*.domain.com`), dan kebijakan akses penerbit/CN untuk rute proksi terbalik (`internal/mtls/verifier.go`).
- **Otoritas Sertifikat Efemeral (CA) & Jaring Peering**: Pembuatan root CA ECDSA P-256 dalam memori tanpa dependensi eksternal, penerbitan dinamis sertifikat klien, pelacakan nomor seri CRL, dan token peering antarklaster yang ditandatangani secara kriptografis (`internal/mtls/ca.go`).
- **Atestasi Perangkat Keras TPM 2.0 & Penyimpanan Sertifikat Windows**: Memanfaatkan Windows Cryptography Next Generation (CNG) `ncrypt.dll` untuk mengueri keberadaan TPM 2.0 di Microsoft Platform Crypto Provider, memeriksa penyimpanan sertifikat sistem (`crypt32.dll`), dan menghasilkan pengukuran PCR deterministik (`internal/mtls/tpm_windows.go`).
- **Mesin Konsensus Raft Ketersediaan Tinggi**: Klaster konsensus Raft multi-node dengan batas waktu pemilihan acak (150-300ms), pemilihan pemimpin kuorum mayoritas, jurnal Write-Ahead Log (WAL) dengan pengindeksan monoton dan verifikasi SHA-256, serta penemuan peer dinamis melalui DNS SRV (`net.LookupSRV`) tanpa dependensi eksternal (`internal/raft/`).
- **Kompilator Virtual & Pembundel Workers Tepi**: Pengecil leksikal dan validator dalam memori yang sangat cepat untuk bundel JavaScript/Wasm dengan penghapusan komentar, verifikasi sintaksis, dan hash konten SHA-256 (`internal/compiler/bundle.go`).
- **Injeksi Rute Dinamis Multi-Zona**: Kompilator rute wildcard berkinerja tinggi yang memetakan domain arbitrer (`*.domain.com/path/*`) dengan pembobotan prioritas dan tabel injeksi rute dinamis (`internal/compiler/router.go`).
- **Saluran Streaming Langsung Salinan Nol**: Penerusan respons tepi latensi rendah menggunakan kumpulan buffer 64KB yang telah dialokasikan sebelumnya (`sync.Pool`), pembilasan soket langsung (`http.Flusher`), dan deteksi pemutusan klien (`internal/compiler/stream.go`).
- **Artefak Mandiri Multi-Arsitektur**: Pipeline kompilasi silang yang mengemas file biner mandiri yang dioptimalkan untuk `linux/amd64`, `linux/arm64`, `darwin/arm64`, dan `windows/amd64` dengan pembuatan checksum SHA-256 otomatis (`scripts/build_artifacts.mjs`).
- **Pemuatan Ulang Panas Tanpa Downtime**: Pemuatan ulang konfigurasi dan kredensial dinamis melalui pengawasan file dan sinyal OS (`SIGHUP` pada POSIX) yang menyinkronkan penyewa yang dikumpulkan dan brankas rahasia tanpa memulai ulang proses.
- **Agregasi Kuota Multi-Akun**: Menggabungkan beberapa akun Cloudflare gratis dan berbayar ke dalam satu klaster komputasi virtual terpadu, melampaui batas permintaan harian akun individual.
- **Brankas Rahasia Siap Produksi**: Enkripsi amplop saat diam melalui Windows DPAPI (`CryptProtectData`) dan AES-256-GCM lintas platform dengan pembersihan memori yang aman (`Zeroize`).
- **Autentikasi HS256 JWT & RBAC**: Mesin autentikasi administratif dan layanan mikro dengan token HMAC-SHA256, verifikasi waktu konstan, dan daftar blokir JTI yang aman dari thread.
- **Gateway R2 Virtual Kompatibel S3**: Titik akhir dengan protokol REST S3 (`/s3/{bucket}/{key...}`) yang mendistribusikan penyimpanan objek di seluruh bucket R2 gabungan dengan pemotongan otomatis 2MB dan pemeriksaan integritas SHA-256.
- **Cache KV Terpartisi Multi-Akun**: Lapisan penyimpanan KV terpartisi yang aman untuk thread dengan hashing konsisten FNV, caching LRU, dan pengusiran TTL.
- **Pengekspor Metrik Prometheus 0.0.4**: Titik akhir standar `/metrics` yang mengekspor jumlah sewa, kuota penyewa, penolakan pembatasan laju, dan telemetri proksi tanpa dependensi eksternal.
- **Pembatas Laju Jendela Geser & Masa Percobaan Otomatis**: Pelacak laju dalam memori berbasis jendela geser dengan toleransi lonjakan dan mesin status masa percobaan dengan backoff eksponensial.
- **Webhook Kuota Multi-Saluran**: Pemberitahuan kaya di Discord, Slack Block Kit, dan generic JSON yang dipicu pada ambang batas 80% (Peringatan), 95% (Kritis), dan 100% (Habis) dengan peredaman 10 menit.
- **Pencegahan Pembatasan Laju & Failover Otomatis**: Mencegat kesalahan HTTP 429 dan gateway secara real-time, mengalihkan permintaan yang sedang berjalan ke akun sehat berikutnya di pool tanpa gangguan.
- **Proksi Terbalik Transparan**: Meneruskan lalu lintas dengan mulus melalui `/proxy/{service}/*` dengan percobaan ulang badan permintaan yang di-buffer dan penghapusan header hop-by-hop RFC 7230.
- **Sewa Sumber Daya Dinamis (Voucher)**: Menerbitkan token API berumur pendek dengan cakupan terbatas (`lse_...`) untuk layanan mikro internal, pemanggil eksternal, atau rangkaian pengujian.
- **Penyebaran Biner Tunggal**: Dasbor administrasi web estetika glassmorphism disematkan langsung ke dalam biner Go menggunakan `//go:embed`.
- **Jejak Memori Ringan**: Berjalan secara persisten di bawah **12 MB RAM** tanpa dependensi eksternal saat runtime.

---

## 🚀 Mulai Cepat

### 1. Unduh & Jalankan

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
# Buat biner mandiri untuk linux/amd64, linux/arm64, darwin/arm64, windows/amd64
node scripts/build_artifacts.mjs --clean

# Verifikasi checksum SHA-256 kriptografis
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Arsitektur & Penggabungan Sumber Daya

```text
┌────────────────────────────────────────────────────────────────────────┐
│                            cf-lease-broker                             │
│                         Eksekusi Go Tunggal                            │
├───────────────────┬───────────────────┬────────────────────────────────┤
│   internal/raft   │  internal/broker  │       internal/storage         │
│ • Inti Node Raft  │ • Pool Akun       │ • Gateway S3 Virtual (/s3/...) │
│ • Jurnal WAL      │ • Sewa Dinamis    │ • Pemotong Striped 2MB         │
│ • DNS SRV Peering │ • Penyeimbang Kwt │ • Lapisan Cache KV Terpartisi  │
│ • Sinkronisasi FSM│ • Failover Otomats│ • Webhook Multi-Saluran        │
├───────────────────┼───────────────────┼────────────────────────────────┤
│   internal/mtls   │ internal/security │       internal/compiler        │
│ • CA Efemeral     │ • Windows DPAPI   │ • Minifier Virt. JS / Wasm     │
│ • Cert X509 Klien │ • AES-256-GCM     │ • Kompilator Rute Multi-Zona   │
│ • Atestasi TPM    │ • HS256 JWT/RBAC  │ • Pipa Aliran Zero-Copy 64KB   │
│ • Token Peering   │ • Pembersihan Mem │ • Repositori Bundel Penyewa    │
├───────────────────┴───────────────────┴────────────────────────────────┤
│                         internal/quota & config                        │
│  • Prometheus (/metrics) • Jendela Geser • Muat Ulang Panas            │
├────────────────────────────────────────────────────────────────────────┤
│                         Mesin HTTP net/http                            │
│   • /api/mtls/* (Cert/Verif/TPM)       • /api/compiler/*               │
│   • /raft/* (Vote/Append/Status)       • /proxy/{svc}/* (Guarded mTLS) │
│   • /metrics         • /api/auth/*     • /s3/{bkt}/{key}               │
│   • /api/accounts    • /api/security   • /api/config/* (Guarded Auth)  │
├────────────────────────────────────────────────────────────────────────┤
│                       Dasbor Web Tersemat                              │
│                    (UI Glassmorphism via //go:embed)                   │
│                    web/dist/index.html -> Port 9081                    │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 🔒 Manajemen Rahasia & Autentikasi

`cf-lease-broker` menyediakan penyimpanan rahasia tingkat perusahaan dan autentikasi siap pakai tanpa dependensi pihak ketiga:

### 1. Brankas Enkripsi Amplop (`internal/security/vault.go`)
- **Integrasi Windows DPAPI**: Pada sistem Windows, rahasia dilindungi melalui Windows DPAPI (`CryptProtectData`), mengamankan kunci API dengan kredensial tingkat OS (`dpapi:<base64>`).
- **Amplop AES-256-GCM**: Pada platform selain Windows (atau saat ditentukan), rahasia dienkripsi menggunakan AES-256-GCM terotentikasi dengan nonce acak 96-bit yang unik (`aes-gcm:<nonce>:<ciphertext>`).
- **Pembersihan Memori**: Buffer byte sensitif segera dihapus setelah digunakan menggunakan pola pembersihan memori `Zeroize` (`for i := range b { b[i] = 0 }`).

### 2. Middleware JWT & RBAC (`internal/security/middleware.go`)
- **Token Standar HS256**: Menerbitkan dan memverifikasi JWT HMAC-SHA256 dengan verifikasi tanda tangan waktu konstan (`crypto/subtle.ConstantTimeCompare`) untuk mencegah serangan waktu saluran samping.
- **Daftar Penolakan Pencabutan**: Mendukung pencabutan token langsung berdasarkan JTI (`POST /api/auth/revoke`) dengan pembersihan kedaluwarsa TTL otomatis.
- **API Administratif Terlindungi**: Titik akhir administratif (`/api/leases`, `/api/leases/revoke`, dan `/api/security/*`) memerlukan header `Authorization: Bearer <token>` yang valid atau kunci bootstrap admin root.

```bash
# 1. Terbitkan Token JWT Operator
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. Panggil Titik Akhir Sewa Terlindungi dengan Token Bearer
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 Gateway R2 Virtual Kompatibel S3 (`/s3/{bucket}/{key...}`)

Broker mengekspos titik akhir standar yang kompatibel dengan AWS S3 yang memungkinkan SDK S3, layanan mikro, dan alat CLI menyimpan objek di seluruh akun Cloudflare R2 gabungan tanpa melebihi kuota penyimpanan penyewa individual:

### Kemampuan Utama:
1. **Pemotongan Bergaris**: Muatan masuk yang melebihi 2MB secara otomatis dipecah menjadi blok berurutan (`chk_<hash>_<index>`) dan didistribusikan ke akun sehat di pool.
2. **Checksum Integritas**: Setiap potongan menyertakan checksum SHA-256 individual, dan objek menerima ETag komposit standar yang kompatibel dengan unggahan multi-bagian.
3. **Permintaan Rentang HTTP**: Mendukung header `Range: bytes=start-end` untuk streaming video, melanjutkan unduhan, dan pembacaan sebagian aset besar.
4. **ListObjectsV2**: Sesuai dengan format respons XML S3 standar `ListBucketResult`.

### Contoh CLI S3 & Curl:

```bash
# 1. Simpan objek di seluruh bucket R2 gabungan
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. Ambil objek
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. Permintaan rentang byte parsial (Range)
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf

# 4. Daftar objek bucket (S3 XML)
curl http://localhost:9081/s3/assets
```

---

## 🔄 Proksi Terbalik Transparan dengan Failover (`/proxy/{service}/*`)

Broker menyediakan proksi terbalik tepi transparan di `/proxy/{service}/*`. Penelepon mengarahkan permintaan HTTP mereka ke titik akhir ini tanpa perlu mengetahui akun penyewa mana yang saat ini memiliki kuota yang tersedia.

### Cara Kerja Failover:
1. **Resolusi Target**: `{service}` dan subpath permintaan masuk diselesaikan terhadap akun gabungan dengan sisa kuota tertinggi (`SelectOptimalAccount`).
2. **Buffering Muatan**: Badan permintaan di-buffer di memori (hingga 10MB) menggunakan pembaca streaming sehingga percobaan ulang dapat memutar ulang muatan penuh tanpa kehilangan data.
3. **Pencegahan Kesalahan 429 & Gateway**: Jika akun target merespons dengan HTTP `429 Too Many Requests`, `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout`, atau mengalami pemutusan koneksi:
   - Broker segera menandai penyewa tersebut sebagai tidak sehat melalui `TriggerFailover(accountID)`.
   - Permintaan secara otomatis dicoba kembali terhadap akun sehat berikutnya di pool.
4. **Sanitasi Hop-by-Hop**: Header hop-by-hop RFC 7230 (`Connection`, `Keep-Alive`, `Upgrade`, `TE`, `Trailers`, dll.) dihapus di kedua arah.
5. **Header Telemetri**: Respons yang berhasil menyertakan header inspeksi:
   - `X-CF-Broker-Account: {account_id}` (penyewa yang memenuhi permintaan)
   - `X-CF-Broker-Attempts: {count}` (jumlah upaya yang diperlukan)

---

## 🔄 Pemuatan Ulang Panas Tanpa Downtime (`internal/config/`)

`cf-lease-broker` mendukung rotasi kredensial dinamis dan pembaruan kumpulan akun tanpa memulai ulang biner atau memutuskan koneksi HTTP aktif:

### Mekanisme Pemuatan Ulang Panas:
1. **Loop Pengawas File**: Secara berkala melakukan polling pada file konfigurasi (`CF_BROKER_CONFIG_PATH` atau `config.json`) dan mengevaluasi perubahan melalui digest deterministik SHA-256 untuk menghindari pemuatan ulang palsu.
2. **Penanganan Sinyal POSIX**: Menangkap sinyal `SIGHUP` (Unix/Linux/macOS) untuk memicu validasi dan pemuatan ulang konfigurasi sinkron segera.
3. **Pertukaran Status Atomik & Keamanan Fallback**: Jika file yang dimuat ulang berisi JSON tidak valid, batas negatif, atau ID penyewa duplikat, pemuatan ulang gagal dengan aman dengan log kesalahan, menjaga konfigurasi aktif tetap utuh.
4. **Sinkronisasi Kredensial & Pool**: Penyewa yang berhasil dimuat ulang disinkronkan langsung dengan `PoolManager` (`SyncAccounts`) dan dienkripsi ke dalam `SecretVault` (`StoreCredential`).

### Format File Konfigurasi (`config.json`):
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

## 🚀 Kompilator Virtual Worker Tepi & Injeksi Rute Dinamis

Broker ini dilengkapi mesin kompilasi dan perutean terintegrasi untuk skrip Worker tepi multi-penyewa dan modul WebAssembly:

### 1. Minifikasi Leksikal & Pengemasan dalam Memori (`internal/compiler/bundle.go`)
- **Tokenizer Leksikal**: Menghapus komentar baris tunggal (`//`) dan multi-baris (`/* */`), menghilangkan spasi berlebih, menormalkan tanda baca, dan memvalidasi tanda tangan ekspor modul Cloudflare Worker (`export default { fetch(...) }`, `addEventListener('fetch')`).
- **Validasi WebAssembly**: Memverifikasi header ajaib 4-byte (`\x00asm`) dan kepatuhan versi sebelum pengemasan penyewa.
- **Hashing Konten**: Menghasilkan digest deterministik SHA-256 untuk pembuatan versi bundel penyewa dan jejak audit.

### 2. Kompilator Rute Wildcard Multi-Zona (`internal/compiler/router.go`)
- **Pemetaan Domain Arbitrer**: Memetakan titik akhir di seluruh subdomain wildcard (`*.domain.com/*`), host persis (`api.domain.com/v1/*`), atau aturan fallback global (`*/*`).
- **Trie Berbobot Prioritas**: Mengevaluasi host dan jalur permintaan masuk dengan penilaian spesifisitas (host persis > subdomain wildcard > global; prefiks jalur lebih panjang > prefiks pendek), merutekan lalu lintas secara dinamis tanpa pemuatan ulang konfigurasi.

### 3. Saluran Streaming Langsung Salinan Nol (`internal/compiler/stream.go`)
- **Buffer Pool**: Memanfaatkan irisan byte 64KB yang telah dialokasikan sebelumnya (`sync.Pool`) untuk menghilangkan alokasi heap selama transfer proksi throughput tinggi.
- **Pembilasan Soket Langsung**: Memanfaatkan `http.Flusher` untuk mengalirkan potongan respons ke klien secara real-time sambil menonaktifkan caching perantara melalui `X-Accel-Buffering: no`.
- **Deteksi Pembatalan**: Terhubung ke `context.Context` untuk segera menghentikan aliran worker hulu saat klien terputus, mencegah kebocoran soket.

---

## 🤝 Mesin Status Raft Ketersediaan Tinggi untuk Klaster Terdistribusi

`cf-lease-broker` menyematkan mesin konsensus Raft tanpa dependensi (`internal/raft/`) yang memungkinkan pengelompokan broker multi-wilayah, replikasi status sewa tanpa kehilangan data, dan failover pemimpin otomatis:

### Arsitektur Inti & Primitif Konsensus:
1. **Pemilihan Pemimpin Kuorum & Timer Acak**: Node bertransisi melintasi status Follower, Candidate, dan Leader. Batas waktu pemilihan acak (150–300ms) mencegah deadlock suara terbagi, sementara ticker detak jantung 50ms mempertahankan otoritas pemimpin di seluruh kuorum mayoritas `((N/2) + 1)`.
2. **Jurnal Write-Ahead Log (WAL) (`internal/raft/journal.go`)**: Menegakkan pengindeksan transaksi monoton dengan checksum integritas SHA-256. Konsistensi log pengikut divalidasi sebelum menambahkan, secara otomatis memotong entri konflik yang belum dikomit. Mendukung persistensi disk khusus penambahan opsional (`raft_journal.wal`).
3. **Penemuan Peer Dinamis melalui DNS SRV (`internal/raft/discovery.go`)**: Secara otomatis menyelesaikan peer klaster melalui catatan DNS SRV RFC 2782 (`_raft._tcp.<domain>`) dengan fallback elegan ke pemetaan lingkungan statis (`CF_RAFT_PEERS`) dan pendaftaran manual runtime (`POST /raft/peers`).
4. **Mesin Status Terbatas Broker (FSM) (`internal/raft/fsm_broker.go`)**: Mereplikasi perintah transaksi secara deterministik di semua node:
   - `GRANT_LEASE`: Mereplikasi voucher sewa aktif di seluruh klaster.
   - `REVOKE_LEASE`: Menyebarkan pencabutan sewa ke semua mesin peer.
   - `SYNC_ACCOUNT`: Menyinkronkan profil penyewa dan batas kuota.
   - `UPDATE_QUOTA`: Menyebarkan kenaikan permintaan pekerja harian, delta penyimpanan R2, dan jumlah pembacaan KV.
   - `NOOP`: Pemeriksaan penghalang konsensus untuk memeriksa kesiapan klaster.

### Alur Topologi Klaster:

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

### Contoh Curl Klaster Raft:

```bash
# 1. Periksa status node lokal dan topologi klaster
curl http://localhost:9081/raft/status

# 2. Kueri kesehatan peer dan latensi bolak-balik (RTT)
curl http://localhost:9081/raft/peers

# 3. Daftarkan node peer baru secara manual
curl -X POST http://localhost:9081/raft/peers \
  -H "Content-Type: application/json" \
  -d '{"id": "node-2", "address": "http://127.0.0.1:9082"}'

# 4. Usulkan transaksi mesin status ke klaster
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

# 5. Periksa entri jurnal WAL yang direplikasi dan checksum SHA-256
curl http://localhost:9081/raft/journal
```

---

## 🛡️ Mutual TLS Zero-Trust (mTLS) & Atestasi Perangkat Keras

`cf-lease-broker` menggabungkan mesin mutual TLS (mTLS) zero-trust lengkap, Otoritas Sertifikat Efemeral (CA) dinamis dalam memori, dan atestasi TPM 2.0 yang didukung perangkat keras:

### 1. Otoritas Sertifikat Efemeral & Penerbitan Dinamis (`internal/mtls/ca.go`)
- **Root Anchor dalam Memori**: Melakukan bootstrap dinamis pada root CA ECDSA P-256 (`x509.IsCA = true`) tanpa alat eksternal atau dependensi OpenSSL.
- **Sertifikat Klien & Peer**: Menerbitkan sertifikat klien dan peer X.509 berumur pendek yang ditandatangani secara kriptografis dengan Common Name, jendela validitas, dan SAN kustom.
- **Pencabutan CRL**: Memelihara Daftar Pencabutan Sertifikat (CRL) yang cepat dan aman dari thread yang diindeks oleh nomor seri heksadesimal dengan pencarian $O(1)$.
- **Voucher Peering Bertanda Tangan**: Menghasilkan dan memvalidasi token peering ECDSA-SHA256 untuk komunikasi klaster antar-broker zero-trust.

### 2. Verifikasi Sertifikat Klien & Penegakan Kebijakan (`internal/mtls/verifier.go`)
- **Verifikasi Rantai Kepercayaan**: Memvalidasi rantai sertifikat klien X.509 yang masuk terhadap pool root CA (`x509.CertPool`).
- **Penegakan Kebijakan**: Menegakkan pencocokan SAN wildcard (`*.edge.cluster.local`, blok CIDR), Common Name yang diizinkan, Penerbit yang diizinkan, dan jendela kedaluwarsa yang ketat.
- **Ekstraksi Header Tepi Transparan**: Mengekstrak sertifikat dari koneksi TLS langsung (`r.TLS.PeerCertificates`) atau header penerusan tepi standar (`X-Client-Cert-Pem`, `X-Forwarded-Client-Cert`, `X-Client-Cert-Der`).
- **Perlindungan Proksi Terbalik**: Ketika `CF_REQUIRE_MTLS=true` diaktifkan, semua permintaan melalui `/proxy/{service}/*` harus menunjukkan sertifikat klien yang diverifikasi.

### 3. Atestasi Perangkat Keras TPM 2.0 (`internal/mtls/tpm_windows.go`)
- **Integrasi Windows CNG**: Mengueri Windows Cryptography Next Generation (`ncrypt.dll`) untuk "Microsoft Platform Crypto Provider" untuk memverifikasi keberadaan TPM 2.0.
- **Penyimpanan Sertifikat Sistem Windows**: Memeriksa dan memvalidasi penyimpanan sertifikat mesin/pengguna melalui `crypt32.dll` (`CertOpenSystemStoreW`).
- **Kutipan Platform**: Menghasilkan digest Platform Configuration Register (PCR) deterministik dan pengukuran kunci pengesahan untuk jejak audit.

### Contoh Curl mTLS & TPM:

```bash
# 1. Periksa status mTLS, Root CA aktif, dan status atestasi TPM
curl http://localhost:9081/api/mtls/status

# 2. Unduh sertifikat Root CA dalam format PEM
curl http://localhost:9081/api/mtls/ca/cert -o root-ca.pem

# 3. Terbitkan sertifikat klien efemeral
curl -X POST http://localhost:9081/api/mtls/ca/issue \
  -H "Content-Type: application/json" \
  -d '{
    "common_name": "worker-edge-alpha",
    "sans": ["*.edge.broker.local", "127.0.0.1"],
    "validity_hours": 24
  }'

# 4. Verifikasi sertifikat PEM X.509 terhadap kebijakan aktif
curl -X POST http://localhost:9081/api/mtls/verify \
  -H "Content-Type: application/json" \
  -d '{"certificate_pem": "-----BEGIN CERTIFICATE-----\n..."}'

# 5. Cabut nomor seri sertifikat
curl -X POST http://localhost:9081/api/mtls/revoke \
  -H "Content-Type: application/json" \
  -d '{"serial_number": "7b8a1c9d2e"}'

# 6. Kueri kutipan atestasi perangkat keras TPM 2.0
curl http://localhost:9081/api/mtls/tpm/attest
```

---

## 💰 Optimalisasi Biaya Otonom & Buku Besar Penagihan Multi-Penyewa

`cf-lease-broker` menyediakan mesin penagihan multi-penyewa berpresisi tinggi dan pengoptimal biaya yang dirancang untuk melacak konsumsi hingga mikro-sen dan mencegah kelebihan tagihan cloud secara otonom:

### 1. Atribusi Biaya Mikro-Sen Terperinci (`internal/billing/meter.go`)
- **Presisi Mikro-Sen**: Melacak penggunaan sumber daya dengan matematika bilangan bulat mikro-sen ($1.00 = 1.000.000 mikro-sen) untuk sepenuhnya menghilangkan pergeseran pembulatan floating-point.
- **Model Penetapan Harga Cloudflare Lengkap**: Telah dikalibrasi sebelumnya dengan tarif resmi Cloudflare:
  - Permintaan Workers: $0.30 per 1M permintaan (300.000 mikro-sen)
  - Durasi CPU Workers: $0.02 per 1M milidetik CPU (20.000 mikro-sen)
  - Pembacaan KV: $0.50 per 1M pembacaan (500.000 mikro-sen)
  - Penulisan KV: $5.00 per 1M penulisan (5.000.000 mikro-sen)
  - Penyimpanan R2: $0.015 per GB-bulan (15.000 mikro-sen)
  - Operasi Kelas A R2: $4.50 per 1M operasi (4.500.000 mikro-sen)
  - Operasi Kelas B R2: $0.36 per 1M operasi (360.000 mikro-sen)
  - Bandwidth Keluar: $0.00 (jaminan keluar tanpa biaya dari Cloudflare)
- **Tunjangan Tingkat Gratis Harian**: Melacak tunjangan gratis harian (100.000 permintaan Workers gratis dan 100.000 pembacaan KV gratis per akun) dengan rollover tengah malam UTC otomatis.

### 2. Migrasi Beban Kerja Otonom (`internal/billing/optimizer.go`)
- **Deteksi Kelebihan Proaktif**: Mengaudit penggunaan akun secara terus-menerus terhadap ambang batas kelebihan yang dapat dikonfigurasi (default 80%).
- **Prioritas Tingkat Gratis Siaga**: Saat akun berbayar mendekati kuota atau plafon anggaran, pengoptimal mengalihkan lalu lintas secara otonom atau menetapkan masa percobaan, mengarahkan sewa ke akun gratis siaga yang sehat.
- **Pelacakan Penghematan Kumulatif**: Mengukur dan melaporkan mikro-sen pasti yang dihemat dengan mengarahkan lalu lintas dari kelebihan tingkat berbayar.

### 3. OpenCost 1.0 & Ekspor Keuangan Multi-Format (`internal/billing/journal.go`)
- **Spesifikasi OpenCost 1.0**: Mengekspor alokasi penagihan penyewa yang secara langsung cocok dengan spesifikasi komputasi tepi OpenCost (`/api/billing/ledger?format=opencost`).
- **Ekspor CSV RFC 4180**: Menghasilkan catatan penagihan yang dipisahkan koma untuk analisis spreadsheet dan audit keuangan (`/api/billing/ledger?format=csv`).
- **JSON Dibatasi Baris Baru (JSONL)**: Mengalirkan rekaman log terstruktur untuk konsumsi ke BigQuery, Datadog, atau Elasticsearch (`/api/billing/ledger?format=jsonl`).

### Contoh Curl Penagihan & Biaya:

```bash
# 1. Kueri ringkasan biaya seluruh klaster dan rincian penyewa
curl http://localhost:9081/api/billing/summary

# 2. Ekspor buku besar penagihan dalam format JSON OpenCost 1.0
curl http://localhost:9081/api/billing/ledger?format=opencost

# 3. Unduh jurnal penagihan CSV RFC 4180
curl http://localhost:9081/api/billing/ledger?format=csv -o ledger.csv

# 4. Picu evaluasi migrasi beban kerja sesuai permintaan
curl -X POST http://localhost:9081/api/billing/optimize

# 5. Periksa riwayat migrasi otonom terkini dan total penghematan
curl http://localhost:9081/api/billing/migrations

# 6. Masukkan peristiwa penggunaan sumber daya penyewa kustom
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

## 🛰️ Radar Kesehatan Tepi Cerdas & Perutean Geo Anycast

`cf-lease-broker` memiliki mesin Radar Kesehatan Tepi otonom yang memantau PoP global Cloudflare, mendeteksi anomali jaringan sebelum kehilangan paket, dan menyelesaikan rute Anycast optimal dengan presisi sub-milidetik:

### 1. Penyelidikan Tepi Sintetis & Telemetri (`internal/radar/probe.go`)
- **Exponential Moving Average (EMA)**: Menghaluskan pengamatan penyelidikan frekuensi tinggi (`alpha=0.25`) untuk mencegah fluktuasi rute selama jitter sesaat.
- **Skor Jitter & Kehilangan Paket**: Menghitung jitter statistik dan rasio kehilangan paket, menghasilkan indeks kesehatan komposit (0-100) per PoP.
- **Pengiriman Bersamaan**: Kumpulan pekerja terbatas melakukan ping ke titik akhir tepi secara bersamaan tanpa kehabisan sumber daya.

### 2. Perutean Geo Anycast Sub-Milidetik (`internal/radar/router.go`)
- **Jarak Haversine**: Menghitung jarak bola lingkaran besar yang tepat antara koordinat klien dan lokasi PoP tepi.
- **Penilaian Komposit**: Mengoptimalkan jarak geografis, EMA latensi, jitter, dan status kesehatan operasional.
- **Afinitas Penyewa**: Mengikat sewa penyewa ke PoP regional optimal sambil mempertahankan resolusi rute sub-milidetik.

### 3. Deteksi Anomali Rute BGP & Migrasi Proaktif
- **Deteksi Flap Rute**: Menandai penyimpangan latensi yang melebihi 35ms atau 1.8x garis dasar EMA sebagai anomali kritis.
- **Evakuasi Preemptif**: Secara otomatis memigrasikan sewa aktif yang ditugaskan ke PoP yang terdegradasi ke alternatif sehat sebelum klien terputus.

### Contoh Curl Radar:

```bash
# 1. Kueri status radar dan ringkasan telemetri global
curl http://localhost:9081/api/radar/status

# 2. Buat daftar semua PoP yang dipantau di Asia Pasifik
curl "http://localhost:9081/api/radar/pops?region=Asia%20Pacific"

# 3. Selesaikan PoP tepi optimal untuk lokasi klien (Seoul, KR)
curl -X POST http://localhost:9081/api/radar/route \
  -H "Content-Type: application/json" \
  -d '{"lat": 37.5665, "lon": 126.9780, "country": "KR"}'

# 4. Picu penyelidikan sintetis bersamaan di semua PoP tepi
curl -X POST http://localhost:9081/api/radar/probe \
  -H "Content-Type: application/json" \
  -d '{"concurrency": 10}'

# 5. Periksa flap rute BGP terkini dan peristiwa migrasi otonom
curl http://localhost:9081/api/radar/events
```

---

## 🔄 Pemutar Token Tepi Otonom & Jaring Pencabutan

`cf-lease-broker` menyediakan mesin rotasi token tepi otonom, jaring pencabutan berbasis gosip berkecepatan tinggi, dan atestasi TPM 2.0 yang didukung perangkat keras untuk operasi penyegaran yang didelegasikan:

### 1. Pembaruan Token Proaktif Otonom (`internal/rotator/rotator.go`)
- **Pemicu Ambang Batas Proaktif**: Secara otomatis mengevaluasi token sewa aktif dan memicu pembaruan latar belakang saat token mencapai 80% dari masa berlakunya.
- **Masa Tenggang Serah Terima Tanpa Downtime**: Mempertahankan kunci token sebelumnya dalam pemetaan masa tenggang dalam memori (default 30 detik) tanpa kesalahan HTTP 401.
- **Sinkronisasi Voucher Sewa**: Menyinkronkan kunci token yang diperbarui dan masa kedaluwarsa yang diperpanjang dengan `internal/broker/lease.go` secara real time.

### 2. Jaring Pencabutan Gosip Sub-5ms (`internal/rotator/mesh.go`)
- **Buku Besar Nisan Terdesentralisasi**: Menyimpan nisan pencabutan yang ditandatangani secara kriptografis dengan kompleksitas pencarian $O(1)$ (`IsRevoked(id)`).
- **Perambatan Sub-5ms**: Menyiarkan nisan secara langsung ke seluruh node klaster dalam waktu kurang dari 5 milidetik.
- **Verifikasi Anti-Tamper HMAC-SHA256**: Setiap nisan ditandatangani dengan nomor urut monoton dan divalidasi dengan pemeriksaan kesetaraan waktu konstan.
- **Sinkronisasi Status Anti-Entropi**: Secara otomatis merekonsiliasi nisan yang hilang selama jabat tangan gosip antar-peer berkala.

### 3. Atestasi Perangkat Keras TPM 2.0 untuk Penyegaran Didelegasikan (`internal/rotator/attest.go`)
- **Nonce Tantangan Sekali Pakai**: Menghasilkan nonce tantangan kriptografis dengan pelacakan kedaluwarsa, secara ketat menolak serangan replay.
- **Verifikasi Integritas Platform**: Memvalidasi digest PCR dan kutipan TPM 2.0 sebelum menerbitkan token sewa yang diperbarui untuk permintaan yang didelegasikan.

### Contoh Curl Pemutar & Jaring:

```bash
# 1. Kueri ringkasan pemutar, token aktif, dan latensi jaring
curl http://localhost:9081/api/rotator/status

# 2. Buat daftar semua token yang dikelola dalam rotasi
curl http://localhost:9081/api/rotator/tokens

# 3. Picu rotasi sesuai permintaan secara manual untuk sebuah token
curl -X POST http://localhost:9081/api/rotator/renew \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "operator_manual_trigger"}'

# 4. Siarkan pencabutan instan di seluruh jaring gosip (<5ms)
curl -X POST http://localhost:9081/api/rotator/revoke \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "security_audit_invalidation"}'

# 5. Minta nonce tantangan atestasi perangkat keras sekali pakai
curl http://localhost:9081/api/rotator/challenge

# 6. Lakukan penyegaran token yang didelegasikan dengan kutipan TPM
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

# 7. Buat daftar nisan pencabutan aktif
curl http://localhost:9081/api/rotator/tombstones
```

---

## 📦 Pembatas Memori Sandbox Wasm Tepi & Isolasi Kerusakan

Beban kerja worker tepi yang berjalan dalam penyebaran broker multi-penyewa memerlukan batasan sumber daya mutlak dan isolasi kesalahan untuk mencegah skrip nakal, kebocoran memori, atau pengecualian yang tidak tertangani menurunkan performa broker host atau membocorkan data antar penyewa.

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

### Kemampuan Arsitektur Inti

1. **Batas Fisik Ketat 32 MiB**: Memori linear WebAssembly dipartisi menjadi halaman 64 KiB (`WasmPageSize = 65536`). Bahkan jika kebijakan penyewa meminta lebih banyak, broker secara ketat membatasi memori linear hingga 512 halaman (32 MiB, `HardCeilingMemoryPages`). Setiap alokasi yang melebihi batas atas ini ditolak dengan `ErrMemoryHardCeilingExceeded` dan dicatat sebagai intersepsi OOM.
2. **Akuntansi Gas Deterministik**: Instruksi mendebet bobot yang telah ditentukan (operasi dasar: 1, aritmatika: 2, akses memori: 10, langkah loop: 5, panggilan: 20, pertumbuhan memori: 100/halaman). Jika gas habis, eksekusi berhenti secara deterministik dengan `ErrGasExhausted`, memutus loop tak terbatas tanpa menggantung thread broker.
3. **Isolasi Kepanikan & Kesalahan**: Eksekusi tamu dibungkus dalam logika pemulihan tertunda (`defer func() { if r := recover(); ... }`). Kesalahan parah (misalnya, simulasi dereferensi penunjuk nol SIGSEGV atau kepanikan runtime) terperangkap, menghasilkan `CrashReport` terisolasi dengan pelacakan tumpukan sementara broker host terus melayani lalu lintas tanpa gangguan.
4. **Jurnal Kerusakan & Karantina Progresif**: Buffer cincin melingkar yang aman untuk thread menyimpan 500 laporan kerusakan terbaru dengan stempel waktu, alasan, dan status memori. Jika penyewa melebihi ambang batas kerusakan berturut-turut yang dikonfigurasi (default: 5), penyewa secara otomatis ditempatkan dalam status `QUARANTINED` hingga operator melakukan reset.
5. **Pembersihan Zeromisasi (Zeroization)**: Setelah pembongkaran atau pengaturan ulang instans, semua irisan memori linear dihapus dengan byte nol (`subtle.ConstantTimeCompare`), mencegah persistensi data antar-penyewa dan kebocoran token.

### Penggunaan & Contoh Perintah

```bash
# 1. Periksa status pengawas sandbox dan metrik agregat
curl http://localhost:9081/api/sandbox/status

# 2. Jalankan beban kerja komputasi di sandbox Wasm tamu
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "compute",
    "gas_budget": 100000
  }'

# 3. Uji perangkap loop tak terbatas (berhenti saat kehabisan gas)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "infinite_loop",
    "gas_budget": 5000
  }'

# 4. Uji penegakan batas fisik 32MB (penolakan OOM)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "memory_alloc",
    "requested_pages": 600
  }'

# 5. Uji isolasi kesalahan tamu (broker host tetap 100% stabil)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "panic_fault"
  }'

# 6. Buat daftar laporan jurnal kerusakan terkini dan pelacakan tumpukan
curl "http://localhost:9081/api/sandbox/crashes?limit=10"

# 7. Daftarkan atau perbarui kebijakan kuota sandbox penyewa kustom
curl -X POST http://localhost:9081/api/sandbox/tenants \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "max_memory_pages": 256,
    "gas_limit": 2000000,
    "max_crash_threshold": 5
  }'

# 8. Setel ulang instans dan bersihkan karantina
curl -X POST http://localhost:9081/api/sandbox/reset \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant-edge-prod", "instance_id": "inst-1"}'
```

---

## 📡 Referensi API REST

| Titik Akhir | Metode | Autentikasi | Deskripsi |
| :--- | :---: | :---: | :--- |
| `/api/sandbox/status` | `GET` | Publik | Status pengawas sandbox, instans aktif, intersepsi OOM, dan pemulihan panik |
| `/api/sandbox/execute` | `POST` | Publik | Eksekusi beban kerja Wasm tamu di bawah batas ketat memori dan pengukuran gas |
| `/api/sandbox/tenants` | `GET/POST` | Publik | Periksa atau perbarui kebijakan isolasi dan kuota sandbox penyewa |
| `/api/sandbox/crashes` | `GET` | Publik | Jurnal kerusakan cincin yang mencantumkan kesalahan dan jejak tumpukan |
| `/api/sandbox/crashes/clear` | `POST` | Publik | Bersihkan buffer cincin jurnal kerusakan dalam memori |
| `/api/sandbox/reset` | `POST` | Publik | Setel ulang instans sandbox yang rusak atau dikarantina ke status IDLE segar |
| `/api/steering/status` | `GET` | Publik | Status pengarahan eBPF HTTP/3, mode aktif, dan statistik throughput |
| `/api/steering/endpoints` | `GET/POST/DELETE` | Publik | Daftar, daftarkan, atau hapus titik akhir worker hulu untuk pengarahan langsung |
| `/api/steering/rules` | `GET/POST/DELETE` | Publik | Daftar, tambah, atau hapus aturan perutean awalan ID Koneksi QUIC |
| `/api/steering/simulate` | `POST` | Publik | Simulasikan inspeksi header paket UDP/QUIC dan ukur latensi mikrodetik |
| `/api/steering/stats` | `GET` | Publik | Telemetri pengiriman mikrodetik terperinci dan rasio sub-10µs |
| `/api/rotator/status` | `GET` | Publik | Status pemutar, token aktif, pembaruan, dan latensi propagasi jaring |
| `/api/rotator/tokens` | `GET` | Publik | Buat daftar semua token aktif di bawah manajemen rotasi |
| `/api/rotator/renew` | `POST` | Publik | Picu rotasi langsung dan pembaruan kunci token |
| `/api/rotator/revoke` | `POST` | Publik | Siarkan pencabutan instan token/sewa di seluruh jaring gosip |
| `/api/rotator/gossip` | `POST` | Publik | Titik pertukaran gosip antar-peer untuk nisan dan keanggotaan jaring |
| `/api/rotator/delegate` | `POST` | Publik | Pembaruan token yang didelegasikan memerlukan atestasi perangkat keras TPM |
| `/api/rotator/challenge` | `GET` | Publik | Hasilkan nonce tantangan sekali pakai untuk atestasi perangkat keras |
| `/api/rotator/tombstones` | `GET` | Publik | Buat daftar nisan pencabutan yang ditandatangani di buku besar lokal |
| `/api/rotator/peers` | `GET/POST` | Publik | Buat daftar peer jaring atau daftarkan node peer gosip baru |
| `/api/rotator/events` | `GET` | Publik | Log audit peristiwa rotasi dan pencabutan terkini |
| `/api/radar/status` | `GET` | Publik | Status mesin radar, PoP yang dilacak, latensi global, dan jumlah migrasi |
| `/api/radar/pops` | `GET` | Publik | Buat daftar PoP tepi yang dipantau dengan statistik telemetri (`?region=...`) |
| `/api/radar/probe` | `POST` | Publik | Picu penyelidikan sintetis untuk colo tertentu (`?colo=...`) atau semua PoP |
| `/api/radar/route` | `GET/POST` | Publik | Penyelesai rute Anycast sub-milidetik (`?lat=...&lon=...&country=...`) |
| `/api/radar/events` | `GET` | Publik | Anomali rute BGP terkini dan jurnal migrasi otonom |
| `/api/radar/migrate` | `POST` | Publik | Evakuasi sewa aktif dari colo tertentu yang terdegradasi |
| `/api/billing/summary` | `GET` | Publik | Ringkasan biaya klaster, penghematan, dan agregat penyewa/akun |
| `/api/billing/ledger` | `GET` | Publik | Ekspor buku besar (`?format=json|csv|jsonl|opencost&tenant=...&limit=...`) |
| `/api/billing/meter` | `POST` | Publik | Masukkan dan atribusikan catatan konsumsi sumber daya |
| `/api/billing/optimize` | `POST` | Publik | Picu evaluasi migrasi beban kerja otonom |
| `/api/billing/migrations` | `GET` | Publik | Periksa riwayat migrasi beban kerja terkini dan total penghematan |
| `/api/billing/rates` | `GET/POST` | Publik | Kueri atau perbarui tarif harga Cloudflare aktif dalam mikro-sen |
| `/api/billing/reset` | `POST` | Publik | Bersihkan buku besar penagihan dalam memori dan agregat |
| `/api/mtls/status` | `GET` | Publik | Periksa statistik Root CA aktif, kebijakan, dan atestasi TPM 2.0 |
| `/api/mtls/ca/cert` | `GET` | Publik | Unduh sertifikat publik Root CA dalam format PEM |
| `/api/mtls/ca/issue` | `POST` | Publik | Terbitkan sertifikat klien/peer efemeral dengan SAN dan validitas kustom |
| `/api/mtls/verify` | `POST` | Publik | Evaluasi sertifikat X.509 PEM terhadap kepercayaan CA dan kebijakan aktif |
| `/api/mtls/revoke` | `POST` | Publik | Cabut nomor seri sertifikat dan tambahkan ke daftar penolakan CRL |
| `/api/mtls/tpm/attest` | `GET` | Publik | Kueri kutipan penyedia platform TPM 2.0 Windows dan penyimpanan sertifikat |
| `/api/mtls/peer-token` | `POST` | Publik | Hasilkan token peering yang ditandatangani ECDSA untuk sinkronisasi klaster |
| `/api/mtls/peer-token/verify` | `POST` | Publik | Verifikasi token peering klaster antar-broker secara kriptografis |
| `/api/mtls/policy` | `POST/PUT` | Publik | Perbarui kebijakan verifikasi sertifikat klien aktif |
| `/raft/status` | `GET` | Publik | Periksa peran node lokal, term, ID pemimpin, indeks komit, dan telemetri |
| `/raft/peers` | `GET/POST` | Publik | Periksa peer klaster dan latensi; daftarkan node jarak jauh secara manual |
| `/raft/propose` | `POST` | Publik | Usulkan perintah (`GRANT_LEASE`, `REVOKE_LEASE`, `UPDATE_QUOTA`, `SYNC_ACCOUNT`) |
| `/raft/journal` | `GET` | Publik | Cuplikan entri jurnal WAL yang direplikasi dengan checksum SHA-256 |
| `/raft/vote` | `POST` | Publik | Titik akhir pemungutan suara pemilihan Raft untuk suara kandidat |
| `/raft/append` | `POST` | Publik | Titik akhir RPC detak jantung Raft dan replikasi entri log |
| `/metrics` | `GET` | Publik | Titik akhir pengikis metrik format teks standar Prometheus 0.0.4 |
| `/api/compiler/compile` | `POST` | Publik | Kompilasi & kecilkan sumber JS worker, kembalikan rasio dan status sintaksis |
| `/api/compiler/workers` | `GET` | Publik | Buat daftar bundel worker yang disebarkan dan hash konten SHA-256 |
| `/api/compiler/workers` | `POST` | Publik | Paketkan dan sebarkan bundel Edge Worker baru untuk penyewa |
| `/api/compiler/workers` | `DELETE` | Publik | Batalkan penyebaran bundel worker (`?id=bundle-...`) |
| `/api/compiler/routes` | `GET` | Publik | Daftar rute dinamis terdaftar yang diurutkan berdasarkan prioritas menurun |
| `/api/compiler/routes` | `POST` | Publik | Daftarkan pola rute dinamis baru (`pattern`, `zone_id`, `tenant_id`, `worker_id`) |
| `/api/compiler/routes` | `DELETE` | Publik | Hapus pengikatan rute dinamis (`?id=route-...`) |
| `/api/compiler/routes/match` | `POST` | Publik | Uji pencocokan pola rute URL (`host`, `path`) |
| `/api/compiler/stream-test` | `GET` | Publik | Titik akhir tolok ukur streaming salinan nol (`?size_mb=1..50`) |
| `/api/config/status` | `GET` | **Bearer** | Telemetri pemuatan ulang panas dan daftar akun yang disanitasi (token disembunyikan) |
| `/api/config/reload` | `POST` | **Bearer** | Memicu pemuatan ulang panas konfigurasi langsung tanpa downtime dari disk |
| `/api/quota/alerts` | `GET` | Publik | Riwayat pemberitahuan ambang batas kuota yang dicatat |
| `/api/quota/webhooks/config` | `GET/POST` | Publik | Periksa atau perbarui URL webhook Discord/Slack |
| `/api/quota/webhooks/test` | `POST` | Publik | Mengirimkan pemberitahuan ambang batas kuota uji (80%/95%/100%) |
| `/api/quota/probation` | `GET` | Publik | Catatan masa percobaan saat ini dan penghitung waktu untuk akun yang dikumpulkan |
| `/api/quota/probation/recover` | `POST` | Publik | Pulihkan akun dari masa percobaan secara manual (`?id=...`) |
| `/api/quota/probation/trigger` | `POST` | Publik | Simulasikan pemicu masa percobaan batas laju 429 (`?id=...`) |
| `/api/quota/ratelimit` | `GET` | Publik | Telemetri pembatas laju jendela geser (`?key=...`) |
| `/api/auth/token` | `POST` | Publik / Key | Terbitkan token pembawa JWT HS256 dengan cakupan terbatas |
| `/api/auth/revoke` | `POST` | Bearer | Cabut token JWT berdasarkan JTI |
| `/api/auth/me` | `GET` | Bearer | Verifikasi dan periksa klaim pemanggil yang aktif |
| `/api/auth/status` | `GET` | Publik | Periksa penyedia kriptografi dan status autentikasi |
| `/api/security/encrypt` | `POST` | Bearer | Enkripsi muatan rahasia ke dalam amplop DPAPI atau AES-GCM |
| `/api/security/decrypt` | `POST` | Bearer | Uji dekripsi amplop bolak-balik |
| `/api/security/status` | `GET` | Bearer | Periksa mesin brankas rahasia dan penyedia aktif |
| `/s3/{bucket}/{key...}` | `PUT/GET/HEAD/DELETE` | Publik | Gateway penyimpanan virtual kompatibel S3 dengan pemotongan bergaris |
| `/s3/{bucket}` | `GET` | Publik | Daftar XML ListObjectsV2 S3 untuk bucket yang ditentukan |
| `/api/kv/{ns}/{key}` | `GET/PUT/DELETE` | Publik | Titik akhir caching KV terpartisi multi-akun |
| `/api/kv/stats` | `GET` | Publik | Efisiensi cache, rasio hit, dan metrik partisi penyewa |
| `/proxy/{service}/*` | `ANY` | Publik | Proksi terbalik failover transparan dengan intersepsi 429 |
| `/api/accounts` | `GET` | Publik | Cuplikan dari semua akun Cloudflare yang dikumpulkan dan batas tersisa |
| `/api/leases` | `GET` | **Bearer** | Buat daftar semua sewa sumber daya yang aktif dan dicabut |
| `/api/leases` | `POST` | **Bearer** | Berikan sewa dinamis baru (`target_service`, `resource_type`) |
| `/api/leases/revoke` | `POST` | **Bearer** | Segera batalkan dan cabut voucher sewa (`?id=lse_...`) |
| `/api/health` | `GET` | Publik | Memverifikasi latensi dan konektivitas ke tepi API publik Cloudflare |

---

## 🗺️ Peta Jalan (Roadmap)

Lihat [ROADMAP.md](../../ROADMAP.md) untuk tujuan pencapaian dan fitur yang direncanakan.

---

## 🔒 Keamanan (Security)

Untuk pengungkapan kerentanan dan prosedur pelaporan, silakan merujuk ke [SECURITY.md](../../SECURITY.md).

---

## 📄 Lisensi (License)

Dilisensikan di bawah [Apache License 2.0](../../LICENSE).
