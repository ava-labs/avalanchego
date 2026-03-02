// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Engine describes the standard interface of a consensus engine.
//
// All nodeIDs are assumed to be authenticated.
//
// A consensus engine may recover after returning an error, but it isn't
// required.
type Engine interface {
	Handler

	// Start engine operations from given request ID
	Start(ctx context.Context, startReqID uint32) error

	// Returns nil if the engine is healthy.
	// Periodically called and reported through the health API
	health.Checker
}

type Handler interface {
	AllGetsServer
	StateSummaryFrontierHandler
	AcceptedStateSummaryHandler
	AcceptedFrontierHandler
	AcceptedHandler
	AncestorsHandler
	PutHandler
	QueryHandler
	ChitsHandler
	AppHandler
	InternalHandler
}

type AllGetsServer interface {
	GetStateSummaryFrontierHandler
	GetAcceptedStateSummaryHandler
	GetAcceptedFrontierHandler
	GetAcceptedHandler
	GetAncestorsHandler
	GetHandler
}

type GetStateSummaryFrontierHandler interface {
	// Notify this engine of a request for a StateSummaryFrontier message with
	// the same requestID and the engine's most recently accepted state summary.
	//
	// This function can be called by any node at any time.
	GetStateSummaryFrontier(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type StateSummaryFrontierHandler interface {
	// Notify this engine of the response to a previously sent
	// GetStateSummaryFrontier message with the same requestID.
	//
	// It is not guaranteed that the summary bytes are from a valid state
	// summary.
	StateSummaryFrontier(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		summary []byte,
	) error

	// Notify this engine that a GetStateSummaryFrontier request it issued has
	// failed.
	//
	// This function will be called if a GetStateSummaryFrontier message with
	// nodeID and requestID was previously sent by this engine and will not
	// receive a response.
	GetStateSummaryFrontierFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type GetAcceptedStateSummaryHandler interface {
	// Notify this engine of a request for an AcceptedStateSummary message with
	// the same requestID and the state summary IDs at the requested heights.
	// If this node doesn't have access to a state summary ID at a requested
	// height, that height should be ignored.
	//
	// This function can be called by any node at any time.
	GetAcceptedStateSummary(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		heights set.Set[uint64],
	) error
}

type AcceptedStateSummaryHandler interface {
	// Notify this engine of the response to a previously sent
	// GetAcceptedStateSummary message with the same requestID.
	//
	// It is not guaranteed that the summaryIDs have heights corresponding to
	// the heights in the request.
	AcceptedStateSummary(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		summaryIDs set.Set[ids.ID],
	) error

	// Notify this engine that a GetAcceptedStateSummary request it issued has
	// failed.
	//
	// This function will be called if a GetAcceptedStateSummary message with
	// nodeID and requestID was previously sent by this engine and will not
	// receive a response.
	GetAcceptedStateSummaryFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type GetAcceptedFrontierHandler interface {
	// Notify this engine of a request for an AcceptedFrontier message with the
	// same requestID and the ID of the most recently accepted container.
	//
	// This function can be called by any node at any time.
	GetAcceptedFrontier(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type AcceptedFrontierHandler interface {
	// Notify this engine of the response to a previously sent
	// GetAcceptedFrontier message with the same requestID.
	AcceptedFrontier(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		containerID ids.ID,
	) error

	// Notify this engine that a GetAcceptedFrontier request it issued has
	// failed.
	//
	// This function will be called if a GetAcceptedFrontier message with
	// nodeID and requestID was previously sent by this engine and will not
	// receive a response.
	GetAcceptedFrontierFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type GetAcceptedHandler interface {
	// Notify this engine of a request for an Accepted message with the same
	// requestID and the subset of containerIDs that this node has accepted.
	//
	// This function can be called by any node at any time.
	GetAccepted(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		containerIDs set.Set[ids.ID],
	) error
}

type AcceptedHandler interface {
	// Notify this engine of the response to a previously sent GetAccepted
	// message with the same requestID.
	//
	// It is not guaranteed that the containerIDs are a subset of the
	// containerIDs provided in the request.
	Accepted(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		containerIDs set.Set[ids.ID],
	) error

	// Notify this engine that a GetAccepted request it issued has failed.
	//
	// This function will be called if a GetAccepted message with nodeID and
	// requestID was previously sent by this engine and will not receive a
	// response.
	GetAcceptedFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type GetAncestorsHandler interface {
	// Notify this engine of a request for an Ancestors message with the same
	// requestID, containerID, and some of its ancestors on a best effort basis.
	//
	// This function can be called by any node at any time.
	GetAncestors(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		containerID ids.ID,
	) error
}

type AncestorsHandler interface {
	// Notify this engine of the response to a previously sent GetAncestors
	// message with the same requestID.
	//
	// It is expected, but not guaranteed, that the first element in containers
	// should be the container referenced in the request and that the rest of
	// the containers should be referenced by a prior container in the list.
	Ancestors(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		containers [][]byte,
	) error

	// Notify this engine that a GetAncestors request it issued has failed.
	//
	// This function will be called if a GetAncestors message with nodeID and
	// requestID was previously sent by this engine and will not receive a
	// response.
	GetAncestorsFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type GetHandler interface {
	// Notify this engine of a request for a Put message with the same requestID
	// and the container whose ID is containerID.
	//
	// This function can be called by any node at any time.
	Get(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		containerID ids.ID,
	) error
}

type PutHandler interface {
	// Notify this engine of either the response to a previously sent Get
	// message with the same requestID or an unsolicited container if the
	// requestID is MaxUint32.
	//
	// It is not guaranteed that container can be parsed or issued.
	Put(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		container []byte,
	) error

	// Notify this engine that a Get request it issued has failed.
	//
	// This function will be called if a Get message with nodeID and requestID
	// was previously sent by this engine and will not receive a response.
	GetFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type QueryHandler interface {
	// Notify this engine of a request for a Chits message with the same
	// requestID.
	//
	// If the provided containerID is not processing, the engine is expected to
	// respond with the node's current preferences before attempting to issue
	// it.
	//
	// This function can be called by any node at any time.
	PullQuery(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		containerID ids.ID,
		requestedHeight uint64,
	) error

	// Notify this engine of a request for a Chits message with the same
	// requestID.
	//
	// If the provided container is not processing, the engine is expected to
	// respond with the node's current preferences before attempting to issue
	// it.
	//
	// It is not guaranteed that container can be parsed or issued.
	//
	// This function can be called by any node at any time.
	PushQuery(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		container []byte,
		requestedHeight uint64,
	) error
}

type ChitsHandler interface {
	// Notify this engine of the response to a previously sent PullQuery or
	// PushQuery message with the same requestID.
	//
	// It is expected, but not guaranteed, that preferredID transitively
	// references preferredIDAtHeight and acceptedID.
	Chits(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		preferredID ids.ID,
		preferredIDAtHeight ids.ID,
		acceptedID ids.ID,
		acceptedHeight uint64,
	) error

	// Notify this engine that a Query request it issued has failed.
	//
	// This function will be called if a PullQuery or PushQuery message with
	// nodeID and requestID was previously sent by this engine and will not
	// receive a response.
	QueryFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
	) error
}

type AppHandler interface {
	AppRequestHandler
	AppResponseHandler
	AppGossipHandler
}

type AppRequestHandler interface {
	// Notify this engine of a request for an AppResponse with the same
	// requestID.
	//
	// The meaning of request, and what should be sent in response to it, is
	// application (VM) specific.
	//
	// It is not guaranteed that request is well-formed or valid.
	//
	// This function can be called by any node at any time.
	AppRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		deadline time.Time,
		request []byte,
	) error
}

type AppResponseHandler interface {
	// Notify this engine of the response to a previously sent AppRequest with
	// the same requestID.
	//
	// The meaning of response is application (VM) specific.
	//
	// It is not guaranteed that response is well-formed or valid.
	AppResponse(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		response []byte,
	) error

	// Notify this engine that an AppRequest it issued has failed.
	//
	// This function will be called if an AppRequest message with nodeID and
	// requestID was previously sent by this engine and will not receive a
	// response.
	AppRequestFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		appErr *AppError,
	) error
}

type AppGossipHandler interface {
	// Notify this engine of a gossip message from nodeID.
	//
	// The meaning of msg is application (VM) specific, and the VM defines how
	// to react to this message.
	//
	// This message is not expected in response to any event, and it does not
	// need to be responded to.
	AppGossip(
		ctx context.Context,
		nodeID ids.NodeID,
		msg []byte,
	) error
}

type InternalHandler interface {
	// Notify this engine of peer changes.
	validators.Connector

	// Gossip to the network a container on the accepted frontier
	Gossip(context.Context) error

	// Shutdown this engine.
	//
	// This function will be called when the environment is exiting.
	Shutdown(context.Context) error

	// Notify this engine of a message from the virtual machine.
	Notify(context.Context, Message) error
}
