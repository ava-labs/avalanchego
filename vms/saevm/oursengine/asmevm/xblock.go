//go:build rwtrace

package asmevm

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/holiman/uint256"
)

// Cross-block optimistic execution for catch-up (patch sixgg, E4 of the faster firewood sixgg
// brief). W workers run whole blocks n+1..n+L (exact block context, from the window) against
// the committed state; the committer takes the blocks in order: per tx, every recorded read
// value must equal the current value (value validation, as P1's pxValid), else that tx is
// re-executed on the committed State; then it applies the writes and publishes the block's
// write sets, which the workers' replicas apply before their next speculation. A block nobody
// has started yet is executed by the committer itself. Capture, validation and apply are
// copies of P1's pxCapture, pxSpec, pxValid, pxApply and bpVal (parallel_test.go, pin
// 2560733b), renamed so the P1 tests still build; bpVal reads through the storage base.

type xbAcct struct {
	exists          bool
	bal, nonce, cod bool
	// add: balance is the amount the tx added to the balance (a credit: it wrote the balance
	// without reading it, and the account exists after it), not the value; the result holds
	// whatever the balance was, so the balance is no read of the tx (see xbCaptureRun)
	add     bool
	balance uint256.Int
	n               uint64
	code            []byte
}

type xbWrites struct {
	acct    map[Address]*xbAcct
	slots   map[slotKey]uint256.Int
	cbDelta uint256.Int
	cbNeg   bool
}

type xbKey struct {
	kind byte // 'e' existence/emptiness, 'b' balance, 'n' nonce, 'c' code, 's' storage
	addr Address
	slot Hash
}

type xbRec struct {
	ok      bool
	reads   map[xbKey]any
	w       xbWrites
	readsCB bool
	res     ExecResult
	gas     uint64
	warm    []slotKey // every slot the tx touched, as used (raw key; Spec's alias count)
	// coins: the run used a multicoin balance or a native asset precompile (State.coinUse),
	// which neither the reads nor the write set hold (ok is false)
	coins bool
	// baseFee: the run read the block's base fee (BASEFEE ran) or ran init code; false: the
	// result holds under any base fee at the same gas price. price: the run read its gas price
	// or the sender's balance (GASPRICE, BALANCE or SELFBALANCE of the origin ran), ran init code
	// or its sender has code; false: at another gas price only the sender's balance and the
	// coinbase move, by the price difference times the receipt gas (the coinbase's balance reads
	// are refused apart: readsCB)
	baseFee, price bool
	// rcpt: the consensus receipt encoded by the worker at cumulative gas rcptCum (nil: not
	// encoded); the committer takes it when its cumulative gas is the same
	rcpt    []byte
	rcptCum uint64
	// list: reads as a slice (the committer's validation walks it); slotWs: the tx's slot
	// writes in tier0 form, old values from the reads (the committer takes them when the reads
	// hold; slotOK false: it derives them itself)
	list   []xbRead
	slotWs []T0Write
	slotOK bool
}

type xbRead struct {
	k xbKey
	v any
}

// xbFinish fills rec.list and rec.slotWs once rec.reads is final (on the worker). Every written
// slot is warm, so it is in the reads with its value before the tx; slotWs is left out when an
// account the tx wrote was created or deleted or got code, or a written slot's account did not
// exist before the tx.
func xbFinish(rec *xbRec) {
	rec.list = make([]xbRead, 0, len(rec.reads))
	for k, v := range rec.reads {
		rec.list = append(rec.list, xbRead{k, v})
	}
	for _, x := range rec.w.acct {
		if !x.exists || x.cod {
			return
		}
	}
	for k, v := range rec.w.slots {
		r, ok := rec.reads[xbKey{kind: 's', addr: k.addr, slot: k.slot}]
		old, isVal := r.(uint256.Int)
		if !ok || !isVal {
			rec.slotWs = nil
			return
		}
		if old != v {
			rec.slotWs = append(rec.slotWs, T0Write{Addr: k.addr, Kind: T0Slot, Slot: T0Mask(k.slot), Old: old.Bytes32(), New: v.Bytes32()})
		}
	}
	rec.slotOK = true
}

