//go:build rwtrace

package asmevm

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/holiman/uint256"
)

// Cross-block speculation as the executor of real blocks (E4 of the faster firewood sixgg
// brief on Chain, catch-up milestone 2 step 4). The driver offers blocks ahead of execution
// (Ahead); W workers each keep a replica State over the Chain's Loader and speculate an offered
// block up to L past the committed state, every tx on top of the block's earlier speculated txs,
// recording the value of every key it read (xbSpec). Exec commits blocks in order on the Chain's
// State: a tx whose recorded reads all equal the committed values is applied from its
// speculation, any other is re-executed there; the tier0 facts are computed on the committed
// State exactly as Chain.Exec computes them (account fields against the shadow, slot old values
// read before the apply), and the block's write sets go to the replicas. The SAE execution base
// fee depends on the previous block's gas, so an offered block carries a predicted fee: a
// speculation is used only when its base fee and every tx's gas price equal the committed
// block's; Ahead again with a new prediction replaces a speculation not yet committed. With
// NoWait the committer never waits for a running speculation: it runs that block itself.

// SpecStats counts what the committer did.
type SpecStats struct {
	Blocks      int           // committed
	Specced     int           // committed from a speculation
	SelfRun     int           // run by the committer alone (not offered, not started, or no usable speculation)
	FeeMiss     int           // speculations dropped: their base fee or a gas price differs
	Late        int           // NoWait: blocks run by the committer while their speculation still ran
	Clean       int           // committed from a speculation with every tx valid
	Reoffered   int           // speculations replaced by a new offer while started
	FeeKept     int           // started speculations kept when offered again at other fees
	TxsValid    int           // txs applied from a speculation
	TxsReexec   int           // txs re-executed by the committer (self-run blocks included)
	Lag         int           // sum over specced blocks of (block - next block to commit when the speculation started)
	Why         map[byte]int  // invalid speculated txs by first failed check (see xbValid; N: coinbase existence)
	WaitT       time.Duration // committer waiting for a running speculation
	Forwarded   int           // finished speculations whose write sets a later speculation started from
	Repriced    int           // txs of fee-missed speculations taken at another gas price (xbReprice)
	ValidT      time.Duration // committer time on the txs taken from speculations (validation to receipt)
	ReexecT     time.Duration // committer time on the txs it re-executed (a failed validation included)
	Respec      int           // finished speculations offered again because a committed block made them stale
	PrevUsed    int           // blocks committed from the stale speculation while its replacement still ran
	LiveTxs     int           // txs taken from a speculation still running when the commit started (as the worker finished them)
	Reused      int           // txs a re-speculation took from the stale speculation it replaces (their reads held on its replica)
	FeeMissTxs  int           // txs of the FeeMiss blocks
	FeeMissSame int           // of those, txs whose gas price the miss left unchanged
	LateTxs     int           // txs of the Late blocks
	LatePart    int           // Late blocks committed with the txs their speculation had finished
	LatePartTxs int           // the txs taken so
	ReplicaFail int           // replicas stopped by a loader failure (their blocks fall back to the committer)
	// Stale: invalid speculated txs whose first failed check is a read (Why s, n, b, e, c), by
	// the likely source of the stale value: 'I' an earlier tx of the block was re-executed;
	// 'P' a block before it had not finished when the speculation started (not forwarded);
	// 'F' every block before it was forwarded and one of them committed a re-executed tx;
	// 'X' none of these
	Stale map[byte]int
	// FeeWhy: txs of fee-missed speculations dropped ('f' in Why) by reason: 'B' xbRec.baseFee;
	// 'P' its gas price changed and xbRec.price; 'R' xbReprice refused (the balance read does
	// not cover the higher price); 'L' the price is below the committed base fee; 'N' a new
	// coinbase or the sender is the coinbase
	FeeWhy map[byte]int
	// First: blocks with a re-executed tx, by the reason of their first one: a Stale class
	// ('P', 'F', 'X'), 'f' a fee reject, 'p', 'C' or 'M' (see Why), 'L' the late speculation had not
	// reached it, 'N' the coinbase's existence changed, 'S' no speculation used
	First map[byte]int
	// Dep (Speculator.DepStats only): how the txs the committer re-executed in a block depend
	// on each other (see depBlock)
	Dep DepStats
	// Miss (Speculator.MissStats only): where the changed reads of re-executed txs came from
	Miss MissStats
}

// DepStats measures whether a block's re-executed txs could run in parallel: tx j depends on an
// earlier re-executed tx i of its block when j read a key i wrote (the coinbase's fee balance
// excluded, as in validation); the valid txs between them are known before (their speculated
// writes), so only these edges order the re-execution.
type DepStats struct {
	Blocks, Multi int           // blocks with a re-executed tx; with two or more
	Txs, Indep    int           // re-executed txs; of those after their block's first, the ones with no edge
	Serial, Crit  time.Duration // committer time of the re-executed txs; the sum over blocks of the longest chain
	// Frac: blocks with two or more by the share of independent txs after the first, in tenths
	// (index 10: all)
	Frac [11]int
}

func (d *DepStats) add(o *DepStats) {
	d.Blocks += o.Blocks
	d.Multi += o.Multi
	d.Txs += o.Txs
	d.Indep += o.Indep
	d.Serial += o.Serial
	d.Crit += o.Crit
	for i := range d.Frac {
		d.Frac[i] += o.Frac[i]
	}
}

// depTx is one re-executed tx for DepStats: the keys it read and wrote and its committer time.
type depTx struct {
	reads, writes map[xbKey]any
	t             time.Duration
}

// depWrites is the key set w writes (an account write that deletes the account writes every field).
func depWrites(w *xbWrites) map[xbKey]any {
	m := map[xbKey]any{}
	for addr, x := range w.acct {
		m[xbKey{kind: 'e', addr: addr}] = nil
		for _, f := range []struct {
			on   bool
			kind byte
		}{{x.bal, 'b'}, {x.nonce, 'n'}, {x.cod, 'c'}} {
			if f.on || !x.exists {
				m[xbKey{kind: f.kind, addr: addr}] = nil
			}
		}
	}
	for k := range w.slots {
		m[xbKey{kind: 's', addr: k.addr, slot: k.slot}] = nil
	}
	return m
}

