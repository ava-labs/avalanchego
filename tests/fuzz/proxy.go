// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"sync"
)

// A proxy carries every connection that a node dials to a peer. It forwards
// whole TLS records in each direction and never decrypts them, so it can
// only hold and release traffic, as a real network can.
//
// Bytes that the proxy has not read yet wait in the kernel's socket buffers.
// A sender blocks only when those fill, as on a stalled real link.
//
// The zero value is ready to use.
type proxy struct {
	mu     sync.Mutex
	conns  map[net.Conn]struct{} // the proxy's ends of open links
	closed bool

	wg sync.WaitGroup
}

// DialContext dials address and returns a connection to it through the
// proxy. It has the signature of DialContext in [node.Options].
func (p *proxy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	remote, err := d.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	local, inner, err := loopbackPair(ctx)
	if err != nil {
		_ = remote.Close() // the link was never used
		return nil, err
	}

	if !p.track(inner, remote) {
		_ = local.Close() // the proxy is closed, so nothing uses the link
		return nil, errors.New("proxy is closed")
	}
	go p.forward(remote, inner)
	go p.forward(inner, remote)

	return local, nil
}

// track records the proxy's ends of a new link, and reports whether the
// proxy is still open. If it is, the caller must start two forwarders.
func (p *proxy) track(inner, remote net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		_ = inner.Close() // the link was never used
		_ = remote.Close()
		return false
	}

	if p.conns == nil {
		p.conns = make(map[net.Conn]struct{})
	}
	p.conns[inner] = struct{}{}
	p.conns[remote] = struct{}{}
	p.wg.Add(2)
	return true
}

// forward copies whole TLS records from src to dst until either side fails.
// Then it closes both, which also ends the forwarder in the other direction.
func (p *proxy) forward(dst, src net.Conn) {
	defer p.wg.Done()
	defer p.untrack(src, dst)

	// A TLS record is a 5-byte header, whose last 2 bytes hold the length of
	// the body, and then the body.
	const headerLen = 5
	buf := make([]byte, headerLen+math.MaxUint16)
	for {
		if _, err := io.ReadFull(src, buf[:headerLen]); err != nil {
			return
		}

		n := headerLen + int(binary.BigEndian.Uint16(buf[3:headerLen]))
		if _, err := io.ReadFull(src, buf[headerLen:n]); err != nil {
			return
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func (p *proxy) untrack(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, c := range conns {
		delete(p.conns, c)
		_ = c.Close() // a link that failed has nothing left to report
	}
}

// Close closes every link and waits until the proxy stops.
func (p *proxy) Close() {
	p.mu.Lock()
	p.closed = true
	for c := range p.conns {
		_ = c.Close() // the forwarders see the close and exit
	}
	p.mu.Unlock()

	p.wg.Wait()
}

// loopbackPair returns both ends of a new loopback TCP connection.
func loopbackPair(ctx context.Context) (net.Conn, net.Conn, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = ln.Close() // it accepts one connection only
	}()

	// Buffered, so the goroutine exits even if the dial fails.
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept() // a failed accept sends nil
		accepted <- c
	}()

	var d net.Dialer
	dialed, err := d.DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		return nil, nil, err
	}

	c := <-accepted
	if c == nil {
		_ = dialed.Close() // the link has no other end
		return nil, nil, errors.New("loopback listener did not accept the connection")
	}

	return dialed, c, nil
}
