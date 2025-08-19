// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"crypto"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
)

var (
	defaultHealthConfig = HealthConfig{
		MinConnectedPeers:            1,
		MaxTimeSinceMsgReceived:      time.Minute,
		MaxTimeSinceMsgSent:          time.Minute,
		MaxPortionSendQueueBytesFull: .9,
		MaxSendFailRate:              .1,
		SendFailRateHalflife:         time.Second,
	}
	defaultPeerListGossipConfig = PeerListGossipConfig{
		PeerListNumValidatorIPs: 100,
		PeerListPullGossipFreq:  time.Second,
		PeerListBloomResetFreq:  constants.DefaultNetworkPeerListBloomResetFreq,
	}
	defaultTimeoutConfig = TimeoutConfig{
		PingPongTimeout:      30 * time.Second,
		ReadHandshakeTimeout: 15 * time.Second,
	}
	defaultDelayConfig = DelayConfig{
		MaxReconnectDelay:     time.Hour,
		InitialReconnectDelay: time.Second,
	}
	defaultThrottlerConfig = ThrottlerConfig{
		InboundConnUpgradeThrottlerConfig: throttling.InboundConnUpgradeThrottlerConfig{
			UpgradeCooldown:        time.Second,
			MaxRecentConnsUpgraded: 100,
		},
		InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
			MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
				VdrAllocSize:        1 * units.GiB,
				AtLargeAllocSize:    1 * units.GiB,
				NodeMaxAtLargeBytes: constants.DefaultMaxMessageSize,
			},
			BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
				RefillRate:   units.MiB,
				MaxBurstSize: constants.DefaultMaxMessageSize,
			},
			CPUThrottlerConfig: throttling.SystemThrottlerConfig{
				MaxRecheckDelay: 50 * time.Millisecond,
			},
			MaxProcessingMsgsPerNode: 100,
			DiskThrottlerConfig: throttling.SystemThrottlerConfig{
				MaxRecheckDelay: 50 * time.Millisecond,
			},
		},
		OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
			VdrAllocSize:        1 * units.GiB,
			AtLargeAllocSize:    1 * units.GiB,
			NodeMaxAtLargeBytes: constants.DefaultMaxMessageSize,
		},
		MaxInboundConnsPerSec: 100,
	}
	defaultDialerConfig = dialer.Config{
		ThrottleRps:       100,
		ConnectionTimeout: time.Second,
	}

	defaultConfig = Config{
		HealthConfig:         defaultHealthConfig,
		PeerListGossipConfig: defaultPeerListGossipConfig,
		TimeoutConfig:        defaultTimeoutConfig,
		DelayConfig:          defaultDelayConfig,
		ThrottlerConfig:      defaultThrottlerConfig,

		DialerConfig: defaultDialerConfig,

		NetworkID:          49463,
		MaxClockDifference: time.Minute,
		PingFrequency:      constants.DefaultPingFrequency,
		AllowPrivateIPs:    true,

		CompressionType: constants.DefaultNetworkCompressionType,

		UptimeCalculator:  uptime.NewManager(uptime.NewTestState(), &mockable.Clock{}),
		UptimeMetricFreq:  30 * time.Second,
		UptimeRequirement: .8,

		RequireValidatorToConnect: false,

		MaximumInboundMessageTimeout: 30 * time.Second,
		ResourceTracker:              newDefaultResourceTracker(),
		CPUTargeter:                  nil, // Set in init
		DiskTargeter:                 nil, // Set in init
	}
)

func init() {
	defaultConfig.CPUTargeter = newDefaultTargeter(defaultConfig.ResourceTracker.CPUTracker())
	defaultConfig.DiskTargeter = newDefaultTargeter(defaultConfig.ResourceTracker.DiskTracker())
}

func newDefaultTargeter(t tracker.Tracker) tracker.Targeter {
	return tracker.NewTargeter(
		logging.NoLog{},
		&tracker.TargeterConfig{
			VdrAlloc:           10,
			MaxNonVdrUsage:     10,
			MaxNonVdrNodeUsage: 10,
		},
		validators.NewManager(),
		t,
	)
}

