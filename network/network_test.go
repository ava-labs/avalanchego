// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/version"
)

var (
	errClosed  = errors.New("closed")
	errRefused = errors.New("connection refused")
)

type testListener struct {
	addr    net.Addr
	inbound chan net.Conn
}

func (l *testListener) Accept() (net.Conn, error) {
	if c, ok := <-l.inbound; ok {
		return c, nil
	}
	return nil, errClosed
}

func (l *testListener) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errClosed
		}
	}()
	close(l.inbound)
	return
}

func (l *testListener) Addr() net.Addr { return l.addr }

type testDialer struct {
	addr      net.Addr
	outbounds map[string]*testListener
}

func (d *testDialer) Dial(ip utils.IPDesc) (net.Conn, error) {
	outbound, ok := d.outbounds[ip.String()]
	if !ok {
		return nil, errRefused
	}
	server := &testConn{
		pendingReads:  make(chan []byte, 1<<10),
		pendingWrites: make(chan []byte, 1<<10),
		local:         outbound.addr,
		remote:        d.addr,
	}
	client := &testConn{
		pendingReads:  server.pendingWrites,
		pendingWrites: server.pendingReads,
		local:         d.addr,
		remote:        outbound.addr,
	}
	outbound.inbound <- server
	return client, nil
}

type testConn struct {
	partialRead   []byte
	pendingReads  chan []byte
	pendingWrites chan []byte

	local, remote net.Addr
}

func (c *testConn) Read(b []byte) (int, error) {
	for len(c.partialRead) == 0 {
		read, ok := <-c.pendingReads
		if !ok {
			return 0, errClosed
		}
		c.partialRead = read
	}

	copy(b, c.partialRead)
	if length := len(c.partialRead); len(b) > length {
		c.partialRead = nil
		return length, nil
	}
	c.partialRead = c.partialRead[len(b):]
	return len(b), nil
}

func (c *testConn) Write(b []byte) (length int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errClosed
		}
	}()

	newB := make([]byte, len(b))
	copy(newB, b)
	c.pendingWrites <- newB
	length = len(b)
	return
}

func (c *testConn) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errClosed
		}
	}()
	close(c.pendingReads)
	close(c.pendingWrites)
	return
}

func (c *testConn) LocalAddr() net.Addr              { return c.local }
func (c *testConn) RemoteAddr() net.Addr             { return c.remote }
func (c *testConn) SetDeadline(time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(time.Time) error { return nil }

type testHandler struct {
	connected    func(ids.ShortID) bool
	disconnected func(ids.ShortID) bool
}

func (h *testHandler) Connected(id ids.ShortID) bool {
	return h.connected != nil && h.connected(id)
}
func (h *testHandler) Disconnected(id ids.ShortID) bool {
	return h.disconnected != nil && h.disconnected(id)
}

func TestNewDefaultNetwork(t *testing.T) {
	log := logging.NoLog{}
	id := ids.ShortEmpty
	ip := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 0,
	}
	networkID := uint32(0)
	appVersion := version.NewDefaultVersion("app", 0, 1, 0)
	versionParser := version.NewDefaultParser()

	listener := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
	}
	caller := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		outbounds: make(map[string]*testListener),
	}
	serverUpgrader := NewIPUpgrader()
	clientUpgrader := NewIPUpgrader()

	vdrs := validators.NewSet()
	handler := router.Router(nil)

	net := NewDefaultNetwork(
		log,
		id,
		ip,
		networkID,
		appVersion,
		versionParser,
		listener,
		caller,
		serverUpgrader,
		clientUpgrader,
		vdrs,
		handler,
	)
	assert.NotNil(t, net)

	go func() {
		err := net.Close()
		assert.NoError(t, err)
	}()

	err := net.Dispatch()
	assert.Error(t, err)
}

func TestEstablishConnection(t *testing.T) {
	log := logging.NoLog{}
	networkID := uint32(0)
	appVersion := version.NewDefaultVersion("app", 0, 1, 0)
	versionParser := version.NewDefaultParser()

	id0 := ids.NewShortID([20]byte{0})
	ip0 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 0,
	}
	id1 := ids.NewShortID([20]byte{1})
	ip1 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 1,
	}

	listener0 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
	}
	caller0 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		outbounds: make(map[string]*testListener),
	}
	listener1 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 1,
		},
		inbound: make(chan net.Conn, 1<<10),
	}
	caller1 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 1,
		},
		outbounds: make(map[string]*testListener),
	}

	caller0.outbounds[ip1.String()] = listener1
	caller1.outbounds[ip0.String()] = listener0

	serverUpgrader := NewIPUpgrader()
	clientUpgrader := NewIPUpgrader()

	vdrs := validators.NewSet()
	handler := router.Router(nil)

	net0 := NewDefaultNetwork(
		log,
		id0,
		ip0,
		networkID,
		appVersion,
		versionParser,
		listener0,
		caller0,
		serverUpgrader,
		clientUpgrader,
		vdrs,
		handler,
	)
	assert.NotNil(t, net0)

	net1 := NewDefaultNetwork(
		log,
		id1,
		ip1,
		networkID,
		appVersion,
		versionParser,
		listener1,
		caller1,
		serverUpgrader,
		clientUpgrader,
		vdrs,
		handler,
	)
	assert.NotNil(t, net1)

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	h0 := &testHandler{
		connected: func(id ids.ShortID) bool {
			if !id.Equals(id0) {
				wg0.Done()
			}
			return false
		},
	}
	h1 := &testHandler{
		connected: func(id ids.ShortID) bool {
			if !id.Equals(id1) {
				wg1.Done()
			}
			return false
		},
	}

	net0.RegisterHandler(h0)
	net1.RegisterHandler(h1)

	net0.Track(ip1)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()

	wg0.Wait()
	wg1.Wait()
}
