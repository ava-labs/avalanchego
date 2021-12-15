// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

const (
	defaultAliasTimeout               = 2 * time.Second
	defaultGossipPeerListFreq         = time.Minute
	defaultPeerListSize               = 50
	defaultGossipPeerListTo           = 100
	defaultGossipAcceptedFrontierSize = 35
	defaultGossipOnAcceptSize         = 20
	defaultAppGossipNonValidatorSize  = 2
	defaultAppGossipValidatorSize     = 4
)

var (
	errClosed  = errors.New("closed")
	errRefused = errors.New("connection refused")

	testSubnetID          = ids.GenerateTestID()
	defaulAppVersion      = version.NewDefaultApplication("app", 0, 1, 0)
	defaultVersionManager = version.NewCompatibility(
		defaulAppVersion,
		defaulAppVersion,
		time.Now(),
		defaulAppVersion,
		defaulAppVersion,
		time.Now(),
		defaulAppVersion,
	)
)

type testListener struct {
	addr    net.Addr
	inbound chan net.Conn
	once    sync.Once
	closed  chan struct{}
}

func getDefaultManager() validators.Manager {
	defaultValidators := validators.NewManager()
	err := defaultValidators.Set(constants.PrimaryNetworkID, validators.NewSet())
	if err != nil {
		panic(err)
	}
	return defaultValidators
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
	addr net.Addr

	outbounds     map[string]*testListener
	outboundsLock sync.Mutex

	// clients tracks opened client connections
	// by IP
	clients map[string]*testConn

	// closer is invoked when testDialer is closed
	closer func(net.Addr, net.Addr)
}

func (d *testDialer) Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error) {
	d.outboundsLock.Lock()
	defer d.outboundsLock.Unlock()

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
		closer:        d.closer,
	}
	client := &testConn{
		pendingReads:  server.pendingWrites,
		pendingWrites: server.pendingReads,
		closed:        make(chan struct{}),
		local:         d.addr,
		remote:        outbound.addr,
		closer:        d.closer,
	}

	select {
	case outbound.inbound <- server:
		if d.clients == nil {
			d.clients = make(map[string]*testConn)
		}
		d.clients[ip.String()] = client
		return client, nil
	default:
		return nil, errRefused
	}
}

func (d *testDialer) Update(ip utils.DynamicIPDesc, listener *testListener) {
	d.outboundsLock.Lock()
	defer d.outboundsLock.Unlock()

	d.outbounds[ip.String()] = listener
}

type testConn struct {
	partialRead   []byte
	pendingReads  chan []byte
	pendingWrites chan []byte
	closed        chan struct{}
	once          sync.Once

	// closer is invoked when testConn is closed
	closer func(net.Addr, net.Addr)

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
	c.once.Do(func() {
		close(c.closed)

		if c.closer != nil {
			c.closer(c.local, c.remote)
		}
	})
	return nil
}

func (c *testConn) LocalAddr() net.Addr              { return c.local }
func (c *testConn) RemoteAddr() net.Addr             { return c.remote }
func (c *testConn) SetDeadline(time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(time.Time) error { return nil }

type testHandler struct {
	router.Router
	ConnectedF    func(nodeID ids.ShortID, nodeVersion version.Application)
	DisconnectedF func(nodeID ids.ShortID)
	PutF          func(
		validatorID ids.ShortID,
		chainID ids.ID,
		requestID uint32,
		containerID ids.ID,
		container []byte,
		onFinishedHandling func(),
	)
	AppGossipF func(
		nodeID ids.ShortID,
		chainID ids.ID,
		appGossipBytes []byte,
		onFinishedHandling func(),
	)
}

func (h *testHandler) Connected(id ids.ShortID, nodeVersion version.Application) {
	if h.ConnectedF != nil {
		h.ConnectedF(id, nodeVersion)
	}
}

func (h *testHandler) Disconnected(id ids.ShortID) {
	if h.DisconnectedF != nil {
		h.DisconnectedF(id)
	}
}

func (h *testHandler) HandleInbound(msg message.InboundMessage) {
	switch msg.Op() {
	case message.Put:
		chainID, _ := ids.ToID(msg.Get(message.ChainID).([]byte))
		requestID, _ := msg.Get(message.RequestID).(uint32)
		containerID, _ := ids.ToID(msg.Get(message.ContainerID).([]byte))
		container, _ := msg.Get(message.ContainerBytes).([]byte)

		if h.PutF != nil {
			h.PutF(
				msg.NodeID(),
				chainID,
				requestID,
				containerID,
				container,
				msg.OnFinishedHandling,
			)
		}
	case message.AppGossip:
		chainID, _ := ids.ToID(msg.Get(message.ChainID).([]byte))
		appBytes := msg.Get(message.AppBytes).([]byte)

		if h.AppGossipF != nil {
			h.AppGossipF(msg.NodeID(), chainID, appBytes, msg.OnFinishedHandling)
		}
	default:
		return
	}
}

// testUpgrader is used to provide a custom
// id for a given IP
type testUpgrader struct {
	// ids is a mapping of IP addresses
	// to id
	ids     map[string]ids.ShortID
	certs   map[string]*x509.Certificate
	idsLock sync.Mutex
}

func (u *testUpgrader) Upgrade(conn net.Conn) (ids.ShortID, net.Conn, *x509.Certificate, error) {
	u.idsLock.Lock()
	defer u.idsLock.Unlock()
	addr := conn.RemoteAddr()
	str := addr.String()
	return u.ids[str], conn, u.certs[str], nil
}

func (u *testUpgrader) Update(ip utils.DynamicIPDesc, id ids.ShortID) {
	u.idsLock.Lock()
	defer u.idsLock.Unlock()

	u.ids[ip.String()] = id
}

var (
	certLock   sync.Mutex
	cert0      *tls.Certificate
	tlsConfig0 *tls.Config
	cert1      *tls.Certificate
	tlsConfig1 *tls.Config
	cert2      *tls.Certificate
	tlsConfig2 *tls.Config
)

func initCerts(t *testing.T) {
	certLock.Lock()
	defer certLock.Unlock()
	if cert0 != nil {
		return
	}
	var err error
	cert0, err = staking.NewTLSCert()
	assert.NoError(t, err)
	tlsConfig0 = TLSConfig(*cert0)
	cert1, err = staking.NewTLSCert()
	assert.NoError(t, err)
	tlsConfig1 = TLSConfig(*cert1)
	cert2, err = staking.NewTLSCert()
	assert.NoError(t, err)
	tlsConfig2 = TLSConfig(*cert2)
}

var (
	defaultInboundMsgThrottler  = throttling.NewNoInboundThrottler()
	defaultOutboundMsgThrottler = throttling.NewNoOutboundThrottler()
)

func TestNewDefaultNetwork(t *testing.T) {
	initCerts(t)

	ip := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id := ids.ShortID(hashing.ComputeHash160Array([]byte(ip.IP().String())))

	listener := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}

	vdrs := getDefaultManager()
	beacons := validators.NewSet()
	metrics := prometheus.NewRegistry()
	msgCreator, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler := &testHandler{}
	net, err := newDefaultNetwork(
		id,
		ip,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener,
		metrics,
		msgCreator,
		handler,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net)

	go func() {
		err := net.Close()
		assert.NoError(t, err)
	}()

	err = net.Dispatch()
	assert.Error(t, err)
}