func newDefaultResourceTracker() tracker.ResourceTracker {
	tracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		10*time.Second,
	)
	if err != nil {
		panic(err)
	}
	return tracker
}

func newTestNetworkConnectToAll(t *testing.T, count int) (*testDialer, []*testListener, []ids.NodeID, []*Config) {
	return newTestNetworkInner(t, count, true)
}

func newTestNetwork(t *testing.T, count int) (*testDialer, []*testListener, []ids.NodeID, []*Config) {
	return newTestNetworkInner(t, count, false)
}

func newTestNetworkInner(t *testing.T, count int, connectToAllValidators bool) (*testDialer, []*testListener, []ids.NodeID, []*Config) {
	var (
		dialer    = newTestDialer()
		listeners = make([]*testListener, count)
		nodeIDs   = make([]ids.NodeID, count)
		configs   = make([]*Config, count)
	)
	for i := 0; i < count; i++ {
		ip, listener := dialer.NewListener()

		tlsCert, err := staking.NewTLSCert()
		require.NoError(t, err)

		cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
		require.NoError(t, err)
		nodeID := ids.NodeIDFromCert(cert)

		blsKey, err := localsigner.New()
		require.NoError(t, err)

		config := defaultConfig
		config.TLSConfig = peer.TLSConfig(*tlsCert, nil)
		config.MyNodeID = nodeID
		config.MyIPPort = utils.NewAtomic(ip)
		config.TLSKey = tlsCert.PrivateKey.(crypto.Signer)
		config.BLSKey = blsKey
		config.ConnectToAllValidators = connectToAllValidators

		listeners[i] = listener
		nodeIDs[i] = nodeID
		configs[i] = &config
	}
	return dialer, listeners, nodeIDs, configs
}

func newMessageCreator(t *testing.T) message.Creator {
	t.Helper()

	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)

	return mc
}

func newFullyConnectedTestNetwork(t *testing.T, handlers []router.InboundHandler) ([]ids.NodeID, []*network, *errgroup.Group) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, len(handlers))

	var (
		networks = make([]*network, len(configs))

		globalLock     sync.Mutex
		numConnected   int
		allConnected   bool
		onAllConnected = make(chan struct{})
	)
	for i, config := range configs {
		msgCreator := newMessageCreator(t)
		registry := prometheus.NewRegistry()

		beacons := validators.NewManager()
		require.NoError(beacons.AddStaker(constants.PrimaryNetworkID, nodeIDs[0], nil, ids.GenerateTestID(), 1))

		vdrs := validators.NewManager()
		for _, nodeID := range nodeIDs {
			require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.GenerateTestID(), 1))
		}

		config.Beacons = beacons
		config.Validators = vdrs

		var connected set.Set[ids.NodeID]
		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			msgCreator,
			registry,
			logging.NoLog{},
			listeners[i],
			dialer,
			&testHandler{
				InboundHandler: handlers[i],
				ConnectedF: func(nodeID ids.NodeID, _ *version.Application, _ ids.ID) {
					t.Logf("%s connected to %s", config.MyNodeID, nodeID)

					globalLock.Lock()
					defer globalLock.Unlock()

					require.False(connected.Contains(nodeID))
					connected.Add(nodeID)
					numConnected++

					if !allConnected && numConnected == len(nodeIDs)*(len(nodeIDs)-1) {
						allConnected = true
						close(onAllConnected)
					}
				},
				DisconnectedF: func(nodeID ids.NodeID) {
					t.Logf("%s disconnected from %s", config.MyNodeID, nodeID)

					globalLock.Lock()
					defer globalLock.Unlock()

					require.True(connected.Contains(nodeID))
					connected.Remove(nodeID)
					numConnected--
				},
			},
		)
		require.NoError(err)
		networks[i] = net.(*network)
	}

	eg := &errgroup.Group{}
	for i, net := range networks {
		if i != 0 {
			config := configs[0]
			net.ManuallyTrack(config.MyNodeID, config.MyIPPort.Get())
			// Wait until the node is connected to the first node.
			// This forces nodes to connect to each other in a deterministic order.
			require.Eventually(func() bool {
				return len(net.PeerInfo([]ids.NodeID{config.MyNodeID})) > 0
			}, 10*time.Second, time.Millisecond)
		}

		eg.Go(net.Dispatch)
	}

	if len(networks) > 1 {
		<-onAllConnected
	}

	return nodeIDs, networks, eg
}

