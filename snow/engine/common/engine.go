// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ error = (*AppError)(nil)

	// ErrUnexpected is a default error used to signal unexpected behavior
	ErrUnexpected = &AppError{
		Code:    0,
		Message: "unexpected error",
	}

	// ErrTimeout is used to signal a response timeout
	ErrTimeout = &AppError{
		Code:    1,
		Message: "timed out",
	}
)

// Engine describes the standard interface of a consensus engine.
//
// All nodeIDs are assumed to be authenticated.
//
// A consensus engine may recover after returning an error, but it isn't
// required.
type Engine interface {
	Handler

	// Return the context of the chain this engine is working on
	Context() *snow.ConsensusContext

	// Start engine operations from given request ID
	Start(ctx context.Context, startReqID uint32) error

	// Returns nil if the engine is healthy.
	// Periodically called and reported through the health API
	health.Checker

	// GetVM returns this engine's VM
	GetVM() VM
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

type NetworkAppHandler interface {
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
	// The meaning of response is application (VM) specifc.
	//
	// It is not guaranteed that response is well-formed or valid.
	AppResponse(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		response []byte,
	) error

	// AppRequestFailed is called when a pending AppRequest failed due to either
	// a timeout or an application-defined reason.
	AppRequestFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		err error,
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

type CrossChainAppHandler interface {
	CrossChainAppRequestHandler
	CrossChainAppResponseHandler
}

type CrossChainAppRequestHandler interface {
	// Notify this engine of a request for a CrossChainAppResponse with the same
	// requestID.
	//
	// The meaning of request, and what should be sent in response to it, is
	// application (VM) specific.
	//
	// Guarantees surrounding the request are specific to the implementation of
	// the requesting VM. For example, the request may or may not be guaranteed
	// to be well-formed/valid depending on the implementation of the requesting
	// VM.
	CrossChainAppRequest(
		ctx context.Context,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		request []byte,
	) error
}

type CrossChainAppResponseHandler interface {
	// Notify this engine of the response to a previously sent
	// CrossChainAppRequest with the same requestID.
	//
	// The meaning of response is application (VM) specifc.
	//
	// Guarantees surrounding the response are specific to the implementation of
	// the responding VM. For example, the response may or may not be guaranteed
	// to be well-formed/valid depending on the implementation of the requesting
	// VM.
	CrossChainAppResponse(
		ctx context.Context,
		chainID ids.ID,
		requestID uint32,
		response []byte,
	) error

	// CrossChainAppRequestFailed is called when a pending CrossChainAppRequest
	// failed due to either a timeout or an application-defined reason.
	CrossChainAppRequestFailed(
		ctx context.Context,
		chainID ids.ID,
		requestID uint32,
		err error,
	) error
}

type AppHandler interface {
	NetworkAppHandler
	CrossChainAppHandler
}

type InternalHandler interface {
	// Notify this engine of peer changes.
	validators.Connector

	// Notify this engine that a registered timeout has fired.
	Timeout(context.Context) error

	// Gossip to the network a container on the accepted frontier
	Gossip(context.Context) error

	// Halt this engine.
	//
	// This function will be called before the environment starts exiting. This
	// function is special, in that it does not expect the chain's context lock
	// to be held before calling this function. This function also does not
	// require the engine to have been started.
	Halt(context.Context)

	// Shutdown this engine.
	//
	// This function will be called when the environment is exiting.
	Shutdown(context.Context) error

	// Notify this engine of a message from the virtual machine.
	Notify(context.Context, Message) error
}

// AppError is an application-defined error
type AppError struct {
	// Code is application-defined and should be used for error matching
	Code uint32
	// Message is a human-readable error message
	Message string
}

func (a *AppError) Error() string {
	return fmt.Sprintf("%d: %s", a.Code, a.Message)
}

func (a *AppError) Is(target error) bool {
	appErr, ok := target.(*AppError)
	if !ok {
		return false
	}

	return a.Code == appErr.Code
}
