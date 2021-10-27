// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/triggers"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errNetworkClosed       = errors.New("network closed")
	errPeerIsMyself        = errors.New("peer is myself")
	errNoPrimaryValidators = errors.New("no default subnet validators")
)

var _ Network = &network{}

func init() { rand.Seed(time.Now().UnixNano()) }

// Network defines the functionality of the networking library.
type Network interface {
	// All consensus messages can be sent through this interface. Thread safety
	// must be managed internally in the network.
	sender.ExternalSender

	// The network must be able to broadcast accepted decisions to random peers.
	// Thread safety must be managed internally in the network.
	triggers.Acceptor

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
	Peers(nodeIDs []ids.ShortID) []PeerID

	// Close this network and all existing connections it has. Thread safety
	// must be managed internally to the network. Calling close multiple times
	// will return a nil error.
	Close() error

	// Return the IP of the node
	IP() utils.IPDesc

	// Has a health check
	health.Checkable
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
	clock       timer.Clock
	c           message.Codec
	b           message.Builder

	stateLock sync.RWMutex
	closed    utils.AtomicBool

	// May contain peers that we have not finished the handshake with.
	peers peersData

	// disconnectedIPs, connectedIPs, peerAliasIPs, and myIPs
	// are maps with ip.String() keys that are used to determine if
	// we should attempt to dial an IP. [stateLock] should be held
	// whenever accessing one of these maps.
	disconnectedIPs map[string]struct{} // set of IPs we are attempting to connect to
	connectedIPs    map[string]struct{} // set of IPs we have open connections with
	peerAliasIPs    map[string]struct{} // set of alternate IPs we've reached existing peers at
	// TODO: bound the size of [myIPs] to avoid DoS. LRU caching would be ideal
	myIPs map[string]struct{} // set of IPs that resulted in my ID.

	// retryDelay is a map with ip.String() keys that is used to track
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

	// Contains []byte. Used as an optimization.
	// Can be accessed by multiple goroutines concurrently.
	byteSlicePool sync.Pool

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
	Validators validators.Manager `json:"validators"`

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
		byteSlicePool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, constants.DefaultByteSliceCap)
			},
		},
		versionCompatibility: version.GetCompatibility(config.NetworkID),
		config:               config,
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
		return nil, fmt.Errorf("initializing inbound message throttler failed with: %s", err)
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
		return nil, fmt.Errorf("initializing outbound message throttler failed with: %s", err)
	}
	netw.outboundMsgThrottler = outboundMsgThrottler

	codec, err := message.NewCodecWithAllocator(
		fmt.Sprintf("%s_codec", config.Namespace),
		metricsRegisterer,
		func() []byte {
			return netw.byteSlicePool.Get().([]byte)
		},
		int64(constants.DefaultMaxMessageSize),
	)
	if err != nil {
		return nil, fmt.Errorf("initializing codec failed with: %s", err)
	}
	netw.c = codec
	netw.b = message.NewBuilder(codec)
	netw.peers.initialize()
	netw.sendFailRateCalculator = math.NewSyncAverager(math.NewAverager(0, config.MaxSendFailRateHalflife, netw.clock.Time()))
	if err := netw.metrics.initialize(config.Namespace, metricsRegisterer); err != nil {
		return nil, fmt.Errorf("initializing network failed with: %s", err)
	}
	return netw, nil
}