func TestNewNetwork(t *testing.T) {
	require := require.New(t)
	_, networks, eg := newFullyConnectedTestNetwork(t, []router.InboundHandler{nil, nil, nil})
	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestIngressConnCount(t *testing.T) {
	require := require.New(t)

	emptyHandler := func(context.Context, message.InboundMessage) {}

	_, networks, eg := newFullyConnectedTestNetwork(
		t, []router.InboundHandler{
			router.InboundHandlerFunc(emptyHandler),
			router.InboundHandlerFunc(emptyHandler),
			router.InboundHandlerFunc(emptyHandler),
		})

	for _, net := range networks {
		net.config.NoIngressValidatorConnectionGracePeriod = 0
		net.config.HealthConfig.Enabled = true
	}

	require.Eventually(func() bool {
		result := true
		for _, net := range networks {
			result = result && len(net.PeerInfo(nil)) == len(networks)-1
		}
		return result
	}, time.Minute, time.Millisecond*10)

	ingressConnCount := set.Of[int]()

	for _, net := range networks {
		connCount := net.IngressConnCount()
		ingressConnCount.Add(connCount)
		_, err := net.HealthCheck(context.Background())
		if connCount == 0 {
			require.ErrorContains(err, ErrNoIngressConnections.Error()) //nolint
		} else {
			require.NoError(err)
		}
	}

	// Some node has all nodes connected to it.
	// Some other node has only the remaining last node connected to it.
	// The remaining last node has no node connected to it, as it connects to the first and second node.
	// Since it has no one connecting to it, its health check fails.
	require.Equal(set.Of(0, 1, 2), ingressConnCount)

	for _, net := range networks {
		net.StartClose()
	}

	require.NoError(eg.Wait())
}

func TestSend(t *testing.T) {
	require := require.New(t)

	received := make(chan message.InboundMessage)
	nodeIDs, networks, eg := newFullyConnectedTestNetwork(
		t,
		[]router.InboundHandler{
			router.InboundHandlerFunc(func(context.Context, message.InboundMessage) {
				require.FailNow("unexpected message received")
			}),
			router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
				received <- msg
			}),
			router.InboundHandlerFunc(func(context.Context, message.InboundMessage) {
				require.FailNow("unexpected message received")
			}),
		},
	)

	net0 := networks[0]

	mc := newMessageCreator(t)
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty)
	require.NoError(err)

	toSend := set.Of(nodeIDs[1])
	sentTo := net0.Send(
		outboundGetMsg,
		common.SendConfig{
			NodeIDs: toSend,
		},
		constants.PrimaryNetworkID,
		subnets.NoOpAllower,
	)
	require.Equal(toSend, sentTo)

	inboundGetMsg := <-received
	require.Equal(message.GetOp, inboundGetMsg.Op())

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestSendWithFilter(t *testing.T) {
	require := require.New(t)

	received := make(chan message.InboundMessage)
	nodeIDs, networks, eg := newFullyConnectedTestNetwork(
		t,
		[]router.InboundHandler{
			router.InboundHandlerFunc(func(context.Context, message.InboundMessage) {
				require.FailNow("unexpected message received")
			}),
			router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
				received <- msg
			}),
			router.InboundHandlerFunc(func(context.Context, message.InboundMessage) {
				require.FailNow("unexpected message received")
			}),
		},
	)

	net0 := networks[0]

	mc := newMessageCreator(t)
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty)
	require.NoError(err)

	toSend := set.Of(nodeIDs...)
	validNodeID := nodeIDs[1]
	sentTo := net0.Send(
		outboundGetMsg,
		common.SendConfig{
			NodeIDs: toSend,
		},
		constants.PrimaryNetworkID,
		newNodeIDConnector(validNodeID),
	)
	require.Len(sentTo, 1)
	require.Contains(sentTo, validNodeID)

	inboundGetMsg := <-received
	require.Equal(message.GetOp, inboundGetMsg.Op())

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestTrackVerifiesSignatures(t *testing.T) {
	require := require.New(t)

	_, networks, eg := newFullyConnectedTestNetwork(t, []router.InboundHandler{nil})

	network := networks[0]

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	nodeID := ids.NodeIDFromCert(cert)

	require.NoError(network.config.Validators.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.Empty, 1))

	stakingCert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)

	err = network.Track([]*ips.ClaimedIPPort{
		ips.NewClaimedIPPort(
			stakingCert,
			netip.AddrPortFrom(
				netip.AddrFrom4([4]byte{123, 132, 123, 123}),
				10000,
			),
			1000, // timestamp
			nil,  // signature
		),
	})
	// The signature is wrong so this peer tracking info isn't useful.
	require.ErrorIs(err, staking.ErrECDSAVerificationFailure)

	network.peersLock.RLock()
	require.Empty(network.trackedIPs)
	network.peersLock.RUnlock()

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestTrackDoesNotDialPrivateIPs(t *testing.T) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, 2)

	networks := make([]Network, len(configs))
	for i, config := range configs {
		msgCreator := newMessageCreator(t)
		registry := prometheus.NewRegistry()

		beacons := validators.NewManager()
		require.NoError(beacons.AddStaker(constants.PrimaryNetworkID, nodeIDs[0], nil, ids.GenerateTestID(), 1))

		vdrs := validators.NewManager()
		for _, nodeID := range nodeIDs {
			require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.GenerateTestID(), 1))
		}

		config.Beacons = beacons
		config.Validators = vdrs
		config.AllowPrivateIPs = false

		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			msgCreator,
			registry,
			logging.NoLog{},
			listeners[i],
			dialer,
			&testHandler{
				InboundHandler: nil,
				ConnectedF: func(ids.NodeID, *version.Application, ids.ID) {
					require.FailNow("unexpectedly connected to a peer")
				},
				DisconnectedF: nil,
			},
		)
		require.NoError(err)
		networks[i] = net
	}

	eg := &errgroup.Group{}
	for i, net := range networks {
		if i != 0 {
			config := configs[0]
			net.ManuallyTrack(config.MyNodeID, config.MyIPPort.Get())
		}

		eg.Go(net.Dispatch)
	}

	network := networks[1].(*network)
	require.Eventually(
		func() bool {
			network.peersLock.RLock()
			defer network.peersLock.RUnlock()

			nodeID := nodeIDs[0]
			require.Contains(network.trackedIPs, nodeID)
			ip := network.trackedIPs[nodeID]
			return ip.getDelay() != 0
		},
		10*time.Second,
		50*time.Millisecond,
	)

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func testDialDeletesNonValidators(t *testing.T, connectToAllValidators bool) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetworkInner(t, 2, connectToAllValidators)

	vdrs := validators.NewManager()
	for _, nodeID := range nodeIDs {
		require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.GenerateTestID(), 1))
	}

	networks := make([]Network, len(configs))
	for i, config := range configs {
		msgCreator := newMessageCreator(t)
		registry := prometheus.NewRegistry()

		beacons := validators.NewManager()
		require.NoError(beacons.AddStaker(constants.PrimaryNetworkID, nodeIDs[0], nil, ids.GenerateTestID(), 1))

		config.Beacons = beacons
		config.Validators = vdrs
		config.AllowPrivateIPs = false

		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			msgCreator,
			registry,
			logging.NoLog{},
			listeners[i],
			dialer,
			&testHandler{
				InboundHandler: nil,
				ConnectedF: func(ids.NodeID, *version.Application, ids.ID) {
					require.FailNow("unexpectedly connected to a peer")
				},
				DisconnectedF: nil,
			},
		)
		require.NoError(err)
		networks[i] = net
	}

	config := configs[0]
	signer := peer.NewIPSigner(config.MyIPPort, config.TLSKey, config.BLSKey)
	ip, err := signer.GetSignedIP()
	require.NoError(err)

	eg := &errgroup.Group{}
	for i, net := range networks {
		if i != 0 {
			stakingCert, err := staking.ParseCertificate(config.TLSConfig.Certificates[0].Leaf.Raw)
			require.NoError(err)

			require.NoError(net.Track([]*ips.ClaimedIPPort{
				ips.NewClaimedIPPort(
					stakingCert,
					ip.AddrPort,
					ip.Timestamp,
					ip.TLSSignature,
				),
			}))
		}

		eg.Go(net.Dispatch)
	}

	// Give the dialer time to run one iteration. This is racy, but should ony
	// be possible to flake as a false negative (test passes when it shouldn't).
	time.Sleep(50 * time.Millisecond)

	network := networks[1].(*network)
	require.NoError(vdrs.RemoveWeight(constants.PrimaryNetworkID, nodeIDs[0], 1))
	require.Eventually(
		func() bool {
			network.peersLock.RLock()
			defer network.peersLock.RUnlock()

			nodeID := nodeIDs[0]
			_, ok := network.trackedIPs[nodeID]
			return !ok
		},
		10*time.Second,
		50*time.Millisecond,
	)

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestDialDeletesNonValidators(t *testing.T) {
	t.Run("connectToAllValidators=false", func(t *testing.T) {
		testDialDeletesNonValidators(t, false)
	})
	t.Run("connectToAllValidators=true", func(t *testing.T) {
		testDialDeletesNonValidators(t, true)
	})
}

