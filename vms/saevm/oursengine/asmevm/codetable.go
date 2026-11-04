package asmevm

import (
	"math/bits"
	"unsafe"
)

// codeTable lists the accounts the native CALL may enter without the host
// (amd64 core): warm in this transaction, not a precompile, with code and its
// pre-decoded stream. Go inserts an account when a frame of its code starts
// in runAsm and empties the table (generation bump) when warmth or code can
// change: revert of a warm-address or account-creation record, SetCode,
// SELFDESTRUCT, a new transaction. Pointers are stored as integers; the
// account (code) and its decoded stream keep the targets alive. Same probing
// as slotTable.
type codeTable struct {
	gen   uint64
	shift uint64
	mask  uint64
	n     uint64
	e     []codeEntry
	// cold CALL in the core (amd64): cidx indexes every enterable account of
	// the loaded snapshot (State.IndexStorage), and cold is 1 while this table
	// holds every warm address of the tx (a marker entry, code 0, for the ones
	// the core may not enter), so a miss here is a cold address. Any reset
	// (revert of a warm mark, new code, SELFDESTRUCT) clears it until the next tx.
	cidx *codeTable
	cold uint64
}

type codeEntry struct {
	addr     addrWords
	gen      uint64
	code     uintptr
	codeLen  uint64
	handlers uintptr
	args     uintptr
}

var _ [64]byte = [unsafe.Sizeof(codeEntry{})]byte{} // core_amd64.s steps by 64

func (t *codeTable) find(a *addrWords) *codeEntry {
	for i := (a.hash() * slotHashMul) >> t.shift; ; i = (i + 1) & t.mask {
		if e := &t.e[i]; e.gen != t.gen || e.addr == *a {
			return e
		}
	}
}

func (t *codeTable) alloc(n int) {
	t.e = make([]codeEntry, n)
	t.mask = uint64(n - 1)
	t.shift = 64 - uint64(bits.TrailingZeros(uint(n)))
	t.n = 0
}

func (t *codeTable) put(a *addrWords, code []byte, d *decoded) {
	e, _ := t.slot(a)
	e.code, e.codeLen = uintptr(unsafe.Pointer(unsafe.SliceData(code))), uint64(len(code))
	e.handlers = uintptr(unsafe.Pointer(unsafe.SliceData(d.handlers)))
	e.args = uintptr(unsafe.Pointer(unsafe.SliceData(d.args)))
}

// slot returns the entry of a, inserting it (growing the table) if absent,
// and whether it was absent.
func (t *codeTable) slot(a *addrWords) (*codeEntry, bool) {
	e := t.find(a)
	if e.gen == t.gen {
		return e, false
	}
	if 2*(t.n+1) > uint64(len(t.e)) {
		old, gen := t.e, t.gen
		t.alloc(2 * len(old))
		for i := range old {
			if old[i].gen == gen {
				*t.find(&old[i].addr) = old[i]
				t.n++
			}
		}
		e = t.find(a)
	}
	e.addr, e.gen = *a, t.gen
	t.n++
	return e, true
}

func (t *codeTable) reset() {
	t.gen, t.n = t.gen+1, 0
	if t.gen == 0 { // wrapped: zeroed entries would match generation 0
		clear(t.e)
		t.gen = 1
	}
}

// resetCodes empties the code table, if any.
func (s *State) resetCodes() {
	if s.ctab != nil {
		s.ctab.reset()
		s.ctab.cold = 0
	}
}

// noteWarm keeps the table complete while cold is 1 (see codeTable.cold): addr
// just became warm.
func (s *State) noteWarm(addr Address) {
	t := s.ctab
	if t == nil || t.cold == 0 {
		return
	}
	w := wordsOf(&addr)
	e, added := t.slot(&w)
	if !added {
		return
	}
	*e = codeEntry{addr: w, gen: t.gen} // marker: warm, not entered by the core
	if c := t.cidx.find(&w); c.gen == t.cidx.gen {
		e.code, e.codeLen, e.handlers, e.args = c.code, c.codeLen, c.handlers, c.args
	}
}

// anyPrecompile: addr is a precompile here or on any chain the EVM knows (the
// core never enters it).
func anyPrecompile(addr Address) bool {
	_, pre := precompile(addr)
	return pre || addr == p256Addr || addr == warpAddr || addr[0] == 1 && addr == Address{0: 1, 19: addr[19]}
}

// indexCodes builds codeTable.cidx: every loaded account with code the core
// can run (decoded), not a precompile.
func (s *State) indexCodes() {
	n := 0
	for _, a := range s.accounts {
		if len(a.code) > 0 {
			n++
		}
	}
	x := &codeTable{gen: 1}
	x.alloc(1 << bits.Len(uint(2*n+1)))
	for addr, a := range s.accounts {
		if len(a.code) > 0 && a.dec != nil && !anyPrecompile(addr) {
			w := wordsOf(&addr)
			x.put(&w, a.code, a.dec)
		}
	}
	if s.ctab == nil {
		s.ctab = &codeTable{gen: 1}
		s.ctab.alloc(16)
	}
	s.ctab.cidx = x
}

// noteCode inserts the code of a starting frame when the native CALL may
// enter it directly next time: warm, not a precompile, the account's own code.
func (s *State) noteCode(c *Contract) {
	if s.ctab == nil {
		s.ctab = &codeTable{gen: 1}
		s.ctab.alloc(16)
	}
	a := s.accounts[c.codeAddr]
	if a == nil || len(c.Code) == 0 || len(a.code) != len(c.Code) || &a.code[0] != &c.Code[0] || a.dec != c.dec ||
		!s.warmAddr.has(&c.codeAddr, addrHash(&c.codeAddr)) {
		return
	}
	if _, pre := precompile(c.codeAddr); pre {
		return
	}
	w := wordsOf(&c.codeAddr)
	s.ctab.put(&w, c.Code, c.dec)
}
