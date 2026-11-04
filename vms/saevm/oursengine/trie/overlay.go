package trie

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/tier0"
)

// The live overlay (overlay build 2026-10-30). With Top.LiveOverlay the engine's map of the
// writes (fileWrites: per address 16 maps by the last nibble of keccak(slot), filled by the
// parallel apply) is not dropped at a tier0's end: it holds every write since the last Swap and
// is the store's RAM overlay, so the store is not given the tier0 (no file re-read, no decode, no
// per window sort, no settle). Swap (the hourly fold) freezes it, starts a fresh one and drains
// the frozen one sorted to a level 1 file on side goroutines; once written, the file (mapped,
// bsearch per key) replaces the frozen overlay at the next block. Reads go newest first: live,
// frozen, level 1 files (newest first), the store's Settled view, the base.

// DefaultK is the default cut size (keys per cut) of a built top. An opened top keeps the K of
// its file.
const DefaultK = 128

// hasLayers: a live overlay holds writes above the store (LiveOverlay).
func (e *Engine) hasLayers() bool {
	return !e.fw.empty() || e.frozen != nil || len(e.l1) > 0 || e.drainC != nil
}

func (f *fileWrites) empty() bool {
	return len(f.slots) == 0 && len(f.accts) == 0 && len(f.wiped) == 0
}

// slot is sh of addr in this layer; stop: found (v), or addr was deleted in this layer and sh
// not written since (v zero): the layers below do not count.
func (f *fileWrites) slot(addr *[20]byte, sh *[32]byte) (v [32]byte, stop bool) {
	if m := f.slots[*addr]; m != nil {
		if v, ok := m.get(sh); ok {
			return v, true
		}
	}
	return [32]byte{}, f.wiped[*addr]
}

func memAcct(a ovAcct) (ovAcct, bool, bool) {
	if a.gone {
		return ovAcct{}, false, true
	}
	return a, true, true
}

// slotAbove is sh of addr in the layers above the store (live, frozen, level 1); ok false: none
// holds it.
func (e *Engine) slotAbove(addr *[20]byte, sh *[32]byte) (v [32]byte, ok bool) {
	return slotIn(&e.fw, e.frozen, e.l1, addr, sh)
}

func slotIn(fw, frozen *fileWrites, l1 []*l1File, addr *[20]byte, sh *[32]byte) (v [32]byte, ok bool) {
	if v, ok = fw.slot(addr, sh); ok {
		return v, true
	}
	if frozen != nil {
		if v, ok = frozen.slot(addr, sh); ok {
			return v, true
		}
	}
	for _, l := range l1 {
		if v, ok = l.slot(addr, sh); ok {
			return v, true
		}
	}
	return [32]byte{}, false
}

// readLayers are the layers above the store as UpperSlot reads them without a lock: the live
// overlay's maps, the frozen overlay, the level 1 files (a new slice at every install).
type readLayers struct {
	fw     fileWrites
	frozen *fileWrites
	l1     []*l1File
}

// publish makes the layers as they are now the ones UpperSlot reads (t.mu held, or the
// executor's own call).
func (t *Top) publish() {
	t.lay.Store(&readLayers{fw: t.E.fw, frozen: t.E.frozen, l1: t.E.l1})
}

// changing marks a change of the layers (ver odd) until the returned func publishes them and
// ends it (ver even).
func (t *Top) changing() func() {
	t.ver.Add(1)
	return func() {
		t.publish()
		t.ver.Add(1)
	}
}

// slotNow is sh of addr from the layers, else the store's Settled view; ok false: the base's.
func (e *Engine) slotNow(w *worker, addr *[20]byte, sh *[32]byte) (v [32]byte, ok bool) {
	if v, ok = e.slotAbove(addr, sh); ok {
		return v, true
	}
	return e.settledSlot(w, addr, sh)
}

// acctAbove is addr's account in the layers above the store; ok false: none holds it.
func (e *Engine) acctAbove(addr *[20]byte) (a ovAcct, exists, ok bool) {
	if a, ok := e.fw.accts[*addr]; ok {
		return memAcct(a)
	}
	if f := e.frozen; f != nil {
		if a, ok := f.accts[*addr]; ok {
			return memAcct(a)
		}
	}
	for _, l := range e.l1 {
		if a, exists, ok = l.acct(addr); ok {
			return a, exists, true
		}
	}
	return ovAcct{}, false, false
}

