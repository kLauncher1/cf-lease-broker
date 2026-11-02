package enclave

import (
	"errors"
	"os"
	"time"
)

// EnclaveType defines the hardware or emulation environment.
type EnclaveType string

const (
	// EnclaveTypeSGX represents Intel Software Guard Extensions hardware enclave.
	EnclaveTypeSGX EnclaveType = "intel-sgx"
	// EnclaveTypeSEV represents AMD Secure Encrypted Virtualization hardware enclave.
	EnclaveTypeSEV EnclaveType = "amd-sev"
	// EnclaveTypeEmulated represents software-isolated confidential emulation.
	EnclaveTypeEmulated EnclaveType = "confidential-emulation"
)

// EnclaveStatus defines operational readiness.
type EnclaveStatus string

const (
	EnclaveStatusActive        EnclaveStatus = "active"
	EnclaveStatusUninitialized EnclaveStatus = "uninitialized"
	EnclaveStatusDegraded      EnclaveStatus = "degraded"
)

// SealingPolicy defines the identity scope for hardware key derivation.
type SealingPolicy string

const (
	// SealPolicyMRSigner binds secret to enclave author key (survives enclave updates).
	SealPolicyMRSigner SealingPolicy = "mrsigner"
	// SealPolicyMREnclave binds secret to exact enclave code measurement.
	SealPolicyMREnclave SealingPolicy = "mrenclave"
)

// MeasurementRegister holds cryptographic hardware digests.
type MeasurementRegister struct {
	MRENCLAVE       string `json:"mrenclave"`
	MRSIGNER        string `json:"mrsigner"`
	SEVLaunchDigest string `json:"sev_launch_digest"`
	ISVProdID       uint16 `json:"isv_prod_id"`
	ISVSVN          uint16 `json:"isv_svn"`
}

// AttestationQuote contains a verifiable hardware quote.
type AttestationQuote struct {
	QuoteID        string              `json:"quote_id"`
	EnclaveType    EnclaveType         `json:"enclave_type"`
	Timestamp      time.Time           `json:"timestamp"`
	Nonce          string              `json:"nonce"`
	UserData       string              `json:"user_data"`
	UserDataHash   string              `json:"user_data_hash"`
	Measurement    MeasurementRegister `json:"measurement"`
	TCBVersion     uint32              `json:"tcb_version"`
	Signature      string              `json:"signature"`
	PublicKey      string              `json:"public_key"`
	HardwareReport string              `json:"hardware_report"`
}

// QuoteVerificationPolicy sets criteria for remote attestation validation.
type QuoteVerificationPolicy struct {
	AllowedSigners      []string      `json:"allowed_signers"`
	MinISVSVN           uint16        `json:"min_isv_svn"`
	RequireHardwareSGX  bool          `json:"require_hardware_sgx"`
	AllowedEnclaveTypes []EnclaveType `json:"allowed_enclave_types"`
	MaxQuoteAgeSec      int64         `json:"max_quote_age_sec"`
}

// SealedBlob encapsulates hardware-encrypted confidential data.
type SealedBlob struct {
	BlobID     string        `json:"blob_id"`
	Ciphertext string        `json:"ciphertext"`
	Nonce      string        `json:"nonce"`
	Tag        string        `json:"tag"`
	Policy     SealingPolicy `json:"policy"`
	KeyID      string        `json:"key_id"`
	MRENCLAVE  string        `json:"mrenclave"`
	MRSIGNER   string        `json:"mrsigner"`
	CreatedAt  time.Time     `json:"created_at"`
}

// EnclavePage represents an individual isolated memory page.
type EnclavePage struct {
	PageID      int    `json:"page_id"`
	Address     uint64 `json:"address"`
	Size        int    `json:"size"`
	IsAllocated bool   `json:"is_allocated"`
	IsZeroed    bool   `json:"is_zeroed"`
	TenantID    string `json:"tenant_id"`
}

// EnclaveStats exposes telemetry on memory, attestation, and sealing.
type EnclaveStats struct {
	EnclaveType       EnclaveType   `json:"enclave_type"`
	Status            EnclaveStatus `json:"status"`
	EPCTotalBytes     int64         `json:"epc_total_bytes"`
	EPCAllocatedBytes int64         `json:"epc_allocated_bytes"`
	PageCount         int           `json:"page_count"`
	AllocatedPages    int           `json:"allocated_pages"`
	QuotesGenerated   int64         `json:"quotes_generated"`
	QuotesVerified    int64         `json:"quotes_verified"`
	SecretsSealed     int64         `json:"secrets_sealed"`
	SecretsUnsealed   int64         `json:"secrets_unsealed"`
	ExecutionsRun     int64         `json:"executions_run"`
	AvgLatencyUs      float64       `json:"avg_latency_us"`
}

// EnclaveConfig specifies configuration parameters for the enclave subsystem.
type EnclaveConfig struct {
	EnclaveType          EnclaveType             `json:"enclave_type"`
	EPCCapacityMB        int                     `json:"epc_capacity_mb"`
	PageSizeKB           int                     `json:"page_size_kb"`
	SignerKey            string                  `json:"signer_key"`
	HardwareRootKey      string                  `json:"hardware_root_key"`
	EnableHardwareDriver bool                    `json:"enable_hardware_driver"`
	StrictAttestation    bool                    `json:"strict_attestation"`
	DefaultPolicy        QuoteVerificationPolicy `json:"default_policy"`
}

// DefaultEnclaveConfig returns production baseline settings, configurable via environment.
func DefaultEnclaveConfig() EnclaveConfig {
	signerKey := os.Getenv("CF_ENCLAVE_SIGNER_KEY")
	if signerKey == "" {
		signerKey = "cf-lease-broker-mrsigner-production-k"
	}

	rootKey := os.Getenv("CF_ENCLAVE_ROOT_KEY")
	if rootKey == "" {
		rootKey = "cf-hardware-enclave-root-key-32b"
	}

	return EnclaveConfig{
		EnclaveType:          EnclaveTypeEmulated,
		EPCCapacityMB:        128, // 128MB EPC hardware ceiling
		PageSizeKB:           4,   // 4KB standard EPC page
		SignerKey:            signerKey,
		HardwareRootKey:      rootKey,
		EnableHardwareDriver: true,
		StrictAttestation:    false,
		DefaultPolicy: QuoteVerificationPolicy{
			AllowedSigners:      []string{signerKey},
			MinISVSVN:           1,
			RequireHardwareSGX:  false,
			AllowedEnclaveTypes: []EnclaveType{EnclaveTypeSGX, EnclaveTypeSEV, EnclaveTypeEmulated},
			MaxQuoteAgeSec:      300,
		},
	}
}

// Common error definitions.
var (
	ErrEnclaveOutOfMemory      = errors.New("enclave EPC memory exhausted")
	ErrOutOfBounds             = errors.New("enclave memory access out of bounds")
	ErrQuoteVerificationFailed = errors.New("remote attestation quote verification failed")
	ErrNonceReplayed           = errors.New("attestation nonce has already been consumed or replayed")
	ErrQuoteExpired            = errors.New("attestation quote has expired")
	ErrUnsealFailed            = errors.New("failed to unseal confidential secret: authentication failed")
	ErrInvalidPolicy           = errors.New("invalid or unsatisfied verification policy")
	ErrExecutionFailed         = errors.New("confidential worker execution faulted")
)
