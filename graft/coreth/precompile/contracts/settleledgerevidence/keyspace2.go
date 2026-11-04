package settleledger

// Keyspace 2: the ledger slots named by small integers instead of keccak preimages.
//
// A packed row already names its instrument and parties by 2-byte index, and the v1
// keyspace turns each index back into an address (an index-table read) and then hashes
// (domain, token, party) into the slot key. Keyspace 2 keys the slot by the indices
// themselves: key = 0x5e, domain, a (uint64), b (uint64). Nothing on the settle path
// hashes and nothing reads an index table; addresses stay in the index tables for
// readers and logs. Omnibuses get an index of their own the first time one is named
// (bind or setHolderAllowed), so custody is keyed by (token index, holder index).
//
// It is a different state layout, so it belongs to a chain from genesis: the plant
// setting settle-keyspace=2 must be the same on every node from block 0. The ERC-20
// surface and the address readers still use the v1 keys and are not coherent with a
// keyspace-2 ledger; the plant does not use them.
//
// Byte 0 is 0x5e: its low bit is 0, so coreth's state-key normalization (which clears
// that bit) leaves the key unchanged, and no keccak output collides with it in practice.

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/holiman/uint256"
)

var keyspaceV2 = settingFile("settle-keyspace") == "2"

// bulkSlots selects the StateDB bulk slot path (GetStates/SetStates) in keyspace 2 when
// the StateDB offers it. Plant setting settle-bulk=1.
var bulkSlots = settingFile("settle-bulk") == "1"

const (
	// Address -> index reverse maps and the holder counter, written by the setup calls
	// only; hashed keys (v1 slot derivation) are fine off the settle path. Domains 21 to
	// 24 are unused by v1 and the ERC-20 surface.
	tokenIdxDomain  byte = 21
	partyIdxDomain  byte = 22
	holderIdxDomain byte = 23
	holderCntDomain byte = 24
)

func k2(domain byte, a, b uint64) common.Hash {
	var k common.Hash
	k[0] = 0x5e
	k[1] = domain
	binary.BigEndian.PutUint64(k[8:16], a)
	binary.BigEndian.PutUint64(k[16:24], b)
	return k
}

func k2Shadow(tok, party uint64) common.Hash           { return k2(shadowDomain, tok, party) }
func k2Held(tok, holder uint64) common.Hash            { return k2(heldDomain, tok, holder) }
func k2Binding(party uint64) common.Hash               { return k2(bindingDomain, party, 0) }
func k2Allowed(holder uint64) common.Hash              { return k2(allowedDomain, holder, 0) }
func k2Handled(block uint64) common.Hash               { return k2(6, block, 0) }
func k2Supply(tok uint64) common.Hash                  { return k2(10, tok, 0) }
func k2Cash() common.Hash                              { return k2(cashDomain, 0, 0) }
func revKey(domain byte, a common.Address) common.Hash { return slot(domain, a, common.Address{}) }

func getU64(s contract.StateDB, addr common.Address, k common.Hash) uint64 {
	v := s.GetState(addr, k)
	return binary.BigEndian.Uint64(v[24:32])
}

func setU64(s contract.StateDB, addr common.Address, k common.Hash, v uint64) {
	var h common.Hash
	binary.BigEndian.PutUint64(h[24:32], v)
	s.SetState(addr, k, h)
}

// holderIndex returns the omnibus's index, giving it the next one the first time.
func holderIndex(s contract.StateDB, addr, holder common.Address) uint64 {
	if v := getU64(s, addr, revKey(holderIdxDomain, holder)); v != 0 {
		return v - 1
	}
	cnt := revKey(holderCntDomain, common.Address{})
	n := getU64(s, addr, cnt)
	setU64(s, addr, cnt, n+1)
	setU64(s, addr, revKey(holderIdxDomain, holder), n+1)
	return n
}