// GetAcceptedFrontier implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendGetAcceptedFrontier(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration) []ids.ShortID {
	msg, err := n.b.GetAcceptedFrontier(chainID, requestID, uint64(deadline))
	n.log.AssertNoError(err)
	msgLen := len(msg.Bytes())

	sentTo := make([]ids.ShortID, 0, nodeIDs.Len())
	now := n.clock.Time()
	for _, peerElement := range n.getPeerElements(nodeIDs) {
		peer := peerElement.peer
		nodeID := peerElement.id
		if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, false) {
			n.log.Debug("failed to send GetAcceptedFrontier(%s, %s, %d)",
				nodeID,
				chainID,
				requestID)
			n.metrics.getAcceptedFrontier.numFailed.Inc()
			n.sendFailRateCalculator.Observe(1, now)
		} else {
			sentTo = append(sentTo, nodeID)
			n.metrics.getAcceptedFrontier.numSent.Inc()
			n.sendFailRateCalculator.Observe(0, now)
			n.metrics.getAcceptedFrontier.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				n.metrics.getAcceptedFrontier.savedSentBytes.Observe(float64(saved))
			}
		}
	}
	return sentTo
}

// AcceptedFrontier implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendAcceptedFrontier(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID) {
	now := n.clock.Time()

	peer := n.getPeer(nodeID)
	msg, err := n.b.AcceptedFrontier(chainID, requestID, containerIDs)
	if err != nil {
		n.log.Error("failed to build AcceptedFrontier(%s, %d, %s): %s",
			chainID,
			requestID,
			containerIDs,
			err)
		n.sendFailRateCalculator.Observe(1, now)
		return // Packing message failed
	}

	msgLen := len(msg.Bytes())
	if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, true) {
		n.log.Debug("failed to send AcceptedFrontier(%s, %s, %d, %s)",
			nodeID,
			chainID,
			requestID,
			containerIDs)
		n.metrics.acceptedFrontier.numFailed.Inc()
		n.sendFailRateCalculator.Observe(1, now)
	} else {
		n.metrics.acceptedFrontier.numSent.Inc()
		n.sendFailRateCalculator.Observe(0, now)
		n.metrics.acceptedFrontier.sentBytes.Add(float64(msgLen))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			n.metrics.acceptedFrontier.savedSentBytes.Observe(float64(saved))
		}
	}
}

// GetAccepted implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendGetAccepted(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerIDs []ids.ID) []ids.ShortID {
	now := n.clock.Time()

	msg, err := n.b.GetAccepted(chainID, requestID, uint64(deadline), containerIDs)
	if err != nil {
		n.log.Error("failed to build GetAccepted(%s, %d, %s): %s",
			chainID,
			requestID,
			containerIDs,
			err)
		n.sendFailRateCalculator.Observe(1, now)
		return nil
	}
	msgLen := len(msg.Bytes())

	sentTo := make([]ids.ShortID, 0, nodeIDs.Len())
	for _, peerElement := range n.getPeerElements(nodeIDs) {
		peer := peerElement.peer
		vID := peerElement.id
		if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, false) {
			n.log.Debug("failed to send GetAccepted(%s, %s, %d, %s)",
				vID,
				chainID,
				requestID,
				containerIDs)
			n.metrics.getAccepted.numFailed.Inc()
			n.sendFailRateCalculator.Observe(1, now)
		} else {
			n.metrics.getAccepted.numSent.Inc()
			n.sendFailRateCalculator.Observe(0, now)
			n.metrics.getAccepted.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				n.metrics.getAccepted.savedSentBytes.Observe(float64(saved))
			}
			sentTo = append(sentTo, vID)
		}
	}
	return sentTo
}

// Accepted implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendAccepted(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID) {
	now := n.clock.Time()

	msg, err := n.b.Accepted(chainID, requestID, containerIDs)
	if err != nil {
		n.log.Error("failed to build Accepted(%s, %d, %s): %s",
			chainID,
			requestID,
			containerIDs,
			err)
		n.sendFailRateCalculator.Observe(1, now)
		return // Packing message failed
	}
	msgLen := len(msg.Bytes())

	peer := n.getPeer(nodeID)
	if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, true) {
		n.log.Debug("failed to send Accepted(%s, %s, %d, %s)",
			nodeID,
			chainID,
			requestID,
			containerIDs)
		n.metrics.accepted.numFailed.Inc()
		n.sendFailRateCalculator.Observe(1, now)
	} else {
		n.sendFailRateCalculator.Observe(0, now)
		n.metrics.accepted.numSent.Inc()
		n.metrics.accepted.sentBytes.Add(float64(msgLen))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			n.metrics.accepted.savedSentBytes.Observe(float64(saved))
		}
	}
}