// depBlock adds one block's re-executed txs (in order) to d.
func (d *DepStats) depBlock(txs []depTx) {
	if len(txs) == 0 {
		return
	}
	d.Blocks++
	d.Txs += len(txs)
	crit := make([]time.Duration, len(txs))
	var longest time.Duration
	indep := 0
	for j := range txs {
		var before time.Duration
		dep := false
		for i := range j {
			for k := range txs[i].writes {
				if _, ok := txs[j].reads[k]; ok {
					dep = true
					before = max(before, crit[i])
					break
				}
			}
		}
		crit[j] = before + txs[j].t
		longest = max(longest, crit[j])
		d.Serial += txs[j].t
		if j > 0 && !dep {
			indep++
		}
	}
	d.Crit += longest
	d.Indep += indep
	if len(txs) > 1 {
		d.Multi++
		d.Frac[indep*10/(len(txs)-1)]++
	}
}

func (d *DepStats) String() string {
	if d.Txs == 0 {
		return ""
	}
	return fmt.Sprintf("; re-execution dependencies: %d blocks (%d with two or more), %d txs, %d of %d after a block's first independent of the earlier ones, committer time %.2fs vs longest chains %.2fs (%.2fx at most), blocks by independent share in tenths %v",
		d.Blocks, d.Multi, d.Txs, d.Indep, d.Txs-d.Blocks, d.Serial.Seconds(), d.Crit.Seconds(), d.Serial.Seconds()/max(d.Crit.Seconds(), 1e-9), d.Frac)
}

// Add adds o's counts to s (a run that replaced its Speculator).
func (s *SpecStats) Add(o *SpecStats) {
	s.Blocks += o.Blocks
	s.Specced += o.Specced
	s.SelfRun += o.SelfRun
	s.FeeMiss += o.FeeMiss
	s.Late += o.Late
	s.Clean += o.Clean
	s.Reoffered += o.Reoffered
	s.FeeKept += o.FeeKept
	s.TxsValid += o.TxsValid
	s.TxsReexec += o.TxsReexec
	s.Lag += o.Lag
	if s.Why == nil {
		s.Why = map[byte]int{}
	}
	for k, v := range o.Why {
		s.Why[k] += v
	}
	s.WaitT += o.WaitT
	s.FeeMissTxs += o.FeeMissTxs
	s.Forwarded += o.Forwarded
	s.Respec += o.Respec
	s.Repriced += o.Repriced
	s.ValidT += o.ValidT
	s.ReexecT += o.ReexecT
	s.PrevUsed += o.PrevUsed
	s.LiveTxs += o.LiveTxs
	s.Reused += o.Reused
	s.FeeMissSame += o.FeeMissSame
	s.LateTxs += o.LateTxs
	s.LatePart += o.LatePart
	s.LatePartTxs += o.LatePartTxs
	s.ReplicaFail += o.ReplicaFail
	if s.Stale == nil {
		s.Stale = map[byte]int{}
	}
	for k, v := range o.Stale {
		s.Stale[k] += v
	}
	if s.FeeWhy == nil {
		s.FeeWhy = map[byte]int{}
	}
	for k, v := range o.FeeWhy {
		s.FeeWhy[k] += v
	}
	if s.First == nil {
		s.First = map[byte]int{}
	}
	for k, v := range o.First {
		s.First[k] += v
	}
	s.Dep.add(&o.Dep)
	s.Miss.add(&o.Miss)
}

func perTx(d time.Duration, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(d.Microseconds()) / float64(n)
}

func (s *SpecStats) String() string {
	why := ""
	for _, k := range []byte("snbecCNpfM") {
		if s.Why[k] > 0 {
			why += fmt.Sprintf(" %c=%d", k, s.Why[k])
		}
	}
	stale := ""
	for _, k := range []byte("IPFX") {
		if s.Stale[k] > 0 {
			stale += fmt.Sprintf(" %c=%d", k, s.Stale[k])
		}
	}
	for _, k := range []byte("BPRLN") {
		if s.FeeWhy[k] > 0 {
			stale += fmt.Sprintf(" f%c=%d", k, s.FeeWhy[k])
		}
	}
	stale += "; first re-executed tx of a block:"
	for _, k := range []byte("PFXfpCMLNS") {
		if s.First[k] > 0 {
			stale += fmt.Sprintf(" %c=%d", k, s.First[k])
		}
	}
	lag, fwdPer := 0.0, 0.0
	if s.Specced > 0 {
		lag = float64(s.Lag) / float64(s.Specced)
		fwdPer = float64(s.Forwarded) / float64(s.Specced)
	}
	return fmt.Sprintf("%d blocks: %d from speculations (%d all txs valid), %d run by the committer (%d late, %d txs; %d late blocks took %d finished txs), %d fee misses (%d txs, %d at an unchanged gas price, %d repriced), %d re-offered (%d kept at other fees), %d re-speculated as stale (%d committed from the stale one); %d txs taken from a running speculation; %d txs re-speculated by reuse; txs valid %d, re-executed %d; why:%s; stale:%s; mean lag %.2f, %.2f blocks forwarded per speculation; wait %.2fs; replica failures %d; committer per valid tx %.1f μs, per re-executed tx %.1f μs%s",
		s.Blocks, s.Specced, s.Clean, s.SelfRun, s.Late, s.LateTxs, s.LatePart, s.LatePartTxs, s.FeeMiss, s.FeeMissTxs, s.FeeMissSame, s.Repriced, s.Reoffered, s.FeeKept, s.Respec, s.PrevUsed, s.LiveTxs, s.Reused, s.TxsValid, s.TxsReexec, why, stale, lag, fwdPer, s.WaitT.Seconds(), s.ReplicaFail, perTx(s.ValidT, s.TxsValid), perTx(s.ReexecT, s.TxsReexec), s.Dep.String()+s.Miss.String())
}

