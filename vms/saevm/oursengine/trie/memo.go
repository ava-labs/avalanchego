package trie

import (
	"math/bits"
	"sort"
	"sync/atomic"
)

// A cut memo keeps, in RAM only, the positions of one or two levels below a cut, the base run
// start of each sub-cut at its lowest level and the live keys of each, so a Root rehashes only
// the sub-cuts holding a changed key instead of the whole cut (a cut of a few hundred keys whose
// one key changed: a few keys and two branches instead of every key). Nothing of it is written:
// the stored top, the top file and every root are what they are without it. It is filled the
// first time its cut is recomputed, kept while the cut keeps changing (memoKeep files) and goes
// when its trie is re-cut.

// subMask marks the dirty sub-cuts of a cut at two nibbles below it (the key's nibbles d and d+1).
type subMask [4]uint64

var allSubs = subMask{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)}

// subOf is k's sub-cut two nibbles below depth d.
func subOf(k *[32]byte, d int) int { return int(nib(k, d))<<4 | int(nib(k, d+1)) }

func (m *subMask) set(i int) { m[i>>6] |= 1 << (i & 63) }

// any reports whether a bit in [lo, hi) is set (hi - lo a power of 16, lo aligned to it).
func (m *subMask) any(lo, hi int) bool {
	if hi-lo >= 64 {
		for w := lo >> 6; w < hi>>6; w++ {
			if m[w] != 0 {
				return true
			}
		}
		return false
	}
	return m[lo>>6]>>(lo&63)&(1<<(hi-lo)-1) != 0
}

func (m *subMask) count() int {
	return bits.OnesCount64(m[0]) + bits.OnesCount64(m[1]) + bits.OnesCount64(m[2]) + bits.OnesCount64(m[3])
}

type cutMemo struct {
	m   uint8    // levels kept below the cut: 0 (none: not filled, or the cut is small), 1 or 2
	gen uint64   // the engine's file generation of its last use
	pos []Pos    // level d+1 (16), then level d+2 (256) when m = 2
	bnd []uint32 // sub-cut s at level d+m: base run [bnd[s], bnd[s+1]) from the cut's run start
	n   []uint32 // live keys of each sub-cut at level d+m
}

// memoOff is the start in pos of the level j below the cut (j >= 1).
func memoOff(j int) int { return levelOff(j) - 1 }

func (m *cutMemo) bytes() int { return 80 + len(m.pos)*34 + len(m.bnd)*4 + len(m.n)*4 }

// subBounds records, while a cut's run is gathered, the run start of every sub-cut two nibbles
// below the cut (relative to the run's start).
type subBounds struct {
	d     int
	start uint64
	next  int
	b     [257]uint32
}

func (s *subBounds) reset(d int, start uint64) { s.d, s.start, s.next = d, start, 0 }

// at notes a base record of key k at run position p.
func (s *subBounds) at(k *[32]byte, p uint64) {
	for x := subOf(k, s.d); s.next <= x; s.next++ {
		s.b[s.next] = uint32(p - s.start)
	}
}

func (s *subBounds) end(p uint64) {
	for ; s.next <= 256; s.next++ {
		s.b[s.next] = uint32(p - s.start)
	}
}

// Memo defaults: a cut of at least memoKeys[0] keys keeps one level, of memoKeys[1] two.
var defaultMemoKeys = [2]int{32, 128}

// memoKeep: a memo not used for this many files after its last use goes.
const memoKeep = 1

func (e *Engine) memoLevels(n int) uint8 {
	k := e.memoKeys
	if k == [2]int{} {
		k = defaultMemoKeys
	}
	switch {
	case n >= k[1]:
		return 2
	case n >= k[0]:
		return 1
	}
	return 0
}

// memoGrown empties memo when its cut, now of n keys, has grown to more levels than it keeps:
// the cut's next recompute fills it again at the deeper level (a memo filled while its cut was
// small would otherwise keep one level while the cut grows to the re-cut limit, and every write
// would rehash a sixteenth of the cut).
func (e *Engine) memoGrown(m *cutMemo, n int) {
	if m != nil && m.m > 0 && e.memoLevels(n) > m.m {
		m.m = 0
	}
}

// memoFor is cut c's memo, created empty (called before the parallel phases: it writes t.memo).
func (e *Engine) memoFor(t *Trie, c uint32) *cutMemo {
	if e.NoMemo {
		return nil
	}
	e.memoMap(t)
	m := t.memo[c]
	if m == nil {
		m = &cutMemo{}
		t.memo[c] = m
	}
	m.gen = e.gen
	return m
}

// memoMap makes t's memo map when it has none and registers t with the sweep.
func (e *Engine) memoMap(t *Trie) {
	if t.memo == nil {
		t.memo = map[uint32]*cutMemo{}
		if e.memoTries == nil {
			e.memoTries = map[*Trie]struct{}{}
		}
		e.memoTries[t] = struct{}{}
	}
}

// sweepMemos drops the memos not used for memoKeep files and counts the rest.
func (e *Engine) sweepMemos() {
	var n, b uint64
	for t := range e.memoTries {
		for c, m := range t.memo {
			if m.gen+memoKeep < e.gen {
				delete(t.memo, c)
				e.S.MemoDrops++
				continue
			}
			n++
			b += uint64(m.bytes())
		}
		if len(t.memo) == 0 {
			t.memo = nil
			delete(e.memoTries, t)
		}
	}
	e.S.MemoCuts, e.S.MemoBytes = n, b
}

