package cblock

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// MainnetChainID is the C-Chain mainnet chain id (the Sender argument for mainnet blocks).
const MainnetChainID = 43114

// Transaction types (EIP-2718). Blob (3) and set-code (4) txs are not part of the C-Chain
// (no Cancun blobs, no Prague) and are rejected.
const (
	LegacyTxType     = 0
	AccessListTxType = 1
	DynamicFeeTxType = 2
)

// AccessTuple is one EIP-2930 access list entry.
type AccessTuple struct {
	Address     [20]byte
	StorageKeys [][32]byte
}

// Tx is a signed transaction. Fields not used by its Type are zero.
type Tx struct {
	Type       byte
	ChainID    U256 // typed txs; legacy txs carry it in V (see Protected, LegacyChainID)
	Nonce      uint64
	GasPrice   U256 // legacy and access list
	GasTipCap  U256 // dynamic fee
	GasFeeCap  U256 // dynamic fee
	Gas        uint64
	To         *[20]byte // nil: contract creation
	Value      U256
	Data       []byte
	AccessList []AccessTuple
	V, R, S    U256 // legacy: V as signed (27/28 or EIP-155); typed: V is the y parity

	// Enc is the transaction's canonical encoding: the RLP list for a legacy tx, type byte ||
	// RLP list for a typed one (in a block body the typed form sits inside an RLP string).
	// Decode sets it to a slice of the input; build it with Encode.
	Enc []byte
}

// Hash is the transaction hash, keccak256(Enc).
func (t *Tx) Hash() [32]byte { return keccak(t.Enc) }

// Encode sets t.Enc from the fields and returns it.
func (t *Tx) Encode() []byte {
	t.Enc = t.appendEnc(nil)
	return t.Enc
}

func (t *Tx) appendEnc(dst []byte) []byte {
	if t.Type != LegacyTxType {
		dst = append(dst, t.Type)
	}
	s := len(dst)
	return wrapList(t.appendFields(dst, true), s)
}

// appendFields appends the list payload in RLP order, with or without the signature.
func (t *Tx) appendFields(dst []byte, sig bool) []byte {
	to := func() {
		if t.To == nil {
			dst = append(dst, 0x80)
		} else {
			dst = appendBytes(dst, t.To[:])
		}
	}
	al := func() {
		s := len(dst)
		for i := range t.AccessList {
			e := &t.AccessList[i]
			es := len(dst)
			dst = appendBytes(dst, e.Address[:])
			ks := len(dst)
			for j := range e.StorageKeys {
				dst = appendBytes(dst, e.StorageKeys[j][:])
			}
			dst = wrapList(dst, ks)
			dst = wrapList(dst, es)
		}
		dst = wrapList(dst, s)
	}
	switch t.Type {
	case LegacyTxType:
		dst = appendUint(dst, t.Nonce)
		dst = appendU256(dst, &t.GasPrice)
		dst = appendUint(dst, t.Gas)
		to()
		dst = appendU256(dst, &t.Value)
		dst = appendBytes(dst, t.Data)
	case AccessListTxType:
		dst = appendU256(dst, &t.ChainID)
		dst = appendUint(dst, t.Nonce)
		dst = appendU256(dst, &t.GasPrice)
		dst = appendUint(dst, t.Gas)
		to()
		dst = appendU256(dst, &t.Value)
		dst = appendBytes(dst, t.Data)
		al()
	case DynamicFeeTxType:
		dst = appendU256(dst, &t.ChainID)
		dst = appendUint(dst, t.Nonce)
		dst = appendU256(dst, &t.GasTipCap)
		dst = appendU256(dst, &t.GasFeeCap)
		dst = appendUint(dst, t.Gas)
		to()
		dst = appendU256(dst, &t.Value)
		dst = appendBytes(dst, t.Data)
		al()
	}
	if sig {
		dst = appendU256(dst, &t.V)
		dst = appendU256(dst, &t.R)
		dst = appendU256(dst, &t.S)
	}
	return dst
}

// decodeBodyTx decodes one element of a block's tx list: an RLP list (legacy) or an RLP
// string holding type || payload (libevm Transaction.DecodeRLP).
func decodeBodyTx(d *dec) *Tx {
	raw := d.raw()
	if d.err != nil {
		return nil
	}
	list, c, _, _ := split(raw)
	var t *Tx
	var err error
	if list {
		t, err = DecodeTx(raw)
	} else if len(c) > 0 && c[0] < 0x80 {
		t, err = DecodeTx(c)
	} else {
		err = errors.New("cblock: typed transaction too short")
	}
	d.fail(err)
	return t
}

