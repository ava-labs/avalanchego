// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"crypto"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cryptorand "crypto/rand"
	gomath "math"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errNetworkClosed       = errors.New("network closed")
	errPeerIsMyself        = errors.New("peer is myself")
	errNoPrimaryValidators = errors.New("no default subnet validators")

	_ Network = &network{}
)

func init() { rand.Seed(time.Now().UnixNano()) }

// Network defines the functionality of the networking library.
type Network interface {
	// All consensus messages can be sent through this interface. Thread safety
	// must be managed internally in the network.
	sender.ExternalSender

	// The network must be able to broadcast accepted decisions to random peers.
	// Thread safety must be managed internally in the network.
	snow.Acceptor

	// Should only be called once, will run until either a fatal error occurs,
	// or the network is closed. Returns a non-nil error.
	Dispatch() error

	// Attempt to connect to this IP. Thread safety must be managed internally
	// to the network. The network will never stop attempting to connect to this
	// IP.
	TrackIP(ip utils.IPDesc)

	// Attempt to connect to this node ID at IP. Thread safety must be managed
	// internally to the network.
	Track(ip utils.IPDesc, nodeID ids.ShortID)

	// Returns the description of the specified [nodeIDs] this network is currently
	// connected to externally or all nodes this network is connected to if [nodeIDs]
	// is empty. Thread safety must be managed internally to the network.
	Peers(nodeIDs []ids.ShortID) []PeerInfo

	// Close this network and all existing connections it has. Thread safety
	// must be managed internally to the network. Calling close multiple times
	// will return a nil error.
	Close() error

	// Return the IP of the node
	IP() utils.IPDesc

	NodeUptime() (UptimeResult, bool)

	// Has a health check
	health.Checker
}

type UptimeResult struct {
	WeightedAveragePercentage float64
	RewardingStakePercentage  float64
}

type network struct {
	config *Config
	// The metrics that this network tracks
	metrics metrics
	// Unix time at which last message of any type received over network
	// Must only be accessed atomically
	lastMsgReceivedTime int64
	// Unix time at which last message of any type sent over network
	// Must only be accessed atomically
	lastMsgSentTime int64
	// Keeps track of the percentage of sends that fail
	sendFailRateCalculator math.Averager
	log                    logging.Logger
	currentIP              utils.DynamicIPDesc
	versionCompatibility   version.Compatibility
	parser                 version.ApplicationParser
	listener               net.Listener
	dialer                 dialer.Dialer
	serverUpgrader         Upgrader
	clientUpgrader         Upgrader
	router                 router.Router // router must be thread safe
	// This field just makes sure we don't connect to ourselves when TLS is
	// disabled. So, cryptographically secure random number generation isn't
	// used here.
	dummyNodeID uint32
	clock       mockable.Clock
	mc          message.Creator

	stateLock sync.RWMutex
	closed    utils.AtomicBool

	// May contain peers that we have not finished the handshake with.
	peers peersData

	// disconnectedIPs, connectedIPs, peerAliasIPs, and myIPs
	// are maps with utils.IPDesc.String() keys that are used to determine if
	// we should attempt to dial an IP. [stateLock] should be held
	// whenever accessing one of these maps.
	disconnectedIPs map[string]struct{} // set of IPs we are attempting to connect to
	connectedIPs    map[string]struct{} // set of IPs we have open connections with
	peerAliasIPs    map[string]struct{} // set of alternate IPs we've reached existing peers at
	// TODO: bound the size of [myIPs] to avoid DoS. LRU caching would be ideal
	myIPs map[string]struct{} // set of IPs that resulted in my ID.

	// retryDelay is a map with utils.IPDesc.String() keys that is used to track
	// the backoff delay we should wait before attempting to dial an IP address
	// again.
	retryDelay map[string]time.Duration

	// ensures the close of the network only happens once.
	closeOnce sync.Once

	hasMasked        bool
	maskedValidators ids.ShortSet

	benchlistManager benchlist.Manager

	// [lastTimestampLock] should be held when touching  [lastVersionIP],
	// [lastVersionTimestamp], and [lastVersionSignature]
	timeForIPLock sync.Mutex
	// The IP for ourself that we included in the most recent Version message we
	// sent.
	lastVersionIP utils.IPDesc
	// The timestamp we included in the most recent Version message we sent.
	lastVersionTimestamp uint64
	// The signature we included in the most recent Version message we sent.
	lastVersionSignature []byte

	// Node ID --> Latest IP/timestamp of this node from a Version or PeerList message
	// The values in this map all have [signature] == nil
	// A peer is removed from this map when [connected] is called with the peer as the argument
	// TODO also remove from this map when the peer leaves the validator set
	latestPeerIP map[ids.ShortID]signedPeerIP

	// Node ID --> Function to execute to stop trying to dial the node.
	// A node is present in this map if and only if we are actively
	// trying to dial the node.
	connAttempts sync.Map

	// Rate-limits incoming messages
	inboundMsgThrottler         throttling.InboundMsgThrottler
	inboundConnUpgradeThrottler throttling.InboundConnUpgradeThrottler

	// Rate-limits outgoing messages
	outboundMsgThrottler throttling.OutboundMsgThrottler
}

type PeerListGossipConfig struct {
	PeerListSize                 uint32        `json:"peerListSize"`
	PeerListGossipSize           uint32        `json:"peerListGossipSize"`
	PeerListStakerGossipFraction uint32        `json:"peerListStakerGossipFraction"`
	PeerListGossipFreq           time.Duration `json:"peerListGossipFreq"`
}

type TimeoutConfig struct {
	GetVersionTimeout    time.Duration `json:"getVersionTimeout"`
	PingPongTimeout      time.Duration `json:"pingPongTimeout"`
	ReadHandshakeTimeout time.Duration `json:"readHandshakeTimeout"`
	// peerAliasTimeout is the age a peer alias must
	// be before we attempt to release it (so that we
	// attempt to dial the IP again if gossiped to us).
	PeerAliasTimeout time.Duration `json:"peerAliasTimeout"`
}

