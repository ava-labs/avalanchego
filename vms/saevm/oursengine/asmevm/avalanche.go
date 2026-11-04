package asmevm

// Avalanche C-Chain (coreth, post Granite/Helicon) execution rules that differ
// from Ethereum: P256VERIFY at 0x0100, the deprecated native-asset
// precompiles 0x0100..00/01/02, and the Warp precompile 0x0200..05 with its
// access-list predicates. Reference: avalanchego 7e7ce92
// graft/coreth/params/hooks_libevm.go and
// graft/coreth/precompile/contracts/warp. The node does not validate: a
// predicate counts as valid unless the block header marks it failed.

import (
	stdecdsa "crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/bits"
)

// Avalanche holds the chain inputs; BlockContext.Avalanche nil means Ethereum rules.
type Avalanche struct {
	NetworkID    uint32
	BlockchainID Hash
	Warm         []Address // active precompiles, warm at tx start
}

var (
	p256Addr = Address{18: 0x01}
	warpAddr = Address{0: 0x02, 19: 0x05}

	evWarpSend    = Hash(Keccak256([]byte("SendWarpMessage(address,bytes32,bytes)")))
	selBlockchain = sel4("getBlockchainID()")
	selWarpHash   = sel4("getVerifiedWarpBlockHash(uint32)")
	selWarpMsg    = sel4("getVerifiedWarpMessage(uint32)")
	selWarpSend   = sel4("sendWarpMessage(bytes)")

	errWarp = errors.New("warp precompile error")
)

func sel4(sig string) (s [4]byte) {
	h := Keccak256([]byte(sig))
	copy(s[:], h[:4])
	return s
}

// Granite warp gas (warp.GasConfig).
const (
	warpGetBlockchainIDGas = 200
	warpGetVerifiedBase    = 750
	warpPerChunk           = 512
	warpPerSigner          = 250
	warpPredicateBase      = 125_000
	warpSendBase           = 375 + 3*375 + 20_000 + 20_000 // LOG + 3 topics + add message + one slot write
	warpPerMessageByte     = 8
	p256VerifyGas          = 6900
)

// warpGas is warp.GasConfig: Granite's (the constants above) or the one before it.
type warpGas struct{ blockchainID, verifiedBase, perChunk, perSigner, predicateBase uint64 }

var (
	warpGasGranite    = warpGas{warpGetBlockchainIDGas, warpGetVerifiedBase, warpPerChunk, warpPerSigner, warpPredicateBase}
	warpGasPreGranite = warpGas{2, 2, 3_200, 500, 200_000}
)

func warpGasFor(r *Rules) *warpGas {
	if r == nil || r.Granite {
		return &warpGasGranite
	}
	return &warpGasPreGranite
}

// callKind tells a stateful precompile how it was reached.
type callKind byte

const (
	kindCall callKind = iota
	kindCallCode
	kindDelegate
	kindStatic
)

// special returns the Avalanche precompile at addr, if any.
func (evm *EVM) special(addr Address) (precompiledContract, bool) {
	if evm.Block.Avalanche == nil {
		return nil, false
	}
	if r := evm.Block.Rules; r != nil { // coreth params/hooks_libevm.go currentPrecompiles
		native := addr[0] == 1 && addr[19] <= 2 && addr == Address{0: 1, 19: addr[19]}
		switch {
		case native && (r.Banff || r.Pre6 && !r.AP6):
			return deprecatedPrecompile{}, true
		case native && r.AP2 && addr[19] == 0:
			return deprecatedPrecompile{}, true
		case native && r.AP2 && addr[19] == 1:
			return nativeAssetBalance{}, true
		case native && r.AP2:
			return nativeAssetCall{}, true
		case addr == p256Addr:
			return p256Verify{}, r.Granite
		case addr == warpAddr:
			return warpPrecompile{}, r.Durango
		}
		return nil, false
	}
	switch {
	case addr == p256Addr:
		return p256Verify{}, true
	case addr == warpAddr:
		return warpPrecompile{}, true
	case addr[0] == 1 && addr[19] <= 2 && addr == Address{0: 1, 19: addr[19]}:
		return deprecatedPrecompile{}, true
	}
	return nil, false
}