// Speculator runs a Chain's blocks speculatively.
type Speculator struct {
	c      *Chain
	w, l   int
	mu     sync.Mutex
	cond   *sync.Cond
	blocks map[uint64]*specBlock // offered, not committed
	next   uint64                // the next block to commit
	nextA  atomic.Uint64         // next, read without mu
	hashes map[uint64]Hash       // known block hashes from next-256 on (ancestors, committed, offered)
	pub    map[uint64]specPub    // committed write sets, until every live replica applied them
	reps   []*specRep
	closed bool
	// NoWait: the committer runs a block itself when its speculation is still running instead of
	// waiting for it. Set before the first Exec.
	NoWait bool
	// LazyReceipts: the committer leaves the receipts it would encode itself (txs it re-executed
	// or whose cumulative gas moved) to T0Tx.EncodeReceipt, called later by the caller. Set
	// before the first Exec.
	LazyReceipts bool
	// DepStats: measure the dependencies among the txs the committer re-executes (Stats.Dep;
	// the re-executions record their reads). Set before the first Exec.
	DepStats bool
	// MissStats: measure where the changed reads of re-executed txs came from (Stats.Miss). Set
	// before the first Exec.
	MissStats bool
	missW     map[uint64]missCommitted // the last 64 committed blocks' keys (MissStats)
	idle      time.Duration            // worker time in cond.Wait (Speculator.mu; MissStats reports it)
	pruned    int                      // pub entries respec skipped (Speculator.mu; MissStats reports it)
	missPrev  bool                     // MissStats: Exec took a stale speculation (committer)
	t0        time.Time                // MissStats: the Speculator's start
	reused    atomic.Int64             // Stats.Reused (workers)
	wg        sync.WaitGroup
	Stats     SpecStats
	// redone: committed blocks whose write sets may differ from their speculation's (a tx
	// re-executed, or no speculation used), for SpecStats.Stale (committer only)
	redone map[uint64]bool
}

type specPub struct {
	cb Address
	ws []*xbWrites // per tx, then the atomic txs' accounts
}

type specRep struct {
	st     *State
	seen   map[Address]bool // asked of the loader (as Chain.seen)
	next   uint64           // committed blocks below next are applied
	dead   bool
	hm     map[uint64]Hash // BLOCKHASH window of the replica
	lo, hi uint64          // hm holds lo..hi
}

type specBlock struct {
	rb    *RBlock
	claim int    // 0 free, 1 worker, 2 committer (Speculator.mu)
	at    uint64 // next block to commit when the speculation started
	cbNew bool
	ok    bool // the speculation ran to the end
	recs  []*xbRec
	gas   uint64 // receipts gas of the speculated txs
	done  chan struct{}
	// reads: the block's reads of values from before it (the first read of a key no earlier tx
	// of the block wrote), for the staleness check; checked: committed blocks below it were
	// checked against reads
	reads   map[xbKey]any
	checked uint64
	// pending: the blocks from pending up to this one were not finished when this speculation
	// started (not forwarded), or were forwarded from a stale speculation (its re-speculation
	// running then, or started later); their current speculations are checked against reads
	// once they finish
	pending uint64
	// prev: the finished speculation this one replaces after a committed block made it stale;
	// the committer and the forwarding take prev while this one runs
	prev *specBlock
	// arr[:nrec]: the txs speculated so far (cbNew and arr are set before nrec moves), for a
	// committer that reaches the block while it still runs
	arr  []*xbRec
	nrec atomic.Int32
	// MissStats: offered, speculation started and ended (tDone before done closes); respec: a
	// re-speculation
	tOffer, tStart, tDone time.Time
	respec                bool
}

// finished is sb when its speculation finished, else the finished one it replaces, else nil.
func (sb *specBlock) finished() *specBlock {
	select {
	case <-sb.done:
		return sb
	default:
		return sb.prev
	}
}

// NewSpeculator starts W workers speculating up to L blocks past the committed state of c (W = 0:
// Exec is c.Exec). Call after every AddHash; c's blocks then run only through the Speculator.
func NewSpeculator(c *Chain, W, L int) *Speculator {
	s := &Speculator{c: c, w: W, l: L, blocks: map[uint64]*specBlock{}, hashes: map[uint64]Hash{}, pub: map[uint64]specPub{},
		Stats: SpecStats{Why: map[byte]int{}, Stale: map[byte]int{}, FeeWhy: map[byte]int{}, First: map[byte]int{}}, redone: map[uint64]bool{}, missW: map[uint64]missCommitted{}}
	s.cond = sync.NewCond(&s.mu)
	s.t0 = time.Now()
	for n, h := range c.hashes {
		s.hashes[n] = h
		s.next = max(s.next, n+1)
	}
	s.nextA.Store(s.next)
	for range W {
		lo := s.next - min(s.next, 256)
		r := &specRep{st: NewState(), seen: map[Address]bool{}, next: s.next, hm: map[uint64]Hash{}, lo: lo, hi: lo}
		r.st.base = c.l
		r.st.OnMiss = func(addr Address, slot *Hash) {
			if slot != nil || r.seen[addr] {
				return
			}
			r.seen[addr] = true
			if err := loadAccount(r.st, c.l, addr); err != nil {
				panic(err)
			}
		}
		s.reps = append(s.reps, r)
		s.wg.Add(1)
		go s.work(r)
	}
	return s
}

// Close stops the workers (a running speculation finishes first).
func (s *Speculator) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cond.Broadcast()
	s.wg.Wait()
	s.Stats.Reused = int(s.reused.Load())
	if s.MissStats {
		s.Stats.Miss.Idle, s.Stats.Miss.Run, s.Stats.Miss.Pruned = s.idle, time.Duration(s.w)*time.Since(s.t0), s.pruned
	}
}

