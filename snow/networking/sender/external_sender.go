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
	GetAcceptedFrontier(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time)
	AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID)

	GetAccepted(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerIDs []ids.ID)
	Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID)

	GetAncestors(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID)
	MultiPut(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte)

	Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID)
	Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte)

	PushQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID, container []byte)
	PullQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID)
	Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes []ids.ID)

	Gossip(chainID ids.ID, containerID ids.ID, container []byte)
}
