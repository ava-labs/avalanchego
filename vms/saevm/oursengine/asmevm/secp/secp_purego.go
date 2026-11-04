package secp

import (
	"math/big"

	gsecp "github.com/consensys/gnark-crypto/ecc/secp256k1"
	sfp "github.com/consensys/gnark-crypto/ecc/secp256k1/fp"
	"github.com/consensys/gnark-crypto/ecc/secp256k1/fr"
)

// Recover without cgo: gnark-crypto (about 4x slower than libsecp256k1).
func Recover(msg, sig []byte, recid int, pub *[64]byte) bool {
	n := fr.Modulus()
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	if r.Sign() == 0 || s.Sign() == 0 || r.Cmp(n) >= 0 || s.Cmp(n) >= 0 {
		return false
	}
	// R = (r, y) with y^2 = r^3 + 7 and y's parity from recid (r < n < p, so x = r).
	var R gsecp.G1Affine
	var y2, seven sfp.Element
	R.X.SetBigInt(r)
	y2.Square(&R.X).Mul(&y2, &R.X)
	y2.Add(&y2, seven.SetUint64(7))
	if R.Y.Sqrt(&y2) == nil {
		return false
	}
	if yb := R.Y.Bytes(); int(yb[31]&1) != recid {
		R.Y.Neg(&R.Y)
	}
	// Q = r^-1 (s R - z G) = u1 G + u2 R
	rinv := new(big.Int).ModInverse(r, n)
	u1 := new(big.Int).SetBytes(msg[:32])
	u1.Mul(u1, rinv).Neg(u1).Mod(u1, n)
	u2 := s.Mul(s, rinv)
	u2.Mod(u2, n)
	var q gsecp.G1Jac
	q.JointScalarMultiplicationBase(&R, u1, u2)
	if q.Z.IsZero() {
		return false
	}
	var a gsecp.G1Affine
	a.FromJacobian(&q)
	xb, yb := a.X.Bytes(), a.Y.Bytes()
	copy(pub[:32], xb[:])
	copy(pub[32:], yb[:])
	return true
}
