// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	"github.com/ava-labs/avalanchego/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// Engine describes the standard interface of a consensus engine
type Engine interface {
	Handler

	// Return the context of the chain this engine is working on
	Context() *snow.Context

	// Returns true iff the chain is done bootstrapping
	IsBootstrapped() bool

	// Returns nil if the engine is healthy.
	// Periodically called and reported through the health API
	health.Checkable

	// GetVM returns this engine's VM
	GetVM() VM
}

// Handler defines the functions that are acted on the node
type Handler interface {
	ExternalHandler
	InternalHandler
}

// ExternalHandler defines how a consensus engine reacts to messages and
// requests from other validators
type ExternalHandler interface {
	FrontierHandler
	AcceptedHandler
	FetchHandler
	QueryHandler
	FastSyncHandler
}

type FastSyncHandler interface {
	GetStateSummaryFrontier(validatorID ids.ShortID, requestID uint32) error
	StateSummaryFrontier(validatorID ids.ShortID, requestID uint32, summary []byte) error
	GetAcceptedStateSummary(validatorID ids.ShortID, requestID uint32, summaries [][]byte) error
	AcceptedStateSummary(validatorID ids.ShortID, requestID uint32, summaries [][]byte) error

	GetAcceptedStateSummaryFailed(validatorID ids.ShortID, requestID uint32) error
	GetStateSummaryFrontierFailed(validatorID ids.ShortID, requestID uint32) error
}

// FrontierHandler defines how a consensus engine reacts to frontier messages
// from other validators. Returned errors should be treated as fatal and require
// the chain to shutdown.
type FrontierHandler interface {
	// Notify this engine of a request for the accepted frontier of vertices.
	//
	// The accepted frontier is the set of accepted vertices that do not have
	// any accepted descendants.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID. However, the validatorID is
	// assumed to be authenticated.
	//
	// This engine should respond with an AcceptedFrontier message with the same
	// requestID, and the engine's current accepted frontier.
	GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error

	// Notify this engine of an accepted frontier.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is in response to a GetAcceptedFrontier message, is
	// utilizing a unique requestID, or that the containerIDs from a valid
	// frontier. However, the validatorID is  assumed to be authenticated.
	AcceptedFrontier(
		validatorID ids.ShortID,
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
	GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error
}

// AcceptedHandler defines how a consensus engine reacts to messages pertaining
// to accepted containers from other validators. Functions only return fatal
// errors if they occur.
type AcceptedHandler interface {
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
		validatorID ids.ShortID,
		requestID uint32,
		containerIDs []ids.ID,
	) error

	// Notify this engine of a set of accepted vertices.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is in response to a GetAccepted message, is utilizing a
	// unique requestID, or that the containerIDs are a subset of the
	// containerIDs from a GetAccepted message. However, the validatorID is
	// assumed to be authenticated.
	Accepted(
		validatorID ids.ShortID,
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
	GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error
}

type AppHandler interface {
	// Notify this engine of a request for data from [nodeID].
	// This node should send an AppResponse to [nodeID] in response to
	// this message using the same request ID and before the deadline.
	// The VM may choose to not send a response to this request.
	// The meaning of [request], and what should be sent in response to it,
	// is application (VM) defined.
	// It is not guaranteed that [request] is well-formed/valid.
	// The VM must do this validation.
	// A non-nil return value causes this engine to shutdown.
	AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error

	// Notify this engine that an AppRequest message it sent to
	// [nodeID] with request ID [requestID] failed.
	// This may be because the request timed out or because the message
	// couldn't be sent to [nodeID].
	// It is guaranteed that:
	// * This engine sent a request to [nodeID] with ID [requestID].
	// * AppRequestFailed([nodeID], [requestID]) has not already been called.
	// * AppResponse([nodeID], [requestID]) has not already been called.
	// A non-nil return value causes this engine to shutdown.
	AppRequestFailed(nodeID ids.ShortID, requestID uint32) error

	// Notify this engine of a response to the AppRequest message it sent
	// to [nodeID] with request ID [requestID].
	// The meaning of [response] is application (VM) defined.
	// It is guaranteed that:
	// * This engine sent a request to [nodeID] with ID [requestID].
	// * AppRequestFailed([nodeID], [requestID]) has not already been called.
	// * AppResponse([nodeID], [requestID]) has not already been called.
	// It is not guaranteed that [response] contains the expected response,
	// or that [response] is well-formed/valid.
	// The VM must perform the validation of [response].
	// If [response] is invalid or not the expected response, the VM chooses how to react.
	// For example, the VM may send another AppRequest, or it may give up
	// trying to get the requested information.
	// A non-nil return value causes this engine to shutdown.
	// Therefore, receipt of an unexpected or invalid [response]
	// should not cause this method to return a non-nil error!
	AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error

	// Notify this engine of a gossip message from [nodeID].
	// This message is not expected in response to any event, and it does
	// not need to be responded to.
	// The meaning of [msg] is application (VM) defined, and the VM defines
	// how to react to this message.
	// A node may gossip the same message multiple times. That is,
	// AppGossip([nodeID], [msg]) may be called multiple times.
	// A non-nil return value causes this engine to shutdown.
	AppGossip(nodeID ids.ShortID, msg []byte) error
}

// FetchHandler defines how a consensus engine reacts to retrieval messages from
// other validators. Functions only return fatal errors if they occur.
type FetchHandler interface {
	AppHandler

	// Notify this engine of a request for a container.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID. It is also not safe to
	// assume the requested containerID exists. However, the validatorID is
	// assumed to be authenticated.
	//
	// There should never be a situation where a virtuous node sends a Get
	// request to another virtuous node that does not have the requested
	// container. Unless that container was pruned from the active set.
	//
	// This engine should respond with a Put message with the same requestID if
	// the container was locally available. Otherwise, the message can be safely
	// dropped.
	Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error

	// Notify this engine of a request for a container and its ancestors.
	// The request is from validator [validatorID]. The requested container is [containerID].
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID. It is also not safe to
	// assume the requested containerID exists. However, the validatorID is
	// assumed to be authenticated.
	//
	// This engine should respond with a MultiPut message with the same requestID,
	// which contains [containerID] as well as its ancestors. See MultiPut's documentation.
	//
	// If this engine doesn't have some ancestors, it should reply with its best effort attempt at getting them.
	// If this engine doesn't have [containerID] it can ignore this message.
	GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error

	// Notify this engine of a container.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID or even that the containerID
	// matches the ID of the container bytes. However, the validatorID is
	// assumed to be authenticated.
	//
	// This engine needs to request and receive missing ancestors of the
	// container before adding the container to consensus. Once all ancestor
	// containers are added, pushes the container into the consensus.
	Put(
		validatorID ids.ShortID,
		requestID uint32,
		containerID ids.ID,
		container []byte,
	) error

	// Notify this engine of multiple containers.
	// Each element of [containers] is the byte representation of a container.
	//
	// This should only be called during bootstrapping, and in response to a GetAncestors message to
	// [validatorID] with request ID [requestID]. This call should contain the container requested in
	// that message, along with ancestors.
	// The containers should be in BFS order (ie the first container must be the container
	// requested in the GetAncestors message and further back ancestors are later in [containers]
	//
	// It is not safe to assume this message is in response to a GetAncestor message, that this
	// message has a unique requestID or that any of the containers in [containers] are valid.
	// However, the validatorID is assumed to be authenticated.
	MultiPut(
		validatorID ids.ShortID,
		requestID uint32,
		containers [][]byte,
	) error

	// Notify this engine that a get request it issued has failed.
	//
	// This function will be called if the engine sent a Get message that is not
	// anticipated to be responded to. This could be because the recipient of
	// the message is unknown or if the message request has timed out.
	//
	// The validatorID and requestID are assumed to be the same as those sent in
	// the Get message.
	GetFailed(validatorID ids.ShortID, requestID uint32) error

	// Notify this engine that a GetAncestors request it issued has failed.
	//
	// This function will be called if the engine sent a GetAncestors message that is not
	// anticipated to be responded to. This could be because the recipient of
	// the message is unknown or if the message request has timed out.
	//
	// The validatorID and requestID are assumed to be the same as those sent in
	// the GetAncestors message.
	GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) error
}

