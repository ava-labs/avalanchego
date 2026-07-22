// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// MessageStack holds per-peer P2P resources selected by nodeID allowlist.
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
