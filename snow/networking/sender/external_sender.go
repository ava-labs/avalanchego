// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// ExternalSender sends consensus messages to other validators
// Right now this is implemented in the networking package
type ExternalSender interface {
	GetMsgBuilder() message.Builder // TODO ABENEGIA: remove once a better place for msg builder is found
	IsCompressionEnabled() bool     // TODO ABENEGIA: remove once this config is duly propagated to sender and network

	Send(msgType constants.MsgType, msg message.OutboundMessage, nodeIDs ids.ShortSet) ids.ShortSet
	Gossip(msgType constants.MsgType, msg message.OutboundMessage, subnetID ids.ID) bool
}
