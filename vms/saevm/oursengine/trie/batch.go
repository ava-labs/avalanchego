package trie

import (
	"sync/atomic"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/trie/keccaksimd"
)

// upChunk and topChunk: stored nodes and cuts per parallel item of uppers and cutTops, so one
// worker hashes about a lanes batch of nodes per level at once. itemChunk: cuts per unit of
// Root's item build for a heap trie with many dirty cuts.
const (
	upChunk   = 8
	topChunk  = 8
	itemChunk = 256
)

// cnode is one node for combineMany: out at depth l over kids; ok is combine's result.
type cnode struct {
	kids [16]*Pos
	out  *Pos
	l    int
	ok   bool
	x    int32 // cutTops: the node's index in its memo level
	ci   int32 // cutTops: its cut in the chunk
}

// nodeBatch is combineMany's scratch.
type nodeBatch struct {
	arena []byte
	spans [][2]int32
	own   []int32
	kind  []uint8
	msgs  [][]byte
	sums  [][32]byte
	idx   []int
}

// combineMany is combine for every node of ns (independent nodes: none is another's child): the
// encodings of 32 bytes and more are hashed in one lanes batch (scalar keccak without Lanes).
func (e *Engine) combineMany(w *worker, ns []cnode) {
	b := &w.cb
	b.arena, b.spans, b.own, b.kind = b.arena[:0], b.spans[:0], b.own[:0], b.kind[:0]
	for i := range ns {
		n := &ns[i]
		enc, kind, ok := nodeEnc(w, &n.kids, n.l)
		n.ok = ok
		switch {
		case !ok:
		case enc == nil:
			*n.out = Pos{}
		case len(enc) < 32:
			*n.out = posOf(enc, kind)
		default:
			s := len(b.arena)
			b.arena = append(b.arena, enc...)
			b.spans = append(b.spans, [2]int32{int32(s), int32(len(b.arena))})
			b.own = append(b.own, int32(i))
			b.kind = append(b.kind, kind)
		}
	}
	n := len(b.spans)
	if n == 0 {
		return
	}
	b.msgs = b.msgs[:0]
	for _, sp := range b.spans {
		b.msgs = append(b.msgs, b.arena[sp[0]:sp[1]])
	}
	if cap(b.sums) < n {
		b.sums = make([][32]byte, n, 2*n)
	}
	b.sums = b.sums[:n]
	if Lanes && n > 1 {
		b.idx = keccaksimd.HashBatch(b.sums, b.msgs, b.idx)
	} else {
		for i, m := range b.msgs {
			b.sums[i] = w.h.k.Sum(m)
		}
	}
	for i, j := range b.own {
		*ns[j].out = Pos{Ref: b.sums[i], N: 32, Kind: b.kind[i]}
	}
}

// ctop is one dirty cut for cutTops.
type ctop struct {
	t     *Trie
	c     uint32
	dirty *subMask
	memo  *cutMemo
}

// cutTops is cutTop for the cuts cs at once, level by level: each memo level's dirty nodes of
// all of them go through one combineMany.
func (e *Engine) cutTops(w *worker, storage bool, cs []ctop) {
	for j := 1; j >= 0; j-- { // level d+j of each cut whose memo keeps more than j levels
		ns := w.cn[:0]
		for ci := range cs {
			c := &cs[ci]
			if c.memo == nil || int(c.memo.m) <= j {
				continue
			}
			d := int(c.t.depth)
			span := 4 * (2 - j)
			ko := memoOff(j + 1)
			for x := 0; x < 1<<(4*j); x++ {
				if !c.dirty.any(x<<span, (x+1)<<span) {
					continue
				}
				var n cnode
				for i := range n.kids {
					n.kids[i] = &c.memo.pos[ko+16*x+i]
				}
				n.out = c.t.cut(c.c)
				if j > 0 {
					n.out = &c.memo.pos[memoOff(j)+x]
				}
				n.l, n.x, n.ci = d+j, int32(x), int32(ci)
				ns = append(ns, n)
			}
		}
		e.combineMany(w, ns)
		for k := range ns {
			if ns[k].ok {
				continue
			}
			// one leaf or extension below: recompute the node's range of sub-cuts
			c := &cs[ns[k].ci]
			m, x := int(c.memo.m), int(ns[k].x)
			s0, s1 := x<<(4*(m-j)), (x+1)<<(4*(m-j))
			lo := c.t.bound(int(c.c))
			e.flush(w)
			e.begin(w)
			ovk := e.subList(w, c.t.ov[c.c], !storage, int(c.t.depth), m, s0, s1)
			e.gather(w, c.t, storage, ovk, lo+uint64(c.memo.bnd[s0]), lo+uint64(c.memo.bnd[s1]), nil)
			w.h.Add(w.kvs, ns[k].l, ns[k].out)
			w.h.Finish()
			atomic.AddUint64(&e.S.SlowNodes, 1)
		}
		w.cn = ns
	}
}