// Ahead offers rb (its BaseFee and gas prices from a predicted execution base fee) for
// speculation. Blocks are offered in order. Offering a block again with other fees keeps a
// started speculation (the committer reprices its txs, see commit) and updates one not started.
func (s *Speculator) Ahead(rb *RBlock) {
	if s.w == 0 {
		return
	}
	lockSpin(&s.mu) // the committer (Ahead: the executor thread): see lockSpin
	if rb.Number < s.next {
		s.mu.Unlock()
		return
	}
	s.hashes[rb.Number] = rb.Hash
	if old := s.blocks[rb.Number]; old != nil {
		switch old.claim {
		case 2:
			s.mu.Unlock()
			return
		case 0:
			old.rb = rb
			s.mu.Unlock()
			return
		}
		if sameTxs(old.rb, rb) {
			s.Stats.FeeKept++
			s.mu.Unlock()
			return
		}
		s.Stats.Reoffered++
	}
	s.blocks[rb.Number] = &specBlock{rb: rb, done: make(chan struct{}), tOffer: time.Now()}
	s.mu.Unlock()
	s.cond.Broadcast()
}

// Gas is the receipts gas of block n's speculation if it finished with every tx run.
func (s *Speculator) Gas(n uint64) (uint64, bool) {
	s.mu.Lock()
	sb := s.blocks[n]
	s.mu.Unlock()
	if sb != nil {
		sb = sb.finished()
	}
	if sb == nil || !sb.ok {
		return 0, false
	}
	for _, r := range sb.recs {
		if !r.ok {
			return 0, false
		}
	}
	return sb.gas, true
}

func (s *Speculator) work(r *specRep) {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		var sb *specBlock
		for {
			if s.closed {
				s.mu.Unlock()
				return
			}
			for n := s.next; n < s.next+uint64(s.l) && sb == nil; n++ {
				if b := s.blocks[n]; b != nil && b.claim == 0 {
					sb = b
				}
			}
			if sb != nil {
				break
			}
			if sb = s.respec(); sb != nil {
				break
			}
			t := time.Now()
			s.cond.Wait()
			s.idle += time.Since(t)
		}
		sb.claim, sb.at, sb.checked, sb.tStart = 1, s.next, s.next, time.Now()
		var pubs []specPub
		for n := r.next; n < s.next; n++ {
			pubs = append(pubs, s.pub[n])
		}
		var fwd []*specBlock // finished speculations of the blocks between, forwarded in order
	forward:
		for n := s.next; n < sb.rb.Number; n++ {
			fb := s.blocks[n]
			if fb != nil {
				fb = fb.finished()
			}
			if fb == nil || !fb.ok {
				break forward
			}
			fwd = append(fwd, fb)
		}
		s.Stats.Forwarded += len(fwd)
		sb.pending = s.next + uint64(len(fwd))
		for _, fb := range fwd { // a stale one forwarded while its re-speculation runs: checked against that once it finishes
			if s.blocks[fb.rb.Number] != fb {
				sb.pending = fb.rb.Number
				break
			}
		}
		b := sb.rb.Number
		for n := max(r.hi, b-min(b, 256)); n < b; n++ { // the BLOCKHASH window: hashes never change
			if h, ok := s.hashes[n]; ok {
				r.hm[n] = h
			}
		}
		r.hi = max(r.hi, b)
		for ; r.lo+256 < b && r.lo < r.hi; r.lo++ {
			delete(r.hm, r.lo)
		}
		s.mu.Unlock()
		ok := s.speculate(r, sb, pubs, fwd)
		sb.tDone = time.Now()
		close(sb.done)
		s.mu.Lock()
		r.next += uint64(len(pubs))
		if !ok {
			r.dead = true
			s.Stats.ReplicaFail++
		}
		s.prune()
		s.mu.Unlock()
		if !ok {
			return
		}
	}
}

// respec finds a finished speculation in the window that a block committed after it started,
// or the finished speculation of a block before it that was still running when it started,
// made stale (that write set has a value the speculation read otherwise from before its
// block) and offers its block again, the finished one kept as prev; nil: none
// (Speculator.mu held; called by an idle worker).
func (s *Speculator) respec() *specBlock {
	for n := s.next; n < s.next+uint64(s.l); n++ {
		b := s.blocks[n]
		if b == nil || b.claim != 1 {
			continue
		}
		select {
		case <-b.done:
		default:
			continue
		}
		if b.reads == nil { // the speculation failed
			continue
		}
		stale := false
		for ; b.checked < s.next && !stale; b.checked++ {
			p, ok := s.pub[b.checked]
			if !ok {
				s.pruned++
				continue // pruned: not checked
			}
			stale = staleBy(b.reads, &p)
		}
	pending:
		for b.pending = max(b.pending, s.next); b.pending < n && !stale; b.pending++ {
			f := s.blocks[b.pending]
			if f == nil {
				continue
			}
			select {
			case <-f.done:
			default:
				break pending // still running (a re-speculation too, not its stale one): checked once it finishes
			}
			if f.ok {
				var ws []*xbWrites
				for _, rec := range f.recs {
					if rec.ok {
						ws = append(ws, &rec.w)
					}
				}
				stale = staleBy(b.reads, &specPub{ws: ws})
			}
		}
		if stale {
			nb := &specBlock{rb: b.rb, done: make(chan struct{}), prev: b, tOffer: b.tOffer, respec: true}
			s.blocks[n] = nb
			for m := n + 1; m < s.next+uint64(s.l); m++ { // the later ones that forwarded b: checked against nb once it finishes
				if o := s.blocks[m]; o != nil && o.claim == 1 && o.pending > n {
					o.pending = n
				}
			}
			s.Stats.Respec++
			return nb
		}
	}
	return nil
}

