package mtls

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNoClientCert is returned when no mTLS peer certificate or edge header is provided.
	ErrNoClientCert = errors.New("no client certificate found in TLS connection or forwarding headers")
)

// CertVerifier evaluates client certificates against configurable zero-trust security policies.
type CertVerifier struct {
	ca     *EphemeralCA
	mu     sync.RWMutex
	policy VerificationPolicy
}

// NewCertVerifier constructs a certificate verifier bound to an EphemeralCA.
func NewCertVerifier(ca *EphemeralCA, defaultPolicy VerificationPolicy) *CertVerifier {
	return &CertVerifier{
		ca:     ca,
		policy: defaultPolicy,
	}
}

// SetPolicy updates the active verification policy safely under mutex.
func (cv *CertVerifier) SetPolicy(policy VerificationPolicy) {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	cv.policy = policy
}

// GetPolicy returns the currently active verification policy.
func (cv *CertVerifier) GetPolicy() VerificationPolicy {
	cv.mu.RLock()
	defer cv.mu.RUnlock()
	return cv.policy
}

// VerifyCertificate validates an X.509 certificate against CA trust and policy rules.
func (cv *CertVerifier) VerifyCertificate(cert *x509.Certificate, policyOverride ...VerificationPolicy) VerificationResult {
	now := time.Now().UTC()
	result := VerificationResult{
		EvaluatedAt: now,
	}

	if cert == nil {
		result.Valid = false
		result.Reason = "nil certificate provided"
		return result
	}

	policy := cv.GetPolicy()
	if len(policyOverride) > 0 {
		policy = policyOverride[0]
	}

	details := extractDetails(cert)
	result.Certificate = details

	// 1. Expiry and Validity Window Check
	if now.Before(cert.NotBefore) {
		result.Valid = false
		result.Reason = fmt.Sprintf("certificate is not yet valid (notBefore: %s)", cert.NotBefore.Format(time.RFC3339))
		return result
	}
	if now.After(cert.NotAfter) {
		result.Valid = false
		result.Reason = fmt.Sprintf("certificate has expired (notAfter: %s)", cert.NotAfter.Format(time.RFC3339))
		return result
	}
	if policy.MinCertValidity > 0 && cert.NotAfter.Sub(now) < policy.MinCertValidity {
		result.Valid = false
		result.Reason = fmt.Sprintf("certificate remaining validity %s is below minimum required %s", cert.NotAfter.Sub(now), policy.MinCertValidity)
		return result
	}

	// 2. Revocation Check
	if cv.ca != nil && cv.ca.IsRevoked(details.SerialNumberHex) {
		result.Valid = false
		result.Reason = fmt.Sprintf("certificate serial %s is revoked", details.SerialNumberHex)
		return result
	}

	// 3. Cryptographic Chain Verification against Root CA
	if cv.ca != nil && !policy.AllowSelfSigned {
		opts := x509.VerifyOptions{
			Roots:     cv.ca.GetCertPool(),
			CurrentTime: now,
			KeyUsages: []x509.ExtKeyUsage{
				x509.ExtKeyUsageClientAuth,
				x509.ExtKeyUsageAny,
			},
		}
		if _, err := cert.Verify(opts); err != nil {
			result.Valid = false
			result.Reason = fmt.Sprintf("trust chain verification failed: %v", err)
			return result
		}
	}

	// 4. Allowed Common Name Enforcement
	if len(policy.AllowedCommonNames) > 0 {
		matchedCN := false
		for _, acn := range policy.AllowedCommonNames {
			if strings.EqualFold(acn, cert.Subject.CommonName) || acn == "*" {
				matchedCN = true
				break
			}
		}
		if !matchedCN {
			result.Valid = false
			result.Reason = fmt.Sprintf("common name '%s' not permitted by policy", cert.Subject.CommonName)
			return result
		}
	}

	// 5. Allowed Issuer Enforcement
	if len(policy.AllowedIssuers) > 0 {
		matchedIssuer := false
		for _, ai := range policy.AllowedIssuers {
			if strings.EqualFold(ai, cert.Issuer.CommonName) || ai == "*" {
				matchedIssuer = true
				break
			}
		}
		if !matchedIssuer {
			result.Valid = false
			result.Reason = fmt.Sprintf("issuer '%s' not in allowed list", cert.Issuer.CommonName)
			return result
		}
	}

	// 6. Subject Alternative Names (SAN) Validation
	if len(policy.AllowedSANs) > 0 {
		matchedSAN := false
		allCertSANs := append([]string{}, cert.DNSNames...)
		for _, ip := range cert.IPAddresses {
			allCertSANs = append(allCertSANs, ip.String())
		}

		for _, allowedPattern := range policy.AllowedSANs {
			for _, san := range allCertSANs {
				if matchesSAN(allowedPattern, san) {
					matchedSAN = true
					break
				}
			}
			if matchedSAN {
				break
			}
		}

		if !matchedSAN {
			result.Valid = false
			result.Reason = fmt.Sprintf("certificate SANs %v do not match allowed patterns %v", allCertSANs, policy.AllowedSANs)
			return result
		}
	}

	details.Verified = true
	result.Valid = true
	result.Reason = "certificate verified successfully"
	return result
}

