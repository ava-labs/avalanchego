// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"crypto"
	"crypto/rsa"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
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
		PeerListNumValidatorIPs:        100,
		PeerListValidatorGossipSize:    100,
		PeerListNonValidatorGossipSize: 100,
		PeerListPeersGossipSize:        100,
		PeerListGossipFreq:             time.Second,
		PeerListPullGossipFreq:         time.Second,
		PeerListBloomResetFreq:         constants.DefaultNetworkPeerListBloomResetFreq,
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

		Namespace:          "",
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

func newTestNetwork(t *testing.T, count int) (*testDialer, []*testListener, []ids.NodeID, []*Config) {
	var (
		dialer    = newTestDialer()
		listeners = make([]*testListener, count)
		nodeIDs   = make([]ids.NodeID, count)
		configs   = make([]*Config, count)
	)
	for i := 0; i < count; i++ {
		ip, listener := dialer.NewListener()
		nodeID, tlsCert, tlsConfig := getTLS(t, i)

		config := defaultConfig
		config.TLSConfig = tlsConfig
		config.MyNodeID = nodeID
		config.MyIPPort = ip
		config.TLSKey = tlsCert.PrivateKey.(crypto.Signer)

		listeners[i] = listener
		nodeIDs[i] = nodeID
		configs[i] = &config
	}
	return dialer, listeners, nodeIDs, configs
}

func newMessageCreator(t *testing.T) message.Creator {
	t.Helper()

	mc, err := message.NewCreator(
		logging.NoLog{},
		prometheus.NewRegistry(),
		"",
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)

	return mc
}

