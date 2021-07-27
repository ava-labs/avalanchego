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
	AppSender
}

// FrontierSender defines how a consensus engine sends frontier messages to
// other validators
type FrontierSender interface {
	// SendGetAcceptedFrontier requests that every validator in [validatorIDs] sends
	// an AcceptedFrontier message.
	SendGetAcceptedFrontier(validatorIDs ids.ShortSet, requestID uint32)

	// SendAcceptedFrontier responds to a AcceptedFrontier message with this
	// engine's current accepted frontier.
	SendAcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID)
}

// AcceptedSender defines how a consensus engine sends messages pertaining to
// accepted containers
type AcceptedSender interface {
	// SendGetAccepted requests that every validator in [validatorIDs] sends an
	// Accepted message with all the IDs in [containerIDs] that the validator
	// thinks is accepted.
	SendGetAccepted(validatorIDs ids.ShortSet, requestID uint32, containerIDs []ids.ID)

	// SendAccepted responds to a GetAccepted message with a set of IDs of
	// containers that are accepted.
	SendAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID)
}

// FetchSender defines how a consensus engine sends retrieval messages to other
// validators
type FetchSender interface {
	// Request a container from a validator.
	// Request that the specified validator send the specified container
	// to this validator
	SendGet(validatorID ids.ShortID, requestID uint32, containerID ids.ID)

	// SendGetAncestors requests that the validator with ID [validatorID] send container [containerID] and its
	// ancestors. The maximum number of ancestors to send in response is defined in snow/engine/common/bootstrapper.go
	SendGetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID)

	// Tell the specified validator that the container whose ID is <containerID>
	// has body <container>
	SendPut(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte)

	// Give the specified validator several containers at once
	// Should be in response to a GetAncestors message with request ID [requestID] from the validator
	SendMultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte)
}

// QuerySender defines how a consensus engine sends query messages to other
// validators
type QuerySender interface {
	// Request from the specified validators their preferred frontier, given the
	// existence of the specified container.
	// This is the same as PullQuery, except that this message includes not only
	// the ID of the container but also its body.
	SendPushQuery(validatorIDs ids.ShortSet, requestID uint32, containerID ids.ID, container []byte)

	// Request from the specified validators their preferred frontier, given the
	// existence of the specified container.
	SendPullQuery(validatorIDs ids.ShortSet, requestID uint32, containerID ids.ID)

	// Send chits to the specified node
	SendChits(validatorID ids.ShortID, requestID uint32, votes []ids.ID)
}

// Gossiper defines how a consensus engine gossips a container on the accepted
// frontier to other validators
type Gossiper interface {
	// Gossip the provided container throughout the network
	SendGossip(containerID ids.ID, container []byte)
}

// Sends app-level messages
type AppSender interface {
	// Send an application-level request
	SendAppRequest(nodeIDs ids.ShortSet, requestID uint32, appRequestBytes []byte)
	SendAppResponse(nodeIDs ids.ShortID, requestID uint32, appResponseBytes []byte)
	SendAppGossip(nodeIDs ids.ShortSet, requestID uint32, appGossipBytes []byte)
}
