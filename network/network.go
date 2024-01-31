// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pires/go-proxyproto"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"

	safemath "github.com/ava-labs/avalanchego/utils/math"
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

	errNotValidator        = errors.New("node is not a validator")
	errNotTracked          = errors.New("subnet is not tracked")
	errExpectedProxy       = errors.New("expected proxy")
	errExpectedTCPProtocol = errors.New("expected TCP protocol")
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

// To avoid potential deadlocks, we maintain that locks must be grabbed in the
// following order:
//
// 1. peersLock
// 2. manuallyTrackedIDsLock
//
// If a higher lock (e.g. manuallyTrackedIDsLock) is held when trying to grab a
// lower lock (e.g. peersLock) a deadlock could occur.
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
	onCloseCtxCancel context.CancelFunc

	sendFailRateCalculator safemath.Averager

	// Tracks which peers know about which peers
	ipTracker *ipTracker
	peersLock sync.RWMutex
	// trackedIPs contains the set of IPs that we are currently attempting to
	// connect to. An entry is added to this set when we first start attempting
	// to connect to the peer. An entry is deleted from this set once we have
	// finished the handshake.
	trackedIPs      map[ids.NodeID]*trackedIP
	connectingPeers peer.Set
	connectedPeers  peer.Set
	closing         bool

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
	if config.ProxyEnabled {
		// Wrap the listener to process the proxy header.
		listener = &proxyproto.Listener{
			Listener: listener,
			Policy: func(net.Addr) (proxyproto.Policy, error) {
				// Do not perform any fuzzy matching, the header must be
				// provided.
				return proxyproto.REQUIRE, nil
			},
			ValidateHeader: func(h *proxyproto.Header) error {
				if !h.Command.IsProxy() {
					return errExpectedProxy
				}
				if h.TransportProtocol != proxyproto.TCPv4 && h.TransportProtocol != proxyproto.TCPv6 {
					return errExpectedTCPProtocol
				}
				return nil
			},
			ReadHeaderTimeout: config.ProxyReadHeaderTimeout,
		}
	}

	inboundMsgThrottler, err := throttling.NewInboundMsgThrottler(
		log,
		config.Namespace,
		metricsRegisterer,
		config.Validators,
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
		config.Validators,
		config.ThrottlerConfig.OutboundMsgThrottlerConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing outbound message throttler failed with: %w", err)
	}

	peerMetrics, err := peer.NewMetrics(log, config.Namespace, metricsRegisterer)
	if err != nil {
		return nil, fmt.Errorf("initializing peer metrics failed with: %w", err)
	}

	metrics, err := newMetrics(config.Namespace, metricsRegisterer, config.TrackedSubnets)
	if err != nil {
		return nil, fmt.Errorf("initializing network metrics failed with: %w", err)
	}

	ipTracker, err := newIPTracker(log, config.Namespace, metricsRegisterer)
	if err != nil {
		return nil, fmt.Errorf("initializing ip tracker failed with: %w", err)
	}
	config.Validators.RegisterCallbackListener(constants.PrimaryNetworkID, ipTracker)

	// Track all default bootstrappers to ensure their current IPs are gossiped
	// like validator IPs.
	for _, bootstrapper := range genesis.GetBootstrappers(config.NetworkID) {
		ipTracker.ManuallyTrack(bootstrapper.ID)
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
		MySubnets:            config.TrackedSubnets,
		Beacons:              config.Beacons,
		NetworkID:            config.NetworkID,
		PingFrequency:        config.PingFrequency,
		PongTimeout:          config.PingPongTimeout,
		MaxClockDifference:   config.MaxClockDifference,
		SupportedACPs:        config.SupportedACPs.List(),
		ObjectedACPs:         config.ObjectedACPs.List(),
		ResourceTracker:      config.ResourceTracker,
		UptimeCalculator:     config.UptimeCalculator,
		IPSigner:             peer.NewIPSigner(config.MyIPPort, config.TLSKey),
	}

	// Invariant: We delay the activation of durango during the TLS handshake to
	// avoid gossiping any TLS certs that anyone else in the network may
	// consider invalid. Recall that if a peer gossips an invalid cert, the
	// connection is terminated.
	durangoTime := version.GetDurangoTime(config.NetworkID)
	durangoTimeWithClockSkew := durangoTime.Add(config.MaxClockDifference)
	onCloseCtx, cancel := context.WithCancel(context.Background())
	n := &network{
		config:               config,
		peerConfig:           peerConfig,
		metrics:              metrics,
		outboundMsgThrottler: outboundMsgThrottler,

		inboundConnUpgradeThrottler: throttling.NewInboundConnUpgradeThrottler(log, config.ThrottlerConfig.InboundConnUpgradeThrottlerConfig),
		listener:                    listener,
		dialer:                      dialer,
		serverUpgrader:              peer.NewTLSServerUpgrader(config.TLSConfig, metrics.tlsConnRejected, durangoTimeWithClockSkew),
		clientUpgrader:              peer.NewTLSClientUpgrader(config.TLSConfig, metrics.tlsConnRejected, durangoTimeWithClockSkew),

		onCloseCtx:       onCloseCtx,
		onCloseCtxCancel: cancel,

		sendFailRateCalculator: safemath.NewSyncAverager(safemath.NewAverager(
			0,
			config.SendFailRateHalflife,
			time.Now(),
		)),

		trackedIPs:      make(map[ids.NodeID]*trackedIP),
		ipTracker:       ipTracker,
		connectingPeers: peer.NewSet(),
		connectedPeers:  peer.NewSet(),
		router:          router,
	}
	n.peerConfig.Network = n
	return n, nil
}

