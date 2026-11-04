package store

import (
	"bytes"
	"slices"
)

// FoldWalk (persist harness, an hourly level 1 fold): for every account of the overlay in
// keccak(addr) order, f gets each slot key whose newest settled version is of a cut >= from
// (keccak ascending) with its value without leading zero bytes. The account list is taken under
// one short read lock; the walk itself takes no lock (an account's runs are replaced, never
// changed in place). It returns the accounts and the keys it gave f.
func (s *State) FoldWalk(from int, acct func(ah [32]byte, keys int), f func(sh [32]byte, v []byte)) (accts, keys int) {
	type e struct {
		ah   [32]byte
		rs   *runs
		vers []acctVer
	}
	s.mu.RLock()
	list := make([]e, 0, len(s.accts))
	for _, ov := range s.accts {
		if ov.runs != nil {
			list = append(list, e{ov.ah, ov.runs, ov.vers})
		}
	}
	view := s.set
	s.mu.RUnlock()
	slices.SortFunc(list, func(a, b e) int { return bytes.Compare(a.ah[:], b.ah[:]) })
	type kv struct {
		sh [32]byte
		v  []byte
	}
	var out []kv
	for _, a := range list {
		del, _ := delAt(a.vers, view)
		out = out[:0]
		r, m := &a.rs.recent, &a.rs.main
		i, j := 0, 0
		for i < r.n() || j < m.n() {
			var k [32]byte
			switch {
			case i == r.n():
				k = m.key(j)
			case j == m.n() || bytes.Compare(r.sh(i), m.sh(j)) < 0:
				k = r.key(i)
			default:
				k = m.key(j)
			}
			ri, rc := r.newest(&k, view)
			mi, mc := m.newest(&k, view)
			src, idx, c := r, ri, rc
			if mc > rc {
				src, idx, c = m, mi, mc
			}
			if c >= from && c >= del {
				out = append(out, kv{k, src.raw(idx)})
			}
			for ; i < r.n() && bytes.Equal(r.sh(i), k[:]); i++ {
			}
			for ; j < m.n() && bytes.Equal(m.sh(j), k[:]); j++ {
			}
		}
		if len(out) == 0 {
			continue
		}
		acct(a.ah, len(out))
		for _, x := range out {
			f(x.sh, x.v)
		}
		accts++
		keys += len(out)
	}
	return accts, keys
}
