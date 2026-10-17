<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <a href="./zh-CN.md">🇨🇳 中文</a> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <strong>🇫🇷 Français</strong> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**Mutualisation de ressources multi-comptes Cloudflare et courtier dynamique de baux de jetons pour Workers, KV et R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-démarrage-rapide)

<sub>Conçu par **kLauncher1** · Mutualisation de ressources périphériques haute disponibilité, coffre-fort de secrets et baux dynamiques multi-locataires.</sub>

</div>

---

## ⚡ Points Forts (Highlights)

- **Rotateur Autonome de Jetons Edge**: Surveillance proactive du cycle de vie des bons de bail et des jetons locataires déclenchant des renouvellements en arrière-plan à 80% de validité, avec double validité sans interruption durant un délai de grâce de 30 secondes (`internal/rotator/rotator.go`).
- **Maillage de Révocation de Jetons via Gossip (<5ms)**: Registre décentralisé de stèles d'annulation (tombstones) avec requêtes en $O(1)$, signatures HMAC-SHA256, séquençage monotone, synchronisation anti-entropie et diffusion P2P en moins de 5 ms sur l'ensemble du cluster (`internal/rotator/mesh.go`).
- **Attestation Matérielle TPM 2.0 pour le Renouvellement Délégué**: Vérification cryptographique de l'intégrité de la plateforme pour les demandes déléguées, nonces à usage unique contre les attaques par rejeu et contrôle en temps constant des empreintes PCR (`internal/rotator/attest.go`).
- **Radar Intelligent de Santé Edge et Routage Géographique Anycast**: Sondage synthétique simultané des points de présence (PoP) Cloudflare mondiaux, suivi de la latence par moyenne mobile exponentielle (EMA, `alpha=0.25`), mesure de gigue et scores de santé, couplés à un routage Anycast sous la milliseconde et détection d'instabilités de routes BGP (`internal/radar/`).
- **Migration Préventive Autonome de Baux**: Déplacement dynamique des baux de microservices actifs depuis les PoPs en dégradation vers des relais de secours sains avant toute perte de paquets ou expiration de socket.
- **Optimisation Autonome des Coûts et Registre de Facturation Multi-Locataire**: Mesure granulaire du calcul, des requêtes et du stockage avec une précision au micro-centime ($1.00 = 1 000 000 micro-centimes) basée sur les tarifs officiels de Cloudflare, couplée au délestage automatique vers des comptes gratuits de secours en cas de risque de dépassement (`internal/billing/`).
- **Export Financier Standardisé OpenCost 1.0**: Génération native de journaux de facturation aux formats RFC 4180 CSV, JSON délimité par des retours à la ligne (JSONL) et spécification standard OpenCost 1.0 (`/api/billing/ledger?format=opencost`).
- **Sécurité Zero-Trust Mutual TLS (mTLS) et Contrôle des Certificats Clients**: Vérification rigoureuse des chaînes de certificats X.509, consultation des listes de révocation (CRL), fenêtres de validité strictes, correspondance des noms alternatifs (SAN) génériques (`*.domain.com`) et protection des routes en amont (`internal/mtls/verifier.go`).
- **Autorité de Certification (CA) Éphémère et Maillage d'Interconnexion**: Génération en mémoire et sans dépendance de certificats racine ECDSA P-256, émission dynamique de certificats clients et jetons d'appairage signés cryptographiquement (`internal/mtls/ca.go`).
- **Attestation Matérielle TPM 2.0 et Magasin de Certificats Windows**: Exploitation de Windows CNG (`ncrypt.dll`) et du Microsoft Platform Crypto Provider pour valider la puce TPM 2.0 matérielle et inspecter le magasin de certificats (`crypt32.dll`) (`internal/mtls/tpm_windows.go`).
- **Moteur de Consensus Raft Haute Disponibilité**: Cluster distribué avec minuteries d'élection aléatoires (150-300ms), quorum majoritaire, journal d'écriture anticipée (WAL) vérifié par empreinte SHA-256 et découverte dynamique via DNS SRV (`internal/raft/`).
- **Compilateur Virtuel et Empaqueteur d'Edge Workers**: Minificateur lexical et validateur en mémoire ultra-rapide pour scripts JavaScript/Wasm avec suppression des commentaires, vérification syntaxique et hachage SHA-256 du contenu (`internal/compiler/bundle.go`).
- **Injection Dynamique de Routes Multi-Zones**: Compilateur de motifs d'URL génériques (`*.domain.com/path/*`) haute performance avec calcul de spécificité et tables d'injection à chaud (`internal/compiler/router.go`).
- **Pipeline de Diffusion Directe Zero-Copy**: Diffusion de flux à très faible latence grâce à des pools de tampons de 64 Ko préalloués (`sync.Pool`), vidage immédiat des paquets (`http.Flusher`) et détection de déconnexion client (`internal/compiler/stream.go`).
- **Exécutables Autonomes Multi-Architectures**: Chaîne de compilation croisée produisant des binaires autonomes pour `linux/amd64`, `linux/arm64`, `darwin/arm64` et `windows/amd64` avec sommes de contrôle cryptographiques SHA-256 (`scripts/build_artifacts.mjs`).
- **Rechargement à Chaud Sans Interruption de Service**: Synchronisation dynamique des comptes et des secrets par surveillance des fichiers et interception de signaux système (`SIGHUP`) sans relancer le processus.
- **Agrégation de Quotas Multi-Comptes**: Mutualisation de plusieurs comptes gratuits et payants Cloudflare au sein d'un cluster virtuel unifié pour dépasser les quotas journaliers individuels.
- **Coffre-Fort de Secrets pour la Production**: Chiffrement d'enveloppe au repos utilisant Windows DPAPI (`CryptProtectData`) et AES-256-GCM multiplateforme avec nettoyage immédiat de la mémoire (`Zeroize`).
- **Authentification HS256 JWT et Gestion RBAC**: Moteur d'authentification HMAC-SHA256 avec validation en temps constant et listes de blocage par identifiant JTI.
- **Passerelle Virtuelle R2 Compatible S3**: Points de terminaison conformes au protocole AWS S3 REST (`/s3/{bucket}/{key...}`) avec découpage automatique par blocs de 2 Mo et contrôle ETag composite.
- **Cache KV Partitionné Multi-Comptes**: Couche de stockage KV thread-safe avec hachage FNV cohérent, mise en cache LRU en mémoire et politique d'expiration TTL.
- **Exportateur de Métriques Prometheus 0.0.4**: Endpoint standard `/metrics` exposant les baux en cours, les quotas de comptes et la télémétrie réseau sans aucune dépendance.
- **Limitation de Débit par Fenêtre Glissante avec Période Probatoire**: Régulation de trafic en mémoire avec machine à états et recul exponentiel pour les comptes restreints.
- **Webhooks d'Alerte Multi-Canaux**: Notifications enrichies pour Discord, Slack et au format JSON déclenchées aux seuils de 80%, 95% et 100%.
- **Interception Automatique des Erreurs 429 et Basculement Transparent**: Détection immédiate des codes 429 Too Many Requests et retransmission instantanée sur le compte opérationnel suivant.
- **Proxy Inverse Transparent**: Acheminement fluide via `/proxy/{service}/*` avec reprise de charge utile et suppression des en-têtes de saut (RFC 7230).
- **Attribution Dynamique de Baux de Ressources**: Émission de jetons d'API ciblés et éphémères (`lse_...`) pour microsservices et suites de tests.
- **Déploiement en Fichier Binaire Unique**: Tableau de bord Glassmorphism moderne intégré directement dans l'exécutable Go via `//go:embed`.
- **Empreinte Mémoire Minime**: Fonctionnement continu avec moins de **12 Mo de RAM** sans dépendance externe au runtime.