// staleBy reports whether a committed block's write sets (p) hold a value other than one the
// reads saw.
func staleBy(reads map[xbKey]any, p *specPub) bool {
	for _, w := range p.ws {
		for addr, x := range w.acct {
			if v, ok := reads[xbKey{kind: 'e', addr: addr}]; ok && (x.add && x.balance.IsZero() || v != [2]bool{x.exists, !x.exists || x.n == 0 && x.balance.IsZero() && len(x.code) == 0}) {
				return true // (a zero credit leaves the balance unknown here)
			}
			if !x.exists {
				continue
			}
			if v, ok := reads[xbKey{kind: 'b', addr: addr}]; ok && x.bal && (x.add && !x.balance.IsZero() || !x.add && v != any(x.balance)) {
				return true
			}
			if v, ok := reads[xbKey{kind: 'n', addr: addr}]; ok && x.nonce && v != any(x.n) {
				return true
			}
			if _, ok := reads[xbKey{kind: 'c', addr: addr}]; ok && x.cod {
				return true
			}
		}
		for k, v := range w.slots {
			if r, ok := reads[xbKey{kind: 's', addr: k.addr, slot: k.slot}]; ok && r != any(v) {
				return true
			}
		}
	}
	return false
}

// prune drops the write sets every live replica applied (Speculator.mu held).
func (s *Speculator) prune() {
	lo := s.next
	for _, r := range s.reps {
		if !r.dead {
			lo = min(lo, r.next)
		}
	}
	for n := range s.pub {
		if n < lo {
			delete(s.pub, n)
		}
	}
}

// speculate brings r to the committed state (pubs), applies the write sets of the finished
// speculations of the blocks between (fwd, in order; the committer validates every read, so a
// wrong forwarded write only costs a re-execution), speculates sb on that, and leaves r at the
// committed state. false: the replica failed (a loader error) and must not be used again.
func (s *Speculator) speculate(r *specRep, sb *specBlock, pubs []specPub, fwd []*specBlock) (ok bool) {
	defer func() {
		if p := recover(); p != nil {
			ok, sb.ok = false, false
		}
	}()
	st := r.st
	for _, p := range pubs {
		for _, w := range p.ws {
			for addr := range w.acct { // load first: an account the run deletes stays deleted
				st.acct(addr)
			}
			for k := range w.slots {
				st.acct(k.addr)
			}
			xbApply(st, w, p.cb)
			st.beginTx()
		}
	}
	type cbWas struct {
		addr Address
		a    *account
	}
	type fwdUndo struct {
		u  xbUndo
		cb Address
	}
	var fundo []fwdUndo
	var fcb []cbWas
	for _, fb := range fwd {
		cb := fb.rb.Coinbase
		if !slices.ContainsFunc(fcb, func(c cbWas) bool { return c.addr == cb }) {
			fcb = append(fcb, cbWas{cb, st.acct(cb)})
		}
		for _, rec := range fb.recs {
			if !rec.ok {
				continue
			}
			for addr := range rec.w.acct {
				st.acct(addr)
			}
			for k := range rec.w.slots {
				st.acct(k.addr)
			}
			fundo = append(fundo, fwdUndo{xbInverse(st, &rec.w), cb})
			xbApply(st, &rec.w, cb)
			st.beginTx()
		}
	}
	defer func() { // back to the committed state (a panic below leaves the replica dead anyway)
		if !ok {
			return
		}
		for j := len(fundo) - 1; j >= 0; j-- {
			fundo[j].u.undo(st, fundo[j].cb)
		}
		for _, c := range fcb {
			if c.a == nil && st.accounts[c.addr] != nil {
				delete(st.accounts, c.addr)
				st.resetCodes()
			} else if c.a != nil && st.accounts[c.addr] != c.a {
				st.accounts[c.addr] = c.a
				st.resetCodes()
			}
		}
		st.beginTx()
	}()
	rb := sb.rb
	blk, failed, fr, err := s.c.execCtx(rb, r.hm) // a pre-tx group (RBlock.Fork.Begin) runs on the committer only
	if err != nil {
		return true // the committer reports it
	}
	cb := rb.Coinbase
	st.beginTx()
	cbAcct := st.acct(cb)
	sb.cbNew = cbAcct == nil
	sb.arr = make([]*xbRec, len(rb.Txs))
	// old: a re-speculation of the same input takes each tx of the stale speculation it replaces
	// whose reads all hold on the replica at that point (the committer's validation, here): the
	// same tx on the same reads runs the same, so only the txs whose reads changed run again
	var old []*xbRec
	if p := sb.prev; p != nil && p.ok && p.cbNew == sb.cbNew && sameInput(p.rb, rb) {
		old = p.recs
	}
	encOK := true
	var undo []xbUndo
	for i := range rb.Txs {
		if s.nextA.Load() > rb.Number {
			break // committed meanwhile (late): the rest is of no use
		}
		t := &rb.Txs[i]
		var rec *xbRec
		if i < len(old) && specHolds(st, old[i]) {
			c := *old[i] // a copy: the committer may take old[i] meanwhile; its maps and slices are never written
			rec = &c
			for addr := range rec.w.acct { // loaded before the apply, as after a run
				st.acct(addr)
			}
			for k := range rec.w.slots {
				st.acct(k.addr)
			}
			s.reused.Add(1)
		} else {
			tx := t.tx()
			rec = xbSpec(st, &blk, fr, &tx, TxContext{WarpFailed: failed[t.Hash]}, t.feeFloor(), sb.cbNew, s.c.asm)
		}
		sb.recs = append(sb.recs, rec)
		if !rec.ok {
			encOK = false
			sb.arr[i] = rec
			sb.nrec.Store(int32(i + 1))
			continue
		}
		if rec.list == nil { // run here (a reused one is final)
			for k := range rec.reads { // a read of an account never accessed (a warm address): load it as the committer will
				if st.accounts[k.addr] == nil && !r.seen[k.addr] {
					st.acct(k.addr)
					rec.reads[k] = xbVal(st, k)
				}
			}
			xbFinish(rec)
		}
		sb.gas += rec.gas
		if encOK && rec.rcptCum != sb.gas { // every tx before ran: the cumulative gas is the speculation's
			rec.rcptCum = sb.gas
			rec.rcpt = EncodeReceipt(&Receipt{Type: t.Type, Success: rec.res.Status == "success", CumGas: sb.gas, Logs: rec.res.Logs})
		} else if !encOK {
			rec.rcpt = nil
		}
		undo = append(undo, xbInverse(st, &rec.w))
		xbApply(st, &rec.w, cb)
		st.beginTx()
		sb.arr[i] = rec // finished: the committer may take it while the block still runs
		sb.nrec.Store(int32(i + 1))
	}
	for j := len(undo) - 1; j >= 0; j-- {
		undo[j].undo(st, cb)
	}
	if cbAcct == nil && st.accounts[cb] != nil {
		delete(st.accounts, cb)
		st.resetCodes()
	} else if cbAcct != nil && st.accounts[cb] != cbAcct {
		st.accounts[cb] = cbAcct
		st.resetCodes()
	}
	st.beginTx()
	sb.reads = map[xbKey]any{}
	wrote := map[xbKey]bool{}
	for _, rec := range sb.recs {
		if !rec.ok {
			break // what follows ran on a state without this tx's writes
		}
		for k, v := range rec.reads {
			if _, ok := sb.reads[k]; !ok && !wrote[k] {
				sb.reads[k] = v
			}
		}
		for addr := range rec.w.acct {
			for _, kind := range []byte("ebnc") {
				wrote[xbKey{kind: kind, addr: addr}] = true
			}
		}
		for k := range rec.w.slots {
			wrote[xbKey{kind: 's', addr: k.addr, slot: k.slot}] = true
		}
	}
	sb.ok = true
	return true
}

