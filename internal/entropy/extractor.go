package entropy

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
)

var (
	// DefaultExtractorSalt is a 32-byte fixed salt used when no domain-specific salt is provided.
	DefaultExtractorSalt = []byte("cf-quantum-entropy-extractor-v1\x00")
)

// CryptographicExtractor transforms min-entropy raw physical samples into uniform random seeds
// using HKDF-SHA256 per the Leftover Hash Lemma (LHL).
type CryptographicExtractor struct {
	salt []byte
}

// NewCryptographicExtractor instantiates an extractor with an optional custom domain salt.
func NewCryptographicExtractor(salt []byte) *CryptographicExtractor {
	s := salt
	if len(s) == 0 {
		s = DefaultExtractorSalt
	}
	return &CryptographicExtractor{
		salt: append([]byte(nil), s...),
	}
}

// HKDFExtract implements the RFC 5869 HKDF-Extract step: PRK = HMAC-SHA256(salt, IKM).
func HKDFExtract(salt, ikm []byte) []byte {
	if len(salt) == 0 {
		salt = DefaultExtractorSalt
	}
	mac := hmac.New(sha256.New, salt)
	mac.Write(ikm)
	return mac.Sum(nil)
}

// HKDFExpand implements the RFC 5869 HKDF-Expand step: OKM = T(1) || T(2) || ... || T(N).
// Intermediate buffers are immediately zeroized upon completion.
func HKDFExpand(prk, info []byte, outLen int) ([]byte, error) {
	if outLen <= 0 {
		return nil, errors.New("entropy extractor: output length must be positive")
	}
	if outLen > 255*32 {
		return nil, fmt.Errorf("entropy extractor: requested length %d exceeds HKDF maximum (8160 bytes)", outLen)
	}

	okm := make([]byte, outLen)
	var tPrev []byte
	var generated int

	blockCount := (outLen + 31) / 32
	for i := 1; i <= blockCount; i++ {
		mac := hmac.New(sha256.New, prk)
		if len(tPrev) > 0 {
			mac.Write(tPrev)
			Zeroize(tPrev)
		}
		if len(info) > 0 {
			mac.Write(info)
		}
		mac.Write([]byte{byte(i)})
		tPrev = mac.Sum(nil)

		toCopy := 32
		if generated+toCopy > outLen {
			toCopy = outLen - generated
		}
		copy(okm[generated:], tPrev[:toCopy])
		generated += toCopy
	}

	if len(tPrev) > 0 {
		Zeroize(tPrev)
	}

	return okm, nil
}

// Extract extracts and expands cryptographically uniform randomness from raw min-entropy samples.
// Intermediate PRK buffers are securely zeroized.
func (e *CryptographicExtractor) Extract(rawSamples []byte, info string, outLen int) ([]byte, error) {
	if len(rawSamples) == 0 {
		return nil, errors.New("entropy extractor: raw samples buffer cannot be empty")
	}
	if outLen <= 0 {
		outLen = 32
	}

	// 1. HKDF-Extract to obtain uniform Pseudorandom Key (PRK)
	prk := HKDFExtract(e.salt, rawSamples)
	defer Zeroize(prk)

	// 2. HKDF-Expand to produce requested output length
	infoBytes := []byte(info)
	okm, err := HKDFExpand(prk, infoBytes, outLen)
	if err != nil {
		return nil, err
	}

	return okm, nil
}

// ExtractUniformSeed is a convenience method returning a 32-byte cryptographic seed.
func (e *CryptographicExtractor) ExtractUniformSeed(rawSamples []byte, contextInfo string) ([32]byte, error) {
	var out [32]byte
	bytes, err := e.Extract(rawSamples, contextInfo, 32)
	if err != nil {
		return out, err
	}
	copy(out[:], bytes)
	Zeroize(bytes)
	return out, nil
}
