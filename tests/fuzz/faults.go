// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
)

// How long the checks that assume prompt delivery wait out a fault span
// (faults).
const (
	// closeDelay is the shortest delay that can close a link. A node closes
	// a link on a pong that answers no ping in flight (network/peer/peer.go
	// handlePong), and a ping goes out every ping period, so two pings in
	// flight need a round trip of a ping period. A round trip crosses each
	// route once, so a delay below half a ping period keeps every link up.
	// The read deadline is a pong timeout, far longer.
	closeDelay = pingFrequency / 2
	// linkTail is how long after a span a link that it closes can still
	// close. A held link closes on the burst of pongs that its release lets
	// through, as soon as they are read.
	linkTail = 2 * pingFrequency
	// recoverTail is how long after a span that closed links the nodes take
	// to be back in sync: a node redials within MaxReconnectDelay, a second
	// on the test network (nodes.go setTestDefaults), and then catches up by
	// consensus.
	recoverTail = 3 * time.Second
)

// faults records the network faults that the fuzzer injected (injector), so
// that the checks that assume prompt delivery judge only what a fault cannot
// explain. Liveness and prompt delivery hold only after GST (Stateful
// Fuzzing.md 1).
//
// A span starts when some route gets a delay, a link is refused, or an
// honest node has no link without a delay, and ends when none of these
// holds, or when they change and the next span starts. Each check excuses
// what a span can cause it, and no more (excusal), so a brief hold inside a
// long mild delay excuses only its own time.
type faults struct {
	mu    sync.Mutex
	spans []faultSpan
	cuts  []linkCut
}

// A faultSpan is a time in which the network had one set of faults.
type faultSpan struct {
	span
	// delay is the longest delay that a route had in the span, and hold for
	// a refused link.
	delay time.Duration
	// cutOff reports whether some honest node had fewer than alpha links
	// without a delay in the span, too few to reach alpha from honest votes,
	// and unlinked whether one had no open link at all.
	cutOff, unlinked bool
	// carried is how long before the span's start a record that still
	// waits in it can have been read: a span that follows another with no
	// gap inherits what waits in it (observe).
	carried time.Duration
}

// effectiveDelay returns the longest time that a record can have waited in
// s: its longest delay, or its length and what it carried if that is
// shorter, since every record that waits goes out when the faults end.
func (s faultSpan) effectiveDelay() time.Duration {
	if s.end.IsZero() {
		return s.delay
	}

	return min(s.delay, s.carried+s.end.Sub(s.start))
}

// closes reports whether s can have closed a link (closeDelay).
func (s faultSpan) closes() bool {
	return s.effectiveDelay() >= closeDelay
}

// near reports whether a time from from to to falls in s, or less than tail
// after it.
func (s faultSpan) near(from, to time.Time, tail time.Duration) bool {
	return !s.start.After(to) && (s.end.IsZero() || !s.end.Add(tail).Before(from))
}

// A linkCut is a cut of the link between two honest nodes (disconnectOp).
type linkCut struct {
	at   time.Time
	a, b ids.NodeID
}

// observe records the network's faults at at: the longest delay of a route
// (0 for none, hold for a refused link), whether some honest node has no
// link without a delay, and whether one has no open link.
//
// A span that starts as another ends inherits what waits in it. Under a
// delay no longer than the old span's, a record may have waited through the
// old span and what it carried; under a longer one, the proxy holds a waiting
// record for the new delay from when it read it (proxy.wait), which is at
// most the old delay before the change.
func (f *faults) observe(at time.Time, delay time.Duration, cutOff, unlinked bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	next := faultSpan{span: span{start: at}, delay: delay, cutOff: cutOff, unlinked: unlinked}
	var last *faultSpan
	if n := len(f.spans); n > 0 && f.spans[n-1].end.IsZero() {
		last = &f.spans[n-1]
	}
	faulty := delay > 0 || cutOff || unlinked
	switch {
	case last == nil && !faulty:
		return
	case last == nil:
		f.spans = append(f.spans, next)
		return
	case last.delay == delay && last.cutOff == cutOff && last.unlinked == unlinked:
		return
	}

	last.end = at
	if !faulty {
		return
	}
	next.carried = last.carried + at.Sub(last.start)
	if delay > last.delay {
		next.carried = min(next.carried, last.delay)
	}
	f.spans = append(f.spans, next)
}

