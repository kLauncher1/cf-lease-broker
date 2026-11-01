package zkproof

import (
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// ZKVerifier evaluates cryptographic zero-knowledge proofs and enforces anti-replay nullifier policies.
type ZKVerifier struct {
	mu           sync.RWMutex
	vkMap        map[StatementType]*VerificationKey
	nullifiers   map[string]int64 // nullifier -> expiry unix timestamp
	nullifierTTL time.Duration
}

// NewZKVerifier constructs an initialized verifier with the specified nullifier TTL.
func NewZKVerifier(ttl time.Duration) *ZKVerifier {
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}
	return &ZKVerifier{
		vkMap:        make(map[StatementType]*VerificationKey),
		nullifiers:   make(map[string]int64),
		nullifierTTL: ttl,
	}
}

// RegisterKey registers or updates a VerificationKey for a statement type.
func (v *ZKVerifier) RegisterKey(vk *VerificationKey) {
	if vk == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.vkMap[vk.Type] = vk
}

// GetKey retrieves the active VerificationKey for a given statement type.
func (v *ZKVerifier) GetKey(t StatementType) (*VerificationKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	vk, ok := v.vkMap[t]
	return vk, ok
}

// ListKeys returns all registered verification keys.
func (v *ZKVerifier) ListKeys() []*VerificationKey {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]*VerificationKey, 0, len(v.vkMap))
	for _, vk := range v.vkMap {
		out = append(out, vk)
	}
	return out
}

// VerifyProof verifies a single zk-SNARK proof, enforcing nullifier uniqueness and equation satisfiability.
func (v *ZKVerifier) VerifyProof(proof *ZKProof) (*VerificationResult, error) {
	start := time.Now()

	if proof == nil {
		return &VerificationResult{Valid: false, Reason: "nil proof"}, ErrProofVerificationFailed
	}
	if proof.ProofID == "" {
		return &VerificationResult{Valid: false, Reason: "missing proof_id"}, ErrProofVerificationFailed
	}

	// 1. Anti-Replay Nullifier Verification
	if err := v.consumeNullifier(proof.Nullifier); err != nil {
		return &VerificationResult{
			Valid:              false,
			ProofID:            proof.ProofID,
			StatementType:      proof.Type,
			VerificationTimeUs: time.Since(start).Microseconds(),
			Reason:             err.Error(),
		}, err
	}

	// 2. Fetch Verification Key
	vk, exists := v.GetKey(proof.Type)
	if !exists {
		return &VerificationResult{
			Valid:              false,
			ProofID:            proof.ProofID,
			StatementType:      proof.Type,
			VerificationTimeUs: time.Since(start).Microseconds(),
			Reason:             "verification key not found for statement type",
		}, ErrKeyNotFound
	}

	// 3. Parse and Validate Curve Points (A, B, C)
	ptA, err := ProofPointToPoint(proof.A)
	if err != nil {
		return &VerificationResult{Valid: false, ProofID: proof.ProofID, Reason: err.Error()}, err
	}
	ptB, err := ProofPointToPoint(proof.B)
	if err != nil {
		return &VerificationResult{Valid: false, ProofID: proof.ProofID, Reason: err.Error()}, err
	}
	ptC, err := ProofPointToPoint(proof.C)
	if err != nil {
		return &VerificationResult{Valid: false, ProofID: proof.ProofID, Reason: err.Error()}, err
	}

	if ptA.Infinity || ptB.Infinity {
		return &VerificationResult{Valid: false, ProofID: proof.ProofID, Reason: "proof points cannot be infinity"}, ErrInvalidProofPoint
	}

	// 4. Validate Public Inputs Length
	if len(proof.PublicInputs) == 0 {
		return &VerificationResult{Valid: false, ProofID: proof.ProofID, Reason: "empty public inputs"}, ErrInvalidPublicInputs
	}
	if len(vk.IC) < len(proof.PublicInputs)+1 {
		return &VerificationResult{Valid: false, ProofID: proof.ProofID, Reason: "verification key IC length insufficient"}, ErrInvalidPublicInputs
	}

	// 5. Parse Commitment Point: format "0xX,0xY"
	comPoint, err := parseCommitmentPoint(proof.Commitment)
	if err != nil {
		return &VerificationResult{Valid: false, ProofID: proof.ProofID, Reason: err.Error()}, err
	}

	// 6. Compute Public Input Linear Combination: vk_x = IC_0 + sum_{i=1}^n [x_i] IC_i
	vkX, err := computeVKX(vk, proof.PublicInputs)
	if err != nil {
		return &VerificationResult{Valid: false, ProofID: proof.ProofID, Reason: err.Error()}, err
	}

	// 7. Recompute Fiat-Shamir Challenge Scalar c
	c := computeFiatShamirChallenge(proof.PublicInputs, proof.Commitment, proof.Nullifier, ptA)

	// 8. Cryptographic Non-Interactive Argument Verification:
	// LHS = B + C
	// RHS = A + [c] (comPoint + vkX)
	lhs := PointAdd(ptB, ptC)

	targetBase := PointAdd(comPoint, vkX)
	cTarget := ScalarMult(targetBase, c)
	rhs := PointAdd(ptA, cTarget)

	latencyUs := time.Since(start).Microseconds()

	// 9. Constant-time comparison of affine coordinates
	if lhs.Infinity != rhs.Infinity {
		return &VerificationResult{
			Valid:              false,
			ProofID:            proof.ProofID,
			StatementType:      proof.Type,
			VerificationTimeUs: latencyUs,
			Reason:             "infinity check mismatch",
		}, ErrProofVerificationFailed
	}

	xMatch := subtle.ConstantTimeCompare(lhs.X.Bytes(), rhs.X.Bytes()) == 1
	yMatch := subtle.ConstantTimeCompare(lhs.Y.Bytes(), rhs.Y.Bytes()) == 1

	if !xMatch || !yMatch {
		return &VerificationResult{
			Valid:              false,
			ProofID:            proof.ProofID,
			StatementType:      proof.Type,
			VerificationTimeUs: latencyUs,
			Reason:             "succinct argument relation check failed",
		}, ErrProofVerificationFailed
	}

	return &VerificationResult{
		Valid:              true,
		ProofID:            proof.ProofID,
		StatementType:      proof.Type,
		VerificationTimeUs: latencyUs,
		VectorAccelerated:  false,
	}, nil
}

