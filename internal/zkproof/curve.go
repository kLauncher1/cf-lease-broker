package zkproof

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)

// BN254 / Alt_BN128 field and curve parameters.
// Field modulus: q = 21888242871839275222246405745257275088696331177787505543966570424936030570749
// Curve equation: y^2 = x^3 + 3 (mod q)
// Generator G1: (1, 2)
var (
	FieldModulus, _ = new(big.Int).SetString("30644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd47", 16)
	OrderModulus, _ = new(big.Int).SetString("30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000001", 16)
	CurveB          = big.NewInt(3)

	// Base generator G1 on BN254: (1, 2)
	G1 = Point{
		X:        big.NewInt(1),
		Y:        big.NewInt(2),
		Infinity: false,
	}

	// Secondary generator H1 for Pedersen vector commitments
	H1 Point
)

func init() {
	hScalar := HashToScalar([]byte("CF_BROKER_ZK_PEDERSEN_GENERATOR_H1"))
	H1 = ScalarMult(G1, hScalar)
}

func modField(a *big.Int) *big.Int {
	res := new(big.Int).Mod(a, FieldModulus)
	if res.Sign() < 0 {
		res.Add(res, FieldModulus)
	}
	return res
}

func modOrder(a *big.Int) *big.Int {
	res := new(big.Int).Mod(a, OrderModulus)
	if res.Sign() < 0 {
		res.Add(res, OrderModulus)
	}
	return res
}

// Point represents an affine point on the Weierstrass curve y^2 = x^3 + 3 (mod FieldModulus).
type Point struct {
	X        *big.Int
	Y        *big.Int
	Infinity bool
}

// InfinityPoint returns the neutral identity element (point at infinity).
func InfinityPoint() Point {
	return Point{X: big.NewInt(0), Y: big.NewInt(0), Infinity: true}
}

// IsOnCurve returns true if the point lies on y^2 = x^3 + 3 (mod q) or is the point at infinity.
func (p Point) IsOnCurve() bool {
	if p.Infinity {
		return true
	}
	if p.X == nil || p.Y == nil {
		return false
	}
	y2 := modField(new(big.Int).Mul(p.Y, p.Y))

	x3 := new(big.Int).Mul(p.X, p.X)
	x3.Mul(x3, p.X)
	x3.Add(x3, CurveB)
	x3 = modField(x3)

	return y2.Cmp(x3) == 0
}

// PointAdd computes P1 + P2 on the Weierstrass curve.
func PointAdd(p1, p2 Point) Point {
	if p1.Infinity {
		return p2
	}
	if p2.Infinity {
		return p1
	}

	// Case 1: P1 == -P2 (x1 == x2 && y1 == -y2 mod q) -> Infinity
	negY2 := modField(new(big.Int).Sub(FieldModulus, p2.Y))
	if p1.X.Cmp(p2.X) == 0 && p1.Y.Cmp(negY2) == 0 {
		return InfinityPoint()
	}

	var lambda *big.Int

	// Case 2: P1 == P2 (Point Doubling)
	if p1.X.Cmp(p2.X) == 0 && p1.Y.Cmp(p2.Y) == 0 {
		if p1.Y.Sign() == 0 {
			return InfinityPoint()
		}
		// lambda = (3 * x1^2) / (2 * y1) mod q
		num := new(big.Int).Mul(p1.X, p1.X)
		num.Mul(num, big.NewInt(3))
		num = modField(num)

		den := modField(new(big.Int).Mul(p1.Y, big.NewInt(2)))
		denInv := new(big.Int).ModInverse(den, FieldModulus)
		if denInv == nil {
			return InfinityPoint()
		}
		lambda = modField(new(big.Int).Mul(num, denInv))
	} else {
		// Case 3: P1 != P2
		// lambda = (y2 - y1) / (x2 - x1) mod q
		num := modField(new(big.Int).Sub(p2.Y, p1.Y))
		den := modField(new(big.Int).Sub(p2.X, p1.X))
		denInv := new(big.Int).ModInverse(den, FieldModulus)
		if denInv == nil {
			return InfinityPoint()
		}
		lambda = modField(new(big.Int).Mul(num, denInv))
	}

	// x3 = lambda^2 - x1 - x2 mod q
	x3 := new(big.Int).Mul(lambda, lambda)
	x3.Sub(x3, p1.X)
	x3.Sub(x3, p2.X)
	x3 = modField(x3)

	// y3 = lambda * (x1 - x3) - y1 mod q
	y3 := new(big.Int).Sub(p1.X, x3)
	y3.Mul(y3, lambda)
	y3.Sub(y3, p1.Y)
	y3 = modField(y3)

	return Point{X: x3, Y: y3, Infinity: false}
}

