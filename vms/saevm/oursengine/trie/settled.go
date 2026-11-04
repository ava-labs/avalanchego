package trie

import (
	"bytes"
	"cmp"
	"errors"
	"slices"
	"sort"
)

// Settled is the overlay a tier0 starts from, as the node's store keeps it (store.State's settled
// overlay: every write of the tier0s the trie applied, without the base). An engine reading from
// one (Top.ReadFrom) keeps no written values of its own: its overlay lists hold the written keys
// only (state keys and addresses, sorted by their keccak), and a value is read from the current
// tier0's writes (kept until the tier0 is applied), else from the Settled overlay, else from the
// base the engine reads itself. A storage list holds the keccak of each state key (the trie
// key itself: no hashing to keep it sorted or to merge it at Root), an account list the
// address (SettledAccount reads by address).
type Settled interface {
	// SettledSlotAt: the state key whose keccak256 is sh; found false when the overlay does not
	// hold the key; wiped: the account was deleted since the base (its base storage is gone).
	SettledSlotAt(addr [20]byte, sh [32]byte) (v [32]byte, found, wiped bool)
	// SettledSlotsAt is SettledSlotAt for the keys shs of addr at once (one lock and one account
	// lookup): vs[i] and found[i] for shs[i], wiped as SettledSlotAt's
	SettledSlotsAt(addr [20]byte, shs [][32]byte, vs [][32]byte, found []bool) (wiped bool)
	// SettledAccount: found false when the overlay does not hold the account; codeHash 0: the
	// account does not exist, keccak256(""): no code.
	SettledAccount(addr [20]byte) (nonce uint64, balance, codeHash [32]byte, multicoin, found bool)
}

// SettledLanes is a Settled whose reads can each take the read lock of a reader lane (the
// engine passes its worker's index): the workers' read locks then do not share one cache line.
type SettledLanes interface {
	SettledSlotAtLane(lane int, addr [20]byte, sh [32]byte) (v [32]byte, found, wiped bool)
	SettledSlotsAtLane(lane int, addr [20]byte, shs [][32]byte, vs [][32]byte, found []bool) (wiped bool)
	SettledAccountLane(lane int, addr [20]byte) (nonce uint64, balance, codeHash [32]byte, multicoin, found bool)
}

// fileWrites are the writes of the tier0 being applied, up to the current write.
type fileWrites struct {
	slots  map[[20]byte]*slotMaps // address -> keccak of the state key -> value
	accts  map[[20]byte]ovAcct
	wiped  map[[20]byte]bool // deleted in this tier0: the settled and base storage is gone
	nslots int               // the keys in slots (Stats.DirtyStorageKeys without a walk of slots per Root)
}

// part is the part (0-15) of slot key sh in slotMaps and Trie.pend: its last nibble, spread
// evenly also over keys adjacent in the trie (a run of one contract's slots).
func part(sh *[32]byte) int { return int(sh[31] & 15) }

// slotMaps are one address's slot writes of the tier0 being applied (keccak of the state key ->
// value), one map per part of the key: the parallel apply writes each on its own goroutine.
type slotMaps [16]map[[32]byte][32]byte

func (s *slotMaps) get(sh *[32]byte) ([32]byte, bool) {
	v, ok := s[part(sh)][*sh] // a nil map reads as empty
	return v, ok
}

// put sets sh to v and reports whether sh is new.
func (s *slotMaps) put(sh *[32]byte, v [32]byte) bool {
	x := part(sh)
	if s[x] == nil {
		s[x] = map[[32]byte][32]byte{}
	}
	_, ok := s[x][*sh]
	s[x][*sh] = v
	return !ok
}

func (s *slotMaps) len() int {
	n := 0
	for _, m := range s {
		n += len(m)
	}
	return n
}

func newFileWrites() fileWrites {
	return fileWrites{slots: map[[20]byte]*slotMaps{}, accts: map[[20]byte]ovAcct{}, wiped: map[[20]byte]bool{}}
}