// GetAncestors implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendGetAncestors(nodeID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) bool {
	now := n.clock.Time()

	peer := n.getPeer(nodeID)

	msg, err := n.b.GetAncestors(chainID, requestID, uint64(deadline), containerID)
	if err != nil {
		n.log.Error("failed to build GetAncestors message: %s", err)
		n.sendFailRateCalculator.Observe(1, now)
		return false
	}

	msgLen := len(msg.Bytes())
	if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, true) {
		n.log.Debug("failed to send GetAncestors(%s, %s, %d, %s)",
			nodeID,
			chainID,
			requestID,
			containerID)
		n.metrics.getAncestors.numFailed.Inc()
		n.sendFailRateCalculator.Observe(1, now)
		return false
	}
	n.metrics.getAncestors.numSent.Inc()
	n.sendFailRateCalculator.Observe(0, now)
	n.metrics.getAncestors.sentBytes.Add(float64(msgLen))
	// assume that if [saved] == 0, [msg] wasn't compressed
	if saved := msg.BytesSavedCompression(); saved != 0 {
		n.metrics.getAncestors.savedSentBytes.Observe(float64(saved))
	}
	return true
}

// MultiPut implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendMultiPut(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte) {
	now := n.clock.Time()

	// Compress this message only if the peer can handle compressed
	// messages and we have compression enabled
	msg, err := n.b.MultiPut(chainID, requestID, containers, n.config.CompressionEnabled)
	if err != nil {
		n.log.Error("failed to build MultiPut message because of container of size %d", len(containers))
		n.sendFailRateCalculator.Observe(1, now)
		return
	}

	msgLen := len(msg.Bytes())
	if peer := n.getPeer(nodeID); peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, true) {
		n.log.Debug("failed to send MultiPut(%s, %s, %d, %d)",
			nodeID,
			chainID,
			requestID,
			len(containers))
		n.metrics.multiPut.numFailed.Inc()
		n.sendFailRateCalculator.Observe(1, now)
	} else {
		n.metrics.multiPut.numSent.Inc()
		n.sendFailRateCalculator.Observe(0, now)
		n.metrics.multiPut.sentBytes.Add(float64(msgLen))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			n.metrics.multiPut.savedSentBytes.Observe(float64(saved))
		}
	}
}

// Get implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendGet(nodeID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) bool {
	now := n.clock.Time()

	msg, err := n.b.Get(chainID, requestID, uint64(deadline), containerID)
	n.log.AssertNoError(err)

	msgLen := len(msg.Bytes())
	peer := n.getPeer(nodeID)
	if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, true) {
		n.log.Debug("failed to send Get(%s, %s, %d, %s)",
			nodeID,
			chainID,
			requestID,
			containerID)
		n.metrics.get.numFailed.Inc()
		n.sendFailRateCalculator.Observe(1, now)
		return false
	}
	n.metrics.get.numSent.Inc()
	n.sendFailRateCalculator.Observe(0, now)
	n.metrics.get.sentBytes.Add(float64(msgLen))
	// assume that if [saved] == 0, [msg] wasn't compressed
	if saved := msg.BytesSavedCompression(); saved != 0 {
		n.metrics.get.savedSentBytes.Observe(float64(saved))
	}
	return true
}

