package settleledger

// A settlement batch touches few distinct slots many times: a few hundred rows across a
// handful of parties, instruments and omnibuses. Read through and written back per row,
// that is four storage writes a row for balances alone, and a fresh big.Int for every
// intermediate value.
//
// This holds the batch's slots in memory instead. A slot is read from state once, mutated
// in place, and written back once at the end — and only if it actually changed. Row
// semantics are unchanged because the running value lives in the cache: a row still sees
// the balance left by the rows before it, and is still skipped if that balance is short.
//
// Values are uint256 rather than big.Int. Storage is a 32-byte word either way, so this
// changes no arithmetic; it removes the allocation per operation.
//
// Key derivation is cached alongside the values. A slot key is a keccak over the
// (domain, token, party) triple that names it, and a batch names the same few triples
// thousands of times, so each is hashed once and looked up thereafter. Every memo here
// is a pure function of its key or, for the index tables, a value nothing on the settle
// path can write — and the batch lives for one call, so nothing carries across calls.

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/holiman/uint256"
)

// cell is one storage slot, with the value it held when first read so an unchanged slot
// can be left alone at flush.
type cell struct {
	value    uint256.Int
	original uint256.Int
}

// slotRef names a ledger slot by the triple that derives it, so one keccak — and one
// cell lookup — serves every reference to that slot in the batch.
type slotRef struct {
	domain byte
	a, b   common.Address
}

// indexRef names an index-table entry the same way.
type indexRef struct {
	domain byte
	i      uint64
}

type batch struct {
	state contract.StateDB
	addr  common.Address

	// kh is the batch's one keccak state, reset per derivation, so a key costs a
	// permutation rather than a permutation plus a fresh hasher.
	kh crypto.KeccakState

	cells   map[common.Hash]*cell
	slots   map[slotRef]*cell
	indexed map[indexRef]common.Address
	handled map[uint256.Int]*cell
	holders map[common.Address]common.Address
	allows  map[common.Address]bool
}

func newBatch(state contract.StateDB, addr common.Address) *batch {
	return &batch{
		state:   state,
		addr:    addr,
		kh:      crypto.NewKeccakState(),
		cells:   make(map[common.Hash]*cell),
		slots:   make(map[slotRef]*cell),
		indexed: make(map[indexRef]common.Address),
		handled: make(map[uint256.Int]*cell),
		holders: make(map[common.Address]common.Address),
		allows:  make(map[common.Address]bool),
	}
}

// cell returns the slot, reading it from state the first time it is asked for.
func (b *batch) cell(key common.Hash) *cell {
	if c, held := b.cells[key]; held {
		return c
	}
	c := new(cell)
	c.value.SetBytes(b.state.GetState(b.addr, key).Bytes())
	c.original.Set(&c.value)
	b.cells[key] = c
	return c
}

func (b *batch) get(key common.Hash) *uint256.Int { return &b.cell(key).value }

func (b *batch) set(key common.Hash, v *uint256.Int) { b.cell(key).value.Set(v) }

// cellFor resolves a (domain, a, b) triple straight to its cell, hashing the key and
// looking the cell up once per triple per batch. The key is a pure function of the
// triple and the cell is the batch's single copy of the slot, so the memo cannot go
// stale.
func (b *batch) cellFor(domain byte, x, y common.Address) *cell {
	ref := slotRef{domain: domain, a: x, b: y}
	if c, seen := b.slots[ref]; seen {
		return c
	}
	c := b.cell(slotWith(b.kh, domain, x, y))
	b.slots[ref] = c
	return c
}

// handledCell returns the replay-bitmap word covering a reference, with the bit that
// stands for it. References in one block share a word, so a run of consecutive
// references derives its key once; the block is a pure function of the reference, so
// the memo cannot go stale.
func (b *batch) handledCell(ref *uint256.Int) (*cell, uint) {
	var block uint256.Int
	block.Rsh(ref, 8)
	if c, seen := b.handled[block]; seen {
		return c, uint(ref[0] & 255)
	}
	key, bit := handledKey(b.kh, ref)
	c := b.cell(key)
	b.handled[block] = c
	return c, bit
}

// flush writes back every slot whose value moved. A slot read but not changed is not
// written, so a batch that settles nothing writes nothing.
func (b *batch) flush() {
	for key, c := range b.cells {
		if c.value.Eq(&c.original) {
			continue
		}
		b.state.SetState(b.addr, key, c.value.Bytes32())
	}
}

// holderOf resolves the omnibus a party clears through, once per party per batch. A party
// bound to nothing cannot settle, so this reports that separately rather than returning
// the zero address and letting an unbound party trade against slot zero.
func (b *batch) holderOf(party common.Address) (common.Address, bool) {
	if h, seen := b.holders[party]; seen {
		return h, h != (common.Address{})
	}
	h := common.Address(b.get(slotWith(b.kh, bindingDomain, common.Address{}, party)).Bytes20())
	b.holders[party] = h
	return h, h != (common.Address{})
}

// canHold is the holding restriction, once per holder per batch.
func (b *batch) canHold(holder common.Address) bool {
	if allowed, seen := b.allows[holder]; seen {
		return allowed
	}
	allowed := !b.get(slotWith(b.kh, allowedDomain, common.Address{}, holder)).IsZero()
	b.allows[holder] = allowed
	return allowed
}

// move transfers within one ledger. It reports whether the source could cover the amount
// rather than reverting, because one short party must not cost the rest of the batch its
// settlement.
func (b *batch) move(domain byte, token, from, to common.Address, amount *uint256.Int) bool {
	source := b.cellFor(domain, token, from)
	if source.value.Lt(amount) {
		return false
	}
	source.value.Sub(&source.value, amount)
	target := b.cellFor(domain, token, to)
	target.value.Add(&target.value, amount)
	return true
}

func (b *batch) moveShadow(token, from, to common.Address, amount *uint256.Int) bool {
	return b.move(shadowDomain, token, from, to, amount)
}

// moveHeld is the only path custody moves by, which is what makes the holding
// restriction hold: a destination that may not hold cannot receive.
func (b *batch) moveHeld(token, from, to common.Address, amount *uint256.Int) bool {
	if !b.canHold(to) {
		return false
	}
	return b.move(heldDomain, token, from, to, amount)
}

// addressAt reads an index table, deriving the key and reading the slot once per index
// per batch. Nothing on the settle path writes an index table, so the memoized address
// cannot go stale within the batch. A packed row names its instrument and parties by
// index, so a few hundred rows resolve a handful of entries between them.
func (b *batch) addressAt(domain byte, i uint64) common.Address {
	ref := indexRef{domain: domain, i: i}
	if a, seen := b.indexed[ref]; seen {
		return a
	}
	a := common.Address(b.get(indexKeyWith(b.kh, domain, i)).Bytes20())
	b.indexed[ref] = a
	return a
}
