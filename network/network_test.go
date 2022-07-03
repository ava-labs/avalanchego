// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
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
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
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

		CompressionEnabled: true,

		UptimeCalculator:  uptime.NewManager(uptime.NewTestState()),
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
		&tracker.TargeterConfig{
			VdrAlloc:           10,
			MaxNonVdrUsage:     10,
			MaxNonVdrNodeUsage: 10,
		},
		validators.NewSet(),
		t,
	)
}

func newDefaultResourceTracker() tracker.ResourceTracker {
	tracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, 10*time.Second)
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
		prometheus.NewRegistry(),
		true,
		"",
		10*time.Second,
	)
	assert.NoError(t, err)
	return mc
}

func newFullyConnectedTestNetwork(t *testing.T, handlers []router.InboundHandler) ([]ids.NodeID, []Network, *sync.WaitGroup) {
	assert := assert.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, len(handlers))

	beacons := validators.NewSet()
	err := beacons.AddWeight(nodeIDs[0], 1)
	assert.NoError(err)

	vdrs := validators.NewManager()
	for _, nodeID := range nodeIDs {
		err := vdrs.AddWeight(constants.PrimaryNetworkID, nodeID, 1)
		assert.NoError(err)
	}

	msgCreator := newMessageCreator(t)

	var (
		networks = make([]Network, len(configs))

		globalLock     sync.Mutex
		numConnected   int
		allConnected   bool
		onAllConnected = make(chan struct{})
	)
	for i, config := range configs {
		config := config

		config.Beacons = beacons
		config.Validators = vdrs

		var connected ids.NodeIDSet
		net, err := NewNetwork(
			config,
			msgCreator,
			prometheus.NewRegistry(),
			logging.NoLog{},
			listeners[i],
			dialer,
			&testHandler{
				InboundHandler: handlers[i],
				ConnectedF: func(nodeID ids.NodeID, _ *version.Application, _ ids.ID) {
					t.Logf("%s connected to %s", config.MyNodeID, nodeID)

					globalLock.Lock()
					defer globalLock.Unlock()

					assert.False(connected.Contains(nodeID))
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

					assert.True(connected.Contains(nodeID))
					connected.Remove(nodeID)
					numConnected--
				},
			},
			benchlist.NewManager(&benchlist.Config{}),
		)
		assert.NoError(err)
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

			err := net.Dispatch()
			assert.NoError(err)
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
	assert := assert.New(t)

	received := make(chan message.InboundMessage)
	nodeIDs, networks, wg := newFullyConnectedTestNetwork(
		t,
		[]router.InboundHandler{
			router.InboundHandlerFunc(func(message.InboundMessage) {
				t.Fatal("unexpected message received")
			}),
			router.InboundHandlerFunc(func(msg message.InboundMessage) {
				received <- msg
			}),
			router.InboundHandlerFunc(func(message.InboundMessage) {
				t.Fatal("unexpected message received")
			}),
		},
	)

	net0 := networks[0]

	mc := newMessageCreator(t)
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty)
	assert.NoError(err)

	toSend := ids.NodeIDSet{}
	toSend.Add(nodeIDs[1])
	sentTo := net0.Send(outboundGetMsg, toSend, constants.PrimaryNetworkID, false)
	assert.EqualValues(toSend, sentTo)

	inboundGetMsg := <-received
	assert.Equal(message.Get, inboundGetMsg.Op())

	for _, net := range networks {
		net.StartClose()
	}
	wg.Wait()
}

func TestTrackVerifiesSignatures(t *testing.T) {
	assert := assert.New(t)

	_, networks, wg := newFullyConnectedTestNetwork(t, []router.InboundHandler{nil})

	network := networks[0].(*network)
	nodeID, tlsCert, _ := getTLS(t, 1)
	err := network.config.Validators.AddWeight(constants.PrimaryNetworkID, nodeID, 1)
	assert.NoError(err)

	useful := network.Track(ips.ClaimedIPPort{
		Cert: tlsCert.Leaf,
		IPPort: ips.IPPort{
			IP:   net.IPv4(123, 132, 123, 123),
			Port: 10000,
		},
		Timestamp: 1000,
		Signature: nil,
	})
	// The signature is wrong so this peer tracking info isn't useful.
	assert.False(useful)

	network.peersLock.RLock()
	assert.Empty(network.trackedIPs)
	network.peersLock.RUnlock()

	for _, net := range networks {
		net.StartClose()
	}
	wg.Wait()
}
