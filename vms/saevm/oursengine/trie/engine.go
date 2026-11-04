package trie

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/era"
)

// Trie is one trie (the account trie or one storage trie) cut at a uniform depth.
type Trie struct {
	pos    []Pos                 // levels 0..depth concatenated (level l at levelOff(l)); in the top file when ro
	bounds []uint64              // per cut: run start in the base (storage: byte offset; accounts: ordinal); + end
	bb     []byte                // the bounds as the top file holds them (u64 little-endian) when bounds is nil
	ro     bool                  // pos is the top file's (read only): changed positions live in mod
	mod    map[int32]*Pos        // positions changed since the top file was opened (ro only)
	addr   [20]byte              // the owner's address (Settled source: its values are read by address)
	ov     map[uint32][][32]byte // overlay keys per cut, sorted
	vals   map[[32]byte]sval     // storage overlay values
	depth  uint8
	noBase bool                // created or wiped after the base: no base run
	over   map[uint32]struct{} // cuts whose run held more than 16 K keys at their last recompute
	memo   map[uint32]*cutMemo // RAM-only levels below recently recomputed cuts (memo.go)
	pend   [16]pendPart        // slot keys written since the last Root by part (part), to list and mark (flushPending)
	pendOn bool                // on e.pendTries
	npend  int                 // the keys in pend
}

// pendPart is one part's pending slot keys of a trie, alone on its cache line (the
// parallel apply appends to the 16 parts on 16 goroutines).
type pendPart struct {
	ks [][32]byte
	_  [40]byte
}

// pendN is the number of slot keys pending in t.
func (t *Trie) pendN() int { return t.npend }

type sval struct {
	n uint8
	v [32]byte
}

type ovAcct struct {
	gone      bool
	nonce     uint64
	balance   [32]byte
	code      [32]byte
	hasCode   bool
	multicoin bool
}

func levelOff(l int) int { return ((1 << (4 * l)) - 1) / 15 }

func (t *Trie) cuts() int { return 1 << (4 * int(t.depth)) }

func (t *Trie) cutOf(k *[32]byte) uint32 {
	var p uint32
	for i := 0; i < int(t.depth); i++ {
		p = p<<4 | uint32(nib(k, i))
	}
	return p
}

func (t *Trie) cut(c uint32) *Pos { return t.at(levelOff(int(t.depth)) + int(c)) }

// bound is cut c's run start (c = cuts(): the end).
func (t *Trie) bound(c int) uint64 {
	if t.bounds != nil {
		return t.bounds[c]
	}
	return binary.LittleEndian.Uint64(t.bb[8*c:])
}

// at is position i: a changed one from mod, else pos (read only in a top file trie: write only
// through own).
func (t *Trie) at(i int) *Pos {
	if p := t.mod[int32(i)]; p != nil {
		return p
	}
	return &t.pos[i]
}

// own makes position i writable and returns it. Not safe for concurrent use: Root owns every
// position it will write before its parallel phases.
func (t *Trie) own(i int) *Pos {
	if !t.ro {
		return &t.pos[i]
	}
	if p := t.mod[int32(i)]; p != nil {
		return p
	}
	if len(t.mod) >= len(t.pos)/8 { // an eighth changed: one heap copy is smaller than a map entry each
		pos := slices.Clone(t.pos)
		for j, p := range t.mod {
			pos[j] = *p
		}
		t.pos, t.mod, t.ro = pos, nil, false
		return &t.pos[i]
	}
	if t.mod == nil {
		t.mod = map[int32]*Pos{}
	}
	p := new(Pos)
	*p = t.pos[i]
	t.mod[int32(i)] = p
	return p
}

// ownPath makes cut c and its stored ancestors writable.
func (t *Trie) ownPath(c uint32) {
	d := int(t.depth)
	for l := d; l >= 0; l-- {
		t.own(levelOff(l) + int(c>>(4*(d-l))))
	}
}

// depthFor puts n / 16^d closest to k in log scale.
func depthFor(n int64, k int) uint8 {
	if n <= int64(4*k) {
		return 0
	}
	d := math.Round(math.Log(float64(n)/float64(k)) / math.Log(16))
	return uint8(max(0, min(d, 7)))
}

func newTrie(depth uint8) *Trie {
	n := 1 << (4 * int(depth))
	return &Trie{depth: depth, pos: make([]Pos, levelOff(int(depth)+1)), bounds: make([]uint64, n+1)}
}

// Engine holds the base, the overlay and every trie.
type Engine struct {
	K          int
	FiveFields bool // coreth account RLP (multicoin field)
	Threads    int

	s       *era.Snap
	check   func(off, n uint64) error // the base's first-touch chunk check (base.Base.Check)
	acctOff []uint64                  // account ordinal -> record offset
	acct    *Trie
	stor    map[[32]byte]*Trie // every trie (built), or the tries touched since the top file was opened (nil: deleted)
	top     *topFile           // the opened top file the untouched tries are read from (nil: built)
	ov      map[[32]byte]*ovAcct

	dirtyStor map[[32]byte]map[uint32]subMask // dirty cuts and their dirty sub-cuts
	dirtyAcct map[uint32]subMask

	NoMemo    bool   // no cut memos (memo.go): every dirty cut is rehashed whole
	memoKeys  [2]int // cut sizes that keep one and two memo levels (zero: defaultMemoKeys)
	memoTries map[*Trie]struct{}
	gen       uint64     // files applied (Top.apply): the memos' clock
	pendTries []pendTrie // Settled source: tries with slot writes to list and mark (flushPending)
	pendAddrs []pendAddr

	src   Settled      // nil: the overlay keeps its values (t.vals, e.ov)
	lanes SettledLanes // src's lane reads, when it has them
	fw    fileWrites   // with src: the writes of the tier0 being applied (Top.LiveOverlay: since the last Swap, the live overlay)
	// Top.LiveOverlay (overlay.go): the overlay frozen at the last Swap while it drains (nil:
	// none), the drained level 1 files (newest first), the running drain, the finished drains
	frozen *fileWrites
	l1     []*l1File
	drainC chan drainDone
	Drains []DrainStats
	kc     *Keccaker // sequential keccak (overlay list inserts)

	workers []*worker
	S       Stats
}

