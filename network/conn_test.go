// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import "net"

var _ net.Conn = (*testConn)(nil)

type testConn struct {
	net.Conn

	localAddr  net.Addr
	remoteAddr net.Addr
}

func (c *testConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *testConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}