// Put implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendPut(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	now := n.clock.Time()

	// Compress this message only if the peer can handle compressed
	// messages and we have compression enabled
	msg, err := n.b.Put(chainID, requestID, containerID, container, n.config.CompressionEnabled)
	if err != nil {
		n.log.Error("failed to build Put(%s, %d, %s): %s. len(container) : %d",
			chainID,
			requestID,
			containerID,
			err,
			len(container))
		n.sendFailRateCalculator.Observe(1, now)
		return
	}
	msgLen := len(msg.Bytes())

	if peer := n.getPeer(nodeID); peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, true) {
		n.log.Debug("failed to send Put(%s, %s, %d, %s)",
			nodeID,
			chainID,
			requestID,
			containerID)
		n.log.Verbo("container: %s", formatting.DumpBytes{Bytes: container})
		n.metrics.put.numFailed.Inc()
		n.sendFailRateCalculator.Observe(1, now)
	} else {
		n.metrics.put.numSent.Inc()
		n.sendFailRateCalculator.Observe(0, now)
		n.metrics.put.sentBytes.Add(float64(msgLen))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			n.metrics.put.savedSentBytes.Observe(float64(saved))
		}
	}
}

// PushQuery implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendPushQuery(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID, container []byte) []ids.ShortID {
	now := n.clock.Time()

	msg, err := n.b.PushQuery(chainID, requestID, uint64(deadline), containerID, container, n.config.CompressionEnabled)
	if err != nil {
		n.log.Error("failed to build PushQuery(%s, %d, %s): %s. len(container): %d",
			chainID,
			requestID,
			containerID,
			err,
			len(container))
		n.log.Verbo("container: %s", formatting.DumpBytes{Bytes: container})
		n.sendFailRateCalculator.Observe(1, now)
		return nil // Packing message failed
	}

	sentTo := make([]ids.ShortID, 0, nodeIDs.Len())
	for _, peerElement := range n.getPeerElements(nodeIDs) {
		peer := peerElement.peer
		vID := peerElement.id
		if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, false) {
			n.log.Debug("failed to send PushQuery(%s, %s, %d, %s)",
				vID,
				chainID,
				requestID,
				containerID)
			n.log.Verbo("container: %s", formatting.DumpBytes{Bytes: container})
			n.metrics.pushQuery.numFailed.Inc()
			n.sendFailRateCalculator.Observe(1, now)
		} else {
			sentTo = append(sentTo, vID)
			n.metrics.pushQuery.numSent.Inc()
			n.sendFailRateCalculator.Observe(0, now)
			n.metrics.pushQuery.sentBytes.Add(float64(len(msg.Bytes())))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				n.metrics.pushQuery.savedSentBytes.Observe(float64(saved))
			}
		}
	}
	return sentTo
}

// PullQuery implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendPullQuery(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) []ids.ShortID {
	now := n.clock.Time()

	msg, err := n.b.PullQuery(chainID, requestID, uint64(deadline), containerID)
	n.log.AssertNoError(err)
	msgLen := len(msg.Bytes())

	sentTo := make([]ids.ShortID, 0, nodeIDs.Len())
	for _, peerElement := range n.getPeerElements(nodeIDs) {
		peer := peerElement.peer
		vID := peerElement.id
		if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, false) {
			n.log.Debug("failed to send PullQuery(%s, %s, %d, %s)",
				vID,
				chainID,
				requestID,
				containerID)
			n.metrics.pullQuery.numFailed.Inc()
			n.sendFailRateCalculator.Observe(1, now)
		} else {
			sentTo = append(sentTo, vID)
			n.metrics.pullQuery.numSent.Inc()
			n.sendFailRateCalculator.Observe(0, now)
			n.metrics.pullQuery.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				n.metrics.pullQuery.savedSentBytes.Observe(float64(saved))
			}
		}
	}
	return sentTo
}

