// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sync"
	"syscall"
	"time"
)

// A memNet carries the test network's connections in memory, under loopback
// addresses: the nodes' listeners, their links, and the API clients'
// requests. A goroutine blocked on a socket does not count as idle in a
// synctest bubble, so time there would not move while a node waits for a
// peer; one blocked on a memNet connection does. The zero value is ready to
// use.
type memNet struct {
	mu        sync.Mutex
	listeners map[netip.AddrPort]*memListener
	lastPort  uint16
}

// Listen ignores network. Port 0 picks a free port.
func (m *memNet) Listen(_, address string) (net.Listener, error) {
	// The usual SOMAXCONN.
	const backlog = 128

	addr, err := netip.ParseAddrPort(address)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if addr.Port() == 0 {
		addr = netip.AddrPortFrom(addr.Addr(), m.freePort(addr.Addr()))
	}
	if _, ok := m.listeners[addr]; ok {
		return nil, fmt.Errorf("listen %s: %w", addr, syscall.EADDRINUSE)
	}

	l := &memListener{
		net:     m,
		addr:    addr,
		backlog: make(chan *memConn, backlog),
		closed:  make(chan struct{}),
	}
	if m.listeners == nil {
		m.listeners = make(map[netip.AddrPort]*memListener)
	}
	m.listeners[addr] = l

	return l, nil
}

// DialContext ignores network. Its connections have no latency.
func (m *memNet) DialContext(ctx context.Context, _, address string) (net.Conn, error) {
	return m.dial(ctx, address, nil, nil)
}

// dial connects to the listener at address. What the dialer writes waits
// out, and what it reads waits in. A nil latency is none.
func (m *memNet) dial(ctx context.Context, address string, out, in latency) (*memConn, error) {
	addr, err := netip.ParseAddrPort(address)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	l, ok := m.listeners[addr]
	local := netip.AddrPortFrom(addr.Addr(), m.freePort(addr.Addr()))
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("dial %s: %w", addr, syscall.ECONNREFUSED)
	}

	up, down := &pipe{latency: out}, &pipe{latency: in}
	done := &connDone{ch: make(chan struct{})}
	c := &memConn{in: down, out: up, local: local, remote: addr, done: done}
	accepted := &memConn{in: up, out: down, local: addr, remote: local, done: done}

	select {
	case l.backlog <- accepted:
	case <-l.closed:
		return nil, fmt.Errorf("dial %s: %w", addr, syscall.ECONNREFUSED)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Close may have drained the backlog before the send.
	select {
	case <-l.closed:
		accepted.reset()
	default:
	}

	return c, nil
}

// freePort returns a port that no listener on ip holds, from the ephemeral
// range, as the OS picks one. m.mu must be held.
func (m *memNet) freePort(ip netip.Addr) uint16 {
	const firstPort = 49152

	for {
		m.lastPort++
		if m.lastPort < firstPort { // unset, or wrapped past 65535
			m.lastPort = firstPort
		}
		if _, ok := m.listeners[netip.AddrPortFrom(ip, m.lastPort)]; !ok {
			return m.lastPort
		}
	}
}

// A memListener completes dials into its backlog, as a TCP listener does, so
// a dial does not wait for Accept.
type memListener struct {
	net     *memNet
	addr    netip.AddrPort
	backlog chan *memConn
	closed  chan struct{}
	once    sync.Once
}

func (l *memListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.backlog:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *memListener) Addr() net.Addr {
	return net.TCPAddrFromAddrPort(l.addr)
}

// Close frees l's address, and resets the connections that l did not
// accept, as a TCP listener does.
func (l *memListener) Close() error {
	l.net.mu.Lock()
	if l.net.listeners[l.addr] == l {
		delete(l.net.listeners, l.addr)
	}
	l.net.mu.Unlock()

	l.once.Do(func() { close(l.closed) })
	for {
		select {
		case c := <-l.backlog:
			c.reset()
		default:
			return nil
		}
	}
}

// A latency returns how long bytes wait in a pipe, or [hold] to keep them
// until it changes, and a channel that is closed when it changes.
type latency func() (time.Duration, <-chan struct{})

// A memConn is one end of an in-memory connection.
type memConn struct {
	in, out       *pipe
	local, remote netip.AddrPort
	done          *connDone
}

// A connDone is closed when either end of a connection closes.
type connDone struct {
	ch   chan struct{}
	once sync.Once
}

func (d *connDone) close() {
	d.once.Do(func() { close(d.ch) })
}

func (c *memConn) Read(b []byte) (int, error) {
	return c.in.read(b)
}

func (c *memConn) Write(b []byte) (int, error) {
	return c.out.write(b)
}

// Close lets the peer read what c wrote, then EOF.
func (c *memConn) Close() error {
	c.in.stop(net.ErrClosed)
	c.out.closeWrite()
	c.done.close()

	return nil
}

// reset closes both directions and drops what they hold, as a TCP reset
// does.
func (c *memConn) reset() {
	c.in.stop(syscall.ECONNRESET)
	c.out.stop(syscall.ECONNRESET)
	c.done.close()
}

