package store

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"runtime"
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/asmevm"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/tier0"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/trie/keccaksimd"
)

// State is the base under the overlay of every write since the base: the value of every key
// the cut tier0 files wrote, and the code of every code hash they set. It implements
// asmevm.Loader (the executor's view of the state below its own run). Safe for concurrent use.
//
// One overlay serves two views, so the trie side needs no copy of its own: the executor reads
// every applied file (Apply), the trie side reads the settled view (Settled*): the files it has
// applied (Settle), the state its next file starts from. File i applied since New is cut i. The
// overlay keeps versions of a key (its value as of the end of a cut that wrote it): the newest
// (the executor's), every version of the newest pendKeep pending cuts, and the newest settled
// one. In the steady state (the trie side a file or two behind) that is about one version per
// key. A resume applies a whole window before the trie side settles any of it and still holds
// about one version per key: Settle reads a file older than the newest pendKeep again to put
// its versions back as the settled ones.
//
// Layout (no per-key Go map): per account written since the base, its account record versions
// and its slot versions in two sorted runs, main and recent (the newest versions, merged into
// main once it holds a quarter of main's entries). A run is fixed 40-byte records sorted by
// keccak(state key), then cut descending, values without leading zero bytes (inline up to 4
// bytes, else in the run's value bytes), and a bucket index on the keccak's top bits (keys are
// uniform: about 4 records per bucket). Slots are found by keccak(state key), the base's own
// key, so a miss hashes once for both.
//
// Apply and Settle run one at a time; they build the new runs without blocking readers and
// publish them, with the new view bound, in one short write-locked step.
type State struct {
	b     Base
	wmu   sync.Mutex   // Apply, Settle and SetBase, one at a time
	bmu   sync.RWMutex // b and the handles' base handles: readers of the base hold it across the read (taken before mu)
	mu    sync.RWMutex // readers; Apply and Settle hold it only to publish
	lanes [16]lane     // the Settled*Lane readers' locks; a writer of accts or set holds mu and all of them (lockAll)
	accts map[[20]byte]*acctOv
	n     int        // files applied: the executor view reads versions of cuts < n
	set   int        // files settled: the settled view reads versions of cuts < set
	pend  []pendFile // applied, not settled; oldest first
	codes map[[32]byte][]byte
	hs    []handle // storage handle h is hs[h-1]
	hOf   map[[20]byte]uint32
	Txs   int       // txs applied
	sc    slotCache // Slot's answers in the executor view (slotcache.go)
	ac    acctCache // Account's answers in the executor view (acctcache.go)
	nAcct int       // accounts with an account record version (Accounts), kept at every publish
	nRec  int       // slot version records in all runs, kept at every publish
	up    Upper     // above the overlay for Value (SetUpper)
	// SlotValue's lock-free read: rd is the view as of the last change of the overlay or the
	// base, ver a sequence count, odd while Apply, Settle or SetBase changes them; a read that
	// saw it change reads again.
	rd  atomic.Pointer[readView]
	ver atomic.Uint64
}

// readView is what SlotValue reads without a lock: the base, the executor view, and per address
// read since the view was published its storage as of then.
type readView struct {
	b    Base
	n    int
	ents sync.Map // [20]byte -> *readEnt
}

// readEnt is one address's storage in a readView.
type readEnt struct {
	h    uint32 // 0: no storage (every slot 0)
	ov   bool   // the overlay holds the account: vers, runs
	vers []acctVer
	runs *runs
	bh   uint32
	hk   handleKeyer // nil: the base reads by KeyAt
	bk   base.Handle
}

// handleKeyer is a Base whose storage handle records can be read without a lock (base.Base).
type handleKeyer interface {
	Handle(h uint32) (base.Handle, error)
	KeyIn(h base.Handle, sh *[32]byte) ([32]byte, error)
}

// changing marks a change of the overlay or the base (ver odd) until the returned func
// publishes a fresh readView and ends it (ver even). s.wmu held.
func (s *State) changing() func() {
	s.ver.Add(1)
	return func() {
		s.mu.RLock()
		s.rd.Store(&readView{b: s.b, n: s.n})
		s.mu.RUnlock()
		s.ver.Add(1)
	}
}

// pendFile is an applied file the trie side has not settled yet.
type pendFile struct {
	first   uint64
	blocks  int
	touched []*acctOv // accounts it wrote
}

// acctOv is one account's overlay. Its fields are replaced, never changed in place, under mu.
type acctOv struct {
	ah   [32]byte
	vers []acctVer // account record versions, newest first
	runs *runs     // nil: no slot versions
}

// runs are an account's slot versions.
type runs struct {
	recent run // newer files' versions, merged into main at a quarter of its size
	main   run
}

// acctVer is an account record as of the end of cut `cut` (full, not a diff).
type acctVer struct {
	cut       uint16
	del       uint16 // storage starts at this cut: slot versions of older cuts are gone (with wiped)
	multicoin bool   // the sticky coreth flag
	wiped     bool   // deleted since the base: its base storage is gone (a new account starts empty)
	nonce     uint64
	balance   [32]byte
	codeHash  [32]byte // 0: absent; keccak256(""): no code
}

// Run record layout: keccak(state key) [0:32], cut u16 LE [32:34], value length [34], [35]
// unused, [36:40] the value when at most 4 bytes, else its offset in vals (u32 LE).
const (
	recSize   = 40
	inlineMax = 4
	bucketRun = 16 // runs of more records get a bucket index
)

// run is a sorted list of slot versions (keccak ascending, cut descending).
type run struct {
	rec   []byte
	vals  []byte
	idx   []uint32 // idx[b]: the first record whose keccak's top bits are >= b (len 2^bits+1)
	shift uint8    // bucket of a keccak = its first 8 bytes >> shift
	top   int      // the newest cut in the run (0 when empty)
	multi int      // records that are not the first kept version of their key (what a prune may drop)
}

type handle struct {
	addr [20]byte
	bh   uint32  // base handle (0: no base slots)
	ov   *acctOv // the account's overlay (nil: none yet)
}

var emptyCode = base.Keccak(nil)

// maxCuts bounds the files one State applies (cut numbers are 16 bits; a fold replaces the
// State every 256 files).
const maxCuts = 1<<16 - 1

// pendKeep is how many of the newest pending files keep every version (see State).
const pendKeep = 8