// ScalarMult computes [k]P using double-and-add scalar multiplication.
func ScalarMult(p Point, k *big.Int) Point {
	if p.Infinity || k.Sign() == 0 {
		return InfinityPoint()
	}
	scalar := modOrder(k)
	if scalar.Sign() == 0 {
		return InfinityPoint()
	}

	result := InfinityPoint()
	current := p

	for i := 0; i < scalar.BitLen(); i++ {
		if scalar.Bit(i) == 1 {
			result = PointAdd(result, current)
		}
		current = PointAdd(current, current)
	}

	return result
}

// MultiScalarMult computes the linear combination sum_i [s_i] P_i.
func MultiScalarMult(points []Point, scalars []*big.Int) Point {
	res := InfinityPoint()
	for i := 0; i < len(points) && i < len(scalars); i++ {
		term := ScalarMult(points[i], scalars[i])
		res = PointAdd(res, term)
	}
	return res
}

// HashToScalar hashes arbitrary binary inputs to a scalar in F_r via SHA-256.
func HashToScalar(data ...[]byte) *big.Int {
	h := sha256.New()
	for _, d := range data {
		h.Write(d)
	}
	sum := h.Sum(nil)
	scalar := new(big.Int).SetBytes(sum)
	return modOrder(scalar)
}

// HashToCurve deterministically maps an arbitrary byte sequence to a point on y^2 = x^3 + 3 (mod q).
func HashToCurve(data ...[]byte) Point {
	s := HashToScalar(data...)
	if s.Sign() == 0 {
		s = big.NewInt(1)
	}
	return ScalarMult(G1, s)
}

// RandomScalar generates a cryptographically secure non-zero scalar in [1, OrderModulus - 1].
func RandomScalar() (*big.Int, error) {
	for {
		s, err := rand.Int(rand.Reader, OrderModulus)
		if err != nil {
			return nil, err
		}
		if s.Sign() > 0 {
			return s, nil
		}
	}
}

// PointToProofPoint converts an internal Point into a serializable ProofPoint.
func PointToProofPoint(p Point) ProofPoint {
	if p.Infinity {
		return ProofPoint{Infinity: true, X: "0x0", Y: "0x0"}
	}
	return ProofPoint{
		X:        "0x" + hex.EncodeToString(p.X.Bytes()),
		Y:        "0x" + hex.EncodeToString(p.Y.Bytes()),
		Infinity: false,
	}
}

// ProofPointToPoint parses a ProofPoint back into an internal Point.
func ProofPointToPoint(pp ProofPoint) (Point, error) {
	if pp.Infinity {
		return InfinityPoint(), nil
	}
	xBytes, err := hex.DecodeString(stripHexPrefix(pp.X))
	if err != nil {
		return InfinityPoint(), err
	}
	yBytes, err := hex.DecodeString(stripHexPrefix(pp.Y))
	if err != nil {
		return InfinityPoint(), err
	}

	p := Point{
		X:        new(big.Int).SetBytes(xBytes),
		Y:        new(big.Int).SetBytes(yBytes),
		Infinity: false,
	}
	if !p.IsOnCurve() {
		return InfinityPoint(), ErrInvalidProofPoint
	}
	return p, nil
}

func stripHexPrefix(s string) string {
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	if len(s)%2 != 0 {
		s = "0" + s
	}
	return s
}