// Stats of the last Build or Root.
type Stats struct {
	Leaves, Nodes     uint64 // hashes
	Runs, RunKeys     uint64 // cut recomputes and the live keys of those cuts
	StorTries, Recuts uint64
	SlowNodes         uint64
	MemoFills         uint64 // cuts recomputed whole that filled their memo
	MemoHits          uint64 // cuts recomputed from their memo
	MemoCuts          uint64 // memos kept after the last file (Top.apply)
	MemoBytes         uint64
	MemoDrops         uint64 // memos the last file's sweep dropped
	StoredNodes, Cuts uint64 // whole engine, after Build
	Phases            []Phase
	DirtyAccounts     uint64 // Settled source: distinct accounts the tier0 being applied wrote
	DirtyStorageKeys  uint64 // Settled source: distinct slots the tier0 being applied wrote
	AccountCuts       uint64
	StorageCuts       uint64
	AccountTrieDepth  uint8
	ThreadsUsed       int
}

type Phase struct {
	Name string
	Dur  time.Duration
}

func (s *Stats) phase(name string, t0 *time.Time) {
	now := time.Now()
	s.Phases = append(s.Phases, Phase{name, now.Sub(*t0)})
	*t0 = now
}

func (s Stats) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "leaves=%d nodes=%d runs=%d runkeys=%d slow=%d recuts=%d storcuts=%d acctcuts=%d stortries=%d dirtyslots=%d dirtyaccts=%d memo fills=%d hits=%d kept=%d (%d MB)", s.Leaves, s.Nodes, s.Runs, s.RunKeys, s.SlowNodes, s.Recuts, s.StorageCuts, s.AccountCuts, s.StorTries, s.DirtyStorageKeys, s.DirtyAccounts, s.MemoFills, s.MemoHits, s.MemoCuts, s.MemoBytes>>20)
	for _, p := range s.Phases {
		fmt.Fprintf(&b, " %s=%.3fs", p.Name, p.Dur.Seconds())
	}
	return b.String()
}

type worker struct {
	prep  []prepW // Top.prepare's arena
	h     *Hasher
	kvs   []kv
	arena []byte
	rec   era.Rec
	pend  int
	key   [32]byte
	sb    subBounds
	ovv   [][32]byte // slotsNow's values
	ovok  []bool     // and whether the overlay holds them
	mix   []int      // slotsNow: the keys no layer above the store answered (index in shs)
	mks   [][32]byte // and their keccaks, values and found from the Settled view
	mvv   [][32]byte
	mok   []bool
	lane  int         // its index in the pool: its Settled read lane
	cn    []cnode     // combineMany's nodes
	ct    []ctop      // cutTops' cuts
	fks   [][32]byte  // flushPending's keys of a split trie's item
	cb    nodeBatch   // and its encodings
	kmsgs [][]byte    // prepGroups' keys to hash, 8 lanes at a time
	kdst  []*[32]byte // and where each hash goes
	ksums [][32]byte
	kidx  []int
}

func (e *Engine) pool() []*worker {
	if e.Threads <= 0 {
		e.Threads = runtime.GOMAXPROCS(0)
	}
	for len(e.workers) < e.Threads {
		e.workers = append(e.workers, &worker{h: NewHasher(), lane: len(e.workers)})
	}
	return e.workers[:e.Threads]
}

// BaseError is the panic value of an engine method whose base read failed its chunk check
// (an absent or damaged chunk). Top's methods return it as an error; after it the engine's
// state is undefined.
type BaseError struct{ Err error }

func (b BaseError) Error() string { return "trie: base read: " + b.Err.Error() }
func (b BaseError) Unwrap() error { return b.Err }

// baseBytes returns the base bytes [off, end) after checking their chunks.
func (e *Engine) baseBytes(off, end uint64) []byte {
	if end > off {
		if err := e.check(off, end-off); err != nil {
			panic(BaseError{err})
		}
	}
	return e.s.B[off:end]
}

// maxAcctRec is the longest account record (era.Decode): tag, key, flags, nonce varint, balance,
// code hash.
const maxAcctRec = 1 + 32 + 1 + 10 + 33 + 32

// acctRec returns the bytes that hold the account record at off (checked).
func (e *Engine) acctRec(off uint64) []byte {
	return e.baseBytes(off, min(off+maxAcctRec, e.s.StateEnd()))
}

// parallel runs fn(w, i) for i in [0, n) on the pool; flush is called per started worker at the end.
// A panic in a worker stops the others' next items and is raised again in the caller.
func (e *Engine) parallel(n int, fn func(w *worker, i int), flush func(w *worker)) {
	ws := e.pool()
	// a worker with no item to take is not started: a per-block Root runs this five times, often
	// for 0-3 items, and starting and waiting for every worker cost more than its few hashes
	ws = ws[:min(n, len(ws))]
	if len(ws) == 1 { // on the caller's goroutine
		for i := range n {
			fn(ws[0], i)
		}
		if flush != nil {
			flush(ws[0])
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	var failed atomic.Pointer[any]
	for _, w := range ws {
		wg.Add(1)
		go func(w *worker) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					failed.CompareAndSwap(nil, &r)
					next.Store(int64(n))
				}
			}()
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					break
				}
				fn(w, i)
			}
			if flush != nil {
				flush(w)
			}
		}(w)
	}
	wg.Wait()
	if r := failed.Load(); r != nil {
		panic(*r)
	}
}

func (e *Engine) collectHashStats() {
	for _, w := range e.workers {
		e.S.Leaves += w.h.Leaves
		e.S.Nodes += w.h.Nodes
		w.h.Leaves, w.h.Nodes = 0, 0
	}
}

// --- runs ---

const batchLeaves = 512

// queue gathers the run of cuts [c0, c1) of t and queues it for hashing at depth.
func (e *Engine) queue(w *worker, t *Trie, storage bool, ah *[32]byte, c0, c1 uint32, depth int, out *Pos) int {
	if w.pend == 0 {
		w.h.Begin()
		w.arena = w.arena[:0]
	}
	w.kvs = w.kvs[:0]
	if storage {
		e.storageRun(w, t, c0, c1)
	} else {
		e.accountRun(w, t, c0, c1)
	}
	n := len(w.kvs)
	w.h.Add(w.kvs, depth, out)
	w.pend += n + 1
	if w.pend >= batchLeaves {
		w.h.Finish()
		w.pend = 0
	}
	return n
}

