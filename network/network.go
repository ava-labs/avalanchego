// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/api/health"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/sender"
	"github.com/ava-labs/gecko/snow/triggers"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/sampler"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/version"
)

// reasonable default values
const (
	defaultInitialReconnectDelay                     = time.Second
	defaultMaxReconnectDelay                         = time.Hour
	DefaultMaxMessageSize                     uint32 = 1 << 21
	defaultSendQueueSize                             = 1 << 10
	defaultMaxNetworkPendingSendBytes                = 1 << 29 // 512MB
	defaultNetworkPendingSendBytesToRateLimit        = defaultMaxNetworkPendingSendBytes / 4
	defaultMaxClockDifference                        = time.Minute
	defaultPeerListGossipSpacing                     = time.Minute
	defaultPeerListGossipSize                        = 100
	defaultPeerListStakerGossipFraction              = 2
	defaultGetVersionTimeout                         = 2 * time.Second
	defaultAllowPrivateIPs                           = true
	defaultGossipSize                                = 50
	defaultPingPongTimeout                           = time.Minute
	defaultPingFrequency                             = 3 * defaultPingPongTimeout / 4
)

func init() { rand.Seed(time.Now().UnixNano()) }

// Network defines the functionality of the networking library.
type Network interface {
	// All consensus messages can be sent through this interface. Thread safety
	// must be managed internally in the network.
	sender.ExternalSender

	// The network must be able to broadcast accepted decisions to random peers.
	// Thread safety must be managed internally in the network.
	triggers.Acceptor

	// The network should be able to report the last time the network interacted
	// with a peer
	health.Heartbeater

	// Should only be called once, will run until either a fatal error occurs,
	// or the network is closed. Returns a non-nil error.
	Dispatch() error

	// Attempt to connect to this IP. Thread safety must be managed internally
	// to the network. The network will never stop attempting to connect to this
	// IP.
	Track(ip utils.IPDesc)

	// Register a new connector that is called whenever a peer is connected to
	// or disconnected from. If the connector returns true, then it will never
	// be called again. Thread safety must be managed internally in the network.
	// The handler will initially be called with this local node's ID.
	RegisterConnector(h validators.Connector)

	// Returns the description of the nodes this network is currently connected
	// to externally. Thread safety must be managed internally to the network.
	Peers() []PeerID

	// Close this network and all existing connections it has. Thread safety
	// must be managed internally to the network. Calling close multiple times
	// will return a nil error.
	Close() error
}

type network struct {
	// The metrics that this network tracks
	metrics

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
	vdrs           validators.Set // set of current validators in the Avalanche network
	beacons        validators.Set // set of beacons in the Avalanche network
	router         router.Router  // router must be thread safe

	nodeID uint32

	clock         timer.Clock
	lastHeartbeat int64

	initialReconnectDelay              time.Duration
	maxReconnectDelay                  time.Duration
	maxMessageSize                     uint32
	sendQueueSize                      int
	maxNetworkPendingSendBytes         int
	networkPendingSendBytesToRateLimit int
	maxClockDifference                 time.Duration
	peerListGossipSpacing              time.Duration
	peerListGossipSize                 int
	peerListStakerGossipFraction       int
	getVersionTimeout                  time.Duration
	allowPrivateIPs                    bool
	gossipSize                         int
	pingPongTimeout                    time.Duration
	pingFrequency                      time.Duration

	executor timer.Executor

	b Builder

	stateLock       sync.Mutex
	pendingBytes    int
	closed          bool
	disconnectedIPs map[string]struct{}
	connectedIPs    map[string]struct{}
	retryDelay      map[string]time.Duration
	// TODO: bound the size of [myIPs] to avoid DoS. LRU caching would be ideal
	myIPs    map[string]struct{} // set of IPs that resulted in my ID.
	peers    map[[20]byte]*peer
	handlers []validators.Connector
}

