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
)

const largeMessageMetricsPrefix = "large_message_"

// MessageStacks holds default and elevated per-peer P2P resource stacks.
type MessageStacks struct {
	largeMessageConfig LargeMessageConfig
	Default            peer.MessageStack
	Elevated           peer.MessageStack
}

// MsgCreator returns the node-wide message creator for outbound consensus
// traffic. When the elevated stack is enabled it returns the large creator so
// the sender can build payloads above the default max size; per-peer frame size
// enforcement at write time still rejects oversized frames for unselected
// peers. For payloads within the default size both creators produce identical
// bytes.
func (s *MessageStacks) MsgCreator() message.Creator {
	if !s.largeMessageConfig.Enabled {
		return s.Default.MessageCreator
	}
	return s.Elevated.MessageCreator
}

// Resolve returns the stack for [nodeID].
func (s *MessageStacks) Resolve(nodeID ids.NodeID) peer.MessageStack {
	if s.largeMessageConfig.Enabled && s.largeMessageConfig.AppliesTo(nodeID) {
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
		largeMessageConfig: config.LargeMessageConfig,
		Default: peer.MessageStack{
			MaxFrameSize:        constants.DefaultMaxMessageSize,
			MessageCreator:      defaultCreator,
			InboundMsgThrottler: defaultInbound,
			OutboundThrottler:   defaultOutbound,
		},
	}

	if !config.LargeMessageConfig.Enabled {
		return stacks, nil
	}

	largeRegisterer := prometheus.WrapRegistererWithPrefix(largeMessageMetricsPrefix, registerer)

	throttler := config.LargeMessageConfig.Throttler
	largeInboundConfig := throttler.InboundMsgThrottlerConfig
	largeOutboundConfig := throttler.OutboundMsgThrottlerConfig
	log.Warn(
		"large message config enabled",
		zap.Uint32("maxMessageSize", config.LargeMessageConfig.MaxMessageSize),
		zap.Bool("allowAllPeers", config.LargeMessageConfig.AllowAll),
		zap.Int("allowlistedPeers", config.LargeMessageConfig.Allowlist.Len()),
		zap.Uint64("inboundNodeMaxAtLargeBytes", largeInboundConfig.MsgByteThrottlerConfig.NodeMaxAtLargeBytes),
		zap.Uint64("inboundValidatorAllocSize", largeInboundConfig.MsgByteThrottlerConfig.VdrAllocSize),
		zap.Uint64("inboundBandwidthRefillRate", largeInboundConfig.BandwidthThrottlerConfig.RefillRate),
		zap.Uint64("inboundBandwidthMaxBurstSize", largeInboundConfig.BandwidthThrottlerConfig.MaxBurstSize),
		zap.Uint64("inboundAtLargeAllocSize", largeInboundConfig.MsgByteThrottlerConfig.AtLargeAllocSize),
		zap.Uint64("outboundNodeMaxAtLargeBytes", largeOutboundConfig.NodeMaxAtLargeBytes),
		zap.Uint64("outboundValidatorAllocSize", largeOutboundConfig.VdrAllocSize),
		zap.Uint64("outboundAtLargeAllocSize", largeOutboundConfig.AtLargeAllocSize),
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