// Chits implements the Sender interface.
// Assumes [n.stateLock] is not held.
func (n *network) SendChits(nodeID ids.ShortID, chainID ids.ID, requestID uint32, votes []ids.ID) {
	now := n.clock.Time()

	peer := n.getPeer(nodeID)
	msg, err := n.b.Chits(chainID, requestID, votes)
	if err != nil {
		n.log.Error("failed to build Chits(%s, %d, %s): %s",
			chainID,
			requestID,
			votes,
			err)
		n.sendFailRateCalculator.Observe(1, now)
		return
	}
	msgLen := len(msg.Bytes())

	if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, true) {
		n.log.Debug("failed to send Chits(%s, %s, %d, %s)",
			nodeID,
			chainID,
			requestID,
			votes)
		n.metrics.chits.numFailed.Inc()
		n.sendFailRateCalculator.Observe(1, now)
	} else {
		n.sendFailRateCalculator.Observe(0, now)
		n.metrics.chits.numSent.Inc()
		n.metrics.chits.sentBytes.Add(float64(msgLen))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			n.metrics.chits.savedSentBytes.Observe(float64(saved))
		}
	}
}

// AppRequest implements the Sender interface.
// assumes the stateLock is not held.
func (n *network) SendAppRequest(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, appRequestBytes []byte) []ids.ShortID {
	now := n.clock.Time()

	msg, err := n.b.AppRequest(chainID, requestID, uint64(deadline), appRequestBytes, n.config.CompressionEnabled)
	if err != nil {
		n.log.Error("failed to build AppRequest(%s, %d): %s", chainID, requestID, err)
		n.log.Verbo("message: %s", formatting.DumpBytes{Bytes: appRequestBytes})
		n.sendFailRateCalculator.Observe(1, now)
		return nil
	}

	sentTo := make([]ids.ShortID, 0, nodeIDs.Len())
	for _, peerElement := range n.getPeerElements(nodeIDs) {
		peer := peerElement.peer
		nodeID := peerElement.id
		if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, false) {
			n.log.Debug("failed to send AppRequest(%s, %s, %d)", nodeID, chainID, requestID)
			n.log.Verbo("failed message: %s", formatting.DumpBytes{Bytes: appRequestBytes})
			n.metrics.appRequest.numFailed.Inc()
			n.sendFailRateCalculator.Observe(1, now)
		} else {
			sentTo = append(sentTo, nodeID)
			n.metrics.appRequest.numSent.Inc()
			n.sendFailRateCalculator.Observe(0, now)
			n.metrics.appRequest.sentBytes.Add(float64(len(msg.Bytes())))
			if saved := msg.BytesSavedCompression(); saved != 0 {
				n.metrics.appRequest.savedSentBytes.Observe(float64(saved))
			}
		}
	}
	return sentTo
}

// AppResponse implements the Sender interface.
// assumes the stateLock is not held.
func (n *network) SendAppResponse(nodeID ids.ShortID, chainID ids.ID, requestID uint32, appResponse []byte) {
	now := n.clock.Time()

	msg, err := n.b.AppResponse(chainID, requestID, appResponse, n.config.CompressionEnabled)
	if err != nil {
		n.log.Error("failed to build AppResponse(%s, %d): %s", chainID, requestID, err)
		n.log.Verbo("message: %s", formatting.DumpBytes{Bytes: appResponse})
		n.sendFailRateCalculator.Observe(1, now)
		return
	}

	peer := n.getPeer(nodeID)
	if peer == nil || !peer.finishedHandshake.GetValue() || !peer.Send(msg, true) {
		n.log.Debug("failed to send AppResponse(%s, %s, %d)", nodeID, chainID, requestID)
		n.log.Verbo("container: %s", formatting.DumpBytes{Bytes: appResponse})
		n.metrics.appResponse.numFailed.Inc()
		n.sendFailRateCalculator.Observe(1, now)
	} else {
		n.metrics.appResponse.numSent.Inc()
		n.sendFailRateCalculator.Observe(0, now)
		n.metrics.appResponse.sentBytes.Add(float64(len(msg.Bytes())))
		if saved := msg.BytesSavedCompression(); saved != 0 {
			n.metrics.appResponse.savedSentBytes.Observe(float64(saved))
		}
	}
}