type DelayConfig struct {
	InitialReconnectDelay time.Duration `json:"initialReconnectDelay"`
	MaxReconnectDelay     time.Duration `json:"maxReconnectDelay"`
}

type GossipConfig struct {
	GossipAcceptedFrontierSize uint `json:"gossipAcceptedFrontierSize"`
	GossipOnAcceptSize         uint `json:"gossipOnAcceptSize"`
	AppGossipNonValidatorSize  uint `json:"appGossipNonValidatorSize"`
	AppGossipValidatorSize     uint `json:"appGossipValidatorSize"`
}

type ThrottlerConfig struct {
	InboundConnUpgradeThrottlerConfig throttling.InboundConnUpgradeThrottlerConfig `json:"inboundConnUpgradeThrottlerConfig"`
	InboundMsgThrottlerConfig         throttling.InboundMsgThrottlerConfig         `json:"inboundMsgThrottlerConfig"`
	OutboundMsgThrottlerConfig        throttling.MsgByteThrottlerConfig            `json:"outboundMsgThrottlerConfig"`
	MaxIncomingConnsPerSec            float64                                      `json:"maxIncomingConnsPerSec"`
}

type Config struct {
	HealthConfig         `json:"healthConfig"`
	PeerListGossipConfig `json:"peerListGossipConfig"`
	GossipConfig         `json:"gossipConfig"`
	TimeoutConfig        `json:"timeoutConfigs"`
	DelayConfig          `json:"delayConfig"`
	ThrottlerConfig      ThrottlerConfig `json:"throttlerConfig"`

	DialerConfig dialer.Config `json:"dialerConfig"`
	TLSConfig    *tls.Config   `json:"-"`

	Namespace          string              `json:"namespace"`
	MyNodeID           ids.ShortID         `json:"myNodeID"`
	MyIP               utils.DynamicIPDesc `json:"myIP"`
	NetworkID          uint32              `json:"networkID"`
	MaxClockDifference time.Duration       `json:"maxClockDifference"`
	PingFrequency      time.Duration       `json:"pingFrequency"`
	AllowPrivateIPs    bool                `json:"allowPrivateIPs"`
	CompressionEnabled bool                `json:"compressionEnabled"`
	// This node's TLS key
	TLSKey crypto.Signer `json:"-"`
	// WhitelistedSubnets of the node
	WhitelistedSubnets ids.Set        `json:"whitelistedSubnets"`
	Beacons            validators.Set `json:"beacons"`
	// Current validators in the Avalanche network
	Validators        validators.Manager `json:"validators"`
	UptimeCalculator  uptime.Calculator  `json:"-"`
	UptimeMetricFreq  time.Duration      `json:"uptimeMetricFreq"`
	UptimeRequirement float64            `json:"uptimeRequirement"`

	// Require that all connections must have at least one validator between the
	// 2 peers. This can be useful to enable if the node wants to connect to the
	// minimum number of nodes without impacting the network negatively.
	RequireValidatorToConnect bool `json:"requireValidatorToConnect"`
}

// peerElement holds onto the peer object as a result of helper functions
type peerElement struct {
	// the peer, if it wasn't a peer when we cloned the list this value will be
	// nil
	peer *peer
	// this is the validator id for the peer, we pass back to the caller for
	// logging purposes
	id ids.ShortID
}

// NewNetwork returns a new Network implementation with the provided parameters.
func NewNetwork(
	config *Config,
	msgCreator message.Creator,
	metricsRegisterer prometheus.Registerer,
	log logging.Logger,
	listener net.Listener,
	router router.Router,
	benchlistManager benchlist.Manager,
) (Network, error) {
	// #nosec G404
	netw := &network{
		log:                         log,
		currentIP:                   config.MyIP,
		parser:                      version.NewDefaultApplicationParser(),
		listener:                    listener,
		router:                      router,
		dummyNodeID:                 rand.Uint32(),
		disconnectedIPs:             make(map[string]struct{}),
		connectedIPs:                make(map[string]struct{}),
		peerAliasIPs:                make(map[string]struct{}),
		retryDelay:                  make(map[string]time.Duration),
		myIPs:                       map[string]struct{}{config.MyIP.IP().String(): {}},
		inboundConnUpgradeThrottler: throttling.NewInboundConnUpgradeThrottler(log, config.ThrottlerConfig.InboundConnUpgradeThrottlerConfig),
		benchlistManager:            benchlistManager,
		latestPeerIP:                make(map[ids.ShortID]signedPeerIP),
		versionCompatibility:        version.GetCompatibility(config.NetworkID),
		config:                      config,
		mc:                          msgCreator,
	}

	netw.serverUpgrader = NewTLSServerUpgrader(config.TLSConfig)
	netw.clientUpgrader = NewTLSClientUpgrader(config.TLSConfig)

	netw.dialer = dialer.NewDialer(constants.NetworkType, config.DialerConfig, log)
	primaryNetworkValidators, ok := config.Validators.GetValidators(constants.PrimaryNetworkID)
	if !ok {
		return nil, errNoPrimaryValidators
	}

	inboundMsgThrottler, err := throttling.NewInboundMsgThrottler(
		log,
		config.Namespace,
		metricsRegisterer,
		primaryNetworkValidators,
		config.ThrottlerConfig.InboundMsgThrottlerConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing inbound message throttler failed with: %w", err)
	}
	netw.inboundMsgThrottler = inboundMsgThrottler

	outboundMsgThrottler, err := throttling.NewSybilOutboundMsgThrottler(
		log,
		config.Namespace,
		metricsRegisterer,
		primaryNetworkValidators,
		config.ThrottlerConfig.OutboundMsgThrottlerConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing outbound message throttler failed with: %w", err)
	}
	netw.outboundMsgThrottler = outboundMsgThrottler

	netw.peers.initialize()
	netw.sendFailRateCalculator = math.NewSyncAverager(math.NewAverager(0, config.MaxSendFailRateHalflife, netw.clock.Time()))
	if err := netw.metrics.initialize(config.Namespace, metricsRegisterer); err != nil {
		return nil, fmt.Errorf("initializing network failed with: %w", err)
	}
	return netw, nil
}

