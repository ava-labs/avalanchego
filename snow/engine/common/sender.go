// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
// other nodes.
type FrontierSender interface {
	// SendGetAcceptedFrontier requests that every node in [nodeIDs] sends an
	// AcceptedFrontier message.
	SendGetAcceptedFrontier(nodeIDs ids.ShortSet, requestID uint32)

	// SendAcceptedFrontier responds to a AcceptedFrontier message with this
	// engine's current accepted frontier.
	SendAcceptedFrontier(
		nodeID ids.ShortID,
		requestID uint32,
		containerIDs []ids.ID,
	)
}

// AcceptedSender defines how a consensus engine sends messages pertaining to
// accepted containers
type AcceptedSender interface {
	// SendGetAccepted requests that every node in [nodeIDs] sends an Accepted
	// message with all the IDs in [containerIDs] that the node thinks are
	// accepted.
	SendGetAccepted(
		nodeIDs ids.ShortSet,
		requestID uint32,
		containerIDs []ids.ID,
	)

	// SendAccepted responds to a GetAccepted message with a set of IDs of
	// containers that are accepted.
	SendAccepted(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID)
}

// FetchSender defines how a consensus engine sends retrieval messages to other
// nodes.
type FetchSender interface {
	// Request that the specified node send the specified container to this
	// node.
	SendGet(nodeID ids.ShortID, requestID uint32, containerID ids.ID)

	// SendGetAncestors requests that node [nodeID] send container [containerID]
	// and its ancestors.
	SendGetAncestors(nodeID ids.ShortID, requestID uint32, containerID ids.ID)

	// Tell the specified node that the container whose ID is [containerID] has
	// body [container].
	SendPut(
		nodeID ids.ShortID,
		requestID uint32,
		containerID ids.ID,
		container []byte,
	)

	// Give the specified node several containers at once. Should be in response
	// to a GetAncestors message with request ID [requestID] from the node.
	SendAncestors(nodeID ids.ShortID, requestID uint32, containers [][]byte)
}

// QuerySender defines how a consensus engine sends query messages to other
// nodes.
type QuerySender interface {
	// Request from the specified nodes their preferred frontier, given the
	// existence of the specified container.
	// This is the same as PullQuery, except that this message includes not only
	// the ID of the container but also its body.
	SendPushQuery(
		nodeIDs ids.ShortSet,
		requestID uint32,
		containerID ids.ID,
		container []byte,
	)

	// Request from the specified nodes their preferred frontier, given the
	// existence of the specified container.
	SendPullQuery(nodeIDs ids.ShortSet, requestID uint32, containerID ids.ID)

	// Send chits to the specified node
	SendChits(nodeID ids.ShortID, requestID uint32, votes []ids.ID)
}

// Gossiper defines how a consensus engine gossips a container on the accepted
// frontier to other nodes
type Gossiper interface {
	// Gossip the provided container throughout the network
	SendGossip(containerID ids.ID, container []byte)
}

// AppSender sends application (VM) level messages.
// See also common.AppHandler.
type AppSender interface {
	// Send an application-level request.
	// A nil return value guarantees that for each nodeID in [nodeIDs],
	// the VM corresponding to this AppSender eventually receives either:
	// * An AppResponse from nodeID with ID [requestID]
	// * An AppRequestFailed from nodeID with ID [requestID]
	// Exactly one of the above messages will eventually be received per nodeID.
	// A non-nil error should be considered fatal.
	SendAppRequest(nodeIDs ids.ShortSet, requestID uint32, appRequestBytes []byte) error
	// Send an application-level response to a request.
	// This response must be in response to an AppRequest that the VM corresponding
	// to this AppSender received from [nodeID] with ID [requestID].
	// A non-nil error should be considered fatal.
	SendAppResponse(nodeID ids.ShortID, requestID uint32, appResponseBytes []byte) error
	// Gossip an application-level message.
	// A non-nil error should be considered fatal.
	SendAppGossip(appGossipBytes []byte) error
	SendAppGossipSpecific(nodeIDs ids.ShortSet, appGossipBytes []byte) error
}
