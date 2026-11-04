package trie

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/era"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/tier0"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/trie/keccaksimd"
)

// Top is the stored trie top over a snapshot base plus the overlay of every tier0 applied
// since. It is not safe for concurrent use: the node runs it on one side thread and hands it
// each tier0 at its cut.
type Top struct {
	E     *Engine
	Block uint64   // the state's block: the base block, then the last applied tier0's last block
	Root  [32]byte // the state root at Block
	// EveryBlock: ApplyTier0 computes the root after every block and checks every settled
	// height inside the file, not only the highest (the live node's per-block check)
	EveryBlock bool
	// Live: the roots ApplyBlock keeps for the headers that settle them (nil: ApplyBlock is
	// refused); a fold's new top takes the old top's Live
	Live  *Live
	open  *openTier0 // the tier0 ApplyBlock is applying (nil: none)
	err   error      // sticky: a failed apply leaves the state part-applied
	bprep []prepW    // prepareBlock's, reused block after block
	// LiveOverlay (overlay build 2026-10-30): the writes stay in the engine's live overlay past
	// the tier0's end until Swap (overlay.go); the store is not given the tier0s. mu: ApplyBlock
	// and Swap hold it, UpperValue reads under it.
	LiveOverlay bool
	mu          sync.RWMutex
	// The lock-free slot read (UpperSlot): lay is the layers as of the last change, ver a
	// sequence count, odd while Swap, a drain's install or a tier0's end changes them; a read
	// that saw it change reads again. The live overlay's maps are the executor's own: ApplyBlock
	// writes them in place, so UpperSlot must not run during an ApplyBlock (the executor
	// sequences its reads and its blocks).
	lay atomic.Pointer[readLayers]
	ver atomic.Uint64
	// prepWorkers: the workers the last prepareBlock ran on
	prepWorkers int
}

// BuildTop builds the trie top of a base (the state after b.Block()) with cut size k. It reads
// (and so checks) the whole state region of the base. It is a verification and bootstrap tool:
// a fold writes the node's applied top (Top.WriteBased), never a built one. Its memory is not
// bounded yet and must be before a 16 GB node runs it: over a 57 GB base it peaked at RssAnon
// 6.2 GB plus 33 GB of the base mapped (fold f2m, 2026-10-22).
func BuildTop(b *base.Base, k, threads int) (*Top, error) {
	if k <= 0 {
		k = DefaultK
	}
	e := &Engine{K: k, Threads: threads, FiveFields: true}
	root, err := e.Build(b)
	if err != nil {
		return nil, err
	}
	return &Top{E: e, Block: b.Block(), Root: root}, nil
}

// Check is one comparison of a computed root with a header's state root. Under SAE a header's
// state root is the post-execution root of its settled block (avalanchego saevm
// block_builder: hdr.Root = lastSettled.PostExecutionStateRoot()), not of its own block;
// before SAE (no settled height) it is its own block's.
type Check struct {
	Height uint64   // the block whose post-state is compared
	Header uint64   // the block whose header carries the root
	Want   [32]byte // the header's state root
	Got    [32]byte
}

func (c Check) OK() bool { return c.Want == c.Got }

func (c Check) String() string {
	r := "ok"
	if !c.OK() {
		r = fmt.Sprintf("MISMATCH: computed %x", c.Got)
	}
	return fmt.Sprintf("state after block %d vs header %d root %x: %s", c.Height, c.Header, c.Want, r)
}

// Version is what applying one tier0 gave: the state at its last block. Only the current trie
// is kept (no per-file trie rows), so proofs are served at the top's block.
type Version struct {
	First, Last uint64
	Root        [32]byte // the state root after Last
	Checks      []Check
	Writes      int // writes applied
	Recuts      int // tries re-cut at the end of the file
	Prefetch    time.Duration
	Apply       time.Duration
	Roots       time.Duration // the Root calls
	RootCalls   int
	Recut       time.Duration
}

// RootMismatch is returned when a computed root differs from a header's.
type RootMismatch struct{ Check Check }

func (e *RootMismatch) Error() string { return "trie: " + e.Check.String() }

type hdrRoot struct {
	header uint64
	root   [32]byte
}

