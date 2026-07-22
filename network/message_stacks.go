// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

const largeMessageMetricsPrefix = "large_message_"

// MessageStacks holds default and elevated per-peer P2P resource stacks.
type MessageStacks struct {
	allowlist       set.Set[ids.NodeID]
	elevatedEnabled bool
	Default         peer.MessageStack
	Elevated        peer.MessageStack
}

// MsgCreator returns the node-wide message creator for outbound consensus
// traffic. When the elevated stack is enabled it returns the large creator so
// the sender can build payloads above the default max size; per-peer frame size
// enforcement at write time still rejects oversized frames for non-allowlisted
// peers. For payloads within the default size both creators produce identical
// bytes.
func (s *MessageStacks) MsgCreator() message.Creator {
	if !s.elevatedEnabled {
		return s.Default.MessageCreator
	}
	return s.Elevated.MessageCreator
}

// Resolve returns the stack for [nodeID].
func (s *MessageStacks) Resolve(nodeID ids.NodeID) peer.MessageStack {
	if s.elevatedEnabled && s.allowlist.Contains(nodeID) {
		return s.Elevated
	}
	return s.Default
}

func newMessageStacks(
	log logging.Logger,
	registerer prometheus.Registerer,
	vdrs validators.Manager,
	config *Config,
) (*MessageStacks, error) {
	defaultCreator, err := message.NewCreator(
		registerer,
		config.CompressionType,
		config.MaximumInboundMessageTimeout,
		int64(constants.DefaultMaxMessageSize),
	)
	if err != nil {
		return nil, fmt.Errorf("initializing default message creator: %w", err)
	}

	defaultInbound, err := throttling.NewInboundMsgThrottler(
		log,
		registerer,
		vdrs,
		config.ThrottlerConfig.InboundMsgThrottlerConfig,
		config.ResourceTracker,
		config.CPUTargeter,
		config.DiskTargeter,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing default inbound message throttler: %w", err)
	}

	defaultOutbound, err := throttling.NewSybilOutboundMsgThrottler(
		log,
		registerer,
		vdrs,
		config.ThrottlerConfig.OutboundMsgThrottlerConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing default outbound message throttler: %w", err)
	}

	stacks := &MessageStacks{
		allowlist: config.LargeMessageConfig.Allowlist,
		Default: peer.MessageStack{
			MaxFrameSize:        constants.DefaultMaxMessageSize,
			MessageCreator:      defaultCreator,
			InboundMsgThrottler: defaultInbound,
			OutboundThrottler:   defaultOutbound,
		},
	}

	if !config.LargeMessageConfig.Enabled() {
		return stacks, nil
	}

	stacks.elevatedEnabled = true
	largeRegisterer := prometheus.WrapRegistererWithPrefix(largeMessageMetricsPrefix, registerer)

	allowlistLen := config.LargeMessageConfig.Allowlist.Len()
	maxSize := uint64(config.LargeMessageConfig.MaxMessageSize)
	atLargeAllocSize := maxSize * uint64(allowlistLen)
	log.Warn(
		"large message config enabled; throttler limits for allowlisted peers are derived from network-max-message-size and override throttler-inbound-node-max-at-large-bytes, throttler-inbound-bandwidth-max-burst-size, throttler-inbound-at-large-alloc-size, throttler-outbound-node-max-at-large-bytes, and throttler-outbound-at-large-alloc-size",
		zap.Uint32("maxMessageSize", config.LargeMessageConfig.MaxMessageSize),
		zap.Int("allowlistedPeers", allowlistLen),
		zap.Uint64("inboundNodeMaxAtLargeBytes", maxSize),
		zap.Uint64("inboundBandwidthMaxBurstSize", maxSize),
		zap.Uint64("inboundAtLargeAllocSize", atLargeAllocSize),
		zap.Uint64("outboundNodeMaxAtLargeBytes", maxSize),
		zap.Uint64("outboundAtLargeAllocSize", atLargeAllocSize),
	)

	largeCreator, err := message.NewCreator(
		largeRegisterer,
		config.CompressionType,
		config.MaximumInboundMessageTimeout,
		int64(config.LargeMessageConfig.MaxMessageSize),
	)
	if err != nil {
		return nil, fmt.Errorf("initializing large message creator: %w", err)
	}

	largeInboundConfig := elevatedInboundThrottlerConfig(
		config.ThrottlerConfig.InboundMsgThrottlerConfig,
		maxSize,
		allowlistLen,
	)
	largeInbound, err := throttling.NewInboundMsgThrottler(
		log,
		largeRegisterer,
		vdrs,
		largeInboundConfig,
		config.ResourceTracker,
		config.CPUTargeter,
		config.DiskTargeter,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing large inbound message throttler: %w", err)
	}

	largeOutboundConfig := elevatedOutboundThrottlerConfig(
		config.ThrottlerConfig.OutboundMsgThrottlerConfig,
		maxSize,
		allowlistLen,
	)
	largeOutbound, err := throttling.NewSybilOutboundMsgThrottler(
		log,
		largeRegisterer,
		vdrs,
		largeOutboundConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing large outbound message throttler: %w", err)
	}

	stacks.Elevated = peer.MessageStack{
		MaxFrameSize:        config.LargeMessageConfig.MaxMessageSize,
		MessageCreator:      largeCreator,
		InboundMsgThrottler: largeInbound,
		OutboundThrottler:   largeOutbound,
	}
	return stacks, nil
}

func elevatedInboundThrottlerConfig(
	base throttling.InboundMsgThrottlerConfig,
	maxSize uint64,
	allowlistLen int,
) throttling.InboundMsgThrottlerConfig {
	cfg := base
	cfg.MsgByteThrottlerConfig.NodeMaxAtLargeBytes = maxSize
	cfg.BandwidthThrottlerConfig.MaxBurstSize = maxSize
	cfg.MsgByteThrottlerConfig.AtLargeAllocSize = maxSize * uint64(allowlistLen)
	return cfg
}

func elevatedOutboundThrottlerConfig(
	base throttling.MsgByteThrottlerConfig,
	maxSize uint64,
	allowlistLen int,
) throttling.MsgByteThrottlerConfig {
	cfg := base
	cfg.NodeMaxAtLargeBytes = maxSize
	cfg.AtLargeAllocSize = maxSize * uint64(allowlistLen)
	return cfg
}
