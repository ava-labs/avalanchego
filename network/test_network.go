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
	"github.com/ava-labs/avalanchego/utils/constant"
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
		logging.NoLog{},
		metrics,
		"",
		constant.DefaultNetworkCompressionType,
		constant.DefaultNetworkMaximumInboundTimeout,
	)
	if err != nil {
		return nil, err
	}

	networkConfig := Config{
		ThrottlerConfig: ThrottlerConfig{
			InboundConnUpgradeThrottlerConfig: throttling.InboundConnUpgradeThrottlerConfig{
				UpgradeCooldown:        constant.DefaultInboundConnUpgradeThrottlerCooldown,
				MaxRecentConnsUpgraded: int(math.Ceil(constant.DefaultInboundThrottlerMaxConnsPerSec * constant.DefaultInboundConnUpgradeThrottlerCooldown.Seconds())),
			},

			InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
				MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
					VdrAllocSize:        constant.DefaultInboundThrottlerVdrAllocSize,
					AtLargeAllocSize:    constant.DefaultInboundThrottlerAtLargeAllocSize,
					NodeMaxAtLargeBytes: constant.DefaultInboundThrottlerNodeMaxAtLargeBytes,
				},

				BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
					RefillRate:   constant.DefaultInboundThrottlerBandwidthRefillRate,
					MaxBurstSize: constant.DefaultInboundThrottlerBandwidthMaxBurstSize,
				},

				CPUThrottlerConfig: throttling.SystemThrottlerConfig{
					MaxRecheckDelay: constant.DefaultInboundThrottlerCPUMaxRecheckDelay,
				},

				DiskThrottlerConfig: throttling.SystemThrottlerConfig{
					MaxRecheckDelay: constant.DefaultInboundThrottlerDiskMaxRecheckDelay,
				},

				MaxProcessingMsgsPerNode: constant.DefaultInboundThrottlerMaxProcessingMsgsPerNode,
			},
			OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
				VdrAllocSize:        constant.DefaultOutboundThrottlerVdrAllocSize,
				AtLargeAllocSize:    constant.DefaultOutboundThrottlerAtLargeAllocSize,
				NodeMaxAtLargeBytes: constant.DefaultOutboundThrottlerNodeMaxAtLargeBytes,
			},

			MaxInboundConnsPerSec: constant.DefaultInboundThrottlerMaxConnsPerSec,
		},

		HealthConfig: HealthConfig{
			Enabled:                      true,
			MinConnectedPeers:            constant.DefaultNetworkHealthMinPeers,
			MaxTimeSinceMsgReceived:      constant.DefaultNetworkHealthMaxTimeSinceMsgReceived,
			MaxTimeSinceMsgSent:          constant.DefaultNetworkHealthMaxTimeSinceMsgSent,
			MaxPortionSendQueueBytesFull: constant.DefaultNetworkHealthMaxPortionSendQueueFill,
			MaxSendFailRate:              constant.DefaultNetworkHealthMaxSendFailRate,
			SendFailRateHalflife:         constant.DefaultHealthCheckAveragerHalflife,
		},

		ProxyEnabled:           constant.DefaultNetworkTCPProxyEnabled,
		ProxyReadHeaderTimeout: constant.DefaultNetworkTCPProxyReadTimeout,

		DialerConfig: dialer.Config{
			ThrottleRps:       constant.DefaultOutboundConnectionThrottlingRps,
			ConnectionTimeout: constant.DefaultOutboundConnectionTimeout,
		},

		TimeoutConfig: TimeoutConfig{
			PingPongTimeout:      constant.DefaultPingPongTimeout,
			ReadHandshakeTimeout: constant.DefaultNetworkReadHandshakeTimeout,
		},

		PeerListGossipConfig: PeerListGossipConfig{
			PeerListNumValidatorIPs:        constant.DefaultNetworkPeerListNumValidatorIPs,
			PeerListValidatorGossipSize:    constant.DefaultNetworkPeerListValidatorGossipSize,
			PeerListNonValidatorGossipSize: constant.DefaultNetworkPeerListNonValidatorGossipSize,
			PeerListPeersGossipSize:        constant.DefaultNetworkPeerListPeersGossipSize,
			PeerListGossipFreq:             constant.DefaultNetworkPeerListGossipFreq,
		},

		DelayConfig: DelayConfig{
			InitialReconnectDelay: constant.DefaultNetworkInitialReconnectDelay,
			MaxReconnectDelay:     constant.DefaultNetworkMaxReconnectDelay,
		},

		MaxClockDifference:           constant.DefaultNetworkMaxClockDifference,
		CompressionType:              constant.DefaultNetworkCompressionType,
		PingFrequency:                constant.DefaultPingFrequency,
		AllowPrivateIPs:              !constant.ProductionNetworkIDs.Contains(networkID),
		UptimeMetricFreq:             constant.DefaultUptimeMetricFreq,
		MaximumInboundMessageTimeout: constant.DefaultNetworkMaximumInboundTimeout,

		RequireValidatorToConnect: constant.DefaultNetworkRequireValidatorToConnect,
		PeerReadBufferSize:        constant.DefaultNetworkPeerReadBufferSize,
		PeerWriteBufferSize:       constant.DefaultNetworkPeerWriteBufferSize,
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
	networkConfig.Validators.Add(constant.PrimaryNetworkID, currentValidators)
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
		constant.DefaultHealthCheckAveragerHalflife,
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
			constant.NetworkType,
			dialer.Config{
				ThrottleRps:       constant.DefaultOutboundConnectionThrottlingRps,
				ConnectionTimeout: constant.DefaultOutboundConnectionTimeout,
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