func TestEstablishConnection(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id0 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip0.IP().String())))
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	id1 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip1.IP().String())))

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

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0

	vdrs := getDefaultManager()
	beacons := validators.NewSet()

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id0 {
				wg0.Done()
			}
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id1 {
				wg1.Done()
			}
		},
	}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()

	net0.Track(ip1.IP(), id1)

	wg0.Wait()
	wg1.Wait()

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestDoubleTrack(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id0 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip0.IP().String())))
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	id1 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip1.IP().String())))

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

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0

	vdrs := getDefaultManager()
	beacons := validators.NewSet()

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id0 {
				wg0.Done()
			}
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id1 {
				wg1.Done()
			}
		},
	}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	net0.Track(ip1.IP(), id1)
	net0.Track(ip1.IP(), id1)

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

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestDoubleClose(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id0 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip0.IP().String())))
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	id1 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip1.IP().String())))

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

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0

	vdrs := getDefaultManager()
	beacons := validators.NewSet()

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id0 {
				wg0.Done()
			}
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id1 {
				wg1.Done()
			}
		},
	}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	net0.Track(ip1.IP(), id1)

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

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestTrackConnected(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id0 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip0.IP().String())))
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	id1 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip1.IP().String())))

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

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0

	vdrs := getDefaultManager()
	beacons := validators.NewSet()

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id0 {
				wg0.Done()
			}
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id1 {
				wg1.Done()
			}
		},
	}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	net0.Track(ip1.IP(), id1)

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

	net0.Track(ip1.IP(), id1)

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestTrackConnectedRace(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id0 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip0.IP().String())))
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	id1 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip1.IP().String())))

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

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0

	vdrs := getDefaultManager()
	beacons := validators.NewSet()
	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)

	handler := &testHandler{}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	net0.Track(ip1.IP(), id1)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func assertEqualPeers(t *testing.T, expected map[string]ids.ShortID, actual []PeerInfo) {
	assert.Len(t, actual, len(expected))
	for _, p := range actual {
		match, ok := expected[p.IP]
		assert.True(t, ok, "peer with IP %s missing", p.IP)
		assert.Equal(t, match.PrefixedString(constants.NodeIDPrefix), p.ID)
	}
}

func TestPeerAliasesTicker(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id0 := certToID(cert0.Leaf)
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	id1 := certToID(cert1.Leaf)
	ip2 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		2,
	)
	id2 := certToID(cert2.Leaf)

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
	listener2 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}
	caller2 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		outbounds: make(map[string]*testListener),
	}
	listener3 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}
	caller3 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		outbounds: make(map[string]*testListener),
	}

	caller0.outbounds[ip1.IP().String()] = listener1
	caller0.outbounds[ip2.IP().String()] = listener2
	caller1.outbounds[ip0.IP().String()] = listener0
	caller2.outbounds[ip0.IP().String()] = listener0
	caller3.outbounds[ip0.IP().String()] = listener0

	upgrader := &testUpgrader{
		ids: map[string]ids.ShortID{
			ip0.IP().String(): id0,
			ip1.IP().String(): id1,
			ip2.IP().String(): id1,
		},
		certs: map[string]*x509.Certificate{
			ip0.IP().String(): cert0.Leaf,
			ip1.IP().String(): cert1.Leaf,
			ip2.IP().String(): cert2.Leaf,
		},
	}

	vdrs := getDefaultManager()
	beacons := validators.NewSet()

	var (
		wg0     sync.WaitGroup
		wg1     sync.WaitGroup
		wg1Done bool
		wg2     sync.WaitGroup

		cleanup bool
	)
	wg0.Add(2)
	wg1.Add(1)
	wg2.Add(2)

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id == id1 {
				wg0.Done()
				return
			}
			if id == id2 {
				wg2.Done()
				return
			}
			if cleanup {
				return
			}

			assert.Fail(t, "handler 0 unauthorized connection", id.String())
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id == id0 {
				wg0.Done()
				return
			}
			if cleanup {
				return
			}

			assert.Fail(t, "handler 1 unauthorized connection", id.String())
		},
	}

	metrics2 := prometheus.NewRegistry()
	msgCreator2, err := message.NewCreator(metrics2, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler2 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if cleanup {
				return
			}

			assert.Fail(t, "handler 2 unauthorized connection", id.String())
		},
	}

	metrics3 := prometheus.NewRegistry()
	msgCreator3, err := message.NewCreator(metrics3, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler3 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id == id0 {
				wg2.Done()
				return
			}
			if cleanup {
				return
			}

			assert.Fail(t, "handler 3 unauthorized connection", id.String())
		},
	}

	caller0.closer = func(local net.Addr, remote net.Addr) {
		if remote.String() == ip2.String() && !wg1Done {
			wg1.Done()
			return
		}
		if cleanup {
			return
		}

		assert.Fail(t, "caller 0 unauthorized close", local.String(), remote.String())
	}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)
	netw0 := net0.(*network)

	netw0.serverUpgrader = upgrader
	netw0.clientUpgrader = upgrader

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)
	netw1 := net1.(*network)

	netw1.serverUpgrader = upgrader
	netw1.clientUpgrader = upgrader

	net2, err := newTestNetwork(
		id2,
		ip2,
		defaultVersionManager,
		vdrs,
		beacons,
		cert2.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig2,
		listener2,
		caller2,
		metrics2,
		msgCreator2,
		handler2,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net2)

	netw2 := net2.(*network)

	netw2.serverUpgrader = upgrader
	netw2.clientUpgrader = upgrader

	net3, err := newTestNetwork(
		id2,
		ip2,
		defaultVersionManager,
		vdrs,
		beacons,
		cert2.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig2,
		listener3,
		caller3,
		metrics3,
		msgCreator3,
		handler3,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net3)

	netw3 := net3.(*network)

	netw3.serverUpgrader = upgrader
	netw3.clientUpgrader = upgrader

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net2.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net3.Dispatch()
		assert.Error(t, err)
	}()

	// Connect to peer with id1
	net0.Track(ip1.IP(), id1)

	// Confirm peers correct
	wg0.Wait()
	assertEqualPeers(t, map[string]ids.ShortID{
		ip1.String(): id1,
	}, net0.Peers(nil))
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net1.Peers(nil))
	assert.Len(t, net2.Peers(nil), 0)
	assert.Len(t, net3.Peers(nil), 0)

	// Attempt to connect to ip2 (same id as ip1)
	net0.Track(ip2.IP(), id2)

	// Confirm that ip2 was not added to net0 peers
	wg1.Wait()
	wg1Done = true
	assertEqualPeers(t, map[string]ids.ShortID{
		ip1.String(): id1,
	}, net0.Peers(nil))
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net1.Peers(nil))
	assert.Len(t, net2.Peers(nil), 0)
	assert.Len(t, net3.Peers(nil), 0)

	// Subsequent track call returns immediately with no connection attempts
	// (would cause fatal error from unauthorized connection if allowed)
	net0.Track(ip2.IP(), id2)

	// Wait for aliases to be removed by peer
	time.Sleep(3 * time.Second)

	// Track ip2 on net3
	upgrader.Update(ip2, id2)
	caller0.Update(ip2, listener3)
	net0.Track(ip2.IP(), id2)

	// Confirm that id2 was added as peer
	wg2.Wait()
	assertEqualPeers(t, map[string]ids.ShortID{
		ip1.String(): id1,
		ip2.String(): id2,
	}, net0.Peers(nil))
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net1.Peers(nil))
	assert.Len(t, net2.Peers(nil), 0)
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net3.Peers(nil))

	// Cleanup
	cleanup = true
	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)

	err = net2.Close()
	assert.NoError(t, err)

	err = net3.Close()
	assert.NoError(t, err)
}