// Assumes [n.stateLock] is not held.
func (n *network) Send(msg message.OutboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet {
	// retrieve target peers
	peers := n.getPeers(nodeIDs, subnetID, validatorOnly)
	return n.send(msg, true, peers)
}

// Assumes [n.stateLock] is not held.
func (n *network) Gossip(
	msg message.OutboundMessage,
	subnetID ids.ID,
	validatorOnly bool,
	numValidatorsToSend int,
	numNonValidatorsToSend int,
) ids.ShortSet {
	peers, err := n.selectPeersForGossip(subnetID, validatorOnly, numValidatorsToSend, numNonValidatorsToSend)
	if err != nil {
		n.log.Error("failed to sample peers: %s", err)

		// Because we failed to sample the peers, the message will never be
		// sent. This means that we should return the bytes allocated for the
		// message.
		msg.DecRef()
		return nil
	}
	return n.send(msg, true, peers)
}

// Accept is called after every consensus decision
// Assumes [n.stateLock] is not held.
func (n *network) Accept(ctx *snow.ConsensusContext, containerID ids.ID, container []byte) error {
	if ctx.GetState() != snow.NormalOp {
		// don't gossip during bootstrapping
		return nil
	}

	now := n.clock.Time()
	msg, err := n.mc.Put(ctx.ChainID, constants.GossipMsgRequestID, containerID, container)
	if err != nil {
		n.log.Debug("failed to build Put message for gossip (%s, %s): %s", ctx.ChainID, containerID, err)
		n.log.Verbo("container:\n%s", formatting.DumpBytes(container))
		n.sendFailRateCalculator.Observe(1, now)
		return fmt.Errorf("attempted to pack too large of a Put message.\nContainer length: %d", len(container))
	}

	n.Gossip(msg, ctx.SubnetID, ctx.IsValidatorOnly(), 0, int(n.config.GossipOnAcceptSize))
	return nil
}

// Select peers to gossip to.
func (n *network) selectPeersForGossip(subnetID ids.ID, validatorOnly bool, numValidatorsToSample, numNonValidatorsToSample int) ([]*peer, error) {
	n.stateLock.RLock()
	// Gossip the message to numNonValidatorsToSample random nodes in the
	// network. If this is a validator only subnet, selects only validators.
	peersAll, err := n.peers.sample(subnetID, validatorOnly, numNonValidatorsToSample)
	if err != nil {
		n.log.Debug("failed to sample %d peers: %s", numNonValidatorsToSample, err)
		n.stateLock.RUnlock()
		return nil, err
	}

	// Gossip the message to numValidatorsToSample random validators in the
	// network. This does not gossip by stake - but uniformly to the validator
	// set.
	peersValidators, err := n.peers.sample(subnetID, true, numValidatorsToSample)
	n.stateLock.RUnlock()
	if err != nil {
		n.log.Debug("failed to sample %d validators: %s", numValidatorsToSample, err)
		return nil, err
	}
	peersAll = append(peersAll, peersValidators...)
	return peersAll, nil
}

// Send the message to the provided peers.
//
// Send takes ownership of the provided message reference. So, the provided
// message should only be inspected if the reference has been externally
// increased.
//
// Assumes stateLock is not held.
func (n *network) send(msg message.OutboundMessage, connectedOnly bool, peers []*peer) ids.ShortSet {
	var (
		now    = n.clock.Time()
		msgLen = len(msg.Bytes())
		sentTo = ids.NewShortSet(len(peers))
		op     = msg.Op()
	)

	msgMetrics := n.metrics.messageMetrics[op]
	if msgMetrics == nil {
		n.log.Error("unregistered metric for message with op %s. Dropping it", op)

		// The message wasn't passed to any peers, so we can remove the
		// reference that was added.
		msg.DecRef()
		return sentTo
	}

	// send to peer and update metrics
	// note: peer may be nil
	for _, peer := range peers {
		// Add a reference to the message so that if it is sent, it won't be
		// collected until it is done being processed.
		msg.AddRef()
		if peer != nil &&
			(!connectedOnly || peer.finishedHandshake.GetValue()) &&
			!sentTo.Contains(peer.nodeID) &&
			peer.Send(msg) {
			sentTo.Add(peer.nodeID)

			// record metrics for success
			n.sendFailRateCalculator.Observe(0, now)
			msgMetrics.numSent.Inc()
			msgMetrics.sentBytes.Add(float64(msgLen))
			if saved := msg.BytesSavedCompression(); saved != 0 {
				msgMetrics.savedSentBytes.Observe(float64(saved))
			}
		} else {
			// record metrics for failure
			n.sendFailRateCalculator.Observe(1, now)
			msgMetrics.numFailed.Inc()

			// The message wasn't passed to the peer, so we should remove the
			// reference that was added.
			msg.DecRef()
		}
	}

	// The message has been passed to all peers that it will be sent to, so we
	// can decrease the sender reference now.
	msg.DecRef()
	return sentTo
}

// shouldUpgradeIncoming returns whether we should upgrade an incoming
// connection from a peer at the IP whose string repr. is [ipStr].
// Assumes stateLock is not held.
func (n *network) shouldUpgradeIncoming(ip utils.IPDesc) bool {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	ipStr := ip.String()
	if _, ok := n.connectedIPs[ipStr]; ok {
		n.log.Debug("not upgrading connection to %s because it's connected", ipStr)
		return false
	}
	if _, ok := n.myIPs[ipStr]; ok {
		n.log.Debug("not upgrading connection to %s because it's myself", ipStr)
		return false
	}
	if _, ok := n.peerAliasIPs[ipStr]; ok {
		n.log.Debug("not upgrading connection to %s because it's an alias", ipStr)
		return false
	}
	if !n.inboundConnUpgradeThrottler.ShouldUpgrade(ip) {
		n.log.Debug("not upgrading connection to %s due to rate-limiting", ipStr)
		n.metrics.inboundConnRateLimited.Inc()
		return false
	}
	n.metrics.inboundConnAllowed.Inc()

	// Note that we attempt to upgrade remote addresses in
	// [n.disconnectedIPs] because that could allow us to initialize
	// a connection with a peer we've been attempting to dial.
	return true
}

// shouldHoldConnection returns true if this node should have a connection to
// the provided peerID. If the node is attempting to connect to the minimum
// number of peers, then it should only connect if this node is a validator, or
// the peer is a validator/beacon.
func (n *network) shouldHoldConnection(peerID ids.ShortID) bool {
	return !n.config.RequireValidatorToConnect ||
		n.config.Validators.Contains(constants.PrimaryNetworkID, n.config.MyNodeID) ||
		n.config.Validators.Contains(constants.PrimaryNetworkID, peerID) ||
		n.config.Beacons.Contains(peerID)
}

// Dispatch starts accepting connections from other nodes attempting to connect
// to this node.
// Assumes [n.stateLock] is not held.
func (n *network) Dispatch() error {
	go n.gossipPeerList()      // Periodically gossip peers
	go n.updateUptimeMetrics() // Periodically update uptime metrics
	go n.inboundConnUpgradeThrottler.Dispatch()
	defer n.inboundConnUpgradeThrottler.Stop()
	go func() {
		duration := time.Until(n.versionCompatibility.MaskTime())
		time.Sleep(duration)

		n.stateLock.Lock()
		defer n.stateLock.Unlock()

		n.hasMasked = true
		for _, vdrID := range n.maskedValidators.List() {
			if err := n.config.Validators.MaskValidator(vdrID); err != nil {
				n.log.Error("failed to mask validator %s due to %s", vdrID, err)
			}
		}
		n.maskedValidators.Clear()
		n.log.Verbo("The new staking set is:\n%s", n.config.Validators)
	}()
	for { // Continuously accept new connections
		conn, err := n.listener.Accept() // Returns error when n.Close() is called
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				// Sleep for a small amount of time to try to wait for the
				// temporary error to go away.
				time.Sleep(time.Millisecond)
				continue
			}

			// When [n].Close() is called, [n.listener].Close() is called.
			// This causes [n.listener].Accept() to return an error.
			// If that happened, don't log/return an error here.
			if n.closed.GetValue() {
				return errNetworkClosed
			}
			n.log.Debug("error during server accept: %s", err)
			return err
		}

		// We pessimistically drop an incoming connection if the remote
		// address is found in connectedIPs, myIPs, or peerAliasIPs.
		// This protects our node from spending CPU cycles on TLS
		// handshakes to upgrade connections from existing peers.
		// Specifically, this can occur when one of our existing
		// peers attempts to connect to one our IP aliases (that they
		// aren't yet aware is an alias).
		remoteAddr := conn.RemoteAddr().String()
		ip, err := utils.ToIPDesc(remoteAddr)
		if err != nil {
			return fmt.Errorf("unable to convert remote address %s to IPDesc: %w", remoteAddr, err)
		}
		upgrade := n.shouldUpgradeIncoming(ip)
		if !upgrade {
			_ = conn.Close()
			continue
		}

		if conn, ok := conn.(*net.TCPConn); ok {
			if err := conn.SetLinger(0); err != nil {
				n.log.Warn("failed to set no linger due to: %s", err)
			}
			if err := conn.SetNoDelay(true); err != nil {
				n.log.Warn("failed to set socket nodelay due to: %s", err)
			}
		}

		go func() {
			if err := n.upgrade(newPeer(n, conn, utils.IPDesc{}), n.serverUpgrader); err != nil {
				n.log.Verbo("failed to upgrade connection: %s", err)
			}
		}()
	}
}