// NewDefaultNetwork returns a new Network implementation with the provided
// parameters and some reasonable default values.
func NewDefaultNetwork(
	registerer prometheus.Registerer,
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
	beacons validators.Set,
	router router.Router,
) Network {
	return NewNetwork(
		registerer,
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
		beacons,
		router,
		defaultInitialReconnectDelay,
		defaultMaxReconnectDelay,
		DefaultMaxMessageSize,
		defaultSendQueueSize,
		defaultMaxNetworkPendingSendBytes,
		defaultNetworkPendingSendBytesToRateLimit,
		defaultMaxClockDifference,
		defaultPeerListGossipSpacing,
		defaultPeerListGossipSize,
		defaultPeerListStakerGossipFraction,
		defaultGetVersionTimeout,
		defaultAllowPrivateIPs,
		defaultGossipSize,
		defaultPingPongTimeout,
		defaultPingFrequency,
	)
}

// NewNetwork returns a new Network implementation with the provided parameters.
func NewNetwork(
	registerer prometheus.Registerer,
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
	beacons validators.Set,
	router router.Router,
	initialReconnectDelay,
	maxReconnectDelay time.Duration,
	maxMessageSize uint32,
	sendQueueSize int,
	maxNetworkPendingSendBytes int,
	networkPendingSendBytesToRateLimit int,
	maxClockDifference time.Duration,
	peerListGossipSpacing time.Duration,
	peerListGossipSize int,
	peerListStakerGossipFraction int,
	getVersionTimeout time.Duration,
	allowPrivateIPs bool,
	gossipSize int,
	pingPongTimeout time.Duration,
	pingFrequency time.Duration,
) Network {
	netw := &network{
		log:            log,
		id:             id,
		ip:             ip,
		networkID:      networkID,
		version:        version,
		parser:         parser,
		listener:       listener,
		dialer:         dialer,
		serverUpgrader: serverUpgrader,
		clientUpgrader: clientUpgrader,
		vdrs:           vdrs,
		beacons:        beacons,
		router:         router,
		// This field just makes sure we don't connect to ourselves when TLS is
		// disabled. So, cryptographically secure random number generation isn't
		// used here.
		nodeID:                             rand.Uint32(),
		initialReconnectDelay:              initialReconnectDelay,
		maxReconnectDelay:                  maxReconnectDelay,
		maxMessageSize:                     maxMessageSize,
		sendQueueSize:                      sendQueueSize,
		maxNetworkPendingSendBytes:         maxNetworkPendingSendBytes,
		networkPendingSendBytesToRateLimit: networkPendingSendBytesToRateLimit,
		maxClockDifference:                 maxClockDifference,
		peerListGossipSpacing:              peerListGossipSpacing,
		peerListGossipSize:                 peerListGossipSize,
		peerListStakerGossipFraction:       peerListStakerGossipFraction,
		getVersionTimeout:                  getVersionTimeout,
		allowPrivateIPs:                    allowPrivateIPs,
		gossipSize:                         gossipSize,
		pingPongTimeout:                    pingPongTimeout,
		pingFrequency:                      pingFrequency,
		disconnectedIPs:                    make(map[string]struct{}),
		connectedIPs:                       make(map[string]struct{}),
		retryDelay:                         make(map[string]time.Duration),
		myIPs:                              map[string]struct{}{ip.String(): {}},
		peers:                              make(map[[20]byte]*peer),
	}
	if err := netw.initialize(registerer); err != nil {
		log.Warn("initializing network metrics failed with: %s", err)
	}
	netw.executor.Initialize()
	netw.heartbeat()
	return netw
}

// GetAcceptedFrontier implements the Sender interface.
func (n *network) GetAcceptedFrontier(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time) {
	msg, err := n.b.GetAcceptedFrontier(chainID, requestID, uint64(deadline.Sub(n.clock.Time())))
	n.log.AssertNoError(err)

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	for _, validatorID := range validatorIDs.List() {
		vID := validatorID
		peer, sent := n.peers[vID.Key()]
		if sent {
			sent = peer.send(msg)
		}
		if !sent {
			n.executor.Add(func() { n.router.GetAcceptedFrontierFailed(vID, chainID, requestID) })
			n.getAcceptedFrontier.numFailed.Inc()
		} else {
			n.getAcceptedFrontier.numSent.Inc()
		}
	}
}