// ReadFrom makes the engine read written values from s instead of keeping them. Call it before
// the first ApplyTier0; s must then be settled with each tier0 right after ApplyTier0 returns it.
func (t *Top) ReadFrom(s Settled) error {
	e := t.E
	if len(e.ov) > 0 || len(e.acct.ov) > 0 || len(e.dirtyStor) > 0 || len(e.dirtyAcct) > 0 {
		return errors.New("trie: ReadFrom on a top with an overlay")
	}
	for _, tr := range e.stor {
		if tr != nil && len(tr.ov) > 0 {
			return errors.New("trie: ReadFrom on a top with an overlay")
		}
	}
	e.src = s
	e.lanes, _ = s.(SettledLanes)
	return nil
}

func (e *Engine) keccaker() *Keccaker {
	if e.kc == nil {
		e.kc = NewKeccaker()
	}
	return e.kc
}

// keyHash is the trie key of an overlay list entry: the entry itself, except an account entry
// with a Settled source: keccak of the address in its first 20 bytes.
func (e *Engine) keyHash(k *[32]byte, acct bool, kc *Keccaker) [32]byte {
	if e.src != nil && acct {
		return kc.Sum(k[:20])
	}
	return *k
}

// insertKey inserts entry k (trie key h) into l, kept sorted by trie key.
func (e *Engine) insertKey(l [][32]byte, k, h [32]byte, acct bool, kc *Keccaker) ([][32]byte, bool) {
	i, found := sort.Find(len(l), func(i int) int {
		x := e.keyHash(&l[i], acct, kc)
		return bytes.Compare(h[:], x[:])
	})
	if found {
		return l, false
	}
	return slices.Insert(l, i, k), true
}

// slotsNow is the current value of the state keys of addr whose keccaks are shs, from the
// overlay (Settled source only), into w.ovv; w.ovok[i] false: the overlay does not hold shs[i],
// its value is the base's. Newest first per key: the tier0's writes (with LiveOverlay: the live
// overlay), the frozen overlay, the level 1 files; then one Settled call for the keys none of
// them holds, not one (and a lock) per key.
func (e *Engine) slotsNow(w *worker, addr *[20]byte, shs [][32]byte) {
	n := len(shs)
	if cap(w.ovv) < n {
		w.ovv, w.ovok = make([][32]byte, n, 2*n), make([]bool, n, 2*n)
	}
	w.ovv, w.ovok = w.ovv[:n], w.ovok[:n]
	clear(w.ovok)
	left := w.fromLayer(&e.fw, addr, shs, n)
	if f := e.frozen; f != nil && left > 0 {
		left = w.fromLayer(f, addr, shs, left)
	}
	for _, l := range e.l1 {
		if left == 0 {
			break
		}
		left = w.fromL1(l, addr, shs, left)
	}
	switch {
	case left == 0:
	case left == n: // no layer above holds any: the Settled view for all of them at once
		if e.settledSlots(w, addr, shs, w.ovv, w.ovok) {
			for i := range w.ovok {
				if !w.ovok[i] {
					w.ovv[i], w.ovok[i] = [32]byte{}, true // wiped since the base: a key the overlay does not hold is 0
				}
			}
		}
	default:
		w.mix, w.mks = w.mix[:0], w.mks[:0]
		for i := range shs {
			if !w.ovok[i] {
				w.mix = append(w.mix, i)
				w.mks = append(w.mks, shs[i])
			}
		}
		m := len(w.mix)
		if cap(w.mvv) < m {
			w.mvv, w.mok = make([][32]byte, m, 2*m), make([]bool, m, 2*m)
		}
		w.mvv, w.mok = w.mvv[:m], w.mok[:m]
		wiped := e.settledSlots(w, addr, w.mks, w.mvv, w.mok)
		for j, i := range w.mix {
			if w.mok[j] {
				w.ovv[i], w.ovok[i] = w.mvv[j], true
			} else if wiped {
				w.ovv[i], w.ovok[i] = [32]byte{}, true
			}
		}
	}
}

