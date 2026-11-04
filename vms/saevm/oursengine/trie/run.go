package trie

import (
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/trie/keccaksimd"
)

// Lanes switches leaf hashing between batched lanes keccak (true) and scalar keccak.
var Lanes = true

// kv is one leaf of a run: its key and its leaf value item.
type kv struct {
	key *[32]byte
	val []byte
}

type job struct {
	kids  [16]int32
	key   *[32]byte // a key below this job (for path nibbles)
	depth uint8     // leaf: slot depth (suffix start); branch: branch depth c
	ext   uint8     // branch: slot depth (extension = key[ext:c])
	leaf  bool
	msg   int32 // leaf: index into msgs, -1 = inline
	pos   Pos   // leaf ref, or branch ref without its extension
}

type item struct {
	depth  int
	jobs   [2]int32 // [first, end) jobs
	leaves int
	out    *Pos
}

// Hasher hashes runs; one per worker. Leaves of every run added between Begin and Finish
// share one lanes keccak batch.
type Hasher struct {
	k     *Keccaker
	jobs  []job
	items []item
	arena []byte
	spans [][2]int32
	msgs  [][]byte
	sums  [][32]byte
	idx   []int
	buf   []byte
	kids  [16][]byte
	// Finish's per-job scratch: the job's height (leaf 0, branch 1 + its highest kid) and its
	// reference as its parent holds it (with its extension); the nodes of one height are
	// encoded into narena and hashed in one lanes batch
	lvl    []int32
	eff    []Pos
	byLvl  [][]int32
	narena []byte
	nspans [][2]int32
	nmsgs  [][]byte
	nsums  [][32]byte
	nown   []int32
	Leaves uint64 // leaf hashes (lanes or scalar)
	Nodes  uint64 // branch and extension hashes
}

func NewHasher() *Hasher { return &Hasher{k: NewKeccaker(), buf: make([]byte, 0, 1024)} }

func (h *Hasher) Begin() {
	h.jobs, h.items, h.arena, h.spans = h.jobs[:0], h.items[:0], h.arena[:0], h.spans[:0]
}

func lcpNibs(a, b *[32]byte, from int) int {
	i := from
	for i < 64 && nib(a, i) == nib(b, i) {
		i++
	}
	return i
}

// shape appends the jobs of kvs (all sharing nibbles [0, depth)) in post-order and returns the
// top job index.
func (h *Hasher) shape(kvs []kv, depth int) int32 {
	if len(kvs) == 1 {
		enc := leafEnc(h.buf, kvs[0].key, depth, kvs[0].val)
		j := job{key: kvs[0].key, depth: uint8(depth), leaf: true, msg: -1}
		if len(enc) < 32 {
			j.pos = posOf(enc, kOther)
		} else {
			s := len(h.arena)
			h.arena = append(h.arena, enc...)
			j.msg = int32(len(h.spans))
			h.spans = append(h.spans, [2]int32{int32(s), int32(len(h.arena))})
		}
		h.jobs = append(h.jobs, j)
		return int32(len(h.jobs) - 1)
	}
	c := lcpNibs(kvs[0].key, kvs[len(kvs)-1].key, depth)
	var kids [16]int32
	for i := range kids {
		kids[i] = -1
	}
	for lo := 0; lo < len(kvs); {
		n := nib(kvs[lo].key, c)
		hi := lo + 1
		for hi < len(kvs) && nib(kvs[hi].key, c) == n {
			hi++
		}
		kids[n] = h.shape(kvs[lo:hi], c+1)
		lo = hi
	}
	h.jobs = append(h.jobs, job{kids: kids, key: kvs[0].key, depth: uint8(c), ext: uint8(depth)})
	return int32(len(h.jobs) - 1)
}

// Add queues a run (sorted kvs sharing nibbles [0, depth)); out is set by Finish. kvs and
// their values must stay valid until Finish.
func (h *Hasher) Add(kvs []kv, depth int, out *Pos) {
	it := item{depth: depth, out: out, leaves: len(kvs)}
	it.jobs[0] = int32(len(h.jobs))
	if len(kvs) > 0 {
		h.shape(kvs, depth)
	}
	it.jobs[1] = int32(len(h.jobs))
	h.items = append(h.items, it)
}

