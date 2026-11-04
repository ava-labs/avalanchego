package trie

import (
	"fmt"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/tier0"
)

// applyPar: a block of at least this many writes is applied on all threads (Settled source).
var applyPar = 256

// slotRun is one address's writes in one group, holding a slot write.
type slotRun struct {
	ws    []tier0.Write
	ps    []prepW
	at    int // the run's first write in the block
	n     int // its slot writes
	t     *Trie
	sm    *slotMaps
	wiped bool
}

// blockPar is block for a block with no account removal (Settled source): the account writes are
// applied in order on the caller's goroutine, the slot writes on 16 goroutines, one per part of
// the keccak of the state key (a key's writes stay in block order on one goroutine,
// and with no removal a slot write depends on nothing else). The first error by write position
// is returned (an account write's error counts at its run's first write). ok false: the block
// has a write that can remove an account (a code hash write of 0, after which the account's
// storage is gone for the block's later slot writes); nothing was applied, block applies it in
// order.
func (a *applier) blockPar(n uint64, groups [][]tier0.Write, ps []prepW) (nw int, err error, ok bool) {
	e := a.e
	type acctRun struct {
		ws []tier0.Write
		ps []prepW
		at int
	}
	var runs []slotRun
	var accts []acctRun
	for _, g := range groups {
		pg := ps[:len(g)]
		ps = ps[len(g):]
		for lo := 0; lo < len(g); {
			hi, slots, acct := lo, 0, false
			for ; hi < len(g) && g[hi].Addr == g[lo].Addr; hi++ {
				switch g[hi].Kind {
				case tier0.KindSlot:
					slots++
				case tier0.KindCode:
					if g[hi].New == ([32]byte{}) {
						return 0, nil, false
					}
					acct = true
				default:
					acct = true
				}
			}
			if acct {
				accts = append(accts, acctRun{g[lo:hi], pg[lo:hi], nw + lo})
			}
			if slots > 0 {
				runs = append(runs, slotRun{ws: g[lo:hi], ps: pg[lo:hi], at: nw + lo, n: slots})
			}
			lo = hi
		}
		nw += len(g)
	}
	errAt, errV := -1, error(nil)
	setErr := func(at int, err error) {
		if errAt < 0 || at < errAt {
			errAt, errV = at, err
		}
	}
	for _, r := range accts {
		if err := a.address(r.ws, r.ps, false); err != nil {
			setErr(r.at, err)
			break
		}
	}
	// the tries and the address maps, read on all threads; the few missing are made after
	wiped := len(e.fw.wiped) > 0
	const chunk = 256
	e.parallel((len(runs)+chunk-1)/chunk, func(w *worker, i int) {
		for j := i * chunk; j < min((i+1)*chunk, len(runs)); j++ {
			r := &runs[j]
			r.t = e.stor[r.ps[0].ah] // nil: not touched yet, or dropped
			r.sm = e.fw.slots[r.ws[0].Addr]
			r.wiped = wiped && e.fw.wiped[r.ws[0].Addr]
		}
	}, nil)
	for j := range runs {
		r := &runs[j]
		if r.t == nil {
			r.t = e.slotTrie(r.ps[0].ah)
		}
		if r.sm == nil {
			addr := r.ws[0].Addr
			if r.sm = e.fw.slots[addr]; r.sm == nil {
				r.sm = &slotMaps{}
				e.fw.slots[addr] = r.sm
			}
		}
	}
	var added [16]int
	var serrAt [16]int
	var serr [16]error
	var failed atomic.Bool
	e.parallel(16, func(w *worker, x int) {
		serrAt[x] = -1
		for j := range runs {
			if failed.Load() {
				return
			}
			r := &runs[j]
			for i := range r.ws {
				wr, p := &r.ws[i], &r.ps[i]
				if wr.Kind != tier0.KindSlot || part(&p.sh) != x {
					continue
				}
				m := r.sm[x]
				old, ok := m[p.sh]
				if !ok && !r.wiped {
					old = p.old
				}
				if old != wr.Old {
					serrAt[x] = r.at + i
					serr[x] = fmt.Errorf("%w: %x slot %x: write says %x, state has %x", errOld, wr.Addr, wr.Slot, wr.Old, old)
					failed.Store(true)
					return
				}
				if m == nil {
					m = map[[32]byte][32]byte{}
					r.sm[x] = m
				}
				if !ok {
					added[x]++
				}
				m[p.sh] = wr.New
				r.t.pend[x].ks = append(r.t.pend[x].ks, p.sh)
			}
		}
	}, nil)
	for x := range 16 {
		e.fw.nslots += added[x]
		if serrAt[x] >= 0 {
			setErr(serrAt[x], serr[x])
		}
	}
	for j := range runs {
		r := &runs[j]
		if !r.t.pendOn {
			r.t.addr = r.ws[0].Addr
		}
		r.t.npend += r.n
		e.listTrie(r.t, r.ps[0].ah)
	}
	if errV != nil {
		return nw, fmt.Errorf("trie: block %d: %w", n, errV), true
	}
	return nw, nil, true
}
