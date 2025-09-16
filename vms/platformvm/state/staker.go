// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"fmt"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ btree.LessFunc[*Staker] = (*Staker).Less

	errInvalidContinuationPeriod        = fmt.Errorf("continuation period invalid transition")
	errDecreasedWeight                  = fmt.Errorf("weight decreased")
	errDecreasedAccruedRewards          = fmt.Errorf("accrued rewards decreased")
	errDecreasedAccruedDelegateeRewards = fmt.Errorf("accrued delegatee rewards decreased")
	errImmutableFieldsModified          = fmt.Errorf("immutable fields modified")
	errStartTimeTooEarly                = fmt.Errorf("start time too early")
)

// Staker contains all information required to represent a validator or
// delegator in the current and pending validator sets.
// Invariant: Staker's size is bounded to prevent OOM DoS attacks.
type Staker struct {
	TxID                    ids.ID
	NodeID                  ids.NodeID
	PublicKey               *bls.PublicKey
	SubnetID                ids.ID
	Weight                  uint64 // it includes [AccruedRewards] and [AccruedDelegateeRewards]
	StartTime               time.Time
	EndTime                 time.Time
	PotentialReward         uint64
	AccruedRewards          uint64
	AccruedDelegateeRewards uint64

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

	// ContinuationPeriod is used by continuous stakers.
	// ContinuationPeriod > 0  => running continuous staker
	// ContinuationPeriod == 0 => a stopped continuous staker OR a fixed staker, we don't care since we will stop at EndTime.
	ContinuationPeriod time.Duration
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

	var (
		endTime            time.Time
		continuationPeriod time.Duration
	)

	switch tTx := staker.(type) {
	case txs.FixedStaker:
		endTime = tTx.EndTime()
		continuationPeriod = 0

	case txs.ContinuousStaker:
		endTime = startTime.Add(tTx.PeriodDuration())
		continuationPeriod = tTx.PeriodDuration()
	default:
		return nil, fmt.Errorf("unexpected staker tx type: %T", staker)
	}

	return &Staker{
		TxID:               txID,
		NodeID:             staker.NodeID(),
		PublicKey:          publicKey,
		SubnetID:           staker.SubnetID(),
		Weight:             staker.Weight(),
		StartTime:          startTime,
		EndTime:            endTime,
		PotentialReward:    potentialReward,
		NextTime:           endTime,
		Priority:           staker.CurrentPriority(),
		ContinuationPeriod: continuationPeriod,
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

// todo: test this
func (s *Staker) ValidMutation(ms Staker) error {
	if s.ContinuationPeriod != ms.ContinuationPeriod && ms.ContinuationPeriod != 0 {
		// Only transition allowed for continuation period is setting it to 0.
		return errInvalidContinuationPeriod
	}

	if s.Weight > ms.Weight {
		// Weight can only increase (by accruing rewards from continuous staking).
		return errDecreasedWeight
	}

	if s.AccruedRewards > ms.AccruedRewards {
		// AccruedRewards can only increase.
		return errDecreasedAccruedRewards
	}

	if s.AccruedDelegateeRewards > ms.AccruedDelegateeRewards {
		// AccruedRewards can only increase.
		return errDecreasedAccruedDelegateeRewards
	}

	if !ms.StartTime.Equal(s.StartTime) && s.EndTime.After(ms.StartTime) {
		// New [StartTime] should be AFTER the previous [EndTime].
		return errStartTimeTooEarly
	}

	if !s.immutableFieldsAreUnmodified(ms) {
		return errImmutableFieldsModified
	}

	return nil
}

// todo: test this
func (s *Staker) resetContinuationStakerCycle(startTime time.Time, weight, potentialReward, totalAccruedRewards, totalAccruedDelegateeRewards uint64) error {
	if s.ContinuationPeriod == 0 {
		return fmt.Errorf("cannot reset a non-continuous validator")
	}

	if totalAccruedRewards < s.AccruedRewards {
		return fmt.Errorf("accrued rewards cannot be less than current value")
	}

	if totalAccruedDelegateeRewards < s.AccruedDelegateeRewards {
		return fmt.Errorf("accrued delegatee rewards cannot be less than current value")
	}

	if weight < s.Weight {
		return fmt.Errorf("weight cannot be less than current value")
	}

	endTime := startTime.Add(s.ContinuationPeriod)

	s.StartTime = startTime
	s.EndTime = endTime
	s.PotentialReward = potentialReward
	s.AccruedRewards = totalAccruedRewards
	s.AccruedDelegateeRewards = totalAccruedDelegateeRewards
	s.Weight = weight

	return nil
}

// todo: test this
func (s Staker) immutableFieldsAreUnmodified(ms Staker) bool {
	// Mutable fields: Weight, StartTime, EndTime, PotentialReward, AccruedRewards, ContinuationPeriod
	return s.TxID == ms.TxID &&
		s.NodeID == ms.NodeID &&
		s.PublicKey.Equals(ms.PublicKey) &&
		s.SubnetID == ms.SubnetID &&
		s.NextTime.Equal(ms.NextTime) &&
		s.Priority == ms.Priority
}
