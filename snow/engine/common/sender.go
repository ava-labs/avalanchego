// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// SendConfig is used to specify who to send messages to over the p2p network.
type SendConfig struct {
	NodeIDs set.Set[ids.NodeID]
	// If Validators >= number of connected validators, the message will be sent to all
	// connected validators.
	Validators int
	// If NonValidators >= number of connected non-validators, the message will be sent to
	// all connected non-validators.
	NonValidators int
	// If Peers >= number of connected peers, the message will be sent to all connected peers.
	Peers int
}

// Sender defines how a consensus engine sends messages and requests to other
// validators.
//
// Messages can be categorized as either: requests, responses, or gossip. Gossip
// messages do not include requestIDs, because no response is expected from the
// peer. However, both requests and responses include requestIDs.
//
// It is expected that each [nodeID + requestID + expected response type] that
// is outstanding at any given time is unique.
//
// As an example, it is valid to send `Get(nodeA, request0)` and
// `PullQuery(nodeA, request0)` because they have different expected response
// types, `Put` and `Chits`.
//
// Additionally, after having sent `Get(nodeA, request0)` and receiving either
// `Put(nodeA, request0)` or `GetFailed(nodeA, request0)`, it is valid to resend
// `Get(nodeA, request0)`. Because the initial `Get` request is no longer
// outstanding.
//
// This means that requestIDs can be reused. In practice, requests always have a
// reasonable maximum timeout, so it is generally safe to assume that by the
// time the requestID space has been exhausted, the beginning of the requestID
// space is free of conflicts.
type Sender interface {
	StateSummarySender
	AcceptedStateSummarySender
	FrontierSender
	AcceptedSender
	FetchSender
	QuerySender
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
		containerID ids.ID,
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
	SendPushQuery(
		ctx context.Context,
		nodeIDs set.Set[ids.NodeID],
		requestID uint32,
		container []byte,
		requestedHeight uint64,
	)

	// Request from the specified nodes their preferred frontier, given the
	// existence of the specified container.
	SendPullQuery(
		ctx context.Context,
		nodeIDs set.Set[ids.NodeID],
		requestID uint32,
		containerID ids.ID,
		requestedHeight uint64,
	)

	// Send chits to the specified node
	SendChits(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		preferredID ids.ID,
		preferredIDAtHeight ids.ID,
		acceptedID ids.ID,
		acceptedHeight uint64,
	)
}

// AppSender sends VM-level messages to nodes in the network.
type AppSender interface {
	// Send an application-level request.
	//
	// The VM corresponding to this AppSender may receive either:
	// * An AppResponse from nodeID with ID [requestID]
	// * An AppRequestFailed from nodeID with ID [requestID]
	//
	// A nil return value guarantees that the VM corresponding to this AppSender
	// will receive exactly one of the above messages.
	//
	// A non-nil return value guarantees that the VM corresponding to this
	// AppSender will receive at most one of the above messages.
	SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error
	// Send an application-level response to a request.
	// This response must be in response to an AppRequest that the VM corresponding
	// to this AppSender received from [nodeID] with ID [requestID].
	SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error
	// SendAppError sends an application-level error to an AppRequest
	SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error
	// Gossip an application-level message.
	SendAppGossip(
		ctx context.Context,
		config SendConfig,
		appGossipBytes []byte,
	) error
}