// settlements maps each settled height the file's headers name to the first header naming it
// and its root; headers naming the same height must agree.
func settlements(f *tier0.File) (map[uint64]hdrRoot, error) {
	out := map[uint64]hdrRoot{}
	for i := range f.Blocks {
		h, err := fileHeader(f, i)
		if err != nil {
			return nil, err
		}
		s := h.Settles
		if p, ok := out[s]; ok {
			if p.root != h.Root {
				return nil, fmt.Errorf("trie: headers %d and %d settle block %d with different roots %x and %x", p.header, h.Number, s, p.root, h.Root)
			}
			continue
		}
		out[s] = hdrRoot{h.Number, h.Root}
	}
	return out, nil
}

// ApplyTier0 applies the write sets of the next tier0 (it must start at Block+1), checks the
// roots its headers allow and returns what the apply gave.
//
// Checks: (1) if a header of the file settles Block (the previous version's block, or the
// base block), the previous root against it; (2) the state after S, the highest block the
// file's headers settle, if S lies inside the file: Root after applying blocks up to S (with
// EveryBlock: every settled height inside the file, each after its block). The
// version's own root (after the last block) is usually settled only by a header of the next
// tier0, and is checked there by (1). A mismatch returns *RootMismatch and leaves the Top
// unusable (part-applied).
//
// A tier0 that ApplyBlock began (the live node) is finished instead: its blocks ApplyBlock has
// not applied yet are applied the same way, block by block, then the tier0 ends as here.
func (t *Top) ApplyTier0(f *tier0.File) (*Version, error) {
	defer t.changing()()
	if t.err != nil {
		return nil, t.err
	}
	if t.open != nil {
		return t.finishOpen(f)
	}
	if f.First != t.Block+1 {
		return nil, fmt.Errorf("trie: tier0 starts at block %d, the top is at block %d", f.First, t.Block)
	}
	v, err := t.apply(f)
	if err == nil && t.Live != nil {
		t.Live.restart(v.Last, v.Root) // the roots of a tier0 applied whole: only its last block's
	}
	if err != nil {
		var be BaseError
		if errors.As(err, &be) {
			err = be // absent or damaged base chunk: the caller may refetch it and restart from a cut
		}
		t.err = fmt.Errorf("trie: top unusable after a failed apply at block %d: %w", f.First, err)
	}
	return v, err
}

func (t *Top) apply(f *tier0.File) (v *Version, err error) {
	defer func() {
		if r := recover(); r != nil {
			be, ok := r.(BaseError)
			if !ok {
				panic(r)
			}
			err = be
		}
	}()
	last := f.First + uint64(f.Blocks) - 1
	v = &Version{First: f.First, Last: last}
	set, err := settlements(f)
	if err != nil {
		return nil, err
	}
	check := func(height uint64, got [32]byte) error {
		h := set[height]
		c := Check{Height: height, Header: h.header, Want: h.root, Got: got}
		v.Checks = append(v.Checks, c)
		if !c.OK() {
			return &RootMismatch{c}
		}
		return nil
	}
	if _, ok := set[t.Block]; ok {
		if err := check(t.Block, t.Root); err != nil {
			return v, err
		}
	}
	var top uint64 // the highest settled height inside the file
	for s := range set {
		if s >= f.First && s <= last && s > top {
			top = s
		}
	}
	t.E.gen++
	a := applier{e: t.E}
	var gs [][][]tier0.Write
	var bp [][]prepW
	if t.E.src != nil {
		if t.LiveOverlay && t.E.hasLayers() {
			return v, errors.New("trie: a whole tier0 over a live overlay (the store must hold every write below it)")
		}
		t0 := time.Now()
		if gs, bp, err = t.prepare(f.Blocks, f.Writes); err != nil {
			return v, err
		}
		v.Prefetch = time.Since(t0)
		t.E.fw = newFileWrites()
		defer func() { t.E.fw = fileWrites{} }() // settled into the store once applied
	} else if Prefetch {
		t0 := time.Now()
		if err := t.prefetch(f); err != nil {
			return v, err
		}
		v.Prefetch = time.Since(t0)
	}
	for i := range f.Blocks {
		n := f.First + uint64(i)
		var groups [][]tier0.Write
		var ps []prepW
		if gs != nil {
			groups, ps = gs[i], bp[i]
		} else if groups, err = f.Writes(i); err != nil {
			return v, err
		}
		t0 := time.Now()
		nw, err := a.block(n, groups, ps)
		v.Writes += nw
		if err != nil {
			return v, err
		}
		v.Apply += time.Since(t0)
		_, settled := set[n]
		if n == top || n == last || t.EveryBlock {
			t0 = time.Now()
			root := t.E.Root()
			v.Roots += time.Since(t0)
			v.RootCalls++
			if n == top || (t.EveryBlock && settled) {
				if err := check(n, root); err != nil {
					return v, err
				}
			}
			if n == last {
				v.Root = root
			}
		}
	}
	t.end(v)
	return v, nil
}