// lookupPrecompile is precompile() plus the chain's own precompiles.
func (evm *EVM) lookupPrecompile(addr Address) (precompiledContract, bool) {
	if p, ok := precompile(addr); ok {
		switch r := evm.Block.Rules; {
		case r.cancun():
		case addr[19] == 10: // point evaluation is Cancun's (EIP-4844)
			return nil, false
		case addr[19] == 5 && !r.Berlin:
			return modexpEIP198{}, true
		}
		return p, true
	}
	return evm.special(addr)
}

// statefulPrecompile sees the EVM and its call context.
type statefulPrecompile interface {
	call(evm *EVM, caller Address, input []byte, gas uint64, kind callKind) ([]byte, uint64, error)
}

// runPrecompileAt runs p as reached by caller (stateful ones get the context).
func (evm *EVM) runPrecompileAt(p precompiledContract, caller Address, input []byte, gas uint64, kind callKind) ([]byte, uint64, error) {
	if sp, ok := p.(statefulPrecompile); ok {
		return sp.call(evm, caller, input, gas, kind)
	}
	return runPrecompile(p, input, gas)
}

// p256Verify is RIP-7212 at its Osaka price: 160 bytes (hash, r, s, x, y)
// give a 32-byte 1 on a valid signature and empty output otherwise.
type p256Verify struct{}

func (p256Verify) requiredGas([]byte) uint64 { return p256VerifyGas }

func (p256Verify) run(in []byte) ([]byte, error) {
	if len(in) != 160 {
		return nil, nil
	}
	b := func(i int) *big.Int { return new(big.Int).SetBytes(in[i : i+32]) }
	pub := &stdecdsa.PublicKey{Curve: elliptic.P256(), X: b(96), Y: b(128)}
	if !stdecdsa.Verify(pub, in[:32], b(32), b(64)) { // rejects r, s outside [1, n-1] and points off the curve
		return nil, nil
	}
	out := make([]byte, 32)
	out[31] = 1
	return out, nil
}

// deprecatedPrecompile (nativeasset.DeprecatedContract) reverts with empty
// output and returns all supplied gas.
type deprecatedPrecompile struct{}

func (deprecatedPrecompile) requiredGas([]byte) uint64  { return 0 }
func (deprecatedPrecompile) run([]byte) ([]byte, error) { return nil, ErrExecutionReverted }
func (deprecatedPrecompile) call(evm *EVM, _ Address, _ []byte, gas uint64, kind callKind) ([]byte, uint64, error) {
	if (kind == kindDelegate || kind == kindCallCode) && (evm.Block.Rules == nil || evm.Block.Rules.Granite) {
		return nil, 0, ErrExecutionReverted // hooks_libevm.go makePrecompile: Granite reverts delegated calls with no gas left
	}
	return nil, gas, ErrExecutionReverted
}

type warpPrecompile struct{}

func (warpPrecompile) requiredGas([]byte) uint64  { return 0 }
func (warpPrecompile) run([]byte) ([]byte, error) { return nil, errWarp }

