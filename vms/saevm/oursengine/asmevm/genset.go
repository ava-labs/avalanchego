package asmevm

import (
	"encoding/binary"
	"iter"
	"math/bits"
)

// genSet is a hash set whose clear is a generation bump: an entry belongs to
// the set while its gen is the set's and it is not dead (a reverted insert,
// kept so linear probing still passes over it). Go maps clear in time
// proportional to their capacity, which the per-tx warm sets paid every tx.
// Callers pass the key's hash (addrHash, slotHash).
type genSet[K comparable, V any] struct {
	gen, shift, mask uint64
	n                int
	e                []genEnt[K, V]
	idx              []uint32 // positions of the entries inserted in this generation, in order (all)
}

type genEnt[K comparable, V any] struct {
	k    K
	h    uint64
	gen  uint64
	dead bool
	v    V
}

func newGenSet[K comparable, V any](n int) genSet[K, V] {
	t := genSet[K, V]{gen: 1}
	t.alloc(n)
	return t
}

func (t *genSet[K, V]) alloc(n int) {
	t.e = make([]genEnt[K, V], n)
	t.mask = uint64(n - 1)
	t.shift = 64 - uint64(bits.TrailingZeros(uint(n)))
	t.n = 0
}

func (t *genSet[K, V]) find(k *K, h uint64) *genEnt[K, V] { return &t.e[t.findIdx(k, h)] }

func (t *genSet[K, V]) findIdx(k *K, h uint64) uint64 {
	for i := (h * slotHashMul) >> t.shift; ; i = (i + 1) & t.mask {
		if e := &t.e[i]; e.gen != t.gen || e.k == *k {
			return i
		}
	}
}

// all yields the live entries in insertion order: per-tx scans cost the entries, not the
// table's capacity (which never shrinks).
func (t *genSet[K, V]) all() iter.Seq[*genEnt[K, V]] {
	return func(yield func(*genEnt[K, V]) bool) {
		for _, i := range t.idx {
			if e := &t.e[i]; !e.dead && !yield(e) {
				return
			}
		}
	}
}

func (t *genSet[K, V]) has(k *K, h uint64) bool { return t.get(k, h) != nil }

// add inserts k and reports whether it was absent.
func (t *genSet[K, V]) add(k *K, h uint64) bool {
	_, added := t.slot(k, h)
	return added
}

// slot returns the entry of k, inserting it (zero value) if absent.
func (t *genSet[K, V]) slot(k *K, h uint64) (*genEnt[K, V], bool) {
	i := t.findIdx(k, h)
	e := &t.e[i]
	if e.gen == t.gen {
		if !e.dead {
			return e, false
		}
	} else {
		if 2*(t.n+1) > len(t.e) {
			t.grow()
			i = t.findIdx(k, h)
			e = &t.e[i]
		}
		e.k, e.h, e.gen = *k, h, t.gen
		t.n++
		t.idx = append(t.idx, uint32(i))
	}
	var zero V
	e.v, e.dead = zero, false
	return e, true
}

// get returns k's live entry, or nil.
func (t *genSet[K, V]) get(k *K, h uint64) *genEnt[K, V] {
	if e := t.find(k, h); e.gen == t.gen && !e.dead {
		return e
	}
	return nil
}

func (t *genSet[K, V]) remove(k *K, h uint64) {
	if e := t.find(k, h); e.gen == t.gen {
		e.dead = true
	}
}

func (t *genSet[K, V]) clear() {
	t.gen, t.n, t.idx = t.gen+1, 0, t.idx[:0]
	if t.gen == 0 { // wrapped: zeroed entries would match generation 0
		clear(t.e)
		t.gen = 1
	}
}

func (t *genSet[K, V]) grow() {
	old, idx := t.e, t.idx
	t.alloc(2 * len(old))
	t.idx = make([]uint32, 0, 2*cap(idx))
	for _, j := range idx { // in insertion order, so all keeps its order
		if o := &old[j]; !o.dead {
			i := t.findIdx(&o.k, o.h)
			t.e[i] = *o
			t.idx = append(t.idx, uint32(i))
			t.n++
		}
	}
}

func addrHash(a *Address) uint64 {
	return binary.LittleEndian.Uint64(a[0:]) ^ binary.LittleEndian.Uint64(a[8:]) ^ uint64(binary.LittleEndian.Uint32(a[16:]))<<29
}

func slotHash(a *Address, s *Hash) uint64 {
	return addrHash(a) ^ binary.LittleEndian.Uint64(s[0:]) ^ binary.LittleEndian.Uint64(s[8:]) ^
		binary.LittleEndian.Uint64(s[16:]) ^ binary.LittleEndian.Uint64(s[24:])
}