// end ends the tier0 of v (applied up to v.Last, whose root is v.Root): the re-cuts at the
// file's end, so the layout follows from the state at cuts, and the memo sweep.
func (t *Top) end(v *Version) {
	t0 := time.Now()
	v.Recuts = t.E.Recut()
	t.E.sweepMemos()
	v.Recut = time.Since(t0)
	t.Block, t.Root = v.Last, v.Root
}

// block applies block n's write groups (ps: prepare's results for them, else nil) and returns
// the writes applied.
func (a *applier) block(n uint64, groups [][]tier0.Write, ps []prepW) (int, error) {
	if ps != nil && len(ps) >= applyPar {
		if nw, err, ok := a.blockPar(n, groups, ps); ok {
			return nw, err
		}
	}
	nw := 0
	for _, g := range groups {
		var pg []prepW
		if ps != nil {
			pg, ps = ps[:len(g)], ps[len(g):]
		}
		if err := a.group(g, pg); err != nil {
			return nw, fmt.Errorf("trie: block %d: %w", n, err)
		}
		nw += len(g)
	}
	return nw, nil
}

// Prefetch makes ApplyTier0 read, on all threads and before applying anything, the base bytes
// the file's writes and roots will touch (a var so measurements can turn it off).
var Prefetch = true

// prepW is what prepare finds for one write, in the file's write order.
type prepW struct {
	ah     [32]byte // keccak of the address
	sh     [32]byte // slot writes: keccak of the state key
	old    [32]byte // slot writes: the value before the file (Settled overlay, else the base)
	acct   ovAcct   // an address's first write in a group: the account before the file
	exists bool
}

// prepare (Settled source) decodes the write groups of nb blocks (writes(i): block i's) and finds, on all threads and before
// applying anything, every write's hashes and the values before the file it will be checked
// against (per block, in write order; the slices come from the workers' reused arenas): the
// Settled overlay does not move while the file is applied and the base never does, so only the
// file's own earlier writes are left to the sequential apply. With Prefetch it also touches the
// base run the root will rehash for every write: its sub-cut's when the cut has a filled memo,
// else the whole cut's (the account cut's every record).
func (t *Top) prepare(nb int, writes func(i int) ([][]tier0.Write, error)) ([][][]tier0.Write, [][]prepW, error) {
	e := t.E
	gs := make([][][]tier0.Write, nb)
	bp := make([][]prepW, nb)
	for _, w := range e.pool() {
		w.prep = w.prep[:0]
	}
	var perr atomic.Value
	var sink atomic.Uint64
	e.parallel(nb, func(w *worker, i int) {
		groups, err := writes(i)
		if err != nil {
			perr.Store(err)
			return
		}
		gs[i] = groups
		n := 0
		for _, g := range groups {
			n += len(g)
		}
		if cap(w.prep)-len(w.prep) < n { // a new arena: the old one stays with the blocks that use it
			w.prep = make([]prepW, 0, max(2*cap(w.prep), n, 4096))
		}
		ps := w.prep[len(w.prep) : len(w.prep)+n]
		w.prep = w.prep[:len(w.prep)+n]
		bp[i] = ps
		sink.Add(e.prepGroups(w, groups, ps, nil))
	}, nil)
	if err, _ := perr.Load().(error); err != nil {
		return nil, nil, err
	}
	return gs, bp, nil
}

