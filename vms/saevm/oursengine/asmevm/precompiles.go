package asmevm

import (
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/asmevm/secp"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fp"
	gokzg4844 "github.com/crate-crypto/go-kzg-4844"
	"golang.org/x/crypto/ripemd160"
)

var (
	errBn256InvalidPoint  = errors.New("bn256: invalid point")
	errBn256PairingLength = errors.New("bn256: bad pairing input length")
	errBlake2FInputLength = errors.New("blake2f: invalid input length")
	errBlake2FFinalFlag   = errors.New("blake2f: invalid final flag")
	errKZGInputLength     = errors.New("kzg: invalid input length")
	errKZGVersionedHash   = errors.New("kzg: mismatched versioned hash")
	errKZGProof           = errors.New("kzg: proof verification failed")
)

type precompiledContract interface {
	requiredGas(input []byte) uint64
	run(input []byte) ([]byte, error)
}

// precompile returns the contract at addr if addr is 0x01..0x0a.
func precompile(addr Address) (precompiledContract, bool) {
	for i := 0; i < 19; i++ {
		if addr[i] != 0 {
			return nil, false
		}
	}
	n := addr[19]
	if n >= 1 && n <= 10 {
		return precompiles[n], true
	}
	return nil, false
}

func runPrecompile(p precompiledContract, input []byte, suppliedGas uint64) ([]byte, uint64, error) {
	gasCost := p.requiredGas(input)
	if suppliedGas < gasCost {
		return nil, 0, ErrOutOfGas
	}
	suppliedGas -= gasCost
	ret, err := p.run(input)
	return ret, suppliedGas, err
}

// Cancun precompile set.
var precompiles = [11]precompiledContract{
	1:  ecrecover{},
	2:  sha256hash{},
	3:  ripemd160hash{},
	4:  identity{},
	5:  modexp{},
	6:  bn256Add{},
	7:  bn256ScalarMul{},
	8:  bn256Pairing{},
	9:  blake2F{},
	10: pointEvaluation{},
}

// getData returns data[start:start+size], right-padded with zeros to size.
func getData(data []byte, start, size uint64) []byte {
	l := uint64(len(data))
	if start > l {
		start = l
	}
	end := l
	if size <= l-start {
		end = start + size
	}
	out := make([]byte, size)
	copy(out, data[start:end])
	return out
}

// --- 0x01 ecrecover ---

type ecrecover struct{}

func (ecrecover) requiredGas([]byte) uint64 { return 3000 }

// run accepts any s in (0,N) (no low-s rule) and v in {27,28}; a bad input
// returns empty output, not an error.
func (ecrecover) run(input []byte) ([]byte, error) {
	in := getData(input, 0, 128)
	for _, b := range in[32:63] {
		if b != 0 {
			return nil, nil
		}
	}
	v := in[63]
	if v != 27 && v != 28 {
		return nil, nil
	}
	var k [128]byte
	copy(k[:], in)
	sh := &recCache[in[0]&(recShards-1)]
	sh.mu.Lock()
	e, hit := sh.m[k]
	sh.mu.Unlock()
	if hit {
		recHits.Add(1)
	} else {
		var pub [64]byte
		if e.ok = secp.Recover(in[:32], in[64:128], int(v-27), &pub); e.ok {
			h := Keccak256(pub[:])
			copy(e.addr[:], h[12:])
		}
		sh.mu.Lock()
		if sh.m == nil || len(sh.m) >= recShardCap {
			sh.m = make(map[[128]byte]recResult, recShardCap)
		}
		sh.m[k] = e
		sh.mu.Unlock()
	}
	if !e.ok {
		return nil, nil
	}
	out := make([]byte, 32)
	copy(out[12:], e.addr[:])
	return out, nil
}

// recCache memoizes ecrecover by its input: the speculation workers and the committer run the
// same calls (a tx re-executed on the committer, a block speculated again), so the committer
// mostly finds the result a worker computed instead of recovering on its critical path. Shards
// by the first hash byte; a full shard starts over (recShards x recShardCap entries, ~26 MB).
const recShards, recShardCap = 64, 2048

type recResult struct {
	addr [20]byte
	ok   bool
}

var (
	recCache [recShards]struct {
		mu sync.Mutex
		m  map[[128]byte]recResult
	}
	recHits atomic.Uint64 // calls answered from recCache
)

// --- 0x02 sha256 ---

type sha256hash struct{}