// DecodeTx decodes a transaction's canonical encoding (legacy RLP list or type || payload),
// strictly and with no trailing bytes.
func DecodeTx(b []byte) (*Tx, error) {
	if len(b) == 0 {
		return nil, errors.New("cblock: empty transaction")
	}
	t := &Tx{Enc: b}
	body := b
	if b[0] < 0x80 { // typed (libevm decodeTyped)
		if len(b) <= 1 {
			return nil, errors.New("cblock: typed transaction too short")
		}
		t.Type, body = b[0], b[1:]
		if t.Type != AccessListTxType && t.Type != DynamicFeeTxType {
			return nil, fmt.Errorf("cblock: transaction type %d not supported", t.Type)
		}
	} else if b[0] < 0xC0 {
		return nil, errExpList
	}
	err := top(body, func(d *dec) {
		to := func() {
			if c := d.bytes(); d.err == nil && len(c) > 0 {
				if len(c) != 20 {
					d.fail(fmt.Errorf("cblock: tx recipient of %d bytes", len(c)))
				}
				t.To = new([20]byte)
				copy(t.To[:], c)
			}
		}
		al := func() {
			l := d.list()
			for l.more() {
				e := l.list()
				var a AccessTuple
				e.fixed(a.Address[:])
				ks := e.list()
				for ks.more() {
					var k [32]byte
					ks.fixed(k[:])
					a.StorageKeys = append(a.StorageKeys, k)
				}
				e.end(ks)
				l.absorb(e)
				t.AccessList = append(t.AccessList, a)
			}
			d.absorb(l)
		}
		switch t.Type {
		case LegacyTxType:
			t.Nonce = d.u64()
			t.GasPrice = d.u256()
			t.Gas = d.u64()
			to()
			t.Value = d.u256()
			t.Data = d.bytes()
		case AccessListTxType:
			t.ChainID = d.u256()
			t.Nonce = d.u64()
			t.GasPrice = d.u256()
			t.Gas = d.u64()
			to()
			t.Value = d.u256()
			t.Data = d.bytes()
			al()
		case DynamicFeeTxType:
			t.ChainID = d.u256()
			t.Nonce = d.u64()
			t.GasTipCap = d.u256()
			t.GasFeeCap = d.u256()
			t.Gas = d.u64()
			to()
			t.Value = d.u256()
			t.Data = d.bytes()
			al()
		}
		t.V = d.u256()
		t.R = d.u256()
		t.S = d.u256()
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Protected reports whether a legacy tx is EIP-155 replay protected (libevm isProtectedV:
// V other than 27, 28, 0, 1). Typed txs are always protected.
func (t *Tx) Protected() bool {
	if t.Type != LegacyTxType {
		return true
	}
	v, ok := t.V.Uint64()
	return !ok || v > 255 || (v != 27 && v != 28 && v != 0 && v != 1)
}

// LegacyChainID is the chain id a protected legacy tx commits to: (V - 35) / 2.
func (t *Tx) LegacyChainID() *big.Int {
	v := new(big.Int).SetBytes(t.V[:])
	return v.Rsh(v.Sub(v, big.NewInt(35)), 1)
}

// SigningHash is the hash the sender signed for the given chain id: EIP-155 for protected
// legacy txs, the Homestead hash for unprotected ones, keccak(type || rlp(fields)) for typed.
func (t *Tx) SigningHash(chainID uint64) [32]byte {
	var dst []byte
	if t.Type != LegacyTxType {
		dst = append(dst, t.Type)
	}
	s := len(dst)
	dst = t.appendFields(dst, false)
	if t.Type == LegacyTxType && t.Protected() {
		dst = appendUint(dst, chainID)
		dst = append(dst, 0x80, 0x80)
	}
	return keccak(wrapList(dst, s))
}

var (
	errInvalidChainID = errors.New("cblock: invalid chain id for signer")
	errInvalidSig     = errors.New("cblock: invalid transaction v, r, s values")
	errRecover        = errors.New("cblock: ecrecover: no public key for the signature")
	secpN             = secp256k1.S256().N
	secpHalfN         = new(big.Int).Rsh(secpN, 1)
)

// Recover writes the public key (x||y) of the signer of the 32-byte hash msg for the 64-byte
// signature sig (r||s) with recovery id recid (0 or 1); false: there is none. Sender calls it.
// The default is decred's pure Go recovery; a binary that links libsecp256k1 through
// asmevm/secp sets secp.Recover, several times faster (a package that links libevm, which
// carries its own libsecp256k1, cannot link asmevm/secp: the C symbols collide).
var Recover = recoverDecred

func recoverDecred(msg, sig []byte, recid int, pub *[64]byte) bool {
	var c [65]byte
	c[0] = 27 + byte(recid)
	copy(c[1:], sig[:64])
	k, _, err := ecdsa.RecoverCompact(c[:], msg)
	if err != nil {
		return false
	}
	copy(pub[:], k.SerializeUncompressed()[1:])
	return true
}

// Sender recovers the sender address the way libevm's LatestSignerForChainID(chainID)
// (cancun signer) does: typed and protected legacy txs must carry chainID, unprotected
// legacy txs use the Homestead rules, and r, s must satisfy the Homestead (low s) bounds.
func (t *Tx) Sender(chainID uint64) ([20]byte, error) {
	cid := U256From(chainID)
	var rec uint64 // recovery id
	switch {
	case t.Type != LegacyTxType:
		if t.ChainID != cid {
			return [20]byte{}, errInvalidChainID
		}
		v, ok := t.V.Uint64()
		if !ok || v > 1 {
			return [20]byte{}, errInvalidSig
		}
		rec = v
	case !t.Protected():
		v, _ := t.V.Uint64()
		if v != 27 && v != 28 {
			return [20]byte{}, errInvalidSig
		}
		rec = v - 27
	default:
		if t.LegacyChainID().Cmp(new(big.Int).SetUint64(chainID)) != 0 {
			return [20]byte{}, errInvalidChainID
		}
		v, _ := t.V.Uint64() // fits: the chain id matched
		rec = v - 35 - 2*chainID
	}
	r, s := new(big.Int).SetBytes(t.R[:]), new(big.Int).SetBytes(t.S[:])
	if r.Sign() == 0 || s.Sign() == 0 || r.Cmp(secpN) >= 0 || s.Cmp(secpHalfN) > 0 {
		return [20]byte{}, errInvalidSig
	}
	h := t.SigningHash(chainID)
	var sig [64]byte
	copy(sig[:32], t.R[:])
	copy(sig[32:], t.S[:])
	var pub [64]byte
	if !Recover(h[:], sig[:], int(rec), &pub) {
		return [20]byte{}, errRecover
	}
	kh := keccak(pub[:])
	var a [20]byte
	copy(a[:], kh[12:])
	return a, nil
}