func TestPeerAliasesDisconnect(t *testing.T) {
	initCerts(t)

	vdrs := getDefaultManager()
	beacons := validators.NewSet()

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id0 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip0.IP().String())))
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	id1 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip1.IP().String())))
	ip2 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		2,
	)
	id2 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip2.IP().String())))

	err := vdrs.AddWeight(constants.PrimaryNetworkID, id0, 1)
	if err != nil {
		t.Fatal(err)
	}

	err = vdrs.AddWeight(constants.PrimaryNetworkID, id1, 1)
	if err != nil {
		t.Fatal(err)
	}

	err = vdrs.AddWeight(constants.PrimaryNetworkID, id2, 1)
	if err != nil {
		t.Fatal(err)
	}

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
	listener2 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}
	caller2 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		outbounds: make(map[string]*testListener),
	}
	listener3 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}
	caller3 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		outbounds: make(map[string]*testListener),
	}

	caller0.outbounds[ip1.IP().String()] = listener1
	caller0.outbounds[ip2.IP().String()] = listener2
	caller1.outbounds[ip0.IP().String()] = listener0
	caller2.outbounds[ip0.IP().String()] = listener0
	caller3.outbounds[ip0.IP().String()] = listener0

	upgrader := &testUpgrader{
		ids: map[string]ids.ShortID{
			ip0.IP().String(): id0,
			ip1.IP().String(): id1,
			ip2.IP().String(): id1,
		},
		certs: map[string]*x509.Certificate{
			ip0.IP().String(): cert0.Leaf,
			ip1.IP().String(): cert1.Leaf,
			ip2.IP().String(): cert2.Leaf,
		},
	}

	var (
		wg0     sync.WaitGroup
		wg1     sync.WaitGroup
		wg1Done bool
		wg2     sync.WaitGroup
		wg2Done bool
		wg3     sync.WaitGroup

		cleanup bool
	)
	wg0.Add(2)
	wg1.Add(1)
	wg2.Add(3)
	wg3.Add(2)

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id == id1 {
				wg0.Done()
				return
			}
			if id == id2 {
				wg3.Done()
				return
			}
			if cleanup {
				return
			}

			assert.Fail(t, "handler 0 unauthorized connection", id.String())
		},
		DisconnectedF: func(id ids.ShortID) {
			if id == id1 {
				wg2.Done()
				return
			}
			if cleanup {
				return
			}

			assert.Fail(t, "handler 0 unauthorized disconnect", id.String())
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id == id0 {
				wg0.Done()
				return
			}
			if cleanup {
				return
			}

			assert.Fail(t, "handler 1 unauthorized connection", id.String())
		},
	}

	metrics2 := prometheus.NewRegistry()
	msgCreator2, err := message.NewCreator(metrics2, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler2 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if cleanup {
				return
			}

			assert.Fail(t, "handler 2 unauthorized connection", id.String())
		},
	}

	metrics3 := prometheus.NewRegistry()
	msgCreator3, err := message.NewCreator(metrics3, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler3 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id == id0 {
				wg3.Done()
				return
			}
			if cleanup {
				return
			}

			assert.Fail(t, "handler 3 unauthorized connection", id.String())
		},
	}

	caller0.closer = func(local net.Addr, remote net.Addr) {
		if remote.String() == ip2.String() && !wg1Done {
			wg1.Done()
			return
		}
		if remote.String() == ip1.String() && !wg2Done {
			wg2.Done()
			return
		}
		if local.String() == ip1.String() && !wg2Done {
			wg2.Done()
			return
		}
		if cleanup {
			return
		}

		assert.Fail(t, "caller 0 unauthorized close", local.String(), remote.String())
	}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	netw0 := net0.(*network)
	netw0.serverUpgrader = upgrader
	netw0.clientUpgrader = upgrader

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	netw1 := net1.(*network)
	netw1.serverUpgrader = upgrader
	netw1.clientUpgrader = upgrader

	net2, err := newTestNetwork(
		id1,
		ip2,
		defaultVersionManager,
		vdrs,
		beacons,
		cert2.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig2,
		listener2,
		caller2,
		metrics2,
		msgCreator2,
		handler2,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net2)

	netw2 := net2.(*network)
	netw2.serverUpgrader = upgrader
	netw2.clientUpgrader = upgrader

	net3, err := newTestNetwork(
		id2,
		ip2,
		defaultVersionManager,
		vdrs,
		beacons,
		cert2.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig2,
		listener3,
		caller3,
		metrics3,
		msgCreator3,
		handler3,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net3)

	netw3 := net3.(*network)
	netw3.serverUpgrader = upgrader
	netw3.clientUpgrader = upgrader

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net2.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net3.Dispatch()
		assert.Error(t, err)
	}()

	// Connect to peer with id1
	net0.Track(ip1.IP(), id1)

	// Confirm peers correct
	wg0.Wait()
	assertEqualPeers(t, map[string]ids.ShortID{
		ip1.String(): id1,
	}, net0.Peers(nil))
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net1.Peers(nil))
	assert.Len(t, net2.Peers(nil), 0)
	assert.Len(t, net3.Peers(nil), 0)

	// Attempt to connect to ip2 (same id as ip1)
	net0.Track(ip2.IP(), id2)

	// Confirm that ip2 was not added to net0 peers
	wg1.Wait()
	wg1Done = true
	assertEqualPeers(t, map[string]ids.ShortID{
		ip1.String(): id1,
	}, net0.Peers(nil))
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net1.Peers(nil))
	assert.Len(t, net2.Peers(nil), 0)
	assert.Len(t, net3.Peers(nil), 0)

	// Disconnect original peer
	_ = caller0.clients[ip1.String()].Close()

	// Track ip2 on net3
	wg2.Wait()
	wg2Done = true
	assertEqualPeers(t, map[string]ids.ShortID{}, net0.Peers(nil))
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net1.Peers(nil))
	assert.Len(t, net2.Peers(nil), 0)
	assert.Len(t, net3.Peers(nil), 0)
	upgrader.Update(ip2, id2)
	caller0.Update(ip2, listener3)
	net0.Track(ip2.IP(), id2)

	// Confirm that id2 was added as peer
	wg3.Wait()
	assertEqualPeers(t, map[string]ids.ShortID{
		ip2.String(): id2,
	}, net0.Peers(nil))
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net1.Peers(nil))
	assert.Len(t, net2.Peers(nil), 0)
	assertEqualPeers(t, map[string]ids.ShortID{
		ip0.String(): id0,
	}, net3.Peers(nil))

	// Cleanup
	cleanup = true
	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)

	err = net2.Close()
	assert.NoError(t, err)

	err = net3.Close()
	assert.NoError(t, err)
}

