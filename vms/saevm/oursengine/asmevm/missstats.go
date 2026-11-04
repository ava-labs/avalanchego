//go:build rwtrace

package asmevm

import (
	"fmt"
	"time"
)

// MissStats (Speculator.MissStats only, a measurement) splits the txs the committer re-executed
// after a failed read check by where the changed values came from: a key an earlier tx of the
// same block wrote, on the committer or in the speculation (an in-block cascade: that tx's
// speculated writes were wrong), or a value from before the block that the replica saw older
// than the committed one (speculation age). For an age miss it names the latest committed block
// before it that wrote the key, and the time from that block's commit to the start of this one's.
type MissStats struct {
	Blocks int // blocks committed from a speculation with a re-executed tx
	// by tx: 'F' a block's first re-executed tx, 'A' a later one; then the class of its changed
	// reads: 'c' all in-block (cascade), 'a' all from before the block (age), 'm' both,
	// 'o' no read check failed (fee, panic, coinbase, multicoin, a new coinbase, or the late
	// speculation had not reached it)
	N map[[2]byte]int
	T map[[2]byte]time.Duration // committer time on those txs (validation and re-execution)
	// Pos: the first re-executed tx's index in its block, buckets 0, 1, 2-3, 4-7, 8-15, 16-31, 32+;
	// Txs: the blocks' txs
	Pos [7]int
	Txs int
	// age reads by the distance to the latest block that committed the key (1, 2, 3-4, 5-8, 9+;
	// index 5: no block within the last 64 committed it) and by the time from that block's
	// commit to the start of this one's (< 50 μs, < 200 μs, < 1 ms, < 5 ms, >= 5 ms)
	// [0]: reads of a block's first re-executed tx, [1]: of the later ones
	Dist [2][6]int
	Win  [2][5]int
	// Src: age reads (first tx, later txs) by where the stale value came from: [0] the writer
	// block is inside the speculation's span (committed after it started: the staleness check's
	// domain), [1] the same but the committer took a stale speculation whose replacement still
	// ran, [2] the writer is before the span or none (the replica had the committed value, so a
	// forwarded speculation wrote the key and its block's commit did not), [3] the same, stale
	// speculation taken; Pruned: committed write sets the staleness check skipped (pruned)
	Src    [2][4]int
	Pruned int
	// Span: age reads in the span of a speculation the committer used as it was (not a stale one
	// taken), (first tx, later txs) by the used one: [0] a first speculation, finished; [1] a
	// re-speculation, finished; [2] still running (late); then [0] the writer committed before
	// the used one finished (or it still runs), [1] after it finished (a staleness check could
	// have re-speculated it)
	Span [2][3][2]int
	// Idle: worker time waiting for work (summed over workers); Run: the time measured over
	Idle, Run time.Duration
	// used speculations: count, the time from offer, speculation start and speculation end to
	// the commit's start (summed; End only over the finished ones), finished late (NoWait), and
	// how many were a re-speculation
	Used, Late, Respec int
	Offer, Start, End  time.Duration
	SpecDur            time.Duration // speculation end minus start (finished ones)
	Lag                [6]int        // block minus next block to commit at the start: 0, 1, 2, 3-4, 5-8, 9+
}

func (m *MissStats) init() {
	if m.N == nil {
		m.N, m.T = map[[2]byte]int{}, map[[2]byte]time.Duration{}
	}
}

func (m *MissStats) add(o *MissStats) {
	m.init()
	m.Blocks += o.Blocks
	for k, v := range o.N {
		m.N[k] += v
	}
	for k, v := range o.T {
		m.T[k] += v
	}
	for i := range m.Pos {
		m.Pos[i] += o.Pos[i]
	}
	m.Txs += o.Txs
	for j := range 2 {
		for i := range m.Dist[j] {
			m.Dist[j][i] += o.Dist[j][i]
		}
		for i := range m.Win[j] {
			m.Win[j][i] += o.Win[j][i]
		}
		for i := range m.Src[j] {
			m.Src[j][i] += o.Src[j][i]
		}
		for i := range m.Span[j] {
			for k := range m.Span[j][i] {
				m.Span[j][i][k] += o.Span[j][i][k]
			}
		}
	}
	m.Pruned += o.Pruned
	m.Idle += o.Idle
	m.Run += o.Run
	for i := range m.Lag {
		m.Lag[i] += o.Lag[i]
	}
	m.Used += o.Used
	m.Late += o.Late
	m.Respec += o.Respec
	m.Offer += o.Offer
	m.Start += o.Start
	m.End += o.End
	m.SpecDur += o.SpecDur
}

func bucket(v uint64, lim ...uint64) int {
	for i, l := range lim {
		if v < l {
			return i
		}
	}
	return len(lim)
}