// AppGossip implements the Sender interface.
// assumes the stateLock is not held.
func (n *network) SendAppGossip(subnetID, chainID ids.ID, appGossipBytes []byte, validatorOnly bool) {
	n.stateLock.RLock()
	// Gossip the message to [n.config.AppGossipNonValidatorSize] random nodes
	// in the network. If this is a validator only subnet, selects only validators.
	peersAll, err := n.peers.sample(subnetID, validatorOnly, int(n.config.AppGossipNonValidatorSize))
	if err != nil {
		n.log.Debug("failed to sample %d peers for AppGossip: %s", n.config.AppGossipNonValidatorSize, err)
		n.stateLock.RUnlock()
		return
	}

	// Gossip the message to [n.config.AppGossipValidatorSize] random validators
	// in the network. This does not gossip by stake - but uniformly to the
	// validator set.
	peersValidators, err := n.peers.sample(subnetID, true, int(n.config.AppGossipValidatorSize))
	n.stateLock.RUnlock()
	if err != nil {
		n.log.Debug("failed to sample %d validators for AppGossip: %s", n.config.AppGossipValidatorSize, err)
		return
	}
	peersAll = append(peersAll, peersValidators...)
	if err := n.appGossipPeers(peersAll, chainID, appGossipBytes); err != nil {
		n.log.Debug("failed to SendAppGossip(%s, %s): %s", chainID, err)
		n.log.Verbo("message:\n%s", formatting.DumpBytes{Bytes: appGossipBytes})
	}
}

// SendAppGossipSpecific attempts to gossip the container to specific peers.
func (n *network) SendAppGossipSpecific(nodeIDs ids.ShortSet, subnetID, chainID ids.ID, appGossipBytes []byte, validatorOnly bool) {
	peers := n.getSubnetPeers(nodeIDs, subnetID, validatorOnly)
	if err := n.appGossipPeers(peers, chainID, appGossipBytes); err != nil {
		n.log.Debug("failed to SendAppGossipSpecific(%s, %s): %s", chainID, err)
		n.log.Verbo("message:\n%s", formatting.DumpBytes{Bytes: appGossipBytes})
	}
}

// SendGossip attempts to gossip the container to the network
// Assumes [n.stateLock] is not held.
func (n *network) SendGossip(subnetID, chainID, containerID ids.ID, container []byte, validatorOnly bool) {
	if err := n.gossipContainer(subnetID, chainID, containerID, container, n.config.GossipAcceptedFrontierSize, validatorOnly); err != nil {
		n.log.Debug("failed to Gossip(%s, %s): %s", chainID, containerID, err)
		n.log.Verbo("container:\n%s", formatting.DumpBytes{Bytes: container})
	}
}

// Accept is called after every consensus decision
// Assumes [n.stateLock] is not held.
func (n *network) Accept(ctx *snow.Context, containerID ids.ID, container []byte) error {
	if !ctx.IsBootstrapped() {
		// don't gossip during bootstrapping
		return nil
	}
	return n.gossipContainer(ctx.SubnetID, ctx.ChainID, containerID, container, n.config.GossipOnAcceptSize, ctx.IsValidatorOnly())
}