func (e *Engine) flush(w *worker) {
	if w.pend > 0 {
		w.h.Finish()
		w.pend = 0
	}
}

// storageRun appends the leaves of cuts [c0, c1) of storage trie t to w.kvs.
func (e *Engine) storageRun(w *worker, t *Trie, c0, c1 uint32) {
	var ovk [][32]byte
	if c1 == c0+1 {
		ovk = t.ov[c0]
	} else { // one copy of the cuts' lists (appending to a capped slice copied it per cut)
		n := 0
		for c := c0; c < c1; c++ {
			n += len(t.ov[c])
		}
		if n > 0 {
			ovk = make([][32]byte, 0, n)
			for c := c0; c < c1; c++ {
				ovk = append(ovk, t.ov[c]...)
			}
		}
	}
	e.storageRange(w, t, ovk, t.bound(int(c0)), t.bound(int(c1)), nil)
}

// storageRange merges base run [lo, hi) of storage trie t with sorted overlay keys ovk into
// w.kvs, in key order; on equal keys the overlay wins. sb, when set, records the sub-cut bounds.
func (e *Engine) storageRange(w *worker, t *Trie, ovk [][32]byte, lo, hi uint64, sb *subBounds) {
	var b []byte
	if !t.noBase {
		b = e.baseBytes(lo, hi)
	}
	p := lo
	ovh := ovk // the entries are the trie keys (with a Settled source too)
	oi := 0
	if e.src != nil && len(ovh) > 0 {
		e.slotsNow(w, &t.addr, ovh)
	}
	emitOv := func(i int, baseVal []byte) { // baseVal: the base's value of the key (nil: none)
		var v []byte
		if e.src != nil {
			v = baseVal
			if w.ovok[i] {
				v = trim(w.ovv[i][:])
			}
		} else {
			sv := t.vals[ovk[i]]
			v = sv.v[:sv.n]
		}
		if len(v) == 0 {
			return
		}
		s := len(w.arena)
		w.arena = StorageVal(w.arena, v)
		w.kvs = append(w.kvs, kv{key: &ovh[i], val: w.arena[s:]})
	}
	for len(b) > 0 {
		r := &w.rec
		if err := era.Decode(b, r); err != nil {
			panic(err)
		}
		b = b[r.Len:]
		p += uint64(r.Len)
		if r.Tag != era.TagSlot {
			continue
		}
		k := (*[32]byte)(r.Key)
		if sb != nil {
			sb.at(k, p-uint64(r.Len))
		}
		for oi < len(ovh) && bytes.Compare(ovh[oi][:], k[:]) < 0 {
			emitOv(oi, nil)
			oi++
		}
		if oi < len(ovh) && ovh[oi] == *k {
			emitOv(oi, r.Value)
			oi++
			continue
		}
		s := len(w.arena)
		w.arena = StorageVal(w.arena, r.Value)
		w.kvs = append(w.kvs, kv{key: k, val: w.arena[s:]})
	}
	if sb != nil {
		sb.end(hi)
	}
	for ; oi < len(ovh); oi++ {
		emitOv(oi, nil)
	}
}

func (e *Engine) storageRoot(k *[32]byte, kc *Keccaker) [32]byte {
	if t, ok := e.stor[*k]; ok {
		if t == nil {
			return EmptyRoot // deleted
		}
		return kc.RootHash(t.at(0))
	}
	if e.top != nil {
		if off, ok := e.top.find(k); ok {
			return kc.RootHash(e.top.root(off))
		}
	}
	return EmptyRoot
}

// accountRun appends the leaves of cuts [c0, c1) of the account trie t to w.kvs.
func (e *Engine) accountRun(w *worker, t *Trie, c0, c1 uint32) {
	var ovk [][32]byte
	for c := c0; c < c1; c++ {
		if l := t.ov[c]; len(l) > 0 {
			ovk = append(ovk[:len(ovk):len(ovk)], l...)
		}
	}
	e.accountRange(w, t, ovk, t.bound(int(c0)), t.bound(int(c1)), nil)
}

// accountRange merges the base accounts of ordinals [lo, hi) with sorted overlay list ovk into
// w.kvs, in key order; on equal keys the overlay wins. sb, when set, records the sub-cut bounds.
func (e *Engine) accountRange(w *worker, t *Trie, ovk [][32]byte, lo, hi uint64, sb *subBounds) {
	ovh := ovk // the entries' trie keys
	if e.src != nil {
		ovh = make([][32]byte, len(ovk))
		for i := range ovk {
			ovh[i] = w.h.k.Sum(ovk[i][:20])
		}
	}
	oi := 0
	emitOv := func(i int, r *era.Rec) { // r: the base's record of the account (nil: none)
		var a ovAcct
		if e.src != nil {
			x, exists, ok := e.acctNow(w, (*[20]byte)(ovk[i][:20]))
			if !ok && r != nil {
				x, exists = baseAcct(r), true
			}
			if !exists {
				return
			}
			a = x
		} else if a = *e.ov[ovk[i]]; a.gone {
			return
		}
		k := &ovh[i]
		root := e.storageRoot(k, w.h.k)
		var code []byte
		if a.hasCode {
			code = a.code[:]
		}
		s := len(w.arena)
		w.arena = AccountVal(w.arena, a.nonce, a.balance[:], &root, code, a.multicoin, e.FiveFields)
		w.kvs = append(w.kvs, kv{key: k, val: w.arena[s:]})
	}
	for i := lo; i < hi; i++ {
		r := &w.rec
		if err := era.Decode(e.acctRec(e.acctOff[i]), r); err != nil {
			panic(err)
		}
		k := (*[32]byte)(r.Key)
		if sb != nil {
			sb.at(k, i)
		}
		for oi < len(ovh) && bytes.Compare(ovh[oi][:], k[:]) < 0 {
			emitOv(oi, nil)
			oi++
		}
		if oi < len(ovh) && ovh[oi] == *k {
			emitOv(oi, r)
			oi++
			continue
		}
		root := e.storageRoot(k, w.h.k)
		s := len(w.arena)
		w.arena = AccountVal(w.arena, r.Nonce, r.Balance, &root, r.CodeHash, r.Flags&1 != 0, e.FiveFields)
		w.kvs = append(w.kvs, kv{key: k, val: w.arena[s:]})
	}
	if sb != nil {
		sb.end(hi)
	}
	for ; oi < len(ovh); oi++ {
		emitOv(oi, nil)
	}
}