// xbFeeDeps is xbRec.baseFee and xbRec.price for the tx just run on st (st.FeeReads cleared
// before it; reverted frames count): what its ops read that the fee moves, a create (init
// code), or a sender with code (its own frames move its balance).
func xbFeeDeps(st *State, tx *Tx, ptr map[Address]*account) (baseFee, price bool) {
	if tx.To == nil {
		return true, true
	}
	for _, e := range st.journal {
		if e.kind == jCreateAcct {
			return true, true
		}
	}
	if a := st.accounts[tx.Caller]; a != nil && len(a.code) > 0 || ptr[tx.Caller] != nil && len(ptr[tx.Caller].code) > 0 {
		price = true
	}
	return st.FeeReads&FeeReadBaseFee != 0, price || st.FeeReads&FeeReadPrice != 0
}

// xbReprice is rec at gas price p instead of q (rec.price false): the sender's balance moves
// by (q - p) times the receipt gas and the coinbase by the opposite (the base fee goes to the
// coinbase); ok false when the sender's balance before the tx (as read) does not cover the gas
// limit at the higher price plus the value, where the run could have gone another way.
func xbReprice(rec *xbRec, from Address, gasLimit uint64, value, p, q *uint256.Int) (*xbRec, bool) {
	bal, ok := rec.reads[xbKey{kind: 'b', addr: from}].(uint256.Int)
	x := rec.w.acct[from]
	if !ok || x == nil || !x.exists || !x.bal || x.add {
		return nil, false
	}
	hi := p
	if q.Gt(p) {
		hi = q
	}
	need, of := new(uint256.Int).MulOverflow(hi, uint256.NewInt(gasLimit))
	if of {
		return nil, false
	}
	if _, of = need.AddOverflow(need, value); of || bal.Lt(need) {
		return nil, false
	}
	d := new(uint256.Int).Mul(new(uint256.Int).Sub(maxU(p, q), minU(p, q)), uint256.NewInt(rec.gas))
	up := p.Gt(q) // the new price is higher: the sender pays d more, the coinbase gets d more
	nr := *rec
	nr.w.acct = make(map[Address]*xbAcct, len(rec.w.acct))
	for a, v := range rec.w.acct {
		nr.w.acct[a] = v
	}
	nx := *x
	if up {
		if nx.balance.Lt(d) {
			return nil, false
		}
		nx.balance.Sub(&nx.balance, d)
	} else {
		nx.balance.Add(&nx.balance, d)
	}
	nr.w.acct[from] = &nx
	// coinbase: the signed delta (cbNeg: negative) plus d (up) or minus d
	pos := up != rec.w.cbNeg // d has the sign of the delta: magnitudes add
	if pos {
		nr.w.cbDelta.Add(&rec.w.cbDelta, d)
	} else if rec.w.cbDelta.Lt(d) {
		nr.w.cbDelta.Sub(d, &rec.w.cbDelta)
		nr.w.cbNeg = !rec.w.cbNeg
	} else {
		nr.w.cbDelta.Sub(&rec.w.cbDelta, d)
	}
	if nr.w.cbDelta.IsZero() {
		nr.w.cbNeg = false
	}
	return &nr, true
}

func maxU(a, b *uint256.Int) *uint256.Int {
	if a.Gt(b) {
		return a
	}
	return b
}

func minU(a, b *uint256.Int) *uint256.Int {
	if a.Gt(b) {
		return b
	}
	return a
}

func xbVal(st *State, k xbKey) any {
	a := st.accounts[k.addr]
	switch {
	case k.kind == 'e':
		return [2]bool{a != nil, a == nil || a.empty()}
	case a == nil:
		return nil
	case k.kind == 'b':
		return a.balance
	case k.kind == 'n':
		return a.nonce
	case k.kind == 'c':
		return a.codeHash
	}
	v, _ := st.stored(a, &k.slot)
	return v
}

// xbEq is xbVal(st, k) == v without boxing the current value (xbVal's result is an any: a
// balance, slot or code hash read allocated 32 bytes per compare on the committer).
func xbEq(st *State, k xbKey, v any) bool {
	a := st.accounts[k.addr]
	switch {
	case k.kind == 'e':
		x, ok := v.([2]bool)
		return ok && x == [2]bool{a != nil, a == nil || a.empty()}
	case a == nil:
		return v == nil
	case k.kind == 'b':
		x, ok := v.(uint256.Int)
		return ok && x == a.balance
	case k.kind == 'n':
		x, ok := v.(uint64)
		return ok && x == a.nonce
	case k.kind == 'c':
		x, ok := v.(Hash)
		return ok && x == a.codeHash
	}
	x, ok := v.(uint256.Int)
	if !ok {
		return false
	}
	cur, _ := st.stored(a, &k.slot)
	return x == cur
}