func (c *memConn) LocalAddr() net.Addr {
	return net.TCPAddrFromAddrPort(c.local)
}

func (c *memConn) RemoteAddr() net.Addr {
	return net.TCPAddrFromAddrPort(c.remote)
}

func (c *memConn) SetDeadline(t time.Time) error {
	c.in.setDeadline(t, true)
	c.out.setDeadline(t, false)

	return nil
}

func (c *memConn) SetReadDeadline(t time.Time) error {
	c.in.setDeadline(t, true)

	return nil
}

func (c *memConn) SetWriteDeadline(t time.Time) error {
	c.out.setDeadline(t, false)

	return nil
}

// A pipe carries one direction of a connection. Each write waits in it for
// the latency, as the latency is when the write's turn comes, so lowering
// the latency releases writes that already wait. A write also waits for the
// writes before it.
type pipe struct {
	latency latency

	mu          sync.Mutex
	chunks      []chunk
	queued      int   // bytes in chunks
	err         error // why the pipe stopped
	writeClosed bool
	readBy      time.Time // deadlines
	writeBy     time.Time
	changed     chan struct{} // closed when the fields above change
}

// A chunk is one write, and when it was written.
type chunk struct {
	b       []byte
	written time.Time
}

func (p *pipe) read(b []byte) (int, error) {
	for {
		var (
			d            time.Duration
			delayChanged <-chan struct{}
		)
		if p.latency != nil {
			// Before p.mu: the proxy locks p.mu under its own lock.
			d, delayChanged = p.latency()
		}

		p.mu.Lock()
		now := time.Now()
		var wake time.Time
		switch {
		case p.err != nil:
			p.mu.Unlock()
			return 0, p.err
		case len(p.chunks) > 0:
			head := &p.chunks[0]
			if d == hold {
				break
			}
			due := head.written.Add(d)
			if now.Before(due) {
				wake = due
				break
			}

			n := copy(b, head.b)
			head.b = head.b[n:]
			if len(head.b) == 0 {
				p.chunks = p.chunks[1:]
			}
			p.queued -= n
			p.signal() // a writer may wait for room
			p.mu.Unlock()
			return n, nil
		case p.writeClosed:
			p.mu.Unlock()
			return 0, io.EOF
		}
		if !p.readBy.IsZero() {
			if !now.Before(p.readBy) {
				p.mu.Unlock()
				return 0, os.ErrDeadlineExceeded
			}
			if wake.IsZero() || p.readBy.Before(wake) {
				wake = p.readBy
			}
		}
		changed := p.changedCh()
		p.mu.Unlock()

		wait(wake, changed, delayChanged)
	}
}

func (p *pipe) write(b []byte) (int, error) {
	// A node's messages are up to a few MiB; a full buffer makes the writer
	// wait for the reader, as a full socket buffer does.
	const bufSize = 1 << 20

	for {
		p.mu.Lock()
		switch {
		case p.writeClosed:
			p.mu.Unlock()
			return 0, net.ErrClosed
		case errors.Is(p.err, net.ErrClosed):
			p.mu.Unlock()
			return 0, syscall.EPIPE // the reader closed
		case p.err != nil:
			p.mu.Unlock()
			return 0, p.err
		case !p.writeBy.IsZero() && !time.Now().Before(p.writeBy):
			p.mu.Unlock()
			return 0, os.ErrDeadlineExceeded
		case p.queued < bufSize:
			if len(b) > 0 {
				p.chunks = append(p.chunks, chunk{b: append([]byte(nil), b...), written: time.Now()})
				p.queued += len(b)
				p.signal()
			}
			p.mu.Unlock()
			return len(b), nil
		}
		writeBy, changed := p.writeBy, p.changedCh()
		p.mu.Unlock()

		wait(writeBy, changed, nil)
	}
}

// wait blocks until t, if it is set, or until either channel is closed.
func wait(t time.Time, a, b <-chan struct{}) {
	var timeout <-chan time.Time
	if !t.IsZero() {
		timer := time.NewTimer(time.Until(t))
		defer timer.Stop()
		timeout = timer.C
	}

	select {
	case <-timeout:
	case <-a:
	case <-b:
	}
}

// stop makes reads and writes fail with err, and drops what p holds.
func (p *pipe) stop(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.err == nil {
		p.err = err
		p.chunks, p.queued = nil, 0
	}
	p.signal()
}

func (p *pipe) closeWrite() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.writeClosed = true
	p.signal()
}

func (p *pipe) setDeadline(t time.Time, read bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if read {
		p.readBy = t
	} else {
		p.writeBy = t
	}
	p.signal()
}

// changedCh returns a channel that is closed when p changes. p.mu must be
// held.
func (p *pipe) changedCh() <-chan struct{} {
	if p.changed == nil {
		p.changed = make(chan struct{})
	}

	return p.changed
}

// signal wakes the reads and writes that wait for p to change. p.mu must be
// held.
func (p *pipe) signal() {
	if p.changed != nil {
		close(p.changed)
		p.changed = nil
	}
}
