// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ btree.LessFunc[*Staker] = (*Staker).Less

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
// Invariant: Staker's size is bounded to prevent OOM DoS attacks.
type Staker struct {
	TxID            ids.ID
	NodeID          ids.NodeID
	PublicKey       *bls.PublicKey
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
	Priority txs.Priority
}

// A *Staker is considered to be less than another *Staker when:
//
//  1. If its NextTime is before the other's.
//  2. If the NextTimes are the same, the *Staker with the lesser priority is the
//     lesser one.
//  3. If the priorities are also the same, the one with the lesser txID is
//     lesser.
func (s *Staker) Less(than *Staker) bool {
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

func NewCurrentStaker(
	txID ids.ID,
	staker txs.Staker,
	startTime time.Time,
	potentialReward uint64,
) (*Staker, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return nil, err
	}
	endTime := staker.EndTime()
	return &Staker{
		TxID:            txID,
		NodeID:          staker.NodeID(),
		PublicKey:       publicKey,
		SubnetID:        staker.SubnetID(),
		Weight:          staker.Weight(),
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: potentialReward,
		NextTime:        endTime,
		Priority:        staker.CurrentPriority(),
	}, nil
}

func NewPendingStaker(txID ids.ID, staker txs.ScheduledStaker) (*Staker, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return nil, err
	}
	startTime := staker.StartTime()
	return &Staker{
		TxID:      txID,
		NodeID:    staker.NodeID(),
		PublicKey: publicKey,
		SubnetID:  staker.SubnetID(),
		Weight:    staker.Weight(),
		StartTime: startTime,
		EndTime:   staker.EndTime(),
		NextTime:  startTime,
		Priority:  staker.PendingPriority(),
	}, nil
}

// Staker object contains a staker's hot attributes, likely to be used often.
// StakerRewardAttributes contains a staker's cold attributes which are used less often.
// Note that both Staker and StakerAttribute content comes from the stakerTx creating the staker.
// In state.State we also have StakerMetadata, which contains data about the stakers
// generated during staker's activity (mostly uptimes).
type StakerRewardAttributes struct {
	// common attributes
	Stake   []*avax.TransferableOutput
	Outputs []*avax.TransferableOutput

	// validators specific attributes
	Shares                 uint32
	ValidationRewardsOwner fx.Owner
	DelegationRewardsOwner fx.Owner
	ProofOfPossession      *signer.ProofOfPossession

	// delegators specific attributes
	RewardsOwner fx.Owner
}
