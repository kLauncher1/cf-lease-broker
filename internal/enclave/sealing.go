package enclave

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

// SecretSealer performs hardware-rooted secret sealing and unsealing using AES-256-GCM.
type SecretSealer struct {
	hardwareRootKey []byte
	mrenclave       string
	mrsigner        string
}

// NewSecretSealer constructs a sealing engine bound to hardware keys and measurements.
func NewSecretSealer(rootKey string, mrenclave, mrsigner string) *SecretSealer {
	h := sha256.Sum256([]byte(rootKey))
	return &SecretSealer{
		hardwareRootKey: h[:],
		mrenclave:       mrenclave,
		mrsigner:        mrsigner,
	}
}

// deriveSealingKey derives a 32-byte key via HKDF-Expand with policy binding.
func (ss *SecretSealer) deriveSealingKey(policy SealingPolicy, keyID string) ([]byte, error) {
	// Salt & info binding
	var info []byte
	switch policy {
	case SealPolicyMREnclave:
		info = []byte(fmt.Sprintf("cf-enclave-seal|mrenclave=%s|keyid=%s", ss.mrenclave, keyID))
	case SealPolicyMRSigner:
		fallthrough
	default:
		policy = SealPolicyMRSigner
		info = []byte(fmt.Sprintf("cf-enclave-seal|mrsigner=%s|keyid=%s", ss.mrsigner, keyID))
	}

	// HKDF-Extract: PRK = HMAC-SHA256(hardwareRootKey, "enclave-sealing-prk-v1")
	prkMac := hmac.New(sha256.New, ss.hardwareRootKey)
	prkMac.Write([]byte("enclave-sealing-prk-v1"))
	prk := prkMac.Sum(nil)
	defer Zeroize(prk)

	// HKDF-Expand: OKM = HMAC-SHA256(PRK, info || 0x01)
	expandMac := hmac.New(sha256.New, prk)
	expandMac.Write(info)
	expandMac.Write([]byte{0x01})
	okm := expandMac.Sum(nil)
	defer Zeroize(okm)

	derived := make([]byte, 32)
	copy(derived, okm[:32])
	return derived, nil
}

// Seal encrypts sensitive bytes (e.g. API tokens or private keys) with authenticated AES-GCM.
func (ss *SecretSealer) Seal(plaintext []byte, policy SealingPolicy, keyID string) (*SealedBlob, error) {
	if keyID == "" {
		keyID = "default"
	}
	if policy != SealPolicyMREnclave {
		policy = SealPolicyMRSigner
	}

	key, err := ss.deriveSealingKey(policy, keyID)
	if err != nil {
		return nil, err
	}
	defer Zeroize(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Additional authenticated data (AAD) binding policy, keyID, and measurements
	aad := []byte(fmt.Sprintf("policy=%s;keyid=%s;mrsigner=%s;mrenclave=%s",
		policy, keyID, ss.mrsigner, ss.mrenclave))

	sealed := gcm.Seal(nil, nonce, plaintext, aad)

	// Split tag from ciphertext (AES-GCM appends 16-byte tag at the end)
	tagSize := 16
	if len(sealed) < tagSize {
		return nil, fmt.Errorf("sealed ciphertext too short")
	}
	ciphertext := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]

	blobIDBytes := make([]byte, 16)
	_, _ = rand.Read(blobIDBytes)

	return &SealedBlob{
		BlobID:     "blob-" + hex.EncodeToString(blobIDBytes),
		Ciphertext: hex.EncodeToString(ciphertext),
		Nonce:      hex.EncodeToString(nonce),
		Tag:        hex.EncodeToString(tag),
		Policy:     policy,
		KeyID:      keyID,
		MRENCLAVE:  ss.mrenclave,
		MRSIGNER:   ss.mrsigner,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// Unseal authenticates and decrypts a SealedBlob back into plaintext.
func (ss *SecretSealer) Unseal(blob *SealedBlob) ([]byte, error) {
	if blob == nil {
		return nil, ErrUnsealFailed
	}

	// Measurement validation based on policy
	if blob.Policy == SealPolicyMREnclave && blob.MRENCLAVE != ss.mrenclave {
		return nil, fmt.Errorf("%w: MRENCLAVE mismatch between sealer and caller", ErrUnsealFailed)
	}
	if blob.Policy == SealPolicyMRSigner && blob.MRSIGNER != ss.mrsigner {
		return nil, fmt.Errorf("%w: MRSIGNER mismatch between sealer and caller", ErrUnsealFailed)
	}

	key, err := ss.deriveSealingKey(blob.Policy, blob.KeyID)
	if err != nil {
		return nil, err
	}
	defer Zeroize(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce, err := hex.DecodeString(blob.Nonce)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("%w: invalid nonce encoding", ErrUnsealFailed)
	}

	ciphertext, err := hex.DecodeString(blob.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ciphertext encoding", ErrUnsealFailed)
	}
	defer Zeroize(ciphertext)

	tag, err := hex.DecodeString(blob.Tag)
	if err != nil || len(tag) != 16 {
		return nil, fmt.Errorf("%w: invalid authentication tag", ErrUnsealFailed)
	}
	defer Zeroize(tag)

	// Reassemble full ciphertext + tag for Go standard cipher.AEAD Open
	fullSealed := make([]byte, len(ciphertext)+len(tag))
	copy(fullSealed, ciphertext)
	copy(fullSealed[len(ciphertext):], tag)
	defer Zeroize(fullSealed)

	aad := []byte(fmt.Sprintf("policy=%s;keyid=%s;mrsigner=%s;mrenclave=%s",
		blob.Policy, blob.KeyID, blob.MRSIGNER, blob.MRENCLAVE))

	plaintext, err := gcm.Open(nil, nonce, fullSealed, aad)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsealFailed, err)
	}

	return plaintext, nil
}
