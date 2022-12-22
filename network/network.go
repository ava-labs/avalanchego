// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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

	"go.uber.org/zap"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

const (
	ConnectedPeersKey           = "connectedPeers"
	TimeSinceLastMsgReceivedKey = "timeSinceLastMsgReceived"
	TimeSinceLastMsgSentKey     = "timeSinceLastMsgSent"
	SendFailRateKey             = "sendFailRate"
)

var (
	_ sender.ExternalSender = (*network)(nil)
	_ Network               = (*network)(nil)

	errMissingPrimaryValidators = errors.New("missing primary validator set")
	errNotValidator             = errors.New("node is not a validator")
	errNotWhiteListed           = errors.New("subnet is not whitelisted")
	errSubnetNotExist           = errors.New("subnet does not exist")
)

// Network defines the functionality of the networking library.
type Network interface {
	// All consensus messages can be sent through this interface. Thread safety
	// must be managed internally in the network.
	sender.ExternalSender

	// Has a health check
	health.Checker

	peer.Network

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

	// NodeUptime returns given node's [subnetID] UptimeResults in the view of
	// this node's peer validators.
	NodeUptime(subnetID ids.ID) (UptimeResult, error)
}

type UptimeResult struct {
	// RewardingStakePercentage shows what percent of network stake thinks we're
	// above the uptime requirement.
	RewardingStakePercentage float64

	// WeightedAveragePercentage is the average perceived uptime of this node,
	// weighted by stake.
	// Note that this is different from RewardingStakePercentage, which shows
	// the percent of the network stake that thinks this node is above the
	// uptime requirement. WeightedAveragePercentage is weighted by uptime.
	// i.e If uptime requirement is 85 and a peer reports 40 percent it will be
	// counted (40*weight) in WeightedAveragePercentage but not in
	// RewardingStakePercentage since 40 < 85
	WeightedAveragePercentage float64
}

type network struct {
	config     *Config
	peerConfig *peer.Config
	metrics    *metrics

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

	// Tracks which peers know about which peers
	gossipTracker peer.GossipTracker
	peersLock     sync.RWMutex
	// peerIPs contains the most up to date set of signed IPs for nodes we are
	// currently connected or attempting to connect to.
	// Note: The txID provided inside of a claimed IP is not verified and should
	//       not be accessed from this map.
	peerIPs map[ids.NodeID]*ips.ClaimedIPPort
	// trackedIPs contains the set of IPs that we are currently attempting to
	// connect to. An entry is added to this set when we first start attempting
	// to connect to the peer. An entry is deleted from this set once we have
	// finished the handshake.
	trackedIPs         map[ids.NodeID]*trackedIP
	manuallyTrackedIDs set.Set[ids.NodeID]
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
) (Network, error) {
	primaryNetworkValidators, ok := config.Validators.Get(constants.PrimaryNetworkID)
	if !ok {
		return nil, errMissingPrimaryValidators
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

	peerConfig := &peer.Config{
		ReadBufferSize:  config.PeerReadBufferSize,
		WriteBufferSize: config.PeerWriteBufferSize,
		Metrics:         peerMetrics,
		MessageCreator:  msgCreator,

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
		UptimeCalculator:     config.UptimeCalculator,
		IPSigner:             peer.NewIPSigner(config.MyIPPort, config.TLSKey),
	}

	onCloseCtx, cancel := context.WithCancel(context.Background())
	n := &network{
		config:               config,
		peerConfig:           peerConfig,
		metrics:              metrics,
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

		peerIPs:         make(map[ids.NodeID]*ips.ClaimedIPPort),
		trackedIPs:      make(map[ids.NodeID]*trackedIP),
		gossipTracker:   config.GossipTracker,
		connectingPeers: peer.NewSet(),
		connectedPeers:  peer.NewSet(),
		router:          router,
	}
	n.peerConfig.Network = n
	return n, nil
}

func (n *network) Send(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], subnetID ids.ID, validatorOnly bool) set.Set[ids.NodeID] {
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
) set.Set[ids.NodeID] {
	peers := n.samplePeers(subnetID, validatorOnly, numValidatorsToSend, numNonValidatorsToSend, numPeersToSend)
	return n.send(msg, peers)
}

