// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
	"crypto/tls"
	"net/netip"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

// HealthConfig describes parameters for network layer health checks.
type HealthConfig struct {
	// Marks if the health check should be enabled
	Enabled bool `json:"-"`

	// NoIngressValidatorConnectionGracePeriod denotes the time after which the health check fails
	// for primary network validators with no ingress connections.
	NoIngressValidatorConnectionGracePeriod time.Duration

	// MinConnectedPeers is the minimum number of peers that the network should
	// be connected to be considered healthy.
	MinConnectedPeers uint `json:"minConnectedPeers"`

	// MaxTimeSinceMsgReceived is the maximum amount of time since the network
	// last received a message to be considered healthy.
	MaxTimeSinceMsgReceived time.Duration `json:"maxTimeSinceMsgReceived"`

	// MaxTimeSinceMsgSent is the maximum amount of time since the network last
	// sent a message to be considered healthy.
	MaxTimeSinceMsgSent time.Duration `json:"maxTimeSinceMsgSent"`

	// MaxPortionSendQueueBytesFull is the maximum percentage of the pending
	// send byte queue that should be used for the network to be considered
	// healthy. Should be in (0,1].
	MaxPortionSendQueueBytesFull float64 `json:"maxPortionSendQueueBytesFull"`

	// MaxSendFailRate is the maximum percentage of send attempts that should be
	// failing for the network to be considered healthy. This does not include
	// send attempts that were not made due to benching. Should be in [0,1].
	MaxSendFailRate float64 `json:"maxSendFailRate"`

	// SendFailRateHalflife is the halflife of the averager used to calculate
	// the send fail rate percentage. Should be > 0. Larger values mean that the
	// fail rate is affected less by recently dropped messages.
	SendFailRateHalflife time.Duration `json:"sendFailRateHalflife"`
}

type PeerListGossipConfig struct {
	// PeerListNumValidatorIPs is the number of validator IPs to gossip in every
	// gossip event.
	PeerListNumValidatorIPs uint32 `json:"peerListNumValidatorIPs"`

	// PeerListPullGossipFreq is the frequency that this node will attempt to
	// request signed IPs from its peers.
	PeerListPullGossipFreq time.Duration `json:"peerListPullGossipFreq"`

	// PeerListBloomResetFreq is how frequently this node will recalculate the
	// IP tracker's bloom filter.
	PeerListBloomResetFreq time.Duration `json:"peerListBloomResetFreq"`
}

type TimeoutConfig struct {
	// PingPongTimeout is the maximum amount of time to wait for a Pong response
	// from a peer we sent a Ping to.
	PingPongTimeout time.Duration `json:"pingPongTimeout"`

	// ReadHandshakeTimeout is the maximum amount of time to wait for the peer's
	// connection upgrade to finish before starting the p2p handshake.
	ReadHandshakeTimeout time.Duration `json:"readHandshakeTimeout"`
}

type DelayConfig struct {
	// InitialReconnectDelay is the minimum amount of time the node will delay a
	// reconnection to a peer. This value is used to start the exponential
	// backoff.
	InitialReconnectDelay time.Duration `json:"initialReconnectDelay"`

	// MaxReconnectDelay is the maximum amount of time the node will delay a
	// reconnection to a peer.
	MaxReconnectDelay time.Duration `json:"maxReconnectDelay"`
}

type ThrottlerConfig struct {
	InboundConnUpgradeThrottlerConfig throttling.InboundConnUpgradeThrottlerConfig `json:"inboundConnUpgradeThrottlerConfig"`
	InboundMsgThrottlerConfig         throttling.InboundMsgThrottlerConfig         `json:"inboundMsgThrottlerConfig"`
	OutboundMsgThrottlerConfig        throttling.MsgByteThrottlerConfig            `json:"outboundMsgThrottlerConfig"`
	MaxInboundConnsPerSec             float64                                      `json:"maxInboundConnsPerSec"`
}