// Base is what the overlay reads below it: the snapshot base (*base.Base), or a fold's view of
// it while the fold punches it behind its stream (fold.View). Handles are the Base's own.
type Base interface {
	Account(addr [20]byte) (base.Account, bool, error)
	KeyAt(h uint32, sh [32]byte) ([32]byte, error)
	SlotKeys(h uint32, f func(sh [32]byte) bool) error
	Code(ch [32]byte) ([]byte, error)
}

// New is the base with an empty overlay.
func New(b Base) *State {
	s := &State{b: b, accts: map[[20]byte]*acctOv{}, codes: map[[32]byte][]byte{}, hOf: map[[20]byte]uint32{}}
	s.rd.Store(&readView{b: b})
	return s
}

// Base is the base under the overlay.
func (s *State) Base() Base {
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	return s.b
}

// SetBase puts b under the overlay in place of the current base. b must give, for every key the
// overlay does not hold in a view, the value the current base gives: the same state (a fold's
// view of the base it punches), or the current base with every file applied so far folded in
// once the trie side has settled them all (the fold's new base: every key those files wrote is in
// the overlay). The executor's handles keep their numbers; their base handles are read again from
// b. When SetBase returns no reader uses the old base any more (the caller may close it).
func (s *State) SetBase(b Base) error {
	s.wmu.Lock() // Apply and Settle read the base while they plan
	defer s.wmu.Unlock()
	defer s.changing()()
	resolve := func(hd handle) (uint32, error) {
		if hd.bh == 0 { // no base storage, or deleted since the base: stays so (the overlay holds the rest)
			return 0, nil
		}
		a, ok, err := b.Account(hd.addr)
		if err != nil || !ok {
			return 0, err
		}
		return a.Handle, nil
	}
	s.mu.RLock()
	hs := slices.Clone(s.hs)
	s.mu.RUnlock()
	bhs := make([]uint32, len(hs))
	for i, hd := range hs { // without locks: readers go on meanwhile
		var err error
		if bhs[i], err = resolve(hd); err != nil {
			return err
		}
	}
	s.bmu.Lock()
	defer s.bmu.Unlock()
	s.lockAll()
	defer s.unlockAll()
	for i := len(hs); i < len(s.hs); i++ { // made since the copy (handle holds bmu: they are from the old base)
		bh, err := resolve(s.hs[i])
		if err != nil {
			return err
		}
		bhs = append(bhs, bh)
	}
	for i := range s.hs {
		if s.hs[i].bh != 0 { // a deletion since the copy zeroed it: Apply cannot run here (wmu)
			s.hs[i].bh = bhs[i]
		}
	}
	s.b = b
	s.sc.gen.Add(1) // the same answers by contract; a new base starts with an empty cache all the same
	s.ac.gen.Add(1)
	return nil
}

func (r *run) n() int             { return len(r.rec) / recSize }
func (r *run) sh(i int) []byte    { return r.rec[i*recSize : i*recSize+32] }
func (r *run) cutAt(i int) int    { return int(binary.LittleEndian.Uint16(r.rec[i*recSize+32:])) }
func (r *run) key(i int) [32]byte { return [32]byte(r.rec[i*recSize : i*recSize+32]) }

func (r *run) raw(i int) []byte {
	o := i * recSize
	l := int(r.rec[o+34])
	if l <= inlineMax {
		return r.rec[o+36 : o+36+l]
	}
	off := binary.LittleEndian.Uint32(r.rec[o+36:])
	return r.vals[off : off+uint32(l)]
}

func (r *run) val(i int) (v [32]byte) {
	b := r.raw(i)
	copy(v[32-len(b):], b)
	return v
}

func trim(v *[32]byte) []byte {
	i := 0
	for i < 32 && v[i] == 0 {
		i++
	}
	return v[i:]
}

// builder writes a run of n records with nv value bytes (values over inlineMax).
type builder struct{ r run }

func newBuilder(n, nv int) *builder {
	return &builder{run{rec: make([]byte, 0, n*recSize), vals: make([]byte, 0, nv)}}
}

func (b *builder) add(sh []byte, cut int, v []byte, multi bool) {
	var rec [recSize]byte
	copy(rec[:32], sh)
	binary.LittleEndian.PutUint16(rec[32:], uint16(cut))
	rec[34] = byte(len(v))
	if len(v) <= inlineMax {
		copy(rec[36:], v)
	} else {
		binary.LittleEndian.PutUint32(rec[36:], uint32(len(b.r.vals)))
		b.r.vals = append(b.r.vals, v...)
	}
	b.r.rec = append(b.r.rec, rec[:]...)
	b.r.top = max(b.r.top, cut)
	if multi {
		b.r.multi++
	}
}

// finish indexes the run: about 4 records per bucket.
func (b *builder) finish() run {
	r := b.r
	n := r.n()
	if n <= bucketRun {
		return r
	}
	nb := bits.Len(uint(n / 4))
	r.shift = uint8(64 - nb)
	r.idx = make([]uint32, 1<<nb+1)
	j := 0
	for i := range n {
		for bk := int(binary.BigEndian.Uint64(r.rec[i*recSize:]) >> r.shift); j <= bk; j++ {
			r.idx[j] = uint32(i)
		}
	}
	for ; j < len(r.idx); j++ {
		r.idx[j] = uint32(n)
	}
	return r
}

// lower is the first record whose keccak is >= k.
func (r *run) lower(k *[32]byte) int {
	lo, hi := 0, r.n()
	if r.idx != nil {
		b := binary.BigEndian.Uint64(k[:8]) >> r.shift
		lo, hi = int(r.idx[b]), int(r.idx[b+1])
	}
	for lo < hi && bytes.Compare(r.sh(lo), k[:]) < 0 {
		lo++
	}
	return lo
}

// newest is the newest version of key sh of a cut below view in r: its record and cut (cut -1:
// none).
func (r *run) newest(sh *[32]byte, view int) (int, int) {
	for i := r.lower(sh); i < r.n() && bytes.Equal(r.sh(i), sh[:]); i++ {
		if c := r.cutAt(i); c < view {
			return i, c
		}
	}
	return 0, -1
}

// slotAt is the value of key sh in rs in the view (versions of cuts below view, the account's
// storage starting at cut del); ok false: the overlay does not hold it in this view.
func (rs *runs) slotAt(sh *[32]byte, view, del int) (v [32]byte, ok bool) {
	if rs == nil {
		return v, false
	}
	r := &rs.recent
	i, c := r.newest(sh, view)
	if c <= rs.main.top { // main may hold a newer version (Settle puts older cuts back in recent)
		if j, d := rs.main.newest(sh, view); d > c {
			r, i, c = &rs.main, j, d
		}
	}
	if c < 0 || c < del {
		return v, false
	}
	return r.val(i), true
}

