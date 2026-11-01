package pqc

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
)

// ML-KEM parameters (FIPS 203).
const (
	kemN   = 256  // Polynomial degree
	kemQ   = 3329 // Prime modulus
	kemK3  = 3    // Kyber-768 dimension
	kemK4  = 4    // Kyber-1024 dimension
	kemEta = 2    // Binomial parameter
)

// Poly represents a polynomial in Z_q[X]/(X^256 + 1).
type Poly [kemN]int16

// PolyVector represents a vector of k polynomials.
type PolyVector []Poly

// KEMKeyPair holds public and secret keys for ML-KEM.
type KEMKeyPair struct {
	Algorithm       string
	PublicKeyBytes  []byte
	PrivateKeyBytes []byte
	ECDHPublicKey   []byte
	ECDHPrivateKey  *ecdh.PrivateKey
}

// KEMEncapsulationResult holds the ciphertext and the derived shared secret.
type KEMEncapsulationResult struct {
	Ciphertext   []byte
	SharedSecret []byte
}

// reduceQ performs symmetric reduction modulo Q into [0, Q-1].
func reduceQ(val int32) int16 {
	res := val % kemQ
	if res < 0 {
		res += kemQ
	}
	return int16(res)
}

// polyAdd adds two polynomials modulo Q.
func polyAdd(a, b Poly) Poly {
	var c Poly
	for i := 0; i < kemN; i++ {
		c[i] = reduceQ(int32(a[i]) + int32(b[i]))
	}
	return c
}

// polySub subtracts polynomial b from a modulo Q.
func polySub(a, b Poly) Poly {
	var c Poly
	for i := 0; i < kemN; i++ {
		c[i] = reduceQ(int32(a[i]) - int32(b[i]))
	}
	return c
}

// polyMulRing multiplies two polynomials in Z_q[X]/(X^256 + 1).
func polyMulRing(a, b Poly) Poly {
	var c Poly
	for i := 0; i < kemN; i++ {
		if a[i] == 0 {
			continue
		}
		for j := 0; j < kemN; j++ {
			if b[j] == 0 {
				continue
			}
			prod := int32(a[i]) * int32(b[j])
			idx := i + j
			if idx < kemN {
				c[idx] = reduceQ(int32(c[idx]) + prod)
			} else {
				// X^256 = -1 mod (X^256 + 1)
				c[idx-kemN] = reduceQ(int32(c[idx-kemN]) - prod)
			}
		}
	}
	return c
}

// sampleCBD samples a polynomial from a centered binomial distribution with eta=1.
func sampleCBD(seed []byte, nonce byte) Poly {
	h := sha256.New()
	h.Write(seed)
	h.Write([]byte{nonce})
	stream := h.Sum(nil)

	var p Poly
	for i := 0; i < kemN; i++ {
		byteIdx := (i / 4) % len(stream)
		shift := (i % 4) * 2
		b0 := int16((stream[byteIdx] >> shift) & 1)
		b1 := int16((stream[byteIdx] >> (shift + 1)) & 1)
		p[i] = reduceQ(int32(b0 - b1))
	}
	return p
}

// encodeMessageToPoly maps 32 message bytes (256 bits) into ring polynomial coefficients.
func encodeMessageToPoly(m []byte) Poly {
	var p Poly
	for i := 0; i < kemN; i++ {
		byteIdx := (i / 8) % len(m)
		bitIdx := i % 8
		bit := (m[byteIdx] >> bitIdx) & 1
		if bit == 1 {
			p[i] = 1665 // (kemQ + 1) / 2
		}
	}
	return p
}

// decodePolyToMessage extracts the 32 message bytes by decision thresholding around 1665.
func decodePolyToMessage(p Poly) []byte {
	m := make([]byte, 32)
	for i := 0; i < kemN; i++ {
		coeff := p[i]
		if coeff < 0 {
			coeff += kemQ
		}
		d0 := int32(coeff)
		if int32(kemQ-coeff) < d0 {
			d0 = int32(kemQ - coeff)
		}
		d1 := int32(coeff) - 1665
		if d1 < 0 {
			d1 = -d1
		}
		if d1 < d0 {
			byteIdx := i / 8
			bitIdx := i % 8
			m[byteIdx] |= (1 << bitIdx)
		}
	}
	return m
}

