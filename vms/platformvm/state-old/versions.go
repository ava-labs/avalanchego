// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ Versions = &versions{}

type Versions interface {
	GetState(blkID ids.ID) (Chain, bool)
	SetState(blkID ids.ID, state Chain)
	DeleteState(blkID ids.ID)
}

type versions struct {
	state map[ids.ID]Chain
}

func NewVersions(lastAcceptedID ids.ID, baseState Chain) Versions {
	return &versions{
		state: map[ids.ID]Chain{
			lastAcceptedID: baseState,
		},
	}
}

func (v *versions) GetState(blkID ids.ID) (Chain, bool) {
	state, ok := v.state[blkID]
	return state, ok
}

func (v *versions) SetState(blkID ids.ID, state Chain) {
	v.state[blkID] = state
}

func (v *versions) DeleteState(blkID ids.ID) {
	delete(v.state, blkID)
}
