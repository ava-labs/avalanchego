package trie

import (
	"bytes"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/era"
)

// Where a lookup was answered.
const (
	FromOverlay = 1
	FromBase    = 2
	Absent      = 3 // neither holds it (zero word or no account)
)

// GetAccount returns the latest account: the overlay, then the base (binary search inside
// the account trie's cut, whose ordinal range the key prefix addresses directly).
func (e *Engine) GetAccount(ah *[32]byte, r *era.Rec) int {
	if a := e.ov[*ah]; a != nil {
		if a.gone {
			return Absent
		}
		return FromOverlay
	}
	t := e.acct
	c := t.cutOf(ah)
	lo, hi := int(t.bound(int(c))), int(t.bound(int(c)+1))
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		o := e.acctOff[m]
		switch bytes.Compare(e.baseBytes(o+1, o+33), ah[:]) {
		case 0:
			if err := era.Decode(e.acctRec(o), r); err != nil {
				panic(err)
			}
			return FromBase
		case -1:
			lo = m + 1
		default:
			hi = m
		}
	}
	return Absent
}

// GetSlot returns the latest word: the trie's overlay values, then the base run of the cut
// the slot prefix addresses (a scan of about K records).
func (e *Engine) GetSlot(ah, sh *[32]byte, r *era.Rec) ([]byte, int) {
	t := e.trie(ah)
	if t == nil {
		return nil, Absent
	}
	if v, ok := t.vals[*sh]; ok {
		if v.n == 0 {
			return nil, Absent
		}
		return v.v[:v.n], FromOverlay
	}
	if t.noBase {
		return nil, Absent
	}
	lo, hi := t.runOf(sh)
	b := e.baseBytes(lo, hi)
	for len(b) > 0 {
		if err := era.Decode(b, r); err != nil {
			panic(err)
		}
		b = b[r.Len:]
		if r.Tag != era.TagSlot {
			continue
		}
		switch bytes.Compare(r.Key, sh[:]) {
		case 0:
			return r.Value, FromBase
		case 1:
			return nil, Absent
		}
	}
	return nil, Absent
}

// account returns the latest account fields; false when the account does not exist.
func (e *Engine) account(ah *[32]byte, r *era.Rec) (ovAcct, bool) {
	switch e.GetAccount(ah, r) {
	case FromOverlay:
		return *e.ov[*ah], true
	case FromBase:
		return baseAcct(r), true
	}
	return ovAcct{}, false
}

// baseAcct is the account of base record r.
func baseAcct(r *era.Rec) ovAcct {
	a := ovAcct{nonce: r.Nonce, multicoin: r.Flags&1 != 0}
	copy(a.balance[32-len(r.Balance):], r.Balance)
	if r.CodeHash != nil && !bytes.Equal(r.CodeHash, EmptyCode[:]) {
		a.hasCode = true
		copy(a.code[:], r.CodeHash)
	}
	return a
}