// cut records that the fuzzer cut the link between a and b at at.
func (f *faults) cut(at time.Time, a, b ids.NodeID) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cuts = append(f.cuts, linkCut{at: at, a: a, b: b})
}

// An excusal is what a fault span excuses one check: whether it excuses
// anything of s, and for how long after s ends. Each check's own predicate
// (cutOff, txPushExcused, linkExcused, txPullExcused) and its share of a run
// (excusedTime) read the same excusal.
type excusal func(s faultSpan) (bool, time.Duration)

// requestTail returns how long after s every request that s delayed got its
// reply or failed: recoverTail if s closed links, its effective delay if
// that is below the shortest request timeout, since no request failed and
// each delayed message arrived within it, and otherwise the longest request
// timeout. A propose op's hold lasts a few milliseconds, and a tail of a
// second after it covered dozens of ops.
func requestTail(s faultSpan) time.Duration {
	switch d := s.effectiveDelay(); {
	case s.closes():
		return recoverTail
	case d < minRequestTimeout:
		return d
	default:
		return maxRequestTimeout
	}
}

// progressExcusal excuses a span in which some honest node was cut off, with
// too few clean links to reach alpha from honest votes, until requestTail
// after it. A node that keeps alpha clean links stays live: every sample holds
// alpha honest votes among its own and theirs, whatever the Byzantine
// validator votes.
func progressExcusal(s faultSpan) (bool, time.Duration) {
	return s.cutOff, requestTail(s)
}

// txPushExcusal excuses a span in which a node had no open link, or that cut
// a node off and closed links, which loses what they held, until recoverTail
// after it. A delay that closes nothing only makes a tx push late, and a node
// that keeps one clean link pushes on it.
func txPushExcusal(s faultSpan) (bool, time.Duration) {
	return s.unlinked || s.cutOff && s.closes(), recoverTail
}

// linkExcusal excuses a span that can close links (closeDelay), until
// linkTail after it.
func linkExcusal(s faultSpan) (bool, time.Duration) {
	return s.closes(), linkTail
}

// txPullExcusal excuses every span until requestTail after it: a delayed tx
// pull reply races the pushes of the same txs.
func txPullExcusal(s faultSpan) (bool, time.Duration) {
	return true, requestTail(s)
}

// excuses reports whether e excuses some span at a time from from to to.
func (f *faults) excuses(e excusal, from, to time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.ContainsFunc(f.spans, func(s faultSpan) bool {
		ok, tail := e(s)
		return ok && s.near(from, to, tail)
	})
}

// excusedTime returns how much of the time from from to to e excuses: the
// union of the spans that it excuses, each with its tail. A check that is
// excused most of a run hardly judges it.
func (f *faults) excusedTime(e excusal, from, to time.Time) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	var (
		total time.Duration
		last  = from // the end of the time counted so far
	)
	// Spans are in time order, but tails differ, so a later span's window
	// can end before an earlier one's.
	for _, s := range f.spans {
		ok, tail := e(s)
		if !ok {
			continue
		}

		end := to
		if !s.end.IsZero() && s.end.Add(tail).Before(to) {
			end = s.end.Add(tail)
		}
		start := s.start
		if start.Before(last) {
			start = last
		}
		if end.After(start) {
			total += end.Sub(start)
			last = end
		}
	}

	return total
}