func TestPeerSignature(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	ip2 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		2,
	)

	id0 := certToID(cert0.Leaf)
	id1 := certToID(cert1.Leaf)
	id2 := certToID(cert2.Leaf)

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
	listener2 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}
	caller2 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		outbounds: make(map[string]*testListener),
	}

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0
	caller0.outbounds[ip2.IP().String()] = listener2
	caller1.outbounds[ip2.IP().String()] = listener2

	vdrs := getDefaultManager()
	beacons := validators.NewSet()
	// id2 is a validator
	err := vdrs.AddWeight(constants.PrimaryNetworkID, id2, math.MaxUint64)
	assert.NoError(t, err)

	allPeers := ids.ShortSet{}
	allPeers.Add(id0, id1, id2)

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
		wg2 sync.WaitGroup
	)
	wg0.Add(2)
	wg1.Add(2)
	wg2.Add(2)

	handledLock := sync.RWMutex{}
	handled := make(map[string]struct{})

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id0 {
				handledLock.Lock()
				handled[id0.String()+":"+id.String()] = struct{}{}
				handledLock.Unlock()
				wg0.Done()
			}
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id1 {
				handledLock.Lock()
				handled[id1.String()+":"+id.String()] = struct{}{}
				handledLock.Unlock()
				wg1.Done()
			}
		},
	}

	metrics2 := prometheus.NewRegistry()
	msgCreator2, err := message.NewCreator(metrics2, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler2 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			if id != id2 {
				handledLock.Lock()
				handled[id2.String()+":"+id.String()] = struct{}{}
				handledLock.Unlock()
				wg2.Done()
			}
		},
	}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	net2, err := newTestNetwork(
		id2,
		ip2,
		defaultVersionManager,
		vdrs,
		beacons,
		cert2.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig2,
		listener2,
		caller2,
		metrics2,
		msgCreator2,
		handler2,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net2)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net2.Dispatch()
		assert.Error(t, err)
	}()

	// ip0 -> ip2 and ip0 -> ip1 connect
	net0.Track(ip2.IP(), id2)
	net0.Track(ip1.IP(), id1)

	// now we force send peer message from ip0
	// this should cause a peer ip1 -> ip2 (ip2 is a validator)
	for {
		handledLock.RLock()
		lenhan := len(handled)
		handledLock.RUnlock()
		if lenhan == 6 {
			break
		}
		peers := net0.(*network).getPeerElements(allPeers)
		for _, p := range peers {
			if p.peer == nil {
				continue
			}
			p.peer.sendPeerList()
		}
		time.Sleep(500 * time.Millisecond)
	}
	wg0.Wait()
	wg1.Wait()
	wg2.Wait()

	_, ok := handled[id0.String()+":"+id1.String()]
	assert.True(t, ok)
	_, ok = handled[id0.String()+":"+id2.String()]
	assert.True(t, ok)
	_, ok = handled[id1.String()+":"+id0.String()]
	assert.True(t, ok)
	_, ok = handled[id1.String()+":"+id2.String()]
	assert.True(t, ok)
	_, ok = handled[id2.String()+":"+id0.String()]
	assert.True(t, ok)
	_, ok = handled[id2.String()+":"+id1.String()]
	assert.True(t, ok)

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)

	err = net2.Close()
	assert.NoError(t, err)
}