// Returns information about peers.
// If [nodeIDs] is empty, returns info about all peers that have finished
// the handshake. Otherwise, returns info about the peers in [nodeIDs]
// that have finished the handshake.
// Assumes [n.stateLock] is not held.
func (n *network) Peers(nodeIDs []ids.ShortID) []PeerInfo {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	if len(nodeIDs) == 0 { // Return info about all peers
		peers := make([]PeerInfo, 0, n.peers.size())
		for _, peer := range n.peers.peersList {
			if peer.finishedHandshake.GetValue() {
				peers = append(peers, n.NewPeerInfo(peer))
			}
		}
		return peers
	}

	peers := make([]PeerInfo, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs { // Return info about given peers
		if peer, ok := n.peers.getByID(nodeID); ok && peer.finishedHandshake.GetValue() {
			peers = append(peers, n.NewPeerInfo(peer))
		}
	}
	return peers
}

func (n *network) NewPeerInfo(peer *peer) PeerInfo {
	publicIPStr := ""
	if !peer.ip.IsZero() {
		publicIPStr = peer.getIP().String()
	}
	return PeerInfo{
		IP:             peer.conn.RemoteAddr().String(),
		PublicIP:       publicIPStr,
		ID:             peer.nodeID.PrefixedString(constants.NodeIDPrefix),
		Version:        peer.versionStr.GetValue().(string),
		LastSent:       time.Unix(atomic.LoadInt64(&peer.lastSent), 0),
		LastReceived:   time.Unix(atomic.LoadInt64(&peer.lastReceived), 0),
		Benched:        n.benchlistManager.GetBenched(peer.nodeID),
		ObservedUptime: json.Uint8(peer.observedUptime),
		TrackedSubnets: peer.trackedSubnets.List(),
	}
}

// Close implements the Network interface
// Assumes [n.stateLock] is not held.
func (n *network) Close() error {
	n.closeOnce.Do(n.close)
	return nil
}

func (n *network) close() {
	n.log.Info("shutting down network")

	if err := n.listener.Close(); err != nil {
		n.log.Debug("closing network listener failed with: %s", err)
	}

	if n.closed.GetValue() {
		return
	}

	n.stateLock.Lock()
	if n.closed.GetValue() {
		n.stateLock.Unlock()
		return
	}
	n.closed.SetValue(true)

	peersToClose := make([]*peer, n.peers.size())
	copy(peersToClose, n.peers.peersList)
	n.peers.reset()
	n.stateLock.Unlock()

	for _, peer := range peersToClose {
		peer.Close() // Grabs the stateLock
	}
}

// TrackIP implements the Network interface
// Assumes [n.stateLock] is not held.
func (n *network) TrackIP(ip utils.IPDesc) {
	n.Track(ip, ids.ShortEmpty)
}

// Track implements the Network interface
// Assumes [n.stateLock] is not held.
func (n *network) Track(ip utils.IPDesc, nodeID ids.ShortID) {
	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	n.track(ip, nodeID)
}

func (n *network) IP() utils.IPDesc {
	return n.currentIP.IP()
}

func (n *network) NodeUptime() (UptimeResult, bool) {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()
	primaryValidators, ok := n.config.Validators.GetValidators(constants.PrimaryNetworkID)
	if !ok {
		return UptimeResult{}, false
	}

	myStake, isValidator := primaryValidators.GetWeight(n.config.MyNodeID)
	if !isValidator {
		return UptimeResult{}, false
	}

	var (
		totalWeight          = float64(primaryValidators.Weight())
		totalWeightedPercent = 100 * float64(myStake)
		rewardingStake       = float64(myStake)
	)
	for _, peer := range n.peers.peersList {
		weight, ok := primaryValidators.GetWeight(peer.nodeID)
		if !ok {
			// this is not a validator skip it.
			continue
		}

		if !peer.finishedHandshake.GetValue() {
			continue
		}

		percent := float64(peer.observedUptime)
		weightFloat := float64(weight)
		totalWeightedPercent += percent * weightFloat

		// if this peer thinks we're above requirement add the weight
		if percent/100 >= n.config.UptimeRequirement {
			rewardingStake += weightFloat
		}
	}

	return UptimeResult{
		WeightedAveragePercentage: gomath.Abs(totalWeightedPercent / totalWeight),
		RewardingStakePercentage:  gomath.Abs(100 * rewardingStake / totalWeight),
	}, true
}

// assumes the stateLock is held.
// Try to connect to [nodeID] at [ip].
func (n *network) track(ip utils.IPDesc, nodeID ids.ShortID) {
	if n.closed.GetValue() {
		return
	}

	str := ip.String()
	if _, ok := n.disconnectedIPs[str]; ok {
		return
	}
	if _, ok := n.connectedIPs[str]; ok {
		return
	}
	if _, ok := n.peerAliasIPs[str]; ok {
		return
	}
	if _, ok := n.myIPs[str]; ok {
		return
	}
	// If we saw an IP gossiped for this node ID
	// with a later timestamp, don't track this old IP
	if latestIP, ok := n.latestPeerIP[nodeID]; ok {
		if !latestIP.ip.Equal(ip) {
			return
		}
	}
	n.disconnectedIPs[str] = struct{}{}

	go n.connectTo(ip, nodeID)
}

// Assumes [n.stateLock] is not held. Only returns after the network is closed.
func (n *network) gossipPeerList() {
	t := time.NewTicker(n.config.PeerListGossipFreq)
	defer t.Stop()

	for range t.C {
		if n.closed.GetValue() {
			return
		}

		ipCerts, err := n.validatorIPs()
		if err != nil {
			n.log.Error("failed to fetch validator IPs: %s", err)
			continue
		}

		if len(ipCerts) == 0 {
			n.log.Debug("skipping validator IP gossiping as no IPs are connected")
			continue
		}

		msg, err := n.mc.PeerList(ipCerts)
		if err != nil {
			n.log.Error("failed to build signed peerlist to gossip: %s. len(ips): %d",
				err,
				len(ipCerts))
			continue
		}

		numStakersToSend := int((n.config.PeerListGossipSize + n.config.PeerListStakerGossipFraction - 1) / n.config.PeerListStakerGossipFraction)
		numNonStakersToSend := int(n.config.PeerListGossipSize) - numStakersToSend

		n.Gossip(msg, constants.PrimaryNetworkID, false, numStakersToSend, numNonStakersToSend)
	}
}

// Assumes [n.stateLock] is not held. Only returns after the network is closed.
func (n *network) updateUptimeMetrics() {
	t := time.NewTicker(n.config.UptimeMetricFreq)
	defer t.Stop()

	for range t.C {
		if n.closed.GetValue() {
			return
		}
		result, _ := n.NodeUptime()
		n.metrics.nodeUptimeWeightedAverage.Set(result.WeightedAveragePercentage)
		n.metrics.nodeUptimeRewardingStake.Set(result.RewardingStakePercentage)
	}
}

// Returns when:
// * We connected to [ip]
// * The network is closed
// * We gave up connecting to [ip] because the IP is stale
// If [nodeID] == ids.ShortEmpty, won't cancel an existing
// attempt to connect to the peer with that IP.
// We do this so we don't cancel attempted to connect to bootstrap beacons.
// See method TrackIP.
// Assumes [n.stateLock] isn't held when this method is called.
func (n *network) connectTo(ip utils.IPDesc, nodeID ids.ShortID) {
	str := ip.String()
	n.stateLock.RLock()
	delay := n.retryDelay[str]
	n.stateLock.RUnlock()

	for {
		time.Sleep(delay)

		if delay == 0 {
			delay = n.config.InitialReconnectDelay
		}

		// Randomization is only performed here to distribute reconnection
		// attempts to a node that previously shut down. This doesn't require
		// cryptographically secure random number generation.
		delay = time.Duration(float64(delay) * (1 + rand.Float64())) // #nosec G404
		if delay > n.config.MaxReconnectDelay {
			// set the timeout to [.75, 1) * maxReconnectDelay
			delay = time.Duration(float64(n.config.MaxReconnectDelay) * (3 + rand.Float64()) / 4) // #nosec G404
		}

		n.stateLock.Lock()
		_, isDisconnected := n.disconnectedIPs[str]
		_, isConnected := n.connectedIPs[str]
		_, isMyself := n.myIPs[str]
		// If we saw an IP gossiped for this node ID
		// with a later timestamp, don't track this old IP
		isLatestIP := true
		if latestIP, ok := n.latestPeerIP[nodeID]; ok {
			isLatestIP = latestIP.ip.Equal(ip)
		}
		closed := n.closed

		if !isDisconnected || !isLatestIP || isConnected || isMyself || closed.GetValue() {
			// If the IP was discovered by the peer connecting to us, we don't
			// need to attempt to connect anymore

			// If we've seen an IP gossiped for this peer with a later timestamp,
			// don't try to connect to the old IP anymore

			// If the IP was discovered to be our IP address, we don't need to
			// attempt to connect anymore

			// If the network was closed, we should stop attempting to connect
			// to the peer

			n.stateLock.Unlock()
			return
		}
		n.retryDelay[str] = delay
		n.stateLock.Unlock()

		// If we are already trying to connect to this node ID,
		// cancel the existing attempt.
		// If [nodeID] is the empty ID, [ip] is a bootstrap beacon.
		// In that case, don't cancel existing connection attempt.
		if nodeID != ids.ShortEmpty {
			if cancel, exists := n.connAttempts.Load(nodeID); exists {
				n.log.Verbo("canceling attempt to connect to stale IP of %s%s", constants.NodeIDPrefix, nodeID)
				cancel.(context.CancelFunc)()
			}
		}

		// When [cancel] is called, we give up on this attempt to connect
		// (if we have not yet connected.)
		// This occurs at the sooner of:
		// * [connectTo] is called again with the same node ID
		// * The call to [attemptConnect] below returns
		ctx, cancel := context.WithCancel(context.Background())
		n.connAttempts.Store(nodeID, cancel)

		// Attempt to connect
		err := n.attemptConnect(ctx, ip)
		cancel() // to avoid goroutine leak

		n.connAttempts.Delete(nodeID)

		if err == nil {
			return
		}
		n.log.Verbo("error attempting to connect to %s: %s. Reattempting in %s",
			ip, err, delay)
	}
}

// Attempt to connect to the peer at [ip].
// If [ctx] is canceled, stops trying to connect.
// Returns nil if:
// * A connection was established
// * The network is closed.
// * [ctx] is canceled.
// Assumes [n.stateLock] is not held when this method is called.
func (n *network) attemptConnect(ctx context.Context, ip utils.IPDesc) error {
	n.log.Verbo("attempting to connect to %s", ip)
	conn, err := n.dialer.Dial(ctx, ip)
	if err != nil {
		// If [ctx] was canceled, return nil so we don't try to connect again
		if ctx.Err() == context.Canceled {
			return nil
		}
		// Error wasn't because connection attempt was canceled
		return err
	}

	if conn, ok := conn.(*net.TCPConn); ok {
		if err := conn.SetLinger(0); err != nil {
			n.log.Warn("failed to set no linger due to: %s", err)
		}
		if err := conn.SetNoDelay(true); err != nil {
			n.log.Warn("failed to set socket nodelay due to: %s", err)
		}
	}
	return n.upgrade(newPeer(n, conn, ip), n.clientUpgrader)
}

// Assumes [n.stateLock] is not held. Returns an error if the peer's connection
// wasn't able to be upgraded.
func (n *network) upgrade(p *peer, upgrader Upgrader) error {
	if err := p.conn.SetReadDeadline(time.Now().Add(n.config.ReadHandshakeTimeout)); err != nil {
		_ = p.conn.Close()
		n.log.Verbo("failed to set the read deadline with %s", err)
		return err
	}

	nodeID, conn, cert, err := upgrader.Upgrade(p.conn)
	if err != nil {
		_ = p.conn.Close()
		n.log.Verbo("failed to upgrade connection with %s", err)
		return err
	}

	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		_ = p.conn.Close()
		n.log.Verbo("failed to clear the read deadline with %s", err)
		return err
	}

	p.cert = cert
	p.nodeID = nodeID
	p.conn = conn

	if err := n.tryAddPeer(p); err != nil {
		_ = p.conn.Close()
		n.log.Debug("dropping peer connection due to: %s", err)
	}
	return nil
}

