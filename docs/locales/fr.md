<div align="center">

<p align="center">
  <a href="../../README.md">🇺🇸 English</a> · <a href="./ko.md">🇰🇷 한국어</a> · <a href="./zh-CN.md">🇨🇳 中文</a> · <a href="./es.md">🇪🇸 Español</a> · <a href="./hi.md">🇮🇳 हिन्दी</a><br />
  <a href="./ar.md">🇸🇦 العربية</a> · <a href="./pt-BR.md">🇧🇷 Português</a> · <a href="./ru.md">🇷🇺 Русский</a> · <strong>🇫🇷 Français</strong> · <a href="./id.md">🇮🇩 Bahasa Indonesia</a>
</p>

# ⚡ CF-Lease-Broker

**Mise en commun de ressources multi-comptes Cloudflare et courtier de baux dynamiques de jetons pour Workers, KV et R2.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg?style=flat&logo=go)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6%2B-3178C6.svg?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Cloudflare](https://img.shields.io/badge/Cloudflare-Workers_%2B_R2_%2B_KV-f38020.svg?style=flat&logo=cloudflare)](https://cloudflare.com)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Zero Dependency](https://img.shields.io/badge/Zero--Dependency-Self--Contained-38bdf8.svg)](#-démarrage-rapide)

<sub>Développé par **kLauncher1** · Mutualisation de ressources périphériques haute disponibilité, coffre-fort de secrets et baux dynamiques de locataires.</sub>

</div>

---

## ⚡ Points Forts (Highlights)

- **Bac à Sable Wasm Périphérique et Plafond Physique de Mémoire de 32 Mo** : Mémoire linéaire gérée et sécurisée WebAssembly partitionnée en pages de 64 Kio (`WasmPageSize = 65536`), imposant un plafond strict de 32 Mio (`HardCeilingMemoryPages = 512`), protection contre les accès hors limites, nettoyage par mise à zéro (Zeroization) au démontage et suivi des pics d'allocation (`internal/sandbox/memory.go`).
- **Comptabilité Déterministe du Gaz et Blocage des Boucles** : Budget d'exécution par instruction avec comptabilisation déterministe du gaz et des étapes, interrompant immédiatement les boucles infinies après épuisement du budget sans bloquer le thread du courtier hôte (`internal/sandbox/gas.go`).
- **Isolation des Défaillances de l'Hôte et Récupération Tolérante aux Paniques** : La récupération différée enveloppe les exécutions de charges de travail clientes, interceptant les pannes (violations de mémoire SIGSEGV, paniques d'exécution non gérées, pièges de gaz et OOM) tout en maintenant le courtier hôte 100 % stable, adossé à un tampon annulaire de pannes (conserve jusqu'à 500 rapports récents) et des seuils progressifs de mise en quarantaine (`internal/sandbox/engine.go`).
- **Routage de Paquets eBPF HTTP/3 avec Zéro Surcharge** : Routage de sockets au niveau du noyau contournant la pile TCP de l'espace utilisateur via sockmap eBPF et crochets XDP sous Linux, atteignant des latences de distribution inférieures à 10 microsecondes (< 10µs, mesuré ~0.26µs) pour les charges de proxy périphérique (`internal/steering/engine.go`).
- **Inspection des En-têtes de Paquets UDP/QUIC RFC 9000** : Inspection sans allocation sur le tas qui analyse les en-têtes QUIC Long (Initial, 0-RTT, Handshake, Retry) et Short (1-RTT), extrayant les identifiants de connexion source/destination (DCID/SCID) et acheminant le trafic directement vers les points de terminaison optimaux de Workers selon les règles de préfixe CID et le moindre nombre de connexions (`internal/steering/quic_parser.go`).
- **Contournement de Noyau Multiplateforme et Repli dans l'Espace Utilisateur** : Architecture multiplateforme fournissant des crochets sockmap eBPF natifs sous Linux et une distribution rapide zéro-copie dans l'espace utilisateur sous Windows et macOS avec latence submicroseconde et couverture de test à 100 % (`internal/steering/`).
- **Rotateur Autonome de Jetons Périphériques** : Surveillance proactive du cycle de vie des jetons de bail et locataires déclenchant des renouvellements en arrière-plan dès 80 % de validité, avec double validité sans temps d'arrêt pendant une période de grâce de 30 secondes (`internal/rotator/rotator.go`).
- **Maillage de Révocation de Jetons Basé sur Gossip Inférieur à 5 ms** : Registre décentralisé de pierres tombales de révocation avec recherches en $O(1)$, pierres tombales signées HMAC-SHA256, numérotation séquentielle monotone, synchronisation d'état anti-entropie et diffusion peer-to-peer propagée en moins de 5 ms sur le cluster (`internal/rotator/mesh.go`).
- **Attestation Matérielle TPM 2.0 pour l'Actualisation Déléguée** : Vérification cryptographique de l'intégrité de la plateforme pour les demandes déléguées de rafraîchissement de jetons, nonces de défi à usage unique évitant les attaques par rejeu, validation des étiquettes HMAC en temps constant et vérification des résumés des Registres de Configuration de Plateforme (PCR) (`internal/rotator/attest.go`).
- **Radar Intelligent de Santé Périphérique et Routage Géographique Anycast** : Sondage synthétique concurrent des points de présence (PoPs) mondiaux de Cloudflare, suivi de la latence par moyenne mobile exponentielle (EMA, `alpha=0.25`), gigue (jitter) et scores de santé composites, combiné à un routage géographique Anycast submilliseconde (< 1 ms) et détection des anomalies d'oscillation de routes BGP (`internal/radar/`).
- **Migration Autonome Préventive des Baux** : Transfère dynamiquement les baux actifs de microservices depuis les PoPs dégradés vers des alternatives saines en veille avant l'apparition de pertes de paquets ou d'expirations de sockets.
- **Optimisation Autonome des Coûts et Grand Livre Multi-Locataires** : Mesure granulaire du calcul, des requêtes et du stockage avec imputation des coûts en microcents ($1.00 = 1 000 000 microcents) selon les tarifs officiels de Cloudflare pour Workers, KV et R2, associée à la migration autonome des charges vers les comptes gratuits en veille pour éviter les dépassements (`internal/billing/`).
- **Exportation Financière Multi-Format et OpenCost 1.0** : Prise en charge native de l'exportation des écritures comptables en CSV RFC 4180, JSON délimité par des retours à la ligne (JSONL) et format standard OpenCost 1.0 (`/api/billing/ledger?format=opencost`) pour l'ingénierie financière périphérique.
- **TLS Mutuel Zero-Trust (mTLS) et Vérification des Certificats Clients** : Vérification complète de la chaîne de confiance des certificats clients X.509, consultation des listes de révocation (CRL), application des fenêtres de validité, filtrage par noms alternatifs de sujet (SAN) génériques (`*.domain.com`) et politiques d'accès émetteur/nom commun pour les routes de proxy inverse (`internal/mtls/verifier.go`).
- **Autorité de Certification Éphémère (CA) et Maillage d'Appairage** : Génération en mémoire sans dépendance de certificats racines ECDSA P-256, émission dynamique de certificats clients, suivi des numéros de série CRL et jetons d'appairage inter-clusters signés cryptographiquement (`internal/mtls/ca.go`).
- **Attestation Matérielle TPM 2.0 et Magasin de Certificats Windows** : Utilise Windows Cryptography Next Generation (CNG) `ncrypt.dll` pour interroger la présence du TPM 2.0 dans Microsoft Platform Crypto Provider, inspecter le magasin de certificats système (`crypt32.dll`) et générer des mesures PCR déterministes (`internal/mtls/tpm_windows.go`).
- **Moteur de Consensus Raft Haute Disponibilité** : Cluster de consensus Raft multi-nœuds avec temporisateurs d'élection aléatoires (150-300 ms), élection de leader par quorum majoritaire, journal Write-Ahead Log (WAL) avec indexation monotone et sommes de contrôle SHA-256, et découverte dynamique de pairs par DNS SRV (`net.LookupSRV`) sans dépendance externe (`internal/raft/`).
- **Compilateur Virtuel et Empaqueteur de Workers Périphériques** : Minificateur lexical et validateur en mémoire ultrarapide pour paquets JavaScript/Wasm avec suppression des commentaires, vérification syntaxique et hachage de contenu SHA-256 (`internal/compiler/bundle.go`).
- **Injection Dynamique de Routes Multi-Zones** : Compilateur de routes génériques haute performance associant des domaines arbitraires (`*.domain.com/path/*`) avec pondération de priorité et tables d'injection dynamique de routes (`internal/compiler/router.go`).
- **Canal de Diffusion Directe Zéro-Copie** : Transmission des réponses périphériques à faible latence grâce à des pools de tampons de 64 Ko préalloués (`sync.Pool`), vidage immédiat des fragments (`http.Flusher`) et détection des déconnexions clientes (`internal/compiler/stream.go`).
- **Artéfacts Autonomes Multi-Architectures** : Pipeline de compilation croisée empaquetant des exécutables autonomes optimisés pour `linux/amd64`, `linux/arm64`, `darwin/arm64` et `windows/amd64` avec génération automatique de sommes de contrôle SHA-256 (`scripts/build_artifacts.mjs`).
- **Rechargement à Chaud sans Temps d'Arrêt** : Rechargement dynamique de la configuration et des identifiants via surveillance de fichiers et signaux OS (`SIGHUP` sur POSIX) synchronisant les locataires groupés et le coffre de secrets sans redémarrer le processus.
- **Agrégation de Quotas Multi-Comptes** : Regroupe plusieurs comptes gratuits et payants de Cloudflare en un cluster de calcul virtuel unifié, dépassant les limites quotidiennes de requêtes individuelles.
- **Coffre de Secrets pour la Production** : Chiffrement d'enveloppe au repos via Windows DPAPI (`CryptProtectData`) et AES-256-GCM multiplateforme avec nettoyage sécurisé de la mémoire (`Zeroize`).
- **Authentification JWT HS256 et RBAC** : Moteur d'authentification administrative et de microservices avec jetons HMAC-SHA256, vérification en temps constant et listes de blocage JTI protégées contre les accès concurrents.
- **Passerelle Virtuelle R2 Compatible S3** : Points de terminaison avec protocole REST S3 (`/s3/{bucket}/{key...}`) distribuant le stockage d'objets sur les compartiments R2 groupés avec découpage automatique en blocs de 2 Mo et vérification d'intégrité SHA-256.
- **Cache KV Partitionné Multi-Comptes** : Couche de stockage KV partitionnée sécurisée pour les threads avec hachage FNV cohérent, mise en cache LRU et éviction par TTL.
- **Exportateur de Métriques Prometheus 0.0.4** : Point de terminaison standard `/metrics` exportant les décomptes de baux, les quotas de locataires, les rejets de limitation de débit et la télémétrie de proxy sans dépendance externe.
- **Limitation de Débit par Fenêtre Glissante et Période d'Épreuve Automatique** : Suivi de débit en mémoire par fenêtre glissante avec tolérance aux rafales et machine à états de période d'épreuve avec recul exponentiel.
- **Webhooks Multicanaux de Quotas** : Notifications enrichies sur Discord, Slack Block Kit et JSON générique déclenchées aux seuils de 80 % (Avertissement), 95 % (Critique) et 100 % (Épuisé) avec amortissement de 10 minutes.
- **Interception Automatique des Limites de Débit et Basculement** : Intercepte les erreurs HTTP 429 et de passerelle en temps réel, redirigeant les requêtes en cours vers le compte sain suivant du groupe sans interruption.
- **Proxy Inverse Transparent** : Transmet le trafic de manière fluide via `/proxy/{service}/*` avec rejets de corps mis en mémoire tampon et suppression des en-têtes de saut à saut RFC 7230.
- **Baux Dynamiques de Ressources (Coupons)** : Émet des jetons d'API temporaires à portée restreinte (`lse_...`) pour les microservices internes, les appelants externes ou les suites de tests.
- **Déploiement en un Seul Exécutable** : Le tableau de bord d'administration web avec esthétique glassmorphism est directement intégré dans le binaire Go via `//go:embed`.
- **Empreinte Mémoire Légère** : S'exécute de manière persistante en consommant moins de **12 Mo de RAM** sans aucune dépendance externe au moment de l'exécution.

---

## 🚀 Démarrage Rapide

### 1. Télécharger et Exécuter

```bash
# Cloner le dépôt
git clone https://github.com/kLauncher1/cf-lease-broker.git
cd cf-lease-broker

# Exécuter directement
go run .
```

La console d'administration sera disponible sur :
👉 **`http://localhost:9081`**

### 2. Compilation Croisée d'Artéfacts Autonomes Multi-Architectures

```bash
# Construire des binaires autonomes pour linux/amd64, linux/arm64, darwin/arm64, windows/amd64
node scripts/build_artifacts.mjs --clean

# Vérifier les sommes de contrôle cryptographiques SHA-256
node scripts/build_artifacts.mjs --verify
```

---

## 🏛️ Architecture et Mutualisation des Ressources

```text
┌────────────────────────────────────────────────────────────────────────┐
│                            cf-lease-broker                             │
│                         Binaire Go Autonome                            │
├───────────────────┬───────────────────┬────────────────────────────────┤
│   internal/raft   │  internal/broker  │       internal/storage         │
│ • Cœur de Nœud    │ • Pool de Comptes │ • Passerelle S3 (/s3/...)      │
│ • Journal WAL     │ • Baux Dynamiques │ • Découpeur en Bandes de 2 Mo  │
│ • Pairs DNS SRV   │ • Équilibre Quota │ • Couche Cache KV Partitionnée │
│ • Synchro FSM     │ • Basculement Auto│ • Webhooks Multi-Canaux        │
├───────────────────┼───────────────────┼────────────────────────────────┤
│   internal/mtls   │ internal/security │       internal/compiler        │
│ • CA Éphémère     │ • Windows DPAPI   │ • Minificateur Virtuel JS/Wasm │
│ • Cert X509 Client│ • AES-256-GCM     │ • Compilateur Routes Multi-Zone│
│ • Attestation TPM │ • HS256 JWT/RBAC  │ • Flux Direct Zéro-Copie 64 Ko │
│ • Jetons d'Appair.│ • Nettoyage Mém.  │ • Dépôt de Paquets Locataires  │
├───────────────────┴───────────────────┴────────────────────────────────┤
│                         internal/quota & config                        │
│  • Prometheus (/metrics) • Fenêtre Glissante • Rechargement à Chaud    │
├────────────────────────────────────────────────────────────────────────┤
│                         Moteur HTTP net/http                           │
│   • /api/mtls/* (Cert/Vérif/TPM)       • /api/compiler/*               │
│   • /raft/* (Vote/Ajout/Statut)        • /proxy/{svc}/* (Gardé mTLS)   │
│   • /metrics         • /api/auth/*     • /s3/{bkt}/{key}               │
│   • /api/accounts    • /api/security   • /api/config/* (Gardé Auth)    │
├────────────────────────────────────────────────────────────────────────┤
│                       Tableau de Bord Web Intégré                      │
│                    (UI Glassmorphism via //go:embed)                   │
│                    web/dist/index.html -> Port 9081                    │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 🔒 Gestion des Secrets et Authentification

`cf-lease-broker` offre un stockage de secrets de niveau entreprise et une authentification prête à l'emploi sans dépendance tierce :

### 1. Coffre-Fort à Chiffrement d'Enveloppe (`internal/security/vault.go`)
- **Intégration Windows DPAPI** : Sur les systèmes Windows, les secrets sont protégés via Windows DPAPI (`CryptProtectData`), sécurisant les clés API avec les identifiants de l'OS (`dpapi:<base64>`).
- **Enveloppe AES-256-GCM** : Sur les plateformes non-Windows (ou sur configuration explicite), les secrets sont chiffrés par AES-256-GCM authentifié avec des nonces aléatoires uniques de 96 bits (`aes-gcm:<nonce>:<ciphertext>`).
- **Nettoyage de la Mémoire** : Les tampons d'octets sensibles sont immédiatement écrasés après utilisation selon le modèle de nettoyage `Zeroize` (`for i := range b { b[i] = 0 }`).

### 2. Middleware JWT et RBAC (`internal/security/middleware.go`)
- **Jetons Standard HS256** : Émet et vérifie des jetons HMAC-SHA256 JWT avec comparaison de signature en temps constant (`crypto/subtle.ConstantTimeCompare`) pour parer aux attaques temporelles par canal auxiliaire.
- **Liste de Révocation** : Prend en charge la révocation immédiate de jetons par identifiant JTI (`POST /api/auth/revoke`) avec balayage automatique des expirations TTL.
- **APIs d'Administration Protégées** : Les points de terminaison d'administration (`/api/leases`, `/api/leases/revoke` et `/api/security/*`) exigent un en-tête valide `Authorization: Bearer <token>` ou des clés maîtres d'amorçage.

```bash
# 1. Émettre un jeton JWT d'opérateur
curl -X POST http://localhost:9081/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"sub": "gateway-client", "role": "operator", "duration_hours": 24}'

# 2. Appeler le point de terminaison protégé des baux avec le jeton Bearer
curl -X GET http://localhost:9081/api/leases \
  -H "Authorization: Bearer <issued_jwt_token>"
```

---

## 🪣 Passerelle Virtuelle R2 Compatible S3 (`/s3/{bucket}/{key...}`)

Le courtier expose des points de terminaison compatibles avec le protocole AWS S3 standard, permettant aux SDK S3, microservices et outils CLI existants de stocker des objets sur des comptes Cloudflare R2 mutualisés sans dépasser les quotas de stockage individuels :

### Capacités Principales :
1. **Découpage en Bandes** : Les données entrantes dépassant 2 Mo sont automatiquement divisées en blocs séquentiels (`chk_<hash>_<index>`) et distribuées entre les comptes sains du pool.
2. **Sommes de Contrôle d'Intégrité** : Chaque bloc intègre une somme de contrôle individuelle SHA-256, et les objets reçoivent des ETags composites conformes aux chargements en plusieurs parties.
3. **Requêtes HTTP Range** : Prise en charge des en-têtes `Range: bytes=start-end` pour le streaming vidéo, la reprise de téléchargement et les lectures partielles de fichiers volumineux.
4. **ListObjectsV2** : Conforme aux réponses XML S3 standard `ListBucketResult`.

### Exemples avec la CLI S3 et Curl :

```bash
# 1. Stocker un objet sur les compartiments R2 mutualisés
curl -i -X PUT http://localhost:9081/s3/assets/docs/whitepaper.pdf \
  -H "Content-Type: application/pdf" \
  --data-binary "@whitepaper.pdf"

# 2. Récupérer l'objet
curl http://localhost:9081/s3/assets/docs/whitepaper.pdf -o downloaded.pdf

# 3. Requête de plage d'octets partielle (Range)
curl -i -H "Range: bytes=0-1048575" http://localhost:9081/s3/assets/docs/whitepaper.pdf

# 4. Lister les objets du compartiment (XML S3)
curl http://localhost:9081/s3/assets
```

---

## 🔄 Proxy Inverse Transparent avec Basculement (`/proxy/{service}/*`)

Le courtier fournit un proxy inverse périphérique transparent à l'adresse `/proxy/{service}/*`. Les appelants transmettent leurs requêtes HTTP à ce point de terminaison sans avoir à déterminer quel compte locataire détient du quota disponible.

### Fonctionnement du Basculement :
1. **Résolution de Cible** : Le `{service}` et le sous-chemin de la requête entrante sont résolus vers le compte mutualisé disposant du plus grand quota restant (`SelectOptimalAccount`).
2. **Mise en Mémoire Tampon** : Le corps des requêtes est conservé en mémoire (jusqu'à 10 Mo) via des lecteurs de flux pour rejouer fidèlement l'intégralité de la charge utile en cas de nouvelle tentative.
3. **Interception des Erreurs 429 et Passerelle** : Si le compte cible répond par HTTP `429 Too Many Requests`, `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout` ou rencontre une rupture de connexion :
   - Le courtier marque immédiatement ce locataire comme dégradé via `TriggerFailover(accountID)`.
   - La requête est automatiquement réessayée sur le compte sain suivant dans le pool.
4. **Nettoyage des En-têtes de Saut à Saut** : Les en-têtes de saut à saut RFC 7230 (`Connection`, `Keep-Alive`, `Upgrade`, `TE`, `Trailers`, etc.) sont purgés dans les deux sens.
5. **En-têtes de Télémétrie** : Les réponses réussies incluent des en-têtes d'inspection utiles :
   - `X-CF-Broker-Account: {account_id}` (locataire ayant exécuté la requête)
   - `X-CF-Broker-Attempts: {count}` (nombre de tentatives nécessaires)

---

## 🔄 Rechargement à Chaud sans Temps d'Arrêt (`internal/config/`)

`cf-lease-broker` prend en charge la rotation dynamique des identifiants et la mise à jour des pools de comptes sans redémarrer le processus ni interrompre les connexions HTTP en cours :

### Mécanique du Rechargement à Chaud :
1. **Surveillance des Fichiers** : Scrute périodiquement le fichier de configuration (`CF_BROKER_CONFIG_PATH` ou `config.json`) et évalue les modifications de contenu via des empreintes déterministes SHA-256 pour éviter les faux rechargements.
2. **Gestion des Signaux POSIX** : Capture le signal `SIGHUP` (Unix/Linux/macOS) pour déclencher la validation et le rechargement synchrones et immédiats de la configuration.
3. **Échange d'État Atomique et Repli Sécurisé** : Si le fichier rechargé contient du JSON invalide, des limites négatives ou des doublons d'identifiants, le rechargement échoue en toute sécurité avec journalisation, conservant la configuration active intacte.
4. **Synchronisation des Identifiants et du Pool** : Les locataires rechargés avec succès sont synchronisés directement avec `PoolManager` (`SyncAccounts`) et chiffrés dans `SecretVault` (`StoreCredential`).

### Format du Fichier de Configuration (`config.json`) :
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

## 🚀 Compilateur Virtuel de Workers Périphériques et Injection Dynamique de Routes

Le courtier intègre un moteur de compilation et de routage pour scripts Workers périphériques multi-locataires et modules WebAssembly :

### 1. Minification Lexicale et Empaquètement en Mémoire (`internal/compiler/bundle.go`)
- **Tokeniseur Lexical** : Supprime les commentaires mono-ligne (`//`) et multi-lignes (`/* */`), élimine les espaces superflus, normalise la ponctuation et valide les signatures d'exportation de modules Cloudflare Worker (`export default { fetch(...) }`, `addEventListener('fetch')`).
- **Validation WebAssembly** : Vérifie l'en-tête magique de 4 octets (`\x00asm`) et la conformité de version avant l'empaquetage du locataire.
- **Hachage de Contenu** : Génère des empreintes déterministes SHA-256 pour le suivi des versions et l'audit des paquets de locataires.

### 2. Compilateur de Routes Génériques Multi-Zones (`internal/compiler/router.go`)
- **Mappage de Domaines Arbitraires** : Associe des points de terminaison sur des sous-domaines génériques (`*.domain.com/*`), des hôtes exacts (`api.domain.com/v1/*`) ou des règles de repli globales (`*/*`).
- **Trie Pondéré par Priorité** : Évalue l'hôte et le chemin des requêtes entrantes avec calcul de spécificité (hôte exact > sous-domaine générique > global ; préfixe long > court), routant dynamiquement le trafic sans recharger la configuration.

### 3. Canal de Diffusion Directe Zéro-Copie (`internal/compiler/stream.go`)
- **Pool de Tampons** : Utilise des tranches d'octets préallouées de 64 Ko (`sync.Pool`) pour éliminer les allocations sur le tas lors des transferts proxy à haut débit.
- **Vidage Immédiat des Sockets** : Tire parti de `http.Flusher` pour diffuser les blocs de réponse aux clients en temps réel tout en désactivant la mise en cache intermédiaire via `X-Accel-Buffering: no`.
- **Détection des Déconnexions** : S'associe à `context.Context` pour interrompre immédiatement les flux de Workers en amont lors des déconnexions clientes, évitant les fuites de sockets.

---

## 🤝 Moteur de Consensus Distribué Raft Haute Disponibilité

`cf-lease-broker` intègre un moteur de consensus Raft sans dépendance (`internal/raft/`) permettant la mise en cluster multi-régions, la réplication des baux sans perte de données et le basculement automatique du leader :

### Architecture et Primitives de Consensus :
1. **Élection du Leader par Quorum et Minuteurs Aléatoires** : Les nœuds transitent entre les états Follower, Candidate et Leader. Les délais d'élection aléatoires (150–300 ms) éliminent les blocages par vote partagé, tandis qu'un battement de cœur de 50 ms maintient l'autorité du leader sur un quorum majoritaire `((N/2) + 1)`.
2. **Journal Write-Ahead Log (WAL) (`internal/raft/journal.go`)** : Impose une indexation monotone des transactions avec sommes de contrôle SHA-256. La cohérence du journal des followers est validée avant tout ajout, tronquant automatiquement les entrées non validées en conflit. Prise en charge d'une persistance sur disque en mode ajout seul (`raft_journal.wal`).
3. **Découverte Dynamique des Pairs par DNS SRV (`internal/raft/discovery.go`)** : Résout automatiquement les pairs du cluster via les enregistrements DNS SRV RFC 2782 (`_raft._tcp.<domain>`) avec repli élégant sur les variables d'environnement statiques (`CF_RAFT_PEERS`) ou l'enregistrement manuel à l'exécution (`POST /raft/peers`).
4. **Machine à États Finis (FSM) du Courtier (`internal/raft/fsm_broker.go`)** : Réplique les commandes transactionnelles de manière déterministe sur tous les nœuds :
   - `GRANT_LEASE` : Réplique les baux actifs sur l'ensemble du cluster.
   - `REVOKE_LEASE` : Propage la révocation des baux à tous les pairs.
   - `SYNC_ACCOUNT` : Synchronise les profils des locataires et les quotas.
   - `UPDATE_QUOTA` : Propage les incréments quotidiens de requêtes Workers, les deltas R2 et les lectures KV.
   - `NOOP` : Sondes de barrière de consensus pour vérifier la préparation du cluster.

### Flux de la Topologie du Cluster :

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

### Exemples Curl pour le Cluster Raft :

```bash
# 1. Inspecter l'état du nœud local et la topologie du cluster
curl http://localhost:9081/raft/status

# 2. Vérifier la santé des pairs et la latence aller-retour (RTT)
curl http://localhost:9081/raft/peers

# 3. Enregistrer manuellement un nouveau nœud pair
curl -X POST http://localhost:9081/raft/peers \
  -H "Content-Type: application/json" \
  -d '{"id": "node-2", "address": "http://127.0.0.1:9082"}'

# 4. Proposer une transaction de machine à états au cluster
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

# 5. Consulter les entrées du journal WAL répliquées et les sommes SHA-256
curl http://localhost:9081/raft/journal
```

---

## 🛡️ TLS Mutuel Zero-Trust (mTLS) et Attestation Matérielle

`cf-lease-broker` intègre un moteur complet de TLS mutuel (mTLS) Zero-Trust, une Autorité de Certification (CA) Éphémère dynamique en mémoire et une attestation TPM 2.0 matérielle :

### 1. Autorité de Certification Éphémère et Émission Dynamique (`internal/mtls/ca.go`)
- **Ancre Racine en Mémoire** : Initialise dynamiquement une CA racine ECDSA P-256 (`x509.IsCA = true`) sans outil externe ni dépendance OpenSSL.
- **Certificats Clients et Pairs** : Émet des certificats X.509 clients et pairs éphémères signés cryptographiquement, avec Noms Communs personnalisés, fenêtres de validité et SANs.
- **Révocation CRL** : Maintient une liste de révocation de certificats (CRL) rapide et sécurisée pour les threads, indexée par numéros de série hexadécimaux avec recherche en $O(1)$.
- **Bons d'Appairage Signés** : Génère et valide des jetons d'appairage ECDSA-SHA256 pour la communication inter-clusters Zero-Trust.

### 2. Vérification des Certificats Clients et Politiques (`internal/mtls/verifier.go`)
- **Vérification de la Chaîne de Confiance** : Valide les chaînes de certificats X.509 clients entrantes auprès du pool de la CA racine (`x509.CertPool`).
- **Application des Politiques** : Applique le filtrage par SANs génériques (`*.edge.cluster.local`, blocs CIDR), Noms Communs autorisés, Émetteurs autorisés et fenêtres de validité strictes.
- **Extraction Transparente des En-têtes** : Extrait les certificats des connexions TLS directes (`r.TLS.PeerCertificates`) ou des en-têtes standard de transfert (`X-Client-Cert-Pem`, `X-Forwarded-Client-Cert`, `X-Client-Cert-Der`).
- **Protection du Proxy Inverse** : Lorsque `CF_REQUIRE_MTLS=true` est actif, toutes les requêtes via `/proxy/{service}/*` doivent fournir un certificat client vérifié.

### 3. Attestation Matérielle TPM 2.0 (`internal/mtls/tpm_windows.go`)
- **Intégration Windows CNG** : Interroge Microsoft Cryptography Next Generation (`ncrypt.dll`) pour le "Microsoft Platform Crypto Provider" afin de confirmer la présence du TPM 2.0.
- **Magasin de Certificats Windows** : Inspecte et valide les magasins de certificats machine/utilisateur via `crypt32.dll` (`CertOpenSystemStoreW`).
- **Mesures de Plateforme** : Génère des résumés déterministes des Registres de Configuration de Plateforme (PCR) et des mesures de clés d'endossement pour l'audit.

### Exemples Curl pour mTLS et TPM :

```bash
# 1. Vérifier l'état mTLS, la CA Racine active et l'état d'attestation TPM
curl http://localhost:9081/api/mtls/status

# 2. Télécharger le certificat de la CA Racine au format PEM
curl http://localhost:9081/api/mtls/ca/cert -o root-ca.pem

# 3. Émettre un certificat client éphémère
curl -X POST http://localhost:9081/api/mtls/ca/issue \
  -H "Content-Type: application/json" \
  -d '{
    "common_name": "worker-edge-alpha",
    "sans": ["*.edge.broker.local", "127.0.0.1"],
    "validity_hours": 24
  }'

# 4. Vérifier un certificat PEM X.509 selon la politique active
curl -X POST http://localhost:9081/api/mtls/verify \
  -H "Content-Type: application/json" \
  -d '{"certificate_pem": "-----BEGIN CERTIFICATE-----\n..."}'

# 5. Révoquer un numéro de série de certificat
curl -X POST http://localhost:9081/api/mtls/revoke \
  -H "Content-Type: application/json" \
  -d '{"serial_number": "7b8a1c9d2e"}'

# 6. Interroger l'attestation matérielle TPM 2.0
curl http://localhost:9081/api/mtls/tpm/attest
```

---

## 💰 Optimisation Autonome des Coûts et Grand Livre Multi-Locataires

`cf-lease-broker` intègre un moteur de facturation multi-locataires et un optimiseur de coûts de haute précision conçus pour suivre la consommation au microcent près et prévenir les dépassements budgétaires dans le cloud :

### 1. Imputation Granulaire des Coûts en Microcents (`internal/billing/meter.go`)
- **Précision au Microcent** : Suivi des ressources avec calcul entier en microcents ($1.00 = 1 000 000 microcents) pour éliminer toute dérive d'arrondi en virgule flottante.
- **Grille Tarifaire Complète Cloudflare** : Pré-calibré avec les tarifs officiels de Cloudflare :
  - Requêtes Workers : 0,30 $ par million de requêtes (300 000 microcents)
  - Durée CPU Workers : 0,02 $ par million de millisecondes CPU (20 000 microcents)
  - Lectures KV : 0,50 $ par million de lectures (500 000 microcents)
  - Écritures KV : 5,00 $ par million d'écritures (5 000 000 microcents)
  - Stockage R2 : 0,015 $ par Go-mois (15 000 microcents)
  - Opérations Classe A R2 : 4,50 $ par million d'opérations (4 500 000 microcents)
  - Opérations Classe B R2 : 0,36 $ par million d'opérations (360 000 microcents)
  - Bande Passante Sortante : 0,00 $ (garantie zéro frais de sortie de Cloudflare)
- **Allocations Quotidiennes du Niveau Gratuit** : Suit les quotas gratuits quotidiens (100 000 requêtes Workers et 100 000 lectures KV par compte) avec réinitialisation à minuit UTC, imputant 0 $ de coût tout en cumulant les économies réalisées.

### 2. Migration Autonome des Charges de Travail (`internal/billing/optimizer.go`)
- **Détection Proactive des Dépassements** : Audite en continu l'utilisation des comptes par rapport à des seuils configurables (80 % par défaut).
- **Priorité au Niveau Gratuit en Veille** : Lorsqu'un compte payant s'approche de son plafond, l'optimiseur redirige le trafic ou instaure une période d'épreuve, réacheminant les baux vers des comptes gratuits sains ayant des quotas disponibles.
- **Suivi des Économies Cumulées** : Quantifie et consigne les microcents exacts économisés en évitant les surcoûts des niveaux payants.

### 3. OpenCost 1.0 et Exportations Financières Multi-Formats (`internal/billing/journal.go`)
- **Spécification OpenCost 1.0** : Exporte les données de facturation directement conformes à la spécification OpenCost pour l'edge computing (`/api/billing/ledger?format=opencost`).
- **Exportation CSV RFC 4180** : Génère des écritures séparées par des virgules pour tableurs et audits comptables (`/api/billing/ledger?format=csv`).
- **JSON Délimité par Retours à la Ligne (JSONL)** : Diffuse des flux de journaux pour intégration dans BigQuery, Datadog ou Elasticsearch (`/api/billing/ledger?format=jsonl`).

### Exemples Curl pour Facturation et Coûts :

```bash
# 1. Consulter le récapitulatif des coûts du cluster et la répartition par locataire
curl http://localhost:9081/api/billing/summary

# 2. Exporter le grand livre de facturation au format JSON OpenCost 1.0
curl http://localhost:9081/api/billing/ledger?format=opencost

# 3. Télécharger le journal de facturation en CSV RFC 4180
curl http://localhost:9081/api/billing/ledger?format=csv -o ledger.csv

# 4. Déclencher une évaluation manuelle de migration des charges
curl -X POST http://localhost:9081/api/billing/optimize

# 5. Consulter l'historique des migrations autonomes et les économies
curl http://localhost:9081/api/billing/migrations

# 6. Ingérer un événement personnalisé de consommation locataire
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

## 🛰️ Radar de Santé Périphérique et Routage Géographique Anycast

`cf-lease-broker` embarque un moteur autonome de Radar de Santé qui surveille les Points de Présence (PoPs) mondiaux de Cloudflare, détecte les anomalies avant la perte de paquets et résout les routes Anycast avec une précision submilliseconde :

### 1. Sondage Synthétique Périphérique et Télémétrie (`internal/radar/probe.go`)
- **Moyenne Mobile Exponentielle (EMA)** : Lisse les mesures de sondage haute fréquence (`alpha=0.25`) afin de prévenir les basculements intempestifs lors d'oscillations passagères.
- **Calcul de Gigue et Pertes** : Calcule les taux de gigue et de perte de paquets, produisant un indice de santé composite (0 à 100) par PoP.
- **Distribution Concurrente** : Des pools de threads restreints interrogent simultanément les PoPs sans risquer de saturer le système.

### 2. Routage Géographique Anycast Submilliseconde (`internal/radar/router.go`)
- **Distance Haversine** : Calcule la distance orthodromique exacte entre les coordonnées du client et l'emplacement des PoPs périphériques.
- **Optimisation Multicritère** : Pondère la distance géographique, la latence EMA, la gigue et la disponibilité opérationnelle.
- **Affinité des Locataires** : Associe les baux des locataires à leurs PoPs régionaux optimaux avec une résolution submilliseconde.

### 3. Détection des Anomalies de Routage BGP et Évacuation Proactive
- **Détection des Battements BGP** : Identifie les écarts de latence supérieurs à 35 ms ou 1,8 fois l'EMA de référence comme anomalies critiques.
- **Évacuation Préventive** : Migre automatiquement les baux actifs assignés à des PoPs dégradés vers des nœuds sains en veille avant que les clients ne constatent d'interruptions.

### Exemples Curl pour le Radar :

```bash
# 1. Consulter l'état du radar et la télémétrie mondiale
curl http://localhost:9081/api/radar/status

# 2. Lister tous les PoPs surveillés en région Asie-Pacifique
curl "http://localhost:9081/api/radar/pops?region=Asia%20Pacific"

# 3. Résoudre le PoP périphérique optimal pour un emplacement (Séoul, KR)
curl -X POST http://localhost:9081/api/radar/route \
  -H "Content-Type: application/json" \
  -d '{"lat": 37.5665, "lon": 126.9780, "country": "KR"}'

# 4. Déclencher un sondage synthétique simultané sur tous les PoPs
curl -X POST http://localhost:9081/api/radar/probe \
  -H "Content-Type: application/json" \
  -d '{"concurrency": 10}'

# 5. Consulter les anomalies BGP récentes et les événements de migration
curl http://localhost:9081/api/radar/events
```

---

## 🔄 Rotateur Autonome de Jetons Périphériques et Maillage de Révocation

`cf-lease-broker` intègre un moteur autonome de rotation de jetons, un maillage de révocation ultra-rapide basé sur le protocole Gossip et une attestation TPM 2.0 matérielle pour les actualisations déléguées :

### 1. Renouvellement Proactif Autonome de Jetons (`internal/rotator/rotator.go`)
- **Déclenchement Proactif par Seuil** : Évalue les jetons de bail actifs et déclenche leur renouvellement en arrière-plan lorsqu'ils atteignent 80 % de leur durée de vie, évitant toute coupure cliente.
- **Période de Grâce sans Interruption** : Maintient les anciennes clés de jetons en mémoire (30 secondes par défaut) pour garantir une transition transparente des requêtes en cours sans erreur HTTP 401.
- **Synchronisation des Baux** : Met à jour en temps réel les clés de jetons et les délais d'expiration dans `internal/broker/lease.go`.

### 2. Maillage de Révocation Gossip Sub-5ms (`internal/rotator/mesh.go`)
- **Registre Décentralisé de Pierres Tombales** : Stocke des pierres tombales de révocation signées cryptographiquement avec consultation en $O(1)$ (`IsRevoked(id)`).
- **Propagation en Moins de 5 ms** : Diffuse instantanément les révocations à tous les nœuds du cluster en moins de 5 millisecondes.
- **Vérification Inviolable HMAC-SHA256** : Chaque pierre tombale est signée avec un numéro de séquence monotone et validée en temps constant.
- **Synchronisation Anti-Entropie** : Comble automatiquement les pierres tombales manquantes lors des échanges Gossip périodiques entre pairs.

### 3. Attestation Matérielle TPM 2.0 pour l'Actualisation Déléguée (`internal/rotator/attest.go`)
- **Nonces de Défi à Usage Unique** : Produit des nonces cryptographiques à durée de vie limitée, rejetant catégoriquement toute attaque par rejeu.
- **Vérification de l'Intégrité de la Plateforme** : Valide les résumés PCR et les attestations TPM 2.0 avant d'accorder le renouvellement de jetons pour des requêtes déléguées.

### Exemples Curl pour le Rotateur et le Maillage :

```bash
# 1. Consulter l'état du rotateur, les jetons actifs et la latence du maillage
curl http://localhost:9081/api/rotator/status

# 2. Lister tous les jetons gérés en rotation
curl http://localhost:9081/api/rotator/tokens

# 3. Déclencher manuellement la rotation immédiate d'un jeton
curl -X POST http://localhost:9081/api/rotator/renew \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "operator_manual_trigger"}'

# 4. Diffuser une révocation instantanée sur le maillage Gossip (<5ms)
curl -X POST http://localhost:9081/api/rotator/revoke \
  -H "Content-Type: application/json" \
  -d '{"token_id": "tok_12345678", "reason": "security_audit_invalidation"}'

# 5. Demander un nonce de défi pour l'attestation matérielle
curl http://localhost:9081/api/rotator/challenge

# 6. Effectuer une actualisation déléguée avec attestation matérielle TPM
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

# 7. Lister les pierres tombales de révocation actives
curl http://localhost:9081/api/rotator/tombstones
```

---

## 📦 Limiteur de Mémoire du Bac à Sable Wasm Périphérique et Isolation des Pannes

Les charges de travail de workers périphériques exécutées dans des déploiements de courtiers multi-locataires nécessitent des limites de ressources strictes et une isolation des pannes pour éviter que des scripts corrompus, des fuites de mémoire ou des exceptions non gérées ne dégradent le courtier hôte ou ne fassent fuiter des données entre locataires.

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

### Capacités Architecturales Fondamentales

1. **Plafond Physique Strict de 32 Mio** : La mémoire linéaire WebAssembly est partitionnée en pages de 64 Kio (`WasmPageSize = 65536`). Même si une politique locataire réclame davantage, le courtier plafonne strictement la mémoire linéaire à 512 pages (32 Mio, `HardCeilingMemoryPages`). Toute allocation excédant cette limite est rejetée avec l'erreur `ErrMemoryHardCeilingExceeded` et comptabilisée comme une interception OOM.
2. **Comptabilisation Déterministe du Gaz** : Les instructions déduisent des unités selon des coefficients fixes (opération de base : 1, arithmétique : 2, accès mémoire : 10, pas de boucle : 5, appel : 20, extension mémoire : 100/page). Lorsque le gaz est épuisé, l'exécution s'interrompt de façon déterministe avec `ErrGasExhausted`, brisant les boucles infinies sans figer le thread du courtier.
3. **Isolation des Pannes et Paniques** : L'exécution du code invité est enveloppée dans une logique de récupération différée (`defer func() { if r := recover(); ... }`). Les pannes sévères (par ex., déréférencement de pointeur nul SIGSEGV simulé ou panique d'exécution) sont piégées et converties en `CrashReport` isolé avec trace d'appels, tandis que le courtier hôte poursuit le traitement du trafic sans interruption.
4. **Journal de Pannes et Quarantaine Progressive** : Un tampon annulaire thread-safe conserve les 500 derniers rapports de panne avec horodatage, causes et états mémoire. Si un locataire dépasse son seuil d'échecs consécutifs configuré (par défaut : 5), il bascule automatiquement en état `QUARANTINED` jusqu'à réinitialisation par un opérateur.
5. **Nettoyage par Mise à Zéro (Zeroization)** : Lors de la destruction ou de la réinitialisation d'une instance, toutes les tranches de mémoire linéaire sont nettoyées avec des octets nuls (`subtle.ConstantTimeCompare`), empêchant la persistance de données entre locataires et les fuites de jetons.

### Utilisation et Exemples de Commandes

```bash
# 1. Inspecter l'état du superviseur de sandbox et les métriques agrégées
curl http://localhost:9081/api/sandbox/status

# 2. Exécuter une charge de calcul dans le sandbox Wasm invité
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "compute",
    "gas_budget": 100000
  }'

# 3. Tester l'interception de boucle infinie (arrêt par épuisement de gaz)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "infinite_loop",
    "gas_budget": 5000
  }'

# 4. Tester l'application du plafond mémoire physique de 32 Mo (rejet OOM)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "memory_alloc",
    "requested_pages": 600
  }'

# 5. Tester l'isolation de panne/panique (le courtier hôte reste 100% stable)
curl -X POST http://localhost:9081/api/sandbox/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "instance_id": "inst-1",
    "payload_type": "panic_fault"
  }'

# 6. Lister les rapports récents de pannes et les traces récupérées
curl "http://localhost:9081/api/sandbox/crashes?limit=10"

# 7. Enregistrer ou mettre à jour la politique de quota de sandbox d'un locataire
curl -X POST http://localhost:9081/api/sandbox/tenants \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "tenant-edge-prod",
    "max_memory_pages": 256,
    "gas_limit": 2000000,
    "max_crash_threshold": 5
  }'

# 8. Réinitialiser une instance et lever la mise en quarantaine
curl -X POST http://localhost:9081/api/sandbox/reset \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant-edge-prod", "instance_id": "inst-1"}'
```

---

## 📡 Référence de l'API REST

| Point de Terminaison | Méthode | Authentification | Description |
| :--- | :---: | :---: | :--- |
| `/api/sandbox/status` | `GET` | Public | État du superviseur de sandbox, instances actives, OOM et récupérations de paniques |
| `/api/sandbox/execute` | `POST` | Public | Exécute des charges de travail Wasm sous limites strictes de mémoire et de gaz |
| `/api/sandbox/tenants` | `GET/POST` | Public | Inspecte ou met à jour les politiques de sandbox et quotas des locataires |
| `/api/sandbox/crashes` | `GET` | Public | Journal annulaire des pannes capturées et des traces d'appels associées |
| `/api/sandbox/crashes/clear` | `POST` | Public | Vide le tampon annulaire du journal des pannes en mémoire |
| `/api/sandbox/reset` | `POST` | Public | Réinitialise une instance de sandbox dégradée vers un état IDLE prêt |
| `/api/steering/status` | `GET` | Public | État du routage eBPF HTTP/3, mode actif et métriques de débit |
| `/api/steering/endpoints` | `GET/POST/DELETE` | Public | Liste, enregistre ou supprime les points de terminaison de Workers pour routage |
| `/api/steering/rules` | `GET/POST/DELETE` | Public | Liste, ajoute ou supprime des règles de routage par préfixe d'ID de connexion QUIC |
| `/api/steering/simulate` | `POST` | Public | Simule l'inspection des en-têtes UDP/QUIC et mesure la latence en microsecondes |
| `/api/steering/stats` | `GET` | Public | Télémétrie granulaire de routage en microsecondes et ratio sub-10µs |
| `/api/rotator/status` | `GET` | Public | État du rotateur, jetons actifs, renouvellements et latence de propagation |
| `/api/rotator/tokens` | `GET` | Public | Liste tous les jetons actifs sous gestion de rotation |
| `/api/rotator/renew` | `POST` | Public | Déclenche la rotation immédiate et le renouvellement de clé de jeton |
| `/api/rotator/revoke` | `POST` | Public | Diffuse la révocation immédiate de jetons/baux sur le maillage Gossip |
| `/api/rotator/gossip` | `POST` | Public | Échange Gossip entre pairs pour les pierres tombales et l'adhésion au maillage |
| `/api/rotator/delegate` | `POST` | Public | Actualisation déléguée de jetons exigeant une attestation matérielle TPM |
| `/api/rotator/challenge` | `GET` | Public | Génère un nonce de défi à usage unique pour l'attestation matérielle |
| `/api/rotator/tombstones` | `GET` | Public | Liste les pierres tombales de révocation signées dans le grand livre local |
| `/api/rotator/peers` | `GET/POST` | Public | Liste les pairs du maillage ou enregistre un nouveau nœud Gossip |
| `/api/rotator/events` | `GET` | Public | Journal d'audit des événements récents de rotation et de révocation |
| `/api/radar/status` | `GET` | Public | État du radar, PoPs surveillés, latence globale et décomptes de migrations |
| `/api/radar/pops` | `GET` | Public | Liste les PoPs périphériques surveillés avec télémétrie (`?region=...`) |
| `/api/radar/probe` | `POST` | Public | Déclenche un sondage synthétique pour un PoP spécifique (`?colo=...`) ou tous |
| `/api/radar/route` | `GET/POST` | Public | Résolveur de routes Anycast submilliseconde (`?lat=...&lon=...&country=...`) |
| `/api/radar/events` | `GET` | Public | Anomalies BGP récentes et journaux de migrations autonomes |
| `/api/radar/migrate` | `POST` | Public | Évacue les baux actifs assignés à un PoP dégradé spécifique |
| `/api/billing/summary` | `GET` | Public | Récapitulatif des coûts du cluster, économies et agrégats par locataire/compte |
| `/api/billing/ledger` | `GET` | Public | Exportation du grand livre (`?format=json|csv|jsonl|opencost&tenant=...&limit=...`) |
| `/api/billing/meter` | `POST` | Public | Ingère et impute un enregistrement de consommation de ressources |
| `/api/billing/optimize` | `POST` | Public | Déclenche l'évaluation de migration autonome des charges de travail |
| `/api/billing/migrations` | `GET` | Public | Consulte l'historique des migrations autonomes et les économies réalisées |
| `/api/billing/rates` | `GET/POST` | Public | Consulte ou met à jour la grille tarifaire Cloudflare en microcents |
| `/api/billing/reset` | `POST` | Public | Réinitialise le grand livre de facturation en mémoire et les données agrégées |
| `/api/mtls/status` | `GET` | Public | Inspecte les statistiques de CA Racine, politiques et attestation TPM 2.0 |
| `/api/mtls/ca/cert` | `GET` | Public | Télécharge le certificat public de la CA Racine au format PEM |
| `/api/mtls/ca/issue` | `POST` | Public | Émet des certificats éphémères clients/pairs avec SANs et validité personnalisée |
| `/api/mtls/verify` | `POST` | Public | Évalue un certificat PEM X.509 par rapport à la CA et aux politiques actives |
| `/api/mtls/revoke` | `POST` | Public | Révoque un numéro de série de certificat et l'ajoute à la liste noire CRL |
| `/api/mtls/tpm/attest` | `GET` | Public | Interroge l'attestation du fournisseur TPM 2.0 Windows et le magasin cert |
| `/api/mtls/peer-token` | `POST` | Public | Génère un jeton pair signé ECDSA pour la synchronisation inter-clusters |
| `/api/mtls/peer-token/verify` | `POST` | Public | Vérifie cryptographiquement un jeton pair de synchronisation inter-clusters |
| `/api/mtls/policy` | `POST/PUT` | Public | Met à jour la politique de validation des certificats clients actifs |
| `/raft/status` | `GET` | Public | Inspecte le rôle local, terme, ID leader, index commit et télémétrie de cluster |
| `/raft/peers` | `GET/POST` | Public | Inspecte les pairs du cluster et la latence ; enregistre un nœud distant |
| `/raft/propose` | `POST` | Public | Propose une commande (`GRANT_LEASE`, `REVOKE_LEASE`, `UPDATE_QUOTA`, `SYNC_ACCOUNT`) |
| `/raft/journal` | `GET` | Public | Instantané des écritures WAL répliquées avec sommes de contrôle SHA-256 |
| `/raft/vote` | `POST` | Public | Point de terminaison RPC de vote pour les candidats aux élections Raft |
| `/raft/append` | `POST` | Public | Point de terminaison RPC de battements de cœur et réplication d'écritures Raft |
| `/metrics` | `GET` | Public | Point de collecte de métriques au format texte standard Prometheus 0.0.4 |
| `/api/compiler/compile` | `POST` | Public | Compile et minifie le code source JS d'un Worker, retourne le ratio et la syntaxe |
| `/api/compiler/workers` | `GET` | Public | Liste tous les paquets de Workers déployés avec empreintes SHA-256 |
| `/api/compiler/workers` | `POST` | Public | Empaquette et déploie un nouveau paquet de Worker pour un locataire |
| `/api/compiler/workers` | `DELETE` | Public | Désinstalle un paquet de Worker (`?id=bundle-...`) |
| `/api/compiler/routes` | `GET` | Public | Liste les routes dynamiques enregistrées par priorité décroissante |
| `/api/compiler/routes` | `POST` | Public | Enregistre un motif de route dynamique (`pattern`, `zone_id`, `tenant_id`, `worker_id`) |
| `/api/compiler/routes` | `DELETE` | Public | Supprime une association de route dynamique (`?id=route-...`) |
| `/api/compiler/routes/match` | `POST` | Public | Teste la correspondance d'un motif d'URL avec la table de routes (`host`, `path`) |
| `/api/compiler/stream-test` | `GET` | Public | Point de test de référence du flux direct zéro-copie (`?size_mb=1..50`) |
| `/api/config/status` | `GET` | **Bearer** | Télémétrie de rechargement à chaud et liste des comptes (jetons caviardés) |
| `/api/config/reload` | `POST` | **Bearer** | Déclenche le rechargement immédiat sans temps d'arrêt depuis le disque |
| `/api/quota/alerts` | `GET` | Public | Historique des alertes de franchissement de seuil de quota |
| `/api/quota/webhooks/config` | `GET/POST` | Public | Inspecte ou met à jour les adresses de webhooks Discord/Slack |
| `/api/quota/webhooks/test` | `POST` | Public | Envoie une notification de test de seuil de quota (80%/95%/100%) |
| `/api/quota/probation` | `GET` | Public | Dossiers actuels de mise à l'épreuve et temporisateurs des comptes mutualisés |
| `/api/quota/probation/recover` | `POST` | Public | Restaure manuellement un compte depuis l'état d'épreuve (`?id=...`) |
| `/api/quota/probation/trigger` | `POST` | Public | Simule le déclenchement d'épreuve suite à un dépassement 429 (`?id=...`) |
| `/api/quota/ratelimit` | `GET` | Public | Télémétrie du limiteur de débit à fenêtre glissante (`?key=...`) |
| `/api/auth/token` | `POST` | Public / Key | Émet un jeton Bearer JWT HS256 à périmètre délimité |
| `/api/auth/revoke` | `POST` | Bearer | Révoque immédiatement un jeton JWT via son identifiant JTI |
| `/api/auth/me` | `GET` | Bearer | Vérifie et inspecte les revendications du jeton de l'appelant actif |
| `/api/auth/status` | `GET` | Public | Inspecte les fournisseurs cryptographiques et l'état d'authentification |
| `/api/security/encrypt` | `POST` | Bearer | Chiffre des données sensibles dans une enveloppe DPAPI ou AES-GCM |
| `/api/security/decrypt` | `POST` | Bearer | Teste le déchiffrement aller-retour de l'enveloppe de sécurité |
| `/api/security/status` | `GET` | Bearer | Inspecte le moteur du coffre de secrets et le fournisseur actif |
| `/s3/{bucket}/{key...}` | `PUT/GET/HEAD/DELETE` | Public | Passerelle de stockage virtuel S3 avec découpage en bandes de 2 Mo |
| `/s3/{bucket}` | `GET` | Public | Liste XML ListObjectsV2 compatible S3 pour le compartiment spécifié |
| `/api/kv/{ns}/{key}` | `GET/PUT/DELETE` | Public | Point de mise en cache KV partitionné avec hachage cohérent |
| `/api/kv/stats` | `GET` | Public | Efficacité du cache, taux de réussite et métriques par locataire |
| `/proxy/{service}/*` | `ANY` | Public | Proxy inverse transparent avec basculement automatique et gestion 429 |
| `/api/accounts` | `GET` | Public | Instantané des comptes Cloudflare mutualisés et des limites restantes |
| `/api/leases` | `GET` | **Bearer** | Liste tous les baux de ressources actifs et révoqués |
| `/api/leases` | `POST` | **Bearer** | Alloue et émet un nouveau bail dynamique (`target_service`, `resource_type`) |
| `/api/leases/revoke` | `POST` | **Bearer** | Déclare immédiatement nul et révoque un bon de bail (`?id=lse_...`) |
| `/api/health` | `GET` | Public | Vérifie la latence et la connectivité avec l'API publique de Cloudflare |

---

## 🗺️ Feuille de Route (Roadmap)

Consultez [ROADMAP.md](../../ROADMAP.md) pour découvrir les jalons et fonctionnalités prévues.

---

## 🔒 Sécurité (Security)

Pour la divulgation des vulnérabilités et les procédures de signalement, veuillez vous référer à [SECURITY.md](../../SECURITY.md).

---

## 📄 Licence (License)

Distribué sous licence [Apache License 2.0](../../LICENSE).