func newFullyConnectedTestNetwork(t *testing.T, handlers []router.InboundHandler) ([]ids.NodeID, []*network, *sync.WaitGroup) {
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

		config := config

		config.Beacons = beacons
		config.Validators = vdrs

		var connected set.Set[ids.NodeID]
		net, err := NewNetwork(
			config,
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

	wg := sync.WaitGroup{}
	wg.Add(len(networks))
	for i, net := range networks {
		if i != 0 {
			config := configs[0]
			net.ManuallyTrack(config.MyNodeID, config.MyIPPort.IPPort())
		}

		go func(net Network) {
			defer wg.Done()

			require.NoError(net.Dispatch())
		}(net)
	}

	if len(networks) > 1 {
		<-onAllConnected
	}

	return nodeIDs, networks, &wg
}

func TestNewNetwork(t *testing.T) {
	_, networks, wg := newFullyConnectedTestNetwork(t, []router.InboundHandler{nil, nil, nil})
	for _, net := range networks {
		net.StartClose()
	}
	wg.Wait()
}

func TestSend(t *testing.T) {
	require := require.New(t)

	received := make(chan message.InboundMessage)
	nodeIDs, networks, wg := newFullyConnectedTestNetwork(
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
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	require.NoError(err)

	toSend := set.Of(nodeIDs[1])
	sentTo := net0.Send(outboundGetMsg, toSend, constants.PrimaryNetworkID, subnets.NoOpAllower)
	require.Equal(toSend, sentTo)

	inboundGetMsg := <-received
	require.Equal(message.GetOp, inboundGetMsg.Op())

	for _, net := range networks {
		net.StartClose()
	}
	wg.Wait()
}

func TestSendAndGossipWithFilter(t *testing.T) {
	require := require.New(t)

	received := make(chan message.InboundMessage)
	nodeIDs, networks, wg := newFullyConnectedTestNetwork(
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
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	require.NoError(err)

	toSend := set.Of(nodeIDs...)
	validNodeID := nodeIDs[1]
	sentTo := net0.Send(outboundGetMsg, toSend, constants.PrimaryNetworkID, newNodeIDConnector(validNodeID))
	require.Len(sentTo, 1)
	require.Contains(sentTo, validNodeID)

	inboundGetMsg := <-received
	require.Equal(message.GetOp, inboundGetMsg.Op())

	// Test Gossip now
	sentTo = net0.Gossip(outboundGetMsg, constants.PrimaryNetworkID, 0, 0, len(nodeIDs), newNodeIDConnector(validNodeID))
	require.Len(sentTo, 1)
	require.Contains(sentTo, validNodeID)

	inboundGetMsg = <-received
	require.Equal(message.GetOp, inboundGetMsg.Op())

	for _, net := range networks {
		net.StartClose()
	}
	wg.Wait()
}

func TestTrackVerifiesSignatures(t *testing.T) {
	require := require.New(t)

	_, networks, wg := newFullyConnectedTestNetwork(t, []router.InboundHandler{nil})

	network := networks[0]
	nodeID, tlsCert, _ := getTLS(t, 1)
	require.NoError(network.config.Validators.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.Empty, 1))

	err := network.Track([]*ips.ClaimedIPPort{
		ips.NewClaimedIPPort(
			staking.CertificateFromX509(tlsCert.Leaf),
			ips.IPPort{
				IP:   net.IPv4(123, 132, 123, 123),
				Port: 10000,
			},
			1000, // timestamp
			nil,  // signature
		),
	})
	// The signature is wrong so this peer tracking info isn't useful.
	require.ErrorIs(err, rsa.ErrVerification)

	network.peersLock.RLock()
	require.Empty(network.trackedIPs)
	network.peersLock.RUnlock()

	for _, net := range networks {
		net.StartClose()
	}
	wg.Wait()
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

		config := config

		config.Beacons = beacons
		config.Validators = vdrs
		config.AllowPrivateIPs = false

		net, err := NewNetwork(
			config,
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

	wg := sync.WaitGroup{}
	wg.Add(len(networks))
	for i, net := range networks {
		if i != 0 {
			config := configs[0]
			net.ManuallyTrack(config.MyNodeID, config.MyIPPort.IPPort())
		}

		go func(net Network) {
			defer wg.Done()

			require.NoError(net.Dispatch())
		}(net)
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
	wg.Wait()
}

func TestDialDeletesNonValidators(t *testing.T) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, 2)

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

		config := config

		config.Beacons = beacons
		config.Validators = vdrs
		config.AllowPrivateIPs = false

		net, err := NewNetwork(
			config,
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
	signer := peer.NewIPSigner(config.MyIPPort, config.TLSKey)
	ip, err := signer.GetSignedIP()
	require.NoError(err)

	wg := sync.WaitGroup{}
	wg.Add(len(networks))
	for i, net := range networks {
		if i != 0 {
			err := net.Track([]*ips.ClaimedIPPort{
				ips.NewClaimedIPPort(
					staking.CertificateFromX509(config.TLSConfig.Certificates[0].Leaf),
					ip.IPPort,
					ip.Timestamp,
					ip.Signature,
				),
			})
			require.NoError(err)
		}

		go func(net Network) {
			defer wg.Done()

			require.NoError(net.Dispatch())
		}(net)
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
	wg.Wait()
}

// Test that cancelling the context passed into dial
// causes dial to return immediately.
func TestDialContext(t *testing.T) {
	_, networks, wg := newFullyConnectedTestNetwork(t, []router.InboundHandler{nil})

	dialer := newTestDialer()
	network := networks[0]
	network.dialer = dialer

	var (
		neverDialedNodeID = ids.GenerateTestNodeID()
		dialedNodeID      = ids.GenerateTestNodeID()

		dynamicNeverDialedIP, neverDialedListener = dialer.NewListener()
		dynamicDialedIP, dialedListener           = dialer.NewListener()

		neverDialedIP = &trackedIP{
			ip: dynamicNeverDialedIP.IPPort(),
		}
		dialedIP = &trackedIP{
			ip: dynamicDialedIP.IPPort(),
		}
	)

	network.ManuallyTrack(neverDialedNodeID, neverDialedIP.ip)
	network.ManuallyTrack(dialedNodeID, dialedIP.ip)

	// Sanity check that when a non-cancelled context is given,
	// we actually dial the peer.
	network.dial(dialedNodeID, dialedIP)

	gotDialedIPConn := make(chan struct{})
	go func() {
		_, _ = dialedListener.Accept()
		close(gotDialedIPConn)
	}()
	<-gotDialedIPConn

	// Asset that when [n.onCloseCtx] is cancelled, dial returns immediately.
	// That is, [neverDialedListener] doesn't accept a connection.
	network.onCloseCtxCancel()
	network.dial(neverDialedNodeID, neverDialedIP)

	gotNeverDialedIPConn := make(chan struct{})
	go func() {
		_, _ = neverDialedListener.Accept()
		close(gotNeverDialedIPConn)
	}()

	select {
	case <-gotNeverDialedIPConn:
		require.FailNow(t, "unexpectedly connected to peer")
	default:
	}

	network.StartClose()
	wg.Wait()
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

		config := config

		config.Beacons = beacons
		config.Validators = vdrs
		config.RequireValidatorToConnect = true

		net, err := NewNetwork(
			config,
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

	wg := sync.WaitGroup{}
	wg.Add(len(networks))
	for i, net := range networks {
		if i != 0 {
			config := configs[0]
			net.ManuallyTrack(config.MyNodeID, config.MyIPPort.IPPort())
		}

		go func(net Network) {
			defer wg.Done()

			require.NoError(net.Dispatch())
		}(net)
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
	wg.Wait()
}