// AcceptedFrontier implements the Sender interface.
func (n *network) AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	msg, err := n.b.AcceptedFrontier(chainID, requestID, containerIDs)
	if err != nil {
		n.log.Error("failed to build AcceptedFrontier(%s, %d, %s): %s",
			chainID,
			requestID,
			containerIDs,
			err)
		return // Packing message failed
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.send(msg)
	}
	if !sent {
		n.log.Debug("failed to send AcceptedFrontier(%s, %s, %d, %s)",
			validatorID,
			chainID,
			requestID,
			containerIDs)
		n.acceptedFrontier.numFailed.Inc()
	} else {
		n.acceptedFrontier.numSent.Inc()
	}
}

// GetAccepted implements the Sender interface.
func (n *network) GetAccepted(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerIDs ids.Set) {
	msg, err := n.b.GetAccepted(chainID, requestID, uint64(deadline.Sub(n.clock.Time())), containerIDs)
	if err != nil {
		n.log.Error("failed to build GetAccepted(%s, %d, %s): %s",
			chainID,
			requestID,
			containerIDs,
			err)
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
			sent = peer.send(msg)
		}
		if !sent {
			n.log.Debug("failed to send GetAccepted(%s, %s, %d, %s)",
				validatorID,
				chainID,
				requestID,
				containerIDs)
			n.executor.Add(func() { n.router.GetAcceptedFailed(vID, chainID, requestID) })
			n.getAccepted.numFailed.Inc()
		} else {
			n.getAccepted.numSent.Inc()
		}
	}
}

// Accepted implements the Sender interface.
func (n *network) Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	msg, err := n.b.Accepted(chainID, requestID, containerIDs)
	if err != nil {
		n.log.Error("failed to build Accepted(%s, %d, %s): %s",
			chainID,
			requestID,
			containerIDs,
			err)
		return // Packing message failed
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.send(msg)
	}
	if !sent {
		n.log.Debug("failed to send Accepted(%s, %s, %d, %s)",
			validatorID,
			chainID,
			requestID,
			containerIDs)
		n.accepted.numFailed.Inc()
	} else {
		n.accepted.numSent.Inc()
	}
}

// GetAncestors implements the Sender interface.
func (n *network) GetAncestors(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	msg, err := n.b.GetAncestors(chainID, requestID, uint64(deadline.Sub(n.clock.Time())), containerID)
	if err != nil {
		n.log.Error("failed to build GetAncestors message: %s", err)
		return
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.send(msg)
	}
	if !sent {
		n.log.Debug("failed to send GetAncestors(%s, %s, %d, %s)",
			validatorID,
			chainID,
			requestID,
			containerID)
		n.executor.Add(func() { n.router.GetAncestorsFailed(validatorID, chainID, requestID) })
		n.getAncestors.numFailed.Inc()
	} else {
		n.getAncestors.numSent.Inc()
	}
}

// MultiPut implements the Sender interface.
func (n *network) MultiPut(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte) {
	msg, err := n.b.MultiPut(chainID, requestID, containers)
	if err != nil {
		n.log.Error("failed to build MultiPut message because of container of size %d", len(containers))
		return
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.send(msg)
	}
	if !sent {
		n.log.Debug("failed to send MultiPut(%s, %s, %d, %d)",
			validatorID,
			chainID,
			requestID,
			len(containers))
		n.multiPut.numFailed.Inc()
	} else {
		n.multiPut.numSent.Inc()
	}
}

// Get implements the Sender interface.
func (n *network) Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	msg, err := n.b.Get(chainID, requestID, uint64(deadline.Sub(n.clock.Time())), containerID)
	n.log.AssertNoError(err)

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.send(msg)
	}
	if !sent {
		n.log.Debug("failed to send Get(%s, %s, %d, %s)",
			validatorID,
			chainID,
			requestID,
			containerID)
		n.executor.Add(func() { n.router.GetFailed(validatorID, chainID, requestID) })
		n.get.numFailed.Inc()
	} else {
		n.get.numSent.Inc()
	}
}

// Put implements the Sender interface.
func (n *network) Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	msg, err := n.b.Put(chainID, requestID, containerID, container)
	if err != nil {
		n.log.Error("failed to build Put(%s, %d, %s): %s. len(container) : %d",
			chainID,
			requestID,
			containerID,
			err,
			len(container))
		return
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.send(msg)
	}
	if !sent {
		n.log.Debug("failed to send Put(%s, %s, %d, %s)",
			validatorID,
			chainID,
			requestID,
			containerID)
		n.log.Verbo("container: %s", formatting.DumpBytes{Bytes: container})
		n.put.numFailed.Inc()
	} else {
		n.put.numSent.Inc()
	}
}

