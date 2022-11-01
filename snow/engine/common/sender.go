// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Sender defines how a consensus engine sends messages and requests to other
// validators
type Sender interface {
	snow.Acceptor

	StateSummarySender
	AcceptedStateSummarySender
	FrontierSender
	AcceptedSender
	FetchSender
	QuerySender
	Gossiper
	AppSender
}

// StateSummarySender defines how a consensus engine sends state sync messages to
// other nodes.
type StateSummarySender interface {
	// SendGetStateSummaryFrontier requests that every node in [nodeIDs] sends a
	// StateSummaryFrontier message.
	SendGetStateSummaryFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32)

	// SendStateSummaryFrontier responds to a StateSummaryFrontier message with this
	// engine's current state summary frontier.
	SendStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte)
}

type AcceptedStateSummarySender interface {
	// SendGetAcceptedStateSummary requests that every node in [nodeIDs] sends an
	// AcceptedStateSummary message with all the state summary IDs referenced by [heights]
	// that the node thinks are accepted.
	SendGetAcceptedStateSummary(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, heights []uint64)

	// SendAcceptedStateSummary responds to a AcceptedStateSummary message with a
	// set of summary ids that are accepted.
	SendAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID)
}

// FrontierSender defines how a consensus engine sends frontier messages to
// other nodes.
type FrontierSender interface {
	// SendGetAcceptedFrontier requests that every node in [nodeIDs] sends an
	// AcceptedFrontier message.
	SendGetAcceptedFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32)

	// SendAcceptedFrontier responds to a AcceptedFrontier message with this
	// engine's current accepted frontier.
	SendAcceptedFrontier(
		ctx context.Context,
		nodeID ids.NodeID,
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
		ctx context.Context,
		nodeIDs set.Set[ids.NodeID],
		requestID uint32,
		containerIDs []ids.ID,
	)

	// SendAccepted responds to a GetAccepted message with a set of IDs of
	// containers that are accepted.
	SendAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID)
}

// FetchSender defines how a consensus engine sends retrieval messages to other
// nodes.
type FetchSender interface {
	// Request that the specified node send the specified container to this
	// node.
	SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID)

	// SendGetAncestors requests that node [nodeID] send container [containerID]
	// and its ancestors.
	SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID)

	// Tell the specified node about [container].
	SendPut(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte)

	// Give the specified node several containers at once. Should be in response
	// to a GetAncestors message with request ID [requestID] from the node.
	SendAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte)
}

// QuerySender defines how a consensus engine sends query messages to other
// nodes.
type QuerySender interface {
	// Request from the specified nodes their preferred frontier, given the
	// existence of the specified container.
	// This is the same as PullQuery, except that this message includes the body
	// of the container rather than its ID.
	SendPushQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, container []byte)

	// Request from the specified nodes their preferred frontier, given the
	// existence of the specified container.
	SendPullQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerID ids.ID)

	// Send chits to the specified node
	SendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, votes []ids.ID)
}

// Gossiper defines how a consensus engine gossips a container on the accepted
// frontier to other nodes
type Gossiper interface {
	// Gossip the provided container throughout the network
	SendGossip(ctx context.Context, container []byte)
}

// NetworkAppSender sends VM-level messages to nodes in the network.
type NetworkAppSender interface {
	// Send an application-level request.
	// A nil return value guarantees that for each nodeID in [nodeIDs],
	// the VM corresponding to this AppSender eventually receives either:
	// * An AppResponse from nodeID with ID [requestID]
	// * An AppRequestFailed from nodeID with ID [requestID]
	// Exactly one of the above messages will eventually be received per nodeID.
	// A non-nil error should be considered fatal.
	SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error
	// Send an application-level response to a request.
	// This response must be in response to an AppRequest that the VM corresponding
	// to this AppSender received from [nodeID] with ID [requestID].
	// A non-nil error should be considered fatal.
	SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error
	// Gossip an application-level message.
	// A non-nil error should be considered fatal.
	SendAppGossip(ctx context.Context, appGossipBytes []byte) error
	SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error
}

// CrossChainAppSender sends local VM-level messages to another VM.
type CrossChainAppSender interface {
	// SendCrossChainAppRequest sends an application-level request to a
	// specific chain.
	//
	// A nil return value guarantees that the VM corresponding to this
	// CrossChainAppSender eventually receives either:
	// * A CrossChainAppResponse from [chainID] with ID [requestID]
	// * A CrossChainAppRequestFailed from [chainID] with ID [requestID]
	// Exactly one of the above messages will eventually be received from
	// [chainID].
	// A non-nil error should be considered fatal.
	SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error
	// SendCrossChainAppResponse sends an application-level response to a
	// specific chain
	//
	// This response must be in response to a CrossChainAppRequest that the VM
	// corresponding to this CrossChainAppSender received from [chainID] with ID
	// [requestID].
	// A non-nil error should be considered fatal.
	SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error
}

// AppSender sends application (VM) level messages.
// See also common.AppHandler.
type AppSender interface {
	NetworkAppSender
	CrossChainAppSender
}