// HealthCheck returns information about several network layer health checks.
// 1) Information about health check results
// 2) An error if the health check reports unhealthy
func (n *network) HealthCheck(context.Context) (interface{}, error) {
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
			"unexpectedly connected to peer when not marked as attempting to connect",
			zap.Stringer("nodeID", nodeID),
		)
		n.peersLock.Unlock()
		return
	}

	peerIP := peer.IP()
	newIP := &ips.ClaimedIPPort{
		Cert:      peer.Cert(),
		IPPort:    peerIP.IP.IP,
		Timestamp: peerIP.IP.Timestamp,
		Signature: peerIP.Signature,
	}
	prevIP, ok := n.peerIPs[nodeID]
	if !ok {
		// If the IP wasn't previously tracked, then we never could have
		// gossiped it. This means we don't need to reset the validator's
		// tracked set.
		n.peerIPs[nodeID] = newIP
	} else if prevIP.Timestamp < newIP.Timestamp {
		// The previous IP was stale, so we should gossip the newer IP.
		n.peerIPs[nodeID] = newIP

		if !prevIP.IPPort.Equal(newIP.IPPort) {
			// This IP is actually different, so we should gossip it.
			n.peerConfig.Log.Debug("resetting gossip due to ip change",
				zap.Stringer("nodeID", nodeID),
			)
			_ = n.gossipTracker.ResetValidator(nodeID)
		}
	}

	if tracked, ok := n.trackedIPs[nodeID]; ok {
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
		validators.Contains(n.config.Validators, constants.PrimaryNetworkID, n.config.MyNodeID) ||
		n.WantsConnection(nodeID)
}