// at is the account record in the view (nil: the overlay holds none).
func at(vers []acctVer, view int) *acctVer {
	for i := range vers {
		if int(vers[i].cut) < view {
			return &vers[i]
		}
	}
	return nil
}

// delAt is where the account's storage starts in the view and whether its base storage is gone.
func delAt(vers []acctVer, view int) (int, bool) {
	if a := at(vers, view); a != nil {
		return int(a.del), a.wiped
	}
	return 0, false
}

// keep is which versions of a key the overlay keeps: the newest (the executor's), every version
// of a pending cut >= from (the newest pendKeep files), and the newest of a settled cut (< set)
// unless it is older than del, where the account's storage starts in the settled view.
type keep struct{ set, from, del int }

func (k keep) walk(cuts []int, emit func(x int)) {
	below := false
	for x, c := range cuts {
		if c < k.set {
			if below || c < k.del {
				continue
			}
			below = true
		} else if x > 0 && c < k.from {
			continue
		}
		emit(x)
	}
}

// merge is the run of a's and b's versions that k keeps (a version in both, same key and cut,
// once). Sized exactly in a counting pass: a run never carries spare capacity.
func merge(a, b *run, k keep) run {
	type src struct {
		r *run
		i int
	}
	var grp []src
	var cuts []int
	walk := func(emit func(r *run, i int, multi bool)) {
		i, j, na, nb := 0, 0, a.n(), b.n()
		for i < na || j < nb {
			var key [32]byte
			switch {
			case i == na:
				key = b.key(j)
			case j == nb || bytes.Compare(a.sh(i), b.sh(j)) < 0:
				key = a.key(i)
			default:
				key = b.key(j)
			}
			grp, cuts = grp[:0], cuts[:0]
			for {
				inA, inB := i < na && bytes.Equal(a.sh(i), key[:]), j < nb && bytes.Equal(b.sh(j), key[:])
				if !inA && !inB {
					break
				}
				x := src{a, i}
				if !inA || inB && b.cutAt(j) > a.cutAt(i) {
					x = src{b, j}
					j++
				} else {
					i++
				}
				if c := x.r.cutAt(x.i); len(cuts) == 0 || cuts[len(cuts)-1] != c {
					grp, cuts = append(grp, x), append(cuts, c)
				}
			}
			n := 0
			k.walk(cuts, func(x int) { emit(grp[x].r, grp[x].i, n > 0); n++ })
		}
	}
	n, nv := 0, 0
	walk(func(r *run, i int, _ bool) {
		n++
		if l := len(r.raw(i)); l > inlineMax {
			nv += l
		}
	})
	if n == 0 {
		return run{}
	}
	bd := newBuilder(n, nv)
	walk(func(r *run, i int, multi bool) { bd.add(r.sh(i), r.cutAt(i), r.raw(i), multi) })
	return bd.finish()
}

// pruneVers keeps the account versions k keeps (vs newest first; del does not apply: records
// are whole).
func pruneVers(vs []acctVer, k keep) []acctVer {
	cuts := make([]int, len(vs))
	for i := range vs {
		cuts[i] = int(vs[i].cut)
	}
	k.del = 0
	n := 0
	k.walk(cuts, func(int) { n++ })
	if n == len(vs) {
		return vs
	}
	out := make([]acctVer, 0, n)
	k.walk(cuts, func(x int) { out = append(out, vs[x]) })
	return out
}

// fileAcct is what one file wrote to one account: the last value of each account field it
// wrote, whether it deleted the account, and the final value of each slot (writes before the
// file's last deletion of the account dropped, as the storage went with it).
type fileAcct struct {
	addr                 [20]byte
	ah                   [32]byte
	fields               uint8 // bit k: field kind k written
	deleted              bool
	nonce                uint64
	balance, code, multi [32]byte
	slots                map[[32]byte][32]byte
	run                  run // the slots, all of cut 0 until setCut
}

// setCut stamps every record of r with cut c.
func (r *run) setCut(c int) {
	for i := range r.n() {
		binary.LittleEndian.PutUint16(r.rec[i*recSize+32:], uint16(c))
	}
	r.top = c
}

