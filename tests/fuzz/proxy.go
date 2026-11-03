// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"errors"
	"maps"
	"math"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/set"
)

// A proxy carries every connection that a node dials to a peer, and sets
// how long each direction's bytes take to arrive. It never reads them, so it
// can only delay, hold, and release traffic, as a real network can.
//
// Each direction between two nodes has a delay, 0 unless setDelay changed
// it. Each write arrives no sooner than the delay after it was written, in
// the order written, and writes with a delay of [hold] wait until the delay
// is lowered. A write waits for the delay as it is when the write's turn
// comes, so lowering a delay releases writes that already wait.
//
// A cut resets a link's connections, losing what they held, and the proxy
// can refuse to dial the link again until the cut ends.
//
// The zero value is ready to use.
type proxy struct {
	// net carries the links.
	net memNet

	mu      sync.Mutex
	conns   map[*memConn]link // the dialers' ends of the open links
	closed  bool
	delays  map[route]time.Duration
	refused map[link]struct{}
	// changed is closed when a delay changes, to wake the reads that wait.
	// delay makes a new one.
	changed chan struct{}
}

// hold is the delay of a direction that delivers nothing until the delay is
// lowered.
const hold = time.Duration(math.MaxInt64)

// A route is one direction between two nodes, which the proxy names by their
// staking addresses.
type route struct {
	from, to netip.AddrPort
}

// errRefused means the proxy refuses to carry a link that was cut.
var errRefused = errors.New("link is cut")

// A link is the two nodes at either end of a connection, by their staking
// addresses, in order.
type link [2]netip.AddrPort

// linkOf returns the link between a and b.
func linkOf(a, b netip.AddrPort) link {
	if b.Compare(a) < 0 {
		a, b = b, a
	}

	return link{a, b}
}

// A proxyDialer dials through proxy for the node that listens at from. A
// node that dials a peer sends on the route from its own staking address to
// the peer's, and gets the replies on the route back.
type proxyDialer struct {
	proxy *proxy
	from  netip.AddrPort
}

// DialContext dials address through the proxy. It has the signature of
// DialContext in [node.Options].
func (d *proxyDialer) DialContext(ctx context.Context, _, address string) (net.Conn, error) {
	to, err := netip.ParseAddrPort(address)
	if err != nil {
		return nil, err
	}

	p := d.proxy
	out, in := route{from: d.from, to: to}, route{from: to, to: d.from}
	c, err := p.net.dial(ctx, address, func() (time.Duration, <-chan struct{}) { return p.delay(out) }, func() (time.Duration, <-chan struct{}) { return p.delay(in) })
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	l := linkOf(d.from, to)
	if p.closed {
		c.reset() // the link was never used
		return nil, errors.New("proxy is closed")
	}
	if _, ok := p.refused[l]; ok {
		c.reset() // the link was never used
		return nil, errRefused
	}
	if p.conns == nil {
		p.conns = make(map[*memConn]link)
	}
	p.conns[c] = l

	return c, nil
}

// delay returns the delay of r, and a channel that is closed when a delay
// changes.
func (p *proxy) delay(r route) (time.Duration, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.changed == nil {
		p.changed = make(chan struct{})
	}

	return p.delays[r], p.changed
}

// setDelay sets the delay of r to d, which may be [hold]. It applies to the
// writes that already wait on r too.
func (p *proxy) setDelay(r route, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if d == 0 {
		delete(p.delays, r)
	} else {
		if p.delays == nil {
			p.delays = make(map[route]time.Duration)
		}
		p.delays[r] = d
	}
	p.wake()
}

// routeDelays returns the delay of each route that has one.
func (p *proxy) routeDelays() map[route]time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	return maps.Clone(p.delays)
}

// holdAll holds both directions between each pair of addrs.
func (p *proxy) holdAll(addrs []netip.AddrPort) {
	for _, from := range addrs {
		for _, to := range addrs {
			if from != to {
				p.setDelay(route{from: from, to: to}, hold)
			}
		}
	}
}

// cleanLinks counts, for each of addrs, the others of addrs that it has an
// open connection to with no delay in either direction.
func (p *proxy) cleanLinks(addrs []netip.AddrPort) []int {
	return p.links(addrs, true)
}

// connected reports, for each of addrs, whether it has an open connection to
// another of addrs.
func (p *proxy) connected(addrs []netip.AddrPort) []bool {
	open := make([]bool, len(addrs))
	for i, n := range p.links(addrs, false) {
		open[i] = n > 0
	}

	return open
}

// links counts, for each of addrs, the others of addrs that it has an open
// connection to, with no delay in either direction if clean is true. A node
// can hold two connections to one peer, which count once.
func (p *proxy) links(addrs []netip.AddrPort, clean bool) []int {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.dropClosed()
	peers := make([]set.Set[netip.AddrPort], len(addrs))
	for _, l := range p.conns {
		a, b := l[0], l[1]
		if clean && (p.delays[route{from: a, to: b}] != 0 || p.delays[route{from: b, to: a}] != 0) {
			continue
		}
		i, j := slices.Index(addrs, a), slices.Index(addrs, b)
		if i >= 0 && j >= 0 {
			peers[i].Add(b)
			peers[j].Add(a)
		}
	}

	counts := make([]int, len(addrs))
	for i, s := range peers {
		counts[i] = s.Len()
	}

	return counts
}

// clearDelays sets the delay of every route to 0.
func (p *proxy) clearDelays() {
	p.mu.Lock()
	defer p.mu.Unlock()

	clear(p.delays)
	p.wake()
}

// maxDelay returns the longest delay of a route, or [hold] if a link is
// refused.
func (p *proxy) maxDelay() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.refused) > 0 {
		return hold
	}
	var longest time.Duration
	for _, d := range p.delays {
		longest = max(longest, d)
	}

	return longest
}

// cut resets every connection between a and b, and returns how many it
// reset. What they held is lost. The delays of both routes stay.
func (p *proxy) cut(a, b netip.AddrPort) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.dropClosed()
	cut := linkOf(a, b)
	var n int
	for c, l := range p.conns {
		if l == cut {
			delete(p.conns, c)
			c.reset()
			n++
		}
	}

	return n
}

// dropClosed forgets the connections that either end closed. p.mu must be
// held.
func (p *proxy) dropClosed() {
	for c := range p.conns {
		select {
		case <-c.done.ch:
			delete(p.conns, c)
		default:
		}
	}
}

// refuse sets whether the proxy refuses to dial between a and b. It does not
// close the connections that are open; cut does.
func (p *proxy) refuse(a, b netip.AddrPort, refused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	l := linkOf(a, b)
	if !refused {
		delete(p.refused, l)
		return
	}
	if p.refused == nil {
		p.refused = make(map[link]struct{})
	}
	p.refused[l] = struct{}{}
}

// wake wakes the reads that wait for a delay to change. p.mu must be
// held.
func (p *proxy) wake() {
	if p.changed != nil {
		close(p.changed)
		p.changed = nil
	}
}

// Close resets every link, and refuses to dial more.
func (p *proxy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	for c := range p.conns {
		c.reset()
	}
	clear(p.conns)
}