func (n *network) Send(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], subnetID ids.ID, allower subnets.Allower) set.Set[ids.NodeID] {
	peers := n.getPeers(nodeIDs, subnetID, allower)
	n.peerConfig.Metrics.MultipleSendsFailed(
		msg.Op(),
		nodeIDs.Len()-len(peers),
	)
	return n.send(msg, peers)
}

func (n *network) Gossip(
	msg message.OutboundMessage,
	subnetID ids.ID,
	numValidatorsToSend int,
	numNonValidatorsToSend int,
	numPeersToSend int,
	allower subnets.Allower,
) set.Set[ids.NodeID] {
	peers := n.samplePeers(subnetID, numValidatorsToSend, numNonValidatorsToSend, numPeersToSend, allower)
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

	lastMsgReceivedAt, msgReceived := n.getLastReceived()
	wasMsgReceivedRecently := msgReceived
	timeSinceLastMsgReceived := time.Duration(0)
	if msgReceived {
		timeSinceLastMsgReceived = now.Sub(lastMsgReceivedAt)
		wasMsgReceivedRecently = timeSinceLastMsgReceived <= n.config.HealthConfig.MaxTimeSinceMsgReceived
		details[TimeSinceLastMsgReceivedKey] = timeSinceLastMsgReceived.String()
		n.metrics.timeSinceLastMsgReceived.Set(float64(timeSinceLastMsgReceived))
	}
	healthy = healthy && wasMsgReceivedRecently

	// Make sure we've sent an outgoing message within the threshold
	lastMsgSentAt, msgSent := n.getLastSent()
	wasMsgSentRecently := msgSent
	timeSinceLastMsgSent := time.Duration(0)
	if msgSent {
		timeSinceLastMsgSent = now.Sub(lastMsgSentAt)
		wasMsgSentRecently = timeSinceLastMsgSent <= n.config.HealthConfig.MaxTimeSinceMsgSent
		details[TimeSinceLastMsgSentKey] = timeSinceLastMsgSent.String()
		n.metrics.timeSinceLastMsgSent.Set(float64(timeSinceLastMsgSent))
	}
	healthy = healthy && wasMsgSentRecently

	// Make sure the message send failed rate isn't too high
	isMsgFailRate := sendFailRate <= n.config.HealthConfig.MaxSendFailRate
	healthy = healthy && isMsgFailRate
	details[SendFailRateKey] = sendFailRate
	n.metrics.sendFailRate.Set(sendFailRate)

	// emit metrics about the lifetime of peer connections
	n.metrics.updatePeerConnectionLifetimeMetrics()

	// Network layer is healthy
	if healthy || !n.config.HealthConfig.Enabled {
		return details, nil
	}

	var errorReasons []string
	if !isConnected {
		errorReasons = append(errorReasons, fmt.Sprintf("not connected to a minimum of %d peer(s) only %d", n.config.HealthConfig.MinConnectedPeers, connectedTo))
	}
	if !msgReceived {
		errorReasons = append(errorReasons, "no messages received from network")
	} else if !wasMsgReceivedRecently {
		errorReasons = append(errorReasons, fmt.Sprintf("no messages from network received in %s > %s", timeSinceLastMsgReceived, n.config.HealthConfig.MaxTimeSinceMsgReceived))
	}
	if !msgSent {
		errorReasons = append(errorReasons, "no messages sent to network")
	} else if !wasMsgSentRecently {
		errorReasons = append(errorReasons, fmt.Sprintf("no messages from network sent in %s > %s", timeSinceLastMsgSent, n.config.HealthConfig.MaxTimeSinceMsgSent))
	}

	if !isMsgFailRate {
		errorReasons = append(errorReasons, fmt.Sprintf("messages failure send rate %g > %g", sendFailRate, n.config.HealthConfig.MaxSendFailRate))
	}
	return details, fmt.Errorf("network layer is unhealthy reason: %s", strings.Join(errorReasons, ", "))
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

	if tracked, ok := n.trackedIPs[nodeID]; ok {
		tracked.stopTracking()
		delete(n.trackedIPs, nodeID)
	}
	n.connectingPeers.Remove(nodeID)
	n.connectedPeers.Add(peer)
	n.peersLock.Unlock()

	peerIP := peer.IP()
	newIP := ips.NewClaimedIPPort(
		peer.Cert(),
		peerIP.IPPort,
		peerIP.Timestamp,
		peerIP.Signature,
	)
	n.ipTracker.Connected(newIP)

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
	if !n.config.RequireValidatorToConnect {
		return true
	}
	_, iAmAValidator := n.config.Validators.GetValidator(constants.PrimaryNetworkID, n.config.MyNodeID)
	return iAmAValidator || n.ipTracker.WantsConnection(nodeID)
}

