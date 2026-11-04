package asmevm

import (
	"bytes"
	"math/bits"
	"os"
	"unsafe"

	"github.com/holiman/uint256"
)

// slotTable caches the warm storage slots of a State for the native SLOAD
// and SSTORE (amd64 core): an entry exists only for a slot that is warm in
// this transaction and holds its current and original value. Go code inserts (host SLOAD and
// SSTORE) and keeps it right on writes, reverts and a new transaction; the
// assembly only reads. Open addressing with linear probing over (account,
// slot), pointer-free entries, at most half full; an entry whose gen is not
// the table's is empty. One table per State, made by the first native frame.
type slotTable struct {
	gen   uint64
	shift uint64 // 64 - log2(len(e))
	mask  uint64
	n     uint64
	e     []slotEntry
	// cold SLOAD in the core (amd64): vtab is the State's value table, and
	// cold is 1 while every slot Go holds warm in this transaction is in this
	// table, so a miss here is a cold slot (a revert clears it to 0).
	vtab *slotTable
	cold uint64
}

type slotEntry struct {
	key  uint256.Int
	val  uint256.Int
	orig uint256.Int // value at the start of the transaction (native SSTORE gas)
	addr addrWords
	gen  uint64
}

// addrWords is an address as the low three limbs of its stack word (the
// third holds the top 4 bytes), the form the core reads from the stack.
type addrWords [3]uint64

func wordsOf(a *Address) addrWords {
	var w uint256.Int
	w.SetBytes20(a[:])
	return addrWords{w[0], w[1], w[2]}
}

var _ [128]byte = [unsafe.Sizeof(slotEntry{})]byte{} // core_amd64.s steps by 128

const slotHashMul = 0x9E3779B97F4A7C15 // also in core_amd64.s

func (a *addrWords) hash() uint64 { return a[0] ^ a[1] ^ a[2] }

func newSlotTable() *slotTable {
	t := &slotTable{gen: 1}
	t.alloc(32)
	return t
}

func (t *slotTable) alloc(n int) {
	t.e = make([]slotEntry, n)
	t.mask = uint64(n - 1)
	t.shift = 64 - uint64(bits.TrailingZeros(uint(n)))
	t.n = 0
}

// find returns the entry of (a, k), or the empty entry where it would go.
func (t *slotTable) find(a *addrWords, k *uint256.Int) *slotEntry {
	for i := ((k[0] ^ k[1] ^ k[2] ^ k[3] ^ a.hash()) * slotHashMul) >> t.shift; ; i = (i + 1) & t.mask {
		if e := &t.e[i]; e.gen != t.gen || e.key == *k && e.addr == *a {
			return e
		}
	}
}

func (t *slotTable) put(a *addrWords, k, v, orig *uint256.Int) {
	e := t.find(a, k)
	if e.gen != t.gen {
		if 2*(t.n+1) > uint64(len(t.e)) {
			t.grow()
			e = t.find(a, k)
		}
		e.key, e.addr, e.gen = *k, *a, t.gen
		t.n++
	}
	e.val, e.orig = *v, *orig
}

// update sets the value of (a, k) if it is cached.
func (t *slotTable) update(a *addrWords, k, v *uint256.Int) {
	if e := t.find(a, k); e.gen == t.gen {
		e.val = *v
	}
}

func (t *slotTable) reset() {
	t.gen, t.n = t.gen+1, 0
	if t.gen == 0 { // wrapped: zeroed entries would match generation 0
		clear(t.e)
		t.gen = 1
	}
}

func (t *slotTable) grow() {
	old, gen := t.e, t.gen
	t.alloc(2 * len(old))
	for i := range old {
		if old[i].gen == gen {
			*t.find(&old[i].addr, &old[i].key) = old[i]
			t.n++
		}
	}
}

// slotTable returns the table of s, creating it.
func (s *State) slotTable() *slotTable {
	if s.stab == nil {
		s.stab = newSlotTable()
	}
	return s.stab
}

// nativeJournal holds the SSTOREs the native core made since Go last ran
// (amd64): the core changes only the slotTable entry and appends a record
// here. flushNative replays the records in order through SetStorage and the
// refund counter before any Go code reads the state (every host callback,
// asmChildExit, the end of runAsm), so the Go journal gets the same entries,
// in the same order, as if the SSTOREs had run in Go. pending is the number
// of Go journal entries the records will add: a native snapshot is
// len(journal) + pending.
type nativeJournal struct {
	n       uint64
	pending uint64
	e       [nativeJournalCap]nativeSStore
	dlen    uint64               // bytes of data used by the pending LOG records
	data    [nativeLogArena]byte // LOG data, copied out by flushNative
}

const (
	nativeJournalCap = 32
	nativeLogArena   = 8192
)

type nativeSStore struct {
	addr addrWords
	kind uint64 // 0: SSTORE; 1: cold SLOAD (the slot became warm, val unused); 2: cold CALL or EXTCODESIZE (the account became warm);
	//              3 | topics<<8: LOG (key, val: topics 0 and 1; r1, r2: data offset and length in nativeJournal.data); 4: topics 2 and 3 of the LOG before
	key    uint256.Int
	val    uint256.Int
	r1, r2 int64 // refund changes in gasSStoreEIP2929 order; 0: none
}

var _ [112]byte = [unsafe.Sizeof(nativeSStore{})]byte{} // core_amd64.s steps by 112