// The setup calls keep their v1 writes (index tables, address bindings) and add the
// keyspace-2 view. The loadgen sends them concurrently, so each side completes the
// binding whichever of setPartyAt and bind lands first.

func v2SetTokenAt(s contract.StateDB, addr common.Address, i uint64, token common.Address) {
	setU64(s, addr, revKey(tokenIdxDomain, token), i+1)
	if common.Address(s.GetState(addr, cashKey()).Bytes()[12:32]) == token && token != (common.Address{}) {
		setU64(s, addr, k2Cash(), i+1)
	}
}

func v2SetCashToken(s contract.StateDB, addr common.Address, token common.Address) {
	if v := getU64(s, addr, revKey(tokenIdxDomain, token)); v != 0 {
		setU64(s, addr, k2Cash(), v)
	}
}

func v2SetPartyAt(s contract.StateDB, addr common.Address, i uint64, party common.Address) {
	setU64(s, addr, revKey(partyIdxDomain, party), i+1)
	h := common.BytesToAddress(s.GetState(addr, bindingKey(party)).Bytes())
	if h != (common.Address{}) {
		setU64(s, addr, k2Binding(i), holderIndex(s, addr, h)+1)
	}
}

func v2Bind(s contract.StateDB, addr common.Address, party, holder common.Address) {
	hi := holderIndex(s, addr, holder)
	if p := getU64(s, addr, revKey(partyIdxDomain, party)); p != 0 {
		setU64(s, addr, k2Binding(p-1), hi+1)
	}
}

func v2SetAllowed(s contract.StateDB, addr common.Address, holder common.Address, allowed bool) {
	var v uint64
	if allowed {
		v = 1
	}
	setU64(s, addr, k2Allowed(holderIndex(s, addr, holder)), v)
}

// bulkState is the StateDB bulk slot path (libevm plant branch). One account lookup and
// one journal entry per call.
type bulkState interface {
	GetStates(addr common.Address, keys []common.Hash, out []common.Hash)
	SetStates(addr common.Address, keys, vals []common.Hash)
}

// batch2 is the keyspace-2 batch cache: one map from a packed small-integer key to a
// cell, cells from one slab, bindings and holding flags in slices indexed by party and
// holder index.
type batch2 struct {
	state contract.StateDB
	bulk  bulkState
	addr  common.Address

	cells     map[uint64]*cell
	slab      []cell
	flushList []*cell
	keys      []common.Hash // keys[i] is the slot of flushList[i]
	binding   []int32       // per party index: 0 not read, -1 unbound, else holder index + 1
	allowed   []int8        // per holder index: 0 not read, 1 no, 2 yes

	pend []common.Hash // keys queued for one bulk read
	pidx []uint64      // packed ids of pend
}

// The packed id of a non-handled slot: domain in the top byte, a and b 24 bits each
// (token, party and holder indices are 16-bit). Handled words use domain 6 and the
// block number in the low 56 bits, so they never collide with a ledger id.
func pid(domain byte, a, b uint64) uint64 { return uint64(domain)<<56 | a<<24 | b }

func newBatch2(state contract.StateDB, addr common.Address) *batch2 {
	b := &batch2{
		state:     state,
		addr:      addr,
		cells:     make(map[uint64]*cell, 4096),
		slab:      make([]cell, 0, 4096),
		keys:      make([]common.Hash, 0, 4096),
		flushList: make([]*cell, 0, 4096),
	}
	if bulkSlots {
		b.bulk, _ = state.(bulkState)
	}
	return b
}

func (b *batch2) newCell(id uint64, key common.Hash, v common.Hash) *cell {
	if len(b.slab) == cap(b.slab) { // a fresh slab; cells already handed out stay valid
		b.slab = make([]cell, 0, 2*cap(b.slab))
	}
	b.slab = append(b.slab, cell{})
	c := &b.slab[len(b.slab)-1]
	c.value.SetBytes32(v[:])
	c.original.Set(&c.value)
	b.cells[id] = c
	b.keys = append(b.keys, key)
	b.flushList = append(b.flushList, c)
	return c
}

