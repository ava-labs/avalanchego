// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/avalanchego/ids"
)

// Sender defines how a consensus engine sends messages and requests to other
// validators
type Sender interface {
	FrontierSender
	AcceptedSender
	FetchSender
	QuerySender
	Gossiper
}

// FrontierSender defines how a consensus engine sends frontier messages to
// other validators
type FrontierSender interface {
	// GetAcceptedFrontier requests that every validator in [validatorIDs] sends
	// an AcceptedFrontier message.
	GetAcceptedFrontier(validatorIDs ids.ShortSet, requestID uint32)

	// AcceptedFrontier responds to a AcceptedFrontier message with this
	// engine's current accepted frontier.
	AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID)
}

// AcceptedSender defines how a consensus engine sends messages pertaining to
// accepted containers
type AcceptedSender interface {
	// GetAccepted requests that every validator in [validatorIDs] sends an
	// Accepted message with all the IDs in [containerIDs] that the validator
	// thinks is accepted.
	GetAccepted(validatorIDs ids.ShortSet, requestID uint32, containerIDs []ids.ID)

	// Accepted responds to a GetAccepted message with a set of IDs of
	// containers that are accepted.
	Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID)
}

// FetchSender defines how a consensus engine sends retrieval messages to other
// validators
type FetchSender interface {
	// Request a container from a validator.
	// Request that the specified validator send the specified container
	// to this validator
	Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID)

	// GetAncestors requests that the validator with ID [validatorID] send container [containerID] and its
	// ancestors. The maximum number of ancestors to send in response is defined in snow/engine/common/bootstrapper.go
	GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID)

	// Tell the specified validator that the container whose ID is <containerID>
	// has body <container>
	Put(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte)

	// Give the specified validator several containers at once
	// Should be in response to a GetAncestors message with request ID [requestID] from the validator
	MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte)
}

// QuerySender defines how a consensus engine sends query messages to other
// validators
type QuerySender interface {
	// Request from the specified validators their preferred frontier, given the
	// existence of the specified container.
	// This is the same as PullQuery, except that this message includes not only
	// the ID of the container but also its body.
	PushQuery(validatorIDs ids.ShortSet, requestID uint32, containerID ids.ID, container []byte)

	// Request from the specified validators their preferred frontier, given the
	// existence of the specified container.
	PullQuery(validatorIDs ids.ShortSet, requestID uint32, containerID ids.ID)

	// Chits sends chits to the specified validator
	Chits(validatorID ids.ShortID, requestID uint32, votes []ids.ID)
}

// Gossiper defines how a consensus engine gossips a container on the accepted
// frontier to other validators
type Gossiper interface {
	// Gossip gossips the provided container throughout the network
	Gossip(containerID ids.ID, container []byte)
}