// PushQuery implements the Sender interface.
func (n *network) PushQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID, container []byte) {
	msg, err := n.b.PushQuery(chainID, requestID, uint64(deadline.Sub(n.clock.Time())), containerID, container)

	if err != nil {
		n.log.Error("failed to build PushQuery(%s, %d, %s): %s. len(container): %d",
			chainID,
			requestID,
			containerID,
			err,
			len(container))
		n.log.Verbo("container: %s", formatting.DumpBytes{Bytes: container})
		for _, validatorID := range validatorIDs.List() {
			vID := validatorID
			n.executor.Add(func() { n.router.QueryFailed(vID, chainID, requestID) })
		}
		return // Packing message failed
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	for _, validatorID := range validatorIDs.List() {
		vID := validatorID
		peer, sent := n.peers[vID.Key()]
		if sent {
			sent = peer.send(msg)
		}
		if !sent {
			n.log.Debug("failed to send PushQuery(%s, %s, %d, %s)",
				validatorID,
				chainID,
				requestID,
				containerID)
			n.log.Verbo("container: %s", formatting.DumpBytes{Bytes: container})
			n.executor.Add(func() { n.router.QueryFailed(vID, chainID, requestID) })
			n.pushQuery.numFailed.Inc()
		} else {
			n.pushQuery.numSent.Inc()
		}
	}
}

// PullQuery implements the Sender interface.
func (n *network) PullQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	msg, err := n.b.PullQuery(chainID, requestID, uint64(deadline.Sub(n.clock.Time())), containerID)
	n.log.AssertNoError(err)

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	for _, validatorID := range validatorIDs.List() {
		vID := validatorID
		peer, sent := n.peers[vID.Key()]
		if sent {
			sent = peer.send(msg)
		}
		if !sent {
			n.log.Debug("failed to send PullQuery(%s, %s, %d, %s)",
				validatorID,
				chainID,
				requestID,
				containerID)
			n.executor.Add(func() { n.router.QueryFailed(vID, chainID, requestID) })
			n.pullQuery.numFailed.Inc()
		} else {
			n.pullQuery.numSent.Inc()
		}
	}
}

// Chits implements the Sender interface.
func (n *network) Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set) {
	msg, err := n.b.Chits(chainID, requestID, votes)
	if err != nil {
		n.log.Error("failed to build Chits(%s, %d, %s): %s",
			chainID,
			requestID,
			votes,
			err)
		return
	}

	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peer, sent := n.peers[validatorID.Key()]
	if sent {
		sent = peer.send(msg)
	}
	if !sent {
		n.log.Debug("failed to send Chits(%s, %s, %d, %s)",
			validatorID,
			chainID,
			requestID,
			votes)
		n.chits.numFailed.Inc()
	} else {
		n.chits.numSent.Inc()
	}
}

// Gossip attempts to gossip the container to the network
func (n *network) Gossip(chainID, containerID ids.ID, container []byte) {
	if err := n.gossipContainer(chainID, containerID, container); err != nil {
		n.log.Debug("failed to Gossip(%s, %s): %s", chainID, containerID, err)
		n.log.Verbo("container:\n%s", formatting.DumpBytes{Bytes: container})
	}
}

// Accept is called after every consensus decision
func (n *network) Accept(chainID, containerID ids.ID, container []byte) error {
	return n.gossipContainer(chainID, containerID, container)
}

// heartbeat registers a new heartbeat to signal liveness
func (n *network) heartbeat() { atomic.StoreInt64(&n.lastHeartbeat, n.clock.Time().Unix()) }

// GetHeartbeat returns the most recent heartbeat time
func (n *network) GetHeartbeat() int64 { return atomic.LoadInt64(&n.lastHeartbeat) }

// Dispatch starts accepting connections from other nodes attempting to connect
// to this node.
func (n *network) Dispatch() error {
	go n.gossip()
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			n.stateLock.Lock()
			closed := n.closed
			n.stateLock.Unlock()

			if closed {
				return err
			}
			n.log.Debug("error during server accept: %s", err)
			continue
		}
		go n.upgrade(&peer{
			net:  n,
			conn: conn,
		}, n.serverUpgrader)
	}
}

