// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/sender"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/random"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/version"
)

const (
	defaultInitialReconnectDelay               = time.Second
	defaultMaxReconnectDelay                   = time.Hour
	defaultMaxMessageSize               uint32 = 1 << 21
	defaultSendQueueSize                       = 1 << 10
	defaultMaxClockDifference                  = time.Minute
	defaultPeerListGossipSpacing               = time.Minute
	defaultPeerListGossipSize                  = 100
	defaultPeerListStakerGossipFraction        = 2
	defaultGetVersionTimeout                   = 2 * time.Second
	defaultAllowPrivateIPs                     = true
	defaultGossipSize                          = 50
)

// Network ...
type Network interface {
	sender.ExternalSender

	// Returns non-nil error
	Dispatch() error

	// Must be thread safe. Will never stop attempting to connect to this IP
	Track(ip utils.IPDesc)

	// Calls Connected on all the currently connected peers
	RegisterHandler(h Handler)

	IPs() []utils.IPDesc

	// Must be thread safe
	Close() error
}

type network struct {
	log            logging.Logger
	id             ids.ShortID
	ip             utils.IPDesc
	networkID      uint32
	version        version.Version
	parser         version.Parser
	listener       net.Listener
	dialer         Dialer
	serverUpgrader Upgrader
	clientUpgrader Upgrader
	vdrs           validators.Set // set of current validators in the AVAnet
	router         router.Router

	clock timer.Clock

	initialReconnectDelay        time.Duration
	maxReconnectDelay            time.Duration
	maxMessageSize               uint32
	sendQueueSize                int
	maxClockDifference           time.Duration
	peerListGossipSpacing        time.Duration
	peerListGossipSize           int
	peerListStakerGossipFraction int
	getVersionTimeout            time.Duration
	allowPrivateIPs              bool
	gossipSize                   int

	executor timer.Executor

	b Builder

	stateLock       sync.Mutex
	closed          bool
	disconnectedIPs map[string]struct{}
	connectedIPs    map[string]struct{}
	peers           map[[20]byte]*peer
	handlers        []Handler
}

// NewDefaultNetwork ...
func NewDefaultNetwork(
	log logging.Logger,
	id ids.ShortID,
	ip utils.IPDesc,
	networkID uint32,
	version version.Version,
	parser version.Parser,
	listener net.Listener,
	dialer Dialer,
	serverUpgrader,
	clientUpgrader Upgrader,
	vdrs validators.Set,
	router router.Router,
) Network {
	return NewNetwork(
		log,
		id,
		ip,
		networkID,
		version,
		parser,
		listener,
		dialer,
		serverUpgrader,
		clientUpgrader,
		vdrs,
		router,
		defaultInitialReconnectDelay,
		defaultMaxReconnectDelay,
		defaultMaxMessageSize,
		defaultSendQueueSize,
		defaultMaxClockDifference,
		defaultPeerListGossipSpacing,
		defaultPeerListGossipSize,
		defaultPeerListStakerGossipFraction,
		defaultGetVersionTimeout,
		defaultAllowPrivateIPs,
		defaultGossipSize,
	)
}

// NewNetwork ...
func NewNetwork(
	log logging.Logger,
	id ids.ShortID,
	ip utils.IPDesc,
	networkID uint32,
	version version.Version,
	parser version.Parser,
	listener net.Listener,
	dialer Dialer,
	serverUpgrader,
	clientUpgrader Upgrader,
	vdrs validators.Set,
	router router.Router,
	initialReconnectDelay,
	maxReconnectDelay time.Duration,
	maxMessageSize uint32,
	sendQueueSize int,
	maxClockDifference time.Duration,
	peerListGossipSpacing time.Duration,
	peerListGossipSize int,
	peerListStakerGossipFraction int,
	getVersionTimeout time.Duration,
	allowPrivateIPs bool,
	gossipSize int,
) Network {
	net := &network{
		log:                          log,
		id:                           id,
		ip:                           ip,
		networkID:                    networkID,
		version:                      version,
		parser:                       parser,
		listener:                     listener,
		dialer:                       dialer,
		serverUpgrader:               serverUpgrader,
		clientUpgrader:               clientUpgrader,
		vdrs:                         vdrs,
		router:                       router,
		initialReconnectDelay:        initialReconnectDelay,
		maxReconnectDelay:            maxReconnectDelay,
		maxMessageSize:               maxMessageSize,
		sendQueueSize:                sendQueueSize,
		maxClockDifference:           maxClockDifference,
		peerListGossipSpacing:        peerListGossipSpacing,
		peerListGossipSize:           peerListGossipSize,
		peerListStakerGossipFraction: peerListStakerGossipFraction,
		getVersionTimeout:            getVersionTimeout,
		allowPrivateIPs:              allowPrivateIPs,
		gossipSize:                   gossipSize,

		disconnectedIPs: make(map[string]struct{}),
		connectedIPs:    make(map[string]struct{}),
		peers:           make(map[[20]byte]*peer),
	}
	net.executor.Initialize()
	return net
}

