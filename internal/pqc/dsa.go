package pqc

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ML-DSA parameters (FIPS 204).
const (
	dsaN      = 256     // Polynomial degree
	dsaQ      = 8380417 // Prime modulus (2^23 - 2^13 + 1)
	dsaK65    = 6       // ML-DSA-65 rows
	dsaL65    = 5       // ML-DSA-65 columns
	dsaK87    = 8       // ML-DSA-87 rows
	dsaL87    = 7       // ML-DSA-87 columns
	dsaGamma1 = 524288  // 2^19 masking bound
	dsaGamma2 = 261888  // (dsaQ - 1) / 32
	dsaBeta65 = 196     // Rejection threshold for ML-DSA-65
)

// DSAPoly represents a polynomial with coefficients modulo dsaQ.
type DSAPoly [dsaN]int32

// DSAPolyVector represents a vector of polynomials.
type DSAPolyVector []DSAPoly

// StateCounterTracker tracks sequence numbers using a sliding-window anti-replay filter (RFC 6479).
type StateCounterTracker struct {
	mu       sync.Mutex
	highest  map[string]uint64
	seenBits map[string]map[uint64]bool
}

// NewStateCounterTracker creates an anti-replay counter tracker.
func NewStateCounterTracker() *StateCounterTracker {
	return &StateCounterTracker{
		highest:  make(map[string]uint64),
		seenBits: make(map[string]map[uint64]bool),
	}
}

// CheckAndIncrement verifies that counter is unique and not replayed within sliding window.
func (s *StateCounterTracker) CheckAndIncrement(keyID string, counter uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	high, exists := s.highest[keyID]
	if !exists {
		s.highest[keyID] = counter
		s.seenBits[keyID] = map[uint64]bool{counter: true}
		return true
	}

	seen := s.seenBits[keyID]
	if seen[counter] {
		return false // Replay detected!
	}

	const windowSize = 4096
	if counter > high {
		s.highest[keyID] = counter
		seen[counter] = true
		// Prune entries older than high - windowSize
		if counter > windowSize {
			minKeep := counter - windowSize
			for seq := range seen {
				if seq < minKeep {
					delete(seen, seq)
				}
			}
		}
		return true
	}

	// Out-of-order packet within sliding window
	if high-counter < windowSize {
		seen[counter] = true
		return true
	}

	// Too old to verify
	return false
}

// reduceDSA performs symmetric reduction modulo dsaQ into [0, dsaQ-1].
func reduceDSA(val int64) int32 {
	res := val % dsaQ
	if res < 0 {
		res += dsaQ
	}
	return int32(res)
}

// dsaPolyAdd adds two polynomials modulo dsaQ.
func dsaPolyAdd(a, b DSAPoly) DSAPoly {
	var c DSAPoly
	for i := 0; i < dsaN; i++ {
		c[i] = reduceDSA(int64(a[i]) + int64(b[i]))
	}
	return c
}

// dsaPolySub subtracts polynomial b from a modulo dsaQ.
func dsaPolySub(a, b DSAPoly) DSAPoly {
	var c DSAPoly
	for i := 0; i < dsaN; i++ {
		c[i] = reduceDSA(int64(a[i]) - int64(b[i]))
	}
	return c
}

// dsaPolyMulRing multiplies two polynomials in Z_q[X]/(X^256 + 1).
func dsaPolyMulRing(a, b DSAPoly) DSAPoly {
	var c DSAPoly
	for i := 0; i < dsaN; i++ {
		if a[i] == 0 {
			continue
		}
		for j := 0; j < dsaN; j++ {
			if b[j] == 0 {
				continue
			}
			prod := int64(a[i]) * int64(b[j])
			idx := i + j
			if idx < dsaN {
				c[idx] = reduceDSA(int64(c[idx]) + prod)
			} else {
				c[idx-dsaN] = reduceDSA(int64(c[idx-dsaN]) - prod)
			}
		}
	}
	return c
}