// call follows warp/contract.go; every error halts the frame with all gas spent.
func (warpPrecompile) call(evm *EVM, caller Address, input []byte, gas uint64, kind callKind) ([]byte, uint64, error) {
	if (kind == kindDelegate || kind == kindCallCode) && (evm.Block.Rules == nil || evm.Block.Rules.Granite) {
		return nil, 0, ErrExecutionReverted // Granite: no delegated warp calls (before it allowed; from 2025-08-02 such txs are invalid)
	}
	wg := warpGasFor(evm.Block.Rules)
	if len(input) < 4 {
		return nil, 0, errWarp
	}
	var s [4]byte
	copy(s[:], input)
	args := input[4:]
	use := func(c uint64) bool {
		if gas < c {
			return false
		}
		gas -= c
		return true
	}
	av := evm.Block.Avalanche
	switch s {
	case selBlockchain:
		if !use(wg.blockchainID) {
			return nil, 0, ErrOutOfGas
		}
		return av.BlockchainID[:], gas, nil
	case selWarpSend:
		if !use(warpSendBase) || !use(warpPerMessageByte*uint64(len(args))) {
			return nil, 0, ErrOutOfGas
		}
		if kind == kindStatic || evm.readOnly {
			return nil, 0, errWarp // write protection
		}
		payload, ok := abiBytes(args)
		if !ok {
			return nil, 0, errWarp
		}
		msg := warpUnsigned(av.NetworkID, av.BlockchainID, warpAddressedCall(caller[:], payload))
		id := Hash(sha256.Sum256(msg))
		data := make([]byte, 32, 64+len(msg)+31)
		data[31] = 0x20
		data = abiPackBytes(data, msg)
		var src Hash
		copy(src[12:], caller[:])
		evm.state.addLog(Log{Address: warpAddr, Topics: []Hash{evWarpSend, src, id}, Data: data})
		return id[:], gas, nil
	case selWarpMsg, selWarpHash:
		if !use(wg.verifiedBase) {
			return nil, 0, ErrOutOfGas
		}
		if len(args) < 32 {
			return nil, 0, errWarp
		}
		var w Hash
		copy(w[:], args[:32])
		if !isZero(w[:28]) || binary.BigEndian.Uint32(w[28:]) > 1<<31-1 {
			return nil, 0, errWarp
		}
		idx := int(binary.BigEndian.Uint32(w[28:]))
		blockHash := s == selWarpHash
		if idx >= len(evm.warpPreds) || bitsContain(evm.Tx.WarpFailed, idx) {
			return warpPackFailed(blockHash), gas, nil
		}
		chunks := evm.warpPreds[idx]
		if !use(wg.perChunk * uint64(len(chunks))) {
			return nil, 0, ErrOutOfGas
		}
		m, err := parseWarpPredicate(chunks)
		if err != nil {
			return nil, 0, errWarp
		}
		kind, a, b, err := parseWarpPayload(m.payload)
		if err != nil || (kind == 0) != blockHash {
			return nil, 0, errWarp
		}
		if blockHash {
			out := make([]byte, 96)
			copy(out, m.chain[:])
			copy(out[32:], a)
			out[95] = 1
			return out, gas, nil
		}
		var sender Address // common.BytesToAddress
		if len(a) >= 20 {
			copy(sender[:], a[len(a)-20:])
		} else {
			copy(sender[20-len(a):], a)
		}
		out := make([]byte, 160, 192+len(b)+31)
		out[31] = 0x40
		out[63] = 1
		copy(out[64:], m.chain[:])
		copy(out[96+12:], sender[:])
		out[159] = 0x60
		return abiPackBytes(out, b), gas, nil
	}
	return nil, 0, errWarp
}

// warpPackFailed is the (empty message, false) output.
func warpPackFailed(blockHash bool) []byte {
	if blockHash {
		return make([]byte, 96)
	}
	out := make([]byte, 192)
	out[31] = 0x40
	out[159] = 0x60
	return out
}

// abiBytes decodes one dynamic `bytes` argument (geth abi rules).
func abiBytes(in []byte) ([]byte, bool) {
	if len(in) < 32 || !isZero(in[:24]) {
		return nil, false
	}
	off := binary.BigEndian.Uint64(in[24:32])
	if off > uint64(len(in)) || uint64(len(in))-off < 32 || !isZero(in[off:off+24]) {
		return nil, false
	}
	n := binary.BigEndian.Uint64(in[off+24 : off+32])
	start := off + 32
	if n > uint64(len(in))-start {
		return nil, false
	}
	return in[start : start+n], true
}

// abiPackBytes appends a length word and b right-padded to 32 bytes.
func abiPackBytes(out, b []byte) []byte {
	var l [32]byte
	binary.BigEndian.PutUint64(l[24:], uint64(len(b)))
	out = append(out, l[:]...)
	out = append(out, b...)
	return append(out, make([]byte, (32-len(b)%32)%32)...)
}

// Avalanche linear codec: u16 version 0, u32 big-endian, []byte u32-prefixed.
func warpUnsigned(network uint32, chain Hash, payload []byte) []byte {
	out := make([]byte, 0, 42+len(payload))
	out = binary.BigEndian.AppendUint16(out, 0)
	out = binary.BigEndian.AppendUint32(out, network)
	out = append(out, chain[:]...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(payload)))
	return append(out, payload...)
}

func warpAddressedCall(src, payload []byte) []byte {
	out := make([]byte, 0, 14+len(src)+len(payload))
	out = binary.BigEndian.AppendUint16(out, 0)
	out = binary.BigEndian.AppendUint32(out, 1)
	out = binary.BigEndian.AppendUint32(out, uint32(len(src)))
	out = append(out, src...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(payload)))
	return append(out, payload...)
}

type codecReader struct {
	b   []byte
	bad bool
}