// excusedShares returns, for each check that faults excuse, the share of the
// time from from to to that it does not judge, for the test log.
func (f *faults) excusedShares(from, to time.Time) string {
	checks := []struct {
		name    string
		excusal excusal
	}{
		{name: "progress and commits", excusal: progressExcusal},
		{name: "tx push", excusal: txPushExcusal},
		{name: "honest links", excusal: linkExcusal},
		{name: "tx pull duplicates", excusal: txPullExcusal},
	}

	run := to.Sub(from)
	shares := make([]string, len(checks))
	for i, c := range checks {
		shares[i] = fmt.Sprintf("%s %.0f%%", c.name, 100*float64(f.excusedTime(c.excusal, from, to))/float64(run))
	}

	return fmt.Sprintf("%s of %s", strings.Join(shares, ", "), run.Round(time.Millisecond))
}

// cutOff reports whether a fault explains the network not making progress,
// or a node's verdict not being the network's, some time from from to to
// (progressExcusal). The progress and commits checks read it.
func (f *faults) cutOff(from, to time.Time) bool {
	return f.excuses(progressExcusal, from, to)
}

// txPushExcused reports whether a fault can have kept the tx push of a tx
// that a node's API took at at from every honest peer (txPushExcusal).
func (f *faults) txPushExcused(at time.Time) bool {
	return f.excuses(txPushExcusal, at, at)
}

