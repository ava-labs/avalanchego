package trie

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/cblock"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/tier0"
)

// The live node checks the state root after every block (user rule 2026-10-22): ApplyBlock
// applies the open tier0 block by block as the executor hands the blocks over, computes the
// root after each and checks every header against the root of the height it settles, while
// the tier0 is still open. The cut then only ends the tier0 (ApplyTier0: the re-cuts and the
// memo sweep at its last block, as a tier0 applied whole). Nothing written changes: the
// layout follows from the state at the tier0's end either way.

// Header is what the trie checks of a block header: the height whose post-state its state root
// is (SAE: its settled height; before SAE: its own block) and that root.
type Header struct {
	Number, Settles uint64
	Root            [32]byte
}

// HeaderOf is the Header of a block's body (the coreth block bytes).
func HeaderOf(body []byte) (Header, error) {
	b, err := cblock.Decode(body)
	if err != nil {
		return Header{}, err
	}
	return FromHeader(&b.Header), nil
}

// FromHeader is the Header of a decoded block header.
func FromHeader(h *cblock.Header) Header {
	s := h.Number // pre-SAE: the header commits to its own post-state
	if h.SettledHeight != nil {
		s = *h.SettledHeight
	}
	return Header{Number: h.Number, Settles: s, Root: h.Root}
}

// fileHeader is the Header of block i of f.
func fileHeader(f *tier0.File, i int) (Header, error) {
	body, err := f.Body(i)
	if err != nil {
		return Header{}, err
	}
	h, err := HeaderOf(body)
	if err != nil {
		return Header{}, fmt.Errorf("trie: block %d body: %w", f.First+uint64(i), err)
	}
	if h.Number != f.First+uint64(i) {
		return Header{}, fmt.Errorf("trie: body %d of the tier0 is block %d, want %d", i, h.Number, f.First+uint64(i))
	}
	return h, nil
}

// LiveRoots is how many of the newest roots Live keeps: a header settling an older height is
// not checked (SAE settles a block a few seconds after it; the tip lags 6-7 blocks).
const LiveRoots = 256

// Live keeps the roots ApplyBlock computed for the LiveRoots newest blocks, for the headers
// that settle them, and counts the headers.
type Live struct {
	roots map[uint64][32]byte
	// Checked: headers whose settled root was compared (all equal: a mismatch stops the top);
	// Unchecked: headers settling a height without a kept root (before the first root kept
	// after a tier0 applied whole, or older than LiveRoots blocks)
	Checked, Unchecked int
}

// restart keeps only the root after block n (a tier0 applied whole computes no other).
func (l *Live) restart(n uint64, root [32]byte) {
	l.roots = map[uint64][32]byte{n: root}
}

func (l *Live) keep(n uint64, root [32]byte) {
	if l.roots == nil {
		l.roots = map[uint64][32]byte{}
	}
	l.roots[n] = root
	delete(l.roots, n-LiveRoots)
}

// check compares h with the kept root of the height h settles: nil when none is kept.
func (l *Live) check(h Header) (*Check, error) {
	if h.Settles > h.Number {
		return nil, fmt.Errorf("trie: header %d settles a later block %d", h.Number, h.Settles)
	}
	got, ok := l.roots[h.Settles]
	if !ok {
		l.Unchecked++
		return nil, nil
	}
	l.Checked++
	return &Check{Height: h.Settles, Header: h.Number, Want: h.Root, Got: got}, nil
}

// openTier0 is the tier0 ApplyBlock is applying.
type openTier0 struct {
	first, next uint64 // its first block, the next block to apply
	v           Version
}

// BlockStats is what one ApplyBlock did and took.
type BlockStats struct {
	Writes                      int
	PrepWorkers                 int      // the workers its prepare ran on
	Check                       *Check   // the header's check (nil: no root kept for its height)
	RootHash                    [32]byte // the block's root
	Prepare, Apply, Root, Total time.Duration
}

// Open is the first block of the tier0 ApplyBlock is applying and the next block it takes
// (ok false: none).
func (t *Top) Open() (first, next uint64, ok bool) {
	if t.open == nil {
		return 0, 0, false
	}
	return t.open.first, t.open.next, true
}