func (n *network) Dispatch() error {
	go n.gossip()
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			return err
		}
		go n.upgrade(&peer{
			net:  n,
			conn: conn,
		}, n.serverUpgrader)
	}
}

func (n *network) RegisterHandler(h Handler) {
	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	if h.Connected(n.id) {
		return
	}
	for _, peer := range n.peers {
		if peer.connected {
			if h.Connected(peer.id) {
				return
			}
		}
	}
	n.handlers = append(n.handlers, h)
}

func (n *network) IPs() []utils.IPDesc {
	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	ips := []utils.IPDesc(nil)
	for _, peer := range n.peers {
		if peer.connected {
			ips = append(ips, peer.ip)
		}
	}
	return ips
}

func (n *network) gossip() {
	t := time.NewTicker(n.peerListGossipSpacing)
	defer t.Stop()

	for range t.C {
		ips := n.validatorIPs()
		if len(ips) == 0 {
			n.log.Debug("skipping validator gossiping as no validators are connected")
			continue
		}
		msg, err := n.b.PeerList(ips)
		if err != nil {
			n.log.Warn("failed to gossip PeerList message due to %s", err)
			continue
		}

		n.stateLock.Lock()
		if n.closed {
			n.stateLock.Unlock()
			return
		}

		stakers := []*peer(nil)
		nonStakers := []*peer(nil)
		for _, peer := range n.peers {
			if n.vdrs.Contains(peer.id) {
				stakers = append(stakers, peer)
			} else {
				nonStakers = append(nonStakers, peer)
			}
		}

		numStakersToSend := (n.peerListGossipSize + n.peerListStakerGossipFraction - 1) / n.peerListStakerGossipFraction
		if len(stakers) < numStakersToSend {
			numStakersToSend = len(stakers)
		}
		numNonStakersToSend := n.peerListGossipSize - numStakersToSend
		if len(nonStakers) < numNonStakersToSend {
			numNonStakersToSend = len(nonStakers)
		}

		sampler := random.Uniform{N: len(stakers)}
		for i := 0; i < numStakersToSend; i++ {
			stakers[sampler.Sample()].Send(msg)
		}
		sampler.N = len(nonStakers)
		sampler.Replace()
		for i := 0; i < numNonStakersToSend; i++ {
			nonStakers[sampler.Sample()].Send(msg)
		}
		n.stateLock.Unlock()
	}
}

func (n *network) Close() error {
	n.stateLock.Lock()
	if n.closed {
		n.stateLock.Unlock()
		return nil
	}

	n.closed = true
	err := n.listener.Close()
	n.stateLock.Unlock()

	for _, peer := range n.peers {
		peer.Close() // Grabs the stateLock
	}
	return err
}

func (n *network) Track(ip utils.IPDesc) {
	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	n.track(ip)
}

func (n *network) track(ip utils.IPDesc) {
	if n.closed {
		return
	}

	str := ip.String()
	if _, ok := n.disconnectedIPs[str]; ok {
		return
	}
	if _, ok := n.connectedIPs[str]; ok {
		return
	}
	n.disconnectedIPs[str] = struct{}{}

	go n.connectTo(ip)
}

func (n *network) connectTo(ip utils.IPDesc) {
	str := ip.String()
	delay := n.initialReconnectDelay
	for {
		n.stateLock.Lock()
		_, isDisconnected := n.disconnectedIPs[str]
		_, isConnected := n.connectedIPs[str]
		n.stateLock.Unlock()

		if !isDisconnected || isConnected {
			// If the IP was discovered by that peer connecting to us, we don't
			// need to attempt to connect anymore
			return
		}

		err := n.attemptConnect(ip)
		if err == nil {
			return
		}
		// TODO: Log error

		time.Sleep(delay)
		delay *= 2
		if delay > n.maxReconnectDelay {
			delay = n.maxReconnectDelay
		}
	}
}

