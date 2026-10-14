package mtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EphemeralCA manages root trust anchors, dynamic certificate issuance, and zero-trust revocation.
type EphemeralCA struct {
	mu             sync.RWMutex
	rootCert       *x509.Certificate
	rootKey        *ecdsa.PrivateKey
	rootCertPEM    []byte
	certPool       *x509.CertPool
	revokedSerials map[string]time.Time
	issuedCount    int64
	commonName     string
	createdAt      time.Time
}

// NewEphemeralCA initializes a self-signed in-memory root Certificate Authority.
func NewEphemeralCA(commonName string, validity time.Duration) (*EphemeralCA, error) {
	if commonName == "" {
		commonName = "cf-lease-broker-root-ca"
	}
	if validity <= 0 {
		validity = 365 * 24 * time.Hour
	}

	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA serial number: %w", err)
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Cloudflare Resource Broker Cluster"},
		},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	rootCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse generated CA certificate: %w", err)
	}

	rootCertPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	certPool := x509.NewCertPool()
	certPool.AddCert(rootCert)

	return &EphemeralCA{
		rootCert:       rootCert,
		rootKey:        rootKey,
		rootCertPEM:    rootCertPEM,
		certPool:       certPool,
		revokedSerials: make(map[string]time.Time),
		commonName:     commonName,
		createdAt:      now,
	}, nil
}

// IssueClientCert signs a new ephemeral X.509 client certificate for edge proxy or service authentication.
func (ca *EphemeralCA) IssueClientCert(cn string, sans []string, validity time.Duration) (*IssueCertResponse, error) {
	if cn == "" {
		return nil, errors.New("common name is required")
	}
	if validity <= 0 {
		validity = 24 * time.Hour
	}

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate client private key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: []string{"Cloudflare Lease Broker Tenant"},
		},
		NotBefore: now.Add(-1 * time.Minute),
		NotAfter:  now.Add(validity),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	for _, san := range sans {
		san = strings.TrimSpace(san)
		if san == "" {
			continue
		}
		if ip := net.ParseIP(san); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, san)
		}
	}

	ca.mu.RLock()
	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.rootCert, &clientKey.PublicKey, ca.rootKey)
	ca.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("failed to sign client certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal client private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	fp := sha256.Sum256(certDER)
	serialHex := strings.ToLower(serialNumber.Text(16))

	atomic.AddInt64(&ca.issuedCount, 1)

	return &IssueCertResponse{
		CertificatePEM: string(certPEM),
		PrivateKeyPEM:  string(keyPEM),
		SerialNumber:   serialHex,
		ExpiresAt:      template.NotAfter,
		Fingerprint:    hex.EncodeToString(fp[:]),
	}, nil
}

// RevokeCertificate adds a certificate serial number to the active CRL denylist.
func (ca *EphemeralCA) RevokeCertificate(serialHex string) error {
	serial := strings.ToLower(strings.TrimSpace(serialHex))
	if serial == "" {
		return errors.New("empty serial number")
	}

	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.revokedSerials[serial] = time.Now().UTC()
	return nil
}

// IsRevoked checks if a serial number has been registered on the revocation list.
func (ca *EphemeralCA) IsRevoked(serialHex string) bool {
	serial := strings.ToLower(strings.TrimSpace(serialHex))
	if serial == "" {
		return false
	}

	ca.mu.RLock()
	defer ca.mu.RUnlock()
	_, revoked := ca.revokedSerials[serial]
	return revoked
}

// GetRootCertPEM returns the PEM-encoded Root CA certificate for client trust stores.
func (ca *EphemeralCA) GetRootCertPEM() []byte {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	cp := make([]byte, len(ca.rootCertPEM))
	copy(cp, ca.rootCertPEM)
	return cp
}

// GetCertPool returns a copy of the x509 CertPool containing the active CA.
func (ca *EphemeralCA) GetCertPool() *x509.CertPool {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	return ca.certPool.Clone()
}

// GeneratePeeringToken issues an ECDSA-signed zero-trust token for inter-broker cluster synchronization.
func (ca *EphemeralCA) GeneratePeeringToken(brokerID, clusterID, role string, ttl time.Duration) (*PeeringToken, error) {
	if brokerID == "" {
		brokerID = "broker-node-primary"
	}
	if clusterID == "" {
		clusterID = "cluster-default"
	}
	if role == "" {
		role = "peer"
	}
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}

	now := time.Now().UTC()
	issuedAt := now.Unix()
	expiresAt := now.Add(ttl).Unix()

	payload := fmt.Sprintf("%s:%s:%s:%d:%d", brokerID, clusterID, role, issuedAt, expiresAt)
	hash := sha256.Sum256([]byte(payload))

	ca.mu.RLock()
	sig, err := ecdsa.SignASN1(rand.Reader, ca.rootKey, hash[:])
	ca.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("failed to sign peering token: %w", err)
	}

	return &PeeringToken{
		BrokerID:     brokerID,
		ClusterID:    clusterID,
		Role:         role,
		IssuedAt:     issuedAt,
		ExpiresAt:    expiresAt,
		SignatureHex: hex.EncodeToString(sig),
	}, nil
}

// VerifyPeeringToken validates cryptographic integrity and expiration of a peering voucher.
func (ca *EphemeralCA) VerifyPeeringToken(token *PeeringToken) (bool, string) {
	if token == nil {
		return false, "nil token"
	}
	now := time.Now().UTC().Unix()
	if token.ExpiresAt < now {
		return false, "token expired"
	}
	if token.IssuedAt > now+300 {
		return false, "token issued in the future"
	}

	sigBytes, err := hex.DecodeString(token.SignatureHex)
	if err != nil {
		return false, "invalid signature encoding"
	}

	payload := fmt.Sprintf("%s:%s:%s:%d:%d", token.BrokerID, token.ClusterID, token.Role, token.IssuedAt, token.ExpiresAt)
	hash := sha256.Sum256([]byte(payload))

	ca.mu.RLock()
	pubKey := &ca.rootKey.PublicKey
	ca.mu.RUnlock()

	if !ecdsa.VerifyASN1(pubKey, hash[:], sigBytes) {
		return false, "signature mismatch"
	}

	return true, "valid"
}

// Stats returns operational metrics for the CA.
func (ca *EphemeralCA) Stats() map[string]interface{} {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	return map[string]interface{}{
		"ca_common_name": ca.commonName,
		"created_at":     ca.createdAt.Format(time.RFC3339),
		"issued_certs":   atomic.LoadInt64(&ca.issuedCount),
		"revoked_count":  len(ca.revokedSerials),
		"algorithm":      "ECDSA-P256-SHA256",
	}
}