// RegisterConnector implements the Network interface
func (n *network) RegisterConnector(h validators.Connector) {
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

// IPs implements the Network interface
func (n *network) Peers() []PeerID {
	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	peers := []PeerID{}
	for _, peer := range n.peers {
		if peer.connected {
			peers = append(peers, PeerID{
				IP:           peer.conn.RemoteAddr().String(),
				PublicIP:     peer.ip.String(),
				ID:           peer.id.PrefixedString(constants.NodeIDPrefix),
				Version:      peer.versionStr,
				LastSent:     time.Unix(atomic.LoadInt64(&peer.lastSent), 0),
				LastReceived: time.Unix(atomic.LoadInt64(&peer.lastReceived), 0),
			})
		}
	}
	return peers
}

// Close implements the Network interface
func (n *network) Close() error {
	n.stateLock.Lock()
	if n.closed {
		n.stateLock.Unlock()
		return nil
	}

	n.closed = true
	err := n.listener.Close()
	if err != nil {
		n.log.Debug("closing network listener failed with: %s", err)
	}

	peersToClose := []*peer(nil)
	for _, peer := range n.peers {
		peersToClose = append(peersToClose, peer)
	}
	n.stateLock.Unlock()

	for _, peer := range peersToClose {
		peer.Close() // Grabs the stateLock
	}
	return err
}

// Track implements the Network interface
func (n *network) Track(ip utils.IPDesc) {
	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	n.track(ip)
}

// assumes the stateLock is not held.
func (n *network) gossipContainer(chainID, containerID ids.ID, container []byte) error {
	msg, err := n.b.Put(chainID, constants.GossipMsgRequestID, containerID, container)
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

	s := sampler.NewUniform()
	if err := s.Initialize(uint64(len(allPeers))); err != nil {
		return err
	}
	indices, err := s.Sample(numToGossip)
	if err != nil {
		return err
	}
	for _, index := range indices {
		if allPeers[int(index)].send(msg) {
			n.put.numSent.Inc()
		} else {
			n.put.numFailed.Inc()
		}
	}
	return nil
}

// assumes the stateLock is held.
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
	if _, ok := n.myIPs[str]; ok {
		return
	}
	n.disconnectedIPs[str] = struct{}{}

	go n.connectTo(ip)
}

