package cblock

import (
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/sha3"
)

// U256 is a 256-bit unsigned integer, big-endian.
type U256 [32]byte

// Uint64 returns v if it fits in 64 bits.
func (v *U256) Uint64() (uint64, bool) {
	for _, c := range v[:24] {
		if c != 0 {
			return 0, false
		}
	}
	return binary.BigEndian.Uint64(v[24:]), true
}

// U256From returns x as a U256.
func U256From(x uint64) (v U256) {
	binary.BigEndian.PutUint64(v[24:], x)
	return v
}

func keccak(b ...[]byte) (h [32]byte) {
	k := sha3.NewLegacyKeccak256()
	for _, x := range b {
		k.Write(x)
	}
	k.Sum(h[:0])
	return h
}

// Header is the coreth header (graft/coreth/plugin/evm/customtypes HeaderSerializable at
// avalanchego 4f8c6316bed8), fields in RLP order. The optional fields are pointers: nil
// means absent. Like coreth's rlpgen encoder, a nil optional field followed by a present one
// encodes as 0x80; Decode then sets it (to zero), except ParentBeaconRoot, which needs 32
// bytes (libevm decodes it into a non-nil *common.Hash and rejects 0x80).
type Header struct {
	ParentHash  [32]byte
	UncleHash   [32]byte
	Coinbase    [20]byte
	Root        [32]byte
	TxHash      [32]byte
	ReceiptHash [32]byte
	Bloom       [256]byte
	Difficulty  U256
	Number      uint64 // a *big.Int in coreth; SAE rejects heights above 64 bits
	GasLimit    uint64
	GasUsed     uint64
	Time        uint64
	Extra       []byte
	MixDigest   [32]byte
	Nonce       [8]byte
	ExtDataHash [32]byte

	BaseFee             *U256 // EIP-1559 (Apricot Phase 3)
	ExtDataGasUsed      *U256 // Apricot Phase 4
	BlockGasCost        *U256 // Apricot Phase 4
	BlobGasUsed         *uint64
	ExcessBlobGas       *uint64
	ParentBeaconRoot    *[32]byte
	TimeMilliseconds    *uint64 // Granite
	MinDelayExcess      *uint64 // Granite
	TargetExponent      *uint64 // Helicon
	MinPriceExponent    *uint64 // Helicon (ACP-283)
	SettledHeight       *uint64 // SAE
	SettledGasUnix      *uint64 // SAE
	SettledGasNumerator *uint64 // SAE
	SettledExcess       *uint64 // SAE
}

// nOpt is the number of optional fields that are encoded (the last non-nil one and all before it).
func (h *Header) nOpt() int {
	present := []bool{h.BaseFee != nil, h.ExtDataGasUsed != nil, h.BlockGasCost != nil,
		h.BlobGasUsed != nil, h.ExcessBlobGas != nil, h.ParentBeaconRoot != nil,
		h.TimeMilliseconds != nil, h.MinDelayExcess != nil, h.TargetExponent != nil,
		h.MinPriceExponent != nil, h.SettledHeight != nil, h.SettledGasUnix != nil,
		h.SettledGasNumerator != nil, h.SettledExcess != nil}
	for i := len(present); i > 0; i-- {
		if present[i-1] {
			return i
		}
	}
	return 0
}

