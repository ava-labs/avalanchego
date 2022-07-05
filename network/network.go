// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gomath "math"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

const (
	ConnectedPeersKey           = "connectedPeers"
	TimeSinceLastMsgReceivedKey = "timeSinceLastMsgReceived"
	TimeSinceLastMsgSentKey     = "timeSinceLastMsgSent"
	SendFailRateKey             = "sendFailRate"
)

var (
	_                      sender.ExternalSender = &network{}
	_                      Network               = &network{}
	errNoPrimaryValidators                       = errors.New("no default subnet validators")
)

// Network defines the functionality of the networking library.
type Network interface {
	// All consensus messages can be sent through this interface. Thread safety
	// must be managed internally in the network.
	sender.ExternalSender

	// Has a health check
	health.Checker

	peer.Network
	common.SubnetTracker

	// StartClose this network and all existing connections it has. Calling
	// StartClose multiple times is handled gracefully.
	StartClose()

	// Should only be called once, will run until either a fatal error occurs,
	// or the network is closed.
	Dispatch() error

	// WantsConnection returns true if this node is willing to attempt to
	// connect to the provided nodeID. If the node is attempting to connect to
	// the minimum number of peers, then it should only connect if the peer is a
	// validator or beacon.
	WantsConnection(ids.NodeID) bool

	// Attempt to connect to this IP. The network will never stop attempting to
	// connect to this ID.
	ManuallyTrack(nodeID ids.NodeID, ip ips.IPPort)

	// PeerInfo returns information about peers. If [nodeIDs] is empty, returns
	// info about all peers that have finished the handshake. Otherwise, returns
	// info about the peers in [nodeIDs] that have finished the handshake.
	PeerInfo(nodeIDs []ids.NodeID) []peer.Info

	NodeUptime() (UptimeResult, bool)
}

type UptimeResult struct {
	WeightedAveragePercentage float64
	RewardingStakePercentage  float64
}

type network struct {
	config     *Config
	peerConfig *peer.Config
	metrics    *metrics
	// Signs my IP so I can send my signed IP address to other nodes in Version
	// messages
	ipSigner *ipSigner

	outboundMsgThrottler throttling.OutboundMsgThrottler

	// Limits the number of connection attempts based on IP.
	inboundConnUpgradeThrottler throttling.InboundConnUpgradeThrottler
	// Listens for and accepts new inbound connections
	listener net.Listener
	// Makes new outbound connections
	dialer dialer.Dialer
	// Does TLS handshakes for inbound connections
	serverUpgrader peer.Upgrader
	// Does TLS handshakes for outbound connections
	clientUpgrader peer.Upgrader

	// ensures the close of the network only happens once.
	closeOnce sync.Once
	// Cancelled on close
	onCloseCtx context.Context
	// Call [onCloseCtxCancel] to cancel [onCloseCtx] during close()
	onCloseCtxCancel func()

	sendFailRateCalculator math.Averager

	peersLock sync.RWMutex
	// trackedIPs contains the set of IPs that we are currently attempting to
	// connect to. An entry is added to this set when we first start attempting
	// to connect to the peer. An entry is deleted from this set once we have
	// finished the handshake.
	trackedIPs         map[ids.NodeID]*trackedIP
	manuallyTrackedIDs ids.NodeIDSet
	connectingPeers    peer.Set
	connectedPeers     peer.Set
	closing            bool

	// router is notified about all peer [Connected] and [Disconnected] events
	// as well as all non-handshake peer messages.
	//
	// It is ensured that [Connected] and [Disconnected] are called in
	// consistent ways. Specifically, the a peer starts in the disconnected
	// state and the network can change the peer's state from disconnected to
	// connected and back.
	//
	// It is ensured that [HandleInbound] is only called with a message from a
	// peer that is in the connected state.
	//
	// It is expected that the implementation of this interface can handle
	// concurrent calls to [Connected], [Disconnected], and [HandleInbound].
	router router.ExternalHandler
}