func (b *batch2) cellAt(id uint64, key func() common.Hash) *cell {
	if c, ok := b.cells[id]; ok {
		return c
	}
	k := key()
	return b.newCell(id, k, b.state.GetState(b.addr, k))
}

func (b *batch2) shadow(tok, party uint64) *cell {
	return b.cellAt(pid(shadowDomain, tok, party), func() common.Hash { return k2Shadow(tok, party) })
}

func (b *batch2) held(tok, holder uint64) *cell {
	return b.cellAt(pid(heldDomain, tok, holder), func() common.Hash { return k2Held(tok, holder) })
}

func (b *batch2) handledCell(ref uint64) (*cell, uint) {
	blk := ref >> 8
	return b.cellAt(6<<56|blk, func() common.Hash { return k2Handled(blk) }), uint(ref & 255)
}

// want queues a slot for the next bulk read if it is not held yet.
func (b *batch2) want(id uint64, key common.Hash) {
	if _, ok := b.cells[id]; ok {
		return
	}
	b.cells[id] = nil // queued; filled by fetch
	b.pend = append(b.pend, key)
	b.pidx = append(b.pidx, id)
}

func (b *batch2) fetch() {
	if len(b.pend) == 0 {
		return
	}
	out := make([]common.Hash, len(b.pend))
	b.bulk.GetStates(b.addr, b.pend, out)
	for i, id := range b.pidx {
		b.newCell(id, b.pend[i], out[i])
	}
	b.pend, b.pidx = b.pend[:0], b.pidx[:0]
}

func grow[T any](s []T, n int) []T {
	if n < len(s) {
		return s
	}
	m := 2 * len(s)
	if m < n+1 {
		m = n + 1
	}
	return append(s, make([]T, m-len(s))...)
}

// holderOf returns the party's holder index, or false if the party is bound to nothing.
func (b *batch2) holderOf(party uint64) (uint64, bool) {
	b.binding = grow(b.binding, int(party))
	if v := b.binding[party]; v != 0 {
		return uint64(v - 1), v > 0
	}
	v := int32(b.cellAt(pid(bindingDomain, party, 0), func() common.Hash { return k2Binding(party) }).value.Uint64())
	if v == 0 {
		v = -1
	}
	b.binding[party] = v
	return uint64(v - 1), v > 0
}

func (b *batch2) canHold(holder uint64) bool {
	b.allowed = grow(b.allowed, int(holder))
	if v := b.allowed[holder]; v != 0 {
		return v == 2
	}
	ok := !b.cellAt(pid(allowedDomain, holder, 0), func() common.Hash { return k2Allowed(holder) }).value.IsZero()
	b.allowed[holder] = 1
	if ok {
		b.allowed[holder] = 2
	}
	return ok
}

func move2(source, target *cell, amount *uint256.Int) bool {
	if source.value.Lt(amount) {
		return false
	}
	source.value.Sub(&source.value, amount)
	target.value.Add(&target.value, amount)
	return true
}

func (b *batch2) moveShadow(tok, from, to uint64, amount *uint256.Int) bool {
	src := b.shadow(tok, from)
	if src.value.Lt(amount) {
		return false
	}
	return move2(src, b.shadow(tok, to), amount)
}

func (b *batch2) moveHeld(tok, from, to uint64, amount *uint256.Int) bool {
	if !b.canHold(to) {
		return false
	}
	src := b.held(tok, from)
	if src.value.Lt(amount) {
		return false
	}
	return move2(src, b.held(tok, to), amount)
}

func (b *batch2) flush() {
	var keys, vals []common.Hash
	for i, c := range b.flushList {
		if c.value.Eq(&c.original) {
			continue
		}
		v := common.Hash(c.value.Bytes32())
		if b.bulk == nil {
			b.state.SetState(b.addr, b.keys[i], v)
			continue
		}
		keys = append(keys, b.keys[i])
		vals = append(vals, v)
	}
	if b.bulk != nil && len(keys) > 0 {
		b.bulk.SetStates(b.addr, keys, vals)
	}
}