// AppendRLP appends the header's RLP encoding to dst.
func (h *Header) AppendRLP(dst []byte) []byte {
	s := len(dst)
	dst = appendBytes(dst, h.ParentHash[:])
	dst = appendBytes(dst, h.UncleHash[:])
	dst = appendBytes(dst, h.Coinbase[:])
	dst = appendBytes(dst, h.Root[:])
	dst = appendBytes(dst, h.TxHash[:])
	dst = appendBytes(dst, h.ReceiptHash[:])
	dst = appendBytes(dst, h.Bloom[:])
	dst = appendU256(dst, &h.Difficulty)
	dst = appendUint(dst, h.Number)
	dst = appendUint(dst, h.GasLimit)
	dst = appendUint(dst, h.GasUsed)
	dst = appendUint(dst, h.Time)
	dst = appendBytes(dst, h.Extra)
	dst = appendBytes(dst, h.MixDigest[:])
	dst = appendBytes(dst, h.Nonce[:])
	dst = appendBytes(dst, h.ExtDataHash[:])
	n := h.nOpt()
	big := func(v *U256) {
		if v == nil {
			dst = append(dst, 0x80)
		} else {
			dst = appendU256(dst, v)
		}
	}
	u := func(v *uint64) {
		if v == nil {
			dst = append(dst, 0x80)
		} else {
			dst = appendUint(dst, *v)
		}
	}
	for i := 0; i < n; i++ {
		switch i {
		case 0:
			big(h.BaseFee)
		case 1:
			big(h.ExtDataGasUsed)
		case 2:
			big(h.BlockGasCost)
		case 3:
			u(h.BlobGasUsed)
		case 4:
			u(h.ExcessBlobGas)
		case 5:
			if h.ParentBeaconRoot == nil {
				dst = append(dst, 0x80)
			} else {
				dst = appendBytes(dst, h.ParentBeaconRoot[:])
			}
		case 6:
			u(h.TimeMilliseconds)
		case 7:
			u(h.MinDelayExcess)
		case 8:
			u(h.TargetExponent)
		case 9:
			u(h.MinPriceExponent)
		case 10:
			u(h.SettledHeight)
		case 11:
			u(h.SettledGasUnix)
		case 12:
			u(h.SettledGasNumerator)
		case 13:
			u(h.SettledExcess)
		}
	}
	return wrapList(dst, s)
}

// Hash is keccak256 of the header's RLP encoding (the block hash).
func (h *Header) Hash() [32]byte { return keccak(h.AppendRLP(nil)) }

var errHeightNotUint64 = errors.New("cblock: block height not uint64")

// DecodeHeader decodes one header encoding (no trailing bytes).
func DecodeHeader(b []byte) (*Header, error) {
	h := new(Header)
	return h, top(b, h.decode)
}

func (h *Header) decode(d *dec) {
	d.fixed(h.ParentHash[:])
	d.fixed(h.UncleHash[:])
	d.fixed(h.Coinbase[:])
	d.fixed(h.Root[:])
	d.fixed(h.TxHash[:])
	d.fixed(h.ReceiptHash[:])
	d.fixed(h.Bloom[:])
	h.Difficulty = d.u256()
	if c := d.bytes(); d.err == nil {
		// Number is a *big.Int in coreth: canonical, any size; SAE (blocks/parse.go) then
		// rejects heights that do not fit 64 bits.
		if len(c) > 0 && c[0] == 0 {
			d.fail(errCanonInt)
		} else if len(c) > 8 {
			d.fail(errHeightNotUint64)
		}
		for _, x := range c {
			h.Number = h.Number<<8 | uint64(x)
		}
	}
	h.GasLimit = d.u64()
	h.GasUsed = d.u64()
	h.Time = d.u64()
	h.Extra = d.bytes()
	d.fixed(h.MixDigest[:])
	d.fixed(h.Nonce[:])
	d.fixed(h.ExtDataHash[:])
	big := func() *U256 { v := d.u256(); return &v }
	u := func() *uint64 { v := d.u64(); return &v }
	for i := 0; d.more(); i++ {
		switch i {
		case 0:
			h.BaseFee = big()
		case 1:
			h.ExtDataGasUsed = big()
		case 2:
			h.BlockGasCost = big()
		case 3:
			h.BlobGasUsed = u()
		case 4:
			h.ExcessBlobGas = u()
		case 5:
			h.ParentBeaconRoot = new([32]byte)
			d.fixed(h.ParentBeaconRoot[:])
		case 6:
			h.TimeMilliseconds = u()
		case 7:
			h.MinDelayExcess = u()
		case 8:
			h.TargetExponent = u()
		case 9:
			h.MinPriceExponent = u()
		case 10:
			h.SettledHeight = u()
		case 11:
			h.SettledGasUnix = u()
		case 12:
			h.SettledGasNumerator = u()
		case 13:
			h.SettledExcess = u()
		default:
			return // end() reports the extra elements
		}
	}
}
