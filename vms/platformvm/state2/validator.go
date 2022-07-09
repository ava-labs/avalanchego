// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
)

type Validators interface {
	GetStaker(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error)
	PutStaker(staker *Staker)
	DeleteStaker(staker *Staker)

	GetDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) StakerIterator
	PutDelegator(staker *Staker)
	DeleteDelegator(staker *Staker)

	// GetStakerIterator returns the stakers in the validator set sorted in
	// order of their future removal.
	GetStakerIterator() StakerIterator
}