func (n *network) Track(peerID ids.NodeID, claimedIPPorts []*ips.ClaimedIPPort) ([]*p2ppb.PeerAck, error) {
	// Perform all signature verification and hashing before grabbing the peer
	// lock.
	// Note: Avoiding signature verification when the IP isn't needed is a
	// **significant** performance optimization.
	// Note: To avoid signature verification when the IP isn't needed, we
	// optimistically filter out IPs. This can result in us not tracking an IP
	// that we otherwise would have. This case can only happen if the node
	// became a validator between the time we verified the signature and when we
	// processed the IP; which should be very rare.
	ipAuths, err := n.authenticateIPs(claimedIPPorts)
	if err != nil {
		n.peerConfig.Log.Debug("authenticating claimed IPs failed",
			zap.Stringer("nodeID", peerID),
			zap.Error(err),
		)
		return nil, err
	}

	// Information for them to update about us
	ipLen := len(claimedIPPorts)
	newestTimestamp := make(map[ids.ID]uint64, ipLen)
	// Information for us to update about them
	txIDsWithUpToDateIP := make([]ids.ID, 0, ipLen)

	// Atomically modify peer data
	n.peersLock.Lock()
	defer n.peersLock.Unlock()
	for i, ip := range claimedIPPorts {
		ipAuth := ipAuths[i]
		nodeID := ipAuth.nodeID
		// Invariant: [ip] is only used to modify local node state if
		// [verifiedIP] is true.
		// Note: modifying peer-level state is allowed regardless of
		// [verifiedIP].
		verifiedIP := ipAuth.verified

		// Re-fetch latest info for a [nodeID] in case it changed since we last
		// held [peersLock].
		prevIP, previouslyTracked, shouldUpdateOurIP, shouldDial := n.peerIPStatus(nodeID, ip)
		tracked, isTracked := n.trackedIPs[nodeID]

		// Evaluate if the gossiped IP is useful to us or to the peer that
		// shared it with us.
		switch {
		case previouslyTracked && prevIP.Timestamp > ip.Timestamp:
			// Our previous IP was more up to date. We should tell the peer
			// not to gossip their IP to us. We should still gossip our IP to
			// them.
			newestTimestamp[ip.TxID] = prevIP.Timestamp

			n.metrics.numUselessPeerListBytes.Add(float64(ip.BytesLen()))
		case previouslyTracked && prevIP.Timestamp == ip.Timestamp:
			// Our previous IP was equally fresh. We should tell the peer
			// not to gossip this IP to us. We should not gossip our IP to them.
			newestTimestamp[ip.TxID] = prevIP.Timestamp
			txIDsWithUpToDateIP = append(txIDsWithUpToDateIP, ip.TxID)

			n.metrics.numUselessPeerListBytes.Add(float64(ip.BytesLen()))
		case verifiedIP && shouldUpdateOurIP:
			// This IP is more up to date. We should tell the peer not to gossip
			// this IP to us. We should not gossip our IP to them.
			newestTimestamp[ip.TxID] = ip.Timestamp
			txIDsWithUpToDateIP = append(txIDsWithUpToDateIP, ip.TxID)

			// In the future, we should gossip this IP rather than the old IP.
			n.peerIPs[nodeID] = ip

			// If the new IP is equal to the old IP, there is no reason to
			// refresh the references to it. This can happen when a node
			// restarts but does not change their IP.
			if prevIP.IPPort.Equal(ip.IPPort) {
				continue
			}

			// We should gossip this new IP to all our peers.
			n.peerConfig.Log.Debug("resetting gossip due to ip change",
				zap.Stringer("nodeID", nodeID),
			)
			_ = n.gossipTracker.ResetValidator(nodeID)

			// We should update any existing outbound connection attempts.
			if isTracked {
				// Stop tracking the old IP and start tracking the new one.
				tracked := tracked.trackNewIP(ip.IPPort)
				n.trackedIPs[nodeID] = tracked
				n.dial(n.onCloseCtx, nodeID, tracked)
			}
		case verifiedIP && shouldDial:
			// Invariant: [isTracked] is false here.

			// This is the first we've heard of this IP and we want to connect
			// to it. We should tell the peer not to gossip this IP to us again.
			newestTimestamp[ip.TxID] = ip.Timestamp
			// We should not gossip this IP back to them.
			txIDsWithUpToDateIP = append(txIDsWithUpToDateIP, ip.TxID)

			// We don't need to reset gossip about this validator because
			// we've never gossiped it before.
			n.peerIPs[nodeID] = ip

			tracked := newTrackedIP(ip.IPPort)
			n.trackedIPs[nodeID] = tracked
			n.dial(n.onCloseCtx, nodeID, tracked)
		default:
			// This IP isn't desired
			n.metrics.numUselessPeerListBytes.Add(float64(ip.BytesLen()))
		}
	}

	txIDsToAck := maps.Keys(newestTimestamp)
	txIDsToAck, ok := n.gossipTracker.AddKnown(peerID, txIDsWithUpToDateIP, txIDsToAck)
	if !ok {
		n.peerConfig.Log.Error("failed to update known peers",
			zap.Stringer("nodeID", peerID),
		)
		return nil, nil
	}

	peerAcks := make([]*p2ppb.PeerAck, len(txIDsToAck))
	for i, txID := range txIDsToAck {
		txID := txID
		peerAcks[i] = &p2ppb.PeerAck{
			TxId: txID[:],
			// By responding with the highest timestamp, not just the timestamp
			// the peer provided us, we may be able to avoid some unnecessary
			// gossip in the case that the peer is about to update this
			// validator's IP.
			Timestamp: newestTimestamp[txID],
		}
	}
	return peerAcks, nil
}

func (n *network) MarkTracked(peerID ids.NodeID, ips []*p2ppb.PeerAck) error {
	txIDs := make([]ids.ID, 0, len(ips))

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	for _, ip := range ips {
		txID, err := ids.ToID(ip.TxId)
		if err != nil {
			return err
		}

		// If [txID]'s corresponding nodeID isn't known, then they must no
		// longer be a validator. Therefore we wouldn't gossip their IP anyways.
		nodeID, ok := n.gossipTracker.GetNodeID(txID)
		if !ok {
			continue
		}

		// If the peer returns a lower timestamp than I currently have, then I
		// have updated the IP since I sent the PeerList message this is in
		// response to. That means that I should re-gossip this node's IP to the
		// peer.
		myIP, previouslyTracked := n.peerIPs[nodeID]
		if previouslyTracked && myIP.Timestamp <= ip.Timestamp {
			txIDs = append(txIDs, txID)
		}
	}

	if _, ok := n.gossipTracker.AddKnown(peerID, txIDs, nil); !ok {
		n.peerConfig.Log.Error("failed to update known peers",
			zap.Stringer("nodeID", peerID),
		)
	}
	return nil
}