func (n *network) attemptConnect(ip utils.IPDesc) error {
	n.log.Verbo("attempting to connect to %s", ip)

	conn, err := n.dialer.Dial(ip)
	if err != nil {
		return err
	}
	return n.upgrade(&peer{
		net:  n,
		ip:   ip,
		conn: conn,
	}, n.clientUpgrader)
}

func (n *network) upgrade(p *peer, upgrader Upgrader) error {
	id, conn, err := upgrader.Upgrade(p.conn)
	if err != nil {
		n.log.Verbo("failed to upgrade connection with %s", err)
		return err
	}
	p.sender = make(chan []byte, n.sendQueueSize)
	p.id = id
	p.conn = conn

	key := id.Key()

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	if n.closed {
		return nil
	}

	_, ok := n.peers[key]
	if ok {
		p.conn.Close()
		return nil
	}

	n.peers[key] = p
	p.Start()
	return nil
}

func (n *network) validatorIPs() []utils.IPDesc {
	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	ips := []utils.IPDesc(nil)
	for _, peer := range n.peers {
		if peer.connected &&
			!peer.ip.IsZero() &&
			n.vdrs.Contains(peer.id) {
			ips = append(ips, peer.ip)
		}
	}
	return ips
}

// stateLock is held when called
func (n *network) connected(p *peer) {
	n.log.Debug("connected to %s at %s", p.id, p.ip)
	if !p.ip.IsZero() {
		str := p.ip.String()

		delete(n.disconnectedIPs, str)
		n.connectedIPs[str] = struct{}{}
	}

	for i := 0; i < len(n.handlers); {
		if n.handlers[i].Connected(p.id) {
			newLen := len(n.handlers) - 1
			n.handlers[i] = n.handlers[newLen] // remove the current handler
			n.handlers = n.handlers[:newLen]
		} else {
			i++
		}
	}
}

// stateLock is held when called
func (n *network) disconnected(p *peer) {
	n.log.Debug("disconnected from %s at %s", p.id, p.ip)
	key := p.id.Key()
	delete(p.net.peers, key)

	if !p.ip.IsZero() {
		str := p.ip.String()

		delete(n.disconnectedIPs, str)
		delete(n.connectedIPs, str)

		p.net.track(p.ip)
	}

	if p.connected {
		for i := 0; i < len(n.handlers); {
			if n.handlers[i].Disconnected(p.id) {
				newLen := len(n.handlers) - 1
				n.handlers[i] = n.handlers[newLen] // remove the current handler
				n.handlers = n.handlers[:newLen]
			} else {
				i++
			}
		}
	}
}

// Accept is called after every consensus decision
func (n *network) Accept(chainID, containerID ids.ID, container []byte) error {
	return n.gossipContainer(chainID, containerID, container)
}

// GetAcceptedFrontier implements the Sender interface.
func (n *network) GetAcceptedFrontier(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32) {
	msg, err := n.b.GetAcceptedFrontier(chainID, requestID)
	n.log.AssertNoError(err)

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	for _, validatorID := range validatorIDs.List() {
		vID := validatorID
		peer, sent := n.peers[vID.Key()]
		if sent {
			sent = peer.Send(msg)
		}
		if !sent {
			n.executor.Add(func() { n.router.GetAcceptedFrontierFailed(vID, chainID, requestID) })
		}
	}
}

// AcceptedFrontier implements the Sender interface.
func (n *network) AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	msg, err := n.b.AcceptedFrontier(chainID, requestID, containerIDs)
	if err != nil {
		n.log.Error("attempted to pack too large of an AcceptedFrontier message.\nNumber of containerIDs: %d",
			containerIDs.Len())
		return // Packing message failed
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.Send(msg)
	}
	if !sent {
		n.log.Debug("failed to send an AcceptedFrontier message to: %s", validatorID)
	}
}