func xbCapture(st *State, blk *BlockContext, fr ccFeeRules, tx *Tx, txc TxContext, feeFloor uint64, coinbaseNew, asm bool, reads map[xbKey]any) (ExecResult, uint64, xbWrites, bool, map[Address]*account) {
	return xbCaptureRun(st, blk.Coinbase, reads, func() (ExecResult, uint64) {
		return ccExec(st, blk, fr, tx, txc, feeFloor, coinbaseNew, asm)
	})
}

// xbCaptureRun is xbCapture of any state work run does on st (a tx, or a block's pre-tx group),
// cb being the block's coinbase.
func xbCaptureRun(st *State, cb Address, reads map[xbKey]any, run func() (ExecResult, uint64)) (ExecResult, uint64, xbWrites, bool, map[Address]*account) {
	var cbPre uint256.Int
	if a := st.accounts[cb]; a != nil {
		cbPre = a.balance
	}
	w := xbWrites{acct: map[Address]*xbAcct{}, slots: map[slotKey]uint256.Int{}}
	// pre: the balance before the tx's first balance write, taken when no balance read came
	// before it (preSet); a balance read later still makes the write absolute (balRead)
	type fields struct {
		b, n, c bool
		preSet  bool
		pre     uint256.Int
	}
	wrote := map[Address]*fields{}
	ptr := map[Address]*account{}
	var balRead map[Address]bool
	readsCB := false
	st.RWHook = func(kind byte, addr Address, slot Hash, write bool) {
		if _, ok := ptr[addr]; !ok {
			ptr[addr] = st.accounts[addr]
		}
		if addr == cb && kind == 'b' {
			readsCB = readsCB || !write
			return
		}
		if write {
			f := wrote[addr]
			if f == nil {
				f = &fields{}
				wrote[addr] = f
			}
			switch kind {
			case 'b':
				if !f.b && !balRead[addr] { // the write loads the account right after this hook anyway
					if a := st.acct(addr); a != nil {
						f.pre = a.balance
					}
					f.preSet = true
				}
				f.b = true
			case 'n':
				f.n = true
			case 'c':
				f.c = true
			}
			return
		}
		if kind == 'b' {
			if balRead == nil {
				balRead = map[Address]bool{}
			}
			balRead[addr] = true
		}
		if reads != nil {
			reads[xbKey{kind: kind, addr: addr, slot: slot}] = nil
		}
	}
	defer func() { st.RWHook = nil }()
	st.FeeReads = 0
	r, g := run()
	if reads != nil {
		for e := range st.warmSlot.all() {
			reads[xbKey{kind: 's', addr: e.k.addr, slot: e.k.slot}] = nil
		}
		for e := range st.warmAddr.all() {
			if e.k != cb {
				reads[xbKey{kind: 'c', addr: e.k}] = nil
				if _, ok := ptr[e.k]; !ok {
					ptr[e.k] = st.accounts[e.k]
				}
			}
		}
	}
	for _, e := range st.journal {
		if e.kind == jCreateAcct {
			if _, ok := wrote[e.addr]; !ok {
				wrote[e.addr] = &fields{}
			}
		}
	}
	for _, t := range st.txAccts {
		if _, ok := wrote[t.addr]; !ok {
			wrote[t.addr] = &fields{}
		}
	}
	for addr, f := range wrote {
		if addr == cb {
			continue
		}
		a := st.accounts[addr]
		x := &xbAcct{exists: a != nil, bal: f.b, nonce: f.n, cod: f.c}
		if a != nil {
			x.balance, x.n, x.code = a.balance, a.nonce, a.code
		}
		if f.b && !balRead[addr] {
			if f.preSet && a != nil && !a.balance.Lt(&f.pre) { // a credit
				x.add = true
				x.balance.Sub(&a.balance, &f.pre)
			} else if reads != nil { // absolute: the value it was written from is a read
				reads[xbKey{kind: 'b', addr: addr}] = nil
			}
		}
		w.acct[addr] = x
	}
	if n := len(st.orig.idx); n > 8 { // sized once for the slots the tx wrote (a map holds 8 without growing)
		w.slots = make(map[slotKey]uint256.Int, n)
	}
	for e := range st.orig.all() {
		var v uint256.Int
		if a := st.accounts[e.k.addr]; a != nil {
			v, _ = st.stored(a, &e.k.slot)
		}
		if v != e.v {
			w.slots[e.k] = v
		}
	}
	var cbPost uint256.Int
	if a := st.accounts[cb]; a != nil {
		cbPost = a.balance
	}
	if cbPost.Lt(&cbPre) {
		w.cbNeg = true
		w.cbDelta.Sub(&cbPre, &cbPost)
	} else {
		w.cbDelta.Sub(&cbPost, &cbPre)
	}
	return r, g, w, readsCB, ptr
}