// --- stored levels ---

// node recomputes stored node (l, j) from its 16 children.
func (e *Engine) node(w *worker, t *Trie, storage bool, ah *[32]byte, l, j int) {
	var kids [16]*Pos
	for i := range kids {
		kids[i] = t.at(levelOff(l+1) + 16*j + i)
	}
	out := t.at(levelOff(l) + j)
	if !e.combine(w, &kids, out, l) {
		e.nodeRange(w, t, storage, ah, l, j, out)
	}
}

// nodeRange recomputes stored node (l, j) into out from its key range: one leaf or extension
// below it, whose shape grows by a nibble.
func (e *Engine) nodeRange(w *worker, t *Trie, storage bool, ah *[32]byte, l, j int, out *Pos) {
	sh := 4 * (int(t.depth) - l - 1)
	c0, c1 := uint32(j)<<(sh+4), uint32(j+1)<<(sh+4)
	e.flush(w)
	e.queue(w, t, storage, ah, c0, c1, l, out)
	e.flush(w)
	atomic.AddUint64(&e.S.SlowNodes, 1)
}

// combine sets out, the position at depth l over the 16 child positions kids, and reports true;
// false when exactly one child is a leaf or an extension (its shape grows by a nibble: the
// caller recomputes the node's range).
func (e *Engine) combine(w *worker, kids *[16]*Pos, out *Pos, l int) bool {
	enc, kind, ok := nodeEnc(w, kids, l)
	switch {
	case !ok:
		return false
	case enc == nil:
		*out = Pos{}
	default:
		w.h.k.refOf(out, enc, kind)
	}
	return true
}

// nodeEnc is the encoding (in w.h.buf) and kind of the node at depth l over the 16 child
// positions kids; enc nil: no child (the empty position); ok false: exactly one child, a leaf
// or an extension (combine's false).
func nodeEnc(w *worker, kids *[16]*Pos, l int) (enc []byte, kind uint8, ok bool) {
	m, one := 0, 0
	for i := range kids {
		if kids[i].N != 0 {
			m++
			one = i
		}
	}
	switch {
	case m == 0:
		return nil, 0, true
	case m >= 2:
		for i := range kids {
			w.h.kids[i] = kids[i].ref()
			if kids[i].N == 0 {
				w.h.kids[i] = nil
			}
		}
		w.h.Nodes++
		return branchEnc(w.h.buf, &w.h.kids), kBranch, true
	case kids[one].Kind == kBranch:
		var k [32]byte
		k[l>>1] = byte(one) << 4
		if l&1 == 1 {
			k[l>>1] = byte(one)
		}
		w.h.Nodes++
		return extEnc(w.h.buf, &k, l, l+1, kids[one].ref()), kOther, true
	}
	return nil, 0, false
}

// dtrie is a trie with dirty cuts (sorted, unique), for uppers.
type dtrie struct {
	ah   [32]byte
	t    *Trie
	cuts []uint32
}

// uppers is upper for the tries ds at once, level by level across the pool: at each level, from
// the deepest down, every trie that has the level gives one item per dirty node (the distinct
// parents of its dirty nodes one level below), so a big trie's stored levels are not one
// goroutine's work. ah is passed to node only for storage tries. Each d.cuts is reused.
func (e *Engine) uppers(ds []dtrie, storage bool) {
	maxd := 0
	for i := range ds {
		maxd = max(maxd, int(ds[i].t.depth))
	}
	type up struct {
		d int
		j uint32
	}
	var items []up
	for l := maxd - 1; l >= 0; l-- {
		items = items[:0]
		for i := range ds {
			d := &ds[i]
			if int(d.t.depth) <= l {
				continue
			}
			n := 0
			for _, c := range d.cuts {
				if p := c >> 4; n == 0 || d.cuts[n-1] != p {
					d.cuts[n] = p
					n++
				}
			}
			d.cuts = d.cuts[:n]
			for _, j := range d.cuts {
				items = append(items, up{i, j})
			}
		}
		e.parallel((len(items)+upChunk-1)/upChunk, func(w *worker, i int) {
			ch := items[i*upChunk : min((i+1)*upChunk, len(items))]
			ns := w.cn[:0]
			for _, it := range ch {
				t := ds[it.d].t
				var n cnode
				for k := range n.kids {
					n.kids[k] = t.at(levelOff(l+1) + 16*int(it.j) + k)
				}
				n.out, n.l = t.at(levelOff(l)+int(it.j)), l
				ns = append(ns, n)
			}
			e.combineMany(w, ns)
			for k := range ns {
				if !ns[k].ok {
					it := ch[k]
					d := &ds[it.d]
					var ah *[32]byte
					if storage {
						ah = &d.ah
					}
					e.nodeRange(w, d.t, storage, ah, l, int(it.j), ns[k].out)
				}
			}
			w.cn = ns
		}, nil)
	}
}

// allUpper recomputes every stored node of t, level by level.
func (e *Engine) allUpper(w *worker, t *Trie, storage bool, ah *[32]byte) {
	for l := int(t.depth) - 1; l >= 0; l-- {
		for j := 0; j < 1<<(4*l); j++ {
			e.node(w, t, storage, ah, l, j)
		}
	}
}

// --- build ---

type span struct {
	ah         [32]byte
	start, end uint64
	n          int64
}