func (sha256hash) requiredGas(input []byte) uint64 {
	return 60 + 12*toWordSize(uint64(len(input)))
}
func (sha256hash) run(input []byte) ([]byte, error) {
	h := sha256.Sum256(input)
	return h[:], nil
}

// --- 0x03 ripemd160 ---

type ripemd160hash struct{}

func (ripemd160hash) requiredGas(input []byte) uint64 {
	return 600 + 120*toWordSize(uint64(len(input)))
}
func (ripemd160hash) run(input []byte) ([]byte, error) {
	h := ripemd160.New()
	h.Write(input)
	out := make([]byte, 32)
	copy(out[12:], h.Sum(nil))
	return out, nil
}

// --- 0x04 identity ---

type identity struct{}

func (identity) requiredGas(input []byte) uint64 {
	return 15 + 3*toWordSize(uint64(len(input)))
}
func (identity) run(input []byte) ([]byte, error) {
	out := make([]byte, len(input))
	copy(out, input)
	return out, nil
}

// --- 0x05 modexp (EIP-198 with EIP-2565 gas) ---

type modexp struct{}

func (modexp) requiredGas(input []byte) uint64 {
	baseLen := new(big.Int).SetBytes(getData(input, 0, 32))
	expLen := new(big.Int).SetBytes(getData(input, 32, 32))
	modLen := new(big.Int).SetBytes(getData(input, 64, 32))
	if len(input) > 96 {
		input = input[96:]
	} else {
		input = nil
	}
	big32 := big.NewInt(32)
	// Head (first 32 bytes) of the exponent, zero if it lies past the input.
	expHead := new(big.Int)
	if big.NewInt(int64(len(input))).Cmp(baseLen) > 0 {
		n := uint64(32)
		if expLen.Cmp(big32) <= 0 {
			n = expLen.Uint64()
		}
		expHead.SetBytes(getData(input, baseLen.Uint64(), n))
	}
	adjExpLen := new(big.Int)
	if expLen.Cmp(big32) > 0 {
		adjExpLen.Sub(expLen, big32)
		adjExpLen.Lsh(adjExpLen, 3)
	}
	if bl := expHead.BitLen(); bl > 0 {
		adjExpLen.Add(adjExpLen, big.NewInt(int64(bl-1)))
	}
	if adjExpLen.Sign() == 0 {
		adjExpLen.SetInt64(1)
	}
	words := baseLen
	if modLen.Cmp(baseLen) > 0 {
		words = modLen
	}
	gas := new(big.Int).Add(words, big.NewInt(7))
	gas.Rsh(gas, 3)
	gas.Mul(gas, gas)
	gas.Mul(gas, adjExpLen)
	gas.Div(gas, big.NewInt(3))
	if gas.BitLen() > 64 {
		return ^uint64(0)
	}
	if g := gas.Uint64(); g > 200 {
		return g
	}
	return 200
}

// run relies on requiredGas having bounded the lengths: any length that does
// not fit in the gas limit is rejected before run is called.
func (modexp) run(input []byte) ([]byte, error) {
	baseLen := new(big.Int).SetBytes(getData(input, 0, 32)).Uint64()
	expLen := new(big.Int).SetBytes(getData(input, 32, 32)).Uint64()
	modLen := new(big.Int).SetBytes(getData(input, 64, 32)).Uint64()
	if len(input) > 96 {
		input = input[96:]
	} else {
		input = nil
	}
	if baseLen == 0 && modLen == 0 {
		return []byte{}, nil
	}
	base := new(big.Int).SetBytes(getData(input, 0, baseLen))
	exp := new(big.Int).SetBytes(getData(input, baseLen, expLen))
	mod := new(big.Int).SetBytes(getData(input, baseLen+expLen, modLen))
	out := make([]byte, modLen)
	if mod.Sign() == 0 {
		return out, nil
	}
	var v *big.Int
	if base.BitLen() == 1 { // base 1: big.Exp would walk the whole exponent
		v = base.Mod(base, mod)
	} else {
		v = base.Exp(base, exp, mod)
	}
	v.FillBytes(out)
	return out, nil
}

// --- 0x06..0x08 bn256 (EIP-196/197, Istanbul gas EIP-1108) ---

func bn256G1(b []byte) (*bn254.G1Affine, error) {
	p := new(bn254.G1Affine)
	if p.X.SetBytesCanonical(b[0:32]) != nil || p.Y.SetBytesCanonical(b[32:64]) != nil {
		return nil, errBn256InvalidPoint
	}
	if !p.IsOnCurve() {
		return nil, errBn256InvalidPoint
	}
	return p, nil
}