---

## 🚀 Démarrage Rapide (Quick Start)

### 1. Télécharger et Exécuter

```bash
# Cloner le dépôt
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# Lancer directement
go run .
```

La console d'administration sera accessible à l'adresse suivante :
👉 **`http://localhost:9081`**

### 2. Compilation Croisée Multi-Architectures

```bash
# Compiler les binaires autonomes pour linux/amd64, linux/arm64, darwin/arm64 et windows/amd64
node scripts/build_artifacts.mjs --clean

# Vérifier les sommes de contrôle cryptographiques SHA-256
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Architecture et Mutualisation des Ressources (Architecture)

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

## 🔒 Gestion des Secrets et Authentification

### 1. Coffre-Fort à Chiffrement d'Enveloppe (`internal/security/vault.go`)
- **Intégration Windows DPAPI**: Sous Windows, les secrets sont protégés par l'API système `CryptProtectData` (`dpapi:<base64>`).
- **Enveloppe AES-256-GCM**: Sur les autres systèmes, le chiffrement repose sur AES-256-GCM authentifié avec des nonces aléatoires de 96 bits.
- **Effacement Mémoire Sécurisé**: Les tampons de mémoire sensibles sont immédiatement remis à zéro après utilisation via le motif `Zeroize`.

### 2. Middleware JWT et Contrôle d'Accès RBAC (`internal/security/middleware.go`)
- **Jetons Conformes HS256**: Validation des signatures HMAC-SHA256 en temps constant pour bloquer les attaques temporelles.
- **Révocation Instantanée**: Invalidation immédiate par identifiant JTI (`POST /api/auth/revoke`) avec purge par TTL.
- **Protection des Interfaces Administratives**: En-tête obligatoire `Authorization: Bearer <token>` sur les endpoints sensibles.

```bash
# 1. Émettre un jeton JWT d'opérateur
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. Interroger les baux protégés avec le jeton
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 Passerelle Virtuelle R2 Compatible S3 (`/s3/{bucket}/{key...}`)