type row2 struct {
	ref                     uint64
	security, seller, buyer uint64
	quantity, consideration uint256.Int
}

func runSettlePacked2(
	evm contract.AccessibleState, addr common.Address, body []byte, n uint64, remaining uint64,
) ([]byte, uint64, error) {
	b := newBatch2(evm.GetStateDB(), addr)
	cash := b.cellAt(pid(cashDomain, 0, 0), k2Cash).value.Uint64()
	if cash == 0 {
		return nil, remaining, vm.ErrExecutionReverted
	}
	cash--
	rows := make([]row2, n)
	for i := uint64(0); i < n; i++ {
		r := body[i*packedRowBytes:]
		o := &rows[i]
		o.ref = be(r[0:8])
		o.security = be(r[8:10])
		o.seller = be(r[10:12])
		o.buyer = be(r[12:14])
		o.quantity.SetUint64(be(r[14:18]))
		o.consideration.SetUint64(be(r[18:26]))
	}
	if b.bulk != nil {
		b.prefetch(rows, cash)
	}
	return apply2(b, evm, addr, rows, cash, remaining)
}

// prefetch reads every slot the rows can touch in three bulk rounds: bindings, then
// holding flags, then balances and handled words.
func (b *batch2) prefetch(rows []row2, cash uint64) {
	for i := range rows {
		r := &rows[i]
		b.want(pid(bindingDomain, r.seller, 0), k2Binding(r.seller))
		b.want(pid(bindingDomain, r.buyer, 0), k2Binding(r.buyer))
	}
	b.fetch()
	hs := make([]int64, len(rows)*2)
	for i := range rows {
		r := &rows[i]
		s, sok := b.holderOf(r.seller)
		u, uok := b.holderOf(r.buyer)
		hs[2*i], hs[2*i+1] = -1, -1
		if sok && uok {
			hs[2*i], hs[2*i+1] = int64(s), int64(u)
			if s != u {
				b.want(pid(allowedDomain, s, 0), k2Allowed(s))
				b.want(pid(allowedDomain, u, 0), k2Allowed(u))
			}
		}
	}
	b.fetch()
	for i := range rows {
		r := &rows[i]
		b.want(6<<56|r.ref>>8, k2Handled(r.ref>>8))
		if hs[2*i] < 0 {
			continue
		}
		b.want(pid(shadowDomain, r.security, r.seller), k2Shadow(r.security, r.seller))
		b.want(pid(shadowDomain, r.security, r.buyer), k2Shadow(r.security, r.buyer))
		b.want(pid(shadowDomain, cash, r.seller), k2Shadow(cash, r.seller))
		b.want(pid(shadowDomain, cash, r.buyer), k2Shadow(cash, r.buyer))
		if s, u := uint64(hs[2*i]), uint64(hs[2*i+1]); s != u {
			b.want(pid(heldDomain, r.security, s), k2Held(r.security, s))
			b.want(pid(heldDomain, r.security, u), k2Held(r.security, u))
			b.want(pid(heldDomain, cash, s), k2Held(cash, s))
			b.want(pid(heldDomain, cash, u), k2Held(cash, u))
		}
	}
	b.fetch()
}

