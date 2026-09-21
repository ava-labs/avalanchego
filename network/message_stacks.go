// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const largeMessageMetricsPrefix = "large_message_"

var errTooManyLargeMessageSubnets = errors.New("only one tracked subnet may declare largeMessages; the node builds a single elevated message stack")

// MessageStacks holds the default and, when a tracked subnet declares
// largeMessages, the elevated per-peer P2P resource stack. Which one a
// connection gets is decided per peer by [network.stackFor].
type MessageStacks struct {
	Default peer.MessageStack

	// elevated is only populated when a tracked subnet declares largeMessages.
	elevated elevatedStack
}

// elevatedStack is the stack granted to members of [subnetID]. hasElevated
// reports whether the node built it; the other fields are only meaningful when
// it is true.
type elevatedStack struct {
	hasElevated  bool
	subnetID     ids.ID
	messageStack peer.MessageStack
}

// MsgCreator returns the node-wide message creator for outbound consensus
// traffic. It is the elevated creator whenever that stack exists, so that the
// sender can build oversized payloads at all; per-peer frame enforcement at
// write time is what keeps them from reaching an unelevated peer. Within the
// default size both creators produce identical bytes.
func (s *MessageStacks) MsgCreator() message.Creator {
	if !s.elevated.hasElevated {
		return s.Default.MessageCreator
	}
	return s.elevated.messageStack.MessageCreator
}

// largeMessagesSubnet returns the subnet that declares largeMessages, and the
// block it declares. Both are zero when none does.
//
// The node builds a single elevated stack, so at most one subnet may declare
// the block.
func largeMessagesSubnet(
	subnetConfigs map[ids.ID]subnets.Config,
) (ids.ID, *subnets.LargeMessagesConfig, error) {
	var (
		elevatedSubnetID ids.ID
		largeMessages    *subnets.LargeMessagesConfig
	)
	for subnetID, subnetConfig := range subnetConfigs {
		if subnetConfig.LargeMessages == nil {
			continue
		}
		if largeMessages != nil {
			return ids.Empty, nil, fmt.Errorf(
				"%w: %s and %s both declare it",
				errTooManyLargeMessageSubnets,
				elevatedSubnetID,
				subnetID,
			)
		}

		elevatedSubnetID = subnetID
		largeMessages = subnetConfig.LargeMessages
	}
	return elevatedSubnetID, largeMessages, nil
}

func newMessageStacks(
	log logging.Logger,
	registerer prometheus.Registerer,
	vdrs validators.Manager,
	config *Config,
) (*MessageStacks, error) {
	defaultStack, err := newMessageStack(
		log,
		registerer,
		vdrs,
		config,
		constants.DefaultMaxMessageSize,
		config.ThrottlerConfig.InboundMsgThrottlerConfig,
		config.ThrottlerConfig.OutboundMsgThrottlerConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing default message stack: %w", err)
	}

	stacks := &MessageStacks{
		Default: defaultStack,
	}

	elevatedSubnetID, largeMessages, err := largeMessagesSubnet(config.SubnetConfigs)
	if err != nil {
		return nil, err
	}
	if largeMessages == nil {
		return stacks, nil
	}

	throttler := largeMessages.Throttler()
	log.Warn(
		"large message config enabled",
		zap.Stringer("subnetID", elevatedSubnetID),
		zap.Uint32("maxMessageSize", largeMessages.MaxMessageSize),
		zap.Reflect("throttlerConfig", throttler),
	)

	elevated, err := newMessageStack(
		log,
		prometheus.WrapRegistererWithPrefix(largeMessageMetricsPrefix, registerer),
		vdrs,
		config,
		largeMessages.MaxMessageSize,
		throttler.InboundMsgThrottlerConfig,
		throttler.OutboundMsgThrottlerConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing large message stack: %w", err)
	}

	stacks.elevated = elevatedStack{
		hasElevated:  true,
		subnetID:     elevatedSubnetID,
		messageStack: elevated,
	}
	return stacks, nil
}

// newMessageStack builds one per-peer resource stack: a codec bounded by
// [maxFrameSize] and the throttlers that police it.
func newMessageStack(
	log logging.Logger,
	registerer prometheus.Registerer,
	vdrs validators.Manager,
	config *Config,
	maxFrameSize uint32,
	inboundConfig throttling.InboundMsgThrottlerConfig,
	outboundConfig throttling.MsgByteThrottlerConfig,
) (peer.MessageStack, error) {
	creator, err := message.NewCreatorWithMaxMessageSize(
		registerer,
		config.CompressionType,
		config.MaximumInboundMessageTimeout,
		int64(maxFrameSize),
	)
	if err != nil {
		return peer.MessageStack{}, fmt.Errorf("initializing message creator: %w", err)
	}

	inbound, err := throttling.NewInboundMsgThrottler(
		log,
		registerer,
		vdrs,
		inboundConfig,
		config.ResourceTracker,
		config.CPUTargeter,
		config.DiskTargeter,
	)
	if err != nil {
		return peer.MessageStack{}, fmt.Errorf("initializing inbound message throttler: %w", err)
	}

	outbound, err := throttling.NewSybilOutboundMsgThrottler(
		log,
		registerer,
		vdrs,
		outboundConfig,
	)
	if err != nil {
		return peer.MessageStack{}, fmt.Errorf("initializing outbound message throttler: %w", err)
	}

	return peer.MessageStack{
		MaxFrameSize:        maxFrameSize,
		MessageCreator:      creator,
		InboundMsgThrottler: inbound,
		OutboundThrottler:   outbound,
	}, nil
}