type Config struct {
	HealthConfig         `json:"healthConfig"`
	PeerListGossipConfig `json:"peerListGossipConfig"`
	TimeoutConfig        `json:"timeoutConfigs"`
	DelayConfig          `json:"delayConfig"`
	ThrottlerConfig      ThrottlerConfig `json:"throttlerConfig"`

	ProxyEnabled           bool          `json:"proxyEnabled"`
	ProxyReadHeaderTimeout time.Duration `json:"proxyReadHeaderTimeout"`

	DialerConfig dialer.Config `json:"dialerConfig"`
	TLSConfig    *tls.Config   `json:"-"`

	TLSKeyLogFile string `json:"tlsKeyLogFile"`

	MyNodeID           ids.NodeID                    `json:"myNodeID"`
	MyIPPort           *utils.Atomic[netip.AddrPort] `json:"myIP"`
	NetworkID          uint32                        `json:"networkID"`
	MaxClockDifference time.Duration                 `json:"maxClockDifference"`
	PingFrequency      time.Duration                 `json:"pingFrequency"`
	AllowPrivateIPs    bool                          `json:"allowPrivateIPs"`

	SupportedACPs set.Set[uint32] `json:"supportedACPs"`
	ObjectedACPs  set.Set[uint32] `json:"objectedACPs"`

	// The compression type to use when compressing outbound messages.
	// Assumes all peers support this compression type.
	CompressionType compression.Type `json:"compressionType"`

	// TLSKey is this node's TLS key that is used to sign IPs.
	TLSKey crypto.Signer `json:"-"`
	// BLSKey is this node's BLS key that is used to sign IPs.
	BLSKey bls.Signer `json:"-"`

	// TrackedSubnets of the node.
	// It must not include the primary network ID.
	TrackedSubnets set.Set[ids.ID]    `json:"-"`
	Beacons        validators.Manager `json:"-"`

	// Validators are the current validators in the Avalanche network
	Validators validators.Manager `json:"-"`

	UptimeCalculator uptime.Calculator `json:"-"`

	// UptimeMetricFreq marks how frequently this node will recalculate the
	// observed average uptime metrics.
	UptimeMetricFreq time.Duration `json:"uptimeMetricFreq"`

	// UptimeRequirement is the fraction of time a validator must be online and
	// responsive for us to vote that they should receive a staking reward.
	UptimeRequirement float64 `json:"-"`

	// RequireValidatorToConnect require that all connections must have at least
	// one validator between the 2 peers. This can be useful to enable if the
	// node wants to connect to the minimum number of nodes without impacting
	// the network negatively.
	RequireValidatorToConnect bool `json:"requireValidatorToConnect"`

	// MaximumInboundMessageTimeout is the maximum deadline duration in a
	// message. Messages sent by clients setting values higher than this value
	// will be reset to this value.
	MaximumInboundMessageTimeout time.Duration `json:"maximumInboundMessageTimeout"`

	// Size, in bytes, of the buffer that we read peer messages into
	// (there is one buffer per peer)
	PeerReadBufferSize int `json:"peerReadBufferSize"`

	// Size, in bytes, of the buffer that we write peer messages into
	// (there is one buffer per peer)
	PeerWriteBufferSize int `json:"peerWriteBufferSize"`

	// Tracks the CPU/disk usage caused by processing messages of each peer.
	ResourceTracker tracker.ResourceTracker `json:"-"`

	// Specifies how much CPU usage each peer can cause before
	// we rate-limit them.
	CPUTargeter tracker.Targeter `json:"-"`

	// Specifies how much disk usage each peer can cause before
	// we rate-limit them.
	DiskTargeter tracker.Targeter `json:"-"`

	// If true, connects to all validators regardless of primary network validator
	// status or of configured tracked subnets.
	ConnectToAllValidators bool `json:"connectToAllValidators"`
}