// ApplyBlock applies block n's write groups (the live node, as the executor hands the block
// over): n is the next block of the tier0 ApplyBlock is applying, or Block+1 to begin one. It
// computes the root after n, keeps it in Live and checks h, n's header, against the kept root
// of the height h settles. A mismatch returns *RootMismatch and leaves the Top unusable, as any
// error does. ApplyTier0 of the tier0's cut file ends it.
func (t *Top) ApplyBlock(n uint64, groups [][]tier0.Write, h Header) (BlockStats, error) {
	return t.applyBlockKnown(n, groups, nil, h)
}

// ApplyBlockKnown is ApplyBlock for a caller that read the block's slots through this Top (the
// store's SlotValue with this Top as its Upper) with no ApplyBlock since: shs[i] is keccak of the
// state key of the i-th write (in group order; unused for a write that is not a slot), and every
// write's Old is the value read. The prepare takes them as they are instead of hashing every slot
// and reading its old value again; the apply still checks Old against the live overlay, and the
// root is checked as in ApplyBlock.
func (t *Top) ApplyBlockKnown(n uint64, groups [][]tier0.Write, shs [][32]byte, h Header) (BlockStats, error) {
	nw := 0
	for _, g := range groups {
		nw += len(g)
	}
	if len(shs) != nw {
		return BlockStats{}, fmt.Errorf("trie: block %d: %d slot hashes for %d writes", n, len(shs), nw)
	}
	return t.applyBlockKnown(n, groups, shs, h)
}

func (t *Top) applyBlockKnown(n uint64, groups [][]tier0.Write, known [][32]byte, h Header) (BlockStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err != nil {
		return BlockStats{}, t.err
	}
	switch {
	case t.Live == nil:
		return BlockStats{}, errors.New("trie: ApplyBlock needs a Live")
	case h.Number != n:
		return BlockStats{}, fmt.Errorf("trie: block %d with the header of %d", n, h.Number)
	case t.open == nil && n != t.Block+1:
		return BlockStats{}, fmt.Errorf("trie: block %d begins a tier0, the top is at block %d", n, t.Block)
	case t.open != nil && n != t.open.next:
		return BlockStats{}, fmt.Errorf("trie: block %d, the open tier0 takes block %d next", n, t.open.next)
	}
	if t.open == nil {
		if len(t.Live.roots) == 0 { // a fresh Live: the top's own root is the first one kept
			t.Live.keep(t.Block, t.Root)
		}
		t.open = &openTier0{first: n, next: n, v: Version{First: n}}
		t.E.gen++ // a tier0 for the memos' clock, as apply counts one
		if t.E.src != nil && (!t.LiveOverlay || t.E.fw.slots == nil) {
			t.E.fw = newFileWrites()
		}
	}
	if t.E.drainC != nil {
		done := t.changing()
		err := t.E.pollDrain(n)
		done()
		if err != nil {
			t.err = err
			return BlockStats{}, err
		}
	}
	defer t.publish() // the live overlay's maps may be new (no version: the executor's own)
	bs, err := t.applyBlock(n, groups, known, h)
	if err != nil {
		var be BaseError
		if errors.As(err, &be) {
			err = be
		}
		t.err = fmt.Errorf("trie: top unusable after a failed apply at block %d: %w", n, err)
	}
	return bs, err
}

func (t *Top) applyBlock(n uint64, groups [][]tier0.Write, known [][32]byte, h Header) (bs BlockStats, err error) {
	defer func() {
		if r := recover(); r != nil {
			be, ok := r.(BaseError)
			if !ok {
				panic(r)
			}
			err = be
		}
	}()
	t0 := time.Now()
	v := &t.open.v
	var ps []prepW
	if t.E.src != nil {
		ps = t.prepareBlock(groups, known)
		bs.PrepWorkers = t.prepWorkers
	}
	t1 := time.Now()
	bs.Prepare = t1.Sub(t0)
	a := applier{e: t.E}
	bs.Writes, err = a.block(n, groups, ps)
	v.Writes += bs.Writes
	if err != nil {
		return bs, err
	}
	t2 := time.Now()
	bs.Apply = t2.Sub(t1)
	root := t.E.Root()
	bs.Root = time.Since(t2)
	v.Prefetch += bs.Prepare
	v.Apply += bs.Apply
	v.Roots += bs.Root
	v.RootCalls++
	v.Last, v.Root = n, root
	t.open.next = n + 1
	t.Live.keep(n, root)
	bs.RootHash = root
	c, err := t.Live.check(h)
	if err != nil {
		return bs, err
	}
	bs.Check = c
	bs.Total = time.Since(t0)
	if c != nil && !c.OK() {
		return bs, &RootMismatch{*c}
	}
	return bs, nil
}