Le courtier expose des points d'accès conformes au standard AWS S3 :
1. **Découpage en Bandes**: Les fichiers dépassant 2 Mo sont automatiquement partitionnés en blocs (`chk_<hash>_<index>`) et répartis entre les comptes disponibles.
2. **Contrôle d'Intégrité**: Chaque segment génère une empreinte SHA-256 et l'objet bénéficie d'un ETag composite standard.
3. **Requêtes de Plages d'Octets (HTTP Range)**: Prise en charge de l'en-tête `Range: bytes=start-end` pour le streaming et la reprise de téléchargement.
4. **Listage d'Objets**: Compatibilité totale avec la réponse XML `ListObjectsV2`.

```bash
# 1. Stocker un objet sur le pool R2
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. Récupérer un objet
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. Requête de plage partielle
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf
```

---

## 🔄 Proxy Inverse Transparent avec Relève Automatique (`/proxy/{service}/*`)

1. **Acheminement Intelligent**: Le trafic entrant est orienté vers le compte disposant de la réserve de quota la plus importante (`SelectOptimalAccount`).
2. **Mise en Mémoire Tampon**: Rétention de la charge utile (jusqu'à 10 Mo) pour permettre des tentatives de reprise sans perte de données.
3. **Interception des Erreurs 429**: En cas de code 429 ou de panne 502/503/504, le compte est temporairement désactivé et la requête réémise vers le compte opérationnel suivant.
4. **Purge des En-têtes Intermédiaires**: Suppression bilatérale des en-têtes Hop-by-Hop conformément à la norme RFC 7230.
5. **En-têtes de Télémétrie**: Injection des en-têtes explicatifs `X-CF-Broker-Account` et `X-CF-Broker-Attempts`.

---

## 🔄 Rechargement à Chaud Sans Interruption (`internal/config/`)

1. **Surveillance des Fichiers**: Évaluation continue des empreintes SHA-256 pour éviter les rechargements intempestifs.
2. **Interception de Signaux POSIX**: Prise en compte du signal `SIGHUP` pour appliquer instantanément les modifications de configuration.
3. **Bascule Atomique Sécurisée**: Rejet sans risque de tout fichier JSON non valide, préservant la configuration active inchangée.

---

## 🚀 Compilateur Virtuel Edge Worker et Injection de Routes

1. **Minification Lexicale en Mémoire**: Retrait des commentaires et validation formelle de la structure de module Cloudflare Worker.
2. **Compilateur de Routes Multi-Zones**: Résolution des motifs génériques (`*.domain.com/*`) par ordre de priorité de spécificité.
3. **Flux Direct Zero-Copy**: Gestion des flux via des tampons de 64 Ko et `http.Flusher` pour minimiser l'impact mémoire.

---

## 🤝 Consensus Distribué Raft Haute Disponibilité

1. **Élection Majoritaire**: Délais d'élection aléatoires de 150 à 300 ms et signaux de présence toutes les 50 ms.
2. **Journal Write-Ahead Log (WAL)**: Indexation séquentielle avec vérification d'intégrité par hachage SHA-256.
3. **Résolution Dynamique DNS SRV**: Découverte automatique des membres du cluster via enregistrements RFC 2782 (`net.LookupSRV`).
4. **Machine d'États du Broker**: Réplication synchronisée des commandes `GRANT_LEASE`, `REVOKE_LEASE`, `SYNC_ACCOUNT` et `UPDATE_QUOTA`.

---

## 🛡️ Sécurité mTLS et Attestation de Plateforme TPM 2.0

1. **Autorité de Certification Éphémère**: Racine ECDSA P-256 générée en mémoire et émission à la volée de certificats clients.
2. **Validation des Certificats et Règles SAN**: Contrôle rigoureux de la chaîne de confiance et application des politiques de noms alternatifs.
3. **Attestation Matérielle Windows CNG**: Interrogation de la bibliothèque `ncrypt.dll` pour attester de la présence du TPM 2.0 et extraire les valeurs PCR.

---

## 💰 Optimisation Autonome des Coûts et Registre de Facturation

1. **Précision au Micro-Centime**: Calcul arithmétique entier ($1.00 = 1 000 000 micro-centimes) évitant tout biais d'arrondi.
2. **Bascule Préventive de Charge**: Redirection automatique vers des comptes gratuits de réserve avant facturation d'un surplus.
3. **Export OpenCost 1.0 et CSV/JSONL**: Rapports normalisés pour l'audit et l'intégration financière en entreprise.

---

## 🛰️ Radar de Santé Edge et Routage Anycast

1. **Sondage Continu des Nœuds**: Calcul de latence lissée par EMA (`alpha=0.25`) et suivi régulier de la gigue réseau.
2. **Routage Anycast Géographique Ultra-Précis**: Calcul de distance Haversine pour sélectionner le PoP le plus proche avec affinité locataire.
3. **Détection d'Anomalies de Routage BGP**: Déplacement préventif des sessions actives hors des nœuds instables avant rupture de connexion.

---

## 🔄 Rotateur Autonome de Jetons et Maillage de Révocation Gossip

1. **Renouvellement Anticipé**: Création de nouveaux jetons dès 80% de consommation de validité, avec période tampon de 30 secondes.
2. **Propagation par Gossip (<5ms)**: Notification instantanée des avis de révocation à tous les membres du réseau en moins de 5 ms.
3. **Délégation Certifiée par TPM 2.0**: Contrôle des nonces à usage unique et des mesures PCR pour toute demande de prolongation déléguée.

---

## 📡 Référence de l'API REST

| Point d'Accès | Méthode | Authentification | Description |
| :--- | :---: | :---: | :--- |
| `/api/rotator/status` | `GET` | Publique | État du rotateur, jetons gérés et temps de propagation du maillage |
| `/api/rotator/tokens` | `GET` | Publique | Inventaire des jetons soumis au cycle de rotation |
| `/api/rotator/renew` | `POST` | Publique | Demande manuelle de rotation et renouvellement d'un jeton |
| `/api/rotator/revoke` | `POST` | Publique | Invalidation immédiate et diffusion de révocation via gossip |
| `/api/radar/status` | `GET` | Publique | Métriques du radar, état des PoPs suivis et latence moyenne |
| `/api/radar/route` | `GET/POST`| Publique | Résolution géographique Anycast sous la milliseconde |
| `/api/billing/summary` | `GET` | Publique | Bilan financier du cluster et total des économies réalisées |
| `/api/billing/ledger` | `GET` | Publique | Téléchargement du journal comptable (`?format=json\|csv\|jsonl\|opencost`) |
| `/api/mtls/status` | `GET` | Publique | État de l'autorité de certification, règles mTLS et diagnostic TPM 2.0 |
| `/api/mtls/ca/cert` | `GET` | Publique | Récupération du certificat racine public au format PEM |
| `/api/mtls/ca/issue` | `POST` | Publique | Création d'un certificat client temporaire avec paramètres SAN |
| `/api/mtls/verify` | `POST` | Publique | Validation d'un certificat X.509 par rapport aux politiques de confiance |
| `/raft/status` | `GET` | Publique | Rôle du nœud Raft local, identifiant du leader et index du journal |
| `/metrics` | `GET` | Publique | Collecte de télémétrie conforme au format standard Prometheus 0.0.4 |
| `/api/compiler/compile`| `POST` | Publique | Compilation, analyse syntaxique et minification de code Worker |
| `/s3/{bucket}/{key...}`| `PUT/GET` | Publique | Accès au stockage virtuel compatible avec l'API AWS S3 |
| `/proxy/{service}/*` | `ANY` | Publique | Proxy inverse transparent avec interception des erreurs 429 |
| `/api/accounts` | `GET` | Publique | Inventaire des comptes Cloudflare mutualisés et solde des quotas |
| `/api/leases` | `GET/POST` | **Bearer** | Consultation des baux actifs ou enregistrement d'un nouveau bail |
| `/api/leases/revoke` | `POST` | **Bearer** | Annulation immédiate d'un bon de bail |
| `/api/health` | `GET` | Publique | Contrôle de la connectivité avec l'API publique de Cloudflare |

---

## 🗺️ Feuille de Route (Roadmap)

Consultez le fichier [ROADMAP.md](../../ROADMAP.md) pour découvrir les prochaines étapes et évolutions planifiées.

---

## 🔒 Sécurité (Security)

Pour signaler une vulnérabilité de manière responsable, veuillez vous référer à [SECURITY.md](../../SECURITY.md).

---

## 📄 Licence (License)

Ce projet est distribué sous licence [Apache License 2.0](../../LICENSE).