// shouldUpgradeIncoming returns whether we should
// upgrade an incoming connection from a peer
// at the IP whose string repr. is [ipStr].
// Assumes stateLock is not held.
func (n *network) shouldUpgradeIncoming(ipStr string) bool {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

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
	if !n.inboundConnUpgradeThrottler.ShouldUpgrade(ipStr) {
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
// the provided peerID. If the node is attempting to connect to the minimum number of peers, then it should only connect
// if this node is a validator, or the peer is a validator/beacon.
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
	go n.gossipPeerList() // Periodically gossip peers
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
		ipStr := ip.IP.String()
		upgrade := n.shouldUpgradeIncoming(ipStr)
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
func (n *network) Peers(nodeIDs []ids.ShortID) []PeerID {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	if len(nodeIDs) == 0 { // Return info about all peers
		peers := make([]PeerID, 0, n.peers.size())
		for _, peer := range n.peers.peersList {
			if peer.finishedHandshake.GetValue() {
				peers = append(peers, n.NewPeerID(peer))
			}
		}
		return peers
	}

	peers := make([]PeerID, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs { // Return info about given peers
		if peer, ok := n.peers.getByID(nodeID); ok && peer.finishedHandshake.GetValue() {
			peers = append(peers, n.NewPeerID(peer))
		}
	}
	return peers
}

func (n *network) NewPeerID(peer *peer) PeerID {
	publicIPStr := ""
	if !peer.ip.IsZero() {
		publicIPStr = peer.getIP().String()
	}

	return PeerID{
		IP:           peer.conn.RemoteAddr().String(),
		PublicIP:     publicIPStr,
		ID:           peer.nodeID.PrefixedString(constants.NodeIDPrefix),
		Version:      peer.versionStr.GetValue().(string),
		LastSent:     time.Unix(atomic.LoadInt64(&peer.lastSent), 0),
		LastReceived: time.Unix(atomic.LoadInt64(&peer.lastReceived), 0),
		Benched:      n.benchlistManager.GetBenched(peer.nodeID),
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

// Assumes [n.stateLock] is not held.
func (n *network) gossipContainer(subnetID, chainID, containerID ids.ID, container []byte, numToGossip uint, validatorOnly bool) error {
	now := n.clock.Time()

	// Sent to peers that handle compressed messages (and messages with the isCompress flag)
	msg, err := n.b.Put(chainID, constants.GossipMsgRequestID, containerID, container, n.config.CompressionEnabled)
	if err != nil {
		n.sendFailRateCalculator.Observe(1, now)
		return fmt.Errorf("attempted to pack too large of a Put message.\nContainer length: %d", len(container))
	}

	n.stateLock.RLock()
	peers, err := n.peers.sample(subnetID, validatorOnly, int(numToGossip))
	n.stateLock.RUnlock()
	if err != nil {
		return err
	}

	for _, peer := range peers {
		sent := peer.Send(msg, false)
		if sent {
			n.metrics.put.numSent.Inc()
			n.metrics.put.sentBytes.Add(float64(len(msg.Bytes())))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				n.metrics.put.savedSentBytes.Observe(float64(saved))
			}
			n.sendFailRateCalculator.Observe(0, now)
		} else {
			n.sendFailRateCalculator.Observe(1, now)
			n.metrics.put.numFailed.Inc()
		}
	}
	return nil
}

// helper method for SendAppGossip/SendAppGossipSpecific
func (n *network) appGossipPeers(peers []*peer, chainID ids.ID, appGossipBytes []byte) error {
	now := n.clock.Time()

	msg, err := n.b.AppGossip(chainID, appGossipBytes, n.config.CompressionEnabled)
	if err != nil {
		n.sendFailRateCalculator.Observe(1, now)
		return fmt.Errorf("failed to build AppGossip(%s): %w", chainID, err)
	}

	sentPeers := ids.NewShortSet(len(peers))
	for _, peer := range peers {
		// should never be nil but just in case
		if peer == nil || sentPeers.Contains(peer.nodeID) {
			continue
		}
		sentPeers.Add(peer.nodeID)
		sent := peer.Send(msg, false)
		if !sent {
			n.log.Debug("failed to send AppGossip(%s, %s)", peer.nodeID, chainID)
			n.log.Verbo("failed message: %s", formatting.DumpBytes{Bytes: appGossipBytes})
			n.metrics.appGossip.numFailed.Inc()
			n.sendFailRateCalculator.Observe(1, now)
		} else {
			n.metrics.appGossip.numSent.Inc()
			n.metrics.appGossip.sentBytes.Add(float64(len(msg.Bytes())))
			n.sendFailRateCalculator.Observe(0, now)
			if saved := msg.BytesSavedCompression(); saved != 0 {
				n.metrics.appGossip.savedSentBytes.Observe(float64(saved))
			}
		}
	}
	return nil
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

		allPeers := n.getAllPeers()
		if len(allPeers) == 0 {
			continue
		}

		stakers := make([]*peer, 0, len(allPeers))
		nonStakers := make([]*peer, 0, len(allPeers))
		for _, peer := range allPeers {
			if n.config.Validators.Contains(constants.PrimaryNetworkID, peer.nodeID) {
				stakers = append(stakers, peer)
			} else {
				nonStakers = append(nonStakers, peer)
			}
		}

		numStakersToSend := int((n.config.PeerListGossipSize + n.config.PeerListStakerGossipFraction - 1) / n.config.PeerListStakerGossipFraction)
		if len(stakers) < numStakersToSend {
			numStakersToSend = len(stakers)
		}
		numNonStakersToSend := int(n.config.PeerListGossipSize) - numStakersToSend
		if len(nonStakers) < numNonStakersToSend {
			numNonStakersToSend = len(nonStakers)
		}

		s := sampler.NewUniform()
		if err := s.Initialize(uint64(len(stakers))); err != nil {
			n.log.Error("failed to select stakers to sample: %s. len(stakers): %d",
				err,
				len(stakers))
			continue
		}
		stakerIndices, err := s.Sample(numStakersToSend)
		if err != nil {
			n.log.Error("failed to select stakers to sample: %s. len(stakers): %d",
				err,
				len(stakers))
			continue
		}

		if err := s.Initialize(uint64(len(nonStakers))); err != nil {
			n.log.Error("failed to select non-stakers to sample: %s. len(nonStakers): %d",
				err,
				len(nonStakers))
			continue
		}
		nonStakerIndices, err := s.Sample(numNonStakersToSend)
		if err != nil {
			n.log.Error("failed to select non-stakers to sample: %s. len(nonStakers): %d",
				err,
				len(nonStakers))
			continue
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

		// Sent to peers that handle compressed messages (and messages with the isCompress flag)
		msg, err := n.b.PeerList(ipCerts, n.config.CompressionEnabled)
		if err != nil {
			n.log.Error("failed to build signed peerlist to gossip: %s. len(ips): %d",
				err,
				len(ipCerts))
			continue
		}

		for _, index := range stakerIndices {
			peer := stakers[int(index)]
			peer.Send(msg, false)
		}
		for _, index := range nonStakerIndices {
			peer := nonStakers[int(index)]
			peer.Send(msg, false)
		}
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

	n.router.Connected(p.nodeID)
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
func (n *network) getSubnetPeers(nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) []*peer {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	if n.closed.GetValue() {
		return nil
	}

	peers := make([]*peer, 0, nodeIDs.Len())
	for nodeID := range nodeIDs {
		peer, ok := n.peers.getByID(nodeID)
		if ok && peer.finishedHandshake.GetValue() && peer.trackedSubnets.Contains(subnetID) && (!validatorOnly || n.config.Validators.Contains(subnetID, nodeID)) {
			peers = append(peers, peer)
		}
	}
	return peers
}

// Returns a copy of the peer set.
// Only includes peers that have finished the handshake.
// Assumes [n.stateLock] is not held.
func (n *network) getAllPeers() []*peer {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	if n.closed.GetValue() {
		return nil
	}

	peers := make([]*peer, 0, n.peers.size())
	for _, peer := range n.peers.peersList {
		if peer.finishedHandshake.GetValue() {
			peers = append(peers, peer)
		}
	}
	return peers
}

// Safe find a single peer
// Assumes [n.stateLock] is not held.
func (n *network) getPeer(nodeID ids.ShortID) *peer {
	n.stateLock.RLock()
	defer n.stateLock.RUnlock()

	if n.closed.GetValue() {
		return nil
	}

	res, _ := n.peers.getByID(nodeID) // note: peer may be nil
	return res
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