// specHolds: rec, a finished speculated tx, holds on the replica st (every read equal, as the
// committer's Speculator.valid; a run the committer refuses anyway is not taken).
func specHolds(st *State, rec *xbRec) bool {
	if !rec.ok || rec.coins || rec.readsCB || rec.list == nil {
		return false
	}
	for _, r := range rec.list {
		st.acct(r.k.addr)
		if !xbEq(st, r.k, r.v) {
			return false
		}
	}
	return true
}

// sameInput: the speculation ran rb's block with rb's base fee and gas prices.
func sameInput(a, b *RBlock) bool {
	if a.Hash != b.Hash || a.BaseFee != b.BaseFee || len(a.Txs) != len(b.Txs) {
		return false
	}
	for i := range a.Txs {
		if a.Txs[i].GasPrice != b.Txs[i].GasPrice || a.Txs[i].From != b.Txs[i].From {
			return false
		}
	}
	return true
}

// sameTxs: a and b are the same block with the same senders (their fees may differ).
func sameTxs(a, b *RBlock) bool {
	if a.Hash != b.Hash || len(a.Txs) != len(b.Txs) {
		return false
	}
	for i := range a.Txs {
		if a.Txs[i].From != b.Txs[i].From {
			return false
		}
	}
	return true
}

// Exec commits the next block: same contract and results as Chain.Exec.
func (s *Speculator) Exec(rb *RBlock) (*T0Block, []RTxOut, error) {
	if s.w == 0 {
		return s.c.Exec(rb)
	}
	lockSpin(&s.mu) // the committer (Ahead: the executor thread): see lockSpin
	if rb.Number != s.next {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("block %d: the next block to commit is %d", rb.Number, s.next)
	}
	s.hashes[rb.Number] = rb.Hash
	sb := s.blocks[rb.Number]
	// live: a speculation of the block still running on the same input; the committer takes each
	// of its txs that has finished by the time it reaches that tx (fresher than a stale one's)
	var live *specBlock
	if sb != nil && sb.prev != nil {
		if f := sb.finished(); f != sb { // re-speculating: take the stale one, its reads are validated
			if sb.claim == 1 && sameInput(sb.rb, rb) {
				live = sb
			}
			sb.claim = 2 // a worker that has not started it never will
			sb = f
			s.Stats.PrevUsed++
			s.missPrev = true
		}
	}
	mine := sb == nil || sb.claim == 0
	if sb != nil { // also a worker's: respec and Ahead leave the block being committed alone
		sb.claim = 2
	}
	s.mu.Unlock()
	var use *specBlock
	var recs []*xbRec
	feeOnly := false // use ran at another base fee: its txs at an unchanged gas price that never read the base fee hold
	if !mine && s.NoWait {
		select {
		case <-sb.done:
		default:
			mine = true // still running: run it here; the txs it finished are validated as usual
			s.Stats.Late++
			s.Stats.LateTxs += len(rb.Txs)
			if sameInput(sb.rb, rb) {
				live = sb
			}
			if n := sb.nrec.Load(); n > 0 && sameTxs(sb.rb, rb) {
				use, recs, feeOnly = sb, sb.arr[:n], !sameInput(sb.rb, rb)
				s.Stats.LatePart++
				s.Stats.LatePartTxs += int(n)
			}
		}
	}
	if !mine {
		t := time.Now()
		<-sb.done
		s.Stats.WaitT += time.Since(t)
		switch {
		case !sb.ok:
		case !sameInput(sb.rb, rb):
			s.Stats.FeeMiss++
			s.Stats.FeeMissTxs += len(rb.Txs)
			if sameTxs(sb.rb, rb) {
				for i := range rb.Txs {
					if sb.rb.Txs[i].GasPrice == rb.Txs[i].GasPrice {
						s.Stats.FeeMissSame++
					}
				}
				use, feeOnly = sb, true
			}
		default:
			use = sb
		}
		if use != nil {
			recs = use.recs
		}
	}
	reexec := s.Stats.TxsReexec
	defer func() { s.missPrev = false }()
	if s.MissStats && use != nil {
		s.missUsed(rb.Number, use)
	}
	tb, outs, pub, err := s.commit(rb, use, recs, feeOnly, live)
	if err != nil {
		return nil, nil, err
	}
	if s.MissStats {
		s.missDone(rb.Number, pub)
	}
	if use == nil || s.Stats.TxsReexec > reexec {
		s.redone[rb.Number] = true
	}
	delete(s.redone, rb.Number-min(rb.Number, uint64(2*s.l+256)))
	lockSpin(&s.mu) // the committer (Ahead: the executor thread): see lockSpin
	s.pub[rb.Number] = specPub{cb: rb.Coinbase, ws: pub}
	delete(s.blocks, rb.Number)
	s.next++
	s.nextA.Store(s.next)
	if rb.Number >= 256 {
		delete(s.hashes, rb.Number-256)
	}
	s.prune()
	s.mu.Unlock()
	s.cond.Broadcast()
	s.Stats.Blocks++
	if use != nil {
		s.Stats.Specced++
		s.Stats.Lag += int(rb.Number - use.at)
	} else {
		s.Stats.SelfRun++
	}
	return tb, outs, nil
}

