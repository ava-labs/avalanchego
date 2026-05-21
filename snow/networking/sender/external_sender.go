// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/message"
	"github.com/MetalBlockchain/metalgo/snow/engine/common"
	"github.com/MetalBlockchain/metalgo/subnets"
	"github.com/MetalBlockchain/metalgo/utils/set"
)

// ExternalSender sends consensus messages to other validators
// Right now this is implemented in the networking package
type ExternalSender interface {
	Send(
		msg *message.OutboundMessage,
		config common.SendConfig,
		subnetID ids.ID,
		allower subnets.Allower,
	) set.Set[ids.NodeID]
}