func (m *MissStats) String() string {
	if m.Used == 0 {
		return ""
	}
	s := fmt.Sprintf("; MISS SPLIT: %d blocks with a re-executed tx (%d txs), txs by class [n, committer s]:", m.Blocks, m.Txs)
	for _, p := range []byte("FA") {
		for _, c := range []byte("caom") {
			k := [2]byte{p, c}
			if m.N[k] > 0 {
				s += fmt.Sprintf(" %c%c=%d/%.2fs", p, c, m.N[k], m.T[k].Seconds())
			}
		}
	}
	fin := m.Used - m.Late
	s += fmt.Sprintf("; first re-executed position 0,1,2-3,4-7,8-15,16-31,32+ %v; age reads (first tx, later txs) by distance to the writer 1,2,3-4,5-8,9+,none %v, by commit-to-commit window <50μs,<200μs,<1ms,<5ms,>=5ms %v; age reads by source (in span, in span stale taken, before span, before span stale taken) %v, in span not stale taken by the used speculation (first finished, re-speculation finished, running) and writer committed (before, after) it finished %v, write sets skipped by the staleness check (pruned) %d; workers idle %.1f%%; used speculations %d (late %d, re-speculations %d, lag 0,1,2,3-4,5-8,9+ %v): offer->commit %.0f μs, start->commit %.0f μs, end->commit %.0f μs, speculation %.0f μs (means)",
		m.Pos, m.Dist, m.Win, m.Src, m.Span, m.Pruned, 100*m.Idle.Seconds()/max(m.Run.Seconds(), 1e-9), m.Used, m.Late, m.Respec, m.Lag,
		perTx(m.Offer, m.Used), perTx(m.Start, m.Used), perTx(m.End, fin), perTx(m.SpecDur, fin))
	return s
}

// missBlock is the committer's MissStats state for one block.
type missBlock struct {
	inW  map[xbKey]bool // keys an earlier tx of the block wrote (committed or speculated)
	t0   time.Time      // the commit's start
	at   uint64         // the used speculation's start (next block to commit then)
	prev bool           // it was a stale one whose replacement still ran
	kind int            // the used one: 0 a first speculation, finished; 1 a re-speculation, finished; 2 running
	done time.Time      // when the used one finished (kind 0, 1)
	txs  int
	seen bool // a tx was re-executed already
}

// missTx counts re-executed tx i of the block (cls: missClass, 0 when no read check failed),
// d its committer time.
func (s *Speculator) missTx(mb *missBlock, i int, cls byte, d time.Duration) {
	m := &s.Stats.Miss
	m.init()
	p := byte('A')
	if !mb.seen {
		mb.seen, p = true, 'F'
		m.Blocks++
		m.Txs += mb.txs
		m.Pos[bucket(uint64(i), 1, 2, 4, 8, 16, 32)]++
	}
	if cls == 0 {
		cls = 'o'
	}
	m.N[[2]byte{p, cls}]++
	m.T[[2]byte{p, cls}] += d
}

// missUsed counts the timing of the speculation used for block n.
func (s *Speculator) missUsed(n uint64, sb *specBlock) {
	m := &s.Stats.Miss
	now := time.Now()
	m.Used++
	m.Offer += now.Sub(sb.tOffer)
	m.Start += now.Sub(sb.tStart)
	m.Lag[bucket(n-sb.at, 1, 2, 3, 5, 9)]++
	if sb.respec {
		m.Respec++
	}
	select {
	case <-sb.done:
		m.End += now.Sub(sb.tDone)
		m.SpecDur += sb.tDone.Sub(sb.tStart)
	default:
		m.Late++
	}
}

// missWritten adds w's keys to the block's in-block set.
func (mb *missBlock) written(w *xbWrites) {
	for k := range depWrites(w) {
		mb.inW[k] = true
	}
}

// missClass is the class of rec's changed reads on the committed state ('c', 'a', 'm'), the
// age reads counted in Dist and Win.
func (s *Speculator) missClass(rec *xbRec, mb *missBlock, n uint64) byte {
	st := s.c.st
	in, age := false, false
	f := 0
	if mb.seen {
		f = 1
	}
	for _, r := range rec.list {
		st.acct(r.k.addr)
		if xbEq(st, r.k, r.v) {
			continue
		}
		if mb.inW[r.k] {
			in = true
			continue
		}
		age = true
		d, src := 5, 2
		for m := n - 1; m+64 >= n && m < n; m-- {
			cw := s.missW[m]
			if cw.keys == nil {
				continue
			}
			if cw.keys[r.k] {
				d = bucket(n-m, 2, 3, 5, 9)
				if m >= mb.at {
					src = 0
					if !mb.prev {
						after := 0
						if mb.kind < 2 && cw.at.After(mb.done) {
							after = 1
						}
						s.Stats.Miss.Span[f][mb.kind][after]++
					}
				}
				s.Stats.Miss.Win[f][bucket(uint64(mb.t0.Sub(cw.at).Microseconds()), 50, 200, 1000, 5000)]++
				break
			}
		}
		s.Stats.Miss.Dist[f][d]++
		if mb.prev {
			src++
		}
		s.Stats.Miss.Src[f][src]++
	}
	switch {
	case in && age:
		return 'm'
	case in:
		return 'c'
	}
	return 'a'
}

// missCommitted keeps block n's committed keys and commit time for later age reads.
type missCommitted struct {
	keys map[xbKey]bool
	at   time.Time
}

func (s *Speculator) missDone(n uint64, pub []*xbWrites) {
	keys := map[xbKey]bool{}
	for _, w := range pub {
		for k := range depWrites(w) {
			keys[k] = true
		}
	}
	s.missW[n] = missCommitted{keys, time.Now()}
	delete(s.missW, n-min(n, 64))
}