// prepGroups is prepare's work for groups on worker w: it fills ps (one per write,
// in order) and, with Prefetch, touches the base runs; it returns the sum of the bytes touched.
// known (ApplyBlockKnown; nil: none) is every write's slot hash, and the writes' Old are the
// current values: they are taken instead of hashing and reading again.
func (e *Engine) prepGroups(w *worker, groups [][]tier0.Write, ps []prepW, known [][32]byte) uint64 {
	var x uint64
	touch := func(b []byte) {
		for o := 0; o < len(b); o += 4096 {
			x += uint64(b[o])
		}
		if len(b) > 0 {
			x += uint64(b[len(b)-1])
		}
	}
	at := e.acct
	// the keys' hashes first, in lanes (Lanes): every address's first write in a group and
	// every slot (known: the caller's)
	w.kmsgs, w.kdst = w.kmsgs[:0], w.kdst[:0]
	k := 0
	for _, g := range groups {
		for j := range g {
			wr, p := &g[j], &ps[k]
			k++
			if j == 0 || g[j-1].Addr != wr.Addr {
				w.kmsgs, w.kdst = append(w.kmsgs, wr.Addr[:]), append(w.kdst, &p.ah)
			}
			if wr.Kind == tier0.KindSlot && known == nil {
				w.kmsgs, w.kdst = append(w.kmsgs, wr.Slot[:]), append(w.kdst, &p.sh)
			}
		}
	}
	if Lanes && len(w.kmsgs) > 1 {
		if cap(w.ksums) < len(w.kmsgs) {
			w.ksums = make([][32]byte, len(w.kmsgs), 2*len(w.kmsgs))
		}
		w.ksums = w.ksums[:len(w.kmsgs)]
		w.kidx = keccaksimd.HashBatch(w.ksums, w.kmsgs, w.kidx)
		for i, d := range w.kdst {
			*d = w.ksums[i]
		}
	} else {
		for i, d := range w.kdst {
			*d = w.h.k.Sum(w.kmsgs[i])
		}
	}
	k = 0
	for _, g := range groups {
		for j := range g {
			wr, p := &g[j], &ps[k]
			k++
			first := j == 0 || g[j-1].Addr != wr.Addr
			if first {
				var ok bool
				if p.acct, p.exists, ok = e.acctNow(w, &wr.Addr); !ok {
					p.acct, p.exists = e.account(&p.ah, &w.rec) // the base
				}
			} else {
				p.ah = ps[k-2].ah
			}
			if wr.Kind == tier0.KindSlot {
				if known != nil {
					p.sh, p.old = known[k-1], wr.Old
				} else {
					var ok bool
					if p.old, ok = e.slotNow(w, &wr.Addr, &p.sh); !ok {
						b, _ := e.GetSlot(&p.ah, &p.sh, &w.rec) // the base
						p.old = [32]byte{}
						copy(p.old[32-len(b):], b)
					}
				}
				if Prefetch {
					if st := e.trie(&p.ah); st != nil && !st.noBase {
						touch(e.baseBytes(st.runOf(&p.sh)))
					}
				}
			}
			if Prefetch && first {
				lo, hi := at.runOf(&p.ah)
				for o := lo; o < hi; o++ {
					touch(e.acctRec(e.acctOff[o]))
				}
			}
		}
	}
	return x
}

// prepChunk is the fewest writes prepareBlock gives one worker.
const prepChunk = 32

// prepareBlock is prepare for one block (the live node's per-block apply): the block's write
// groups are split, whole, over the workers in runs of about an equal number of writes (at
// least prepChunk), so a big block's reads and base touches do not wait on one thread. The
// slice is the Top's, reused by the next block.
func (t *Top) prepareBlock(groups [][]tier0.Write, known [][32]byte) []prepW {
	e := t.E
	n := 0
	for _, g := range groups {
		n += len(g)
	}
	if cap(t.bprep) < n {
		t.bprep = make([]prepW, 0, max(n, 2*cap(t.bprep)))
	}
	ps := t.bprep[:n]
	per := max(prepChunk, (n+len(e.pool())-1)/len(e.pool()))
	type run struct{ g0, g1, w0, w1 int } // groups [g0, g1), their writes [w0, w1)
	var runs []run
	r := run{}
	for i, g := range groups {
		r.w1 += len(g)
		if r.w1-r.w0 >= per || i == len(groups)-1 {
			r.g1 = i + 1
			runs = append(runs, r)
			r = run{g0: i + 1, w0: r.w1, w1: r.w1}
		}
	}
	t.prepWorkers = min(len(runs), len(e.pool()))
	var sink atomic.Uint64
	e.parallel(len(runs), func(w *worker, i int) {
		r := runs[i]
		var kn [][32]byte
		if known != nil {
			kn = known[r.w0:r.w1]
		}
		sink.Add(e.prepGroups(w, groups[r.g0:r.g1], ps[r.w0:r.w1], kn))
	}, nil)
	return ps
}

