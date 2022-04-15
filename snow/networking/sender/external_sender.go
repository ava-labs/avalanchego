// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/message"
)

// ExternalSender sends consensus messages to other validators
// Right now this is implemented in the networking package
type ExternalSender interface {
	// Send a message to a specific set of nodes
	Send(
		msg message.OutboundMessage,
		nodeIDs ids.ShortSet,
		subnetID ids.ID,
		validatorOnly bool,
	) ids.ShortSet

	// Send a message to a random group of nodes in a subnet.
	// Nodes are sampled based on their validator status.
	Gossip(
		msg message.OutboundMessage,
		subnetID ids.ID,
		validatorOnly bool,
		numValidatorsToSend int,
		numNonValidatorsToSend int,
		numPeersToSend int,
	) ids.ShortSet
}