func parseCommitmentPoint(com string) (Point, error) {
	parts := strings.Split(com, ",")
	if len(parts) != 2 {
		return InfinityPoint(), fmt.Errorf("%w: invalid commitment format", ErrInvalidProofPoint)
	}
	xBytes, err := hex.DecodeString(stripHexPrefix(parts[0]))
	if err != nil {
		return InfinityPoint(), fmt.Errorf("%w: invalid commitment X: %v", ErrInvalidProofPoint, err)
	}
	yBytes, err := hex.DecodeString(stripHexPrefix(parts[1]))
	if err != nil {
		return InfinityPoint(), fmt.Errorf("%w: invalid commitment Y: %v", ErrInvalidProofPoint, err)
	}
	pt := Point{
		X:        new(big.Int).SetBytes(xBytes),
		Y:        new(big.Int).SetBytes(yBytes),
		Infinity: false,
	}
	if !pt.IsOnCurve() {
		return InfinityPoint(), fmt.Errorf("%w: commitment point not on curve", ErrInvalidProofPoint)
	}
	return pt, nil
}

func (v *ZKVerifier) consumeNullifier(nullifier string) error {
	if nullifier == "" {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now().Unix()
	if exp, exists := v.nullifiers[nullifier]; exists && exp > now {
		return ErrProofReplayDetected
	}

	v.nullifiers[nullifier] = now + int64(v.nullifierTTL.Seconds())
	return nil
}

// PruneExpiredNullifiers sweeps stale nullifiers from memory to prevent unbounded memory growth.
func (v *ZKVerifier) PruneExpiredNullifiers(now time.Time) int {
	v.mu.Lock()
	defer v.mu.Unlock()

	cutoff := now.Unix()
	pruned := 0
	for k, exp := range v.nullifiers {
		if exp <= cutoff {
			delete(v.nullifiers, k)
			pruned++
		}
	}
	return pruned
}

// NullifiersCount returns the number of active nullifiers currently tracked.
func (v *ZKVerifier) NullifiersCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.nullifiers)
}

// ResetNullifiers clears all stored nullifiers.
func (v *ZKVerifier) ResetNullifiers() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.nullifiers = make(map[string]int64)
}