// prefetch touches the base run of every cut the file's writes dirty: the slot's storage cut
// and the account's account cut (every account record in it, as the root rehashes them all).
// It only reads the engine, so it runs on all threads before the sequential apply.
func (t *Top) prefetch(f *tier0.File) error {
	e := t.E
	var perr atomic.Value
	var sink atomic.Uint64
	e.parallel(f.Blocks, func(w *worker, i int) {
		groups, err := f.Writes(i)
		if err != nil {
			perr.Store(err)
			return
		}
		var x uint64
		touch := func(b []byte) {
			for o := 0; o < len(b); o += 4096 {
				x += uint64(b[o])
			}
			if len(b) > 0 {
				x += uint64(b[len(b)-1])
			}
		}
		at := e.acct
		for _, g := range groups {
			for j := range g {
				wr := &g[j]
				ah := w.h.k.Sum(wr.Addr[:])
				if wr.Kind == tier0.KindSlot {
					if st := e.trie(&ah); st != nil && !st.noBase {
						sh := w.h.k.Sum(wr.Slot[:])
						c := int(st.cutOf(&sh))
						touch(e.baseBytes(st.bound(c), st.bound(c+1)))
					}
				}
				if j > 0 && g[j-1].Addr == wr.Addr {
					continue // the account cut was touched for the address's first write
				}
				c := at.cutOf(&ah)
				for o := at.bound(int(c)); o < at.bound(int(c)+1); o++ {
					touch(e.acctRec(e.acctOff[o]))
				}
			}
		}
		sink.Add(x)
	}, nil)
	if err, _ := perr.Load().(error); err != nil {
		return err
	}
	return nil
}

// applier applies tier0 write groups to the engine, checking every old value against the
// state it applies to.
type applier struct {
	e   *Engine
	k   *Keccaker
	rec era.Rec
}

func (a *applier) keccak(b []byte) [32]byte {
	if a.k == nil {
		a.k = NewKeccaker()
	}
	return a.k.Sum(b)
}