// decode is what f wrote, per account, and its codes. The blocks are decoded in parallel ranges
// (combined in block order: a later range's deletion drops the earlier ranges' slots of the
// account), then each account's slots are hashed and sorted in parallel.
func decode(f *tier0.File) ([]*fileAcct, map[[32]byte][]byte, error) {
	w := max(1, min(runtime.GOMAXPROCS(0), 8, f.Blocks/64))
	type part struct {
		byAddr map[[20]byte]*fileAcct
		order  [][20]byte // first-write order, so the combined list does not depend on map order
		codes  map[[32]byte][]byte
		err    error
	}
	parts := make([]part, w)
	var wg sync.WaitGroup
	for p := range parts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pt := &parts[p]
			pt.byAddr, pt.codes = map[[20]byte]*fileAcct{}, map[[32]byte][]byte{}
			q := f.Seq() // a sequential reader per goroutine
			for i := f.Blocks * p / w; i < f.Blocks*(p+1)/w; i++ {
				groups, err := q.Writes(i)
				if err != nil {
					pt.err = err
					return
				}
				for _, g := range groups {
					for _, w := range g {
						fa := pt.byAddr[w.Addr]
						if fa == nil {
							fa = &fileAcct{addr: w.Addr}
							pt.byAddr[w.Addr] = fa
							pt.order = append(pt.order, w.Addr)
						}
						switch w.Kind {
						case tier0.KindSlot:
							if fa.slots == nil {
								fa.slots = map[[32]byte][32]byte{}
							}
							fa.slots[w.Slot] = w.New
							continue
						case tier0.KindBalance:
							fa.balance = w.New
						case tier0.KindNonce:
							fa.nonce = uint256.NewInt(0).SetBytes32(w.New[:]).Uint64()
						case tier0.KindCode:
							fa.code = w.New
							if w.New == ([32]byte{}) { // deletion: the account's storage goes with it (legacy SELFDESTRUCT, EIP-161)
								fa.deleted = true
								fa.slots = nil
							}
						case tier0.KindMulti:
							fa.multi = w.New
						}
						fa.fields |= 1 << w.Kind
					}
				}
				cs, err := q.Codes(i)
				if err != nil {
					pt.err = err
					return
				}
				for _, c := range cs {
					pt.codes[base.Keccak(c)] = c
				}
			}
		}()
	}
	wg.Wait()
	byAddr, codes := parts[0].byAddr, parts[0].codes
	fas := make([]*fileAcct, 0, len(byAddr))
	for p := range parts {
		pt := &parts[p]
		if pt.err != nil {
			return nil, nil, pt.err
		}
		for _, addr := range pt.order {
			fa := pt.byAddr[addr]
			dst := byAddr[addr]
			if p == 0 || dst == nil {
				byAddr[addr] = fa
				fas = append(fas, fa)
				continue
			}
			if fa.deleted {
				dst.deleted, dst.slots = true, nil
			}
			for k, v := range fa.slots {
				if dst.slots == nil {
					dst.slots = map[[32]byte][32]byte{}
				}
				dst.slots[k] = v
			}
			if fa.fields&(1<<tier0.KindBalance) != 0 {
				dst.balance = fa.balance
			}
			if fa.fields&(1<<tier0.KindNonce) != 0 {
				dst.nonce = fa.nonce
			}
			if fa.fields&(1<<tier0.KindCode) != 0 {
				dst.code = fa.code
			}
			if fa.fields&(1<<tier0.KindMulti) != 0 {
				dst.multi = fa.multi
			}
			dst.fields |= fa.fields
		}
		if p > 0 {
			for h, c := range pt.codes {
				codes[h] = c
			}
		}
	}
	var next atomic.Int64
	for range w {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int(next.Add(1)) - 1; i < len(fas); i = int(next.Add(1)) - 1 {
				fa := fas[i]
				fa.ah = base.Keccak(fa.addr[:])
				if len(fa.slots) == 0 {
					fa.slots = nil
					continue
				}
				kvs, nv := slotKVs(fa.slots)
				bd := newBuilder(len(kvs), nv)
				for i := range kvs {
					bd.add(kvs[i].sh[:], 0, trim(&kvs[i].v), false)
				}
				fa.run = bd.finish()
				fa.slots = nil
			}
		}()
	}
	wg.Wait()
	return fas, codes, nil
}

// baseAcct is addr's account in the base, as an overlay record.
func (s *State) baseAcct(addr [20]byte) (acctVer, uint32, error) {
	a, ok, err := s.b.Account(addr)
	if err != nil || !ok {
		return acctVer{}, 0, err
	}
	ch := a.CodeHash
	if ch == ([32]byte{}) {
		ch = emptyCode
	}
	return acctVer{nonce: a.Nonce, balance: a.Balance, codeHash: ch, multicoin: a.MultiCoin}, a.Handle, nil
}

// upd is an account's next overlay, published by Apply or Settle.
type upd struct {
	ov      *acctOv
	addr    [20]byte
	isNew   bool
	deleted bool // Apply: the file deleted the account (its executor handle loses its base storage)
	vers    []acctVer
	runs    *runs
}

// plan is ov's overlay (nil: none yet) with what fa says cut c wrote (the account record as of
// the end of c: the record in view c with c's fields; the slot versions of c; a version already
// there stays), keeping what keep{set, from} keeps. Only Apply and Settle call it (s.wmu held):
// it reads ov without mu and changes nothing.
func (s *State) plan(ov *acctOv, fa *fileAcct, c, set, from int) (upd, error) {
	u := upd{ov: ov, addr: fa.addr, deleted: fa.deleted}
	if ov == nil {
		u.ov, u.isNew = &acctOv{ah: fa.ah}, true
	}
	u.vers, u.runs = u.ov.vers, u.ov.runs
	if fa.fields != 0 {
		i := 0
		for i < len(u.vers) && int(u.vers[i].cut) > c {
			i++
		}
		vs := u.vers
		if i == len(vs) || int(vs[i].cut) != c {
			var v acctVer
			if i < len(vs) {
				v = vs[i]
			} else {
				var err error
				if v, _, err = s.baseAcct(fa.addr); err != nil {
					return upd{}, err
				}
			}
			v.cut = uint16(c)
			if fa.fields&(1<<tier0.KindBalance) != 0 {
				v.balance = fa.balance
			}
			if fa.fields&(1<<tier0.KindNonce) != 0 {
				v.nonce = fa.nonce
			}
			if fa.fields&(1<<tier0.KindCode) != 0 {
				v.codeHash = fa.code
			}
			if fa.fields&(1<<tier0.KindMulti) != 0 {
				v.multicoin = fa.multi[31] == 1
			}
			if fa.deleted {
				v.wiped, v.del = true, uint16(c)
			}
			vs = slices.Insert(slices.Clone(vs), i, v)
		}
		u.vers = pruneVers(vs, keep{set: set, from: from})
	}
	if fa.run.n() > 0 {
		fa.run.setCut(c)
		var rs runs
		if u.runs != nil {
			rs = *u.runs
		}
		del, _ := delAt(u.vers, set)
		k := keep{set: set, from: from, del: del}
		rs.recent = mergePar(&rs.recent, &fa.run, k)
		if rs.recent.n()*4 >= rs.main.n() {
			rs.main = mergePar(&rs.main, &rs.recent, k)
			rs.recent = run{}
		}
		u.runs = &rs
	}
	return u, nil
}

// prune drops from u what keep{set, from} no longer keeps: account versions, the recent run's
// superseded versions, main's when they are an eighth of it, and every slot version older than
// a deletion in cut c (now settled).
func prune(u *upd, c, set, from int) {
	u.vers = pruneVers(u.vers, keep{set: set, from: from})
	if u.runs == nil {
		return
	}
	a := at(u.vers, set)
	del := 0
	if a != nil {
		del = int(a.del)
	}
	deleted := a != nil && a.wiped && int(a.cut) == c && del == c
	k := keep{set: set, from: from, del: del}
	rs := *u.runs
	if rs.recent.multi > 0 || deleted {
		rs.recent = merge(&rs.recent, &run{}, k)
	}
	if rs.main.multi*8 > rs.main.n() || deleted {
		rs.main = merge(&rs.main, &run{}, k)
	}
	if rs.recent.n()+rs.main.n() == 0 {
		u.runs = nil
		return
	}
	u.runs = &rs
}