// NewNetwork returns a new Network implementation with the provided parameters.
func NewNetwork(
	config *Config,
	msgCreator message.Creator,
	metricsRegisterer prometheus.Registerer,
	log logging.Logger,
	listener net.Listener,
	dialer dialer.Dialer,
	router router.ExternalHandler,
	benchlistManager benchlist.Manager,
) (Network, error) {
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
		config.ResourceTracker,
		config.CPUTargeter,
		config.DiskTargeter,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing inbound message throttler failed with: %w", err)
	}

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

	peerMetrics, err := peer.NewMetrics(log, config.Namespace, metricsRegisterer)
	if err != nil {
		return nil, fmt.Errorf("initializing peer metrics failed with: %w", err)
	}

	metrics, err := newMetrics(config.Namespace, metricsRegisterer, config.WhitelistedSubnets)
	if err != nil {
		return nil, fmt.Errorf("initializing network metrics failed with: %w", err)
	}

	pingMessge, err := msgCreator.Ping()
	if err != nil {
		return nil, fmt.Errorf("initializing common ping message failed with: %w", err)
	}

	peerConfig := &peer.Config{
		ReadBufferSize:       config.PeerReadBufferSize,
		WriteBufferSize:      config.PeerWriteBufferSize,
		Metrics:              peerMetrics,
		MessageCreator:       msgCreator,
		Log:                  log,
		InboundMsgThrottler:  inboundMsgThrottler,
		Network:              nil, // This is set below.
		Router:               router,
		VersionCompatibility: version.GetCompatibility(config.NetworkID),
		MySubnets:            config.WhitelistedSubnets,
		Beacons:              config.Beacons,
		NetworkID:            config.NetworkID,
		PingFrequency:        config.PingFrequency,
		PongTimeout:          config.PingPongTimeout,
		MaxClockDifference:   config.MaxClockDifference,
		ResourceTracker:      config.ResourceTracker,
		PingMessage:          pingMessge,
	}
	onCloseCtx, cancel := context.WithCancel(context.Background())
	n := &network{
		config:               config,
		peerConfig:           peerConfig,
		metrics:              metrics,
		ipSigner:             newIPSigner(config.MyIPPort, &peerConfig.Clock, config.TLSKey),
		outboundMsgThrottler: outboundMsgThrottler,

		inboundConnUpgradeThrottler: throttling.NewInboundConnUpgradeThrottler(log, config.ThrottlerConfig.InboundConnUpgradeThrottlerConfig),
		listener:                    listener,
		dialer:                      dialer,
		serverUpgrader:              peer.NewTLSServerUpgrader(config.TLSConfig),
		clientUpgrader:              peer.NewTLSClientUpgrader(config.TLSConfig),

		onCloseCtx:       onCloseCtx,
		onCloseCtxCancel: cancel,

		sendFailRateCalculator: math.NewSyncAverager(math.NewAverager(
			0,
			config.SendFailRateHalflife,
			time.Now(),
		)),

		trackedIPs:      make(map[ids.NodeID]*trackedIP),
		connectingPeers: peer.NewSet(),
		connectedPeers:  peer.NewSet(),
		router:          router,
	}
	n.peerConfig.Network = n
	return n, nil
}

func (n *network) Send(msg message.OutboundMessage, nodeIDs ids.NodeIDSet, subnetID ids.ID, validatorOnly bool) ids.NodeIDSet {
	peers := n.getPeers(nodeIDs, subnetID, validatorOnly)
	n.peerConfig.Metrics.MultipleSendsFailed(
		msg.Op(),
		nodeIDs.Len()-len(peers),
	)
	return n.send(msg, peers)
}

func (n *network) Gossip(
	msg message.OutboundMessage,
	subnetID ids.ID,
	validatorOnly bool,
	numValidatorsToSend int,
	numNonValidatorsToSend int,
	numPeersToSend int,
) ids.NodeIDSet {
	peers := n.samplePeers(subnetID, validatorOnly, numValidatorsToSend, numNonValidatorsToSend, numPeersToSend)
	return n.send(msg, peers)
}

