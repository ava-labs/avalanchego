// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"math"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ btree.LessFunc[*Staker] = (*Staker).Less

	StakerZeroTime                  = time.Unix(0, 0)
	StakerMaxDuration time.Duration = math.MaxInt64 // time.Duration underlying type is currently int64
)

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
	TxID      ids.ID
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	SubnetID  ids.ID
	Weight    uint64

	// StartTime is the time this staker enters the current validators set.
	// Pre ContinuousStakingFork, StartTime is set by the Add*Tx creating the staker.
	// Post ContinuousStakingFork StartTime is set to chain time when Add*Tx is accepted.
	// StartTime does not change during a staker lifetime.
	StartTime time.Time

	// StakingPeriod is the time the staker will stake.
	// Note that it's not necessarily true that StakingPeriod == EndTime - StartTime.
	// StakingPeriod does not change during a staker lifetime.
	StakingPeriod time.Duration

	// StartTime is the time this staker exits the current validators set.
	// Pre ContinuousStakingFork, StartTime is set by the Add*Tx creating the staker.
	// Post ContinuousStakingFork StartTime is set initially to mockable.MaxTime. An
	// explicit StopStaking transaction with set it to a finite value.
	// EndTime may change during a staker lifetime.
	EndTime time.Time

	PotentialReward uint64

	// Pre ContinuousStaking Fork, NextTime is the next time this staker will be
	// moved into/out of the validator set. Specifically
	// a. If staker is pending, NextTime equals StartTime, i.e. the time the staker
	// will enter the current validators set.
	// b. If staker is current, NextTime equals EndTime, i.e. the time the staker
	// will exit the current validators set (and will be possibly rewarded).
	// Post ContinuousStaking Fork, NextTime is the next time the staker will be
	// evaluated for reward. Stakers are marked as current as soon as their creation
	// tx is accepted. Also they will automatically restake until a StopStaking tx is issued.
	// TODO ABENEGIA: consider renaming NextTime to NextRewardTime
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
	stakingDuration time.Duration,
	potentialReward uint64,
) (*Staker, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return nil, err
	}

	return &Staker{
		TxID:            txID,
		NodeID:          staker.NodeID(),
		PublicKey:       publicKey,
		SubnetID:        staker.SubnetID(),
		Weight:          staker.Weight(),
		StartTime:       startTime,
		StakingPeriod:   stakingDuration,
		EndTime:         staker.EndTime(),
		PotentialReward: potentialReward,
		NextTime:        startTime.Add(stakingDuration),
		Priority:        staker.CurrentPriority(),
	}, nil
}

func NewPendingStaker(txID ids.ID, staker txs.Staker) (*Staker, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return nil, err
	}
	startTime := staker.StartTime()
	return &Staker{
		TxID:          txID,
		NodeID:        staker.NodeID(),
		PublicKey:     publicKey,
		SubnetID:      staker.SubnetID(),
		Weight:        staker.Weight(),
		StartTime:     startTime,
		EndTime:       staker.EndTime(),
		StakingPeriod: staker.StakingPeriod(),
		NextTime:      startTime,
		Priority:      staker.PendingPriority(),
	}, nil
}

func (s *Staker) IsPending() bool {
	isPending := s.Priority == txs.PrimaryNetworkDelegatorApricotPendingPriority ||
		s.Priority == txs.PrimaryNetworkValidatorPendingPriority ||
		s.Priority == txs.PrimaryNetworkDelegatorBanffPendingPriority ||
		s.Priority == txs.SubnetPermissionlessValidatorPendingPriority ||
		s.Priority == txs.SubnetPermissionlessDelegatorPendingPriority ||
		s.Priority == txs.SubnetPermissionedValidatorPendingPriority
	return isPending
}

func RotateStakerInPlace(s *Staker) {
	s.NextTime = s.NextTime.Add(s.StakingPeriod)
}
