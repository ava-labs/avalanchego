// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import "github.com/ava-labs/gecko/ids"

// ExternalSender sends consensus messages to other validators
// Right now this is implemented in the networking package
type ExternalSender interface {
	GetAcceptedFrontier(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32)
	AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set)

	GetAccepted(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerIDs ids.Set)
	Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set)

	Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID)
	Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte)

	PushQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte)
	PullQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerID ids.ID)
	Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set)

	Gossip(chainID ids.ID, containerID ids.ID, container []byte)
}