func TestValidatorIPs(t *testing.T) {
	netConfig := newDefaultConfig()
	dummyNetwork := network{config: &netConfig}
	dummyNetwork.config.PeerListSize = 50

	appVersion := version.NewDefaultApplication("app", 1, 1, 0)
	versionManager := version.NewCompatibility(
		appVersion,
		appVersion,
		time.Now(),
		appVersion,
		appVersion,
		time.Now(),
		appVersion,
	)
	dummyNetwork.versionCompatibility = versionManager

	// SCENARIO: Connected validator peers with right version and cert are picked
	// context
	clearPeersData(&dummyNetwork)
	firstValidatorIPDesc := utils.IPDesc{
		IP:   net.IPv4(172, 17, 0, 1),
		Port: 1,
	}
	firstValidatorPeer := createPeer(ids.ShortID{0x01}, firstValidatorIPDesc, appVersion)
	addPeerToNetwork(&dummyNetwork, firstValidatorPeer, true)

	secondValidatorIPDesc := utils.IPDesc{
		IP:   net.IPv4(172, 17, 0, 2),
		Port: 2,
	}
	secondValidatorPeer := createPeer(ids.ShortID{0x02}, secondValidatorIPDesc, appVersion)
	addPeerToNetwork(&dummyNetwork, secondValidatorPeer, true)

	thirdValidatorIPDesc := utils.IPDesc{
		IP:   net.IPv4(172, 17, 0, 3),
		Port: 3,
	}
	thirdValidatorPeer := createPeer(ids.ShortID{0x03}, thirdValidatorIPDesc, appVersion)
	addPeerToNetwork(&dummyNetwork, thirdValidatorPeer, true)

	assert.True(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, firstValidatorPeer.nodeID))
	assert.True(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, secondValidatorPeer.nodeID))
	assert.True(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, thirdValidatorPeer.nodeID))

	// test
	validatorIPs, err := dummyNetwork.validatorIPs()

	// checks
	assert.NoError(t, err)
	assert.True(t, len(validatorIPs) == 3)
	assert.True(t, isIPDescIn(firstValidatorPeer.getIP(), validatorIPs))
	assert.True(t, isIPDescIn(secondValidatorPeer.getIP(), validatorIPs))
	assert.True(t, isIPDescIn(thirdValidatorPeer.getIP(), validatorIPs))

	// SCENARIO: no peers case is handled
	// context
	clearPeersData(&dummyNetwork)

	// test
	validatorIPs, err = dummyNetwork.validatorIPs()

	// checks
	assert.NoError(t, err)
	assert.True(t, len(validatorIPs) == 0)

	// SCENARIO: validators not connected are not picked
	// context
	clearPeersData(&dummyNetwork)
	disconnectedValidatorIPDesc := utils.IPDesc{
		IP:   net.IPv4(172, 17, 0, 4),
		Port: 4,
	}
	disconnectedValidatorPeer := createPeer(ids.ShortID{0x01}, disconnectedValidatorIPDesc, appVersion)
	disconnectedValidatorPeer.finishedHandshake.SetValue(false)
	addPeerToNetwork(&dummyNetwork, disconnectedValidatorPeer, true)
	assert.True(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, disconnectedValidatorPeer.nodeID))

	// test
	validatorIPs, err = dummyNetwork.validatorIPs()

	// checks
	assert.NoError(t, err)
	assert.True(t, len(validatorIPs) == 0)

	// SCENARIO: validators with zeroed IP are not picked
	// context
	clearPeersData(&dummyNetwork)
	zeroIPValidatorIPDesc := utils.IPDesc{
		IP:   net.IPv4zero,
		Port: 1,
	}
	zeroValidatorPeer := createPeer(ids.ShortID{0x01}, zeroIPValidatorIPDesc, appVersion)
	addPeerToNetwork(&dummyNetwork, zeroValidatorPeer, true)
	assert.True(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, zeroValidatorPeer.nodeID))

	// test
	validatorIPs, err = dummyNetwork.validatorIPs()

	// checks
	assert.NoError(t, err)
	assert.True(t, len(validatorIPs) == 0)

	// SCENARIO: Non-validator peer not selected
	// context
	clearPeersData(&dummyNetwork)
	nonValidatorIPDesc := utils.IPDesc{
		IP:   net.IPv4(172, 17, 0, 5),
		Port: 5,
	}

	nonValidatorPeer := createPeer(ids.ShortID{0x04}, nonValidatorIPDesc, appVersion)
	addPeerToNetwork(&dummyNetwork, nonValidatorPeer, false)
	assert.False(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, nonValidatorPeer.nodeID))

	// test
	validatorIPs, err = dummyNetwork.validatorIPs()

	// checks
	assert.NoError(t, err)
	assert.True(t, len(validatorIPs) == 0)

	// SCENARIO: validators with wrong version are not picked
	// context
	clearPeersData(&dummyNetwork)
	maskedVersion := version.NewDefaultApplication("app", 0, 1, 0)

	maskedValidatorIPDesc := utils.IPDesc{
		IP:   net.IPv4(172, 17, 0, 6),
		Port: 6,
	}
	maskedValidatorPeer := createPeer(ids.ShortID{0x01}, maskedValidatorIPDesc, maskedVersion)
	addPeerToNetwork(&dummyNetwork, maskedValidatorPeer, true)
	assert.True(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, maskedValidatorPeer.nodeID))

	// test
	validatorIPs, err = dummyNetwork.validatorIPs()

	// checks
	assert.NoError(t, err)
	assert.True(t, len(validatorIPs) == 0)

	// SCENARIO: validators with wrong certificate are not picked
	// context
	clearPeersData(&dummyNetwork)
	wrongCertValidatorIPDesc := utils.IPDesc{
		IP:   net.IPv4(172, 17, 0, 7),
		Port: 7,
	}
	ipOnCert := utils.IPDesc{
		IP:   net.IPv4(172, 17, 0, 8),
		Port: 8,
	}
	wrongCertValidatorPeer := createPeer(ids.ShortID{0x01}, wrongCertValidatorIPDesc, appVersion)
	wrongCertValidatorPeer.sigAndTime.SetValue(signedPeerIP{
		ip:   ipOnCert,
		time: uint64(0),
	})
	addPeerToNetwork(&dummyNetwork, wrongCertValidatorPeer, true)
	assert.True(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, wrongCertValidatorPeer.nodeID))

	// test
	validatorIPs, err = dummyNetwork.validatorIPs()

	// checks
	assert.NoError(t, err)
	assert.True(t, len(validatorIPs) == 0)

	// SCENARIO: At most peerListSize validators are picked
	// context
	clearPeersData(&dummyNetwork)
	dummyNetwork.config.PeerListSize = 2

	validPeerCount := dummyNetwork.config.PeerListSize * 2
	for i := 0; i < int(validPeerCount); i++ {
		ipDesc := utils.IPDesc{
			IP:   net.IPv4(172, 17, 0, byte(i)),
			Port: uint16(i),
		}
		peer := createPeer(ids.ShortID{byte(i)}, ipDesc, appVersion)
		addPeerToNetwork(&dummyNetwork, peer, true)
		assert.True(t, dummyNetwork.config.Validators.Contains(constants.PrimaryNetworkID, peer.nodeID))
	}

	// test
	IPs, err := dummyNetwork.validatorIPs()

	// checks
	assert.NoError(t, err)
	assert.True(t, len(IPs) == int(dummyNetwork.config.PeerListSize))
}