// Finish hashes every queued run and writes their positions.
func (h *Hasher) Finish() {
	n := len(h.spans)
	if n > 0 {
		h.msgs = h.msgs[:0]
		for _, s := range h.spans {
			h.msgs = append(h.msgs, h.arena[s[0]:s[1]])
		}
		if cap(h.sums) < n {
			h.sums = make([][32]byte, n)
		}
		h.sums = h.sums[:n]
		if Lanes {
			h.idx = keccaksimd.HashBatch(h.sums, h.msgs, h.idx)
		} else {
			for i, m := range h.msgs {
				h.sums[i] = h.k.Sum(m)
			}
		}
		h.Leaves += uint64(n)
	}
	// leaves, then every branch height by height (a branch's kids are all lower): the nodes of
	// one height across every queued run are hashed in one lanes batch, then the extensions
	// over them in another
	nj := len(h.jobs)
	h.lvl = append(h.lvl[:0], make([]int32, nj)...)
	h.eff = append(h.eff[:0], make([]Pos, nj)...)
	top := int32(0)
	for ji := range h.jobs {
		j := &h.jobs[ji]
		if j.leaf {
			if j.msg >= 0 {
				j.pos = Pos{Ref: h.sums[j.msg], N: 32, Kind: kOther}
			}
			h.eff[ji] = j.pos
			continue
		}
		l := int32(0)
		for _, k := range j.kids {
			if k >= 0 {
				l = max(l, h.lvl[k])
			}
		}
		h.lvl[ji] = l + 1
		top = max(top, l+1)
	}
	for len(h.byLvl) <= int(top) {
		h.byLvl = append(h.byLvl, nil)
	}
	for l := range h.byLvl {
		h.byLvl[l] = h.byLvl[l][:0]
	}
	for ji := range h.jobs {
		if !h.jobs[ji].leaf {
			h.byLvl[h.lvl[ji]] = append(h.byLvl[h.lvl[ji]], int32(ji))
		}
	}
	for l := int32(1); l <= top; l++ {
		js := h.byLvl[l]
		h.nodeBatch(js, func(ji int32) ([]byte, bool) {
			j := &h.jobs[ji]
			for i, k := range j.kids {
				h.kids[i] = nil
				if k >= 0 {
					h.kids[i] = h.eff[k].ref()
				}
			}
			h.Nodes++
			return branchEnc(h.buf, &h.kids), true
		}, func(ji int32, p Pos) {
			p.Kind = kBranch
			h.jobs[ji].pos = p
			h.eff[ji] = p
		})
		h.nodeBatch(js, func(ji int32) ([]byte, bool) {
			j := &h.jobs[ji]
			if j.depth == j.ext {
				return nil, false
			}
			h.Nodes++
			return extEnc(h.buf, j.key, int(j.ext), int(j.depth), j.pos.ref()), true
		}, func(ji int32, p Pos) {
			p.Kind = kOther
			h.eff[ji] = p
		})
	}
	for _, it := range h.items {
		if it.jobs[0] == it.jobs[1] {
			*it.out = Pos{}
			continue
		}
		t := it.jobs[1] - 1
		tmp := h.eff[t]
		if j := &h.jobs[t]; !j.leaf && int(j.depth) == it.depth {
			tmp.Kind = kBranch
		} else {
			tmp.Kind = kOther
		}
		*it.out = tmp
	}
}

// nodeBatch encodes the node of each job in js (enc false: the job has none) and sets its
// reference: inline below 32 bytes, else the keccak of the encoding, all hashed in one lanes
// batch (scalar keccak without Lanes).
func (h *Hasher) nodeBatch(js []int32, enc func(ji int32) ([]byte, bool), set func(ji int32, p Pos)) {
	h.narena, h.nspans, h.nown = h.narena[:0], h.nspans[:0], h.nown[:0]
	for _, ji := range js {
		e, ok := enc(ji)
		if !ok {
			continue
		}
		if len(e) < 32 {
			set(ji, posOf(e, 0))
			continue
		}
		s := len(h.narena)
		h.narena = append(h.narena, e...)
		h.nspans = append(h.nspans, [2]int32{int32(s), int32(len(h.narena))})
		h.nown = append(h.nown, ji)
	}
	n := len(h.nspans)
	if n == 0 {
		return
	}
	h.nmsgs = h.nmsgs[:0]
	for _, sp := range h.nspans {
		h.nmsgs = append(h.nmsgs, h.narena[sp[0]:sp[1]])
	}
	if cap(h.nsums) < n {
		h.nsums = make([][32]byte, n)
	}
	h.nsums = h.nsums[:n]
	if Lanes {
		h.idx = keccaksimd.HashBatch(h.nsums, h.nmsgs, h.idx)
	} else {
		for i, m := range h.nmsgs {
			h.nsums[i] = h.k.Sum(m)
		}
	}
	for i, ji := range h.nown {
		set(ji, Pos{Ref: h.nsums[i], N: 32})
	}
}