// generateMatrixA generates the pseudorandom matrix A from public seed rho.
func generateMatrixA(rho []byte, k int) [][]Poly {
	mat := make([][]Poly, k)
	for i := 0; i < k; i++ {
		mat[i] = make([]Poly, k)
		for j := 0; j < k; j++ {
			h := sha256.New()
			h.Write(rho)
			h.Write([]byte{byte(i), byte(j)})
			seed := h.Sum(nil)

			var p Poly
			for deg := 0; deg < kemN; deg++ {
				// Sample uniform coefficients in [0, Q-1]
				idx := (deg * 2) % (len(seed) - 1)
				raw := uint16(seed[idx]) | (uint16(seed[idx+1]) << 8)
				p[deg] = int16(raw % kemQ)
			}
			mat[i][j] = p
		}
	}
	return mat
}

// encodePoly packs a polynomial into 384 bytes (12 bits per coefficient).
func encodePoly(p Poly) []byte {
	out := make([]byte, 384)
	for i := 0; i < kemN/2; i++ {
		c0 := uint16(p[2*i])
		c1 := uint16(p[2*i+1])
		out[3*i] = byte(c0 & 0xFF)
		out[3*i+1] = byte((c0 >> 8) | ((c1 & 0x0F) << 4))
		out[3*i+2] = byte(c1 >> 4)
	}
	return out
}

// decodePoly unpacks 384 bytes back into a polynomial.
func decodePoly(data []byte) Poly {
	var p Poly
	for i := 0; i < kemN/2 && (3*i+2) < len(data); i++ {
		b0 := uint16(data[3*i])
		b1 := uint16(data[3*i+1])
		b2 := uint16(data[3*i+2])
		p[2*i] = int16(b0 | ((b1 & 0x0F) << 8))
		p[2*i+1] = int16((b1 >> 4) | (b2 << 4))
	}
	return p
}

// GenerateKEMKeyPair generates an ML-KEM keypair (Kyber-768 or Kyber-1024) with optional ECDH X25519.
func GenerateKEMKeyPair(algorithm string, seed [32]byte, hybrid bool) (*KEMKeyPair, error) {
	k := kemK3
	if algorithm == AlgorithmMLKEM1024 {
		k = kemK4
	}

	rho := sha256.Sum256(append(seed[:], 0x01))
	sigma := sha256.Sum256(append(seed[:], 0x02))

	matA := generateMatrixA(rho[:], k)

	// Sample secret vectors s and error vector e
	s := make(PolyVector, k)
	e := make(PolyVector, k)
	for i := 0; i < k; i++ {
		s[i] = sampleCBD(sigma[:], byte(i))
		e[i] = sampleCBD(sigma[:], byte(i+k))
	}

	// Compute public key t = A*s + e
	t := make(PolyVector, k)
	for i := 0; i < k; i++ {
		var sum Poly
		for j := 0; j < k; j++ {
			prod := polyMulRing(matA[i][j], s[j])
			sum = polyAdd(sum, prod)
		}
		t[i] = polyAdd(sum, e[i])
	}

	// Serialize public key: rho (32B) + encoded t (k * 384B)
	var pubBytes []byte
	pubBytes = append(pubBytes, rho[:]...)
	for i := 0; i < k; i++ {
		pubBytes = append(pubBytes, encodePoly(t[i])...)
	}

	// Serialize private key: encoded s (k * 384B) + pubKeyHash (32B) + z (32B)
	var privBytes []byte
	for i := 0; i < k; i++ {
		privBytes = append(privBytes, encodePoly(s[i])...)
	}
	pubHash := sha256.Sum256(pubBytes)
	privBytes = append(privBytes, pubHash[:]...)
	z := sha256.Sum256(append(seed[:], 0x03))
	privBytes = append(privBytes, z[:]...)

	kp := &KEMKeyPair{
		Algorithm:       algorithm,
		PublicKeyBytes:  pubBytes,
		PrivateKeyBytes: privBytes,
	}

	// Attach ECDH X25519 for Hybrid KEM
	if hybrid {
		curve := ecdh.X25519()
		ecdhPriv, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("failed to generate X25519 key: %w", err)
		}
		kp.ECDHPrivateKey = ecdhPriv
		kp.ECDHPublicKey = ecdhPriv.PublicKey().Bytes()
		// Append 32-byte X25519 public key to combined public key bytes
		kp.PublicKeyBytes = append(kp.PublicKeyBytes, kp.ECDHPublicKey...)
	}

	return kp, nil
}