// Build loads the base and computes every trie. It returns the state root. Every base byte
// it reads is checked first (it reads the whole state region).
func (e *Engine) Build(b *base.Base) (root [32]byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			be, ok := r.(BaseError)
			if !ok {
				panic(r)
			}
			err = be
		}
	}()
	s := b.Snap()
	e.s, e.check = s, b.Check
	e.S = Stats{}
	e.stor = map[[32]byte]*Trie{}
	e.ov = map[[32]byte]*ovAcct{}
	e.dirtyStor = map[[32]byte]map[uint32]subMask{}
	e.dirtyAcct = map[uint32]subMask{}
	ws := e.pool()
	e.S.ThreadsUsed = len(ws)
	t0 := time.Now()

	// pass 1: account offsets and storage spans, by frame chunks
	nch := min(s.NState, len(ws)*64)
	accts := make([][]uint64, nch)
	spans := make([][]span, nch)
	var perr atomic.Value
	e.parallel(nch, func(w *worker, ci int) {
		f0, f1 := ci*s.NState/nch, (ci+1)*s.NState/nch
		var as []uint64
		var ss []span
		var cur [32]byte
		var prevEnd uint64
		for f := f0; f < f1; f++ {
			_, off, n := s.Frame(f)
			if f > f0 && off != prevEnd {
				perr.Store(fmt.Errorf("cut: frame %d at %d does not follow the previous frame (%d)", f, off, prevEnd))
				return
			}
			prevEnd = off + uint64(n)
			b := e.baseBytes(off, off+uint64(n))
			p := off
			for len(b) > 0 {
				r := &w.rec
				if err := era.Decode(b, r); err != nil {
					perr.Store(err)
					return
				}
				switch r.Tag {
				case era.TagAccount:
					as = append(as, p)
					copy(cur[:], r.Key)
				case era.TagContext:
					copy(cur[:], r.Key)
				case era.TagSlot:
					if len(ss) > 0 && ss[len(ss)-1].ah == cur {
						ss[len(ss)-1].end = p + uint64(r.Len)
						ss[len(ss)-1].n++
					} else {
						ss = append(ss, span{ah: cur, start: p, end: p + uint64(r.Len), n: 1})
					}
				case era.TagAbsent:
					perr.Store(fmt.Errorf("cut: tombstone in a base"))
					return
				}
				b = b[r.Len:]
				p += uint64(r.Len)
			}
		}
		accts[ci], spans[ci] = as, ss
	}, nil)
	if err, _ := perr.Load().(error); err != nil {
		return [32]byte{}, err
	}
	na := 0
	for _, a := range accts {
		na += len(a)
	}
	e.acctOff = make([]uint64, 0, na)
	for i := range accts {
		e.acctOff = append(e.acctOff, accts[i]...)
		accts[i] = nil
	}
	var all []span
	for _, ss := range spans {
		for _, sp := range ss {
			if l := len(all); l > 0 && all[l-1].ah == sp.ah {
				all[l-1].end, all[l-1].n = sp.end, all[l-1].n+sp.n
			} else {
				all = append(all, sp)
			}
		}
	}
	spans = nil
	e.S.phase("scan", &t0)

	// tries and cut bounds
	tries := make([]*Trie, len(all))
	for i, sp := range all {
		t := newTrie(depthFor(sp.n, e.K))
		t.bounds[0], t.bounds[len(t.bounds)-1] = sp.start, sp.end
		e.stor[sp.ah] = t
		tries[i] = t
	}
	e.parallel(len(all), func(w *worker, i int) {
		t := tries[i]
		if t.depth == 0 {
			return
		}
		sp := &all[i]
		nc := t.cuts()
		next := 0 // next cut whose start is unset
		b := e.baseBytes(sp.start, sp.end)
		p := sp.start
		for len(b) > 0 {
			r := &w.rec
			if err := era.Decode(b, r); err != nil {
				panic(err)
			}
			if r.Tag == era.TagSlot {
				c := int(t.cutOf((*[32]byte)(r.Key)))
				for next <= c {
					t.bounds[next] = p
					next++
				}
			}
			b = b[r.Len:]
			p += uint64(r.Len)
		}
		for ; next < nc; next++ {
			t.bounds[next] = sp.end
		}
	}, nil)
	e.acct = newTrie(depthFor(int64(len(e.acctOff)), e.K))
	e.acctBounds(e.acct)
	e.S.phase("bounds", &t0)

	// pass 2: storage cuts, units of up to 256 cuts
	type unit struct {
		t      *Trie
		c0, c1 uint32
	}
	var units []unit
	for _, t := range tries {
		for c := 0; c < t.cuts(); c += 256 {
			units = append(units, unit{t, uint32(c), uint32(min(c+256, t.cuts()))})
		}
	}
	e.parallel(len(units), func(w *worker, i int) {
		u := units[i]
		for c := u.c0; c < u.c1; c++ {
			e.queue(w, u.t, true, nil, c, c+1, int(u.t.depth), u.t.cut(c))
		}
	}, e.flush)
	for _, t := range tries {
		e.S.Runs += uint64(t.cuts())
	}
	e.S.phase("storage-cuts", &t0)
	e.parallel(len(all), func(w *worker, i int) {
		if tries[i].depth > 0 {
			e.allUpper(w, tries[i], true, &all[i].ah)
		}
	}, nil)
	e.S.phase("storage-upper", &t0)

	// pass 3: account cuts, then the account trie
	at := e.acct
	nu := (at.cuts() + 255) / 256
	e.parallel(nu, func(w *worker, i int) {
		for c := i * 256; c < min((i+1)*256, at.cuts()); c++ {
			e.queue(w, at, false, nil, uint32(c), uint32(c+1), int(at.depth), at.cut(uint32(c)))
		}
	}, e.flush)
	e.S.phase("account-cuts", &t0)
	e.allUpper(ws[0], at, false, nil)
	e.S.phase("account-upper", &t0)
	e.collectHashStats()
	e.S.StorTries = uint64(len(tries))
	e.S.AccountTrieDepth = at.depth
	return ws[0].h.k.RootHash(at.at(0)), nil
}

// acctBounds sets the cut bounds of account trie t (account ordinals) from the base.
func (e *Engine) acctBounds(t *Trie) {
	t.bounds[0], t.bounds[len(t.bounds)-1] = 0, uint64(len(e.acctOff))
	for c := 1; c < t.cuts(); c++ {
		var k [32]byte
		for i := 0; i < int(t.depth); i++ {
			v := byte(c>>(4*(int(t.depth)-1-i))) & 15
			if i&1 == 0 {
				k[i>>1] |= v << 4
			} else {
				k[i>>1] |= v
			}
		}
		t.bounds[c] = uint64(sort.Search(len(e.acctOff), func(i int) bool {
			o := e.acctOff[i]
			return bytes.Compare(e.baseBytes(o+1, o+33), k[:]) >= 0
		}))
	}
}

