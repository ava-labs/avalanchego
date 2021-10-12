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

	// Send a message to a specific node
	Send(msg message.OutboundMessage, nodeIDs ids.ShortSet) ids.ShortSet

	// gossip message to set of nodes in a given subnet.
	// nodes to targed can be explicitly provided (nodeIDs non empty) or
	// they can be sampled by Gossip implementation (validatorOnly participates this sampling)
	Gossip(msg message.OutboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) bool
}
