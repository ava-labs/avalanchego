// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// ExternalSender sends consensus messages to other validators
// Right now this is implemented in the networking package
type ExternalSender interface {
	Send(msgType constants.MsgType, msg message.OutboundMessage, nodeIDs ids.ShortSet) ids.ShortSet
	Gossip(msgType constants.MsgType, msg message.OutboundMessage, subnetID ids.ID) bool
}