// assumes the stateLock is not held. Only returns after the network is closed.
func (n *network) gossip() {
	t := time.NewTicker(n.peerListGossipSpacing)
	defer t.Stop()

	for range t.C {
		ips := n.validatorIPs()
		if len(ips) == 0 {
			n.log.Debug("skipping validator gossiping as no public validators are connected")
			continue
		}
		msg, err := n.b.PeerList(ips)
		if err != nil {
			n.log.Error("failed to build peer list to gossip: %s. len(ips): %d",
				err,
				len(ips))
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

		s := sampler.NewUniform()
		if err := s.Initialize(uint64(len(stakers))); err != nil {
			n.log.Error("failed to select stakers to sample: %s. len(stakers): %d",
				err,
				len(stakers))
			n.stateLock.Unlock()
			continue
		}
		stakerIndices, err := s.Sample(numStakersToSend)
		if err != nil {
			n.log.Error("failed to select stakers to sample: %s. len(stakers): %d",
				err,
				len(stakers))
			n.stateLock.Unlock()
			continue
		}
		for _, index := range stakerIndices {
			stakers[int(index)].send(msg)
		}

		if err := s.Initialize(uint64(len(nonStakers))); err != nil {
			n.log.Error("failed to select non-stakers to sample: %s. len(nonStakers): %d",
				err,
				len(nonStakers))
			n.stateLock.Unlock()
			continue
		}
		nonStakerIndices, err := s.Sample(numNonStakersToSend)
		if err != nil {
			n.log.Error("failed to select non-stakers to sample: %s. len(nonStakers): %d",
				err,
				len(nonStakers))
			n.stateLock.Unlock()
			continue
		}
		for _, index := range nonStakerIndices {
			nonStakers[int(index)].send(msg)
		}
		n.stateLock.Unlock()
	}
}

// assumes the stateLock is not held. Only returns if the ip is connected to or
// the network is closed
func (n *network) connectTo(ip utils.IPDesc) {
	str := ip.String()
	n.stateLock.Lock()
	delay := n.retryDelay[str]
	n.stateLock.Unlock()

	for {
		time.Sleep(delay)

		if delay == 0 {
			delay = n.initialReconnectDelay
		}

		// Randomization is only performed here to distribute reconnection
		// attempts to a node that previously shut down. This doesn't require
		// cryptographically secure random number generation.
		delay = time.Duration(float64(delay) * (1 + rand.Float64()))
		if delay > n.maxReconnectDelay {
			// set the timeout to [.75, 1) * maxReconnectDelay
			delay = time.Duration(float64(n.maxReconnectDelay) * (3 + rand.Float64()) / 4)
		}

		n.stateLock.Lock()
		_, isDisconnected := n.disconnectedIPs[str]
		_, isConnected := n.connectedIPs[str]
		_, isMyself := n.myIPs[str]
		closed := n.closed

		if !isDisconnected || isConnected || isMyself || closed {
			// If the IP was discovered by the peer connecting to us, we don't
			// need to attempt to connect anymore

			// If the IP was discovered to be our IP address, we don't need to
			// attempt to connect anymore

			// If the network was closed, we should stop attempting to connect
			// to the peer

			n.stateLock.Unlock()
			return
		}
		n.retryDelay[str] = delay
		n.stateLock.Unlock()

		err := n.attemptConnect(ip)
		if err == nil {
			return
		}
		n.log.Verbo("error attempting to connect to %s: %s. Reattempting in %s",
			ip, err, delay)
	}
}

// assumes the stateLock is not held. Returns nil if a connection was able to be
// established, or the network is closed.
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

// assumes the stateLock is not held. Returns an error if the peer's connection
// wasn't able to be upgraded.
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
		// the network is closing, so make sure that no further reconnect
		// attempts are made.
		_ = p.conn.Close()
		return nil
	}

	// if this connection is myself, then I should delete the connection and
	// mark the IP as one of mine.
	if id.Equals(n.id) {
		if !p.ip.IsZero() {
			// if n.ip is less useful than p.ip set it to this IP
			if n.ip.IsZero() {
				n.log.Info("setting my ip to %s because I was able to connect to myself through this channel",
					p.ip)
				n.ip = p.ip
			}
			str := p.ip.String()
			delete(n.disconnectedIPs, str)
			delete(n.retryDelay, str)
			n.myIPs[str] = struct{}{}
		}
		// don't attempt to reconnect to myself, so return nil even if closing
		// returns an error
		_ = p.conn.Close()
		return nil
	}

	// If I am already connected to this peer, then I should close this new
	// connection.
	if _, ok := n.peers[key]; ok {
		if !p.ip.IsZero() {
			str := p.ip.String()
			delete(n.disconnectedIPs, str)
			delete(n.retryDelay, str)
		}
		// I'm already connected to this peer, so don't attempt to reconnect to
		// this ip, even if an error occurres during closing
		_ = p.conn.Close()
		return nil
	}

	n.peers[key] = p
	n.numPeers.Set(float64(len(n.peers)))
	p.Start()
	return nil
}

// assumes the stateLock is not held. Returns the ips of connections that have
// valid IPs that are marked as validators.
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

// assumes the stateLock is held when called
// should only be called after the peer is marked as connected. Should not be
// called after disconnected is called with this peer.
func (n *network) connected(p *peer) {
	n.log.Debug("connected to %s at %s", p.id, p.ip)
	if !p.ip.IsZero() {
		str := p.ip.String()

		delete(n.disconnectedIPs, str)
		delete(n.retryDelay, str)
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

// assumes the stateLock is held when called
// should only be called after the peer is marked as connected.
func (n *network) disconnected(p *peer) {
	n.log.Debug("disconnected from %s at %s", p.id, p.ip)
	key := p.id.Key()
	delete(n.peers, key)
	n.numPeers.Set(float64(len(n.peers)))

	if !p.ip.IsZero() {
		str := p.ip.String()

		delete(n.disconnectedIPs, str)
		delete(n.connectedIPs, str)

		n.track(p.ip)
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