// MemStats counts the stored nodes and cuts of the tries in the heap (a built engine: all of
// them; an opened one: the account trie and the tries touched since) and their bytes.
func (e *Engine) MemStats() (nodes, cuts, bytes uint64) {
	add := func(t *Trie) {
		nodes += uint64(levelOff(int(t.depth)))
		cuts += uint64(t.cuts())
		bytes += uint64(len(t.pos))*uint64(34) + uint64(len(t.bounds))*8
	}
	add(e.acct)
	for _, t := range e.stor {
		if t != nil {
			add(t)
		}
	}
	bytes += uint64(len(e.acctOff)) * 8
	return
}

// --- overlay ---

func (e *Engine) markAcct(ah [32]byte) *ovAcct {
	a := e.ov[ah]
	if a == nil {
		a = &ovAcct{}
		e.ov[ah] = a
		c := e.acct.cutOf(&ah)
		if e.acct.ov == nil {
			e.acct.ov = map[uint32][][32]byte{}
		}
		e.acct.ov[c], _ = e.insertKey(e.acct.ov[c], ah, ah, true, nil)
	}
	return a
}

// SetAccount writes an account. codeHash nil (or keccak("")) = no code.
func (e *Engine) SetAccount(ah [32]byte, nonce uint64, balance []byte, codeHash []byte, multicoin bool) {
	a := e.markAcct(ah)
	*a = ovAcct{nonce: nonce, multicoin: multicoin}
	copy(a.balance[32-len(balance):], balance)
	if codeHash != nil && !bytes.Equal(codeHash, EmptyCode[:]) {
		a.hasCode = true
		copy(a.code[:], codeHash)
	}
	e.markAcctDirty(&ah)
}

// DeleteAccount removes an account and its storage.
func (e *Engine) DeleteAccount(ah [32]byte) {
	a := e.markAcct(ah)
	*a = ovAcct{gone: true}
	e.dropTrie(ah)
}

// dropTrie removes ah's storage trie with the account.
func (e *Engine) dropTrie(ah [32]byte) {
	if e.top != nil {
		e.stor[ah] = nil // the top file's trie is gone
	} else {
		delete(e.stor, ah)
	}
	delete(e.dirtyStor, ah)
	e.markAcctDirty(&ah)
}

// SetSlot writes a storage word (big-endian, any length; zero deletes).
func (e *Engine) SetSlot(ah, sh [32]byte, v []byte) {
	t := e.slotTrie(ah)
	if t.vals == nil {
		t.vals = map[[32]byte]sval{}
		t.ov = map[uint32][][32]byte{}
	}
	for len(v) > 0 && v[0] == 0 {
		v = v[1:]
	}
	var sv sval
	sv.n = uint8(copy(sv.v[:], v))
	c := t.cutOf(&sh)
	if _, ok := t.vals[sh]; !ok {
		t.ov[c], _ = e.insertKey(t.ov[c], sh, sh, false, nil)
	}
	t.vals[sh] = sv
	e.markSlot(ah, t, c, &sh)
}

// slotTrie is ah's storage trie for a write, created when there is none.
func (e *Engine) slotTrie(ah [32]byte) *Trie {
	t := e.touch(&ah)
	if t == nil {
		t = newTrie(0)
		t.noBase = true
		e.stor[ah] = t
	}
	return t
}

// markSlot marks slot sh (in cut c) of ah's storage trie t and the account dirty.
func (e *Engine) markSlot(ah [32]byte, t *Trie, c uint32, sh *[32]byte) {
	d := e.dirtyStor[ah]
	if d == nil {
		d = map[uint32]subMask{}
		e.dirtyStor[ah] = d
	}
	m := d[c]
	m.set(subOf(sh, int(t.depth)))
	d[c] = m
	e.markAcctDirty(&ah)
}

// markAcctDirty marks account ah dirty in the account trie.
func (e *Engine) markAcctDirty(ah *[32]byte) {
	c := e.acct.cutOf(ah)
	m := e.dirtyAcct[c]
	m.set(subOf(ah, int(e.acct.depth)))
	e.dirtyAcct[c] = m
}

// Dirty returns the number of dirty storage cuts and account cuts pending.
func (e *Engine) Dirty() (stor, acct int) {
	for _, d := range e.dirtyStor {
		stor += len(d)
	}
	return stor + len(e.pendTries), len(e.dirtyAcct) + len(e.pendAddrs)
}

// DirtyAll marks every cut dirty, so the next Root rehashes the whole state from base + overlay
// (a full rebuild that reuses only the cut bounds).
func (e *Engine) DirtyAll() {
	if e.top != nil {
		e.top.each(func(ah [32]byte, _ uint64) { e.touch(&ah) })
	}
	for ah, t := range e.stor {
		if t == nil {
			continue
		}
		d := make(map[uint32]subMask, t.cuts())
		for c := range uint32(t.cuts()) {
			d[c] = allSubs
		}
		e.dirtyStor[ah] = d
	}
	for c := range uint32(e.acct.cuts()) {
		e.dirtyAcct[c] = allSubs
	}
}

// --- root ---