// linkExcused reports whether a fault can have closed the link between
// honest nodes a and b at at: a span did (linkExcusal), or the fuzzer cut
// that very link less than linkTail before.
func (f *faults) linkExcused(at time.Time, a, b ids.NodeID) bool {
	if f.excuses(linkExcusal, at, at) {
		return true
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.ContainsFunc(f.cuts, func(c linkCut) bool {
		return (c.a == a && c.b == b || c.a == b && c.b == a) && !c.at.After(at) && at.Sub(c.at) <= linkTail
	})
}

// txPullExcused reports whether a fault can have made a tx pull reply that
// came at at carry txs that the node already held (txPullExcusal).
func (f *faults) txPullExcused(at time.Time) bool {
	return f.excuses(txPullExcusal, at, at)
}

// A bound ends a fault once an honest node has accepted some number of
// P-Chain blocks, or at a wall-clock cap, whichever comes first. Consensus
// edge cases sit at round boundaries, which the proxy cannot see inside TLS,
// so a fault keys off the heights that the nodes report (Stateful
// Fuzzing.md 2.1). The cap keeps a halted chain from holding the fault for
// good. A zero bound never ends the fault.
type bound struct {
	node     int       // the honest node whose height ends the fault
	accepted int       // the count of its accepted P-Chain blocks that ends it
	end      time.Time // the cap
}

// passed reports whether b has ended at now, with accepted counting each
// honest node's accepted P-Chain blocks.
func (b bound) passed(now time.Time, accepted func(node int) int) bool {
	return !b.end.IsZero() && (!now.Before(b.end) || accepted(b.node) >= b.accepted)
}

// An injector sets the fuzzer's faults on the links between honest nodes,
// through the proxy, ends each one that has a bound once it passes, and
// records them in faults. Its methods are safe to call from the runner and
// the goroutine that run starts.
type injector struct {
	proxy   *proxy
	addrs   []netip.AddrPort // the honest nodes' staking addresses
	nodeIDs []ids.NodeID     // in the order of addrs
	// accepted returns how many P-Chain blocks a node accepted.
	accepted func(node int) int
	faults   *faults

	mu sync.Mutex
	// persistent is the one link whose delays have no bound, if any.
	persistent link
	// delays holds the bound of each route's delay, and refused of each
	// refused link. A delay with no bound has no entry.
	delays  map[route]bound
	refused map[link]bound
	// holding reports whether holdAll's hold lasts, and saved holds the
	// delays that it replaced, by route, for release.
	holding bool
	saved   map[route]time.Duration
}

// setDelay sets the delay of the route from honest node from to honest node
// to, until b passes. A delay with no bound may sit on one link only: one on
// another link clears the delays of the old one that have no bound. The
// delays with no bound then take at most one link from each node, which
// leaves it alpha clean links, so they never halt the network, and the
// checks that a cut-off node excuses keep judging while they last
// (faults.cutOff). A bounded delay may sit on
// any link: it can halt the network until it passes.
func (in *injector) setDelay(from, to int, d time.Duration, b bound) {
	in.mu.Lock()
	defer in.mu.Unlock()

	r := route{from: in.addrs[from], to: in.addrs[to]}
	switch l := linkOf(r.from, r.to); {
	case d == 0:
		delete(in.delays, r)
	case !b.end.IsZero():
		if in.delays == nil {
			in.delays = make(map[route]bound)
		}
		in.delays[r] = b
	case l != in.persistent:
		for _, old := range []route{{from: in.persistent[0], to: in.persistent[1]}, {from: in.persistent[1], to: in.persistent[0]}} {
			if _, ok := in.delays[old]; !ok {
				in.proxy.setDelay(old, 0)
			}
		}
		in.persistent = l
		fallthrough
	default:
		delete(in.delays, r)
	}
	in.proxy.setDelay(r, d)
	in.sync(time.Now())
}

// holdAll holds both directions of every link between honest nodes, until
// release.
func (in *injector) holdAll() {
	in.mu.Lock()
	defer in.mu.Unlock()

	in.holding, in.saved = true, in.proxy.routeDelays()
	in.proxy.holdAll(in.addrs)
	in.sync(time.Now())
}

// release ends holdAll's hold: each route gets back the delay that it had
// before, unless its bound passed during the hold (expire).
func (in *injector) release() {
	in.mu.Lock()
	defer in.mu.Unlock()

	for _, from := range in.addrs {
		for _, to := range in.addrs {
			if from != to {
				r := route{from: from, to: to}
				in.proxy.setDelay(r, in.saved[r])
			}
		}
	}
	in.holding, in.saved = false, nil
	in.sync(time.Now())
}

// clearDelays sets the delay of every route to 0, with a bound or without.
// Refused links stay refused until their bounds pass.
func (in *injector) clearDelays() {
	in.mu.Lock()
	defer in.mu.Unlock()

	in.proxy.clearDelays()
	clear(in.delays)
	in.persistent = link{}
	in.sync(time.Now())
}

// cut closes the link between honest nodes a and b, losing what it held, and
// returns how many connections it closed. With a zero b, the nodes redial on
// their own; otherwise the proxy refuses the link until b passes.
func (in *injector) cut(a, b int, until bound) int {
	in.mu.Lock()
	defer in.mu.Unlock()

	l := linkOf(in.addrs[a], in.addrs[b])
	if !until.end.IsZero() {
		if in.refused == nil {
			in.refused = make(map[link]bound)
		}
		in.refused[l] = until
		in.proxy.refuse(l[0], l[1], true)
	}
	now := time.Now()
	n := in.proxy.cut(l[0], l[1])
	in.faults.cut(now, in.nodeIDs[a], in.nodeIDs[b])
	in.sync(now)

	return n
}

// expire ends each fault whose bound passed by now. A delay that holdAll
// replaced keeps the hold until release.
func (in *injector) expire(now time.Time) {
	in.mu.Lock()
	defer in.mu.Unlock()

	for r, b := range in.delays {
		if !b.passed(now, in.accepted) {
			continue
		}

		delete(in.delays, r)
		if in.holding {
			delete(in.saved, r)
			continue
		}
		in.proxy.setDelay(r, 0)
	}
	for l, b := range in.refused {
		if b.passed(now, in.accepted) {
			in.proxy.refuse(l[0], l[1], false)
			delete(in.refused, l)
		}
	}
	in.sync(now)
}

// run ends the faults whose bounds pass, and syncs the record and the votes
// with the links, until ctx ends. Links also close and reopen on their own.
func (in *injector) run(ctx context.Context) {
	const period = 10 * time.Millisecond

	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			in.expire(now)
		}
	}
}