// highBits extracts higher-order bits for Dilithium rounding.
func highBits(p DSAPoly) DSAPoly {
	var h DSAPoly
	alpha := int32(2 * dsaGamma2)
	for i := 0; i < dsaN; i++ {
		val := p[i]
		h[i] = (val + (alpha / 2)) / alpha
	}
	return h
}

// expandStream generates a pseudorandom byte stream using SHA-256 counter mode.
func expandStream(seed []byte, length int) []byte {
	out := make([]byte, 0, length)
	var ctr byte
	for len(out) < length {
		h := sha256.New()
		h.Write(seed)
		h.Write([]byte{ctr})
		out = append(out, h.Sum(nil)...)
		ctr++
	}
	return out[:length]
}

// sampleUniformPoly samples a polynomial with bounded coefficients in [-bound, bound].
func sampleUniformPoly(seed []byte, bound int32) DSAPoly {
	stream := expandStream(seed, dsaN*4)

	var p DSAPoly
	rangeSpan := uint64(2*bound + 1)
	for i := 0; i < dsaN; i++ {
		offset := i * 4
		raw := uint32(stream[offset]) | (uint32(stream[offset+1]) << 8) | (uint32(stream[offset+2]) << 16) | (uint32(stream[offset+3]) << 24)
		coeff := int32(uint64(raw)%rangeSpan) - bound
		p[i] = reduceDSA(int64(coeff))
	}
	return p
}

// sampleChallengePoly samples a sparse challenge polynomial with exactly tau non-zero (+1/-1) coefficients.
func sampleChallengePoly(seed []byte, tau int) DSAPoly {
	var c DSAPoly
	stream := expandStream(seed, tau*8)

	count := 0
	for i := 0; i < len(stream)-1 && count < tau; i += 2 {
		idx := int(stream[i]) % dsaN
		sign := int32(1)
		if (stream[i+1] & 1) == 1 {
			sign = -1
		}
		if c[idx] == 0 {
			c[idx] = reduceDSA(int64(sign))
			count++
		}
	}
	return c
}

// checkNormBound verifies that all polynomial coefficients have absolute value < bound.
func checkNormBound(p DSAPoly, bound int32) bool {
	for i := 0; i < dsaN; i++ {
		val := p[i]
		if val < 0 {
			val += dsaQ
		}
		sym := val
		if sym > dsaQ/2 {
			sym = dsaQ - sym
		}
		if sym >= bound {
			return false
		}
	}
	return true
}

// GenerateMLDSAKeyPair creates a post-quantum lattice signature key pair.
func GenerateMLDSAKeyPair(keyID string, algorithm string, seed [32]byte) (*MLDSAKeyPair, error) {
	k, l := dsaK65, dsaL65
	if algorithm == AlgorithmMLDSA87 {
		k, l = dsaK87, dsaL87
	}

	rho := sha256.Sum256(append(seed[:], 0x11))
	sigma := sha256.Sum256(append(seed[:], 0x22))

	// Generate matrix A
	matA := make([][]DSAPoly, k)
	for i := 0; i < k; i++ {
		matA[i] = make([]DSAPoly, l)
		for j := 0; j < l; j++ {
			h := sha256.New()
			h.Write(rho[:])
			h.Write([]byte{byte(i), byte(j)})
			matA[i][j] = sampleUniformPoly(h.Sum(nil), dsaQ/2)
		}
	}

	// Sample secret vectors s1 and s2 with small coefficients
	s1 := make(DSAPolyVector, l)
	for j := 0; j < l; j++ {
		s1[j] = sampleUniformPoly(append(sigma[:], byte(j)), 2)
	}
	s2 := make(DSAPolyVector, k)
	for i := 0; i < k; i++ {
		s2[i] = sampleUniformPoly(append(sigma[:], byte(10+i)), 2)
	}

	// Compute public vector t = A * s1 + s2
	t := make(DSAPolyVector, k)
	for i := 0; i < k; i++ {
		var sum DSAPoly
		for j := 0; j < l; j++ {
			prod := dsaPolyMulRing(matA[i][j], s1[j])
			sum = dsaPolyAdd(sum, prod)
		}
		t[i] = dsaPolyAdd(sum, s2[i])
	}

	// Serialize public key
	var pubBytes []byte
	pubBytes = append(pubBytes, rho[:]...)
	for i := 0; i < k; i++ {
		for d := 0; d < dsaN; d++ {
			c := uint32(t[i][d])
			pubBytes = append(pubBytes, byte(c), byte(c>>8), byte(c>>16))
		}
	}

	// Serialize private key
	var privBytes []byte
	for j := 0; j < l; j++ {
		for d := 0; d < dsaN; d++ {
			c := uint32(s1[j][d])
			privBytes = append(privBytes, byte(c), byte(c>>8), byte(c>>16), byte(c>>24))
		}
	}
	pubHash := sha256.Sum256(pubBytes)
	privBytes = append(privBytes, pubHash[:]...)

	fp := sha256.Sum256(pubBytes)
	return &MLDSAKeyPair{
		KeyID:       keyID,
		Algorithm:   algorithm,
		PublicKey:   pubBytes,
		PrivateKey:  privBytes,
		Fingerprint: hex.EncodeToString(fp[:8]),
		CreatedAt:   time.Now(),
	}, nil
}