// Returns an error if the peer couldn't be added.
// Assumes [n.stateLock] is not held.
func (n *network) tryAddPeer(p *peer) error {
	n.stateLock.Lock()
	defer n.stateLock.Unlock()

	if n.closed.GetValue() {
		// the network is closing, so make sure that no further reconnect
		// attempts are made.
		return errNetworkClosed
	}

	ip := p.getIP()

	// if this connection is myself, then I should delete the connection and
	// mark the IP as one of mine.
	if p.nodeID == n.config.MyNodeID {
		if !ip.IsZero() {
			// if n.ip is less useful than p.ip set it to this IP
			if n.currentIP.IP().IsZero() {
				n.log.Info("setting my ip to %s because I was able to connect to myself through this channel",
					p.ip)
				n.currentIP.Update(p.ip)
			}
			str := ip.String()
			delete(n.disconnectedIPs, str)
			delete(n.retryDelay, str)
			n.myIPs[str] = struct{}{}
		}
		return errPeerIsMyself
	}

	if !n.shouldHoldConnection(p.nodeID) {
		if !ip.IsZero() {
			str := ip.String()
			delete(n.disconnectedIPs, str)
			delete(n.retryDelay, str)
		}
		return fmt.Errorf("non-validator connection from %s at %s", p.nodeID.PrefixedString(constants.NodeIDPrefix), ip)
	}

	// If I am already connected to this peer, then I should close this new
	// connection and add an alias record.
	if peer, ok := n.peers.getByID(p.nodeID); ok {
		if !ip.IsZero() {
			str := ip.String()
			delete(n.disconnectedIPs, str)
			delete(n.retryDelay, str)
			peer.addAlias(ip)
		}
		return fmt.Errorf("duplicated connection from %s at %s", p.nodeID.PrefixedString(constants.NodeIDPrefix), ip)
	}

	n.peers.add(p)
	n.metrics.numPeers.Set(float64(n.peers.size()))
	p.Start()
	return nil
}