// HealthCheck returns information about several network layer health checks.
// 1) Information about health check results
// 2) An error if the health check reports unhealthy
func (n *network) HealthCheck() (interface{}, error) {
	n.peersLock.RLock()
	connectedTo := n.connectedPeers.Len()
	n.peersLock.RUnlock()

	sendFailRate := n.sendFailRateCalculator.Read()

	// Make sure we're connected to at least the minimum number of peers
	isConnected := connectedTo >= int(n.config.HealthConfig.MinConnectedPeers)
	healthy := isConnected
	details := map[string]interface{}{
		ConnectedPeersKey: connectedTo,
	}

	// Make sure we've received an incoming message within the threshold
	now := n.peerConfig.Clock.Time()

	lastMsgReceivedAt := time.Unix(atomic.LoadInt64(&n.peerConfig.LastReceived), 0)
	timeSinceLastMsgReceived := now.Sub(lastMsgReceivedAt)
	wasMsgReceivedRecently := timeSinceLastMsgReceived <= n.config.HealthConfig.MaxTimeSinceMsgReceived
	healthy = healthy && wasMsgReceivedRecently
	details[TimeSinceLastMsgReceivedKey] = timeSinceLastMsgReceived.String()
	n.metrics.timeSinceLastMsgReceived.Set(float64(timeSinceLastMsgReceived))

	// Make sure we've sent an outgoing message within the threshold
	lastMsgSentAt := time.Unix(atomic.LoadInt64(&n.peerConfig.LastSent), 0)
	timeSinceLastMsgSent := now.Sub(lastMsgSentAt)
	wasMsgSentRecently := timeSinceLastMsgSent <= n.config.HealthConfig.MaxTimeSinceMsgSent
	healthy = healthy && wasMsgSentRecently
	details[TimeSinceLastMsgSentKey] = timeSinceLastMsgSent.String()
	n.metrics.timeSinceLastMsgSent.Set(float64(timeSinceLastMsgSent))

	// Make sure the message send failed rate isn't too high
	isMsgFailRate := sendFailRate <= n.config.HealthConfig.MaxSendFailRate
	healthy = healthy && isMsgFailRate
	details[SendFailRateKey] = sendFailRate
	n.metrics.sendFailRate.Set(sendFailRate)

	// Network layer is unhealthy
	if !healthy {
		var errorReasons []string
		if !isConnected {
			errorReasons = append(errorReasons, fmt.Sprintf("not connected to a minimum of %d peer(s) only %d", n.config.HealthConfig.MinConnectedPeers, connectedTo))
		}
		if !wasMsgReceivedRecently {
			errorReasons = append(errorReasons, fmt.Sprintf("no messages from network received in %s > %s", timeSinceLastMsgReceived, n.config.HealthConfig.MaxTimeSinceMsgReceived))
		}
		if !wasMsgSentRecently {
			errorReasons = append(errorReasons, fmt.Sprintf("no messages from network sent in %s > %s", timeSinceLastMsgSent, n.config.HealthConfig.MaxTimeSinceMsgSent))
		}
		if !isMsgFailRate {
			errorReasons = append(errorReasons, fmt.Sprintf("messages failure send rate %g > %g", sendFailRate, n.config.HealthConfig.MaxSendFailRate))
		}

		return details, fmt.Errorf("network layer is unhealthy reason: %s", strings.Join(errorReasons, ", "))
	}
	return details, nil
}

// Connected is called after the peer finishes the handshake.
// Will not be called after [Disconnected] is called with this peer.
func (n *network) Connected(nodeID ids.NodeID) {
	n.peersLock.Lock()
	peer, ok := n.connectingPeers.GetByID(nodeID)
	if !ok {
		n.peerConfig.Log.Error(
			"unexpectedly connected to %s when not marked as attempting to connect",
			nodeID,
		)
		n.peersLock.Unlock()
		return
	}

	tracked, ok := n.trackedIPs[nodeID]
	if ok {
		tracked.stopTracking()
		delete(n.trackedIPs, nodeID)
	}
	n.connectingPeers.Remove(nodeID)
	n.connectedPeers.Add(peer)
	n.peersLock.Unlock()

	n.metrics.markConnected(peer)

	peerVersion := peer.Version()
	n.router.Connected(nodeID, peerVersion, constants.PrimaryNetworkID)
	for subnetID := range peer.TrackedSubnets() {
		n.router.Connected(nodeID, peerVersion, subnetID)
	}
}

// AllowConnection returns true if this node should have a connection to the
// provided nodeID. If the node is attempting to connect to the minimum number
// of peers, then it should only connect if this node is a validator, or the
// peer is a validator/beacon.
func (n *network) AllowConnection(nodeID ids.NodeID) bool {
	return !n.config.RequireValidatorToConnect ||
		n.config.Validators.Contains(constants.PrimaryNetworkID, n.config.MyNodeID) ||
		n.WantsConnection(nodeID)
}