// xbSpec runs tx on replica ws and leaves ws unchanged (the reads hold the values it saw).
func xbSpec(ws *State, blk *BlockContext, fr ccFeeRules, tx *Tx, txc TxContext, feeFloor uint64, coinbaseNew, asm bool) (rec *xbRec) {
	rec = &xbRec{reads: map[xbKey]any{}}
	var ptr map[Address]*account
	defer func() {
		if p := recover(); p != nil {
			rec.ok = false
			ws.RWHook = nil
		}
		ws.revert(0)
		for addr, a := range ptr {
			if a != nil && ws.accounts[addr] != a {
				a.destroyed = false
				ws.accounts[addr] = a
			}
		}
		for k := range rec.reads {
			rec.reads[k] = xbVal(ws, k)
		}
	}()
	ws.coinUse = false
	rec.res, rec.gas, rec.w, rec.readsCB, ptr = xbCapture(ws, blk, fr, tx, txc, feeFloor, coinbaseNew, asm, rec.reads)
	for e := range ws.warmSlot.all() { // before the revert drops them
		rec.warm = append(rec.warm, e.k)
	}
	rec.baseFee, rec.price = xbFeeDeps(ws, tx, ptr)
	rec.coins = ws.coinUse // its reads and writes miss the multicoin balances: not usable
	rec.ok = !rec.coins
	return rec
}

// xbValid reports whether rec still holds, else why not: 'M' it used multicoin balances, 'p'
// the speculation panicked (a key outside the prestate), 'C' it read the coinbase balance, or
// the kind of a changed read.
func xbValid(st *State, rec *xbRec) (bool, byte) {
	switch {
	case rec.coins:
		return false, 'M'
	case !rec.ok:
		return false, 'p'
	case rec.readsCB:
		return false, 'C'
	}
	for k, v := range rec.reads {
		if !xbEq(st, k, v) {
			return false, k.kind
		}
	}
	return true, 0
}

func xbApply(st *State, w *xbWrites, cb Address) {
	for addr, x := range w.acct {
		if !x.exists {
			continue
		}
		st.getOrNew(addr)
		if x.bal && x.add {
			if !x.balance.IsZero() {
				st.AddBalance(addr, &x.balance)
			}
		} else if x.bal {
			cur := st.Balance(addr)
			if x.balance.Gt(&cur) {
				st.AddBalance(addr, new(uint256.Int).Sub(&x.balance, &cur))
			} else if x.balance.Lt(&cur) {
				st.SubBalance(addr, new(uint256.Int).Sub(&cur, &x.balance))
			}
		}
		if x.nonce {
			st.SetNonce(addr, x.n)
		}
		if x.cod {
			st.SetCode(addr, x.code)
		}
	}
	for k, v := range w.slots {
		if x := w.acct[k.addr]; x == nil || x.exists {
			st.SetStorage(k.addr, k.slot, v)
		}
	}
	if w.cbNeg {
		st.SubBalance(cb, &w.cbDelta)
	} else if !w.cbDelta.IsZero() {
		st.AddBalance(cb, &w.cbDelta)
	}
	for addr, x := range w.acct {
		if !x.exists && st.accounts[addr] != nil {
			st.forgetIndexed(addr)
			delete(st.accounts, addr)
		}
	}
}

// xbUndo is what reverses one applied tx on a replica: the old fields and slot values, and
// the account objects the tx removed (put back as they were, storage included).
type xbUndo struct {
	w    xbWrites
	gone map[Address]*account
}