// finishOpen ends the tier0 ApplyBlock began, whose cut file is f: the blocks of f not applied
// yet are applied by ApplyBlock (with their headers from f), then the tier0 ends.
func (t *Top) finishOpen(f *tier0.File) (*Version, error) {
	if f.First != t.open.first {
		return nil, fmt.Errorf("trie: tier0 starts at block %d, the open tier0 at block %d", f.First, t.open.first)
	}
	last := f.First + uint64(f.Blocks) - 1
	if t.open.next > last+1 {
		return nil, fmt.Errorf("trie: tier0 %d-%d, ApplyBlock applied up to block %d", f.First, last, t.open.next-1)
	}
	for n := t.open.next; n <= last; n++ {
		i := int(n - f.First)
		groups, err := f.Writes(i)
		if err != nil {
			return nil, err
		}
		h, err := fileHeader(f, i)
		if err != nil {
			return nil, err
		}
		if _, err := t.ApplyBlock(n, groups, h); err != nil {
			return &t.open.v, err
		}
	}
	v := t.open.v
	t.open = nil
	t.end(&v) // the re-cuts read the tier0's writes: before they go
	if !t.LiveOverlay {
		t.E.fw = fileWrites{} // settled into the store once applied
	}
	return &v, nil
}

// ReadBlock reads block n of the open tier0 for ApplyLive: its write groups and body; ok false
// when the tier0 holding n is no longer open (sealed meanwhile: its cut file follows).
type ReadBlock func(n uint64) (groups [][]tier0.Write, body []byte, ok bool, err error)

// ApplyLive applies block n of the open tier0 as the live node hands it over (groups, header
// h): first, with read, the blocks of the tier0 before n that ApplyBlock has not applied (the
// tier0's blocks before the first block executed at the tip, or blocks executed behind it),
// each with its root and check, then n. When read finds the tier0 sealed it stops and returns
// done false: the cut file applies the rest (ApplyTier0). caught is the number of blocks read.
func (t *Top) ApplyLive(n uint64, groups [][]tier0.Write, h Header, read ReadBlock) (caught int, bs BlockStats, done bool, err error) {
	next := t.Block + 1
	if _, nx, ok := t.Open(); ok {
		next = nx
	}
	if n < next {
		return 0, bs, false, fmt.Errorf("trie: live block %d, already applied up to %d", n, next-1)
	}
	for m := next; m < n; m++ {
		gs, body, ok, err := read(m)
		if err != nil || !ok {
			return caught, bs, false, err
		}
		hm, err := HeaderOf(body)
		if err != nil {
			return caught, bs, false, fmt.Errorf("trie: block %d body: %w", m, err)
		}
		if _, err := t.ApplyBlock(m, gs, hm); err != nil {
			return caught, bs, false, err
		}
		caught++
	}
	bs, err = t.ApplyBlock(n, groups, h)
	return caught, bs, err == nil, err
}

// EndTier0 ends the open tier0 without a cut file, as a cut file's end does (the re-cuts and
// the memo sweep): for a live node that keeps no tier0 files. Its cadence sets the layout
// (the re-cuts follow from the state at each end), so every node of a network ends its tier0s
// at the same blocks. It returns the tries re-cut.
func (t *Top) EndTier0() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open == nil {
		return 0
	}
	defer t.changing()()
	v := t.open.v
	t.open = nil
	t.end(&v)
	return v.Recuts
}