// QueryHandler defines how a consensus engine reacts to query messages from
// other validators. Functions only return fatal errors if they occur.
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
		validatorID ids.ShortID,
		requestID uint32,
		containerID ids.ID,
	) error

	// Notify this engine of a request for our preferences.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is utilizing a unique requestID or even that the containerID
	// matches the ID of the container bytes. However, the validatorID is
	// assumed to be authenticated.
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
		validatorID ids.ShortID,
		requestID uint32,
		containerID ids.ID,
		container []byte,
	) error

	// Notify this engine of the specified validators preferences.
	//
	// This function can be called by any validator. It is not safe to assume
	// this message is in response to a PullQuery or a PushQuery message.
	// However, the validatorID is assumed to be authenticated.
	Chits(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error

	// Notify this engine that a query it issued has failed.
	//
	// This function will be called if the engine sent a PullQuery or PushQuery
	// message that is not anticipated to be responded to. This could be because
	// the recipient of the message is unknown or if the message request has
	// timed out.
	//
	// The validatorID and the requestID are assumed to be the same as those
	// sent in the Query message.
	QueryFailed(validatorID ids.ShortID, requestID uint32) error
}

// InternalHandler defines how this consensus engine reacts to messages from
// other components of this validator. Functions only return fatal errors if
// they occur.
type InternalHandler interface {
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

	// Notify this engine of a new peer.
	Connected(validatorID ids.ShortID) error

	// Notify this engine of a removed peer.
	Disconnected(validatorID ids.ShortID) error
}