// sync records the links' faults at now. A node is cut off while it has
// fewer than alpha links to honest nodes with no delay: a poll samples all
// validators but one (snow/engine/snowman/engine.go sendQuery), the node
// among them or not (snow/networking/sender/sender.go SendPushQuery), so a
// sample that leaves out one of its voters holds only as many honest votes
// as it has clean links. in.mu must be held.
func (in *injector) sync(now time.Time) {
	open := in.proxy.connected(in.addrs)
	cutOff := slices.ContainsFunc(in.proxy.cleanLinks(in.addrs), func(n int) bool {
		return n < alpha
	})
	in.faults.observe(now, in.proxy.maxDelay(), cutOff, slices.Contains(open, false))
}

// An opKind is the kind of op that op runs next.
type opKind uint8

const (
	issueKind opKind = iota
	delayKind
	proposeKind
	disconnectKind
	waitKind
)

// drawOp draws the kind of the next op, out of 100: 10 delay ops, 8 propose
// ops, 2 disconnect ops, 2 wait ops, and issue ops otherwise. A delay with no
// bound lasts until a later delay op clears it (half of them clear every
// delay), so it lasts about 20 ops, and links have delays for about half of
// a run. A propose op sends a block only in the proposer's slot, about 2
// times in 3 (proposeOp).
// A wait op sleeps up to 11 s, and a bounded fault can halt the network up to
// its cap, so both stay rare.
func drawOp(t *rapid.T) opKind {
	switch i := uniform(100).Draw(t, "op"); {
	case i < 10:
		return delayKind
	case i < 18:
		return proposeKind
	case i < 20:
		return disconnectKind
	case i < 22:
		return waitKind
	default:
		return issueKind
	}
}

// op runs the next op: usually an issue op, and sometimes a network fault or
// a propose op.
func (r *runner) op(t *rapid.T) {
	switch drawOp(t) {
	case delayKind:
		r.delayOp(t)
	case proposeKind:
		r.proposeOp(t)
	case disconnectKind:
		r.disconnectOp(t)
	case waitKind:
		r.waitOp(t)
	default:
		r.issue(t)
	}
}

// drawDelay draws a delay for one direction of a link, from 5 kinds with one
// chance each. Each kind reaches other code in the receiver:
//
//   - 0, which removes the direction's delay;
//   - up to 100 ms, which reorders messages across links;
//   - 150 ms to 1.5 s, around the request timeout (200 ms to 1 s), so some
//     replies come too late and the requester drops them;
//   - 5 s to 20 s, around the pong timeout (10 s), so links miss their read
//     deadline and the nodes close them. A delay past the ping period (1 s)
//     closes the link sooner: a pong that answers an older ping is
//     unexpected, and the node closes the link (network/peer/peer.go
//     handlePong);
//   - hold, which forwards nothing until the delay is lowered.
func drawDelay(t *rapid.T) time.Duration {
	ms := func(lo, hi int64) time.Duration {
		return time.Duration(rapid.Int64Range(lo, hi).Draw(t, "delayMs")) * time.Millisecond
	}

	switch uniform(5).Draw(t, "delayKind") {
	case 0:
		return 0
	case 1:
		return ms(1, 100)
	case 2:
		return ms(150, 1500)
	case 3:
		return ms(pongTimeout.Milliseconds()/2, 2*pongTimeout.Milliseconds())
	default:
		return hold
	}
}

// drawBound draws how long a fault lasts: 1 time in 2 with no bound, and
// otherwise until a drawn honest node accepts 1 to 3 more P-Chain blocks, or
// for faultCap, whichever comes first.
func (r *runner) drawBound(t *rapid.T) bound {
	// Past a pong timeout, so a hold reaches the read deadline, and short,
	// since the checks that a cut-off node excuses do not judge while it
	// halts the network.
	const faultCap = pongTimeout + 2*time.Second

	if uniform(2).Draw(t, "bounded") == 0 {
		return bound{}
	}

	node := uniform(len(r.stakingAddrs)).Draw(t, "boundNode")
	blocks := 1 + uniform(3).Draw(t, "boundBlocks")
	return bound{node: node, accepted: r.injector.accepted(node) + blocks, end: time.Now().Add(faultCap)}
}

