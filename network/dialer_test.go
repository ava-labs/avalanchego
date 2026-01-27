// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"net"
	"net/netip"

	"github.com/ava-labs/avalanchego/network/dialer"
)

var (
	errRefused = errors.New("connection refused")

	_ dialer.Dialer = (*testDialer)(nil)
)

type testDialer struct {
	// maps [ip.String] to a listener
	listeners map[netip.AddrPort]*testListener
}

func newTestDialer() *testDialer {
	return &testDialer{
		listeners: make(map[netip.AddrPort]*testListener),
	}
}

func (d *testDialer) NewListener() (netip.AddrPort, *testListener) {
	// Uses a private IP to easily enable testing AllowPrivateIPs
	addrPort := netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{10, 0, 0, 0}),
		uint16(len(d.listeners)+1),
	)
	listener := newTestListener(addrPort)
	d.AddListener(addrPort, listener)
	return addrPort, listener
}

func (d *testDialer) AddListener(ip netip.AddrPort, listener *testListener) {
	d.listeners[ip] = listener
}

func (d *testDialer) Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error) {
	listener, ok := d.listeners[ip]
	if !ok {
		return nil, errRefused
	}
	serverConn, clientConn := net.Pipe()
	server := &testConn{
		Conn: serverConn,
		localAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 1,
		},
		remoteAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
	}
	client := &testConn{
		Conn: clientConn,
		localAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 3,
		},
		remoteAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 4,
		},
	}
	select {
	case listener.inbound <- server:
		return client, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-listener.closed:
		return nil, errRefused
	}
}
