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

var _ btree.Item = &Staker{}

// StakerIterator defines an interface for iterating over a set of stakers.
type StakerIterator interface {
	// Next attempts to move the iterator to the next staker. It returns false
	// once there are no more stakers to return.
	Next() bool

	// Value returns the current staker. Value should only be called after a
	// call to Next which returned true.
	Value() *Staker

	// Release any resources associated with the iterator. This must be called
	// after the interator is no longer needed.
	Release()
}

// Staker contains all information required to represent a validator or
// delegator in the current and pending validator sets.
type Staker struct {
	TxID            ids.ID
	NodeID          ids.NodeID
	SubnetID        ids.ID
	Weight          uint64
	StartTime       time.Time
	EndTime         time.Time
	PotentialReward uint64

	// NextTime is the next time this staker will be moved from a validator set.
	// If the staker is in the pending validator set, NextTime will equal
	// StartTime. If the staker is in the current validator set, NextTime will
	// equal EndTime.
	NextTime time.Time

	// Priority specifies how to break ties between stakers with the same
	// NextTime. This ensures that stakers created by the same transaction type
	// are grouped together. The ordering of these groups is documented in
	// [priorities.go] and depends on if the stakers are in the pending or
	// current validator set.
	Priority Priority
}

// A *Staker is considered to be less than another *Staker when:
//  1. If its NextTime is before the other's.
//  2. If the NextTimes are the same, the *Staker with the lesser priority is the
//     lesser one.
//  3. If the priorities are also the same, the one with the lesser txID is
//     lesser.
//
// Invariant: [thanIntf] is a *Staker.
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