// settledSlots is SettledSlotsAt of shs into vs and found, on w's lane.
func (e *Engine) settledSlots(w *worker, addr *[20]byte, shs [][32]byte, vs [][32]byte, found []bool) (wiped bool) {
	if e.lanes != nil {
		return e.lanes.SettledSlotsAtLane(w.lane, *addr, shs, vs, found)
	}
	return e.src.SettledSlotsAt(*addr, shs, vs, found)
}

// settledSlot is the overlay value before the tier0 being applied: the Settled overlay only.
func (e *Engine) settledSlot(w *worker, addr *[20]byte, sh *[32]byte) (v [32]byte, ok bool) {
	var found, wiped bool
	if e.lanes != nil {
		v, found, wiped = e.lanes.SettledSlotAtLane(w.lane, *addr, *sh)
	} else {
		v, found, wiped = e.src.SettledSlotAt(*addr, *sh)
	}
	return v, found || wiped
}

// acctNow is addr's current account from the overlay (Settled source only): exists false when
// it does not exist; ok false: the overlay does not hold it, it is the base's.
func (e *Engine) acctNow(w *worker, addr *[20]byte) (a ovAcct, exists, ok bool) {
	if a, exists, ok = e.acctAbove(addr); ok {
		return a, exists, true
	}
	return e.settledAcct(w, addr)
}

// settledAcct is acctNow before the tier0 being applied: the Settled overlay only.
func (e *Engine) settledAcct(w *worker, addr *[20]byte) (a ovAcct, exists, ok bool) {
	var nonce uint64
	var bal, code [32]byte
	var multi, found bool
	if e.lanes != nil {
		nonce, bal, code, multi, found = e.lanes.SettledAccountLane(w.lane, *addr)
	} else {
		nonce, bal, code, multi, found = e.src.SettledAccount(*addr)
	}
	if !found || code == ([32]byte{}) {
		return ovAcct{}, false, found
	}
	a = ovAcct{nonce: nonce, balance: bal, multicoin: multi}
	if code != EmptyCode {
		a.hasCode, a.code = true, code
	}
	return a, true, true
}

// listAddr puts addr (keccak ah) on the account trie's overlay list (at the next Root:
// flushPending).
func (e *Engine) listAddr(addr *[20]byte, ah *[32]byte) {
	if _, ok := e.fw.accts[*addr]; ok {
		return // listed at its first write in this tier0
	}
	e.pendAddrs = append(e.pendAddrs, pendAddr{*addr, *ah})
}

// putAccount writes account a of addr (Settled source only).
func (e *Engine) putAccount(addr *[20]byte, ah [32]byte, a ovAcct) {
	e.listAddr(addr, &ah)
	e.fw.accts[*addr] = a
	e.markAcctDirty(&ah)
}

// removeAccount deletes addr and its storage (Settled source only).
func (e *Engine) removeAccount(addr *[20]byte, ah [32]byte) {
	e.listAddr(addr, &ah)
	e.fw.accts[*addr] = ovAcct{gone: true}
	e.fw.wiped[*addr] = true
	if m := e.fw.slots[*addr]; m != nil {
		e.fw.nslots -= m.len()
	}
	delete(e.fw.slots, *addr)
	e.dropTrie(ah)
}

// putSlot writes the state key of addr whose keccak is sh (Settled source only).
func (e *Engine) putSlot(addr *[20]byte, ah, sh [32]byte, v [32]byte) {
	t := e.slotTrie(ah)
	t.addr = *addr
	m := e.fw.slots[*addr]
	if m == nil {
		m = &slotMaps{}
		e.fw.slots[*addr] = m
	}
	if m.put(&sh, v) {
		e.fw.nslots++
	}
	e.listTrie(t, ah)
	x := part(&sh)
	t.pend[x].ks = append(t.pend[x].ks, sh) // listed and marked dirty at the next Root (flushPending)
	t.npend++
}