func trim(b []byte) []byte {
	for len(b) > 0 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

var errOld = errors.New("old value differs from the state")

// group applies one tx's (or pre/end ops') writes: sorted by (address, kind, slot), so an
// address's slots come before its account fields. ps: prepare's results for g (Settled source),
// else nil.
func (a *applier) group(g []tier0.Write, ps []prepW) error {
	return a.groupOf(g, ps, true)
}

// groupOf is group; slots false skips the slot writes (the parallel apply checks and records them).
func (a *applier) groupOf(g []tier0.Write, ps []prepW, slots bool) error {
	for lo := 0; lo < len(g); {
		hi := lo + 1
		for hi < len(g) && g[hi].Addr == g[lo].Addr {
			hi++
		}
		var p []prepW
		if ps != nil {
			p = ps[lo:hi]
		}
		if err := a.address(g[lo:hi], p, slots); err != nil {
			return err
		}
		lo = hi
	}
	return nil
}

// address applies one address's writes of a group. ps (Settled source): prepare's hashes and
// values before the file for ws; the file's own earlier writes (fw) come first.
func (a *applier) address(ws []tier0.Write, ps []prepW, slots bool) error {
	addr := ws[0].Addr
	store := a.e.src != nil
	var ah [32]byte
	var cur ovAcct
	var exists bool
	if store {
		ah = ps[0].ah
		if x, ok := a.e.fw.accts[addr]; ok {
			cur, exists = x, !x.gone
			if x.gone {
				cur = ovAcct{}
			}
		} else {
			cur, exists = ps[0].acct, ps[0].exists
		}
	} else {
		ah = a.keccak(addr[:])
		cur, exists = a.e.account(&ah, &a.rec)
	}
	var bal, nonce, code, multi *[32]byte
	for i := range ws {
		w := &ws[i]
		switch w.Kind {
		case tier0.KindSlot:
			if !slots {
				continue
			}
			if store {
				sh := ps[i].sh
				var old [32]byte
				ok := false
				if sm := a.e.fw.slots[addr]; sm != nil {
					old, ok = sm.get(&sh)
				}
				if !ok && !a.e.fw.wiped[addr] {
					old = ps[i].old
				}
				if old != w.Old {
					return fmt.Errorf("%w: %x slot %x: write says %x, state has %x", errOld, addr, w.Slot, w.Old, old)
				}
				a.e.putSlot(&addr, ah, sh, w.New)
				continue
			}
			sh := a.keccak(w.Slot[:])
			old, _ := a.e.GetSlot(&ah, &sh, &a.rec)
			if !bytes.Equal(trim(old), trim(w.Old[:])) {
				return fmt.Errorf("%w: %x slot %x: write says %x, state has %x", errOld, addr, w.Slot, w.Old, old)
			}
			a.e.SetSlot(ah, sh, trim(w.New[:]))
		case tier0.KindBalance:
			if w.Old != cur.balance {
				return fmt.Errorf("%w: %x balance: write says %x, state has %x", errOld, addr, w.Old, cur.balance)
			}
			bal = &w.New
		case tier0.KindNonce:
			if old := u64Word(cur.nonce); w.Old != old {
				return fmt.Errorf("%w: %x nonce: write says %x, state has %x", errOld, addr, w.Old, old)
			}
			nonce = &w.New
		case tier0.KindCode:
			old := [32]byte{}
			if exists {
				old = EmptyCode
				if cur.hasCode {
					old = cur.code
				}
			}
			if w.Old != old {
				return fmt.Errorf("%w: %x code hash: write says %x, state has %x", errOld, addr, w.Old, old)
			}
			code = &w.New
		case tier0.KindMulti: // values 0 or 1 (the tier0 reader checks)
			old := [32]byte{}
			if exists && cur.multicoin {
				old[31] = 1
			}
			if w.Old != old {
				return fmt.Errorf("%w: %x multicoin flag: write says %x, state has %x", errOld, addr, w.Old, old)
			}
			multi = &w.New
		default:
			return fmt.Errorf("write kind %d", w.Kind)
		}
	}
	if bal == nil && nonce == nil && code == nil && multi == nil {
		return nil // slots only: the account leaf follows its storage root
	}
	n := cur
	if bal != nil {
		n.balance = *bal
	}
	if nonce != nil {
		if trimmed := trim(nonce[:]); len(trimmed) > 8 {
			return fmt.Errorf("%x: nonce %x over 64 bits", addr, *nonce)
		}
		n.nonce = wordU64(nonce)
	}
	alive := exists
	if code != nil {
		alive = *code != [32]byte{}
		n.hasCode = alive && *code != EmptyCode
		n.code = [32]byte{}
		if n.hasCode {
			n.code = *code
		}
	}
	if !alive {
		if !exists && code == nil {
			return fmt.Errorf("%x: field writes on an absent account without a code hash write", addr)
		}
		if n.balance != ([32]byte{}) || n.nonce != 0 {
			return fmt.Errorf("%x: removed with balance %x nonce %d", addr, n.balance, n.nonce)
		}
		if store {
			a.e.removeAccount(&addr, ah)
		} else {
			a.e.DeleteAccount(ah)
		}
		return nil
	}
	if !exists {
		n.multicoin = false // a new account; the sticky multicoin flag starts false
	}
	if multi != nil {
		n.multicoin = multi[31] == 1
	}
	if store {
		a.e.putAccount(&addr, ah, n)
		return nil
	}
	var codeHash []byte
	if n.hasCode {
		codeHash = n.code[:]
	}
	a.e.SetAccount(ah, n.nonce, n.balance[:], codeHash, n.multicoin)
	return nil
}

func u64Word(x uint64) (w [32]byte) {
	for i := range 8 {
		w[31-i] = byte(x >> (8 * i))
	}
	return w
}

func wordU64(w *[32]byte) (x uint64) {
	for i := 24; i < 32; i++ {
		x = x<<8 | uint64(w[i])
	}
	return x
}