// size is what one account's overlay adds to the counts Size reports.
func size(vers []acctVer, rs *runs) (acct, recs int) {
	if len(vers) > 0 {
		acct = 1
	}
	if rs != nil {
		recs = rs.recent.n() + rs.main.n()
	}
	return acct, recs
}

// replace replaces ov's versions and runs (s.mu held), keeping the counts.
func (s *State) replace(ov *acctOv, vers []acctVer, rs *runs) {
	a0, r0 := size(ov.vers, ov.runs)
	a1, r1 := size(vers, rs)
	s.nAcct += a1 - a0
	s.nRec += r1 - r0
	ov.vers, ov.runs = vers, rs
}

// publish makes the updates visible (s.mu held).
func (s *State) publish(ups []upd) {
	for i := range ups {
		u := &ups[i]
		if u.isNew {
			s.accts[u.addr] = u.ov
		}
		if h, ok := s.hOf[u.addr]; ok {
			if u.isNew {
				s.hs[h-1].ov = u.ov
			}
			if u.deleted {
				s.hs[h-1].bh = 0
			}
		}
		s.replace(u.ov, u.vers, u.runs)
	}
}

// planChunk is how many accounts one goroutine of planAll takes at a time.
var planChunk = 256

// planAll plans every account of a file on up to 8 goroutines (s.wmu held). One account's plan
// reads its own overlay and the base and changes nothing, so the plans are independent; the
// first base error is returned. Apply runs on the executor thread at every cut: late in a
// window a file touches ~100k accounts, each a merge of its runs and, if new, a base read.
func (s *State) planAll(fas []*fileAcct, c, set, from int) ([]upd, error) {
	ups := make([]upd, len(fas))
	w := min(8, runtime.GOMAXPROCS(0), (len(fas)+planChunk-1)/planChunk)
	var next atomic.Int64
	var first atomic.Pointer[error]
	work := func() {
		for first.Load() == nil {
			lo := int(next.Add(int64(planChunk))) - planChunk
			if lo >= len(fas) {
				return
			}
			for i := lo; i < min(lo+planChunk, len(fas)); i++ {
				var err error
				if ups[i], err = s.plan(s.accts[fas[i].addr], fas[i], c, set, from); err != nil {
					first.CompareAndSwap(nil, &err)
					return
				}
			}
		}
	}
	var wg sync.WaitGroup
	for range w - 1 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			work()
		}()
	}
	work()
	wg.Wait()
	if e := first.Load(); e != nil {
		return nil, *e
	}
	return ups, nil
}

// Apply adds a cut tier0 file to the overlay as the next cut: every write's new value and every
// code. Files are applied in chain order; the trie side settles them in the same order.
func (s *State) Apply(f *tier0.File) error {
	fas, codes, err := decode(f)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	defer s.changing()()
	if s.n >= maxCuts {
		return fmt.Errorf("store: %d files applied, the most one State holds", s.n)
	}
	c := s.n
	ups, err := s.planAll(fas, c, s.set, c+1-pendKeep)
	if err != nil {
		return err // the base failed: nothing is published, the caller stops
	}
	pf := pendFile{first: f.First, blocks: f.Blocks, touched: make([]*acctOv, len(fas))}
	for i := range ups {
		pf.touched[i] = ups[i].ov
	}
	s.lockAll()
	s.publish(ups)
	for h, code := range codes {
		s.codes[h] = code
	}
	s.pend = append(s.pend, pf)
	s.n++
	s.Txs += f.Txs
	s.sc.gen.Add(1) // after the publish: no older answer is served from here on
	s.ac.gen.Add(1)
	s.unlockAll()
	return nil
}

// Settle marks the oldest pending file, which must be f, settled: the trie side calls it once
// it applied f. A file older than the newest pendKeep is read again (its versions the overlay
// dropped come back as the settled ones); then the versions no view keeps any more go. No
// executor-view answer changes (its newest version always stays), so Slot's cache stays valid.
func (s *State) Settle(f *tier0.File) error {
	s.mu.RLock()
	reread := s.set < s.n-pendKeep
	s.mu.RUnlock()
	var fas []*fileAcct
	var err error
	if reread { // before wmu: an Apply waiting for it is not held up by the decode
		if fas, _, err = decode(f); err != nil {
			return err
		}
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	defer s.changing()()
	if len(s.pend) == 0 || s.pend[0].first != f.First || s.pend[0].blocks != f.Blocks {
		return fmt.Errorf("store: settle of blocks %d+%d is not the oldest pending file", f.First, f.Blocks)
	}
	c, from := s.set, s.n-pendKeep
	if !reread && c < from { // Applies came in between: c's dropped versions must be read again
		if fas, _, err = decode(f); err != nil {
			return err
		}
		reread = true
	}
	var ups []upd
	if reread {
		ups = make([]upd, len(fas))
		for i, fa := range fas {
			if ups[i], err = s.plan(s.accts[fa.addr], fa, c, c+1, from); err != nil {
				return err
			}
			ups[i].deleted = false // the executor's handle lost its base storage at Apply
			prune(&ups[i], c, c+1, from)
		}
	} else { // every version of c is still there (c has been within the newest pendKeep files)
		ups = make([]upd, len(s.pend[0].touched))
		for i, ov := range s.pend[0].touched {
			ups[i] = upd{ov: ov, vers: ov.vers, runs: ov.runs}
			prune(&ups[i], c, c+1, from)
		}
	}
	s.lockAll()
	for i := range ups {
		s.replace(ups[i].ov, ups[i].vers, ups[i].runs)
	}
	s.set++
	s.pend[0] = pendFile{}
	s.pend = s.pend[1:]
	s.ac.gen.Add(1) // values stay, but a handle can turn 0 (acctcache.go)
	s.unlockAll()
	return nil
}

// Pending is the number of applied files not settled yet.
func (s *State) Pending() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pend)
}

func (s *State) view(settled bool) int {
	if settled {
		return s.set
	}
	return s.n
}

// account is addr's account in a view with its base handle (0 when deleted since the base);
// s.bmu read-held.
func (s *State) account(addr [20]byte, settled bool) (acctVer, uint32, error) {
	s.mu.RLock()
	var a *acctVer
	if ov := s.accts[addr]; ov != nil {
		a = at(ov.vers, s.view(settled))
	}
	var v acctVer
	if a != nil {
		v = *a
	}
	s.mu.RUnlock()
	ba, bh, err := s.baseAcct(addr)
	if err != nil {
		return acctVer{}, 0, err
	}
	if a == nil {
		return ba, bh, nil
	}
	if v.wiped {
		bh = 0
	}
	return v, bh, nil
}

