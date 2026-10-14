package mtls

import (
	"time"
)

type contextKey string

const (
	// ClientCertContextKey is the context key for extracted and verified client certificate details.
	ClientCertContextKey contextKey = "mtls_client_cert"
)

// ClientCertDetails captures parsed metadata and cryptographic fingerprint from a verified X.509 certificate.
type ClientCertDetails struct {
	SubjectCommonName string    `json:"subject_common_name"`
	SubjectOrg        []string  `json:"subject_org,omitempty"`
	IssuerCommonName  string    `json:"issuer_common_name"`
	IssuerOrg         []string  `json:"issuer_org,omitempty"`
	SerialNumberHex   string    `json:"serial_number_hex"`
	NotBefore         time.Time `json:"not_before"`
	NotAfter          time.Time `json:"not_after"`
	DNSNames          []string  `json:"dns_names,omitempty"`
	IPAddresses       []string  `json:"ip_addresses,omitempty"`
	FingerprintSHA256 string    `json:"fingerprint_sha256"`
	Verified          bool      `json:"verified"`
}

// VerificationPolicy specifies access rules and constraints for mutual TLS client certificates.
type VerificationPolicy struct {
	RequireClientCert  bool          `json:"require_client_cert"`
	AllowedSANs        []string      `json:"allowed_sans,omitempty"`
	AllowedCommonNames []string      `json:"allowed_common_names,omitempty"`
	AllowedIssuers     []string      `json:"allowed_issuers,omitempty"`
	EnforceExpiry      bool          `json:"enforce_expiry"`
	AllowSelfSigned    bool          `json:"allow_self_signed"`
	MinCertValidity    time.Duration `json:"min_cert_validity,omitempty"`
}

// VerificationResult contains the verdict and audit trail for a certificate evaluation.
type VerificationResult struct {
	Valid             bool               `json:"valid"`
	Reason            string             `json:"reason"`
	Certificate       *ClientCertDetails `json:"certificate,omitempty"`
	AttestedHardware  bool               `json:"attested_hardware"`
	EvaluatedAt       time.Time          `json:"evaluated_at"`
}

// PeeringToken carries signed identity and role metadata for zero-trust inter-broker cluster communication.
type PeeringToken struct {
	BrokerID     string `json:"broker_id"`
	ClusterID    string `json:"cluster_id"`
	Role         string `json:"role"`
	IssuedAt     int64  `json:"issued_at"`
	ExpiresAt    int64  `json:"expires_at"`
	SignatureHex string `json:"signature_hex"`
}

// TPMAttestationStatus details hardware-backed security state and PCR quote measurements.
type TPMAttestationStatus struct {
	Available            bool      `json:"available"`
	Provider             string    `json:"provider"`
	PCRDigest            string    `json:"pcr_digest"`
	EndorsementKeySHA256 string    `json:"endorsement_key_sha256"`
	AttestedAt           time.Time `json:"attested_at"`
	PlatformQuote        string    `json:"platform_quote"`
	WindowsStoreSynced   bool      `json:"windows_store_synced"`
}

// IssueCertRequest defines parameters for on-demand ephemeral certificate issuance.
type IssueCertRequest struct {
	CommonName    string   `json:"common_name"`
	SANs          []string `json:"sans,omitempty"`
	ValidityHours int      `json:"validity_hours,omitempty"`
	IsPeer        bool     `json:"is_peer,omitempty"`
	BrokerID      string   `json:"broker_id,omitempty"`
	ClusterID     string   `json:"cluster_id,omitempty"`
}

// IssueCertResponse returns the generated PEM bundle and metadata.
type IssueCertResponse struct {
	CertificatePEM string    `json:"certificate_pem"`
	PrivateKeyPEM  string    `json:"private_key_pem,omitempty"`
	SerialNumber   string    `json:"serial_number"`
	ExpiresAt      time.Time `json:"expires_at"`
	Fingerprint    string    `json:"fingerprint_sha256"`
}

// VerifyCertRequest allows testing raw PEM certificates against verification policy.
type VerifyCertRequest struct {
	CertificatePEM string              `json:"certificate_pem"`
	CustomPolicy   *VerificationPolicy `json:"custom_policy,omitempty"`
}

// RevokeCertRequest targets a certificate serial number for revocation.
type RevokeCertRequest struct {
	SerialNumber string `json:"serial_number"`
	Reason       string `json:"reason,omitempty"`
}
