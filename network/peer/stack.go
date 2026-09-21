// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// MessageStack holds the per-peer P2P resources a connection is started on:
// its frame size and the codec and throttlers bounded by it. Which stack a
// connection gets follows the peer's subnet membership.
type MessageStack struct {
	MaxFrameSize        uint32
	MessageCreator      message.Creator
	InboundMsgThrottler throttling.InboundMsgThrottler
	OutboundThrottler   throttling.OutboundMsgThrottler
}

// NewTestMessageStack returns a default stack for tests.
func NewTestMessageStack(messageCreator message.Creator) MessageStack {
	return MessageStack{
		MaxFrameSize:        constants.DefaultMaxMessageSize,
		MessageCreator:      messageCreator,
		InboundMsgThrottler: throttling.NewNoInboundThrottler(),
		OutboundThrottler:   throttling.NewNoOutboundThrottler(),
	}
}