// bn256G2 decodes (x_im, x_re, y_im, y_re) and checks curve and subgroup.
func bn256G2(b []byte) (*bn254.G2Affine, error) {
	p := new(bn254.G2Affine)
	for i, e := range []*fp.Element{&p.X.A1, &p.X.A0, &p.Y.A1, &p.Y.A0} {
		if e.SetBytesCanonical(b[32*i:32*i+32]) != nil {
			return nil, errBn256InvalidPoint
		}
	}
	if !p.IsInfinity() && !p.IsInSubGroup() {
		return nil, errBn256InvalidPoint
	}
	return p, nil
}

func bn256G1Bytes(p *bn254.G1Affine) []byte {
	out := make([]byte, 64)
	x, y := p.X.Bytes(), p.Y.Bytes()
	copy(out[:32], x[:])
	copy(out[32:], y[:])
	return out
}

type bn256Add struct{}

func (bn256Add) requiredGas([]byte) uint64 { return 150 }
func (bn256Add) run(input []byte) ([]byte, error) {
	in := getData(input, 0, 128)
	a, err := bn256G1(in[:64])
	if err != nil {
		return nil, err
	}
	b, err := bn256G1(in[64:])
	if err != nil {
		return nil, err
	}
	return bn256G1Bytes(new(bn254.G1Affine).Add(a, b)), nil
}

type bn256ScalarMul struct{}

func (bn256ScalarMul) requiredGas([]byte) uint64 { return 6000 }
func (bn256ScalarMul) run(input []byte) ([]byte, error) {
	in := getData(input, 0, 96)
	p, err := bn256G1(in[:64])
	if err != nil {
		return nil, err
	}
	s := new(big.Int).SetBytes(in[64:96])
	return bn256G1Bytes(new(bn254.G1Affine).ScalarMultiplication(p, s)), nil
}

type bn256Pairing struct{}

func (bn256Pairing) requiredGas(input []byte) uint64 {
	return 45000 + uint64(len(input)/192)*34000
}
func (bn256Pairing) run(input []byte) ([]byte, error) {
	if len(input)%192 != 0 {
		return nil, errBn256PairingLength
	}
	var g1 []bn254.G1Affine
	var g2 []bn254.G2Affine
	for i := 0; i < len(input); i += 192 {
		a, err := bn256G1(input[i : i+64])
		if err != nil {
			return nil, err
		}
		b, err := bn256G2(input[i+64 : i+192])
		if err != nil {
			return nil, err
		}
		g1 = append(g1, *a)
		g2 = append(g2, *b)
	}
	out := make([]byte, 32)
	if len(g1) == 0 {
		out[31] = 1
		return out, nil
	}
	ok, err := bn254.PairingCheck(g1, g2)
	if err != nil {
		return nil, err
	}
	if ok {
		out[31] = 1
	}
	return out, nil
}

// --- 0x09 blake2f (EIP-152) ---

type blake2F struct{}

func (blake2F) requiredGas(input []byte) uint64 {
	if len(input) != 213 {
		return 0
	}
	return uint64(binary.BigEndian.Uint32(input[0:4]))
}

func (blake2F) run(input []byte) ([]byte, error) {
	if len(input) != 213 {
		return nil, errBlake2FInputLength
	}
	if input[212] > 1 {
		return nil, errBlake2FFinalFlag
	}
	var h [8]uint64
	var m [16]uint64
	for i := range h {
		h[i] = binary.LittleEndian.Uint64(input[4+8*i:])
	}
	for i := range m {
		m[i] = binary.LittleEndian.Uint64(input[68+8*i:])
	}
	t0 := binary.LittleEndian.Uint64(input[196:])
	t1 := binary.LittleEndian.Uint64(input[204:])
	blake2bF(&h, &m, t0, t1, input[212] == 1, binary.BigEndian.Uint32(input[0:4]))
	out := make([]byte, 64)
	for i := range h {
		binary.LittleEndian.PutUint64(out[8*i:], h[i])
	}
	return out, nil
}

var blake2bIV = [8]uint64{
	0x6a09e667f3bcc908, 0xbb67ae8584caa73b, 0x3c6ef372fe94f82b, 0xa54ff53a5f1d36f1,
	0x510e527fade682d1, 0x9b05688c2b3e6c1f, 0x1f83d9abfb41bd6b, 0x5be0cd19137e2179,
}