// Test that cancelling the context passed into dial
// causes dial to return immediately.
func TestDialContext(t *testing.T) {
	require := require.New(t)

	_, networks, eg := newFullyConnectedTestNetwork(t, []router.InboundHandler{nil})
	dialer := newTestDialer()
	network := networks[0]
	network.dialer = dialer

	var (
		neverDialedNodeID = ids.GenerateTestNodeID()
		dialedNodeID      = ids.GenerateTestNodeID()

		neverDialedIP, neverDialedListener = dialer.NewListener()
		dialedIP, dialedListener           = dialer.NewListener()

		neverDialedTrackedIP = &trackedIP{
			ip: neverDialedIP,
		}
		dialedTrackedIP = &trackedIP{
			ip: dialedIP,
		}
	)

	network.ManuallyTrack(neverDialedNodeID, neverDialedIP)
	network.ManuallyTrack(dialedNodeID, dialedIP)

	// Sanity check that when a non-cancelled context is given,
	// we actually dial the peer.
	network.dial(dialedNodeID, dialedTrackedIP)

	gotDialedIPConn := make(chan struct{})
	go func() {
		_, _ = dialedListener.Accept()
		close(gotDialedIPConn)
	}()
	<-gotDialedIPConn

	// Asset that when [n.onCloseCtx] is cancelled, dial returns immediately.
	// That is, [neverDialedListener] doesn't accept a connection.
	network.onCloseCtxCancel()
	network.dial(neverDialedNodeID, neverDialedTrackedIP)

	gotNeverDialedIPConn := make(chan struct{})
	go func() {
		_, _ = neverDialedListener.Accept()
		close(gotNeverDialedIPConn)
	}()

	select {
	case <-gotNeverDialedIPConn:
		require.FailNow("unexpectedly connected to peer")
	default:
	}

	network.StartClose()
	require.NoError(eg.Wait())
}