// Root applies every pending change to the tries and returns the state root. It never changes
// a trie's cut depth: a cut that grew past 16 K keys is noted, and Recut (called at the end of
// each tier0 file) re-cuts its trie, so the stored layout does not depend on how often Root runs.
func (e *Engine) Root() [32]byte {
	tf := time.Now()
	e.flushPending()
	e.S = Stats{}
	e.S.Phases = append(e.S.Phases, Phase{"flush-pending", time.Since(tf)})
	if e.src != nil { // counted, not walked: a per-block Root must not cost the tier0's writes so far
		e.S.DirtyStorageKeys = uint64(e.fw.nslots)
		e.S.DirtyAccounts = uint64(len(e.fw.accts))
	}
	ws := e.pool()
	e.S.ThreadsUsed = len(ws)
	t0 := time.Now()
	type sitem struct {
		ah    [32]byte
		t     *Trie
		c     uint32
		n     int
		dirty subMask
		memo  *cutMemo
	}
	// the items, on all threads: one work unit per trie (ownPath writes a top file trie's mod
	// map), or per itemChunk cuts of a heap trie with many (own only reads it); the memos a
	// unit does not find are made after, on this goroutine (memoFor writes t.memo)
	type iunit struct {
		d      int      // its trie in dts
		cuts   []uint32 // its cuts (split trie), else nil: all of the trie's from its dirty map
		at     int      // its first item
		missed bool     // a cut without a memo
	}
	var dts []dtrie
	var units []iunit
	nitems := 0
	for ah, cuts := range e.dirtyStor {
		t := e.stor[ah]
		if t == nil {
			continue
		}
		if !e.NoMemo {
			e.memoMap(t)
		}
		di := len(dts)
		dts = append(dts, dtrie{ah: ah, t: t})
		if t.ro || len(cuts) < 2*itemChunk {
			units = append(units, iunit{d: di, at: nitems})
			nitems += len(cuts)
			continue
		}
		cs := make([]uint32, 0, len(cuts))
		for c := range cuts {
			cs = append(cs, c)
		}
		slices.Sort(cs)
		dts[di].cuts = cs
		for lo := 0; lo < len(cs); lo += itemChunk {
			units = append(units, iunit{d: di, cuts: cs[lo:min(lo+itemChunk, len(cs))], at: nitems + lo})
		}
		nitems += len(cs)
	}
	items := make([]sitem, nitems)
	e.parallel(len(units), func(w *worker, i int) {
		u := &units[i]
		d := &dts[u.d]
		t, dm := d.t, e.dirtyStor[d.ah]
		put := func(k int, c uint32) {
			t.ownPath(c)
			it := &items[u.at+k]
			*it = sitem{ah: d.ah, t: t, c: c, dirty: dm[c]}
			if !e.NoMemo {
				if m := t.memo[c]; m != nil {
					m.gen = e.gen
					it.memo = m
				} else {
					u.missed = true
				}
			}
		}
		if u.cuts != nil {
			for k, c := range u.cuts {
				put(k, c)
			}
			return
		}
		cs := make([]uint32, 0, len(dm))
		for c := range dm {
			put(len(cs), c)
			cs = append(cs, c)
		}
		slices.Sort(cs)
		d.cuts = cs
	}, nil)
	for i := range units {
		u := &units[i]
		if !u.missed {
			continue
		}
		n := len(u.cuts)
		if u.cuts == nil {
			n = len(e.dirtyStor[dts[u.d].ah])
		}
		for k := u.at; k < u.at+n; k++ {
			if items[k].memo == nil {
				items[k].memo = e.memoFor(items[k].t, items[k].c)
			}
		}
	}
	e.S.phase("items", &t0)
	var runKeys atomic.Uint64
	e.parallel(len(items), func(w *worker, i int) {
		it := &items[i]
		it.n = e.cutRun(w, it.t, true, it.c, &it.dirty, it.memo)
		runKeys.Add(uint64(it.n))
	}, e.flush)
	e.parallel((len(items)+topChunk-1)/topChunk, func(w *worker, i int) {
		cs := w.ct[:0]
		for j := i * topChunk; j < min((i+1)*topChunk, len(items)); j++ {
			it := &items[j]
			cs = append(cs, ctop{it.t, it.c, &it.dirty, it.memo})
		}
		e.cutTops(w, true, cs)
		w.ct = cs
	}, nil)
	for i := range items {
		e.noteSize(items[i].t, items[i].c, items[i].n)
		e.memoGrown(items[i].memo, items[i].n)
	}
	e.S.StorageCuts = uint64(len(items))
	e.S.phase("storage-cuts", &t0)
	e.uppers(dts, true)
	e.S.StorTries = uint64(len(dts))
	e.S.phase("storage-upper", &t0)

	at := e.acct
	acuts := make([]uint32, 0, len(e.dirtyAcct))
	for c := range e.dirtyAcct {
		at.ownPath(c)
		acuts = append(acuts, c)
	}
	slices.Sort(acuts)
	an := make([]int, len(acuts))
	amask := make([]subMask, len(acuts))
	amemo := make([]*cutMemo, len(acuts))
	for i, c := range acuts {
		amask[i], amemo[i] = e.dirtyAcct[c], e.memoFor(at, c)
	}
	e.parallel(len(acuts), func(w *worker, i int) {
		an[i] = e.cutRun(w, at, false, acuts[i], &amask[i], amemo[i])
		runKeys.Add(uint64(an[i]))
	}, e.flush)
	e.parallel((len(acuts)+topChunk-1)/topChunk, func(w *worker, i int) {
		cs := w.ct[:0]
		for j := i * topChunk; j < min((i+1)*topChunk, len(acuts)); j++ {
			cs = append(cs, ctop{at, acuts[j], &amask[j], amemo[j]})
		}
		e.cutTops(w, false, cs)
		w.ct = cs
	}, nil)
	for i, c := range acuts {
		e.noteSize(at, c, an[i])
		e.memoGrown(amemo[i], an[i])
	}
	e.S.AccountCuts = uint64(len(acuts))
	e.S.phase("account-cuts", &t0)
	e.uppers([]dtrie{{t: at, cuts: acuts}}, false)
	e.S.phase("account-upper", &t0)
	e.S.RunKeys = runKeys.Load()
	e.S.Runs = uint64(len(items) + len(acuts))
	e.collectHashStats()
	// new maps, not clear: clearing (and ranging over) a Go map costs its capacity, which a
	// tier0's Root grows to its dirty cuts and a per-block Root after it would pay every time
	e.dirtyStor = map[[32]byte]map[uint32]subMask{}
	e.dirtyAcct = map[uint32]subMask{}
	return ws[0].h.k.RootHash(at.at(0))
}

// noteSize records whether cut c of trie t (a storage trie or the account trie) is over the re-cut limit (16 K keys) after
// a recompute that found n keys in it.
func (e *Engine) noteSize(t *Trie, c uint32, n int) {
	if n > 16*e.K && t.depth < 7 {
		if t.over == nil {
			t.over = map[uint32]struct{}{}
		}
		t.over[c] = struct{}{}
	} else if t.over != nil {
		delete(t.over, c)
	}
}

