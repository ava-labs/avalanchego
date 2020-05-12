// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
)

// Engine describes the standard interface of a consensus engine
type Engine interface {
	Handler

	// Return the context of the chain this engine is working on
	Context() *snow.Context
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
}

// FrontierHandler defines how a consensus engine reacts to frontier messages
// from other validators
type FrontierHandler interface {
	// GetAcceptedFrontier notifies this consensus engine that its accepted
	// frontier is requested by the specified validator
	GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32)

	// AcceptedFrontier notifies this consensus engine of the specified
	// validators current accepted frontier
	AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set)

	// GetAcceptedFrontierFailed notifies this consensus engine that the
	// requested accepted frontier from the specified validator should be
	// considered lost
	GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32)
}

// AcceptedHandler defines how a consensus engine reacts to messages pertaining
// to accepted containers from other validators
type AcceptedHandler interface {
	// GetAccepted notifies this consensus engine that it should send the set of
	// containerIDs that it has accepted from the provided set to the specified
	// validator
	GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set)

	// Accepted notifies this consensus engine of a set of accepted containerIDs
	Accepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set)

	// GetAcceptedFailed notifies this consensus engine that the requested
	// accepted containers requested from the specified validator should be
	// considered lost
	GetAcceptedFailed(validatorID ids.ShortID, requestID uint32)
}

// FetchHandler defines how a consensus engine reacts to retrieval messages from
// other validators
type FetchHandler interface {
	// Get notifies this consensus engine that the specified validator requested
	// that this engine send the specified container to it
	Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID)

	// Put the container with the specified ID and body.
	// This engine needs to request and receive missing ancestors of the
	// container before adding the container to consensus. Once all ancestor
	// containers are added, pushes the container into the consensus.
	Put(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte)

	// Notify this engine that a get request it issued has failed.
	GetFailed(validatorID ids.ShortID, requestID uint32, containerID ids.ID)
}

// QueryHandler defines how a consensus engine reacts to query messages from
// other validators
type QueryHandler interface {
	// Notify this engine that the specified validator queried it about the
	// specified container. That is, the validator would like to know whether
	// this engine prefers the specified container. If the ancestry of the
	// container is incomplete, or the container is unknown, request the missing
	// data. Once complete, sends this validator the current preferences.
	PullQuery(validatorID ids.ShortID, requestID uint32, containerID ids.ID)

	// Notify this engine that the specified validator queried it about the
	// specified container. That is, the validator would like to know whether
	// this engine prefers the specified container. If the ancestry of the
	// container is incomplete, request it. Once complete, sends this validator
	// the current preferences.
	PushQuery(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte)

	// Notify this engine of the specified validators preferences.
	Chits(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set)

	// Notify this engine that a query it issued has failed.
	QueryFailed(validatorID ids.ShortID, requestID uint32)
}

// InternalHandler defines how this consensus engine reacts to messages from
// other components of this validator
type InternalHandler interface {
	// Startup this engine.
	Startup()

	// Gossip to the network a container on the accepted frontier
	Gossip()

	// Shutdown this engine.
	Shutdown()

	// Notify this engine that the vm has sent a message to it.
	Notify(Message)
}