// VerifyPEM parses and validates a PEM-encoded X.509 certificate.
func (cv *CertVerifier) VerifyPEM(certPEM []byte, policyOverride ...VerificationPolicy) VerificationResult {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return VerificationResult{
			Valid:       false,
			Reason:      "failed to decode valid CERTIFICATE PEM block",
			EvaluatedAt: time.Now().UTC(),
		}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return VerificationResult{
			Valid:       false,
			Reason:      fmt.Sprintf("failed to parse certificate: %v", err),
			EvaluatedAt: time.Now().UTC(),
		}
	}

	return cv.VerifyCertificate(cert, policyOverride...)
}

// ExtractCertFromRequest retrieves client certificates from direct TLS connections or edge forwarding headers.
func (cv *CertVerifier) ExtractCertFromRequest(r *http.Request) (*x509.Certificate, error) {
	// 1. Direct TLS connection peer certificates
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		return r.TLS.PeerCertificates[0], nil
	}

	// 2. Cloudflare / Edge forwarding headers: X-Client-Cert-Pem
	if pemHeader := r.Header.Get("X-Client-Cert-Pem"); pemHeader != "" {
		unescaped, err := url.PathUnescape(pemHeader)
		if err == nil && strings.Contains(unescaped, "-----BEGIN CERTIFICATE-----") {
			pemHeader = unescaped
		}
		block, _ := pem.Decode([]byte(pemHeader))
		if block != nil {
			return x509.ParseCertificate(block.Bytes)
		}
	}

	// 3. X-Forwarded-Client-Cert (Envoy / RFC format: Cert="..."; ...)
	if xfcc := r.Header.Get("X-Forwarded-Client-Cert"); xfcc != "" {
		if strings.Contains(xfcc, "Cert=\"") {
			start := strings.Index(xfcc, "Cert=\"") + 6
			end := strings.Index(xfcc[start:], "\"")
			if end > 0 {
				certData := xfcc[start : start+end]
				if unescaped, err := url.QueryUnescape(certData); err == nil {
					block, _ := pem.Decode([]byte(unescaped))
					if block != nil {
						return x509.ParseCertificate(block.Bytes)
					}
				}
			}
		}
	}

	// 4. Base64 DER header: X-Client-Cert-Der
	if derHeader := r.Header.Get("X-Client-Cert-Der"); derHeader != "" {
		derBytes, err := base64.StdEncoding.DecodeString(derHeader)
		if err == nil {
			return x509.ParseCertificate(derBytes)
		}
	}

	return nil, ErrNoClientCert
}

func extractDetails(cert *x509.Certificate) *ClientCertDetails {
	fp := sha256.Sum256(cert.Raw)
	var ips []string
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}

	return &ClientCertDetails{
		SubjectCommonName: cert.Subject.CommonName,
		SubjectOrg:        cert.Subject.Organization,
		IssuerCommonName:  cert.Issuer.CommonName,
		IssuerOrg:         cert.Issuer.Organization,
		SerialNumberHex:   strings.ToLower(cert.SerialNumber.Text(16)),
		NotBefore:         cert.NotBefore,
		NotAfter:          cert.NotAfter,
		DNSNames:          cert.DNSNames,
		IPAddresses:       ips,
		FingerprintSHA256: hex.EncodeToString(fp[:]),
		Verified:          false,
	}
}

func matchesSAN(pattern, value string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	value = strings.ToLower(strings.TrimSpace(value))

	if pattern == "*" || pattern == value {
		return true
	}

	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // e.g. .cluster.local
		if strings.HasSuffix(value, suffix) && len(value) > len(suffix) {
			prefix := value[:len(value)-len(suffix)]
			return !strings.Contains(prefix, ".")
		}
	}

	if ip := net.ParseIP(value); ip != nil {
		if _, cidr, err := net.ParseCIDR(pattern); err == nil {
			return cidr.Contains(ip)
		}
	}

	return false
}