// commit runs rb on the Chain's State, taking each tx from sb's speculation when its reads still
// hold (sb nil: none). It returns Chain.Exec's results and the write sets for the replicas.
// feeOnly: sb ran at another base fee (sameTxs); a tx of it holds only at an unchanged gas price
// when it never read the base fee (xbRec.baseFee), the price is not below the committed base fee,
// and its reads hold. The base fee goes to the coinbase (cchainFeeRules), so the coinbase gets
// the price times the gas either way.
func (s *Speculator) commit(rb *RBlock, sb *specBlock, recs []*xbRec, feeOnly bool, live *specBlock) (tb *T0Block, outs []RTxOut, pub []*xbWrites, err error) {
	c := s.c
	hook := c.st.RWHook
	defer func() {
		c.st.RWHook = hook
		if p := recover(); p != nil {
			if c.err != nil {
				err = fmt.Errorf("block %d: loader: %w", rb.Number, c.err)
			} else {
				err = fmt.Errorf("block %d: panic: %v", rb.Number, p)
			}
		}
	}()
	blk, failed, fr, err := c.execCtx(rb, c.hashes)
	if err != nil {
		return nil, nil, nil, err
	}
	tb = &T0Block{Number: rb.Number, Hash: rb.Hash}
	if len(rb.Txs) > 0 { // sized once (nil without txs, as Chain.Exec)
		tb.Txs, outs = make([]T0Tx, 0, len(rb.Txs)), make([]RTxOut, 0, len(rb.Txs))
	}
	pub = make([]*xbWrites, 0, len(rb.Txs)+2) // the pre-tx group, the txs, the atomic end
	cb := rb.Coinbase
	if rb.Fork != nil && rb.Fork.Begin != nil { // the pre-tx group as Chain.Exec runs it; its writes go to the replicas first
		c.st.beginTx()
		var berr error
		_, _, w, _, ptr := xbCaptureRun(c.st, cb, nil, func() (ExecResult, uint64) { berr = rb.Fork.Begin(c.st, blk); return ExecResult{}, 0 })
		if berr != nil {
			return nil, nil, nil, fmt.Errorf("block %d: begin: %w", rb.Number, berr)
		}
		clear(c.touched)
		for addr := range ptr { // every address it accessed, as Chain's RWHook sees them
			c.touched[addr] = struct{}{}
		}
		tb.Pre = c.txWrites(cb)
		pub = append(pub, &w)
	}
	c.st.acct(cb)
	cbNew := c.st.accounts[cb] == nil
	cbChanged := sb != nil && sb.cbNew != cbNew
	if cbChanged {
		s.Stats.Why['N'] += len(recs)
		recs = nil
	}
	clean := sb != nil && len(recs) == len(rb.Txs)
	redoneHere := false // a tx of this block was re-executed
	var deps []depTx    // DepStats
	var mb *missBlock   // MissStats
	if s.MissStats && sb != nil {
		mb = &missBlock{inW: map[xbKey]bool{}, t0: time.Now(), txs: len(rb.Txs), at: sb.at, prev: s.missPrev, kind: 2}
		select {
		case <-sb.done:
			mb.kind, mb.done = 0, sb.tDone
			if sb.respec {
				mb.kind = 1
			}
		default:
		}
		for _, w := range pub {
			mb.written(w)
		}
	}
	cum := uint64(0)
	for i := range rb.Txs {
		t := &rb.Txs[i]
		t0 := time.Now()
		var rec *xbRec
		var trig byte     // why rec was dropped (SpecStats.First)
		var cls byte      // MissStats class of rec's changed reads
		fromLive := false // rec is live's
		if live != nil {
			if n := int(live.nrec.Load()); i < n && live.cbNew == cbNew {
				rec, fromLive = live.arr[i], true
				s.Stats.LiveTxs++
			}
		}
		if rec == nil && i < len(recs) {
			rec = recs[i]
		}
		if rec != nil {
			fo := feeOnly && !fromLive
			if fo && sb.rb.Txs[i].GasPrice != t.GasPrice && !rec.baseFee && !rec.price && !sb.cbNew && t.From != cb && !t.GasPrice.Lt(&rb.BaseFee) {
				if nr, ok := xbReprice(rec, t.From, t.Gas, &t.Value, &t.GasPrice, &sb.rb.Txs[i].GasPrice); ok {
					rec = nr // validated below as any other
					s.Stats.Repriced++
				}
			}
			if fo && (rec.baseFee || sb.rb.Txs[i].GasPrice != t.GasPrice && recs[i] == rec || t.GasPrice.Lt(&rb.BaseFee)) {
				s.Stats.Why['f']++
				switch {
				case t.GasPrice.Lt(&rb.BaseFee):
					s.Stats.FeeWhy['L']++
				case rec.baseFee:
					s.Stats.FeeWhy['B']++
				case sb.cbNew || t.From == cb:
					s.Stats.FeeWhy['N']++
				case rec.price:
					s.Stats.FeeWhy['P']++
				default:
					s.Stats.FeeWhy['R']++
				}
				rec, trig = nil, 'f'
			} else if ok, why := s.valid(rec); !ok {
				s.Stats.Why[why]++
				trig = why
				if why != 'p' && why != 'C' && why != 'M' {
					src := sb
					if fromLive {
						src = live
					}
					trig = s.staleCause(rb.Number, src, redoneHere)
					s.Stats.Stale[trig]++
					if mb != nil {
						cls = s.missClass(rec, mb, rb.Number)
					}
				}
				rec = nil
			}
		}
		var ws []T0Write
		var res ExecResult
		var gas uint64
		if rec != nil {
			c.st.RWHook = nil
			var old map[slotKey]uint256.Int
			if !rec.slotOK {
				old = make(map[slotKey]uint256.Int, len(rec.w.slots))
				for k := range rec.w.slots {
					old[k] = c.slotNow(k)
				}
			}
			xbApply(c.st, &rec.w, cb)
			clear(c.touched)
			for addr := range rec.w.acct {
				c.touched[addr] = struct{}{}
			}
			c.touched[cb] = struct{}{}
			ws = make([]T0Write, 0, 2*len(c.touched)+len(rec.slotWs)+len(old)) // sized once, as txWrites
			for addr := range c.touched {
				ws = t0AcctWrites(ws, c.shadow, c.st, addr)
			}
			if rec.slotOK { // the reads held: the slots' committed values are the ones read
				ws = append(ws, rec.slotWs...)
			}
			for k, o := range old {
				if n := c.slotNow(k); n != o {
					ws = append(ws, T0Write{Addr: k.addr, Kind: T0Slot, Slot: T0Mask(k.slot), Old: o.Bytes32(), New: n.Bytes32()})
				}
			}
			c.aq <- aliasMsg{keys: rec.warm}
			c.st.beginTx()
			res, gas = rec.res, rec.gas
			pub = append(pub, &rec.w)
			s.Stats.TxsValid++
			s.Stats.ValidT += time.Since(t0)
		} else {
			clean = false
			tx := t.tx()
			var reads map[xbKey]any
			if s.DepStats {
				reads = map[xbKey]any{}
			}
			r, g, w, _, ptr := xbCapture(c.st, &blk, fr, &tx, TxContext{WarpFailed: failed[t.Hash]}, t.feeFloor(), cbNew, c.asm, reads)
			clear(c.touched)
			for addr := range ptr { // every address the tx accessed, as Chain's RWHook sees them
				c.touched[addr] = struct{}{}
			}
			ws = c.txWrites(cb)
			res, gas = r, g
			pub = append(pub, &w)
			if !redoneHere {
				switch {
				case trig != 0:
				case sb == nil:
					trig = 'S'
				case cbChanged:
					trig = 'N'
				default:
					trig = 'L'
				}
				s.Stats.First[trig]++
			}
			redoneHere = true
			s.Stats.TxsReexec++
			s.Stats.ReexecT += time.Since(t0)
			if s.DepStats {
				deps = append(deps, depTx{reads, depWrites(&w), time.Since(t0)})
			}
			if mb != nil {
				s.missTx(mb, i, cls, time.Since(t0))
			}
		}
		if mb != nil {
			mb.written(pub[len(pub)-1])
			if i < len(recs) && recs[i] != nil && recs[i].ok {
				mb.written(&recs[i].w)
			}
		}
		cum += gas
		if rec != nil && rec.rcpt != nil && rec.rcptCum == cum { // encoded by the worker at this cumulative gas
			tb.Txs = append(tb.Txs, T0Tx{Hash: t.Hash, Receipt: rec.rcpt, Output: res.Output, Logs: res.Logs, Writes: ws})
		} else if s.LazyReceipts {
			tb.Txs = append(tb.Txs, lazyTx(t, &res, cum, ws))
		} else {
			tb.Txs = append(tb.Txs, t0TxOf(t, &res, cum, ws))
		}
		outs = append(outs, RTxOut{Status: res.Status, GasUsed: gas, CumGas: cum})
	}
	if clean {
		s.Stats.Clean++
	}
	s.Stats.Dep.depBlock(deps)
	if rb.Atomic != nil {
		clear(c.touched)
		c.st.RWHook = hook
		if err := rb.Atomic(c.st); err != nil {
			return nil, nil, nil, fmt.Errorf("block %d: atomic: %w", rb.Number, err)
		}
		c.st.RWHook = nil
		aw := &xbWrites{acct: map[Address]*xbAcct{}, slots: map[slotKey]uint256.Int{}}
		for addr := range c.touched {
			tb.End = t0AcctWrites(tb.End, c.shadow, c.st, addr)
			a := c.st.accounts[addr]
			x := &xbAcct{exists: a != nil, bal: true, nonce: true}
			if a != nil {
				x.balance, x.n = a.balance, a.nonce
			}
			aw.acct[addr] = x
		}
		tb.End = c.coinWrites(tb.End) // multicoin balances (an ANT import or export before Banff), as Chain.Exec
		c.st.beginTx()
		pub = append(pub, aw)
	}
	t0Codes(c.st, tb)
	c.hashes[rb.Number] = rb.Hash
	delete(c.hashes, rb.Number-256)
	return tb, outs, pub, nil
}