// listTrie puts t (of account hash ah) on the tries flushPending lists and marks.
func (e *Engine) listTrie(t *Trie, ah [32]byte) {
	if !t.pendOn {
		t.pendOn = true
		e.pendTries = append(e.pendTries, pendTrie{t, ah})
	}
}

// pendTrie is a storage trie with slot writes not yet listed and marked (Settled source).
type pendTrie struct {
	t  *Trie
	ah [32]byte
}

// pendAddr is an address written in this tier0 not yet on the account list.
type pendAddr struct {
	addr [20]byte
	ah   [32]byte
}

// flushSplit: a trie (with cuts) with at least this many keys pending is listed and marked as 16
// items, one per last nibble of the cut (the key's nibble depth-1: the items' cuts are disjoint,
// also for keys adjacent in the trie), so a hot trie does not sort and merge its keys on one
// goroutine.
var flushSplit = 256

// flushPending puts the slot keys and addresses written since the last Root on their overlay
// lists and marks the slots dirty, on all threads (a trie, a sixteenth of a big trie's cuts, or
// an account cut per item): the sequential apply only checks and records the values. A trie
// dropped since its writes is skipped (its keys went with it, as in a drop after an immediate
// insert).
func (e *Engine) flushPending() {
	if len(e.pendTries) == 0 && len(e.pendAddrs) == 0 {
		return
	}
	pts := e.pendTries
	type cutAdd struct {
		c uint32
		m subMask
		l [][32]byte
	}
	type fitem struct {
		p     int        // index in pts
		ks    [][32]byte // its pending keys (not split)
		split bool       // a part of a split trie: its keys are the trie's whose nibble depth-1 is x; the lists and marks go to out (written after)
		made  bool       // it made the trie's dirty map (not split)
		x     int
		out   []cutAdd
	}
	var items []fitem
	dms := make([]map[uint32]subMask, len(pts))
	var splits []int // pts indices of the split tries
	for i := range pts {
		p := &pts[i]
		t := p.t
		if e.stor[p.ah] != t {
			continue
		}
		if t.ov == nil {
			t.ov = map[uint32][][32]byte{}
		}
		n := t.pendN()
		dms[i] = e.dirtyStor[p.ah] // nil: made by the trie's item (a split trie's here), put in after
		e.markAcctDirty(&p.ah)
		if n < flushSplit || t.depth == 0 {
			items = append(items, fitem{p: i})
			continue
		}
		if dms[i] == nil {
			dms[i] = make(map[uint32]subMask, min(n, t.cuts()))
			e.dirtyStor[p.ah] = dms[i]
		}
		for x := range 16 {
			items = append(items, fitem{p: i, split: true, x: x})
		}
		splits = append(splits, i)
	}
	e.parallel(len(items), func(w *worker, i int) {
		it := &items[i]
		t, ks := pts[it.p].t, it.ks
		if !it.split {
			n := t.pendN()
			for x := range t.pend {
				if l := t.pend[x].ks; len(l) == n {
					ks = l // one part holds them all
					break
				} else if len(l) > 0 {
					if ks == nil {
						ks = make([][32]byte, 0, n)
					}
					ks = append(ks, l...)
				}
			}
			if dms[it.p] == nil {
				dms[it.p] = make(map[uint32]subMask, min(n, t.cuts()))
				it.made = true
			}
		} else {
			ks = w.fks[:0]
			for x := range t.pend {
				for _, k := range t.pend[x].ks {
					if int(nib(&k, int(t.depth)-1)) == it.x {
						ks = append(ks, k)
					}
				}
			}
			w.fks = ks
		}
		slices.SortFunc(ks, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
		ks = slices.Compact(ks)
		dm, d := dms[it.p], int(t.depth)
		for lo := 0; lo < len(ks); {
			c := t.cutOf(&ks[lo])
			hi := lo + 1
			for hi < len(ks) && t.cutOf(&ks[hi]) == c {
				hi++
			}
			var m subMask
			for j := lo; j < hi; j++ {
				m.set(subOf(&ks[j], d))
			}
			l := mergeKeys(t.ov[c], ks[lo:hi])
			if it.split { // t.ov and dm are shared with the trie's other parts: written after
				it.out = append(it.out, cutAdd{c, m, l})
			} else {
				t.ov[c] = l
				x := dm[c]
				for j := range x {
					x[j] |= m[j]
				}
				dm[c] = x
			}
			lo = hi
		}
	}, nil)
	for i := range items {
		if it := &items[i]; it.made {
			e.dirtyStor[pts[it.p].ah] = dms[it.p]
		}
	}
	if len(splits) > 0 {
		first := make(map[int]int, len(splits)) // pts index -> its first item
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].split {
				first[items[i].p] = i
			}
		}
		e.parallel(len(splits), func(w *worker, i int) {
			pi := splits[i]
			t, dm := pts[pi].t, dms[pi]
			for j := first[pi]; j < len(items) && items[j].p == pi; j++ {
				for _, a := range items[j].out {
					t.ov[a.c] = a.l
					x := dm[a.c]
					for k := range x {
						x[k] |= a.m[k]
					}
					dm[a.c] = x
				}
			}
		}, nil)
	}
	for i := range pts { // the parts' arrays are reused: nothing kept points into them
		t := pts[i].t
		for x := range t.pend {
			t.pend[x].ks = t.pend[x].ks[:0]
		}
		t.pendOn, t.npend = false, 0
	}
	clear(pts)
	e.pendTries = pts[:0]

	pas := e.pendAddrs
	if len(pas) > 0 {
		at := e.acct
		slices.SortFunc(pas, func(a, b pendAddr) int { return cmp.Compare(at.cutOf(&a.ah), at.cutOf(&b.ah)) })
		var groups [][2]int
		for lo := 0; lo < len(pas); {
			c := at.cutOf(&pas[lo].ah)
			hi := lo + 1
			for hi < len(pas) && at.cutOf(&pas[hi].ah) == c {
				hi++
			}
			groups = append(groups, [2]int{lo, hi})
			lo = hi
		}
		if at.ov == nil {
			at.ov = map[uint32][][32]byte{}
		}
		lists := make([][][32]byte, len(groups))
		for i, g := range groups {
			lists[i] = at.ov[at.cutOf(&pas[g[0]].ah)]
		}
		e.parallel(len(groups), func(w *worker, i int) {
			l := lists[i]
			for _, p := range pas[groups[i][0]:groups[i][1]] {
				var k [32]byte
				copy(k[:], p.addr[:])
				l, _ = e.insertKey(l, k, p.ah, true, w.h.k)
			}
			lists[i] = l
		}, nil)
		for i, g := range groups {
			at.ov[at.cutOf(&pas[g[0]].ah)] = lists[i]
		}
		e.pendAddrs = pas[:0]
	}
}

// mergeKeys merges sorted unique keys ks into sorted list l (keys already there are skipped),
// in place from the back.
func mergeKeys(l, ks [][32]byte) [][32]byte {
	var add [][32]byte
	for _, k := range ks {
		if _, found := slices.BinarySearchFunc(l, k, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) }); !found {
			add = append(add, k)
		}
	}
	if len(add) == 0 {
		return l
	}
	n := len(l)
	l = slices.Grow(l, len(add))[:n+len(add)]
	i, j := n-1, len(add)-1
	for o := len(l) - 1; j >= 0; o-- {
		if i >= 0 && bytes.Compare(l[i][:], add[j][:]) > 0 {
			l[o] = l[i]
			i--
		} else {
			l[o] = add[j]
			j--
		}
	}
	return l
}
