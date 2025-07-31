// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// The network must be "tcp", "tcp4", "tcp6", "unix" or "unixpacket".
	NetworkType = "tcp"

	DefaultMaxMessageSize  = 2 * units.MiB
	DefaultPingPongTimeout = 30 * time.Second
	DefaultPingFrequency   = 3 * DefaultPingPongTimeout / 4
	DefaultByteSliceCap    = 128

	MaxContainersLen = int(4 * DefaultMaxMessageSize / 5)

	DefaultNetworkPeerListNumValidatorIPs        = 15
	DefaultNetworkPeerListValidatorGossipSize    = 20
	DefaultNetworkPeerListNonValidatorGossipSize = 0
	DefaultNetworkPeerListPeersGossipSize        = 10
	DefaultNetworkPeerListGossipFreq             = time.Minute
	DefaultNetworkPeerListPullGossipFreq         = 2 * time.Second
	DefaultNetworkPeerListBloomResetFreq         = time.Minute

	// Inbound Connection Throttling
	DefaultInboundConnUpgradeThrottlerCooldown = 10 * time.Second
	DefaultInboundThrottlerMaxConnsPerSec      = 256

	// Outbound Connection Throttling
	DefaultOutboundConnectionThrottlingRps = 50
	DefaultOutboundConnectionTimeout       = 30 * time.Second

	// Timeouts
	DefaultNetworkInitialTimeout                   = 5 * time.Second
	DefaultNetworkMinimumTimeout                   = 2 * time.Second
	DefaultNetworkMaximumTimeout                   = 10 * time.Second
	DefaultNetworkMaximumInboundTimeout            = 10 * time.Second
	DefaultNetworkTimeoutHalflife                  = 5 * time.Minute
	DefaultNetworkTimeoutCoefficient               = 2
	DefaultNetworkReadHandshakeTimeout             = 15 * time.Second
	DefaultNoIngressValidatorConnectionGracePeriod = 10 * time.Minute

	DefaultNetworkCompressionType           = compression.TypeZstd
	DefaultNetworkMaxClockDifference        = time.Minute
	DefaultNetworkRequireValidatorToConnect = false
	DefaultNetworkPeerReadBufferSize        = 8 * units.KiB
	DefaultNetworkPeerWriteBufferSize       = 8 * units.KiB

	DefaultNetworkTCPProxyEnabled = false

	// The PROXY protocol specification recommends setting this value to be at
	// least 3 seconds to cover a TCP retransmit.
	// Ref: https://www.haproxy.org/download/2.3/doc/proxy-protocol.txt
	// Specifying a timeout of 0 will actually result in a timeout of 200ms, but
	// a timeout of 0 should generally not be provided.
	DefaultNetworkTCPProxyReadTimeout = 3 * time.Second

	// Benchlist
	DefaultBenchlistFailThreshold      = 10
	DefaultBenchlistDuration           = 15 * time.Minute
	DefaultBenchlistMinFailingDuration = 2*time.Minute + 30*time.Second

	// Router
	DefaultConsensusAppConcurrency  = 2
	DefaultConsensusShutdownTimeout = time.Minute
	DefaultFrontierPollFrequency    = 100 * time.Millisecond

	// Inbound Throttling
	DefaultInboundThrottlerAtLargeAllocSize         = 6 * units.MiB
	DefaultInboundThrottlerVdrAllocSize             = 32 * units.MiB
	DefaultInboundThrottlerNodeMaxAtLargeBytes      = DefaultMaxMessageSize
	DefaultInboundThrottlerMaxProcessingMsgsPerNode = 1024
	DefaultInboundThrottlerBandwidthRefillRate      = 512 * units.KiB
	DefaultInboundThrottlerBandwidthMaxBurstSize    = DefaultMaxMessageSize
	DefaultInboundThrottlerCPUMaxRecheckDelay       = 5 * time.Second
	DefaultInboundThrottlerDiskMaxRecheckDelay      = 5 * time.Second
	MinInboundThrottlerMaxRecheckDelay              = time.Millisecond

	// Outbound Throttling
	DefaultOutboundThrottlerAtLargeAllocSize    = 32 * units.MiB
	DefaultOutboundThrottlerVdrAllocSize        = 32 * units.MiB
	DefaultOutboundThrottlerNodeMaxAtLargeBytes = DefaultMaxMessageSize

	// Network Health
	DefaultHealthCheckAveragerHalflife = 10 * time.Second

	DefaultNetworkHealthMaxTimeSinceMsgSent     = time.Minute
	DefaultNetworkHealthMaxTimeSinceMsgReceived = time.Minute
	DefaultNetworkHealthMaxPortionSendQueueFill = 0.9
	DefaultNetworkHealthMinPeers                = 1
	DefaultNetworkHealthMaxSendFailRate         = .9

	// Metrics
	DefaultUptimeMetricFreq = 30 * time.Second

	// Delays
	DefaultNetworkInitialReconnectDelay = time.Second
	DefaultNetworkMaxReconnectDelay     = time.Minute
)