func xbInverse(st *State, w *xbWrites) xbUndo {
	u := xbUndo{w: xbWrites{acct: map[Address]*xbAcct{}, slots: map[slotKey]uint256.Int{}, cbDelta: w.cbDelta, cbNeg: !w.cbNeg}}
	for addr, x := range w.acct {
		a := st.accounts[addr]
		if a != nil && !x.exists {
			if u.gone == nil {
				u.gone = map[Address]*account{}
			}
			u.gone[addr] = a
			continue
		}
		y := &xbAcct{exists: a != nil, bal: x.bal, nonce: x.nonce, cod: x.cod}
		if a != nil {
			y.balance, y.n, y.code = a.balance, a.nonce, a.code
		}
		u.w.acct[addr] = y
	}
	for k := range w.slots {
		if _, ok := u.gone[k.addr]; ok {
			continue
		}
		var v uint256.Int
		if a := st.accounts[k.addr]; a != nil {
			v, _ = st.stored(a, &k.slot)
		}
		u.w.slots[k] = v
	}
	if w.cbDelta.IsZero() {
		u.w.cbNeg = false
	}
	return u
}

func (u *xbUndo) undo(st *State, cb Address) {
	for addr, a := range u.gone {
		st.accounts[addr] = a
		st.resetCodes()
		if st.vtab != nil { // forgetIndexed zeroed its native cold SLOAD values
			w := wordsOf(&addr)
			for k, v := range a.storage {
				st.vtab.update(&w, new(uint256.Int).SetBytes32(k[:]), &v)
			}
		}
	}
	xbApply(st, &u.w, cb)
	st.beginTx()
}

// xbBlock is one block's speculation, or its committed write sets.
type xbBlock struct {
	recs  []*xbRec
	cbNew bool
	at    int // committed blocks when the speculation started
	done  chan struct{}
	w     []*xbWrites // committed write sets, in tx order (published to the replicas)
}

// XBTrust (diagnostic only) applies every speculation that did not panic without validation:
// the gate must then fail, which shows it can see a conflict.
var XBTrust bool

// XBRun is one cross-block run.
type XBRun struct {
	WindowRun
	Workers, Lookahead                int
	Wait                              bool
	Specced, SelfRun, CleanBlocks     int // blocks taken from a worker; run by the committer alone; taken with every tx valid
	Late                              int // wait false: blocks the committer ran itself while their speculation still ran
	TxsValid, TxsReexec, SpecPanics   int
	Lag                               float64       // mean committed-block lag of the speculations (block - committed at start)
	WaitT, CheckT, ApplyT, ReT, SelfT time.Duration // committer: waiting for a worker, validation, apply, re-execution, own blocks
	BusyT, CatchT                     time.Duration // workers, summed: busy (catch-up included), replica catch-up
	CPU                               time.Duration // process user+system CPU over the timed loop
	Why                               map[byte]int  // re-executed txs by first failed check (see xbValid)
	LagTx, LagOK                      [64]int       // taken txs and valid txs by speculation lag (block - committed at its start; 63 = 63 or more)
	Final                             *State
}

func (r *XBRun) String() string {
	return fmt.Sprintf("W=%d L=%d wait=%v wall_ms=%.1f ggas_per_s=%.3f passed=%v | blocks taken from workers %d (all txs valid %d = %.1f%% of all blocks), run by the committer %d (speculation late %d) | txs valid %d, re-executed %d, speculative panics %d | mean lag %.2f blocks | committer: wait %.1f ms, validate %.1f, apply %.1f, re-exec %.1f, own blocks %.1f | workers busy %.1f ms (catch-up %.1f) | cpu %.2f s (%.2f cores) | gcs %d | why %s | valid by lag %s | %v",
		r.Workers, r.Lookahead, r.Wait, xbMs(r.Elapsed), float64(r.Gas)/r.Elapsed.Seconds()/1e9, r.Passed(), r.Specced, r.CleanBlocks, 100*float64(r.CleanBlocks)/float64(r.Blocks), r.SelfRun, r.Late,
		r.TxsValid, r.TxsReexec, r.SpecPanics, r.Lag, xbMs(r.WaitT), xbMs(r.CheckT), xbMs(r.ApplyT), xbMs(r.ReT), xbMs(r.SelfT), xbMs(r.BusyT), xbMs(r.CatchT),
		r.CPU.Seconds(), r.CPU.Seconds()/r.Elapsed.Seconds(), r.GCs, xbWhy(r.Why), xbLag(r), &r.WindowRun)
}