// Returns the IPs, certs and signatures of validators we're connected
// to that have finished the handshake.
// Assumes [n.stateLock] is not held.
func (n *network) validatorIPs() ([]utils.IPCertDesc, error) {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	totalNumPeers := n.peers.size()

	numToSend := int(n.config.PeerListSize)
	if totalNumPeers < numToSend {
		numToSend = totalNumPeers
	}

	if numToSend == 0 {
		return nil, nil
	}
	res := make([]utils.IPCertDesc, 0, numToSend)

	s := sampler.NewUniform()
	if err := s.Initialize(uint64(totalNumPeers)); err != nil {
		return nil, err
	}

	for len(res) != numToSend {
		sampledIdx, err := s.Next() // lazy-sampling
		if err != nil {
			// all peers have been sampled and not enough valid ones found.
			// return what we have
			return res, nil
		}

		// TODO: consider possibility of grouping peers in different buckets
		// (e.g. validators/non-validators, connected/disconnected)
		peer, found := n.peers.getByIdx(int(sampledIdx))
		if !found {
			return res, fmt.Errorf("no peer at index %v", sampledIdx)
		}

		peerIP := peer.getIP()
		switch {
		case !peer.finishedHandshake.GetValue():
			continue
		case peerIP.IsZero():
			continue
		case !n.config.Validators.Contains(constants.PrimaryNetworkID, peer.nodeID):
			continue
		}

		peerVersion := peer.versionStruct.GetValue().(version.Application)
		if n.versionCompatibility.Unmaskable(peerVersion) != nil {
			continue
		}

		signedIP, ok := peer.sigAndTime.GetValue().(signedPeerIP)
		if !ok || !signedIP.ip.Equal(peerIP) {
			continue
		}

		res = append(res, utils.IPCertDesc{
			IPDesc:    peerIP,
			Signature: signedIP.signature,
			Cert:      peer.cert,
			Time:      signedIP.time,
		})
	}

	return res, nil
}