// slot is the value of the state key with keccak sh of addr in a view: the overlay, then the
// base handle bh (s.bmu read-held: bh is the current base's).
func (s *State) slot(addr [20]byte, sh [32]byte, bh uint32, settled bool) ([32]byte, error) {
	s.mu.RLock()
	if ov := s.accts[addr]; ov != nil {
		view := s.view(settled)
		del, wiped := delAt(ov.vers, view)
		v, ok := ov.runs.slotAt(&sh, view, del)
		s.mu.RUnlock()
		if ok || wiped {
			return v, nil
		}
	} else {
		s.mu.RUnlock()
	}
	if bh == 0 {
		return [32]byte{}, nil
	}
	return s.b.KeyAt(bh, sh)
}

// SettledValue is Value in the settled view (the state after the last settled file).
func (s *State) SettledValue(addr [20]byte, kind byte, slot [32]byte) ([32]byte, error) {
	return s.value(addr, kind, slot, true)
}

// SettledWiped reports whether addr was deleted since the base in the settled view.
func (s *State) SettledWiped(addr [20]byte) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ov := s.accts[addr]; ov != nil {
		_, wiped := delAt(ov.vers, s.set)
		return wiped
	}
	return false
}

// Code is the code with hash ch (overlay first, then the base).
func (s *State) Code(ch [32]byte) ([]byte, error) {
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	return s.code(ch)
}

func (s *State) code(ch [32]byte) ([]byte, error) {
	s.mu.RLock()
	c, ok := s.codes[ch]
	s.mu.RUnlock()
	if ok {
		return c, nil
	}
	c, err := s.b.Code(ch)
	// A copy: the base's bytes alias its mapping, which a fold punches behind its stream while
	// the executor keeps the codes it loaded (a tier0 at the tip lasts hours) and closes after
	// the switch. One copy per account load (the State caches it for the tier0).
	return bytes.Clone(c), err
}

// Account implements asmevm.Loader.
func (s *State) Account(addr asmevm.Address) (bool, uint64, uint256.Int, asmevm.Hash, []byte, uint32, error) {
	g := s.ac.gen.Load()
	if c, ok := s.ac.get(g, (*[20]byte)(&addr)); ok {
		return c.ok, c.nonce, c.balance, c.codeHash, c.code, c.h, nil
	}
	c, err := s.accountUncached(addr)
	if err != nil {
		return false, 0, uint256.Int{}, asmevm.Hash{}, nil, 0, err
	}
	s.ac.put(g, (*[20]byte)(&addr), c)
	return c.ok, c.nonce, c.balance, c.codeHash, c.code, c.h, nil
}

// accountUncached is Account (and the multicoin flag of the same read) without the cache.
func (s *State) accountUncached(addr asmevm.Address) (acctAns, error) {
	s.bmu.RLock() // bh below is the current base's until handle stores it
	defer s.bmu.RUnlock()
	a, bh, err := s.account(addr, false)
	if err != nil {
		return acctAns{}, err
	}
	if a.codeHash == ([32]byte{}) {
		return acctAns{multicoin: a.multicoin}, nil
	}
	var code []byte
	ch := asmevm.Hash{}
	if a.codeHash != emptyCode {
		c, err := s.code(a.codeHash)
		if err != nil {
			return acctAns{}, fmt.Errorf("store: code of %x: %w", addr, err)
		}
		code, ch = c, a.codeHash
	}
	s.mu.RLock()
	ov := s.accts[addr]
	hasSlots := ov != nil && ov.runs != nil
	h, known := s.hOf[addr]
	s.mu.RUnlock()
	if bh == 0 && !hasSlots {
		h = 0
	} else if !known { // only an account's first load takes the write lock (every replica and the committer load each account again at every cut)
		h = s.handle(addr, bh)
	}
	c := acctAns{ok: true, nonce: a.nonce, codeHash: ch, code: code, h: h, multicoin: a.multicoin}
	c.balance.SetBytes32(a.balance[:])
	return c, nil
}

func (s *State) handle(addr [20]byte, bh uint32) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.hOf[addr]; ok {
		return h
	}
	s.hs = append(s.hs, handle{addr, bh, s.accts[addr]})
	h := uint32(len(s.hs))
	s.hOf[addr] = h
	return h
}

// Slot implements asmevm.StorageBase: the value of slot (as the EVM sees it) of handle h.
func (s *State) Slot(h uint32, slot *[32]byte) uint256.Int {
	if v, ok := s.sc.get(s.sc.gen.Load(), h, slot); ok {
		return v
	}
	v, g := s.slotUncached(h, slot)
	s.sc.put(g, h, slot, v)
	return v
}

// slotUncached is Slot without the cache, and the cache generation of the view it read (taken
// under the same mu.RLock as the overlay lookup).
func (s *State) slotUncached(h uint32, slot *[32]byte) (uint256.Int, uint64) {
	k := base.Mask(*slot)
	sh := base.Keccak(k[:])
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	v, g, err := s.slotOf(h, &sh)
	if err != nil {
		panic(err) // asmevm's Chain reports a loader panic as the block's error
	}
	var r uint256.Int
	r.SetBytes32(v[:])
	return r, g
}

// slotOf is the value of the state key with keccak sh of handle h in the executor view, and the
// cache generation of the view it read; s.bmu read-held.
func (s *State) slotOf(h uint32, sh *[32]byte) ([32]byte, uint64, error) {
	s.mu.RLock()
	g := s.sc.gen.Load()
	hd := s.hs[h-1]
	if hd.ov != nil {
		del, wiped := delAt(hd.ov.vers, s.n)
		v, ok := hd.ov.runs.slotAt(sh, s.n, del)
		if ok || wiped {
			s.mu.RUnlock()
			return v, g, nil
		}
	}
	s.mu.RUnlock()
	if hd.bh == 0 {
		return [32]byte{}, g, nil
	}
	v, err := s.b.KeyAt(hd.bh, *sh)
	return v, g, err
}

