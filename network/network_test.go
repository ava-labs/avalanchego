// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errClosed  = errors.New("closed")
	errRefused = errors.New("connection refused")
)

type testListener struct {
	addr    net.Addr
	inbound chan net.Conn
	once    sync.Once
	closed  chan struct{}
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
	l.once.Do(func() { close(l.closed) })
	return nil
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
		closed:        make(chan struct{}),
		local:         outbound.addr,
		remote:        d.addr,
	}
	client := &testConn{
		pendingReads:  server.pendingWrites,
		pendingWrites: server.pendingReads,
		closed:        make(chan struct{}),
		local:         d.addr,
		remote:        outbound.addr,
	}

	select {
	case outbound.inbound <- server:
		return client, nil
	default:
		return nil, errRefused
	}
}

type testConn struct {
	partialRead   []byte
	pendingReads  chan []byte
	pendingWrites chan []byte
	closed        chan struct{}
	once          sync.Once

	local, remote net.Addr
}

func (c *testConn) Read(b []byte) (int, error) {
	for len(c.partialRead) == 0 {
		select {
		case read, ok := <-c.pendingReads:
			if !ok {
				return 0, errClosed
			}
			c.partialRead = read
		case <-c.closed:
			return 0, errClosed
		}
	}

	copy(b, c.partialRead)
	if length := len(c.partialRead); len(b) > length {
		c.partialRead = nil
		return length, nil
	}
	c.partialRead = c.partialRead[len(b):]
	return len(b), nil
}

func (c *testConn) Write(b []byte) (int, error) {
	newB := make([]byte, len(b))
	copy(newB, b)

	select {
	case c.pendingWrites <- newB:
	case <-c.closed:
		return 0, errClosed
	}

	return len(b), nil
}

func (c *testConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *testConn) LocalAddr() net.Addr              { return c.local }
func (c *testConn) RemoteAddr() net.Addr             { return c.remote }
func (c *testConn) SetDeadline(time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(time.Time) error { return nil }

type testHandler struct {
	router.Router
	connected    func(ids.ShortID)
	disconnected func(ids.ShortID)
}

func (h *testHandler) Connected(id ids.ShortID) {
	if h.connected != nil {
		h.connected(id)
	}
}
func (h *testHandler) Disconnected(id ids.ShortID) {
	if h.disconnected != nil {
		h.disconnected(id)
	}
}

func TestNewDefaultNetwork(t *testing.T) {
	log := logging.NoLog{}
	ip := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 0,
	}
	id := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip.String())))
	networkID := uint32(0)
	appVersion := version.NewDefaultVersion("app", 0, 1, 0)
	versionParser := version.NewDefaultParser()

	listener := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
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
		prometheus.NewRegistry(),
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

	ip0 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 0,
	}
	id0 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip0.String())))
	ip1 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 1,
	}
	id1 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip1.String())))

	listener0 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
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
		closed:  make(chan struct{}),
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

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	handler0 := &testHandler{
		connected: func(id ids.ShortID) {
			if !id.Equals(id0) {
				wg0.Done()
			}
		},
	}

	handler1 := &testHandler{
		connected: func(id ids.ShortID) {
			if !id.Equals(id1) {
				wg1.Done()
			}
		},
	}

	net0 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler0,
	)
	assert.NotNil(t, net0)

	net1 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler1,
	)
	assert.NotNil(t, net1)

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

	err := net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestDoubleTrack(t *testing.T) {
	log := logging.NoLog{}
	networkID := uint32(0)
	appVersion := version.NewDefaultVersion("app", 0, 1, 0)
	versionParser := version.NewDefaultParser()

	ip0 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 0,
	}
	id0 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip0.String())))
	ip1 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 1,
	}
	id1 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip1.String())))

	listener0 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
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
		closed:  make(chan struct{}),
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

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	handler0 := &testHandler{
		connected: func(id ids.ShortID) {
			if !id.Equals(id0) {
				wg0.Done()
			}
		},
	}

	handler1 := &testHandler{
		connected: func(id ids.ShortID) {
			if !id.Equals(id1) {
				wg1.Done()
			}
		},
	}

	net0 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler0,
	)
	assert.NotNil(t, net0)

	net1 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler1,
	)
	assert.NotNil(t, net1)

	net0.Track(ip1)
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

	err := net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestDoubleClose(t *testing.T) {
	log := logging.NoLog{}
	networkID := uint32(0)
	appVersion := version.NewDefaultVersion("app", 0, 1, 0)
	versionParser := version.NewDefaultParser()

	ip0 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 0,
	}
	id0 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip0.String())))
	ip1 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 1,
	}
	id1 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip1.String())))

	listener0 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
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
		closed:  make(chan struct{}),
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

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	handler0 := &testHandler{
		connected: func(id ids.ShortID) {
			if !id.Equals(id0) {
				wg0.Done()
			}
		},
	}

	handler1 := &testHandler{
		connected: func(id ids.ShortID) {
			if !id.Equals(id1) {
				wg1.Done()
			}
		},
	}

	net0 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler0,
	)
	assert.NotNil(t, net0)

	net1 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler1,
	)
	assert.NotNil(t, net1)

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

	err := net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestTrackConnected(t *testing.T) {
	log := logging.NoLog{}
	networkID := uint32(0)
	appVersion := version.NewDefaultVersion("app", 0, 1, 0)
	versionParser := version.NewDefaultParser()

	ip0 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 0,
	}
	id0 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip0.String())))
	ip1 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 1,
	}
	id1 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip1.String())))

	listener0 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
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
		closed:  make(chan struct{}),
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

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	handler0 := &testHandler{
		connected: func(id ids.ShortID) {
			if !id.Equals(id0) {
				wg0.Done()
			}
		},
	}

	handler1 := &testHandler{
		connected: func(id ids.ShortID) {
			if !id.Equals(id1) {
				wg1.Done()
			}
		},
	}

	net0 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler0,
	)
	assert.NotNil(t, net0)

	net1 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler1,
	)
	assert.NotNil(t, net1)

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

	net0.Track(ip1)

	err := net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestTrackConnectedRace(t *testing.T) {
	log := logging.NoLog{}
	networkID := uint32(0)
	appVersion := version.NewDefaultVersion("app", 0, 1, 0)
	versionParser := version.NewDefaultParser()

	ip0 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 0,
	}
	id0 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip0.String())))
	ip1 := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 1,
	}
	id1 := ids.NewShortID(hashing.ComputeHash160Array([]byte(ip1.String())))

	listener0 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
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
		closed:  make(chan struct{}),
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
		prometheus.NewRegistry(),
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
		vdrs,
		handler,
	)
	assert.NotNil(t, net0)

	net1 := NewDefaultNetwork(
		prometheus.NewRegistry(),
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
		vdrs,
		handler,
	)
	assert.NotNil(t, net1)

	net0.Track(ip1)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()

	err := net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}
