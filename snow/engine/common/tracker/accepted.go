// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Accepted = (*accepted)(nil)

type Accepted interface {
	validators.SetCallbackListener

	// SetLastAccepted updates the latest accepted block for [nodeID] to
	// [blockID]. If [nodeID] is not currently a validator, this is a noop.
	SetLastAccepted(nodeID ids.NodeID, blockID ids.ID)
	// LastAccepted returns the latest known accepted block of [nodeID]. If
	// [nodeID]'s last accepted block was never unknown, false will be returned.
	LastAccepted(nodeID ids.NodeID) (ids.ID, bool)
}

type accepted struct {
	lock       sync.RWMutex
	validators set.Set[ids.GenericNodeID]
	frontier   map[ids.GenericNodeID]ids.ID
}

func NewAccepted() Accepted {
	return &accepted{
		frontier: make(map[ids.GenericNodeID]ids.ID),
	}
}

func (a *accepted) OnValidatorAdded(nodeID ids.GenericNodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.validators.Add(nodeID)
}

func (a *accepted) OnValidatorRemoved(nodeID ids.GenericNodeID, _ uint64) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.validators.Remove(nodeID)
	delete(a.frontier, nodeID)
}

func (*accepted) OnValidatorWeightChanged(_ ids.GenericNodeID, _, _ uint64) {}

func (a *accepted) SetLastAccepted(nodeID ids.NodeID, frontier ids.ID) {
	a.lock.Lock()
	defer a.lock.Unlock()

	genericNodeID := ids.GenericNodeIDFromNodeID(nodeID)
	if a.validators.Contains(genericNodeID) {
		a.frontier[genericNodeID] = frontier
	}
}

func (a *accepted) LastAccepted(nodeID ids.NodeID) (ids.ID, bool) {
	a.lock.RLock()
	defer a.lock.RUnlock()

	genericNodeID := ids.GenericNodeIDFromNodeID(nodeID)
	acceptedID, ok := a.frontier[genericNodeID]
	return acceptedID, ok
}