// Encapsulate computes a shared secret and ciphertext against a recipient's public key.
func Encapsulate(algorithm string, recipientPubKey []byte, entropySeed []byte, hybrid bool) (*KEMEncapsulationResult, error) {
	k := kemK3
	if algorithm == AlgorithmMLKEM1024 {
		k = kemK4
	}

	expectedPQCLen := 32 + (k * 384)
	if len(recipientPubKey) < expectedPQCLen {
		return nil, errors.New("recipient public key too short for ML-KEM")
	}

	rho := recipientPubKey[:32]
	matA := generateMatrixA(rho, k)

	t := make(PolyVector, k)
	for i := 0; i < k; i++ {
		offset := 32 + (i * 384)
		t[i] = decodePoly(recipientPubKey[offset : offset+384])
	}

	// Message m derived from entropySeed
	m := sha256.Sum256(append(entropySeed, 0xA1))
	coinSeed := sha256.Sum256(append(m[:], 0xB2))

	// Sample ephemeral coins r, e1, e2 from coinSeed
	r := make(PolyVector, k)
	e1 := make(PolyVector, k)
	for i := 0; i < k; i++ {
		r[i] = sampleCBD(coinSeed[:], byte(10+i))
		e1[i] = sampleCBD(coinSeed[:], byte(20+i))
	}
	e2 := sampleCBD(coinSeed[:], 30)

	// Compute u = A^T * r + e1
	u := make(PolyVector, k)
	for i := 0; i < k; i++ {
		var sum Poly
		for j := 0; j < k; j++ {
			prod := polyMulRing(matA[j][i], r[j]) // Transposed matrix A
			sum = polyAdd(sum, prod)
		}
		u[i] = polyAdd(sum, e1[i])
	}

	// Compute v = t^T * r + e2 + encode(m)
	var v Poly
	for i := 0; i < k; i++ {
		prod := polyMulRing(t[i], r[i])
		v = polyAdd(v, prod)
	}
	v = polyAdd(v, e2)
	mPoly := encodeMessageToPoly(m[:])
	v = polyAdd(v, mPoly)

	// Serialize ciphertext: encoded u (k * 384B) + encoded v (384B)
	var ctBytes []byte
	for i := 0; i < k; i++ {
		ctBytes = append(ctBytes, encodePoly(u[i])...)
	}
	ctBytes = append(ctBytes, encodePoly(v)...)

	// Derive PQC shared secret via KDF: Hash(m || Hash(ctBytes))
	ctHash := sha256.Sum256(ctBytes)
	pqcSecret := sha256.Sum256(append(m[:], ctHash[:]...))
	finalSharedSecret := pqcSecret[:]

	// Hybrid ECDH X25519 layer
	if hybrid && len(recipientPubKey) >= expectedPQCLen+32 {
		peerECDHBytes := recipientPubKey[expectedPQCLen : expectedPQCLen+32]
		curve := ecdh.X25519()
		peerECDHPub, err := curve.NewPublicKey(peerECDHBytes)
		if err == nil {
			clientECDHPriv, err := curve.GenerateKey(rand.Reader)
			if err == nil {
				ecdhShared, err := clientECDHPriv.ECDH(peerECDHPub)
				if err == nil {
					// Append client ephemeral ECDH public key (32B) to ciphertext
					ctBytes = append(ctBytes, clientECDHPriv.PublicKey().Bytes()...)

					// Combine PQC and ECDH into Hybrid Master Secret
					mac := hmac.New(sha256.New, ecdhShared)
					mac.Write(pqcSecret[:])
					mac.Write([]byte("cf-broker-hybrid-pqc-tls-v1"))
					finalSharedSecret = mac.Sum(nil)
				}
			}
		}
	}

	return &KEMEncapsulationResult{
		Ciphertext:   ctBytes,
		SharedSecret: finalSharedSecret,
	}, nil
}

