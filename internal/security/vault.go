package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Zeroize securely scrubs sensitive byte buffers from memory.
func Zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// CredentialSummary represents non-secret metadata for stored account credentials.
type CredentialSummary struct {
	AccountID   string    `json:"account_id"`
	Provider    string    `json:"provider"`
	EncryptedAt time.Time `json:"encrypted_at"`
}

type storedEntry struct {
	envelope    string
	provider    string
	encryptedAt time.Time
}

// SecretVault provides envelope encryption and secure credential storage.
type SecretVault struct {
	mu          sync.RWMutex
	masterKey   [32]byte
	hasMaster   bool
	credentials map[string]storedEntry
}

// NewSecretVault initializes a secret vault with optional master key derivation.
func NewSecretVault() *SecretVault {
	v := &SecretVault{
		credentials: make(map[string]storedEntry),
	}

	rawKey := os.Getenv("CF_BROKER_MASTER_KEY")
	if rawKey == "" {
		rawKey = os.Getenv("CF_SECRET_KEY")
	}

	if rawKey != "" {
		v.masterKey = sha256.Sum256([]byte(rawKey))
		v.hasMaster = true
	} else {
		// Generate an ephemeral 32-byte key for fallback AES-GCM encryption
		if _, err := io.ReadFull(rand.Reader, v.masterKey[:]); err == nil {
			v.hasMaster = true
		}
	}

	return v
}

// Encrypt encrypts plaintext bytes with DPAPI on Windows or AES-256-GCM elsewhere.
func (v *SecretVault) Encrypt(plaintext []byte, providerPreference string) (string, error) {
	if len(plaintext) == 0 {
		return "", errors.New("cannot encrypt empty payload")
	}

	provider := strings.ToLower(strings.TrimSpace(providerPreference))
	if provider == "" || provider == "auto" {
		if isDPAPIAvailable() {
			provider = "dpapi"
		} else {
			provider = "aes-gcm"
		}
	}

	switch provider {
	case "dpapi":
		if !isDPAPIAvailable() {
			return "", errors.New("DPAPI provider requested but not supported on this OS")
		}
		ciphertext, err := dpapiEncrypt(plaintext)
		if err != nil {
			return "", fmt.Errorf("dpapi encryption failed: %w", err)
		}
		return "dpapi:" + base64.StdEncoding.EncodeToString(ciphertext), nil

	case "aes-gcm":
		v.mu.RLock()
		hasKey := v.hasMaster
		key := v.masterKey
		v.mu.RUnlock()

		if !hasKey {
			return "", errors.New("AES-GCM encryption requires an active master key")
		}

		block, err := aes.NewCipher(key[:])
		if err != nil {
			return "", fmt.Errorf("aes cipher error: %w", err)
		}

		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return "", fmt.Errorf("gcm mode error: %w", err)
		}

		nonce := make([]byte, gcm.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return "", fmt.Errorf("nonce generation failed: %w", err)
		}

		sealed := gcm.Seal(nil, nonce, plaintext, nil)
		nonceB64 := base64.StdEncoding.EncodeToString(nonce)
		sealedB64 := base64.StdEncoding.EncodeToString(sealed)
		return fmt.Sprintf("aes-gcm:%s:%s", nonceB64, sealedB64), nil

	default:
		return "", fmt.Errorf("unsupported encryption provider: %s", provider)
	}
}

// Decrypt unpacks an envelope and decrypts the underlying sensitive payload.
func (v *SecretVault) Decrypt(envelope string) ([]byte, error) {
	parts := strings.SplitN(envelope, ":", 3)
	if len(parts) < 2 {
		return nil, errors.New("invalid envelope format, expected scheme:payload")
	}

	scheme := parts[0]
	switch scheme {
	case "dpapi":
		if !isDPAPIAvailable() {
			return nil, errors.New("cannot decrypt DPAPI envelope on non-Windows OS")
		}
		raw, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid base64 in dpapi envelope: %w", err)
		}
		return dpapiDecrypt(raw)

	case "aes-gcm":
		if len(parts) != 3 {
			return nil, errors.New("invalid aes-gcm envelope format, expected aes-gcm:nonce:ciphertext")
		}
		nonce, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid base64 nonce in aes-gcm envelope: %w", err)
		}
		ciphertext, err := base64.StdEncoding.DecodeString(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid base64 ciphertext in aes-gcm envelope: %w", err)
		}

		v.mu.RLock()
		key := v.masterKey
		v.mu.RUnlock()

		block, err := aes.NewCipher(key[:])
		if err != nil {
			return nil, fmt.Errorf("aes cipher error: %w", err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("gcm mode error: %w", err)
		}

		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return nil, errors.New("failed to decrypt aes-gcm envelope (ciphertext tampered or key mismatch)")
		}
		return plaintext, nil

	default:
		return nil, fmt.Errorf("unknown envelope scheme: %s", scheme)
	}
}

// StoreCredential encrypts and stores an account credential under a safe envelope.
func (v *SecretVault) StoreCredential(accountID string, secret []byte) error {
	envelope, err := v.Encrypt(secret, "auto")
	if err != nil {
		return err
	}

	provider := "aes-gcm"
	if strings.HasPrefix(envelope, "dpapi:") {
		provider = "dpapi"
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.credentials[accountID] = storedEntry{
		envelope:    envelope,
		provider:    provider,
		encryptedAt: time.Now().UTC(),
	}
	return nil
}

// RetrieveDecryptedSecret decrypts and returns the stored secret for an account.
func (v *SecretVault) RetrieveDecryptedSecret(accountID string) ([]byte, error) {
	v.mu.RLock()
	entry, exists := v.credentials[accountID]
	v.mu.RUnlock()

	if !exists {
		return nil, errors.New("no credentials registered for account: " + accountID)
	}
	return v.Decrypt(entry.envelope)
}

// ListCredentials returns non-secret metadata for all stored account credentials.
func (v *SecretVault) ListCredentials() []CredentialSummary {
	v.mu.RLock()
	defer v.mu.RUnlock()

	summaries := make([]CredentialSummary, 0, len(v.credentials))
	for id, entry := range v.credentials {
		summaries = append(summaries, CredentialSummary{
			AccountID:   id,
			Provider:    entry.provider,
			EncryptedAt: entry.encryptedAt,
		})
	}
	return summaries
}

// ServeHTTP provides REST endpoints for secret encryption and status diagnostics.
func (v *SecretVault) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/security")
	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "/encrypt" && r.Method == http.MethodPost:
		var req struct {
			Secret   string `json:"secret"`
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		secretBytes := []byte(req.Secret)
		defer Zeroize(secretBytes)

		env, err := v.Encrypt(secretBytes, req.Provider)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"envelope": env,
			"status":   "encrypted",
		})

	case path == "/decrypt" && r.Method == http.MethodPost:
		var req struct {
			Envelope string `json:"envelope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		decrypted, err := v.Decrypt(req.Envelope)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		defer Zeroize(decrypted)

		_ = json.NewEncoder(w).Encode(map[string]string{
			"secret": string(decrypted),
			"status": "decrypted",
		})

	case path == "/status" && r.Method == http.MethodGet:
		v.mu.RLock()
		credsCount := len(v.credentials)
		v.mu.RUnlock()

		activeProvider := "aes-gcm"
		if isDPAPIAvailable() {
			activeProvider = "dpapi"
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"dpapi_available":   isDPAPIAvailable(),
			"active_provider":   activeProvider,
			"stored_accounts":   credsCount,
			"zeroize_supported": true,
		})

	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}
