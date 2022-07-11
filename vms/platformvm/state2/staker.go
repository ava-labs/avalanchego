// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

const (
	// First subnet delegators are removed from the current validator set,
	SubnetDelegatorCurrentPriority = iota
	// then subnet validators,
	SubnetValidatorCurrentPriority
	// then primary network delegators,
	PrimaryNetworkDelegatorCurrentPriority
	// then primary network validators.
	PrimaryNetworkValidatorCurrentPriority
)

const (
	// First primary network validators are moved from the pending to the
	// current validator set,
	PrimaryNetworkValidatorPendingPriority = iota
	// then primary network delegators,
	PrimaryNetworkDelegatorPendingPriority
	// then subnet validators,
	SubnetValidatorPendingPriority
	// then subnet delegators.
	SubnetDelegatorPendingPriority
)

var _ btree.Item = &Staker{}

type StakerIterator interface {
	Next() bool
	Value() *Staker
	Release()
}

type Staker struct {
	TxID            ids.ID
	NodeID          ids.NodeID
	SubnetID        ids.ID
	Weight          uint64
	StartTime       time.Time
	EndTime         time.Time
	PotentialReward uint64

	// The following fields are only used for the staker tree.
	NextTime time.Time
	Priority byte
}

func (s *Staker) Less(thanIntf btree.Item) bool {
	than := thanIntf.(*Staker)

	if s.NextTime.Before(than.NextTime) {
		return true
	}
	if than.NextTime.Before(s.NextTime) {
		return false
	}

	if s.Priority < than.Priority {
		return true
	}
	if than.Priority < s.Priority {
		return false
	}

	return bytes.Compare(s.TxID[:], than.TxID[:]) == -1
}

func NewPrimaryNetworkStaker(txID ids.ID, vdr *validator.Validator) *Staker {
	return &Staker{
		TxID:      txID,
		NodeID:    vdr.ID(),
		SubnetID:  constants.PrimaryNetworkID,
		Weight:    vdr.Weight(),
		StartTime: vdr.StartTime(),
		EndTime:   vdr.EndTime(),
	}
}

func NewSubnetStaker(txID ids.ID, vdr *validator.SubnetValidator) *Staker {
	return &Staker{
		TxID:      txID,
		NodeID:    vdr.ID(),
		SubnetID:  vdr.SubnetID(),
		Weight:    vdr.Weight(),
		StartTime: vdr.StartTime(),
		EndTime:   vdr.EndTime(),
	}
}
