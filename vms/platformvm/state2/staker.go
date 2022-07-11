// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/google/btree"
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

func NewPrimaryNetworkValidatorStaker(txID ids.ID, tx *txs.AddValidatorTx) *Staker {
	return &Staker{
		TxID:      txID,
		NodeID:    tx.Validator.NodeID,
		SubnetID:  constants.PrimaryNetworkID,
		Weight:    tx.Weight(),
		StartTime: tx.StartTime(),
		EndTime:   tx.EndTime(),
	}
}

func NewPrimaryNetworkDelegatorStaker(txID ids.ID, tx *txs.AddDelegatorTx) *Staker {
	return &Staker{
		TxID:      txID,
		NodeID:    tx.Validator.NodeID,
		SubnetID:  constants.PrimaryNetworkID,
		Weight:    tx.Weight(),
		StartTime: tx.StartTime(),
		EndTime:   tx.EndTime(),
	}
}

func NewSubnetValidatorStaker(txID ids.ID, tx *txs.AddSubnetValidatorTx) *Staker {
	return &Staker{
		TxID:      txID,
		NodeID:    tx.Validator.NodeID,
		SubnetID:  tx.Validator.Subnet,
		Weight:    tx.Weight(),
		StartTime: tx.StartTime(),
		EndTime:   tx.EndTime(),
	}
}