// setBound adds b to the running op's event, and returns it as text.
func (r *runner) setBound(b bound) string {
	if b.end.IsZero() {
		return "no bound"
	}

	r.ev.Set("until_node", r.nodeDraw(b.node))
	r.ev.Set("until_accepted", b.accepted)
	return fmt.Sprintf("until node %d accepts %d P-Chain blocks or %s", b.node, b.accepted, b.end.Format(time.TimeOnly))
}

// drawLink draws two distinct honest nodes.
func (r *runner) drawLink(t *rapid.T) (int, int) {
	n := len(r.stakingAddrs)
	from := uniform(n).Draw(t, "from")
	to := (from + 1 + uniform(n-1).Draw(t, "to")) % n

	return from, to
}

// delayOp runs a delay op as the next step. It either clears every delay, or
// sets the delay of one direction between two honest nodes, which t draws
// (drawDelay), with a bound or none (drawBound). Only one link has delays
// with no bound (injector.setDelay).
func (r *runner) delayOp(t *rapid.T) {
	r.step++
	_, r.ev = r.events.Start(t.Context(), "delay")
	r.ev.Set("step", r.step)
	defer r.endEvent()

	// Half of delay ops clear every delay. A delay past the ping period slows
	// every op on its link, and the checks that faults excuses do not judge
	// while it lasts, so delays must not last most of a run.
	if uniform(2).Draw(t, "clearDelays") == 0 {
		r.injector.clearDelays()
		r.ev.Set("clear", true)
		t.Log("ran op delay: cleared every delay")
		return
	}

	from, to := r.drawLink(t)
	d := drawDelay(t)
	b := r.drawBound(t)
	r.injector.setDelay(from, to, d, b)

	delay := d.String()
	if d == hold {
		delay = "hold"
	}
	r.ev.Set("from", r.nodeDraw(from))
	r.ev.Set("to", r.nodeDraw(to))
	r.ev.Set("delay", delay)
	t.Logf("ran op delay: node %d to node %d, %s, %s", from, to, delay, r.setBound(b))
}

// disconnectOp runs a disconnect op as the next step: it cuts the link
// between two honest nodes that t draws, both TCP connections, and loses
// what they held. With no bound, the nodes redial on their own; with one,
// the proxy refuses the link until the bound passes (drawBound). The
// honest-link check excuses the drop (faults.linkExcused).
func (r *runner) disconnectOp(t *rapid.T) {
	r.step++
	_, r.ev = r.events.Start(t.Context(), "disconnect")
	r.ev.Set("step", r.step)
	defer r.endEvent()

	a, b := r.drawLink(t)
	until := r.drawBound(t)
	closed := r.injector.cut(a, b, until)

	r.ev.Set("from", r.nodeDraw(a))
	r.ev.Set("to", r.nodeDraw(b))
	r.ev.Set("closed", closed)
	t.Logf("ran op disconnect: node %d and node %d, %d connections, %s", a, b, closed, r.setBound(until))
}

// waitOp runs a wait op as the next step: it lets the nodes run while the
// faults in place last, for just under or just over a node timer: the
// shortest and the longest request timeout, the ping period, or the pong
// timeout, so a fault that holds a link meets the timer's both sides. The
// longest request timeout is the ping period on the test network.
func (r *runner) waitOp(t *rapid.T) {
	r.step++
	_, r.ev = r.events.Start(t.Context(), "wait")
	r.ev.Set("step", r.step)
	defer r.endEvent()

	timers := []time.Duration{minRequestTimeout, maxRequestTimeout, pingFrequency, pongTimeout}
	timer := timers[uniform(len(timers)).Draw(t, "waitTimer")]
	d := timer * 11 / 10
	if uniform(2).Draw(t, "waitOver") == 0 {
		d = timer * 9 / 10
	}
	r.ev.Set("wait", d.String())
	select {
	case <-time.After(d):
	case <-t.Context().Done():
	}
	t.Logf("ran op wait: %s", d)
}