// Recut re-cuts every trie with a cut over the limit (the storage tries and the account trie,
// whose depth is otherwise fixed by the base it was built on: one cut over a near-empty genesis
// base) at the depth its size asks for and returns how many it re-cut. Call it only right after Root (nothing pending): the
// layout then follows from the state alone. Re-cutting does not change any root.
func (e *Engine) Recut() int {
	if len(e.dirtyStor) > 0 || len(e.dirtyAcct) > 0 {
		panic("trie: Recut with changes pending (call Root first)")
	}
	type rc struct {
		ah [32]byte
		t  *Trie
	}
	var rs []rc
	for ah, t := range e.stor {
		if t != nil && len(t.over) > 0 {
			rs = append(rs, rc{ah, t})
		}
	}
	for i := range rs { // one at a time, each on every worker (one big trie is the usual case)
		e.recut(rs[i].t, &rs[i].ah)
	}
	n := len(rs)
	if len(e.acct.over) > 0 {
		e.recutAcct()
		n++
	}
	e.S.Recuts = uint64(n)
	return n
}

// recutAcct rebuilds the account trie at the depth its size asks for (base accounts plus the
// overlay's: an upper bound that follows from the state, enough for a depth), at least one
// level deeper.
func (e *Engine) recutAcct() {
	t := e.acct
	n := int64(len(e.acctOff))
	for _, l := range t.ov {
		n += int64(len(l))
	}
	d := max(depthFor(n, e.K), t.depth+1)
	nt := newTrie(min(d, 7))
	ws := e.pool()
	nt.ov = map[uint32][][32]byte{}
	kc := ws[0].h.k
	for _, l := range t.ov {
		for _, k := range l {
			h := e.keyHash(&k, true, kc)
			c := nt.cutOf(&h)
			nt.ov[c], _ = e.insertKey(nt.ov[c], k, h, true, kc)
		}
	}
	e.acctBounds(nt)
	nu := (nt.cuts() + 255) / 256
	e.parallel(nu, func(w *worker, i int) {
		for c := i * 256; c < min((i+1)*256, nt.cuts()); c++ {
			e.queue(w, nt, false, nil, uint32(c), uint32(c+1), int(nt.depth), nt.cut(uint32(c)))
		}
	}, e.flush)
	e.allUpper(ws[0], nt, false, nil)
	// publish in place: e.acct stays the same *Trie
	t.pos, t.bounds, t.ov, t.depth = nt.pos, nt.bounds, nt.ov, nt.depth
	t.bb, t.ro, t.mod = nil, false, nil
	t.over = nil
	t.memo = nil // its cuts change
}

// recut rebuilds a storage trie at the depth its current size asks for.
func (e *Engine) recut(t *Trie, ah *[32]byte) {
	// count the live keys of the whole trie, 256 cuts per item
	cnt := make([]int64, (t.cuts()+255)/256)
	e.parallel(len(cnt), func(w *worker, i int) {
		e.flush(w)
		w.kvs = w.kvs[:0]
		w.arena = w.arena[:0]
		e.storageRun(w, t, uint32(i*256), uint32(min((i+1)*256, t.cuts())))
		cnt[i] = int64(len(w.kvs))
	}, nil)
	var n int64
	for _, x := range cnt {
		n += x
	}
	w := e.pool()[0]
	d := depthFor(n, e.K)
	if d <= t.depth {
		d = t.depth + 1
	}
	nt := newTrie(d)
	nt.noBase, nt.vals, nt.addr = t.noBase, t.vals, t.addr
	// the overlay lists at the new depth: a new cut lies inside one old cut (the depth only
	// grows), so the old cuts split in parallel and their new lists merge without overlap
	type cutList struct {
		c uint32
		l [][32]byte
	}
	olds := make([]uint32, 0, len(t.ov))
	for c := range t.ov {
		olds = append(olds, c)
	}
	parts := make([][]cutList, (len(olds)+63)/64)
	e.parallel(len(parts), func(w *worker, i int) {
		m := map[uint32][][32]byte{}
		for _, oc := range olds[i*64 : min((i+1)*64, len(olds))] {
			for _, k := range t.ov[oc] {
				h := e.keyHash(&k, false, w.h.k)
				c := nt.cutOf(&h)
				m[c], _ = e.insertKey(m[c], k, h, false, w.h.k)
			}
		}
		for c, l := range m {
			parts[i] = append(parts[i], cutList{c, l})
		}
	}, nil)
	nt.ov = map[uint32][][32]byte{}
	for _, p := range parts {
		for _, x := range p {
			nt.ov[x.c] = x.l
		}
	}
	start, end := t.bound(0), t.bound(t.cuts())
	nt.bounds[0], nt.bounds[len(nt.bounds)-1] = start, end
	if !t.noBase {
		next := 0
		b := e.baseBytes(start, end)
		p := start
		for len(b) > 0 {
			r := &w.rec
			if err := era.Decode(b, r); err != nil {
				panic(err)
			}
			if r.Tag == era.TagSlot {
				c := int(nt.cutOf((*[32]byte)(r.Key)))
				for next <= c {
					nt.bounds[next] = p
					next++
				}
			}
			b = b[r.Len:]
			p += uint64(r.Len)
		}
		for ; next < nt.cuts(); next++ {
			nt.bounds[next] = end
		}
	} else {
		for i := range nt.bounds {
			nt.bounds[i] = start
		}
	}
	e.parallel((nt.cuts()+255)/256, func(w *worker, i int) {
		for c := i * 256; c < min((i+1)*256, nt.cuts()); c++ {
			e.queue(w, nt, true, ah, uint32(c), uint32(c+1), int(nt.depth), nt.cut(uint32(c)))
		}
	}, e.flush)
	e.allUpper(w, nt, true, ah)
	// publish: the Trie value is swapped in place so the map keeps its pointer
	t.pos, t.bounds, t.ov, t.depth = nt.pos, nt.bounds, nt.ov, nt.depth
	t.bb, t.ro, t.mod = nil, false, nil
	t.over = nil // over-limit cuts are found again by the Roots after this one
	t.memo = nil // its cuts change
}