func (r *codecReader) take(n int) []byte {
	if r.bad || n < 0 || n > len(r.b) {
		r.bad = true
		return nil
	}
	s := r.b[:n]
	r.b = r.b[n:]
	return s
}
func (r *codecReader) u16() uint16 {
	if s := r.take(2); s != nil {
		return binary.BigEndian.Uint16(s)
	}
	return 0
}
func (r *codecReader) u32() uint32 {
	if s := r.take(4); s != nil {
		return binary.BigEndian.Uint32(s)
	}
	return 0
}
func (r *codecReader) bytes() []byte { return r.take(int(r.u32())) }
func (r *codecReader) err() error {
	if r.bad || len(r.b) != 0 {
		return errWarp
	}
	return nil
}

type warpMessage struct {
	chain   Hash
	payload []byte
	signers []byte
}

// parseWarpPredicate joins the access-list chunks, drops the zero padding and
// the 0xff delimiter, and decodes the signed warp message.
func parseWarpPredicate(chunks []Hash) (*warpMessage, error) {
	raw := make([]byte, 0, 32*len(chunks))
	for _, c := range chunks {
		raw = append(raw, c[:]...)
	}
	n := len(raw)
	for n > 0 && raw[n-1] == 0 {
		n--
	}
	if n == 0 || (n+31)/32 != len(chunks) || raw[n-1] != 0xff {
		return nil, errWarp
	}
	r := &codecReader{b: raw[:n-1]}
	if r.u16() != 0 {
		return nil, errWarp
	}
	r.u32() // network id
	m := &warpMessage{}
	copy(m.chain[:], r.take(32))
	m.payload = r.bytes()
	if r.u32() != 0 { // BitSetSignature
		return nil, errWarp
	}
	m.signers = r.bytes()
	r.take(96)
	if err := r.err(); err != nil {
		return nil, err
	}
	return m, nil
}

// parseWarpPayload returns type 0 Hash (a = hash) or type 1 AddressedCall
// (a = source address, b = payload).
func parseWarpPayload(p []byte) (kind uint32, a, b []byte, err error) {
	r := &codecReader{b: p}
	if r.u16() != 0 {
		return 0, nil, nil, errWarp
	}
	switch kind = r.u32(); kind {
	case 0:
		a = r.take(32)
	case 1:
		a = r.bytes()
		b = r.bytes()
	default:
		return 0, nil, nil, errWarp
	}
	return kind, a, b, r.err()
}

// warpPredicateGas replaces the access-list gas of one warp entry.
func warpPredicateGas(chunks []Hash, wg *warpGas) (uint64, error) {
	m, err := parseWarpPredicate(chunks)
	if err != nil {
		return 0, err
	}
	if _, _, _, err := parseWarpPayload(m.payload); err != nil {
		return 0, err
	}
	if len(m.signers) > 0 && m.signers[0] == 0 {
		return 0, errWarp // a big.Int bitset has no leading zero byte
	}
	n := 0
	for _, x := range m.signers {
		n += bits.OnesCount8(x)
	}
	return wg.predicateBase + wg.perChunk*uint64(len(chunks)) + wg.perSigner*uint64(n), nil
}

func isZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// bitsContain is set.Bits.Contains over a big-endian big.Int byte string.
func bitsContain(b []byte, i int) bool {
	n := len(b)
	return i/8 < n && b[n-1-i/8]>>(i%8)&1 == 1
}

// ParseWarpResults decodes the header's predicate results (Helicon: all of
// header.Extra): tx hash -> failed bitset of the warp precompile.
func ParseWarpResults(extra []byte) (map[Hash][]byte, error) {
	out := map[Hash][]byte{}
	if len(extra) == 0 {
		return out, nil
	}
	r := &codecReader{b: extra}
	if r.u16() != 0 {
		return nil, fmt.Errorf("predicate results: codec version")
	}
	for n := r.u32(); n > 0 && !r.bad; n-- {
		var tx Hash
		copy(tx[:], r.take(32))
		for m := r.u32(); m > 0 && !r.bad; m-- {
			var a Address
			copy(a[:], r.take(20))
			bs := r.bytes()
			if a == warpAddr {
				out[tx] = bs
			}
		}
	}
	if r.err() != nil {
		return nil, fmt.Errorf("predicate results: bad encoding")
	}
	return out, nil
}