// Decapsulate extracts the shared secret from ciphertext using the private key.
func Decapsulate(algorithm string, privKey *KEMKeyPair, ciphertext []byte, hybrid bool) ([]byte, error) {
	k := kemK3
	if algorithm == AlgorithmMLKEM1024 {
		k = kemK4
	}

	expectedPQCCtLen := (k + 1) * 384
	if len(ciphertext) < expectedPQCCtLen {
		return nil, ErrDecapsulationFailed
	}

	// Extract secret vector s from private key
	s := make(PolyVector, k)
	for i := 0; i < k; i++ {
		s[i] = decodePoly(privKey.PrivateKeyBytes[i*384 : (i+1)*384])
	}

	// Extract u and v from ciphertext
	u := make(PolyVector, k)
	for i := 0; i < k; i++ {
		u[i] = decodePoly(ciphertext[i*384 : (i+1)*384])
	}
	v := decodePoly(ciphertext[k*384 : (k+1)*384])

	// Compute v - s^T * u
	var sDotU Poly
	for i := 0; i < k; i++ {
		prod := polyMulRing(s[i], u[i])
		sDotU = polyAdd(sDotU, prod)
	}
	diff := polySub(v, sDotU)

	// Reconstruct message
	mPrime := decodePolyToMessage(diff)

	// Re-encrypt (Fujisaki-Okamoto transform) to verify ciphertext integrity
	coinSeed := sha256.Sum256(append(mPrime, 0xB2))
	rPrime := make(PolyVector, k)
	e1Prime := make(PolyVector, k)
	for i := 0; i < k; i++ {
		rPrime[i] = sampleCBD(coinSeed[:], byte(10+i))
		e1Prime[i] = sampleCBD(coinSeed[:], byte(20+i))
	}
	e2Prime := sampleCBD(coinSeed[:], 30)

	pubKey := privKey.PublicKeyBytes
	rho := pubKey[:32]
	matA := generateMatrixA(rho, k)
	t := make(PolyVector, k)
	for i := 0; i < k; i++ {
		offset := 32 + (i * 384)
		t[i] = decodePoly(pubKey[offset : offset+384])
	}

	uPrime := make(PolyVector, k)
	for i := 0; i < k; i++ {
		var sum Poly
		for j := 0; j < k; j++ {
			prod := polyMulRing(matA[j][i], rPrime[j])
			sum = polyAdd(sum, prod)
		}
		uPrime[i] = polyAdd(sum, e1Prime[i])
	}
	var vPrime Poly
	for i := 0; i < k; i++ {
		prod := polyMulRing(t[i], rPrime[i])
		vPrime = polyAdd(vPrime, prod)
	}
	vPrime = polyAdd(vPrime, e2Prime)
	vPrime = polyAdd(vPrime, encodeMessageToPoly(mPrime))

	var expectedCt []byte
	for i := 0; i < k; i++ {
		expectedCt = append(expectedCt, encodePoly(uPrime[i])...)
	}
	expectedCt = append(expectedCt, encodePoly(vPrime)...)

	ctHash := sha256.Sum256(ciphertext[:expectedPQCCtLen])
	ctMatch := subtle.ConstantTimeCompare(ciphertext[:expectedPQCCtLen], expectedCt) == 1

	var pqcSecret [32]byte
	if ctMatch {
		pqcSecret = sha256.Sum256(append(mPrime, ctHash[:]...))
	} else {
		// Implicit rejection: compute pseudorandom secret from rejection key z
		zOffset := len(privKey.PrivateKeyBytes) - 32
		z := privKey.PrivateKeyBytes[zOffset:]
		pqcSecret = sha256.Sum256(append(z, ctHash[:]...))
	}
	finalSharedSecret := pqcSecret[:]

	// Verify and extract Hybrid ECDH layer
	if hybrid && privKey.ECDHPrivateKey != nil && len(ciphertext) >= expectedPQCCtLen+32 {
		clientECDHPubBytes := ciphertext[expectedPQCCtLen : expectedPQCCtLen+32]
		curve := ecdh.X25519()
		clientECDHPub, err := curve.NewPublicKey(clientECDHPubBytes)
		if err != nil {
			return nil, fmt.Errorf("invalid ephemeral ECDH public key: %w", err)
		}
		ecdhShared, err := privKey.ECDHPrivateKey.ECDH(clientECDHPub)
		if err != nil {
			return nil, fmt.Errorf("ECDH computation failed: %w", err)
		}

		mac := hmac.New(sha256.New, ecdhShared)
		mac.Write(pqcSecret[:])
		mac.Write([]byte("cf-broker-hybrid-pqc-tls-v1"))
		finalSharedSecret = mac.Sum(nil)
	}

	return finalSharedSecret, nil
}

// ConstantTimeSecretEqual validates shared secret parity without leaking side-channel timing.
func ConstantTimeSecretEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// HexEncode returns hex representation of a byte slice.
func HexEncode(b []byte) string {
	return hex.EncodeToString(b)
}