// Test that a node will not finish the handshake if the peer's version
// is incompatible
func TestDontFinishHandshakeOnIncompatibleVersion(t *testing.T) {
	initCerts(t)

	// Node 0 considers node 1  incompatible
	net0Version := version.NewDefaultApplication("app", 1, 4, 7)
	net0MinCompatibleVersion := version.NewDefaultApplication("app", 1, 4, 5)
	// Node 1 considers node 0 compatible
	net1Version := version.NewDefaultApplication("app", 1, 4, 4)
	net1MinCompatibleVersion := version.NewDefaultApplication("app", 1, 4, 4)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)

	id0 := certToID(cert0.Leaf)
	id1 := certToID(cert1.Leaf)

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
		closer:    func(net.Addr, net.Addr) { listener0.Close() },
	}

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0

	vdrs := getDefaultManager()
	beacons := validators.NewSet()
	assert.NoError(t, vdrs.AddWeight(constants.PrimaryNetworkID, id1, 1))
	assert.NoError(t, vdrs.AddWeight(constants.PrimaryNetworkID, id0, 1))

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	net0Compatibility := version.NewCompatibility(
		net0Version,
		net0MinCompatibleVersion,
		time.Now(),
		net0MinCompatibleVersion,
		net0MinCompatibleVersion,
		time.Now(),
		net0MinCompatibleVersion,
	)

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	net1Compatibility := version.NewCompatibility(
		net1Version,
		net1MinCompatibleVersion,
		time.Now(),
		net1MinCompatibleVersion,
		net1MinCompatibleVersion,
		time.Now(),
		net1MinCompatibleVersion,
	)

	net0, err := newTestNetwork(
		id0,
		ip0,
		net0Compatibility,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		&testHandler{},
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		net1Compatibility,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		&testHandler{},
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()

	// net1 connects to net0
	// they start the handshake and exchange versions
	// net1 sees net0 as incompatible and closes the connection
	net1.Track(ip0.IP(), id0)

	select {
	case <-time.After(5 * time.Second):
		t.Error("should have closed immediately because net1 sees net0 as incompatible")
	case <-listener0.closed:
	}

	// Cleanup
	err = net0.Close()
	assert.NoError(t, err)
	err = net1.Close()
	assert.NoError(t, err)
}

func TestPeerTrackedSubnets(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id0 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip0.IP().String())))
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	id1 := ids.ShortID(hashing.ComputeHash160Array([]byte(ip1.IP().String())))

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

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0

	vdrs := getDefaultManager()
	beacons := validators.NewSet()

	var (
		wg0 sync.WaitGroup
		wg1 sync.WaitGroup
	)
	wg0.Add(1)
	wg1.Add(1)

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			assert.NotEqual(t, id0, id)
			wg0.Done()
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			assert.NotEqual(t, id1, id)
			wg1.Done()
		},
	}

	subnetSet := ids.Set{}
	subnetSet.Add(testSubnetID)
	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		subnetSet,
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		subnetSet,
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()

	net0.Track(ip1.IP(), id1)

	wg0.Wait()
	wg1.Wait()
	peers := net0.(*network).peers
	count := 0
	for _, peer := range peers.peersList {
		if peer == nil {
			continue
		}
		count++
		assert.True(t, peer.gotVersion.GetValue())
		assert.True(t, peer.trackedSubnets.Contains(testSubnetID))
		assert.True(t, peer.trackedSubnets.Contains(constants.PrimaryNetworkID))
	}

	assert.Greater(t, count, 0)

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)
}

