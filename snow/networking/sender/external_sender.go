// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// ExternalSender sends consensus messages to other validators
// Right now this is implemented in the networking package
type ExternalSender interface {
	AppSender

	// Send a GetAcceptedFrontier message for chain [chainID] to validators in [nodeIDs].
	// The validator should reply by [deadline].
	// Returns the IDs of validators that may receive the message.
	// If we're not connected to a validator in [nodeIDs], for example,
	// it will not be included in the return value.
	SendGetAcceptedFrontier(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration) []ids.ShortID
	SendAcceptedFrontier(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID)

	SendGetAccepted(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerIDs []ids.ID) []ids.ShortID
	SendAccepted(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID)

	// Request ancestors of container [containerID] in chain [chainID] from validator [nodeID].
	// The validator should reply by [deadline].
	// Returns true if the validator may receive the message.
	// If we're not connected to [nodeID], for example, returns false.
	SendGetAncestors(nodeID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) bool
	SendMultiPut(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte)

	SendGet(nodeID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) bool
	SendPut(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte)

	SendPushQuery(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID, container []byte) []ids.ShortID
	SendPullQuery(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) []ids.ShortID
	SendChits(nodeID ids.ShortID, chainID ids.ID, requestID uint32, votes []ids.ID)

	SendGossip(chainID ids.ID, containerID ids.ID, container []byte)
}

// Sends app-level messages
type AppSender interface {
	// Send an application-level request
	SendAppRequest(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, appRequestBytes []byte) []ids.ShortID
	SendAppResponse(nodeIDs ids.ShortID, chainID ids.ID, requestID uint32, appResponseBytes []byte)
	SendAppGossip(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, appGossipBytes []byte)
}