// runOf is the base run that holds key k: its sub-cut's when its cut has a filled memo, else
// its cut's (storage: bytes; accounts: ordinals).
func (t *Trie) runOf(k *[32]byte) (lo, hi uint64) {
	c := t.cutOf(k)
	lo, hi = t.bound(int(c)), t.bound(int(c)+1)
	if m := t.memo[c]; m != nil && m.m > 0 {
		s := 0
		for j := range int(m.m) {
			s = s<<4 | int(nib(k, int(t.depth)+j))
		}
		lo, hi = lo+uint64(m.bnd[s]), lo+uint64(m.bnd[s+1])
	}
	return lo, hi
}

// subList is the part of overlay list l (sorted by trie key) whose keys' m nibbles from depth d
// lie in [s0, s1).
func (e *Engine) subList(w *worker, l [][32]byte, acct bool, d, m, s0, s1 int) [][32]byte {
	idx := func(i int) int {
		h := e.keyHash(&l[i], acct, w.h.k)
		x := 0
		for j := range m {
			x = x<<4 | int(nib(&h, d+j))
		}
		return x
	}
	i0 := sort.Search(len(l), func(i int) bool { return idx(i) >= s0 })
	i1 := i0 + sort.Search(len(l)-i0, func(i int) bool { return idx(i0+i) >= s1 })
	return l[i0:i1]
}

// gather appends to w.kvs the leaves of base run [lo, hi) (storage: bytes; accounts: ordinals)
// merged with overlay keys ovk; sb, when set, records the sub-cut bounds of the run.
func (e *Engine) gather(w *worker, t *Trie, storage bool, ovk [][32]byte, lo, hi uint64, sb *subBounds) {
	if storage {
		e.storageRange(w, t, ovk, lo, hi, sb)
	} else {
		e.accountRange(w, t, ovk, lo, hi, sb)
	}
}

func (e *Engine) begin(w *worker) {
	if w.pend == 0 {
		w.h.Begin()
		w.arena = w.arena[:0]
	}
	w.kvs = w.kvs[:0]
}

func (e *Engine) added(w *worker, n int) {
	w.pend += n
	if w.pend >= batchLeaves {
		w.h.Finish()
		w.pend = 0
	}
}

// cutRun recomputes dirty cut c of t: the whole run (filling memo when it is set and the cut
// is big enough), or, with a filled memo, only the sub-cuts the mask marks. It returns the cut's
// live keys. A cut computed through its memo gets its position from cutTops.
func (e *Engine) cutRun(w *worker, t *Trie, storage bool, c uint32, dirty *subMask, memo *cutMemo) int {
	d := int(t.depth)
	lo, hi := t.bound(int(c)), t.bound(int(c)+1)
	if memo == nil || memo.m == 0 {
		e.begin(w)
		var sb *subBounds
		if memo != nil {
			sb = &w.sb
			sb.reset(d, lo)
		}
		e.gather(w, t, storage, t.ov[c], lo, hi, sb)
		n := len(w.kvs)
		var m uint8
		if memo != nil {
			m = e.memoLevels(n)
		}
		if m == 0 {
			w.h.Add(w.kvs, d, t.cut(c))
			e.added(w, n+1)
			return n
		}
		ns := 1 << (4 * m)
		memo.m = m
		memo.pos = make([]Pos, memoOff(int(m)+1))
		memo.bnd = make([]uint32, ns+1)
		memo.n = make([]uint32, ns)
		step := 1 << (4 * (2 - m))
		for s := 0; s <= ns; s++ {
			memo.bnd[s] = sb.b[s*step]
		}
		kvs, off := w.kvs, memoOff(int(m))
		for s := 0; s < ns; s++ {
			j := 0
			for j < len(kvs) {
				x := 0
				for i := range int(m) {
					x = x<<4 | int(nib(kvs[j].key, d+i))
				}
				if x != s {
					break
				}
				j++
			}
			w.h.Add(kvs[:j], d+int(m), &memo.pos[off+s])
			memo.n[s] = uint32(j)
			kvs = kvs[j:]
		}
		e.added(w, n+ns)
		*dirty = allSubs // cutTops computes every memo level of a new memo
		atomic.AddUint64(&e.S.MemoFills, 1)
		return n
	}
	m := int(memo.m)
	ns, step, off := 1<<(4*m), 1<<(4*(2-m)), memoOff(m)
	for s := 0; s < ns; s++ {
		if !dirty.any(s*step, (s+1)*step) {
			continue
		}
		e.begin(w)
		ovk := e.subList(w, t.ov[c], !storage, d, m, s, s+1)
		e.gather(w, t, storage, ovk, lo+uint64(memo.bnd[s]), lo+uint64(memo.bnd[s+1]), nil)
		memo.n[s] = uint32(len(w.kvs))
		w.h.Add(w.kvs, d+m, &memo.pos[off+s])
		e.added(w, len(w.kvs)+1)
	}
	atomic.AddUint64(&e.S.MemoHits, 1)
	n := 0
	for _, x := range memo.n {
		n += int(x)
	}
	return n
}