func (n *network) Track(claimedIPPort ips.ClaimedIPPort) bool {
	nodeID := ids.NodeIDFromCert(claimedIPPort.Cert)

	// Verify that we do want to attempt to make a connection to this peer
	// before verifying that the IP has been correctly signed.
	//
	// This check only improves performance, as the values are recalculated once
	// the lock is grabbed before actually attempting to connect to the peer.
	if !n.shouldTrack(nodeID, claimedIPPort) {
		return false
	}

	signedIP := peer.SignedIP{
		IP: peer.UnsignedIP{
			IP:        claimedIPPort.IPPort,
			Timestamp: claimedIPPort.Timestamp,
		},
		Signature: claimedIPPort.Signature,
	}

	if err := signedIP.Verify(claimedIPPort.Cert); err != nil {
		n.peerConfig.Log.Debug("signature verification failed for %s: %s", nodeID, err)
		return false
	}

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	if _, connected := n.connectedPeers.GetByID(nodeID); connected {
		// If I'm currently connected to [nodeID] then they will have told me
		// how to connect to them in the future, and I don't need to attempt to
		// connect to them now.
		return false
	}

	tracked, isTracked := n.trackedIPs[nodeID]
	switch {
	case isTracked:
		if tracked.ip.Timestamp >= claimedIPPort.Timestamp {
			return false
		}
		// Stop tracking the old IP and instead start tracking new one.
		tracked := tracked.trackNewIP(&peer.UnsignedIP{
			IP:        claimedIPPort.IPPort,
			Timestamp: claimedIPPort.Timestamp,
		})
		n.trackedIPs[nodeID] = tracked
		n.dial(n.onCloseCtx, nodeID, tracked)
		return true
	case n.wantsConnection(nodeID):
		tracked := newTrackedIP(&peer.UnsignedIP{
			IP:        claimedIPPort.IPPort,
			Timestamp: claimedIPPort.Timestamp,
		})
		n.trackedIPs[nodeID] = tracked
		n.dial(n.onCloseCtx, nodeID, tracked)
		return true
	default:
		// This node isn't tracked and we don't want to connect to it.
		return false
	}
}

// Disconnected is called after the peer's handling has been shutdown.
// It is not guaranteed that [Connected] was previously called with [nodeID].
// It is guaranteed that [Connected] will not be called with [nodeID] after this
// call. Note that this is from the perspective of a single peer object, because
// a peer with the same ID can reconnect to this network instance.
func (n *network) Disconnected(nodeID ids.NodeID) {
	n.peersLock.RLock()
	_, connecting := n.connectingPeers.GetByID(nodeID)
	peer, connected := n.connectedPeers.GetByID(nodeID)
	n.peersLock.RUnlock()

	if connecting {
		n.disconnectedFromConnecting(nodeID)
	}
	if connected {
		n.disconnectedFromConnected(peer, nodeID)
	}
}

func (n *network) Version() (message.OutboundMessage, error) {
	mySignedIP, err := n.ipSigner.getSignedIP()
	if err != nil {
		return nil, err
	}
	return n.peerConfig.MessageCreator.Version(
		n.peerConfig.NetworkID,
		n.peerConfig.Clock.Unix(),
		mySignedIP.IP.IP,
		n.peerConfig.VersionCompatibility.Version().String(),
		mySignedIP.IP.Timestamp,
		mySignedIP.Signature,
		n.peerConfig.MySubnets.List(),
	)
}

func (n *network) Peers() (message.OutboundMessage, error) {
	peers := n.sampleValidatorIPs()
	return n.peerConfig.MessageCreator.PeerList(peers, true)
}

func (n *network) Pong(nodeID ids.NodeID) (message.OutboundMessage, error) {
	uptimePercentFloat, err := n.config.UptimeCalculator.CalculateUptimePercent(nodeID)
	if err != nil {
		uptimePercentFloat = 0
	}

	uptimePercentInt := uint8(uptimePercentFloat * 100)
	return n.peerConfig.MessageCreator.Pong(uptimePercentInt)
}