func TestAllowConnectionAsAValidator(t *testing.T) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, 2)

	networks := make([]Network, len(configs))
	for i, config := range configs {
		msgCreator := newMessageCreator(t)
		registry := prometheus.NewRegistry()

		beacons := validators.NewManager()
		require.NoError(beacons.AddStaker(constants.PrimaryNetworkID, nodeIDs[0], nil, ids.GenerateTestID(), 1))

		vdrs := validators.NewManager()
		require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, nodeIDs[0], nil, ids.GenerateTestID(), 1))

		config.Beacons = beacons
		config.Validators = vdrs
		config.RequireValidatorToConnect = true

		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			msgCreator,
			registry,
			logging.NoLog{},
			listeners[i],
			dialer,
			&testHandler{
				InboundHandler: nil,
				ConnectedF:     nil,
				DisconnectedF:  nil,
			},
		)
		require.NoError(err)
		networks[i] = net
	}

	eg := &errgroup.Group{}
	for i, net := range networks {
		if i != 0 {
			config := configs[0]
			net.ManuallyTrack(config.MyNodeID, config.MyIPPort.Get())
		}

		eg.Go(net.Dispatch)
	}

	network := networks[1].(*network)
	require.Eventually(
		func() bool {
			network.peersLock.RLock()
			defer network.peersLock.RUnlock()

			nodeID := nodeIDs[0]
			_, contains := network.connectedPeers.GetByID(nodeID)
			return contains
		},
		10*time.Second,
		50*time.Millisecond,
	)

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestGetAllPeers(t *testing.T) {
	require := require.New(t)

	// Create a non-validator peer
	dialer, listeners, nonVdrNodeIDs, configs := newTestNetwork(t, 1)

	configs[0].Beacons = validators.NewManager()
	configs[0].Validators = validators.NewManager()
	nonValidatorNetwork, err := NewNetwork(
		configs[0],
		upgrade.InitiallyActiveTime,
		newMessageCreator(t),
		prometheus.NewRegistry(),
		logging.NoLog{},
		listeners[0],
		dialer,
		&testHandler{
			InboundHandler: nil,
			ConnectedF:     nil,
			DisconnectedF:  nil,
		},
	)
	require.NoError(err)

	// Create a network of validators
	nodeIDs, networks, eg := newFullyConnectedTestNetwork(
		t,
		[]router.InboundHandler{
			nil, nil, nil,
		},
	)

	// Connect the non-validator peer to the validator network
	nonValidatorNetwork.ManuallyTrack(networks[0].config.MyNodeID, networks[0].config.MyIPPort.Get())
	eg.Go(nonValidatorNetwork.Dispatch)

	{
		// The non-validator peer should be able to get all the peers in the network
		peersListFromNonVdr := networks[0].Peers(nonVdrNodeIDs[0], nil, true, bloom.EmptyFilter, []byte{})
		require.Len(peersListFromNonVdr, len(nodeIDs)-1)
		peerNodes := set.NewSet[ids.NodeID](len(peersListFromNonVdr))
		for _, peer := range peersListFromNonVdr {
			peerNodes.Add(peer.NodeID)
		}
		for _, nodeID := range nodeIDs[1:] {
			require.True(peerNodes.Contains(nodeID))
		}
	}

	{
		// A validator peer should be able to get all the peers in the network
		peersListFromVdr := networks[0].Peers(nodeIDs[1], nil, true, bloom.EmptyFilter, []byte{})
		require.Len(peersListFromVdr, len(nodeIDs)-2) // GetPeerList doesn't return the peer that requested it
		peerNodes := set.NewSet[ids.NodeID](len(peersListFromVdr))
		for _, peer := range peersListFromVdr {
			peerNodes.Add(peer.NodeID)
		}
		for _, nodeID := range nodeIDs[2:] {
			require.True(peerNodes.Contains(nodeID))
		}
	}

	nonValidatorNetwork.StartClose()
	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}
