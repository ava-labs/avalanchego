// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// Engine describes the standard interface of a consensus engine
type Engine interface {
	Handler

	// Return the context of the chain this engine is working on
	Context() *snow.ConsensusContext

	// Start engine operations from given request ID
	Start(startReqID uint32) error

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

// GetStateSummaryFrontierHandler defines how a consensus engine reacts to a get
// state summary frontier message from another validator. Functions only return
// fatal errors.
type GetStateSummaryFrontierHandler interface {
	// Notify this engine of a request for the frontier of state summaries.
	//
	// The accepted frontier is the last state summary available locally.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID.
	//
	// This engine should respond with an StateSummaryFrontier message with the
	// same requestID, and the engine's current state summary frontier.
	GetStateSummaryFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// StateSummaryFrontierHandler defines how a consensus engine reacts to a state
// summary frontier message from other validators. Functions only return fatal
// errors.
type StateSummaryFrontierHandler interface {
	// Notify this engine of a state summary frontier.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is in response to a GetStateSummaryFrontier message, is
	// utilizing a unique requestID, or that the summary bytes are from a valid
	// state summary.
	StateSummaryFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32, summary []byte) error

	// Notify this engine that a get state summary frontier request it issued
	// has failed.
	//
	// This function will be called if the engine sent a GetStateSummaryFrontier
	// message that is not anticipated to be responded to. This could be because
	// the recipient of the message is unknown or if the message request has
	// timed out.
	//
	// The validatorID, and requestID, are assumed to be the same as those sent
	// in the GetStateSummaryFrontier message.
	GetStateSummaryFrontierFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// GetAcceptedStateSummaryHandler defines how a consensus engine reacts to a get
// accepted state summary message from another validator. Functions only return
// fatal errors.
type GetAcceptedStateSummaryHandler interface {
	// Notify this engine of a request to return state summary IDs referenced by
	// the provided keys.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID. However, the validatorID is
	// assumed to be authenticated.
	//
	// This engine should respond with an AcceptedStateSummary message with the
	// same requestID, and the subset of the state summaries that this node has
	// locally available.
	GetAcceptedStateSummary(ctx context.Context, validatorID ids.NodeID, requestID uint32, keys []uint64) error
}

// AcceptedStateSummaryHandler defines how a consensus engine reacts to an
// accepted state summary message from another validator. Functions only return
// fatal errors.
type AcceptedStateSummaryHandler interface {
	// Notify this engine of a set of state summaries.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is in response to a GetAcceptedStateSummary message,
	// is utilizing a unique requestID, or that the summaryIDs are a subset of the
	// state summaries requested by key from a GetAcceptedStateSummary message.
	AcceptedStateSummary(ctx context.Context, validatorID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error

	// Notify this engine that a get accepted state summary request it issued has
	// failed.
	//
	// This function will be called if the engine sent a GetAcceptedStateSummary
	// message that is not anticipated to be responded to. This could be because
	// the recipient of the message is unknown or if the message request has
	// timed out.
	//
	// The validatorID, and requestID, are assumed to be the same as those sent
	// in the GetAcceptedStateSummary message.
	GetAcceptedStateSummaryFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// GetAcceptedFrontierHandler defines how a consensus engine reacts to a get
// accepted frontier message from another validator. Functions only return fatal
// errors.
type GetAcceptedFrontierHandler interface {
	// Notify this engine of a request for the accepted frontier of vertices.
	//
	// The accepted frontier is the set of accepted vertices that do not have
	// any accepted descendants.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID.
	//
	// This engine should respond with an AcceptedFrontier message with the same
	// requestID, and the engine's current accepted frontier.
	GetAcceptedFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// AcceptedFrontierHandler defines how a consensus engine reacts to accepted
// frontier messages from other validators. Functions only return fatal errors.
type AcceptedFrontierHandler interface {
	// Notify this engine of an accepted frontier.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is in response to a GetAcceptedFrontier message, is
	// utilizing a unique requestID, or that the containerIDs from a valid
	// frontier.
	AcceptedFrontier(
		ctx context.Context,
		validatorID ids.NodeID,
		requestID uint32,
		containerIDs []ids.ID,
	) error

	// Notify this engine that a get accepted frontier request it issued has
	// failed.
	//
	// This function will be called if the engine sent a GetAcceptedFrontier
	// message that is not anticipated to be responded to. This could be because
	// the recipient of the message is unknown or if the message request has
	// timed out.
	//
	// The validatorID, and requestID, are assumed to be the same as those sent
	// in the GetAcceptedFrontier message.
	GetAcceptedFrontierFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// GetAcceptedHandler defines how a consensus engine reacts to a get accepted
// message from another validator. Functions only return fatal errors.
type GetAcceptedHandler interface {
	// Notify this engine of a request to filter non-accepted vertices.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID. However, the validatorID is
	// assumed to be authenticated.
	//
	// This engine should respond with an Accepted message with the same
	// requestID, and the subset of the containerIDs that this node has decided
	// are accepted.
	GetAccepted(
		ctx context.Context,
		validatorID ids.NodeID,
		requestID uint32,
		containerIDs []ids.ID,
	) error
}

// AcceptedHandler defines how a consensus engine reacts to accepted messages
// from other validators. Functions only return fatal
// errors.
type AcceptedHandler interface {
	// Notify this engine of a set of accepted vertices.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is in response to a GetAccepted message, is utilizing a
	// unique requestID, or that the containerIDs are a subset of the
	// containerIDs from a GetAccepted message.
	Accepted(
		ctx context.Context,
		validatorID ids.NodeID,
		requestID uint32,
		containerIDs []ids.ID,
	) error

	// Notify this engine that a get accepted request it issued has failed.
	//
	// This function will be called if the engine sent a GetAccepted message
	// that is not anticipated to be responded to. This could be because the
	// recipient of the message is unknown or if the message request has timed
	// out.
	//
	// The validatorID, and requestID, are assumed to be the same as those sent
	// in the GetAccepted message.
	GetAcceptedFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// GetAncestorsHandler defines how a consensus engine reacts to a get ancestors
// message from another validator. Functions only return fatal errors.
type GetAncestorsHandler interface {
	// Notify this engine of a request for a container and its ancestors.
	//
	// The request is from validator [validatorID]. The requested container is
	// [containerID].
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID. It is also not safe to
	// assume the requested containerID exists.
	//
	// This engine should respond with an Ancestors message with the same
	// requestID, which contains [containerID] as well as its ancestors. See
	// Ancestors's documentation.
	//
	// If this engine doesn't have some ancestors, it should reply with its best
	// effort attempt at getting them. If this engine doesn't have [containerID]
	// it can ignore this message.
	GetAncestors(ctx context.Context, validatorID ids.NodeID, requestID uint32, containerID ids.ID) error
}

// AncestorsHandler defines how a consensus engine reacts to bootstrapping
// retrieval messages from other validators. Functions only return fatal errors.
type AncestorsHandler interface {
	// Notify this engine of multiple containers.
	//
	// Each element of [containers] is the byte representation of a container.
	//
	// This should only be called during bootstrapping, in response to a
	// GetAncestors message to [validatorID] with request ID [requestID].
	//
	// This call should contain the container requested in that message, along
	// with ancestors. The containers should be in BFS order (ie the first
	// container must be the container requested in the GetAncestors message and
	// further back ancestors are later in [containers]
	//
	// It is not safe to assume this message is in response to a GetAncestor
	// message, that this message has a unique requestID or that any of the
	// containers in [containers] are valid.
	Ancestors(
		ctx context.Context,
		validatorID ids.NodeID,
		requestID uint32,
		containers [][]byte,
	) error

	// Notify this engine that a GetAncestors request it issued has failed.
	//
	// This function will be called if the engine sent a GetAncestors message
	// that is not anticipated to be responded to. This could be because the
	// recipient of the message is unknown or if the message request has timed
	// out.
	//
	// The validatorID and requestID are assumed to be the same as those sent in
	// the GetAncestors message.
	GetAncestorsFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// GetHandler defines how a consensus engine reacts to get message from another
// validator. Functions only return fatal errors.
type GetHandler interface {
	// Notify this engine of a request for a container.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID. It is also not safe to
	// assume the requested containerID exists.
	//
	// There should never be a situation where a virtuous node sends a Get
	// request to another virtuous node that does not have the requested
	// container.
	//
	// This engine should respond with a Put message with the same requestID if
	// the container was locally available. Otherwise, the message can be safely
	// dropped.
	Get(ctx context.Context, validatorID ids.NodeID, requestID uint32, containerID ids.ID) error
}

// PutHandler defines how a consensus engine reacts to put messages from other
// validators. Functions only return fatal errors.
type PutHandler interface {
	// Notify this engine of a container.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID.
	Put(
		ctx context.Context,
		validatorID ids.NodeID,
		requestID uint32,
		container []byte,
	) error

	// Notify this engine that a get request it issued has failed.
	//
	// This function will be called if the engine sent a Get message that is not
	// anticipated to be responded to. This could be because the recipient of
	// the message is unknown or if the message request has timed out.
	//
	// The validatorID and requestID are assumed to be the same as those sent in
	// the Get message.
	GetFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// QueryHandler defines how a consensus engine reacts to query messages from
// other validators. Functions only return fatal errors.
type QueryHandler interface {
	// Notify this engine of a request for our preferences.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID. However, the validatorID is
	// assumed to be authenticated.
	//
	// If the container or its ancestry is incomplete, this engine is expected
	// to request the missing containers from the validator. Once the ancestry
	// is complete, this engine should send this validator the current
	// preferences in a Chits message. The Chits message should have the same
	// requestID that was passed in here.
	PullQuery(
		ctx context.Context,
		validatorID ids.NodeID,
		requestID uint32,
		containerID ids.ID,
	) error

	// Notify this engine of a request for our preferences.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID.
	//
	// This function is meant to behave the same way as PullQuery, except the
	// container is optimistically provided to potentially remove the need for
	// a series of Get/Put messages.
	//
	// If the ancestry of the container is incomplete, this engine is expected
	// to request the ancestry from the validator. Once the ancestry is
	// complete, this engine should send this validator the current preferences
	// in a Chits message. The Chits message should have the same requestID that
	// was passed in here.
	PushQuery(
		ctx context.Context,
		validatorID ids.NodeID,
		requestID uint32,
		container []byte,
	) error
}

// ChitsHandler defines how a consensus engine reacts to query response messages
// from other validators. Functions only return fatal errors.
type ChitsHandler interface {
	// Notify this engine of the specified validators preferences.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is in response to a PullQuery or a PushQuery message.
	// However, the validatorID is assumed to be authenticated.
	Chits(ctx context.Context, validatorID ids.NodeID, requestID uint32, containerIDs []ids.ID) error

	// Notify this engine that a query it issued has failed.
	//
	// This function will be called if the engine sent a PullQuery or PushQuery
	// message that is not anticipated to be responded to. This could be because
	// the recipient of the message is unknown or if the message request has
	// timed out.
	//
	// The validatorID and the requestID are assumed to be the same as those
	// sent in the Query message.
	QueryFailed(ctx context.Context, validatorID ids.NodeID, requestID uint32) error
}

// NetworkAppHandler defines how a consensus engine reacts to app specific
// messages from the network.
//
// Functions only return fatal errors.
type NetworkAppHandler interface {
	// Notify this engine of a request for data from [nodeID].
	//
	// The meaning of [request], and what should be sent in response to it, is
	// application (VM) specific.
	//
	// It is not guaranteed that:
	// * [request] is well-formed/valid.
	//
	// This node should typically send an AppResponse to [nodeID] in response to
	// a valid message using the same request ID before the deadline. However,
	// the VM may arbitrarily choose to not send a response to this request.
	AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error

	// Notify this engine that an AppRequest message it sent to [nodeID] with
	// request ID [requestID] failed.
	//
	// This may be because the request timed out or because the message couldn't
	// be sent to [nodeID].
	//
	// It is guaranteed that:
	// * This engine sent a request to [nodeID] with ID [requestID].
	// * AppRequestFailed([nodeID], [requestID]) has not already been called.
	// * AppResponse([nodeID], [requestID]) has not already been called.
	AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error

	// Notify this engine of a response to the AppRequest message it sent to
	// [nodeID] with request ID [requestID].
	//
	// The meaning of [response] is application (VM) specifc.
	//
	// It is guaranteed that:
	// * This engine sent a request to [nodeID] with ID [requestID].
	// * AppRequestFailed([nodeID], [requestID]) has not already been called.
	// * AppResponse([nodeID], [requestID]) has not already been called.
	//
	// It is not guaranteed that:
	// * [response] contains the expected response
	// * [response] is well-formed/valid.
	//
	// If [response] is invalid or not the expected response, the VM chooses how
	// to react. For example, the VM may send another AppRequest, or it may give
	// up trying to get the requested information.
	AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error

	// Notify this engine of a gossip message from [nodeID].
	//
	// The meaning of [msg] is application (VM) specific, and the VM defines how
	// to react to this message.
	//
	// This message is not expected in response to any event, and it does not
	// need to be responded to.
	//
	// A node may gossip the same message multiple times. That is,
	// AppGossip([nodeID], [msg]) may be called multiple times.
	AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error
}

// CrossChainAppHandler defines how a consensus engine reacts to cross-chain app
// specific messages.
//
// Functions only return fatal errors.
type CrossChainAppHandler interface {
	// CrossChainAppRequest Notify this engine of a request for data from
	// [chainID].
	//
	// The meaning of [request], and what should be sent in response to it, is
	// application (VM) specific.
	//
	// Guarantees surrounding the request are specific to the implementation of
	// the requesting VM. For example, the request may or may not be guaranteed
	// to be well-formed/valid depending on the implementation of the requesting
	// VM.
	//
	// This node should typically send a CrossChainAppResponse to [chainID] in
	// response to a valid message using the same request ID before the
	// deadline. However, the VM may arbitrarily choose to not send a response
	// to this request.
	CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error
	// CrossChainAppRequestFailed notifies this engine that a
	// CrossChainAppRequest message it sent to [chainID] with request ID
	// [requestID] failed.
	//
	// This may be because the request timed out or because the message couldn't
	// be sent to [chainID].
	//
	// It is guaranteed that:
	// * This engine sent a request to [chainID] with ID [requestID].
	// * CrossChainAppRequestFailed([chainID], [requestID]) has not already been
	// called.
	// * CrossChainAppResponse([chainID], [requestID]) has not already been
	// called.
	CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error
	// CrossChainAppResponse notifies this engine of a response to the
	// CrossChainAppRequest message it sent to [chainID] with request ID
	// [requestID].
	//
	// The meaning of [response] is application (VM) specific.
	//
	// It is guaranteed that:
	// * This engine sent a request to [chainID] with ID [requestID].
	// * CrossChainAppRequestFailed([chainID], [requestID]) has not already been
	// called.
	// * CrossChainAppResponse([chainID], [requestID]) has not already been
	// called.
	//
	// Guarantees surrounding the response are specific to the implementation of
	// the responding VM. For example, the response may or may not be guaranteed
	// to be well-formed/valid depending on the implementation of the requesting
	// VM.
	//
	// If [response] is invalid or not the expected response, the VM chooses how
	// to react. For example, the VM may send another CrossChainAppRequest, or
	// it may give up trying to get the requested information.
	CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error
}

// AppHandler defines how a consensus engine reacts to app specific messages.
// Functions only return fatal errors.
type AppHandler interface {
	NetworkAppHandler
	CrossChainAppHandler
}

// InternalHandler defines how this consensus engine reacts to messages from
// other components of this validator. Functions only return fatal errors if
// they occur.
type InternalHandler interface {
	// Notify this engine of peer changes.
	validators.Connector

	// Notify this engine that a registered timeout has fired.
	Timeout() error

	// Gossip to the network a container on the accepted frontier
	Gossip() error

	// Halt this engine.
	//
	// This function will be called before the environment starts exiting. This
	// function is slightly special, in that it does not expect the chain's
	// context lock to be held before calling this function.
	Halt()

	// Shutdown this engine.
	//
	// This function will be called when the environment is exiting.
	Shutdown() error

	// Notify this engine of a message from the virtual machine.
	Notify(Message) error
}