func xbWhy(m map[byte]int) string {
	s := ""
	for _, k := range []byte("snbecCNp") {
		if m[k] > 0 {
			s += fmt.Sprintf("%c=%d ", k, m[k])
		}
	}
	return s
}

func xbLag(r *XBRun) string {
	s := ""
	for _, b := range [][2]int{{0, 0}, {1, 1}, {2, 2}, {3, 4}, {5, 8}, {9, 16}, {17, 32}, {33, 63}} {
		t, ok := 0, 0
		for l := b[0]; l <= b[1]; l++ {
			t, ok = t+r.LagTx[l], ok+r.LagOK[l]
		}
		if t > 0 {
			s += fmt.Sprintf("%d-%d:%d/%d ", b[0], b[1], ok, t)
		}
	}
	return s
}

func xbMs(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1e6 }

func xbCPU() time.Duration {
	var u syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &u)
	return time.Duration(u.Utime.Nano() + u.Stime.Nano())
}

// CrossBlock replays the window at path with W workers speculating up to L blocks past the
// committed state (W = 0: plain sequential replay in the same harness, no capture). setup
// sees every State (the committed one first, then each replica) before the timer and returns
// the function called before each block run on that State (nil: none). wait false: the
// committer never waits for a speculation still running, it runs that block itself. roots
// false skips the receipts-root check (windows without settlement headers).
func CrossBlock(path string, asm bool, W, L int, wait, roots bool, setup func(st *State) func()) (*XBRun, error) {
	w, why, err := ccLoad(path)
	if err != nil {
		return nil, err
	}
	nb := len(w.Blocks)
	blks := make([]BlockContext, nb)
	txs := make([][]Tx, nb)
	txcs := make([][]TxContext, nb)
	n := 0
	for bi := range w.Blocks {
		b := &w.Blocks[bi]
		var failed map[Hash][]byte
		if blks[bi], failed, err = ccBlockCtx(b); err != nil {
			return nil, err
		}
		for ti := range b.Txs {
			tx, txc := ccTxOf(&b.Txs[ti], failed)
			txs[bi] = append(txs[bi], tx)
			txcs[bi] = append(txcs[bi], txc)
			n++
		}
	}
	mk := func(name string) (*State, func(), error) {
		st, acc, slots, err := ccPrestate(w)
		if err != nil {
			return nil, nil, err
		}
		for bi := range blks {
			ccStrict(st, &blks[bi], acc, slots, func() string { return name })
		}
		var hook func()
		if setup != nil {
			hook = setup(st)
		}
		if hook == nil {
			hook = func() {}
		}
		return st, hook, nil
	}
	st, hook, err := mk("committed")
	if err != nil {
		return nil, err
	}
	type replica struct {
		st   *State
		hook func()
		next int // committed blocks applied
	}
	reps := make([]*replica, W)
	for i := range reps {
		rs, h, err := mk("speculative")
		if err != nil {
			return nil, err
		}
		reps[i] = &replica{st: rs, hook: h}
	}
	xb := make([]xbBlock, nb)
	for bi := range xb {
		xb[bi].done = make(chan struct{})
	}
	claim := make([]atomic.Int32, nb) // 0 free, 1 worker, 2 committer
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	committed, issued := 0, 0
	busy := make([]time.Duration, W)
	catch := make([]time.Duration, W)
	panics := make([]int, W)
	var wg sync.WaitGroup
	for i, r := range reps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				for {
					if issued < committed {
						issued = committed
					}
					if issued >= nb || issued < committed+L {
						break
					}
					cond.Wait()
				}
				if issued >= nb {
					mu.Unlock()
					return
				}
				b := issued
				issued++
				c := committed
				mu.Unlock()
				if !claim[b].CompareAndSwap(0, 1) {
					continue
				}
				t0 := time.Now()
				for ; r.next < c; r.next++ { // catch up to the committed state
					p := &w.Blocks[r.next]
					for _, op := range p.PreOps {
						r.st.SetStorage(addrOf(op.Address), hashOf(*op.Slot), u256Of(*op.Value))
					}
					for _, ws := range xb[r.next].w {
						xbApply(r.st, ws, blks[r.next].Coinbase)
					}
					ccEndOps(r.st, p)
				}
				r.st.beginTx()
				t1 := time.Now()
				catch[i] += t1.Sub(t0)
				// speculate block b: its txs in order, each applied for the next, all undone after
				r.hook()
				p := &w.Blocks[b]
				cb := blks[b].Coinbase
				type pre struct {
					a    Address
					s    Hash
					v    uint256.Int
					had  bool
					acct bool
				}
				var pres []pre
				for _, op := range p.PreOps {
					a, s := addrOf(op.Address), hashOf(*op.Slot)
					x := pre{a: a, s: s}
					if ac := r.st.accounts[a]; ac != nil {
						x.acct = true
						x.v, _ = r.st.stored(ac, &s)
					}
					pres = append(pres, x)
					r.st.SetStorage(a, s, u256Of(*op.Value))
				}
				r.st.beginTx()
				cbAcct := r.st.accounts[cb]
				blk := &xb[b]
				blk.cbNew, blk.at = cbAcct == nil, c
				var undo []xbUndo
				for ti := range txs[b] {
					rec := xbSpec(r.st, &blks[b], p.FeeRules, &txs[b][ti], txcs[b][ti], p.Txs[ti].FeeFloorGas, blk.cbNew, asm)
					blk.recs = append(blk.recs, rec)
					if !rec.ok {
						panics[i]++
						continue
					}
					undo = append(undo, xbInverse(r.st, &rec.w))
					xbApply(r.st, &rec.w, cb)
					r.st.beginTx()
				}
				for j := len(undo) - 1; j >= 0; j-- {
					undo[j].undo(r.st, cb)
				}
				if cbAcct == nil && r.st.accounts[cb] != nil {
					delete(r.st.accounts, cb)
					r.st.resetCodes()
				} else if cbAcct != nil && r.st.accounts[cb] != cbAcct {
					r.st.accounts[cb] = cbAcct
					r.st.resetCodes()
				}
				for j := len(pres) - 1; j >= 0; j-- {
					x := pres[j]
					if x.acct {
						r.st.SetStorage(x.a, x.s, x.v)
					} else if r.st.accounts[x.a] != nil {
						r.st.forgetIndexed(x.a)
						delete(r.st.accounts, x.a)
					}
				}
				r.st.beginTx()
				busy[i] += time.Since(t0)
				close(blk.done)
			}
		}()
	}

	out := &XBRun{Workers: W, Lookahead: L, Wait: wait, Why: map[byte]int{}}
	res := make([]ExecResult, 0, n)
	gas := make([]uint64, 0, n)
	runtime.GC()
	hugePages()
	ccTimer(true)
	cpu0 := xbCPU()
	t0 := time.Now()
	lag := 0
	for bi := range w.Blocks {
		hook()
		b := &w.Blocks[bi]
		for _, op := range b.PreOps {
			st.SetStorage(addrOf(op.Address), hashOf(*op.Slot), u256Of(*op.Value))
		}
		cbNew := st.accounts[blks[bi].Coinbase] == nil
		blk := &xb[bi]
		mine := W == 0 || claim[bi].CompareAndSwap(0, 2)
		if !mine && !wait {
			select {
			case <-blk.done:
			default:
				mine = true // still running: its result is ignored
				out.Late++
			}
		}
		if mine {
			s0 := time.Now()
			for ti := range txs[bi] {
				if W == 0 {
					r, g := ccExec(st, &blks[bi], b.FeeRules, &txs[bi][ti], txcs[bi][ti], b.Txs[ti].FeeFloorGas, cbNew, asm)
					res, gas = append(res, r), append(gas, g)
					continue
				}
				r, g, ws, _, _ := xbCapture(st, &blks[bi], b.FeeRules, &txs[bi][ti], txcs[bi][ti], b.Txs[ti].FeeFloorGas, cbNew, asm, nil)
				res, gas = append(res, r), append(gas, g)
				blk.w = append(blk.w, &ws)
			}
			out.SelfRun++
			out.SelfT += time.Since(s0)
		} else {
			w0 := time.Now()
			<-blk.done
			out.WaitT += time.Since(w0)
			out.Specced++
			lag += bi - blk.at
			clean := true
			for ti := range txs[bi] {
				rec := blk.recs[ti]
				c0 := time.Now()
				ok, why := false, byte('N') // N: coinbase existence differs
				if blk.cbNew == cbNew {
					ok, why = xbValid(st, rec)
				}
				if XBTrust && rec.ok {
					ok = true
				}
				if !ok {
					out.Why[why]++
				}
				lg := min(bi-blk.at, 63)
				out.LagTx[lg]++
				if ok {
					out.LagOK[lg]++
				}
				c1 := time.Now()
				out.CheckT += c1.Sub(c0)
				if ok {
					xbApply(st, &rec.w, blks[bi].Coinbase)
					st.beginTx()
					out.ApplyT += time.Since(c1)
					res, gas = append(res, rec.res), append(gas, rec.gas)
					blk.w = append(blk.w, &rec.w)
					out.TxsValid++
					continue
				}
				clean = false
				r, g, ws, _, _ := xbCapture(st, &blks[bi], b.FeeRules, &txs[bi][ti], txcs[bi][ti], b.Txs[ti].FeeFloorGas, cbNew, asm, nil)
				res, gas = append(res, r), append(gas, g)
				blk.w = append(blk.w, &ws)
				out.TxsReexec++
				out.ReT += time.Since(c1)
			}
			if clean {
				out.CleanBlocks++
			}
			blk.recs = nil
		}
		ccEndOps(st, b)
		if W > 0 {
			mu.Lock()
			committed = bi + 1
			mu.Unlock()
			cond.Broadcast()
		}
	}
	out.Elapsed = time.Since(t0)
	out.CPU = xbCPU() - cpu0
	ccTimer(false)
	wg.Wait()
	for i := range W {
		out.BusyT += busy[i]
		out.CatchT += catch[i]
		out.SpecPanics += panics[i]
	}
	if out.Specced > 0 {
		out.Lag = float64(lag) / float64(out.Specced)
	}
	out.Name, out.Blocks, out.Txs, out.Unsupported = filepath.Base(path), nb, n, len(why)
	out.Allocs, out.AllocBytes, out.GCs = ccAllocs, ccAllocB, ccGCs
	out.Final = st
	rcpts := make([][]Receipt, nb)
	i := 0
	for bi := range w.Blocks {
		cum := uint64(0)
		for ti := range w.Blocks[bi].Txs {
			jt := &w.Blocks[bi].Txs[ti]
			cum += gas[i]
			out.Gas += jt.Result.GasUsed
			rcpts[bi] = append(rcpts[bi], ccReceipt(jt, &res[i], cum))
			if bad := ccCheck(&res[i], gas[i], jt); len(bad) > 0 {
				out.Fails = append(out.Fails, fmt.Sprintf("block %d tx %d %s: %v", w.Blocks[bi].Number, ti, jt.Hash, bad))
			}
			i++
		}
	}
	out.Roots = &ccRoots{}
	if roots {
		if out.Roots, err = ccRootCheck(path, w, rcpts); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// StateDiff counts the accounts whose existence, balance, nonce, code hash or storage differ
// between two final States (storage through each State's base; zero equals absent).
func StateDiff(a, b *State) (n int, first string) {
	seen := map[Address]bool{}
	cmp := func(x, y *State, addr Address) {
		if seen[addr] {
			return
		}
		seen[addr] = true
		p, q := x.accounts[addr], y.accounts[addr]
		bad := ""
		switch {
		case (p == nil) != (q == nil):
			bad = "existence"
		case p == nil:
		case p.balance != q.balance:
			bad = "balance"
		case p.nonce != q.nonce:
			bad = "nonce"
		case p.codeHash != q.codeHash:
			bad = "code"
		default:
			for _, m := range []map[Hash]uint256.Int{p.storage, q.storage} {
				for k := range m {
					u, _ := x.stored(p, &k)
					v, _ := y.stored(q, &k)
					if u != v && bad == "" {
						bad = fmt.Sprintf("slot %x", k[:])
					}
				}
			}
		}
		if bad != "" {
			n++
			if first == "" {
				first = fmt.Sprintf("0x%x %s", addr[:], bad)
			}
		}
	}
	for addr := range a.accounts {
		cmp(a, b, addr)
	}
	for addr := range b.accounts {
		cmp(b, a, addr)
	}
	return
}