// staleCause is the SpecStats.Stale class of an invalid read of a tx of block n speculated in
// sb; inBlock: an earlier tx of the block was re-executed.
func (s *Speculator) staleCause(n uint64, sb *specBlock, inBlock bool) byte {
	switch {
	case inBlock:
		return 'I'
	case sb.pending < n:
		return 'P'
	}
	for m := sb.at; m < n; m++ {
		if s.redone[m] {
			return 'F'
		}
	}
	return 'X'
}

// valid is xbValid on the Chain's State, loading every account a read names first (the
// committed State loads lazily; a speculation may read an account it has not loaded yet).
func (s *Speculator) valid(rec *xbRec) (bool, byte) {
	switch {
	case rec.coins:
		return false, 'M'
	case !rec.ok:
		return false, 'p'
	case rec.readsCB:
		return false, 'C'
	}
	st := s.c.st
	for _, r := range rec.list {
		st.acct(r.k.addr)
		if !xbEq(st, r.k, r.v) {
			return false, r.k.kind
		}
	}
	return true, 0
}

// slotNow is the committed value of k (zero for an absent account).
func (c *Chain) slotNow(k slotKey) uint256.Int {
	if a := c.st.acct(k.addr); a != nil {
		v, _ := c.st.stored(a, &k.slot)
		return v
	}
	return uint256.Int{}
}
