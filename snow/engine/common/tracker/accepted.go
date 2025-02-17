// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	// [blockID], with a corresponding height.
	// If [nodeID] is not currently a validator, this is a noop.
	SetLastAccepted(nodeID ids.NodeID, blockID ids.ID, height uint64)
	// LastAccepted returns the latest known accepted block of [nodeID]. If
	// [nodeID]'s last accepted block was never unknown, false will be returned.
	LastAccepted(nodeID ids.NodeID) (ids.ID, uint64, bool)
}

type idHeight struct {
	id     ids.ID
	height uint64
}

type accepted struct {
	lock         sync.RWMutex
	validators   set.Set[ids.NodeID]
	lastAccepted map[ids.NodeID]idHeight
}

func NewAccepted() Accepted {
	return &accepted{
		lastAccepted: make(map[ids.NodeID]idHeight),
	}
}

func (a *accepted) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.validators.Add(nodeID)
}

func (a *accepted) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.validators.Remove(nodeID)
	delete(a.lastAccepted, nodeID)
}

func (*accepted) OnValidatorWeightChanged(_ ids.NodeID, _, _ uint64) {}

func (a *accepted) SetLastAccepted(nodeID ids.NodeID, id ids.ID, height uint64) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.validators.Contains(nodeID) {
		a.lastAccepted[nodeID] = idHeight{
			id:     id,
			height: height,
		}
	}
}

func (a *accepted) LastAccepted(nodeID ids.NodeID) (ids.ID, uint64, bool) {
	a.lock.RLock()
	defer a.lock.RUnlock()

	acceptedAndHeight, ok := a.lastAccepted[nodeID]
	return acceptedAndHeight.id, acceptedAndHeight.height, ok
}
