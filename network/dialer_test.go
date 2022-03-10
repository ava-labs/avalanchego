// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"net"

	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/utils"
)

var (
	errRefused = errors.New("connection refused")

	_ dialer.Dialer = &testDialer{}
)

type testDialer struct {
	// maps [ip.String] to a listener
	listeners map[string]*testListener
}

func newTestDialer() *testDialer {
	return &testDialer{
		listeners: make(map[string]*testListener),
	}
}

func (d *testDialer) NewListener() (utils.DynamicIPDesc, *testListener) {
	ip := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		uint16(len(d.listeners)),
	)
	staticIP := ip.IP()
	listener := newTestListener(staticIP)
	d.AddListener(staticIP, listener)
	return ip, listener
}

func (d *testDialer) AddListener(ip utils.IPDesc, listener *testListener) {
	d.listeners[ip.String()] = listener
}

func (d *testDialer) Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error) {
	listener, ok := d.listeners[ip.String()]
	if !ok {
		return nil, errRefused
	}
	serverConn, clientConn := net.Pipe()
	server := &testConn{
		Conn: serverConn,
		localAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		remoteAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 1,
		},
	}
	client := &testConn{
		Conn: clientConn,
		localAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		remoteAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 3,
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
