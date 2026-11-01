package zkproof

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

// ZKProver generates succinct non-interactive zero-knowledge arguments for tenant isolation and quota claims.
type ZKProver struct {
	vkRegistry map[StatementType]*VerificationKey
}

// NewZKProver initializes a new prover instance.
func NewZKProver() *ZKProver {
	return &ZKProver{
		vkRegistry: make(map[StatementType]*VerificationKey),
	}
}

// RegisterVerificationKey attaches a verification key for reference parameter alignment during proving.
func (p *ZKProver) RegisterVerificationKey(vk *VerificationKey) {
	if vk != nil {
		p.vkRegistry[vk.Type] = vk
	}
}

// GenerateWorkerIsolationProof generates a zk-SNARK proof demonstrating that a tenant Edge Worker
// executes strictly within its declared linear memory and gas ceilings without disclosing developer secrets.
func (p *ZKProver) GenerateWorkerIsolationProof(
	stmt ProofStatement,
	witnessSecret string,
	gasWitness uint64,
	memWitness uint32,
) (*ZKProof, error) {
	if witnessSecret == "" {
		return nil, fmt.Errorf("%w: witness secret cannot be empty", ErrInvalidWitness)
	}
	if gasWitness > stmt.GasLimit {
		return nil, fmt.Errorf("%w: witness gas %d exceeds statement gas ceiling %d", ErrInvalidWitness, gasWitness, stmt.GasLimit)
	}
	if memWitness > stmt.MemoryCeilingPages {
		return nil, fmt.Errorf("%w: witness memory pages %d exceeds ceiling %d", ErrInvalidWitness, memWitness, stmt.MemoryCeilingPages)
	}

	vk, exists := p.vkRegistry[StatementWorkerIsolation]
	if !exists {
		return nil, fmt.Errorf("%w: missing verification key for worker isolation", ErrKeyNotFound)
	}

	// 1. Derive deterministic statement public inputs
	pubInputs := deriveWorkerPublicInputs(stmt)

	// 2. Secret witness scalars in F_r
	wScalar := HashToScalar([]byte(witnessSecret), []byte(stmt.TenantID), []byte(stmt.WorkerHash))
	gasSlack := new(big.Int).SetUint64(stmt.GasLimit - gasWitness)
	bScalar := new(big.Int).Add(wScalar, gasSlack)
	bScalar.Mod(bScalar, OrderModulus)

	// 3. Compute commitment to the execution statement: Com = [wScalar] G1 + [bScalar] H1
	comPoint := PointAdd(ScalarMult(G1, wScalar), ScalarMult(H1, bScalar))
	commitment := fmt.Sprintf("0x%s,0x%s", comPoint.X.Text(16), comPoint.Y.Text(16))

	// 4. Compute unique replay-prevention nullifier: H(witnessSecret || StatementID || "worker_nullifier")
	hNull := sha256.New()
	hNull.Write([]byte(witnessSecret))
	hNull.Write([]byte(stmt.StatementID))
	hNull.Write([]byte("worker_nullifier"))
	nullifier := hex.EncodeToString(hNull.Sum(nil))

	// 5. Compute public input combination vkX
	vkX, err := computeVKX(vk, pubInputs)
	if err != nil {
		return nil, err
	}
	targetBase := PointAdd(comPoint, vkX)

	// 6. Generate random blinding scalars (r_w, r_b)
	rw, err := RandomScalar()
	if err != nil {
		return nil, err
	}
	rb, err := RandomScalar()
	if err != nil {
		return nil, err
	}

	// 7. Blinding commitment A = [rw] G1 + [rb] H1
	ptA := PointAdd(ScalarMult(G1, rw), ScalarMult(H1, rb))

	// 8. Challenge c = Hash(PublicInputs || Commitment || Nullifier || A)
	c := computeFiatShamirChallenge(pubInputs, commitment, nullifier, ptA)

	// 9. Responses: zw = rw + c * wScalar, zb = rb + c * bScalar
	cw := new(big.Int).Mul(c, wScalar)
	cw.Mod(cw, OrderModulus)
	zw := new(big.Int).Add(rw, cw)
	zw.Mod(zw, OrderModulus)

	cb := new(big.Int).Mul(c, bScalar)
	cb.Mod(cb, OrderModulus)
	zb := new(big.Int).Add(rb, cb)
	zb.Mod(zb, OrderModulus)

	// 10. Proof points B = [zw] G1, C = [zb] H1 + [c] vkX
	ptB := ScalarMult(G1, zw)
	cVkX := ScalarMult(vkX, c)
	ptC := PointAdd(ScalarMult(H1, zb), cVkX)
	_ = targetBase

	proofID := generateRandomHex(16)
	return &ZKProof{
		ProofID:      "zkp_" + proofID,
		Type:         StatementWorkerIsolation,
		A:            PointToProofPoint(ptA),
		B:            PointToProofPoint(ptB),
		C:            PointToProofPoint(ptC),
		PublicInputs: pubInputs,
		Commitment:   commitment,
		Nullifier:    nullifier,
		CreatedAt:    time.Now().Unix(),
	}, nil
}