// Upper is a layer above the overlay that holds writes the State is not given (the trie side's
// live overlay, its frozen one and its level 1 files: trie.Top.UpperValue with LiveOverlay).
type Upper interface {
	// UpperValue: found false when the layer does not hold the key
	UpperValue(addr [20]byte, kind byte, slot [32]byte) (v [32]byte, found bool)
	// UpperSlot is UpperValue of a slot by sh, the keccak of its state key
	UpperSlot(addr [20]byte, sh *[32]byte) (v [32]byte, found bool)
	// UpperSlots is UpperSlot of the keys shs[i] not done[i] (one lock for all): it sets vals[i]
	// and done[i] of each key it holds and returns their number
	UpperSlots(addr [20]byte, shs, vals [][32]byte, done []bool) int
}

// SetUpper puts u above the overlay for Value (before any concurrent reader). Only Value asks it:
// the executor-view readers (Slot, Account) and the Settled view do not.
func (s *State) SetUpper(u Upper) { s.up = u }

// Value is the current value of one key in tier0 write form (kind, slot) of addr: the Upper
// layer first when set.
func (s *State) Value(addr [20]byte, kind byte, slot [32]byte) ([32]byte, error) {
	if kind == tier0.KindSlot {
		return s.SlotValue(addr, base.Keccak(slot[:]))
	}
	if s.up != nil {
		if v, ok := s.up.UpperValue(addr, kind, slot); ok {
			return v, nil
		}
	}
	return s.value(addr, kind, slot, false)
}

// SlotValue is Value of a slot of addr by sh, the keccak of its state key (tier0 write form): one
// hash serves the Upper layer, the overlay and the base. An address read before takes its storage
// handle (the EVM's Slot path) instead of reading its account again.
//
// It takes no lock: it reads the readView published at the last change of the overlay or the
// base and reads again when Apply, Settle or SetBase changed them meanwhile (an address's first
// read in a view takes the locks once to find its storage). The Upper's UpperSlot is lock-free
// too (trie.Top: not during an ApplyBlock).
func (s *State) SlotValue(addr [20]byte, sh [32]byte) ([32]byte, error) {
	if s.up != nil {
		if v, ok := s.up.UpperSlot(addr, &sh); ok {
			return v, nil
		}
	}
	for {
		v0 := s.ver.Load()
		if v0&1 != 0 {
			runtime.Gosched()
			continue
		}
		rv := s.rd.Load()
		v, err := s.viewSlot(rv, addr, &sh)
		if s.ver.Load() == v0 {
			return v, err
		}
	}
}

// viewSlot is the slot sh of addr in view rv.
func (s *State) viewSlot(rv *readView, addr [20]byte, sh *[32]byte) ([32]byte, error) {
	var e *readEnt
	if x, ok := rv.ents.Load(addr); ok {
		e = x.(*readEnt)
	} else {
		var err error
		if e, err = s.readEntOf(addr); err != nil {
			return [32]byte{}, err
		}
		rv.ents.Store(addr, e) // a view newer than rv's: ver has moved, the caller reads again
	}
	if e.h == 0 {
		return [32]byte{}, nil
	}
	if e.ov {
		del, wiped := delAt(e.vers, rv.n)
		if v, ok := e.runs.slotAt(sh, rv.n, del); ok || wiped {
			return v, nil
		}
	}
	if e.bh == 0 {
		return [32]byte{}, nil
	}
	if e.hk != nil {
		return e.hk.KeyIn(e.bk, sh)
	}
	return rv.b.KeyAt(e.bh, *sh)
}

// readEntOf is addr's storage now, under the locks (its first read in a view).
func (s *State) readEntOf(addr [20]byte) (*readEnt, error) {
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	h, err := s.storageHandle(addr)
	if err != nil || h == 0 {
		return &readEnt{}, err
	}
	e := &readEnt{h: h}
	s.mu.RLock()
	hd := s.hs[h-1]
	if hd.ov != nil {
		e.ov, e.vers, e.runs = true, hd.ov.vers, hd.ov.runs
	}
	s.mu.RUnlock()
	e.bh = hd.bh
	if hk, ok := s.b.(handleKeyer); ok && hd.bh != 0 {
		if e.bk, err = hk.Handle(hd.bh); err != nil {
			return nil, err
		}
		e.hk = hk
	}
	return e, nil
}

// storageHandle is addr's storage handle in the executor view, made at its first read (its
// address hashed and its account read once); 0: no storage, no handle (as accountUncached).
// s.bmu read-held.
func (s *State) storageHandle(addr [20]byte) (uint32, error) {
	s.mu.RLock()
	h, known := s.hOf[addr]
	s.mu.RUnlock()
	if known {
		return h, nil
	}
	_, bh, err := s.account(addr, false)
	if err != nil {
		return 0, err
	}
	s.mu.RLock()
	ov := s.accts[addr]
	hasSlots := ov != nil && ov.runs != nil
	s.mu.RUnlock()
	if bh == 0 && !hasSlots {
		return 0, nil
	}
	return s.handle(addr, bh), nil
}