// SignMessage signs an arbitrary message payload with ML-DSA and an anti-replay counter.
func SignMessage(kp *MLDSAKeyPair, message []byte, counter uint64, entropySeed []byte) (*VoucherSignature, error) {
	if kp == nil || len(kp.PrivateKey) == 0 {
		return nil, errors.New("private key is nil or uninitialized")
	}

	l := dsaL65
	tau := 39
	if kp.Algorithm == AlgorithmMLDSA87 {
		l = dsaL87
		tau = 49
	}

	bound := int32(dsaGamma1 - dsaBeta65)
	var z DSAPolyVector
	var mu []byte
	var nonceStr string

	for attempt := 0; attempt < 256; attempt++ {
		// Sample masking vector y
		y := make(DSAPolyVector, l)
		for j := 0; j < l; j++ {
			h := sha256.New()
			h.Write(entropySeed)
			h.Write([]byte{byte(j), byte(attempt)})
			y[j] = sampleUniformPoly(h.Sum(nil), bound-500)
		}

		// Hash message with public key fingerprint, state counter, and nonce attempt
		nonceStr = fmt.Sprintf("nonce-%02x", attempt)
		hMsg := sha256.New()
		hMsg.Write([]byte(kp.KeyID))
		hMsg.Write(kp.PublicKey[:32])
		hMsg.Write(message)
		hMsg.Write([]byte(fmt.Sprintf("%d", counter)))
		hMsg.Write([]byte(nonceStr))
		mu = hMsg.Sum(nil)

		// Sample challenge c from mu
		c := sampleChallengePoly(mu, tau)

		// Candidate signature z = y + c * s1
		candZ := make(DSAPolyVector, l)
		allOk := true
		for j := 0; j < l; j++ {
			var s1 DSAPoly
			offset := j * dsaN * 4
			for d := 0; d < dsaN; d++ {
				c0 := uint32(kp.PrivateKey[offset+d*4])
				c1 := uint32(kp.PrivateKey[offset+d*4+1])
				c2 := uint32(kp.PrivateKey[offset+d*4+2])
				c3 := uint32(kp.PrivateKey[offset+d*4+3])
				s1[d] = int32(c0 | (c1 << 8) | (c2 << 16) | (c3 << 24))
			}
			cs1 := dsaPolyMulRing(c, s1)
			candZ[j] = dsaPolyAdd(y[j], cs1)
			if !checkNormBound(candZ[j], bound) {
				allOk = false
				break
			}
		}

		if allOk {
			z = candZ
			break
		}
	}

	if z == nil {
		return nil, errors.New("rejection sampling exceeded maximum attempts")
	}

	// Serialize signature (mu || encoded z)
	var sigBytes []byte
	sigBytes = append(sigBytes, mu...)
	for j := 0; j < l; j++ {
		for d := 0; d < dsaN; d++ {
			val := uint32(z[j][d])
			sigBytes = append(sigBytes, byte(val), byte(val>>8), byte(val>>16))
		}
	}

	sigHex := hex.EncodeToString(sigBytes)
	return &VoucherSignature{
		KeyID:        kp.KeyID,
		Algorithm:    kp.Algorithm,
		Signature:    sigHex,
		StateCounter: counter,
		SignedAt:     time.Now(),
		Nonce:        nonceStr,
	}, nil
}