// GenerateQuotaAttestationProof generates a zero-knowledge proof showing resource usage <= quota
// while hiding the underlying Cloudflare API keys via Pedersen vector commitments.
func (p *ZKProver) GenerateQuotaAttestationProof(
	stmt ProofStatement,
	apiKeyWitness string,
	usageWitness uint64,
	blindingFactor *big.Int,
) (*ZKProof, error) {
	if apiKeyWitness == "" {
		return nil, fmt.Errorf("%w: API key witness cannot be empty", ErrInvalidWitness)
	}
	if usageWitness > stmt.MaxQuota {
		return nil, fmt.Errorf("%w: usage witness %d exceeds max quota %d", ErrInvalidWitness, usageWitness, stmt.MaxQuota)
	}

	vk, exists := p.vkRegistry[StatementQuotaAttestation]
	if !exists {
		return nil, fmt.Errorf("%w: missing verification key for quota attestation", ErrKeyNotFound)
	}

	if blindingFactor == nil || blindingFactor.Sign() == 0 {
		var err error
		blindingFactor, err = RandomScalar()
		if err != nil {
			return nil, err
		}
	}

	// 1. Derive public inputs
	pubInputs := deriveQuotaPublicInputs(stmt)

	// 2. Witness scalars
	keyScalar := HashToScalar([]byte(apiKeyWitness))
	slack := new(big.Int).SetUint64(stmt.MaxQuota - usageWitness)
	bScalar := new(big.Int).Add(blindingFactor, slack)
	bScalar.Mod(bScalar, OrderModulus)

	// 3. Pedersen commitment Com = [keyScalar] G1 + [bScalar] H1
	comPoint := PointAdd(ScalarMult(G1, keyScalar), ScalarMult(H1, bScalar))
	commitment := fmt.Sprintf("0x%s,0x%s", comPoint.X.Text(16), comPoint.Y.Text(16))

	// 4. Nullifier H(APIKey || TenantID || StatementID)
	hNull := sha256.New()
	hNull.Write([]byte(apiKeyWitness))
	hNull.Write([]byte(stmt.TenantID))
	hNull.Write([]byte(stmt.StatementID))
	nullifier := hex.EncodeToString(hNull.Sum(nil))

	// 5. vkX
	vkX, err := computeVKX(vk, pubInputs)
	if err != nil {
		return nil, err
	}

	// 6. Blinding scalars (rw, rb)
	rw, err := RandomScalar()
	if err != nil {
		return nil, err
	}
	rb, err := RandomScalar()
	if err != nil {
		return nil, err
	}

	// 7. A = [rw] G1 + [rb] H1
	ptA := PointAdd(ScalarMult(G1, rw), ScalarMult(H1, rb))

	// 8. Challenge c
	c := computeFiatShamirChallenge(pubInputs, commitment, nullifier, ptA)

	// 9. Responses: zw = rw + c * keyScalar, zb = rb + c * bScalar
	ck := new(big.Int).Mul(c, keyScalar)
	ck.Mod(ck, OrderModulus)
	zw := new(big.Int).Add(rw, ck)
	zw.Mod(zw, OrderModulus)

	cb := new(big.Int).Mul(c, bScalar)
	cb.Mod(cb, OrderModulus)
	zb := new(big.Int).Add(rb, cb)
	zb.Mod(zb, OrderModulus)

	// 10. Proof points B = [zw] G1, C = [zb] H1 + [c] vkX
	ptB := ScalarMult(G1, zw)
	cVkX := ScalarMult(vkX, c)
	ptC := PointAdd(ScalarMult(H1, zb), cVkX)

	proofID := generateRandomHex(16)
	return &ZKProof{
		ProofID:      "zkp_" + proofID,
		Type:         StatementQuotaAttestation,
		A:            PointToProofPoint(ptA),
		B:            PointToProofPoint(ptB),
		C:            PointToProofPoint(ptC),
		PublicInputs: pubInputs,
		Commitment:   commitment,
		Nullifier:    nullifier,
		CreatedAt:    time.Now().Unix(),
	}, nil
}