// Should only be called after the peer finishes the handshake.
// Should not be called after [disconnected] is called with this peer.
// Assumes [n.stateLock] is not held.
func (n *network) connected(p *peer) {
	p.net.stateLock.Lock()
	defer p.net.stateLock.Unlock()

	p.finishedHandshake.SetValue(true)

	peerVersion := p.versionStruct.GetValue().(version.Application)

	if n.hasMasked {
		if n.versionCompatibility.Unmaskable(peerVersion) != nil {
			if err := n.config.Validators.MaskValidator(p.nodeID); err != nil {
				n.log.Error("failed to mask validator %s due to %s", p.nodeID, err)
			}
		} else {
			if err := n.config.Validators.RevealValidator(p.nodeID); err != nil {
				n.log.Error("failed to reveal validator %s due to %s", p.nodeID, err)
			}
		}
		n.log.Verbo("The new staking set is:\n%s", n.config.Validators)
	} else {
		if n.versionCompatibility.WontMask(peerVersion) != nil {
			n.maskedValidators.Add(p.nodeID)
		} else {
			n.maskedValidators.Remove(p.nodeID)
		}
	}

	// TODO also delete an entry from this map when they leave the validator set
	delete(n.latestPeerIP, p.nodeID)

	ip := p.getIP()
	n.log.Debug("connected to %s at %s", p.nodeID, ip)

	if !ip.IsZero() {
		str := ip.String()

		delete(n.disconnectedIPs, str)
		delete(n.retryDelay, str)
		n.connectedIPs[str] = struct{}{}
	}

	n.router.Connected(p.nodeID, peerVersion)
	n.metrics.connected.Inc()
}

// should only be called after the peer is marked as connected.
// Assumes [n.stateLock] is not held.
func (n *network) disconnected(p *peer) {
	p.net.stateLock.Lock()
	defer p.net.stateLock.Unlock()

	ip := p.getIP()
	n.log.Debug("disconnected from %s at %s", p.nodeID, ip)

	n.peers.remove(p)
	n.metrics.numPeers.Set(float64(n.peers.size()))

	p.releaseAllAliases()

	if !ip.IsZero() {
		str := ip.String()

		delete(n.disconnectedIPs, str)
		delete(n.connectedIPs, str)

		if n.config.Validators.Contains(constants.PrimaryNetworkID, p.nodeID) {
			n.track(ip, p.nodeID)
		}
	}

	// Only send Disconnected to router if Connected was sent
	if p.finishedHandshake.GetValue() {
		n.router.Disconnected(p.nodeID)
	}
	n.metrics.disconnected.Inc()
}

