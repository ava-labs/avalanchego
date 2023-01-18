// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var _ Accepted = (*accepted)(nil)

type Accepted interface {
	validators.SetCallbackListener

	// SetAcceptedFrontier updates the latest frontier for [nodeID] to
	// [frontier]. If [nodeID] is not currently a validator, this is a noop.
	SetAcceptedFrontier(nodeID ids.NodeID, frontier []ids.ID)
	// AcceptedFrontier returns the latest known accepted frontier of [nodeID].
	// If [nodeID]'s last accepted frontier is unknown, an empty slice will be
	// returned.
	AcceptedFrontier(nodeID ids.NodeID) []ids.ID
}

type accepted struct {
	lock sync.RWMutex
	// frontier contains an entry for all current validators
	frontier map[ids.NodeID][]ids.ID
}

func NewAccepted() Accepted {
	return &accepted{
		frontier: make(map[ids.NodeID][]ids.ID),
	}
}

func (a *accepted) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.frontier[nodeID] = nil
}

func (a *accepted) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	a.lock.Lock()
	defer a.lock.Unlock()

	delete(a.frontier, nodeID)
}

func (*accepted) OnValidatorWeightChanged(_ ids.NodeID, _, _ uint64) {}

func (a *accepted) SetAcceptedFrontier(nodeID ids.NodeID, frontier []ids.ID) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if _, ok := a.frontier[nodeID]; ok {
		a.frontier[nodeID] = frontier
	}
}

func (a *accepted) AcceptedFrontier(nodeID ids.NodeID) []ids.ID {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return a.frontier[nodeID]
}