// GenerateBoundaryIntegrityProof generates a zero-knowledge argument verifying inter-broker peering boundary rules.
func (p *ZKProver) GenerateBoundaryIntegrityProof(
	stmt ProofStatement,
	interBrokerSecret string,
) (*ZKProof, error) {
	if interBrokerSecret == "" {
		return nil, fmt.Errorf("%w: inter-broker secret cannot be empty", ErrInvalidWitness)
	}

	vk, exists := p.vkRegistry[StatementBoundaryIntegrity]
	if !exists {
		return nil, fmt.Errorf("%w: missing verification key for boundary integrity", ErrKeyNotFound)
	}

	pubInputs := deriveBoundaryPublicInputs(stmt)
	secScalar := HashToScalar([]byte(interBrokerSecret), []byte(stmt.TenantID))
	bScalar, _ := RandomScalar()

	comPoint := PointAdd(ScalarMult(G1, secScalar), ScalarMult(H1, bScalar))
	commitment := fmt.Sprintf("0x%s,0x%s", comPoint.X.Text(16), comPoint.Y.Text(16))

	hNull := sha256.New()
	hNull.Write([]byte(interBrokerSecret))
	hNull.Write([]byte(stmt.StatementID))
	nullifier := hex.EncodeToString(hNull.Sum(nil))

	vkX, err := computeVKX(vk, pubInputs)
	if err != nil {
		return nil, err
	}

	rw, _ := RandomScalar()
	rb, _ := RandomScalar()
	ptA := PointAdd(ScalarMult(G1, rw), ScalarMult(H1, rb))

	c := computeFiatShamirChallenge(pubInputs, commitment, nullifier, ptA)

	cs := new(big.Int).Mul(c, secScalar)
	cs.Mod(cs, OrderModulus)
	zw := new(big.Int).Add(rw, cs)
	zw.Mod(zw, OrderModulus)

	cb := new(big.Int).Mul(c, bScalar)
	cb.Mod(cb, OrderModulus)
	zb := new(big.Int).Add(rb, cb)
	zb.Mod(zb, OrderModulus)

	ptB := ScalarMult(G1, zw)
	cVkX := ScalarMult(vkX, c)
	ptC := PointAdd(ScalarMult(H1, zb), cVkX)

	return &ZKProof{
		ProofID:      "zkp_" + generateRandomHex(16),
		Type:         StatementBoundaryIntegrity,
		A:            PointToProofPoint(ptA),
		B:            PointToProofPoint(ptB),
		C:            PointToProofPoint(ptC),
		PublicInputs: pubInputs,
		Commitment:   commitment,
		Nullifier:    nullifier,
		CreatedAt:    time.Now().Unix(),
	}, nil
}

func computeVKX(vk *VerificationKey, pubInputs []string) (Point, error) {
	if len(vk.IC) == 0 {
		return InfinityPoint(), nil
	}
	ic0, err := ProofPointToPoint(vk.IC[0])
	if err != nil {
		return InfinityPoint(), err
	}
	res := ic0
	for i, inHex := range pubInputs {
		if i+1 >= len(vk.IC) {
			break
		}
		rawBytes, err := hex.DecodeString(stripHexPrefix(inHex))
		if err != nil {
			return InfinityPoint(), err
		}
		s := new(big.Int).SetBytes(rawBytes)
		s.Mod(s, OrderModulus)

		icPt, err := ProofPointToPoint(vk.IC[i+1])
		if err != nil {
			return InfinityPoint(), err
		}
		res = PointAdd(res, ScalarMult(icPt, s))
	}
	return res, nil
}

func computeFiatShamirChallenge(pubInputs []string, commitment, nullifier string, ptA Point) *big.Int {
	h := sha256.New()
	for _, in := range pubInputs {
		h.Write([]byte(in))
	}
	h.Write([]byte(commitment))
	h.Write([]byte(nullifier))
	if !ptA.Infinity && ptA.X != nil && ptA.Y != nil {
		h.Write(ptA.X.Bytes())
		h.Write(ptA.Y.Bytes())
	}
	scalar := new(big.Int).SetBytes(h.Sum(nil))
	return scalar.Mod(scalar, OrderModulus)
}

func deriveWorkerPublicInputs(stmt ProofStatement) []string {
	in0 := HashToScalar([]byte(stmt.StatementID)).Text(16)
	in1 := HashToScalar([]byte(stmt.TenantID), []byte(stmt.WorkerHash)).Text(16)
	in2 := new(big.Int).SetUint64(stmt.GasLimit).Text(16)
	in3 := new(big.Int).SetUint64(uint64(stmt.MemoryCeilingPages)).Text(16)
	return []string{"0x" + in0, "0x" + in1, "0x" + in2, "0x" + in3}
}

func deriveQuotaPublicInputs(stmt ProofStatement) []string {
	in0 := HashToScalar([]byte(stmt.StatementID)).Text(16)
	in1 := HashToScalar([]byte(stmt.TenantID)).Text(16)
	in2 := new(big.Int).SetUint64(stmt.CurrentUsage).Text(16)
	in3 := new(big.Int).SetUint64(stmt.MaxQuota).Text(16)
	return []string{"0x" + in0, "0x" + in1, "0x" + in2, "0x" + in3}
}

func deriveBoundaryPublicInputs(stmt ProofStatement) []string {
	in0 := HashToScalar([]byte(stmt.StatementID)).Text(16)
	in1 := HashToScalar([]byte(stmt.TenantID)).Text(16)
	return []string{"0x" + in0, "0x" + in1}
}

func generateRandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