// Dispatch starts accepting connections from other nodes attempting to connect
// to this node.
func (n *network) Dispatch() error {
	go n.runTimers() // Periodically perform operations
	go n.inboundConnUpgradeThrottler.Dispatch()
	errs := wrappers.Errs{}
	for { // Continuously accept new connections
		conn, err := n.listener.Accept() // Returns error when n.Close() is called
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				// Sleep for a small amount of time to try to wait for the
				// temporary error to go away.
				time.Sleep(time.Millisecond)
				continue
			}

			n.peerConfig.Log.Debug("error during server accept: %s", err)
			break
		}

		// We pessimistically drop an incoming connection if the remote
		// address is found in connectedIPs, myIPs, or peerAliasIPs.
		// This protects our node from spending CPU cycles on TLS
		// handshakes to upgrade connections from existing peers.
		// Specifically, this can occur when one of our existing
		// peers attempts to connect to one our IP aliases (that they
		// aren't yet aware is an alias).
		remoteAddr := conn.RemoteAddr().String()
		ip, err := ips.ToIPPort(remoteAddr)
		if err != nil {
			errs.Add(fmt.Errorf("unable to convert remote address %s to IP: %w", remoteAddr, err))
			break
		}

		if !n.inboundConnUpgradeThrottler.ShouldUpgrade(ip) {
			n.peerConfig.Log.Debug(
				"not upgrading connection to %s due to rate-limiting",
				ip,
			)
			n.metrics.inboundConnRateLimited.Inc()
			_ = conn.Close()
			continue
		}
		n.metrics.inboundConnAllowed.Inc()

		go func() {
			if err := n.upgrade(conn, n.serverUpgrader); err != nil {
				n.peerConfig.Log.Verbo("failed to upgrade inbound connection: %s", err)
			}
		}()
	}
	n.inboundConnUpgradeThrottler.Stop()
	n.StartClose()

	n.peersLock.RLock()
	connecting := n.connectingPeers.Sample(n.connectingPeers.Len(), peer.NoPrecondition)
	connected := n.connectedPeers.Sample(n.connectedPeers.Len(), peer.NoPrecondition)
	n.peersLock.RUnlock()

	for _, peer := range append(connecting, connected...) {
		errs.Add(peer.AwaitClosed(context.TODO()))
	}
	return errs.Err
}

func (n *network) WantsConnection(nodeID ids.NodeID) bool {
	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	return n.wantsConnection(nodeID)
}

func (n *network) wantsConnection(nodeID ids.NodeID) bool {
	return n.config.Validators.Contains(constants.PrimaryNetworkID, nodeID) ||
		n.manuallyTrackedIDs.Contains(nodeID)
}

func (n *network) ManuallyTrack(nodeID ids.NodeID, ip ips.IPPort) {
	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	n.manuallyTrackedIDs.Add(nodeID)

	_, connected := n.connectedPeers.GetByID(nodeID)
	if connected {
		// If I'm currently connected to [nodeID] then they will have told me
		// how to connect to them in the future, and I don't need to attempt to
		// connect to them now.
		return
	}

	_, isTracked := n.trackedIPs[nodeID]
	if !isTracked {
		tracked := newTrackedIP(&peer.UnsignedIP{
			IP:        ip,
			Timestamp: 0,
		})
		n.trackedIPs[nodeID] = tracked
		n.dial(n.onCloseCtx, nodeID, tracked)
	}
}

func (n *network) TracksSubnet(nodeID ids.NodeID, subnetID ids.ID) bool {
	if n.config.MyNodeID == nodeID {
		return subnetID == constants.PrimaryNetworkID || n.config.WhitelistedSubnets.Contains(subnetID)
	}

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	peer, connected := n.connectedPeers.GetByID(nodeID)
	if !connected {
		return false
	}
	trackedSubnets := peer.TrackedSubnets()
	return subnetID == constants.PrimaryNetworkID || trackedSubnets.Contains(subnetID)
}

func (n *network) sampleValidatorIPs() []ips.ClaimedIPPort {
	n.peersLock.RLock()
	peers := n.connectedPeers.Sample(
		int(n.config.PeerListNumValidatorIPs),
		func(p peer.Peer) bool {
			// Only sample validators
			return n.config.Validators.Contains(constants.PrimaryNetworkID, p.ID())
		},
	)
	n.peersLock.RUnlock()

	sampledIPs := make([]ips.ClaimedIPPort, len(peers))
	for i, peer := range peers {
		peerIP := peer.IP()
		sampledIPs[i] = ips.ClaimedIPPort{
			Cert:      peer.Cert(),
			IPPort:    peerIP.IP.IP,
			Timestamp: peerIP.IP.Timestamp,
			Signature: peerIP.Signature,
		}
	}
	return sampledIPs
}

// getPeers returns a slice of connected peers from a set of [nodeIDs].
//
// - [nodeIDs] the IDs of the peers that should be returned if they are
//   connected.
// - [subnetID] the subnetID whose membership should be considered if
//   [validatorOnly] is set to true.
// - [validatorOnly] is the flag to drop any nodes from [nodeIDs] that are not
//   validators in [subnetID].
func (n *network) getPeers(
	nodeIDs ids.NodeIDSet,
	subnetID ids.ID,
	validatorOnly bool,
) []peer.Peer {
	peers := make([]peer.Peer, 0, nodeIDs.Len())

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	for nodeID := range nodeIDs {
		peer, ok := n.connectedPeers.GetByID(nodeID)
		if !ok {
			continue
		}

		trackedSubnets := peer.TrackedSubnets()
		if subnetID != constants.PrimaryNetworkID && !trackedSubnets.Contains(subnetID) {
			continue
		}

		if validatorOnly && !n.config.Validators.Contains(subnetID, nodeID) {
			continue
		}

		peers = append(peers, peer)
	}

	return peers
}

