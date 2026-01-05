// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net"
	"net/netip"
)

var _ net.Listener = (*testListener)(nil)

type testListener struct {
	ip      netip.AddrPort
	inbound chan net.Conn
	closed  chan struct{}
}

func newTestListener(ip netip.AddrPort) *testListener {
	return &testListener{
		ip:      ip,
		inbound: make(chan net.Conn),
		closed:  make(chan struct{}),
	}
}

func (l *testListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.inbound:
		return c, nil
	case <-l.closed:
		return nil, errClosed
	}
}

func (l *testListener) Close() error {
	close(l.closed)
	return nil
}

func (l *testListener) Addr() net.Addr {
	return &net.TCPAddr{
		IP:   l.ip.Addr().AsSlice(),
		Port: int(l.ip.Port()),
	}
}