// apply2 is apply on indices; the row semantics and the batch log are the same. The
// per-row Settled event needs addresses and is not emitted in keyspace 2.
func apply2(
	b *batch2, evm contract.AccessibleState, addr common.Address, rows []row2, cash uint64, remaining uint64,
) ([]byte, uint64, error) {
	seqCell := b.cellAt(1<<63, seqKey)
	var startSeq uint256.Int
	startSeq.Set(&seqCell.value)
	blockNum := evm.GetBlockContext().Number().Uint64()
	one := uint256.NewInt(1)

	bitmapBytes := (len(rows) + 7) / 8
	var bitmaps [reasonCount][]byte
	for i := range bitmaps {
		bitmaps[i] = make([]byte, bitmapBytes)
	}
	applied := bitmaps[reasonSettled]

	for i := range rows {
		r := &rows[i]
		if r.quantity.IsZero() || r.consideration.IsZero() || r.seller == r.buyer {
			setBit(bitmaps[reasonMalformed], i)
			continue
		}
		handled, bit := b.handledCell(r.ref)
		wordIdx, mask := bit>>6, uint64(1)<<(bit&63)
		if handled.value[wordIdx]&mask != 0 {
			setBit(bitmaps[reasonDuplicate], i)
			continue
		}
		sh, sOK := b.holderOf(r.seller)
		bh, bOK := b.holderOf(r.buyer)
		if !sOK || !bOK {
			setBit(bitmaps[reasonUnbound], i)
			continue
		}
		if !b.moveShadow(r.security, r.seller, r.buyer, &r.quantity) {
			setBit(bitmaps[reasonShortSecurity], i)
			continue
		}
		if !b.moveShadow(cash, r.buyer, r.seller, &r.consideration) {
			b.moveShadow(r.security, r.buyer, r.seller, &r.quantity)
			setBit(bitmaps[reasonShortCash], i)
			continue
		}
		if sh != bh {
			if !b.moveHeld(r.security, sh, bh, &r.quantity) {
				b.moveShadow(r.security, r.buyer, r.seller, &r.quantity)
				b.moveShadow(cash, r.seller, r.buyer, &r.consideration)
				setBit(bitmaps[reasonShortCustodySecurity], i)
				continue
			}
			if !b.moveHeld(cash, bh, sh, &r.consideration) {
				b.moveHeld(r.security, bh, sh, &r.quantity)
				b.moveShadow(r.security, r.buyer, r.seller, &r.quantity)
				b.moveShadow(cash, r.seller, r.buyer, &r.consideration)
				setBit(bitmaps[reasonShortCustodyCash], i)
				continue
			}
		}
		seqCell.value.Add(&seqCell.value, one)
		handled.value[wordIdx] |= mask
		setBit(applied, i)
	}
	if !logsDisabled {
		emitSettleBatch(b.state, addr, blockNum, &startSeq, uint64(len(rows)), bitmaps)
	}
	b.flush()

	out := make([]byte, 64+((len(applied)+31)/32)*32)
	out[31] = 32
	length := uint256.NewInt(uint64(len(applied))).Bytes32()
	copy(out[56:64], length[24:32])
	copy(out[64:], applied)
	return out, remaining, nil
}

// runCreditPacked2 opens positions on keyspace-2 slots: shadow (token, party), held
// (token, holder), supply (token).
func runCreditPacked2(
	evm contract.AccessibleState, addr common.Address, amt *uint256.Int, body []byte, n uint64, remaining uint64,
) ([]byte, uint64, error) {
	b := newBatch2(evm.GetStateDB(), addr)
	b.bulk = nil
	for i := uint64(0); i < n; i++ {
		r := body[i*packedPositionBytes:]
		tok, party := be(r[0:2]), be(r[2:4])
		if b.state.GetState(addr, tokenAtKey(tok)) == (common.Hash{}) ||
			b.state.GetState(addr, partyAtKey(party)) == (common.Hash{}) {
			return nil, remaining, vm.ErrExecutionReverted
		}
		h, bound := b.holderOf(party)
		if !bound || !b.canHold(h) {
			return nil, remaining, vm.ErrExecutionReverted
		}
		add(b.shadow(tok, party), amt)
		add(b.held(tok, h), amt)
		add(b.cellAt(pid(10, tok, 0), func() common.Hash { return k2Supply(tok) }), amt)
	}
	b.flush()
	return nil, remaining, nil
}