func (n *network) samplePeers(
	subnetID ids.ID,
	validatorOnly bool,
	numValidatorsToSample,
	numNonValidatorsToSample int,
	numPeersToSample int,
) []peer.Peer {
	if validatorOnly {
		numValidatorsToSample += numNonValidatorsToSample + numPeersToSample
		numNonValidatorsToSample = 0
		numPeersToSample = 0
	}

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	return n.connectedPeers.Sample(
		numValidatorsToSample+numNonValidatorsToSample+numPeersToSample,
		func(p peer.Peer) bool {
			// Only return peers that are tracking [subnetID]
			trackedSubnets := p.TrackedSubnets()
			if subnetID != constants.PrimaryNetworkID && !trackedSubnets.Contains(subnetID) {
				return false
			}

			if numPeersToSample > 0 {
				numPeersToSample--
				return true
			}

			if n.config.Validators.Contains(subnetID, p.ID()) {
				numValidatorsToSample--
				return numValidatorsToSample >= 0
			}

			numNonValidatorsToSample--
			return numNonValidatorsToSample >= 0
		},
	)
}

// send the message to the provided peers.
//
// send takes ownership of the provided message reference. So, the provided
// message should only be inspected if the reference has been externally
// increased.
func (n *network) send(msg message.OutboundMessage, peers []peer.Peer) ids.NodeIDSet {
	sentTo := ids.NewNodeIDSet(len(peers))
	now := n.peerConfig.Clock.Time()

	// send to peer and update metrics
	for _, peer := range peers {
		// Add a reference to the message so that if it is sent, it won't be
		// collected until it is done being processed.
		msg.AddRef()
		if peer.Send(n.onCloseCtx, msg) {
			sentTo.Add(peer.ID())

			// TODO: move send fail rate calculations into the peer metrics
			// record metrics for success
			n.sendFailRateCalculator.Observe(0, now)
		} else {
			// record metrics for failure
			n.sendFailRateCalculator.Observe(1, now)
		}
	}

	// The message has been passed to all peers that it will be sent to, so we
	// can decrease the sender reference now.
	msg.DecRef()
	return sentTo
}

func (n *network) disconnectedFromConnecting(nodeID ids.NodeID) {
	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	n.connectingPeers.Remove(nodeID)

	// The peer that is disconnecting from us didn't finish the handshake
	tracked, ok := n.trackedIPs[nodeID]
	if ok {
		if n.wantsConnection(nodeID) {
			tracked := tracked.trackNewIP(tracked.ip)
			n.trackedIPs[nodeID] = tracked
			n.dial(n.onCloseCtx, nodeID, tracked)
		} else {
			tracked.stopTracking()
			delete(n.trackedIPs, nodeID)
		}
	}

	n.metrics.disconnected.Inc()
}

func (n *network) disconnectedFromConnected(peer peer.Peer, nodeID ids.NodeID) {
	n.router.Disconnected(nodeID)

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	n.connectedPeers.Remove(nodeID)

	// The peer that is disconnecting from us finished the handshake
	if n.wantsConnection(nodeID) {
		tracked := newTrackedIP(&peer.IP().IP)
		n.trackedIPs[nodeID] = tracked
		n.dial(n.onCloseCtx, nodeID, tracked)
	} else {
		delete(n.trackedIPs, nodeID)
	}

	n.metrics.markDisconnected(peer)
}

func (n *network) shouldTrack(nodeID ids.NodeID, ip ips.ClaimedIPPort) bool {
	if !n.config.AllowPrivateIPs && ip.IPPort.IP.IsPrivate() {
		n.peerConfig.Log.Verbo(
			"dropping suggested connected to %s because the ip (%s) is private",
			nodeID, ip.IPPort,
		)
		return false
	}

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	_, connected := n.connectedPeers.GetByID(nodeID)
	if connected {
		// If I'm currently connected to [nodeID] then they will have told me
		// how to connect to them in the future, and I don't need to attempt to
		// connect to them now.
		return false
	}

	tracked, isTracked := n.trackedIPs[nodeID]
	if isTracked {
		return tracked.ip.Timestamp < ip.Timestamp
	}
	return n.wantsConnection(nodeID)
}

