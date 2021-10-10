// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
)

// ExternalSender sends consensus messages to other validators
// Right now this is implemented in the networking package
type ExternalSender interface {
	Send(msg message.OutboundMessage, nodeIDs ids.ShortSet) ids.ShortSet
	Gossip(msg message.OutboundMessage, subnetID ids.ID, validatorOnly bool) bool
	SpecificGossip(msg message.OutboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) bool
}