// Safe copy the peers dressed as a peerElement
// Assumes [n.stateLock] is not held.
func (n *network) getPeerElements(nodeIDs ids.ShortSet) []*peerElement {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	if n.closed.GetValue() {
		return nil
	}

	peerElements := make([]*peerElement, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID                   // Prevent overwrite in next loop iteration
		peer, _ := n.peers.getByID(nodeID) // note: peer may be nil
		peerElements[i] = &peerElement{
			peer: peer,
			id:   nodeID,
		}
		i++
	}
	return peerElements
}

// Safe copy the peers
// Assumes [n.stateLock] is not held.
func (n *network) getPeers(nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) []*peer {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	if n.closed.GetValue() {
		return nil
	}

	var (
		peers = make([]*peer, nodeIDs.Len())
		index int
	)
	for nodeID := range nodeIDs {
		peer, ok := n.peers.getByID(nodeID)
		if ok &&
			peer.finishedHandshake.GetValue() &&
			(subnetID == constants.PrimaryNetworkID || peer.trackedSubnets.Contains(subnetID)) &&
			(!validatorOnly || n.config.Validators.Contains(subnetID, nodeID)) {
			peers[index] = peer
		}
		index++
	}
	return peers
}

// HealthCheck returns information about several network layer health checks.
// 1) Information about health check results
// 2) An error if the health check reports unhealthy
// Assumes [n.stateLock] is not held
func (n *network) HealthCheck() (interface{}, error) {
	// Get some data with the state lock held
	connectedTo := 0
	n.stateLock.RLock()
	for _, peer := range n.peers.peersList {
		if peer != nil && peer.finishedHandshake.GetValue() {
			connectedTo++
		}
	}
	sendFailRate := n.sendFailRateCalculator.Read()
	n.stateLock.RUnlock()

	// Make sure we're connected to at least the minimum number of peers
	isConnected := connectedTo >= int(n.config.HealthConfig.MinConnectedPeers)
	healthy := isConnected
	details := map[string]interface{}{
		"connectedPeers": connectedTo,
	}

	// Make sure we've received an incoming message within the threshold
	now := n.clock.Time()

	lastMsgReceivedAt := time.Unix(atomic.LoadInt64(&n.lastMsgReceivedTime), 0)
	timeSinceLastMsgReceived := now.Sub(lastMsgReceivedAt)
	isMsgRcvd := timeSinceLastMsgReceived <= n.config.HealthConfig.MaxTimeSinceMsgReceived
	healthy = healthy && isMsgRcvd
	details["timeSinceLastMsgReceived"] = timeSinceLastMsgReceived.String()
	n.metrics.timeSinceLastMsgReceived.Set(float64(timeSinceLastMsgReceived))

	// Make sure we've sent an outgoing message within the threshold
	lastMsgSentAt := time.Unix(atomic.LoadInt64(&n.lastMsgSentTime), 0)
	timeSinceLastMsgSent := now.Sub(lastMsgSentAt)
	isMsgSent := timeSinceLastMsgSent <= n.config.HealthConfig.MaxTimeSinceMsgSent
	healthy = healthy && isMsgSent
	details["timeSinceLastMsgSent"] = timeSinceLastMsgSent.String()
	n.metrics.timeSinceLastMsgSent.Set(float64(timeSinceLastMsgSent))

	// Make sure the message send failed rate isn't too high
	isMsgFailRate := sendFailRate <= n.config.HealthConfig.MaxSendFailRate
	healthy = healthy && isMsgFailRate
	details["sendFailRate"] = sendFailRate
	n.metrics.sendFailRate.Set(sendFailRate)

	// Network layer is unhealthy
	if !healthy {
		var errorReasons []string
		if !isConnected {
			errorReasons = append(errorReasons, fmt.Sprintf("not connected to a minimum of %d peer(s) only %d", n.config.HealthConfig.MinConnectedPeers, connectedTo))
		}
		if !isMsgRcvd {
			errorReasons = append(errorReasons, fmt.Sprintf("no messages from network received in %s > %s", timeSinceLastMsgReceived, n.config.HealthConfig.MaxTimeSinceMsgReceived))
		}
		if !isMsgSent {
			errorReasons = append(errorReasons, fmt.Sprintf("no messages from network sent in %s > %s", timeSinceLastMsgSent, n.config.HealthConfig.MaxTimeSinceMsgSent))
		}
		if !isMsgFailRate {
			errorReasons = append(errorReasons, fmt.Sprintf("messages failure send rate %g > %g", sendFailRate, n.config.HealthConfig.MaxSendFailRate))
		}

		return details, fmt.Errorf("network layer is unhealthy reason: %s", strings.Join(errorReasons, ", "))
	}
	return details, nil
}

// assume [n.stateLock] is held. Returns the timestamp and signature that should
// be sent in a Version message. We only update these values when our IP has
// changed.
func (n *network) getVersion(ip utils.IPDesc) (uint64, []byte, error) {
	n.timeForIPLock.Lock()
	defer n.timeForIPLock.Unlock()

	if !ip.Equal(n.lastVersionIP) {
		newTimestamp := n.clock.Unix()
		msgHash := ipAndTimeHash(ip, newTimestamp)
		sig, err := n.config.TLSKey.Sign(cryptorand.Reader, msgHash, crypto.SHA256)
		if err != nil {
			return 0, nil, err
		}

		n.lastVersionIP = ip
		n.lastVersionTimestamp = newTimestamp
		n.lastVersionSignature = sig
	}

	return n.lastVersionTimestamp, n.lastVersionSignature, nil
}