// SlotValues is SlotValue of many slots of addr (tier0 write form) in one call, for a parallel
// reader: addr's storage handle is found once, then g goroutines, each with a contiguous share,
// hash the share's keys in lanes (keccaksimd) and read them with each lock taken once per share
// (the Upper's, the overlay's, the base's handle), not per key. It sets shs[i] = keccak(slots[i])
// and vals[i] = its value.
func (s *State) SlotValues(addr [20]byte, slots, shs, vals [][32]byte, g int) error {
	n := len(slots)
	if len(shs) != n || len(vals) != n {
		return fmt.Errorf("store: SlotValues: %d slots, %d hashes, %d values", n, len(shs), len(vals))
	}
	if n == 0 {
		return nil
	}
	s.bmu.RLock()
	h, err := s.storageHandle(addr) // its number stays across a SetBase
	s.bmu.RUnlock()
	if err != nil {
		return err
	}
	g = max(1, min(g, n))
	errs := make([]error, g)
	var wg sync.WaitGroup
	for j := range g {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lo, hi := j*n/g, (j+1)*n/g
			errs[j] = s.slotShare(addr, h, slots[lo:hi], shs[lo:hi], vals[lo:hi])
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// keysAter is a Base that reads many keys of one handle at once (base.Base).
type keysAter interface {
	KeysAt(h uint32, shs, out [][32]byte, need []bool) error
}

// slotShare is one SlotValues goroutine's share. The Upper is asked before s.bmu is taken, as in
// SlotValue.
func (s *State) slotShare(addr [20]byte, h uint32, slots, shs, vals [][32]byte) error {
	msgs := make([][]byte, len(slots))
	for i := range msgs {
		msgs[i] = slots[i][:]
	}
	keccaksimd.HashBatch(shs, msgs, nil)
	done := make([]bool, len(slots))
	left := len(slots)
	if s.up != nil {
		left -= s.up.UpperSlots(addr, shs, vals, done)
	}
	if left == 0 {
		return nil
	}
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	if h == 0 { // no storage handle: the overlay alone (zero when it does not hold the key)
		for i := range shs {
			if !done[i] {
				v, err := s.slot(addr, shs[i], 0, false)
				if err != nil {
					return err
				}
				vals[i] = v
			}
		}
		return nil
	}
	s.mu.RLock()
	hd := s.hs[h-1]
	if hd.ov != nil {
		del, wiped := delAt(hd.ov.vers, s.n)
		for i := range shs {
			if !done[i] {
				if v, ok := hd.ov.runs.slotAt(&shs[i], s.n, del); ok || wiped {
					vals[i], done[i] = v, true
					left--
				}
			}
		}
	}
	s.mu.RUnlock()
	if left == 0 {
		return nil
	}
	if hd.bh == 0 {
		for i := range shs {
			if !done[i] {
				vals[i] = [32]byte{}
			}
		}
		return nil
	}
	need := done // the keys left, in place: done flips to need
	for i := range need {
		need[i] = !need[i]
	}
	if kb, ok := s.b.(keysAter); ok {
		return kb.KeysAt(hd.bh, shs, vals, need)
	}
	for i := range shs {
		if need[i] {
			v, err := s.b.KeyAt(hd.bh, shs[i])
			if err != nil {
				return err
			}
			vals[i] = v
		}
	}
	return nil
}

func (s *State) value(addr [20]byte, kind byte, slot [32]byte, settled bool) ([32]byte, error) {
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	a, bh, err := s.account(addr, settled)
	if err != nil {
		return [32]byte{}, err
	}
	switch kind {
	case tier0.KindSlot:
		return s.slot(addr, base.Keccak(slot[:]), bh, settled)
	case tier0.KindBalance:
		return a.balance, nil
	case tier0.KindNonce:
		return uint256.NewInt(a.nonce).Bytes32(), nil
	case tier0.KindMulti:
		if a.multicoin {
			return [32]byte{31: 1}, nil
		}
		return [32]byte{}, nil
	}
	return a.codeHash, nil
}

// HasAny implements asmevm.StorageBase.
func (s *State) HasAny(h uint32) bool { return h != 0 }

// BaseSlotKeys implements asmevm.Loader: keccak(state key) of every non-zero slot of handle h
// in the overlay view (base slots the overlay zeroed are left out). f must not call the State
// (the base stays read-locked while it runs).
func (s *State) BaseSlotKeys(h uint32, f func(sh asmevm.Hash) bool) error {
	type kv struct {
		sh   [32]byte
		zero bool
	}
	var over []kv // the overlay's keys in the executor view, keccak ascending
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	s.mu.RLock()
	hd := s.hs[h-1]
	if ov := hd.ov; ov != nil && ov.runs != nil {
		del, _ := delAt(ov.vers, s.n)
		r, m := &ov.runs.recent, &ov.runs.main
		over = make([]kv, 0, r.n()+m.n())
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
			if v, ok := ov.runs.slotAt(&k, s.n, del); ok {
				over = append(over, kv{k, v == [32]byte{}})
			}
			for ; i < r.n() && bytes.Equal(r.sh(i), k[:]); i++ {
			}
			for ; j < m.n() && bytes.Equal(m.sh(j), k[:]); j++ {
			}
		}
	}
	s.mu.RUnlock()
	for _, x := range over {
		if !x.zero && !f(x.sh) {
			return nil
		}
	}
	if hd.bh == 0 {
		return nil
	}
	return s.b.SlotKeys(hd.bh, func(sh [32]byte) bool {
		i := sort.Search(len(over), func(i int) bool { return bytes.Compare(over[i].sh[:], sh[:]) >= 0 })
		if i < len(over) && over[i].sh == sh {
			return true // the overlay's value counts, listed above when non-zero
		}
		return f(sh)
	})
}

var _ asmevm.Loader = (*State)(nil)

// Size is the overlay's size without a walk (the per-cut line on the executor thread): the
// accounts with an account record version (= Accounts) and the slot version records of all
// runs (Slots counts distinct keys, a walk over every record).
func (s *State) Size() (accounts, slotVersions int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nAcct, s.nRec
}

// Accounts counts the accounts with an overlay record; Slots counts the distinct slot keys the
// overlay holds (any view); Codes counts the overlay's codes.
func (s *State) Accounts() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, ov := range s.accts {
		if len(ov.vers) > 0 {
			n++
		}
	}
	return n
}

func (s *State) Slots() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, ov := range s.accts {
		if ov.runs == nil {
			continue
		}
		r, m := &ov.runs.recent, &ov.runs.main
		for _, x := range []*run{r, m} {
			for i := range x.n() {
				if i == 0 || !bytes.Equal(x.sh(i), x.sh(i-1)) {
					n++
				}
			}
		}
		for i := range r.n() { // a key in both runs counted once
			if i == 0 || !bytes.Equal(r.sh(i), r.sh(i-1)) {
				if k := r.key(i); m.lower(&k) < m.n() && bytes.Equal(m.sh(m.lower(&k)), k[:]) {
					n--
				}
			}
		}
	}
	return n
}

func (s *State) Codes() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.codes)
}

// MultiCoin is addr's coreth multicoin flag (overlay first, then the base).
func (s *State) MultiCoin(addr asmevm.Address) bool {
	if c, ok := s.ac.get(s.ac.gen.Load(), (*[20]byte)(&addr)); ok { // the Account load just before it
		return c.multicoin
	}
	v, err := s.Value(addr, tier0.KindMulti, [32]byte{})
	if err != nil {
		panic(err)
	}
	return v[31] == 1
}

// Coin is addr's balance of multicoin asset key (the coin id, bit 248 set: the storage key as
// coreth stores it), overlay first, then the base.
func (s *State) Coin(addr asmevm.Address, key asmevm.Hash) (uint256.Int, error) {
	var out uint256.Int
	v, err := s.Value(addr, tier0.KindSlot, key)
	if err != nil {
		return out, err
	}
	out.SetBytes32(v[:])
	return out, nil
}