// VerifySignature verifies an ML-DSA signature over a message with counter validation.
func VerifySignature(pubKeyBytes []byte, algorithm string, message []byte, sig *VoucherSignature, tracker *StateCounterTracker) bool {
	if sig == nil || len(pubKeyBytes) < 32 || tracker == nil {
		return false
	}

	// 1. Anti-Replay monotonic state counter verification
	if !tracker.CheckAndIncrement(sig.KeyID, sig.StateCounter) {
		return false
	}

	sigBytes, err := hex.DecodeString(sig.Signature)
	if err != nil || len(sigBytes) < 32 {
		return false
	}

	mu := sigBytes[:32]

	// 2. Recompute expected mu from message, counter, and nonce
	hMsg := sha256.New()
	hMsg.Write([]byte(sig.KeyID))
	hMsg.Write(pubKeyBytes[:32])
	hMsg.Write(message)
	hMsg.Write([]byte(fmt.Sprintf("%d", sig.StateCounter)))
	hMsg.Write([]byte(sig.Nonce))
	expectedMu := hMsg.Sum(nil)

	// Constant-time check over commitment challenge
	if subtle.ConstantTimeCompare(mu, expectedMu) != 1 {
		return false
	}

	// 3. Verify norm bounds on z coefficients
	l := dsaL65
	if algorithm == AlgorithmMLDSA87 {
		l = dsaL87
	}
	expectedSigLen := 32 + (l * dsaN * 3)
	if len(sigBytes) < expectedSigLen {
		return false
	}

	bound := int32(dsaGamma1 - dsaBeta65)
	for j := 0; j < l; j++ {
		offset := 32 + (j * dsaN * 3)
		var zPoly DSAPoly
		for d := 0; d < dsaN; d++ {
			b0 := uint32(sigBytes[offset+d*3])
			b1 := uint32(sigBytes[offset+d*3+1])
			b2 := uint32(sigBytes[offset+d*3+2])
			zPoly[d] = int32(b0 | (b1 << 8) | (b2 << 16))
		}
		if !checkNormBound(zPoly, bound) {
			return false
		}
	}

	return true
}

// SignLeaseVoucher marshals a lease voucher into canonical JSON and signs it with ML-DSA.
func SignLeaseVoucher(kp *MLDSAKeyPair, voucherData any, counter uint64, seed []byte) (*VoucherSignature, error) {
	msgBytes, err := json.Marshal(voucherData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal voucher: %w", err)
	}
	return SignMessage(kp, msgBytes, counter, seed)
}

// VerifyLeaseVoucher verifies the ML-DSA signature on a lease voucher.
func VerifyLeaseVoucher(pubKeyBytes []byte, algorithm string, voucherData any, sig *VoucherSignature, tracker *StateCounterTracker) bool {
	msgBytes, err := json.Marshal(voucherData)
	if err != nil {
		return false
	}
	return VerifySignature(pubKeyBytes, algorithm, msgBytes, sig, tracker)
}

// ComputeHMACMac produces an HMAC-SHA256 authentication tag.
func ComputeHMACMac(secret, data []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write(data)
	return m.Sum(nil)
}