// GetAccepted implements the Sender interface.
func (n *network) GetAccepted(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	msg, err := n.b.GetAccepted(chainID, requestID, containerIDs)
	if err != nil {
		for _, validatorID := range validatorIDs.List() {
			vID := validatorID
			n.executor.Add(func() { n.router.GetAcceptedFailed(vID, chainID, requestID) })
		}
		return
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	for _, validatorID := range validatorIDs.List() {
		vID := validatorID
		peer, sent := n.peers[vID.Key()]
		if sent {
			sent = peer.Send(msg)
		}
		if !sent {
			n.executor.Add(func() { n.router.GetAcceptedFailed(vID, chainID, requestID) })
		}
	}
}

// Accepted implements the Sender interface.
func (n *network) Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	msg, err := n.b.Accepted(chainID, requestID, containerIDs)
	if err != nil {
		n.log.Error("attempted to pack too large of an Accepted message.\nNumber of containerIDs: %d",
			containerIDs.Len())
		return // Packing message failed
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.Send(msg)
	}
	if !sent {
		n.log.Debug("failed to send an Accepted message to: %s", validatorID)
	}
}

// Get implements the Sender interface.
func (n *network) Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID) {
	msg, err := n.b.Get(chainID, requestID, containerID)
	n.log.AssertNoError(err)

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.Send(msg)
	}
	if !sent {
		n.log.Debug("failed to send a Get message to: %s", validatorID)
	}
}

// Put implements the Sender interface.
func (n *network) Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	msg, err := n.b.Put(chainID, requestID, containerID, container)
	if err != nil {
		n.log.Error("failed to build Put message because of container of size %d", len(container))
		return
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.Send(msg)
	}
	if !sent {
		n.log.Debug("failed to send a Put message to: %s", validatorID)
	}
}

// PushQuery implements the Sender interface.
func (n *network) PushQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	msg, err := n.b.PushQuery(chainID, requestID, containerID, container)
	if err != nil {
		for _, validatorID := range validatorIDs.List() {
			vID := validatorID
			n.executor.Add(func() { n.router.QueryFailed(vID, chainID, requestID) })
		}
		n.log.Error("attempted to pack too large of a PushQuery message.\nContainer length: %d", len(container))
		return // Packing message failed
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	for _, validatorID := range validatorIDs.List() {
		vID := validatorID
		peer, sent := n.peers[vID.Key()]
		if sent {
			sent = peer.Send(msg)
		}
		if !sent {
			n.log.Debug("failed sending a PushQuery message to: %s", vID)
			n.executor.Add(func() { n.router.QueryFailed(vID, chainID, requestID) })
		}
	}
}

// PullQuery implements the Sender interface.
func (n *network) PullQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerID ids.ID) {
	msg, err := n.b.PullQuery(chainID, requestID, containerID)
	n.log.AssertNoError(err)

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	for _, validatorID := range validatorIDs.List() {
		vID := validatorID
		peer, sent := n.peers[vID.Key()]
		if sent {
			sent = peer.Send(msg)
		}
		if !sent {
			n.log.Debug("failed sending a PullQuery message to: %s", vID)
			n.executor.Add(func() { n.router.QueryFailed(vID, chainID, requestID) })
		}
	}
}

// Chits implements the Sender interface.
func (n *network) Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set) {
	msg, err := n.b.Chits(chainID, requestID, votes)
	if err != nil {
		n.log.Error("failed to build Chits message because of %d votes", votes.Len())
		return
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.Send(msg)
	}
	if !sent {
		n.log.Debug("failed to send a Chits message to: %s", validatorID)
	}
}

// Gossip attempts to gossip the container to the network
func (n *network) Gossip(chainID, containerID ids.ID, container []byte) {
	if err := n.gossipContainer(chainID, containerID, container); err != nil {
		n.log.Error("error gossiping container %s to %s: %s", containerID, chainID, err)
	}
}

func (n *network) gossipContainer(chainID, containerID ids.ID, container []byte) error {
	msg, err := n.b.Put(chainID, math.MaxUint32, containerID, container)
	if err != nil {
		return fmt.Errorf("attempted to pack too large of a Put message.\nContainer length: %d", len(container))
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	allPeers := make([]*peer, 0, len(n.peers))
	for _, peer := range n.peers {
		allPeers = append(allPeers, peer)
	}

	numToGossip := n.gossipSize
	if numToGossip > len(allPeers) {
		numToGossip = len(allPeers)
	}

	sampler := random.Uniform{N: len(allPeers)}
	for i := 0; i < numToGossip; i++ {
		allPeers[sampler.Sample()].Send(msg)
	}
	return nil
}