var blake2bSigma = [10][16]byte{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	{14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3},
	{11, 8, 12, 0, 5, 2, 15, 13, 10, 14, 3, 6, 7, 1, 9, 4},
	{7, 9, 3, 1, 13, 12, 11, 14, 2, 6, 5, 10, 4, 0, 15, 8},
	{9, 0, 5, 7, 2, 4, 10, 15, 14, 1, 11, 12, 6, 8, 3, 13},
	{2, 12, 6, 10, 0, 11, 8, 3, 4, 13, 7, 5, 15, 14, 1, 9},
	{12, 5, 1, 15, 14, 13, 4, 10, 0, 7, 6, 3, 9, 2, 8, 11},
	{13, 11, 7, 14, 12, 1, 3, 9, 5, 0, 15, 4, 8, 6, 2, 10},
	{6, 15, 14, 9, 11, 3, 0, 8, 12, 2, 13, 7, 1, 4, 10, 5},
	{10, 2, 8, 4, 7, 6, 1, 5, 15, 11, 9, 14, 3, 12, 13, 0},
}

// blake2bF is the BLAKE2b compression function F (RFC 7693 3.2) with a
// caller-chosen round count.
func blake2bF(h *[8]uint64, m *[16]uint64, t0, t1 uint64, final bool, rounds uint32) {
	var v [16]uint64
	copy(v[:8], h[:])
	copy(v[8:], blake2bIV[:])
	v[12] ^= t0
	v[13] ^= t1
	if final {
		v[14] = ^v[14]
	}
	g := func(a, b, c, d int, x, y uint64) {
		v[a] += v[b] + x
		v[d] = bits.RotateLeft64(v[d]^v[a], -32)
		v[c] += v[d]
		v[b] = bits.RotateLeft64(v[b]^v[c], -24)
		v[a] += v[b] + y
		v[d] = bits.RotateLeft64(v[d]^v[a], -16)
		v[c] += v[d]
		v[b] = bits.RotateLeft64(v[b]^v[c], -63)
	}
	for r := uint32(0); r < rounds; r++ {
		s := &blake2bSigma[r%10]
		g(0, 4, 8, 12, m[s[0]], m[s[1]])
		g(1, 5, 9, 13, m[s[2]], m[s[3]])
		g(2, 6, 10, 14, m[s[4]], m[s[5]])
		g(3, 7, 11, 15, m[s[6]], m[s[7]])
		g(0, 5, 10, 15, m[s[8]], m[s[9]])
		g(1, 6, 11, 12, m[s[10]], m[s[11]])
		g(2, 7, 8, 13, m[s[12]], m[s[13]])
		g(3, 4, 9, 14, m[s[14]], m[s[15]])
	}
	for i := range h {
		h[i] ^= v[i] ^ v[i+8]
	}
}

// --- 0x0a KZG point evaluation (EIP-4844) ---

var (
	kzgOnce sync.Once
	kzgCtx  *gokzg4844.Context
	kzgErr  error
)

// pointEvaluationReturn is FIELD_ELEMENTS_PER_BLOB (4096) ++ BLS_MODULUS, both 32-byte BE.
var pointEvaluationReturn = func() []byte {
	out := make([]byte, 64)
	binary.BigEndian.PutUint64(out[24:32], 4096)
	mod, _ := new(big.Int).SetString("52435875175126190479447740508185965837690552500527637822603658699938581184513", 10)
	mod.FillBytes(out[32:])
	return out
}()

type pointEvaluation struct{}

func (pointEvaluation) requiredGas([]byte) uint64 { return 50000 }
func (pointEvaluation) run(input []byte) ([]byte, error) {
	if len(input) != 192 {
		return nil, errKZGInputLength
	}
	var (
		z, y       gokzg4844.Scalar
		commitment gokzg4844.KZGCommitment
		proof      gokzg4844.KZGProof
	)
	copy(z[:], input[32:64])
	copy(y[:], input[64:96])
	copy(commitment[:], input[96:144])
	copy(proof[:], input[144:192])
	vh := sha256.Sum256(commitment[:])
	vh[0] = 0x01 // VERSIONED_HASH_VERSION_KZG
	if [32]byte(input[:32]) != vh {
		return nil, errKZGVersionedHash
	}
	kzgOnce.Do(func() { kzgCtx, kzgErr = gokzg4844.NewContext4096Secure() })
	if kzgErr != nil {
		return nil, kzgErr
	}
	if kzgCtx.VerifyKZGProof(commitment, z, y, proof) != nil {
		return nil, errKZGProof
	}
	out := make([]byte, 64)
	copy(out, pointEvaluationReturn)
	return out, nil
}