// dial will spin up a new goroutine and attempt to establish a connection with
// [nodeID] at [ip].
//
// If the connection established at [ip] doesn't match [nodeID]:
// - attempts to reach [nodeID] at [ip] will be halted.
// - the connection will be checked to see if the connection is desired or not.
//
// If [ip] has been flagged with [ip.stopTracking] then this goroutine will
// exit.
//
// If [nodeID] is marked as connecting or connected then this goroutine will
// exit.
//
// If [nodeID] is no longer marked as desired then this goroutine will exit and
// the entry in the [trackedIP]s set will be removed.
//
// If initiating a connection to [ip] fails, then dial will reattempt. However,
// there is a randomized exponential backoff to avoid spamming connection
// attempts.
func (n *network) dial(ctx context.Context, nodeID ids.NodeID, ip *trackedIP) {
	go func() {
		n.metrics.numTracked.Inc()
		defer n.metrics.numTracked.Dec()

		for {
			timer := time.NewTimer(ip.getDelay())

			select {
			case <-ip.onStopTracking:
				timer.Stop()
				return
			case <-timer.C:
			}

			n.peersLock.Lock()
			if !n.wantsConnection(nodeID) {
				// Typically [n.trackedIPs[nodeID]] will already equal [ip], but
				// the reference to [ip] is refreshed to avoid any potential
				// race conditions before removing the entry.
				if ip, exists := n.trackedIPs[nodeID]; exists {
					ip.stopTracking()
					delete(n.trackedIPs, nodeID)
				}
				n.peersLock.Unlock()
				return
			}
			_, connecting := n.connectingPeers.GetByID(nodeID)
			_, connected := n.connectedPeers.GetByID(nodeID)
			n.peersLock.Unlock()

			// While it may not be strictly needed to stop attempting to connect
			// to an already connected peer here. It does prevent unnecessary
			// outbound connections. Additionally, because the peer would
			// immediately drop a duplicated connection, this prevents any
			// "connection reset by peer" errors from interfering with the
			// later duplicated connection check.
			if connecting || connected {
				n.peerConfig.Log.Verbo(
					"exiting attempt to dial %s as we are already connected", nodeID,
				)
				return
			}

			// Increase the delay that we will use for a future connection
			// attempt.
			ip.increaseDelay(
				n.config.InitialReconnectDelay,
				n.config.MaxReconnectDelay,
			)

			conn, err := n.dialer.Dial(ctx, ip.ip.IP)
			if err != nil {
				n.peerConfig.Log.Verbo(
					"failed to reach %s, attempting again in %s",
					ip.ip,
					ip.delay,
				)
				continue
			}

			err = n.upgrade(conn, n.clientUpgrader)
			if err != nil {
				n.peerConfig.Log.Verbo(
					"failed to upgrade %s, attempting again in %s",
					ip.ip,
					ip.delay,
				)
				continue
			}
			return
		}
	}()
}