func TestPeerGossip(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	ip2 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		2,
	)

	id0 := certToID(cert0.Leaf)
	id1 := certToID(cert1.Leaf)
	id2 := certToID(cert2.Leaf)

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
	listener2 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}
	caller2 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		outbounds: make(map[string]*testListener),
	}

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0
	caller0.outbounds[ip2.IP().String()] = listener2
	caller1.outbounds[ip2.IP().String()] = listener2

	vdrs := getDefaultManager()
	beacons := validators.NewSet()
	// id2 is a validator
	err := vdrs.AddWeight(constants.PrimaryNetworkID, id2, math.MaxUint64)
	assert.NoError(t, err)

	allPeers := ids.ShortSet{}
	allPeers.Add(id0, id1, id2)

	var (
		wg0  sync.WaitGroup
		wg1  sync.WaitGroup
		wg1P sync.WaitGroup
		wg2  sync.WaitGroup
		wg2P sync.WaitGroup
	)
	wg0.Add(2)
	wg1.Add(1)
	wg1P.Add(2)
	wg2.Add(1)
	wg2P.Add(1)

	testSubnetContainerID := ids.GenerateTestID()
	testPrimaryContainerID := ids.GenerateTestID()
	allContainerIDs := []ids.ID{testSubnetContainerID, testPrimaryContainerID}

	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			assert.NotEqual(t, id0, id)
			wg0.Done()
		},
		PutF: func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte, onFinishedHandling func()) {
			assert.Fail(t, "this should not receive any gossip")
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			assert.NotEqual(t, id1, id)
			wg1.Done()
		},
		PutF: func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte, onFinishedHandling func()) {
			assert.Contains(t, allContainerIDs, containerID)
			wg1P.Done()
		},
	}

	metrics2 := prometheus.NewRegistry()
	msgCreator2, err := message.NewCreator(metrics2, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler2 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			assert.NotEqual(t, id2, id)
			wg2.Done()
		},
		PutF: func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte, onFinishedHandling func()) {
			// this one should not receive it
			assert.NotEqual(t, testSubnetContainerID, containerID)
			wg2P.Done()
		},
	}

	subnetSet := ids.Set{}
	subnetSet.Add(testSubnetID)

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		subnetSet,
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		subnetSet,
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	net2, err := newTestNetwork(
		id2,
		ip2,
		defaultVersionManager,
		vdrs,
		beacons,
		cert2.PrivateKey.(crypto.Signer),
		ids.Set{}, // tracks no subnet
		tlsConfig2,
		listener2,
		caller2,
		metrics2,
		msgCreator2,
		handler2,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net2)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net2.Dispatch()
		assert.Error(t, err)
	}()

	// ip0 -> ip2 and ip0 -> ip1 connect
	net0.Track(ip2.IP(), id2)
	net0.Track(ip1.IP(), id1)

	wg0.Wait()
	wg1.Wait()
	wg2.Wait()

	gossipMsg0, err := msgCreator0.Put(ids.GenerateTestID(), constants.GossipMsgRequestID, testSubnetContainerID, []byte("test0"))
	assert.NoError(t, err)
	net0.Gossip(gossipMsg0, testSubnetID, false, 0, int(net0.(*network).config.GossipAcceptedFrontierSize))

	gossipMsg1, err := msgCreator0.Put(ids.GenerateTestID(), constants.GossipMsgRequestID, testPrimaryContainerID, []byte("test1"))
	assert.NoError(t, err)
	net0.Gossip(gossipMsg1, constants.PrimaryNetworkID, false, 0, int(net0.(*network).config.GossipAcceptedFrontierSize))

	wg1P.Wait()
	wg2P.Wait()

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)

	err = net2.Close()
	assert.NoError(t, err)
}

func TestAppGossip(t *testing.T) {
	initCerts(t)

	ip0 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	ip2 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		2,
	)

	id0 := certToID(cert0.Leaf)
	id1 := certToID(cert1.Leaf)
	id2 := certToID(cert2.Leaf)

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
	listener2 := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}
	caller2 := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
		outbounds: make(map[string]*testListener),
	}

	caller0.outbounds[ip1.IP().String()] = listener1
	caller1.outbounds[ip0.IP().String()] = listener0
	caller0.outbounds[ip2.IP().String()] = listener2
	caller1.outbounds[ip2.IP().String()] = listener2

	vdrs := getDefaultManager()
	primaryVdrs := validators.NewSet()
	_ = primaryVdrs.Set([]validators.Validator{validators.NewValidator(id2, math.MaxUint64)})
	// id2 is a validator
	_ = vdrs.Set(constants.PrimaryNetworkID, primaryVdrs)

	beacons := validators.NewSet()

	allPeers := ids.ShortSet{}
	allPeers.Add(id0, id1, id2)

	var (
		wg0  sync.WaitGroup
		wg1  sync.WaitGroup
		wg1P sync.WaitGroup
		wg2  sync.WaitGroup
		wg2P sync.WaitGroup
	)
	wg0.Add(2)
	wg1.Add(1)
	wg1P.Add(1)
	wg2.Add(1)
	wg2P.Add(2)

	testAppGossipBytes := []byte("appgossip")
	testAppGossipSpecificBytes := []byte("appgossipspecific")
	metrics0 := prometheus.NewRegistry()
	msgCreator0, err := message.NewCreator(metrics0, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler0 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			assert.NotEqual(t, id0, id)
			wg0.Done()
		},
		AppGossipF: func(
			nodeID ids.ShortID,
			chainID ids.ID,
			appGossipBytes []byte,
			onFinishedHandling func(),
		) {
			assert.Fail(t, "this should not receive any App Gossips")
		},
	}

	metrics1 := prometheus.NewRegistry()
	msgCreator1, err := message.NewCreator(metrics1, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler1 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			assert.NotEqual(t, id1, id)
			wg1.Done()
		},
		AppGossipF: func(
			nodeID ids.ShortID,
			chainID ids.ID,
			appGossipBytes []byte,
			onFinishedHandling func(),
		) {
			assert.Equal(t, testAppGossipBytes, appGossipBytes)
			wg1P.Done()
		},
	}

	metrics2 := prometheus.NewRegistry()
	msgCreator2, err := message.NewCreator(metrics2, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	handler2 := &testHandler{
		ConnectedF: func(id ids.ShortID, nodeVersion version.Application) {
			assert.NotEqual(t, id2, id)
			wg2.Done()
		},
		AppGossipF: func(
			nodeID ids.ShortID,
			chainID ids.ID,
			appGossipBytes []byte,
			onFinishedHandling func(),
		) {
			assert.Contains(t, [][]byte{testAppGossipBytes, testAppGossipSpecificBytes}, appGossipBytes)
			wg2P.Done()
		},
	}

	net0, err := newTestNetwork(
		id0,
		ip0,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener0,
		caller0,
		metrics0,
		msgCreator0,
		handler0,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net0)

	net1, err := newTestNetwork(
		id1,
		ip1,
		defaultVersionManager,
		vdrs,
		beacons,
		cert1.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig1,
		listener1,
		caller1,
		metrics1,
		msgCreator1,
		handler1,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net1)

	net2, err := newTestNetwork(
		id2,
		ip2,
		defaultVersionManager,
		vdrs,
		beacons,
		cert2.PrivateKey.(crypto.Signer),
		ids.Set{}, // tracks no subnet
		tlsConfig2,
		listener2,
		caller2,
		metrics2,
		msgCreator2,
		handler2,
	)
	assert.NoError(t, err)
	assert.NotNil(t, net2)

	go func() {
		err := net0.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net1.Dispatch()
		assert.Error(t, err)
	}()
	go func() {
		err := net2.Dispatch()
		assert.Error(t, err)
	}()

	// ip0 -> ip2 and ip0 -> ip1 connect
	net0.Track(ip2.IP(), id2)
	net0.Track(ip1.IP(), id1)

	wg0.Wait()
	wg1.Wait()
	wg2.Wait()

	chainID := ids.GenerateTestID()
	msg1, err := msgCreator0.AppGossip(chainID, testAppGossipBytes)
	assert.NoError(t, err)
	net0.Gossip(msg1, constants.PrimaryNetworkID, false, int(net0.(*network).config.AppGossipValidatorSize), int(net0.(*network).config.AppGossipNonValidatorSize))

	specificNodeSet := ids.NewShortSet(1)
	specificNodeSet.Add(id2)
	msg2, err := msgCreator0.AppGossip(chainID, testAppGossipSpecificBytes)
	assert.NoError(t, err)
	net0.Send(msg2, specificNodeSet, constants.PrimaryNetworkID, false)

	wg1P.Wait()
	wg2P.Wait()

	err = net0.Close()
	assert.NoError(t, err)

	err = net1.Close()
	assert.NoError(t, err)

	err = net2.Close()
	assert.NoError(t, err)
}