// fromLayer fills the keys of shs w has not answered yet from layer f and returns the keys left;
// 0 when f deleted addr (every key left is then 0).
func (w *worker) fromLayer(f *fileWrites, addr *[20]byte, shs [][32]byte, left int) int {
	if m := f.slots[*addr]; m != nil {
		for i := range shs {
			if !w.ovok[i] {
				if v, ok := m.get(&shs[i]); ok {
					w.ovv[i], w.ovok[i] = v, true
					left--
				}
			}
		}
	}
	if left > 0 && f.wiped[*addr] {
		w.zeroRest()
		return 0
	}
	return left
}

// fromL1 is fromLayer for a level 1 file.
func (w *worker) fromL1(l *l1File, addr *[20]byte, shs [][32]byte, left int) int {
	a, ok := l.accts[*addr]
	if !ok {
		return left
	}
	if a.n > 0 {
		for i := range shs {
			if !w.ovok[i] {
				if v, ok := l.find(&a, &shs[i]); ok {
					w.ovv[i], w.ovok[i] = v, true
					left--
				}
			}
		}
	}
	if left > 0 && a.flags&l1Wiped != 0 {
		w.zeroRest()
		return 0
	}
	return left
}

func (w *worker) zeroRest() {
	for i := range w.ovok {
		if !w.ovok[i] {
			w.ovv[i], w.ovok[i] = [32]byte{}, true
		}
	}
}

// ---- level 1 files ----

// A level 1 file: the slot records, per account in keccak(address) order, each account's sorted
// by keccak(slot): keccak(slot) 32 B, value 32 B; then the account table (l1AcctSize B per
// account, keccak(address) order): address 20, keccak(address) 32, flags 1, nonce u64 LE,
// balance 32, code hash 32, first record u64 LE, records u64 LE; then the footer: accounts,
// table offset, records (u64 LE each) and l1Magic. Fixed records (FoldWalk wrote a per account
// header and trimmed values) so a lookup is a bsearch with no index.
const (
	l1Rec      = 64
	l1AcctSize = 20 + 32 + 1 + 8 + 32 + 32 + 8 + 8
	l1Footer   = 32
	l1Magic    = "ffl1v001"
)

const (
	l1HasAcct = 1 << iota // an account record (else slot writes or a deletion only)
	l1Gone                // the account record says: does not exist
	l1Wiped               // deleted in the hour: older storage is gone
	l1HasCode
	l1Multi
)

type l1Acct struct {
	flags         uint8
	nonce         uint64
	balance, code [32]byte
	off, n        int // slot records [off, off+n)
}

type l1File struct {
	Path  string
	mm    []byte
	rec   []byte
	accts map[[20]byte]l1Acct
}