// upgrade the provided connection, which may be an inbound connection or an
// outbound connection, with the provided [upgrader].
//
// If the connection is successfully upgraded, [nil] will be returned.
//
// If the connection is desired by the node, then the resulting upgraded
// connection will be used to create a new peer. Otherwise the connection will
// be immediately closed.
func (n *network) upgrade(conn net.Conn, upgrader peer.Upgrader) error {
	if conn, ok := conn.(*net.TCPConn); ok {
		// If a connection is closed, we shouldn't bother keeping any messages
		// in memory.
		if err := conn.SetLinger(0); err != nil {
			n.peerConfig.Log.Warn("failed to set no linger due to: %s", err)
		}
	}

	upgradeTimeout := n.peerConfig.Clock.Time().Add(n.config.ReadHandshakeTimeout)
	if err := conn.SetReadDeadline(upgradeTimeout); err != nil {
		_ = conn.Close()
		n.peerConfig.Log.Verbo("failed to set the read deadline with %s", err)
		return err
	}

	nodeID, tlsConn, cert, err := upgrader.Upgrade(conn)
	if err != nil {
		_ = conn.Close()
		n.peerConfig.Log.Verbo("failed to upgrade connection with %s", err)
		return err
	}

	if err := tlsConn.SetReadDeadline(time.Time{}); err != nil {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo("failed to clear the read deadline with %s", err)
		return err
	}

	// At this point we have successfully upgraded the connection and will
	// return a nil error.

	if nodeID == n.config.MyNodeID {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo("dropping connection to myself")
		return nil
	}

	if !n.AllowConnection(nodeID) {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping undesired connection to %s", nodeID,
		)
		return nil
	}

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	if n.closing {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping connection to %s because we are shutting down the p2p network",
			nodeID,
		)
		return nil
	}

	if _, connecting := n.connectingPeers.GetByID(nodeID); connecting {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping duplicate connection to %s because we are already connecting to it",
			nodeID,
		)
		return nil
	}

	if _, connected := n.connectedPeers.GetByID(nodeID); connected {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping duplicate connection to %s because we are already connected to it",
			nodeID,
		)
		return nil
	}

	n.peerConfig.Log.Verbo("starting handshake with %s", nodeID)

	// peer.Start requires there is only ever one peer instance running with the
	// same [peerConfig.InboundMsgThrottler]. This is guaranteed by the above
	// de-duplications for [connectingPeers] and [connectedPeers].
	peer := peer.Start(
		n.peerConfig,
		tlsConn,
		cert,
		nodeID,
		peer.NewThrottledMessageQueue(
			n.peerConfig.Metrics,
			nodeID,
			n.peerConfig.Log,
			n.outboundMsgThrottler,
		),
	)
	n.connectingPeers.Add(peer)
	return nil
}

func (n *network) PeerInfo(nodeIDs []ids.NodeID) []peer.Info {
	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	if len(nodeIDs) == 0 {
		return n.connectedPeers.AllInfo()
	}
	return n.connectedPeers.Info(nodeIDs)
}

func (n *network) StartClose() {
	n.closeOnce.Do(func() {
		n.peerConfig.Log.Info("shutting down the p2p networking")

		if err := n.listener.Close(); err != nil {
			n.peerConfig.Log.Debug("closing the network listener failed with: %s", err)
		}

		n.peersLock.Lock()
		defer n.peersLock.Unlock()

		n.closing = true
		n.onCloseCtxCancel()

		for nodeID, tracked := range n.trackedIPs {
			tracked.stopTracking()
			delete(n.trackedIPs, nodeID)
		}

		for i := 0; i < n.connectingPeers.Len(); i++ {
			peer, _ := n.connectingPeers.GetByIndex(i)
			peer.StartClose()
		}

		for i := 0; i < n.connectedPeers.Len(); i++ {
			peer, _ := n.connectedPeers.GetByIndex(i)
			peer.StartClose()
		}
	})
}

func (n *network) NodeUptime() (UptimeResult, bool) {
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

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	for i := 0; i < n.connectedPeers.Len(); i++ {
		peer, _ := n.connectedPeers.GetByIndex(i)

		nodeID := peer.ID()
		weight, ok := primaryValidators.GetWeight(nodeID)
		if !ok {
			// this is not a validator skip it.
			continue
		}

		observedUptime := peer.ObservedUptime()
		percent := float64(observedUptime)
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

func (n *network) runTimers() {
	gossipPeerlists := time.NewTicker(n.config.PeerListGossipFreq)
	updateUptimes := time.NewTicker(n.config.UptimeMetricFreq)
	defer func() {
		gossipPeerlists.Stop()
		updateUptimes.Stop()
	}()

	for {
		select {
		case <-n.onCloseCtx.Done():
			return
		case <-gossipPeerlists.C:
			validatorIPs := n.sampleValidatorIPs()
			if len(validatorIPs) == 0 {
				n.peerConfig.Log.Debug("skipping validator IP gossiping as no IPs are connected")
				continue
			}

			msg, err := n.peerConfig.MessageCreator.PeerList(validatorIPs, false)
			if err != nil {
				n.peerConfig.Log.Error(
					"failed to gossip %d ips: %s",
					len(validatorIPs),
					err,
				)
				continue
			}

			n.Gossip(
				msg,
				constants.PrimaryNetworkID,
				false,
				int(n.config.PeerListValidatorGossipSize),
				int(n.config.PeerListNonValidatorGossipSize),
				int(n.config.PeerListPeersGossipSize),
			)

		case <-updateUptimes.C:

			result, _ := n.NodeUptime()
			n.metrics.nodeUptimeWeightedAverage.Set(result.WeightedAveragePercentage)
			n.metrics.nodeUptimeRewardingStake.Set(result.RewardingStakePercentage)
		}
	}
}