// Helper method for TestValidatorIPs
func createPeer(peerID ids.ShortID, peerIPDesc utils.IPDesc, peerVersion version.Application) *peer {
	newPeer := peer{
		ip:     peerIPDesc,
		nodeID: peerID,
	}
	newPeer.finishedHandshake.SetValue(true)
	newPeer.versionStruct.SetValue(peerVersion)
	newPeer.sigAndTime.SetValue(signedPeerIP{
		ip:   newPeer.ip,
		time: uint64(0),
	})

	return &newPeer
}

func addPeerToNetwork(targetNetwork *network, peerToAdd *peer, isValidator bool) {
	targetNetwork.peers.add(peerToAdd)

	if isValidator {
		_ = targetNetwork.config.Validators.AddWeight(constants.PrimaryNetworkID, peerToAdd.nodeID, 10)
	}
}

func clearPeersData(targetNetwork *network) {
	targetNetwork.peers.reset()
	targetNetwork.config.Validators = getDefaultManager()
}

func isIPDescIn(targetIP utils.IPDesc, ipDescList []utils.IPCertDesc) bool {
	for _, b := range ipDescList {
		if b.IPDesc.Equal(targetIP) {
			return true
		}
	}
	return false
}

func newDefaultNetwork(
	id ids.ShortID,
	ip utils.DynamicIPDesc,
	vdrs validators.Manager,
	beacons validators.Set,
	tlsKey crypto.Signer,
	subnetSet ids.Set,
	tlsConfig *tls.Config,
	listener net.Listener,
	metrics *prometheus.Registry,
	msgCreator message.Creator,
	router router.Router,
) (Network, error) {
	log := logging.NoLog{}
	networkID := uint32(0)
	benchlistManager := benchlist.NewManager(&benchlist.Config{})
	s := uptime.NewTestState()

	uptimeManager := uptime.NewManager(s)

	netConfig := newDefaultConfig()
	netConfig.Namespace = ""
	netConfig.MyNodeID = id
	netConfig.MyIP = ip
	netConfig.NetworkID = networkID
	netConfig.Validators = vdrs
	netConfig.Beacons = beacons
	netConfig.TLSKey = tlsKey
	netConfig.TLSConfig = tlsConfig
	netConfig.PeerAliasTimeout = defaultAliasTimeout
	netConfig.PeerListSize = defaultPeerListSize
	netConfig.PeerListGossipSize = defaultGossipPeerListTo
	netConfig.PeerListGossipFreq = defaultGossipPeerListFreq
	netConfig.GossipAcceptedFrontierSize = defaultGossipAcceptedFrontierSize
	netConfig.GossipOnAcceptSize = defaultGossipOnAcceptSize
	netConfig.CompressionEnabled = true
	netConfig.WhitelistedSubnets = subnetSet
	netConfig.UptimeCalculator = uptimeManager

	n, err := NewNetwork(&netConfig, msgCreator, metrics, log, listener, router, benchlistManager)
	if err != nil {
		return nil, err
	}

	return n, nil
}

func newTestNetwork(id ids.ShortID,
	ip utils.DynamicIPDesc,
	versionCompatibility version.Compatibility,
	vdrs validators.Manager,
	beacons validators.Set,
	tlsKey crypto.Signer,
	subnetSet ids.Set,
	tlsConfig *tls.Config,
	listener net.Listener,
	dialer dialer.Dialer,
	metrics *prometheus.Registry,
	msgCreator message.Creator,
	router router.Router) (Network, error) {
	n, err := newDefaultNetwork(id, ip, vdrs, beacons, tlsKey, subnetSet, tlsConfig, listener, metrics, msgCreator, router)
	if err != nil {
		return nil, err
	}
	netw := n.(*network)
	netw.dialer = dialer
	netw.versionCompatibility = versionCompatibility
	netw.inboundMsgThrottler = defaultInboundMsgThrottler
	netw.outboundMsgThrottler = defaultOutboundMsgThrottler
	return netw, nil
}

func newDefaultConfig() Config {
	return Config{
		PeerListGossipConfig: PeerListGossipConfig{
			PeerListStakerGossipFraction: 2,
		},
		DelayConfig: DelayConfig{
			MaxReconnectDelay:     time.Hour,
			InitialReconnectDelay: time.Second,
		},
		TimeoutConfig: TimeoutConfig{
			GetVersionTimeout:    10 * time.Second,
			PingPongTimeout:      30 * time.Second,
			ReadHandshakeTimeout: 15 * time.Second,
		},
		GossipConfig: GossipConfig{
			AppGossipNonValidatorSize: defaultAppGossipNonValidatorSize,
			AppGossipValidatorSize:    defaultAppGossipValidatorSize,
		},
		MaxClockDifference: time.Minute,
		AllowPrivateIPs:    true,
		PingFrequency:      constants.DefaultPingFrequency,
		UptimeMetricFreq:   30 * time.Second,
	}
}