func (l *l1File) find(a *l1Acct, sh *[32]byte) (v [32]byte, ok bool) {
	r := l.rec[a.off*l1Rec : (a.off+a.n)*l1Rec]
	lo, hi := 0, a.n
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if bytes.Compare(r[m*l1Rec:m*l1Rec+32], sh[:]) < 0 {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo < a.n && bytes.Equal(r[lo*l1Rec:lo*l1Rec+32], sh[:]) {
		copy(v[:], r[lo*l1Rec+32:lo*l1Rec+64])
		return v, true
	}
	return v, false
}

func (l *l1File) slot(addr *[20]byte, sh *[32]byte) (v [32]byte, stop bool) {
	a, ok := l.accts[*addr]
	if !ok {
		return v, false
	}
	if v, ok = l.find(&a, sh); ok {
		return v, true
	}
	return [32]byte{}, a.flags&l1Wiped != 0
}

func (l *l1File) acct(addr *[20]byte) (ovAcct, bool, bool) {
	a, ok := l.accts[*addr]
	if !ok || a.flags&l1HasAcct == 0 {
		return ovAcct{}, false, false
	}
	if a.flags&l1Gone != 0 {
		return ovAcct{}, false, true
	}
	return ovAcct{nonce: a.nonce, balance: a.balance, code: a.code, hasCode: a.flags&l1HasCode != 0, multicoin: a.flags&l1Multi != 0}, true, true
}

func (l *l1File) close() error {
	if l.mm == nil {
		return nil
	}
	mm := l.mm
	l.mm, l.rec = nil, nil
	return syscall.Munmap(mm)
}

var errL1 = errors.New("trie: not a level 1 file")

// openL1 maps a level 1 file and reads its account table.
func openL1(path string) (*l1File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size < l1Footer {
		return nil, errL1
	}
	mm, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	ft := mm[size-l1Footer:]
	na, toff, nr := binary.LittleEndian.Uint64(ft), binary.LittleEndian.Uint64(ft[8:]), binary.LittleEndian.Uint64(ft[16:])
	if string(ft[24:]) != l1Magic || toff != nr*l1Rec || toff+na*l1AcctSize+l1Footer != uint64(size) {
		syscall.Munmap(mm)
		return nil, errL1
	}
	l := &l1File{Path: path, mm: mm, rec: mm[:toff], accts: make(map[[20]byte]l1Acct, na)}
	for i := range na {
		b := mm[toff+i*l1AcctSize:]
		a := l1Acct{flags: b[52], nonce: binary.LittleEndian.Uint64(b[53:]), balance: [32]byte(b[61:93]), code: [32]byte(b[93:125]),
			off: int(binary.LittleEndian.Uint64(b[125:])), n: int(binary.LittleEndian.Uint64(b[133:]))}
		if uint64(a.off+a.n) > nr || a.off < 0 || a.n < 0 {
			syscall.Munmap(mm)
			return nil, errL1
		}
		l.accts[[20]byte(b[:20])] = a
	}
	return l, nil
}

// ---- the swap and the drain ----

// DrainStats is one drain of a frozen overlay to a level 1 file.
type DrainStats struct {
	Path                string
	Accounts, Keys      int
	Bytes               int64
	Workers             int
	Start, End          time.Time
	Gather, Sort, Write time.Duration // the drain's phases
	Installed           uint64        // the level 1 file replaced the frozen overlay before this block (0: at WaitDrain)
	FrozenKeys          int           // slot keys of the frozen overlay
	WaitedBySwap        time.Duration // a Swap waited this long for it (it outlasted the hour)
}

type drainDone struct {
	l   *l1File
	st  DrainStats
	err error
}

// drainSplit: an account with at least this many keys is sorted in 16 parts (by the first nibble
// of keccak(slot)) on the drain's goroutines.
var drainSplit = 4096

type kv64 struct{ sh, v [32]byte }

// runN runs fn(0..n-1) on g goroutines.
func runN(n, g int, fn func(i int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(g, n) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}

// drainTo writes f sorted to a level 1 file at path on g goroutines and opens it. f is not
// changed (it is frozen: readers use it meanwhile).
func drainTo(f *fileWrites, path string, g int) (*l1File, DrainStats, error) {
	st := DrainStats{Path: path, Workers: g, Start: time.Now(), FrozenKeys: f.nslots}
	type acc struct {
		addr [20]byte
		ah   [32]byte
		kvs  []kv64
		segs [][2]int // independently sorted ranges of kvs, in order
	}
	seen := make(map[[20]byte]struct{}, len(f.slots)+len(f.accts))
	for a := range f.slots {
		seen[a] = struct{}{}
	}
	for a := range f.accts {
		seen[a] = struct{}{}
	}
	for a := range f.wiped {
		seen[a] = struct{}{}
	}
	as := make([]acc, 0, len(seen))
	kc := NewKeccaker()
	for a := range seen {
		as = append(as, acc{addr: a, ah: kc.Sum(a[:])})
	}
	slices.SortFunc(as, func(a, b acc) int { return bytes.Compare(a.ah[:], b.ah[:]) })
	runN(len(as), g, func(i int) {
		a := &as[i]
		m := f.slots[a.addr]
		if m == nil {
			return
		}
		n := m.len()
		kvs := make([]kv64, 0, n)
		for x := range m {
			for sh, v := range m[x] {
				kvs = append(kvs, kv64{sh, v})
			}
		}
		if n < drainSplit {
			a.kvs, a.segs = kvs, [][2]int{{0, n}}
			return
		}
		var cnt [17]int
		for i := range kvs {
			cnt[kvs[i].sh[0]>>4+1]++
		}
		for x := 1; x <= 16; x++ {
			cnt[x] += cnt[x-1]
		}
		out := make([]kv64, n)
		pos := cnt
		for i := range kvs {
			x := kvs[i].sh[0] >> 4
			out[pos[x]] = kvs[i]
			pos[x]++
		}
		a.kvs = out
		for x := range 16 {
			if cnt[x+1] > cnt[x] {
				a.segs = append(a.segs, [2]int{cnt[x], cnt[x+1]})
			}
		}
	})
	t1 := time.Now()
	st.Gather = t1.Sub(st.Start)
	type seg struct{ a, lo, hi int }
	var segs []seg
	for i := range as {
		for _, s := range as[i].segs {
			segs = append(segs, seg{i, s[0], s[1]})
		}
	}
	slices.SortFunc(segs, func(a, b seg) int { return (b.hi - b.lo) - (a.hi - a.lo) }) // big first
	runN(len(segs), g, func(i int) {
		s := segs[i]
		slices.SortFunc(as[s.a].kvs[s.lo:s.hi], func(a, b kv64) int { return bytes.Compare(a.sh[:], b.sh[:]) })
	})
	t2 := time.Now()
	st.Sort = t2.Sub(t1)
	fh, err := os.Create(path)
	if err != nil {
		return nil, st, err
	}
	bw := bufio.NewWriterSize(fh, 4<<20)
	table := make([]byte, 0, len(as)*l1AcctSize)
	off := 0
	for i := range as {
		a := &as[i]
		for j := range a.kvs {
			bw.Write(a.kvs[j].sh[:])
			bw.Write(a.kvs[j].v[:])
		}
		var flags uint8
		var x ovAcct
		if r, ok := f.accts[a.addr]; ok {
			flags |= l1HasAcct
			x = r
			if r.gone {
				flags |= l1Gone
				x = ovAcct{}
			}
			if x.hasCode {
				flags |= l1HasCode
			}
			if x.multicoin {
				flags |= l1Multi
			}
		}
		if f.wiped[a.addr] {
			flags |= l1Wiped
		}
		table = append(table, a.addr[:]...)
		table = append(table, a.ah[:]...)
		table = append(table, flags)
		table = binary.LittleEndian.AppendUint64(table, x.nonce)
		table = append(table, x.balance[:]...)
		table = append(table, x.code[:]...)
		table = binary.LittleEndian.AppendUint64(table, uint64(off))
		table = binary.LittleEndian.AppendUint64(table, uint64(len(a.kvs)))
		off += len(a.kvs)
		a.kvs = nil // the copy goes as it is written
	}
	bw.Write(table)
	var ft [l1Footer]byte
	binary.LittleEndian.PutUint64(ft[:], uint64(len(as)))
	binary.LittleEndian.PutUint64(ft[8:], uint64(off)*l1Rec)
	binary.LittleEndian.PutUint64(ft[16:], uint64(off))
	copy(ft[24:], l1Magic)
	bw.Write(ft[:])
	if err := bw.Flush(); err != nil {
		fh.Close()
		return nil, st, err
	}
	if err := fh.Close(); err != nil { // no fsync: the log of write sets is the crash recovery
		return nil, st, err
	}
	st.Accounts, st.Keys = len(as), off
	l, err := openL1(path)
	if err != nil {
		return nil, st, err
	}
	st.Bytes = int64(len(l.mm))
	st.End = time.Now()
	st.Write = st.End.Sub(t2)
	return l, st, nil
}

// Swap is the hourly fold (LiveOverlay only, between blocks): the live overlay is frozen and a
// fresh one takes the next writes; g side goroutines (not the trie's workers) drain the frozen
// one sorted to a level 1 file at path. Reads see the frozen overlay until the file is written,
// then (at the next ApplyBlock) the file in its place, and the frozen overlay is dropped. A drain
// still running from the last Swap is waited for first (waited: how long).
func (t *Top) Swap(path string, g int) (waited time.Duration, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	defer t.changing()()
	e := t.E
	if !t.LiveOverlay || e.src == nil {
		return 0, errors.New("trie: Swap needs LiveOverlay and a Settled source")
	}
	if e.drainC != nil {
		t0 := time.Now()
		d := <-e.drainC
		waited = time.Since(t0)
		d.st.WaitedBySwap = waited
		if err := e.install(d, 0); err != nil {
			return waited, err
		}
	}
	fr := e.fw
	e.frozen = &fr
	e.fw = newFileWrites()
	c := make(chan drainDone, 1)
	e.drainC = c
	go func() {
		l, st, err := drainTo(&fr, path, g)
		c <- drainDone{l, st, err}
	}()
	return waited, nil
}

// install puts a finished drain's file in place of the frozen overlay (before block n; 0: at a
// Swap or WaitDrain). A failed drain keeps the frozen overlay (reads stay right) and is returned.
func (e *Engine) install(d drainDone, n uint64) error {
	e.drainC = nil
	if d.err != nil {
		return fmt.Errorf("trie: drain to %s: %w", d.st.Path, d.err)
	}
	d.st.Installed = n
	e.l1 = append([]*l1File{d.l}, e.l1...)
	e.frozen = nil
	e.Drains = append(e.Drains, d.st)
	return nil
}

// pollDrain installs a finished drain before block n (ApplyBlock, under t.mu).
func (e *Engine) pollDrain(n uint64) error {
	if e.drainC == nil {
		return nil
	}
	select {
	case d := <-e.drainC:
		return e.install(d, n)
	default:
		return nil
	}
}

// WaitDrain waits for a running drain and installs its file.
func (t *Top) WaitDrain() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	defer t.changing()()
	if t.E.drainC == nil {
		return nil
	}
	return t.E.install(<-t.E.drainC, 0)
}

// Layers is the state of the layers above the store: live slot keys, frozen slot keys (-1:
// none), level 1 files.
func (t *Top) Layers() (live, frozen, l1 int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	frozen = -1
	if f := t.E.frozen; f != nil {
		frozen = f.nslots
	}
	return t.E.fw.nslots, frozen, len(t.E.l1)
}

// UpperValue is the store's read of the layers above it (store.State.SetUpper): the value of one
// key in tier0 write form; found false: no layer holds it (the store's overlay or the base
// does). It waits for an ApplyBlock running (the live overlay is written in place).
func (t *Top) UpperValue(addr [20]byte, kind byte, slot [32]byte) (v [32]byte, found bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if kind == tier0.KindSlot {
		sh := NewKeccaker().Sum(slot[:])
		return t.E.slotAbove(&addr, &sh)
	}
	a, exists, ok := t.E.acctAbove(&addr)
	if !ok {
		return v, false
	}
	if !exists {
		return v, true // absent: every field 0, code hash 0
	}
	switch kind {
	case tier0.KindBalance:
		return a.balance, true
	case tier0.KindNonce:
		return u64Word(a.nonce), true
	case tier0.KindMulti:
		if a.multicoin {
			v[31] = 1
		}
		return v, true
	}
	if a.hasCode {
		return a.code, true
	}
	return EmptyCode, true
}

// UpperSlot is UpperValue of a slot by sh, the keccak of its state key (the caller hashed it),
// without a lock: it reads the published layers and reads again when Swap, a drain's install or
// a tier0's end changed them meanwhile. It must not run during an ApplyBlock (Top.lay).
func (t *Top) UpperSlot(addr [20]byte, sh *[32]byte) (v [32]byte, found bool) {
	for {
		v0 := t.ver.Load()
		if v0&1 != 0 {
			runtime.Gosched()
			continue
		}
		l := t.lay.Load()
		if l == nil { // nothing published yet: the locked read
			t.mu.RLock()
			v, found = t.E.slotAbove(&addr, sh)
			t.mu.RUnlock()
			return v, found
		}
		v, found = slotIn(&l.fw, l.frozen, l.l1, &addr, sh)
		if t.ver.Load() == v0 {
			return v, found
		}
	}
}

// UpperSlots is UpperSlot of the keys shs[i] not done[i] under one read lock (store.Upper).
func (t *Top) UpperSlots(addr [20]byte, shs, vals [][32]byte, done []bool) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	n := 0
	for i := range shs {
		if !done[i] {
			if v, ok := t.E.slotAbove(&addr, &shs[i]); ok {
				vals[i], done[i] = v, true
				n++
			}
		}
	}
	return n
}

// closeL1 unmaps the level 1 files (Top.Close).
func (e *Engine) closeL1() error {
	var err error
	for _, l := range e.l1 {
		if x := l.close(); x != nil && err == nil {
			err = x
		}
	}
	return err
}