// Disconnected is called after the peer's handling has been shutdown.
// It is not guaranteed that [Connected] was previously called with [nodeID].
// It is guaranteed that [Connected] will not be called with [nodeID] after this
// call. Note that this is from the perspective of a single peer object, because
// a peer with the same ID can reconnect to this network instance.
func (n *network) Disconnected(nodeID ids.NodeID) {
	if !n.gossipTracker.StopTrackingPeer(nodeID) {
		n.peerConfig.Log.Error(
			"stopped non-existent peer tracker",
			zap.Stringer("nodeID", nodeID),
		)
	}

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

func (n *network) Peers(peerID ids.NodeID) ([]ips.ClaimedIPPort, error) {
	// Only select validators that we haven't already sent to this peer
	unknownValidators, ok := n.gossipTracker.GetUnknown(peerID)
	if !ok {
		n.peerConfig.Log.Debug(
			"unable to find peer to gossip to",
			zap.Stringer("nodeID", peerID),
		)
		return nil, nil
	}

	// We select a random sample of validators to gossip to avoid starving out a
	// validator from being gossiped for an extended period of time.
	s := sampler.NewUniform()
	if err := s.Initialize(uint64(len(unknownValidators))); err != nil {
		return nil, err
	}

	// Calculate the unknown information we need to send to this peer.
	validatorIPs := make([]ips.ClaimedIPPort, 0, int(n.config.PeerListNumValidatorIPs))
	for i := 0; i < len(unknownValidators) && len(validatorIPs) < int(n.config.PeerListNumValidatorIPs); i++ {
		drawn, err := s.Next()
		if err != nil {
			return nil, err
		}

		validator := unknownValidators[drawn]
		n.peersLock.RLock()
		_, isConnected := n.connectedPeers.GetByID(validator.NodeID)
		peerIP := n.peerIPs[validator.NodeID]
		n.peersLock.RUnlock()
		if !isConnected {
			n.peerConfig.Log.Debug(
				"unable to find validator in connected peers",
				zap.Stringer("nodeID", validator.NodeID),
			)
			continue
		}

		// Note: peerIP isn't used directly here because the TxID may be
		//       incorrect.
		validatorIPs = append(validatorIPs,
			ips.ClaimedIPPort{
				Cert:      peerIP.Cert,
				IPPort:    peerIP.IPPort,
				Timestamp: peerIP.Timestamp,
				Signature: peerIP.Signature,
				TxID:      validator.TxID,
			},
		)
	}

	return validatorIPs, nil
}

// Dispatch starts accepting connections from other nodes attempting to connect
// to this node.
func (n *network) Dispatch() error {
	go n.runTimers() // Periodically perform operations
	go n.inboundConnUpgradeThrottler.Dispatch()
	errs := wrappers.Errs{}
	for { // Continuously accept new connections
		if n.onCloseCtx.Err() != nil {
			break
		}

		conn, err := n.listener.Accept() // Returns error when n.Close() is called
		if err != nil {
			n.peerConfig.Log.Debug("error during server accept", zap.Error(err))
			// Sleep for a small amount of time to try to wait for the
			// error to go away.
			time.Sleep(time.Millisecond)
			n.metrics.acceptFailed.Inc()
			continue
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
			n.peerConfig.Log.Debug("failed to upgrade connection",
				zap.String("reason", "rate-limiting"),
				zap.Stringer("peerIP", ip),
			)
			n.metrics.inboundConnRateLimited.Inc()
			_ = conn.Close()
			continue
		}
		n.metrics.inboundConnAllowed.Inc()

		go func() {
			if err := n.upgrade(conn, n.serverUpgrader); err != nil {
				n.peerConfig.Log.Verbo("failed to upgrade inbound connection",
					zap.Error(err),
				)
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
	return validators.Contains(n.config.Validators, constants.PrimaryNetworkID, nodeID) ||
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
		tracked := newTrackedIP(ip)
		n.trackedIPs[nodeID] = tracked
		n.dial(n.onCloseCtx, nodeID, tracked)
	}
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
	nodeIDs set.Set[ids.NodeID],
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

		if validatorOnly && !validators.Contains(n.config.Validators, subnetID, nodeID) {
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

			if validators.Contains(n.config.Validators, subnetID, p.ID()) {
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
func (n *network) send(msg message.OutboundMessage, peers []peer.Peer) set.Set[ids.NodeID] {
	sentTo := set.NewSet[ids.NodeID](len(peers))
	now := n.peerConfig.Clock.Time()

	// send to peer and update metrics
	for _, peer := range peers {
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
			delete(n.peerIPs, nodeID)
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
		prevIP := n.peerIPs[nodeID]
		tracked := newTrackedIP(prevIP.IPPort)
		n.trackedIPs[nodeID] = tracked
		n.dial(n.onCloseCtx, nodeID, tracked)
	} else {
		delete(n.peerIPs, nodeID)
	}

	n.metrics.markDisconnected(peer)
}

// ipAuth is a helper struct used to convey information about an
// [*ips.ClaimedIPPort].
type ipAuth struct {
	nodeID   ids.NodeID
	verified bool
}

func (n *network) authenticateIPs(ips []*ips.ClaimedIPPort) ([]*ipAuth, error) {
	ipAuths := make([]*ipAuth, len(ips))
	for i, ip := range ips {
		nodeID := ids.NodeIDFromCert(ip.Cert)
		n.peersLock.RLock()
		_, _, shouldUpdateOurIP, shouldDial := n.peerIPStatus(nodeID, ip)
		n.peersLock.RUnlock()
		if !shouldUpdateOurIP && !shouldDial {
			ipAuths[i] = &ipAuth{
				nodeID: nodeID,
			}
			continue
		}

		// Verify signature if needed
		signedIP := peer.SignedIP{
			IP: peer.UnsignedIP{
				IP:        ip.IPPort,
				Timestamp: ip.Timestamp,
			},
			Signature: ip.Signature,
		}
		if err := signedIP.Verify(ip.Cert); err != nil {
			return nil, err
		}
		ipAuths[i] = &ipAuth{
			nodeID:   nodeID,
			verified: true,
		}
	}
	return ipAuths, nil
}

// peerIPStatus assumes the caller holds [peersLock]
func (n *network) peerIPStatus(nodeID ids.NodeID, ip *ips.ClaimedIPPort) (*ips.ClaimedIPPort, bool, bool, bool) {
	prevIP, previouslyTracked := n.peerIPs[nodeID]
	_, connected := n.connectedPeers.GetByID(nodeID)
	shouldUpdateOurIP := previouslyTracked && prevIP.Timestamp < ip.Timestamp
	shouldDial := !previouslyTracked && !connected && n.wantsConnection(nodeID)
	return prevIP, previouslyTracked, shouldUpdateOurIP, shouldDial
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
					delete(n.peerIPs, nodeID)
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
					"exiting attempt to dial peer",
					zap.String("reason", "already connected"),
					zap.Stringer("nodeID", nodeID),
				)
				return
			}

			// Increase the delay that we will use for a future connection
			// attempt.
			ip.increaseDelay(
				n.config.InitialReconnectDelay,
				n.config.MaxReconnectDelay,
			)

			conn, err := n.dialer.Dial(ctx, ip.ip)
			if err != nil {
				n.peerConfig.Log.Verbo(
					"failed to reach peer, attempting again",
					zap.Stringer("peerIP", ip.ip.IP),
					zap.Duration("delay", ip.delay),
				)
				continue
			}

			err = n.upgrade(conn, n.clientUpgrader)
			if err != nil {
				n.peerConfig.Log.Verbo(
					"failed to upgrade, attempting again",
					zap.Stringer("peerIP", ip.ip.IP),
					zap.Duration("delay", ip.delay),
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
	upgradeTimeout := n.peerConfig.Clock.Time().Add(n.config.ReadHandshakeTimeout)
	if err := conn.SetReadDeadline(upgradeTimeout); err != nil {
		_ = conn.Close()
		n.peerConfig.Log.Verbo("failed to set the read deadline",
			zap.Error(err),
		)
		return err
	}

	nodeID, tlsConn, cert, err := upgrader.Upgrade(conn)
	if err != nil {
		_ = conn.Close()
		n.peerConfig.Log.Verbo("failed to upgrade connection",
			zap.Error(err),
		)
		return err
	}

	if err := tlsConn.SetReadDeadline(time.Time{}); err != nil {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo("failed to clear the read deadline",
			zap.Error(err),
		)
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
			"dropping undesired connection",
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	if n.closing {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping connection",
			zap.String("reason", "shutting down the p2p network"),
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	if _, connecting := n.connectingPeers.GetByID(nodeID); connecting {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping connection",
			zap.String("reason", "already connecting to peer"),
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	if _, connected := n.connectedPeers.GetByID(nodeID); connected {
		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping connection",
			zap.String("reason", "already connecting to peer"),
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	n.peerConfig.Log.Verbo("starting handshake",
		zap.Stringer("nodeID", nodeID),
	)

	if !n.gossipTracker.StartTrackingPeer(nodeID) {
		n.peerConfig.Log.Error(
			"started duplicate peer tracker",
			zap.Stringer("nodeID", nodeID),
		)
	}

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
			n.peerConfig.Log.Debug("closing the network listener",
				zap.Error(err),
			)
		}

		n.peersLock.Lock()
		defer n.peersLock.Unlock()

		n.closing = true
		n.onCloseCtxCancel()

		for nodeID, tracked := range n.trackedIPs {
			tracked.stopTracking()
			delete(n.peerIPs, nodeID)
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

func (n *network) NodeUptime(subnetID ids.ID) (UptimeResult, error) {
	if subnetID != constants.PrimaryNetworkID && !n.config.WhitelistedSubnets.Contains(subnetID) {
		return UptimeResult{}, errNotWhiteListed
	}

	validators, ok := n.config.Validators.Get(subnetID)
	if !ok {
		return UptimeResult{}, errSubnetNotExist
	}

	myStake := validators.GetWeight(n.config.MyNodeID)
	if myStake == 0 {
		return UptimeResult{}, errNotValidator
	}

	var (
		totalWeight          = float64(validators.Weight())
		totalWeightedPercent = 100 * float64(myStake)
		rewardingStake       = float64(myStake)
	)

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	for i := 0; i < n.connectedPeers.Len(); i++ {
		peer, _ := n.connectedPeers.GetByIndex(i)

		nodeID := peer.ID()
		weight := validators.GetWeight(nodeID)
		if weight == 0 {
			// this is not a validator skip it.
			continue
		}

		observedUptime, exist := peer.ObservedUptime(subnetID)
		if !exist {
			observedUptime = 0
		}
		percent := float64(observedUptime)
		weightFloat := float64(weight)
		totalWeightedPercent += percent * weightFloat

		// if this peer thinks we're above requirement add the weight
		// TODO: use subnet-specific uptime requirements
		if percent/100 >= n.config.UptimeRequirement {
			rewardingStake += weightFloat
		}
	}

	return UptimeResult{
		WeightedAveragePercentage: gomath.Abs(totalWeightedPercent / totalWeight),
		RewardingStakePercentage:  gomath.Abs(100 * rewardingStake / totalWeight),
	}, nil
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
			n.gossipPeerLists()
		case <-updateUptimes.C:
			primaryUptime, err := n.NodeUptime(constants.PrimaryNetworkID)
			if err != nil {
				n.peerConfig.Log.Debug("failed to get primary network uptime",
					zap.Error(err),
				)
			}
			n.metrics.nodeUptimeWeightedAverage.Set(primaryUptime.WeightedAveragePercentage)
			n.metrics.nodeUptimeRewardingStake.Set(primaryUptime.RewardingStakePercentage)

			for subnetID := range n.config.WhitelistedSubnets {
				result, err := n.NodeUptime(subnetID)
				if err != nil {
					n.peerConfig.Log.Debug("failed to get subnet uptime",
						zap.Stringer("subnetID", subnetID),
						zap.Error(err),
					)
				}
				subnetIDStr := subnetID.String()
				n.metrics.nodeSubnetUptimeWeightedAverage.WithLabelValues(subnetIDStr).Set(result.WeightedAveragePercentage)
				n.metrics.nodeSubnetUptimeRewardingStake.WithLabelValues(subnetIDStr).Set(result.RewardingStakePercentage)
			}
		}
	}
}

// gossipPeerLists gossips validators to peers in the network
func (n *network) gossipPeerLists() {
	peers := n.samplePeers(
		constants.PrimaryNetworkID,
		false,
		int(n.config.PeerListValidatorGossipSize),
		int(n.config.PeerListNonValidatorGossipSize),
		int(n.config.PeerListPeersGossipSize),
	)

	for _, p := range peers {
		p.StartSendPeerList()
	}
}