func (n *network) Track(claimedIPPorts []*ips.ClaimedIPPort) error {
	for _, ip := range claimedIPPorts {
		if err := n.track(ip); err != nil {
			return err
		}
	}
	return nil
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

func (n *network) KnownPeers() ([]byte, []byte) {
	return n.ipTracker.Bloom()
}

func (n *network) Peers(except ids.NodeID, knownPeers *bloom.ReadFilter, salt []byte) []*ips.ClaimedIPPort {
	return n.ipTracker.GetGossipableIPs(
		except,
		knownPeers,
		salt,
		int(n.config.PeerListNumValidatorIPs),
	)
}

// Dispatch starts accepting connections from other nodes attempting to connect
// to this node.
func (n *network) Dispatch() error {
	go n.runTimers() // Periodically perform operations
	go n.inboundConnUpgradeThrottler.Dispatch()
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

		// Note: listener.Accept is rate limited outside of this package, so a
		// peer can not just arbitrarily spin up goroutines here.
		go func() {
			// Note: Calling [RemoteAddr] with the Proxy protocol enabled may
			// block for up to ProxyReadHeaderTimeout. Therefore, we ensure to
			// call this function inside the go-routine, rather than the main
			// accept loop.
			remoteAddr := conn.RemoteAddr().String()
			ip, err := ips.ToIPPort(remoteAddr)
			if err != nil {
				n.peerConfig.Log.Error("failed to parse remote address",
					zap.String("peerIP", remoteAddr),
					zap.Error(err),
				)
				_ = conn.Close()
				return
			}

			if !n.inboundConnUpgradeThrottler.ShouldUpgrade(ip) {
				n.peerConfig.Log.Debug("failed to upgrade connection",
					zap.String("reason", "rate-limiting"),
					zap.Stringer("peerIP", ip),
				)
				n.metrics.inboundConnRateLimited.Inc()
				_ = conn.Close()
				return
			}
			n.metrics.inboundConnAllowed.Inc()

			n.peerConfig.Log.Verbo("starting to upgrade connection",
				zap.String("direction", "inbound"),
				zap.Stringer("peerIP", ip),
			)

			if err := n.upgrade(conn, n.serverUpgrader); err != nil {
				n.peerConfig.Log.Verbo("failed to upgrade connection",
					zap.String("direction", "inbound"),
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

	errs := wrappers.Errs{}
	for _, peer := range append(connecting, connected...) {
		errs.Add(peer.AwaitClosed(context.TODO()))
	}
	return errs.Err
}

func (n *network) ManuallyTrack(nodeID ids.NodeID, ip ips.IPPort) {
	n.ipTracker.ManuallyTrack(nodeID)

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

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
		n.dial(nodeID, tracked)
	}
}

func (n *network) track(ip *ips.ClaimedIPPort) error {
	// To avoid signature verification when the IP isn't needed, we
	// optimistically filter out IPs. This can result in us not tracking an IP
	// that we otherwise would have. This case can only happen if the node
	// became a validator between the time we verified the signature and when we
	// processed the IP; which should be very rare.
	//
	// Note: Avoiding signature verification when the IP isn't needed is a
	// **significant** performance optimization.
	if !n.ipTracker.ShouldVerifyIP(ip) {
		n.metrics.numUselessPeerListBytes.Add(float64(ip.Size()))
		return nil
	}

	// Perform all signature verification and hashing before grabbing the peer
	// lock.
	signedIP := peer.SignedIP{
		UnsignedIP: peer.UnsignedIP{
			IPPort:    ip.IPPort,
			Timestamp: ip.Timestamp,
		},
		Signature: ip.Signature,
	}
	maxTimestamp := n.peerConfig.Clock.Time().Add(n.peerConfig.MaxClockDifference)
	if err := signedIP.Verify(ip.Cert, maxTimestamp); err != nil {
		return err
	}

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	if !n.ipTracker.AddIP(ip) {
		return nil
	}

	if _, connected := n.connectedPeers.GetByID(ip.NodeID); connected {
		// If I'm currently connected to [nodeID] then I'll attempt to dial them
		// when we disconnect.
		return nil
	}

	tracked, isTracked := n.trackedIPs[ip.NodeID]
	if isTracked {
		// Stop tracking the old IP and start tracking the new one.
		tracked = tracked.trackNewIP(ip.IPPort)
	} else {
		tracked = newTrackedIP(ip.IPPort)
	}
	n.trackedIPs[ip.NodeID] = tracked
	n.dial(ip.NodeID, tracked)
	return nil
}

// getPeers returns a slice of connected peers from a set of [nodeIDs].
//
//   - [nodeIDs] the IDs of the peers that should be returned if they are
//     connected.
//   - [subnetID] the subnetID whose membership should be considered if
//     [validatorOnly] is set to true.
//   - [validatorOnly] is the flag to drop any nodes from [nodeIDs] that are not
//     validators in [subnetID].
func (n *network) getPeers(
	nodeIDs set.Set[ids.NodeID],
	subnetID ids.ID,
	allower subnets.Allower,
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

		_, isValidator := n.config.Validators.GetValidator(subnetID, nodeID)
		// check if the peer is allowed to connect to the subnet
		if !allower.IsAllowed(nodeID, isValidator) {
			continue
		}

		peers = append(peers, peer)
	}

	return peers
}

func (n *network) samplePeers(
	subnetID ids.ID,
	numValidatorsToSample,
	numNonValidatorsToSample int,
	numPeersToSample int,
	allower subnets.Allower,
) []peer.Peer {
	// If there are fewer validators than [numValidatorsToSample], then only
	// sample [numValidatorsToSample] validators.
	subnetValidatorsLen := n.config.Validators.Count(subnetID)
	if subnetValidatorsLen < numValidatorsToSample {
		numValidatorsToSample = subnetValidatorsLen
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

			peerID := p.ID()
			_, isValidator := n.config.Validators.GetValidator(subnetID, peerID)
			// check if the peer is allowed to connect to the subnet
			if !allower.IsAllowed(peerID, isValidator) {
				return false
			}

			if numPeersToSample > 0 {
				numPeersToSample--
				return true
			}

			if isValidator {
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
		if n.ipTracker.WantsConnection(nodeID) {
			tracked := tracked.trackNewIP(tracked.ip)
			n.trackedIPs[nodeID] = tracked
			n.dial(nodeID, tracked)
		} else {
			tracked.stopTracking()
			delete(n.trackedIPs, nodeID)
		}
	}

	n.metrics.disconnected.Inc()
}

func (n *network) disconnectedFromConnected(peer peer.Peer, nodeID ids.NodeID) {
	n.ipTracker.Disconnected(nodeID)
	n.router.Disconnected(nodeID)

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	n.connectedPeers.Remove(nodeID)

	// The peer that is disconnecting from us finished the handshake
	if ip, wantsConnection := n.ipTracker.GetIP(nodeID); wantsConnection {
		tracked := newTrackedIP(ip.IPPort)
		n.trackedIPs[nodeID] = tracked
		n.dial(nodeID, tracked)
	}

	n.metrics.markDisconnected(peer)
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
func (n *network) dial(nodeID ids.NodeID, ip *trackedIP) {
	n.peerConfig.Log.Verbo("attempting to dial node",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("ip", ip.ip),
	)
	go func() {
		n.metrics.numTracked.Inc()
		defer n.metrics.numTracked.Dec()

		for {
			timer := time.NewTimer(ip.getDelay())

			select {
			case <-n.onCloseCtx.Done():
				timer.Stop()
				return
			case <-ip.onStopTracking:
				timer.Stop()
				return
			case <-timer.C:
			}

			n.peersLock.Lock()
			// If we no longer desire a connect to nodeID, we should cleanup
			// trackedIPs and this goroutine. This prevents a memory leak when
			// the tracked nodeID leaves the validator set and is never able to
			// be connected to.
			if !n.ipTracker.WantsConnection(nodeID) {
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

			// If the network is configured to disallow private IPs and the
			// provided IP is private, we skip all attempts to initiate a
			// connection.
			//
			// Invariant: We perform this check inside of the looping goroutine
			// because this goroutine must clean up the trackedIPs entry if
			// nodeID leaves the validator set. This is why we continue the loop
			// rather than returning even though we will never initiate an
			// outbound connection with this IP.
			if !n.config.AllowPrivateIPs && ip.ip.IP.IsPrivate() {
				n.peerConfig.Log.Verbo("skipping connection dial",
					zap.String("reason", "outbound connections to private IPs are prohibited"),
					zap.Stringer("nodeID", nodeID),
					zap.Stringer("peerIP", ip.ip),
					zap.Duration("delay", ip.delay),
				)
				continue
			}

			conn, err := n.dialer.Dial(n.onCloseCtx, ip.ip)
			if err != nil {
				n.peerConfig.Log.Verbo(
					"failed to reach peer, attempting again",
					zap.Stringer("nodeID", nodeID),
					zap.Stringer("peerIP", ip.ip),
					zap.Duration("delay", ip.delay),
				)
				continue
			}

			n.peerConfig.Log.Verbo("starting to upgrade connection",
				zap.String("direction", "outbound"),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("peerIP", ip.ip),
			)

			err = n.upgrade(conn, n.clientUpgrader)
			if err != nil {
				n.peerConfig.Log.Verbo(
					"failed to upgrade, attempting again",
					zap.Stringer("nodeID", nodeID),
					zap.Stringer("peerIP", ip.ip),
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
	if n.closing {
		n.peersLock.Unlock()

		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping connection",
			zap.String("reason", "shutting down the p2p network"),
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	if _, connecting := n.connectingPeers.GetByID(nodeID); connecting {
		n.peersLock.Unlock()

		_ = tlsConn.Close()
		n.peerConfig.Log.Verbo(
			"dropping connection",
			zap.String("reason", "already connecting to peer"),
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	if _, connected := n.connectedPeers.GetByID(nodeID); connected {
		n.peersLock.Unlock()

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
	n.peersLock.Unlock()
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
	if subnetID != constants.PrimaryNetworkID && !n.config.TrackedSubnets.Contains(subnetID) {
		return UptimeResult{}, errNotTracked
	}

	myStake := n.config.Validators.GetWeight(subnetID, n.config.MyNodeID)
	if myStake == 0 {
		return UptimeResult{}, errNotValidator
	}

	totalWeightInt, err := n.config.Validators.TotalWeight(subnetID)
	if err != nil {
		return UptimeResult{}, fmt.Errorf("error while fetching weight for subnet %s: %w", subnetID, err)
	}

	var (
		totalWeight          = float64(totalWeightInt)
		totalWeightedPercent = 100 * float64(myStake)
		rewardingStake       = float64(myStake)
	)

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	for i := 0; i < n.connectedPeers.Len(); i++ {
		peer, _ := n.connectedPeers.GetByIndex(i)

		nodeID := peer.ID()
		weight := n.config.Validators.GetWeight(subnetID, nodeID)
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
		WeightedAveragePercentage: math.Abs(totalWeightedPercent / totalWeight),
		RewardingStakePercentage:  math.Abs(100 * rewardingStake / totalWeight),
	}, nil
}

func (n *network) runTimers() {
	pushGossipPeerlists := time.NewTicker(n.config.PeerListGossipFreq)
	pullGossipPeerlists := time.NewTicker(n.config.PeerListPullGossipFreq)
	resetPeerListBloom := time.NewTicker(n.config.PeerListBloomResetFreq)
	updateUptimes := time.NewTicker(n.config.UptimeMetricFreq)
	defer func() {
		pushGossipPeerlists.Stop()
		resetPeerListBloom.Stop()
		updateUptimes.Stop()
	}()

	for {
		select {
		case <-n.onCloseCtx.Done():
			return
		case <-pushGossipPeerlists.C:
			n.pushGossipPeerLists()
		case <-pullGossipPeerlists.C:
			n.pullGossipPeerLists()
		case <-resetPeerListBloom.C:
			if err := n.ipTracker.ResetBloom(); err != nil {
				n.peerConfig.Log.Error("failed to reset ip tracker bloom filter",
					zap.Error(err),
				)
			} else {
				n.peerConfig.Log.Debug("reset ip tracker bloom filter")
			}
		case <-updateUptimes.C:
			primaryUptime, err := n.NodeUptime(constants.PrimaryNetworkID)
			if err != nil {
				n.peerConfig.Log.Debug("failed to get primary network uptime",
					zap.Error(err),
				)
			}
			n.metrics.nodeUptimeWeightedAverage.Set(primaryUptime.WeightedAveragePercentage)
			n.metrics.nodeUptimeRewardingStake.Set(primaryUptime.RewardingStakePercentage)

			for subnetID := range n.config.TrackedSubnets {
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

// pushGossipPeerLists gossips validators to peers in the network
func (n *network) pushGossipPeerLists() {
	peers := n.samplePeers(
		constants.PrimaryNetworkID,
		int(n.config.PeerListValidatorGossipSize),
		int(n.config.PeerListNonValidatorGossipSize),
		int(n.config.PeerListPeersGossipSize),
		subnets.NoOpAllower,
	)

	for _, p := range peers {
		p.StartSendPeerList()
	}
}

// pullGossipPeerLists requests validators from peers in the network
func (n *network) pullGossipPeerLists() {
	peers := n.samplePeers(
		constants.PrimaryNetworkID,
		1, // numValidatorsToSample
		0, // numNonValidatorsToSample
		0, // numPeersToSample
		subnets.NoOpAllower,
	)

	for _, p := range peers {
		p.StartSendGetPeerList()
	}
}

func (n *network) getLastReceived() (time.Time, bool) {
	lastReceived := atomic.LoadInt64(&n.peerConfig.LastReceived)
	if lastReceived == 0 {
		return time.Time{}, false
	}
	return time.Unix(lastReceived, 0), true
}

func (n *network) getLastSent() (time.Time, bool) {
	lastSent := atomic.LoadInt64(&n.peerConfig.LastSent)
	if lastSent == 0 {
		return time.Time{}, false
	}
	return time.Unix(lastSent, 0), true
}
