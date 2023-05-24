// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
	"errors"
	"math"
	"net"
	"runtime"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
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
	"github.com/ava-labs/avalanchego/utils/units"
)

var (
	errClosed = errors.New("closed")

	_ net.Listener    = (*noopListener)(nil)
	_ subnets.Allower = (*nodeIDConnector)(nil)
)

type noopListener struct {
	once   sync.Once
	closed chan struct{}
}

func newNoopListener() net.Listener {
	return &noopListener{
		closed: make(chan struct{}),
	}
}

func (l *noopListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, errClosed
}

func (l *noopListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (*noopListener) Addr() net.Addr {
	return &net.TCPAddr{
		IP:   net.IPv4zero,
		Port: 0,
	}
}

func NewTestNetwork(
	log logging.Logger,
	networkID uint32,
	currentValidators validators.Set,
	trackedSubnets set.Set[ids.ID],
	router router.ExternalHandler,
) (Network, error) {
	metrics := prometheus.NewRegistry()
	msgCreator, err := message.NewCreator(
		metrics,
		"",
		constants.DefaultNetworkCompressionEnabled,
		constants.DefaultNetworkMaximumInboundTimeout,
	)
	if err != nil {
		return nil, err
	}

	networkConfig := Config{
		ThrottlerConfig: ThrottlerConfig{
			InboundConnUpgradeThrottlerConfig: throttling.InboundConnUpgradeThrottlerConfig{
				UpgradeCooldown:        constants.DefaultInboundConnUpgradeThrottlerCooldown,
				MaxRecentConnsUpgraded: int(math.Ceil(constants.DefaultInboundThrottlerMaxConnsPerSec * constants.DefaultInboundConnUpgradeThrottlerCooldown.Seconds())),
			},

			InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
				MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
					VdrAllocSize:        constants.DefaultInboundThrottlerVdrAllocSize,
					AtLargeAllocSize:    constants.DefaultInboundThrottlerAtLargeAllocSize,
					NodeMaxAtLargeBytes: constants.DefaultInboundThrottlerNodeMaxAtLargeBytes,
				},

				BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
					RefillRate:   constants.DefaultInboundThrottlerBandwidthRefillRate,
					MaxBurstSize: constants.DefaultInboundThrottlerBandwidthMaxBurstSize,
				},

				CPUThrottlerConfig: throttling.SystemThrottlerConfig{
					MaxRecheckDelay: constants.DefaultInboundThrottlerCPUMaxRecheckDelay,
				},

				DiskThrottlerConfig: throttling.SystemThrottlerConfig{
					MaxRecheckDelay: constants.DefaultInboundThrottlerDiskMaxRecheckDelay,
				},

				MaxProcessingMsgsPerNode: constants.DefaultInboundThrottlerMaxProcessingMsgsPerNode,
			},
			OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
				VdrAllocSize:        constants.DefaultOutboundThrottlerVdrAllocSize,
				AtLargeAllocSize:    constants.DefaultOutboundThrottlerAtLargeAllocSize,
				NodeMaxAtLargeBytes: constants.DefaultOutboundThrottlerNodeMaxAtLargeBytes,
			},

			MaxInboundConnsPerSec: constants.DefaultInboundThrottlerMaxConnsPerSec,
		},

		HealthConfig: HealthConfig{
			Enabled:                      true,
			MinConnectedPeers:            constants.DefaultNetworkHealthMinPeers,
			MaxTimeSinceMsgReceived:      constants.DefaultNetworkHealthMaxTimeSinceMsgReceived,
			MaxTimeSinceMsgSent:          constants.DefaultNetworkHealthMaxTimeSinceMsgSent,
			MaxPortionSendQueueBytesFull: constants.DefaultNetworkHealthMaxPortionSendQueueFill,
			MaxSendFailRate:              constants.DefaultNetworkHealthMaxSendFailRate,
			SendFailRateHalflife:         constants.DefaultHealthCheckAveragerHalflife,
		},

		ProxyEnabled:           constants.DefaultNetworkTCPProxyEnabled,
		ProxyReadHeaderTimeout: constants.DefaultNetworkTCPProxyReadTimeout,

		DialerConfig: dialer.Config{
			ThrottleRps:       constants.DefaultOutboundConnectionThrottlingRps,
			ConnectionTimeout: constants.DefaultOutboundConnectionTimeout,
		},

		TimeoutConfig: TimeoutConfig{
			PingPongTimeout:      constants.DefaultPingPongTimeout,
			ReadHandshakeTimeout: constants.DefaultNetworkReadHandshakeTimeout,
		},

		PeerListGossipConfig: PeerListGossipConfig{
			PeerListNumValidatorIPs:        constants.DefaultNetworkPeerListNumValidatorIPs,
			PeerListValidatorGossipSize:    constants.DefaultNetworkPeerListValidatorGossipSize,
			PeerListNonValidatorGossipSize: constants.DefaultNetworkPeerListNonValidatorGossipSize,
			PeerListPeersGossipSize:        constants.DefaultNetworkPeerListPeersGossipSize,
			PeerListGossipFreq:             constants.DefaultNetworkPeerListGossipFreq,
		},

		DelayConfig: DelayConfig{
			InitialReconnectDelay: constants.DefaultNetworkInitialReconnectDelay,
			MaxReconnectDelay:     constants.DefaultNetworkMaxReconnectDelay,
		},

		MaxClockDifference:           constants.DefaultNetworkMaxClockDifference,
		CompressionEnabled:           constants.DefaultNetworkCompressionEnabled,
		PingFrequency:                constants.DefaultPingFrequency,
		AllowPrivateIPs:              constants.DefaultNetworkAllowPrivateIPs,
		UptimeMetricFreq:             constants.DefaultUptimeMetricFreq,
		MaximumInboundMessageTimeout: constants.DefaultNetworkMaximumInboundTimeout,

		RequireValidatorToConnect: constants.DefaultNetworkRequireValidatorToConnect,
		PeerReadBufferSize:        constants.DefaultNetworkPeerReadBufferSize,
		PeerWriteBufferSize:       constants.DefaultNetworkPeerWriteBufferSize,
	}

	networkConfig.NetworkID = networkID
	networkConfig.TrackedSubnets = trackedSubnets

	tlsCert, err := staking.NewTLSCert()
	if err != nil {
		return nil, err
	}
	tlsConfig := peer.TLSConfig(*tlsCert, nil)
	networkConfig.TLSConfig = tlsConfig
	networkConfig.TLSKey = tlsCert.PrivateKey.(crypto.Signer)

	validatorManager := validators.NewManager()
	beacons := validators.NewSet()
	networkConfig.Validators = validatorManager
	networkConfig.Validators.Add(constants.PrimaryNetworkID, currentValidators)
	networkConfig.Beacons = beacons
	// This never actually does anything because we never initialize the P-chain
	networkConfig.UptimeCalculator = uptime.NoOpCalculator

	// TODO actually monitor usage
	// TestNetwork doesn't use disk so we don't need to track it, but we should
	// still have guardrails around cpu/memory usage.
	networkConfig.ResourceTracker, err = tracker.NewResourceTracker(
		metrics,
		resource.NoUsage,
		&meter.ContinuousFactory{},
		constants.DefaultHealthCheckAveragerHalflife,
	)
	if err != nil {
		return nil, err
	}
	networkConfig.CPUTargeter = tracker.NewTargeter(
		&tracker.TargeterConfig{
			VdrAlloc:           float64(runtime.NumCPU()),
			MaxNonVdrUsage:     .8 * float64(runtime.NumCPU()),
			MaxNonVdrNodeUsage: float64(runtime.NumCPU()) / 8,
		},
		currentValidators,
		networkConfig.ResourceTracker.CPUTracker(),
	)
	networkConfig.DiskTargeter = tracker.NewTargeter(
		&tracker.TargeterConfig{
			VdrAlloc:           1000 * units.GiB,
			MaxNonVdrUsage:     1000 * units.GiB,
			MaxNonVdrNodeUsage: 1000 * units.GiB,
		},
		currentValidators,
		networkConfig.ResourceTracker.DiskTracker(),
	)

	networkConfig.MyIPPort = ips.NewDynamicIPPort(net.IPv4zero, 0)

	networkConfig.GossipTracker, err = peer.NewGossipTracker(metrics, "")
	if err != nil {
		return nil, err
	}

	return NewNetwork(
		&networkConfig,
		msgCreator,
		metrics,
		log,
		newNoopListener(),
		dialer.NewDialer(
			constants.NetworkType,
			dialer.Config{
				ThrottleRps:       constants.DefaultOutboundConnectionThrottlingRps,
				ConnectionTimeout: constants.DefaultOutboundConnectionTimeout,
			},
			log,
		),
		router,
	)
}

type nodeIDConnector struct {
	nodeID ids.NodeID
}

func newNodeIDConnector(nodeID ids.NodeID) *nodeIDConnector {
	return &nodeIDConnector{nodeID: nodeID}
}

func (f *nodeIDConnector) IsAllowed(nodeID ids.NodeID, _ bool) bool {
	return nodeID == f.nodeID
}