func (s *State) nativeJournal() *nativeJournal {
	if s.nj == nil {
		s.nj = new(nativeJournal)
	}
	return s.nj
}

func refundChange(s *State, r int64) {
	switch {
	case r > 0:
		s.addRefund(uint64(r))
	case r < 0:
		s.subRefund(uint64(-r))
	}
}

// flushNative replays the native SSTOREs (see nativeJournal).
func (s *State) flushNative() {
	nj := s.nj
	if nj == nil || nj.n == 0 {
		return
	}
	var last Address
	var acct *account
	for i := 0; i < int(nj.n); i++ {
		r := &nj.e[i]
		if r.kind&0xff == 3 { // LOG
			w := uint256.Int{r.addr[0], r.addr[1], r.addr[2], 0}
			topics := make([]Hash, r.kind>>8)
			for j := range topics {
				switch j {
				case 0:
					topics[j] = Hash(r.key.Bytes32())
				case 1:
					topics[j] = Hash(r.val.Bytes32())
				case 2:
					topics[j] = Hash(nj.e[i+1].key.Bytes32())
				case 3:
					topics[j] = Hash(nj.e[i+1].val.Bytes32())
				}
			}
			var data []byte // nil when empty, as Memory.getCopy
			if r.r2 > 0 {
				data = bytes.Clone(nj.data[r.r1 : r.r1+r.r2])
			}
			s.addLog(Log{Address: Address(w.Bytes20()), Topics: topics, Data: data})
			if len(topics) > 2 {
				i++ // the kind-4 record
			}
			continue
		}
		if r.kind == 2 { // cold CALL: the core found the address cold and cached it already
			w := uint256.Int{r.addr[0], r.addr[1], r.addr[2], 0}
			addr := Address(w.Bytes20())
			s.warmAddr.add(&addr, addrHash(&addr))
			e := s.jnew()
			e.kind, e.addr = jWarmAddr, addr
			continue
		}
		if r.kind == 1 {
			w := uint256.Int{r.addr[0], r.addr[1], r.addr[2], 0}
			addr, slot := Address(w.Bytes20()), Hash(r.key.Bytes32())
			s.warmSlot.add(&slotKey{addr, slot}, slotHash(&addr, &slot)) // the core found it cold and cached it already
			e := s.jnew()
			e.kind, e.addr, e.slot = jWarmSlot, addr, slot
			continue
		}
		refundChange(s, r.r1)
		refundChange(s, r.r2)
		w := uint256.Int{r.addr[0], r.addr[1], r.addr[2], 0}
		addr := Address(w.Bytes20())
		if acct == nil || addr != last { // consecutive SSTOREs are mostly of one account
			acct, last = s.getOrNew(addr), addr
		}
		s.setStorageFlushed(acct, addr, &r.addr, &r.key, &r.val)
	}
	nj.n, nj.pending, nj.dlen = 0, 0, 0
}

// nativeColdSload enables the native cold SLOAD (State.IndexStorage);
// ASMEVM_COLD=0 turns it off for A/B measurements.
var nativeColdSload = os.Getenv("ASMEVM_COLD") != "0"

// IndexStorage builds the value table: every storage slot the State holds now,
// under the slotTable layout, so the native core serves a cold SLOAD without
// the host (2100 gas, the slot cached as warm and journaled through the
// nativeJournal). Like the code analysis it is setup work, done once per
// loaded snapshot. Go keeps the values right (SetStorage, revert); slots that
// appear later are not added, they go through the host. ASMEVM_COLD=0: off.
func (s *State) IndexStorage() {
	if !nativeColdSload {
		return
	}
	n := 0
	for _, a := range s.accounts {
		n += len(a.storage)
	}
	v := &slotTable{gen: 1}
	v.alloc(1 << bits.Len(uint(2*n+1)))
	for addr, a := range s.accounts {
		w := wordsOf(&addr)
		for k, x := range a.storage {
			v.put(&w, new(uint256.Int).SetBytes32(k[:]), &x, &x)
		}
	}
	s.vtab = v
	s.slotTable().vtab = v
	s.indexCodes()
}

// forgetIndexed keeps the snapshot indexes right when the runner removes an
// account (EIP-6780: created and destroyed in the tx): its next incarnation
// starts with empty storage, so each of its value-table entries becomes 0
// (the core then serves the new incarnation's cold SLOADs natively), and its
// code-index entry becomes a tombstone (bit 63 of the third address word, which
// no address has, so linear probing still passes over it). Nothing else needs
// it: a CREATE/CREATE2 that passes the collision check finds every stored
// value of the account zero already (a non-zero one is a collision) and no
// code (so no code-index entry), and writes and reverts keep the value table
// right through SetStorage and revert. Removal happens after the tx, so no
// revert can undo it.
func (s *State) forgetIndexed(addr Address) {
	w := wordsOf(&addr)
	if a := s.accounts[addr]; a != nil && s.vtab != nil {
		for k := range a.storage {
			if e := s.vtab.find(&w, new(uint256.Int).SetBytes32(k[:])); e.gen == s.vtab.gen {
				e.val, e.orig = uint256.Int{}, uint256.Int{}
			}
		}
	}
	if s.ctab != nil && s.ctab.cidx != nil {
		if e := s.ctab.cidx.find(&w); e.gen == s.ctab.cidx.gen {
			e.addr[2] |= 1 << 63
		}
	}
}
