// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"errors"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ btree.LessFunc[*Staker] = (*Staker).Less

	errDecreasedWeight                  = errors.New("weight decreased")
	errDecreasedAccruedRewards          = errors.New("accrued rewards decreased")
	errDecreasedAccruedDelegateeRewards = errors.New("accrued delegatee rewards decreased")
	errEndTimeAndNextTimeMismatch       = errors.New("end time and next time mismatch")
	errContinuationPeriodIsZero         = errors.New("continuation period is zero")
	errImmutableFieldsModified          = errors.New("immutable fields modified")
)

type ContinuousValidator struct {
	// AccruedRewards is the total rewards accumulated by a continuous staker across all completed staking cycles.
	AccruedRewards uint64

	// AccruedDelegateeRewards is the total delegatee rewards accumulated by a continuous staker across all completed staking cycles.
	AccruedDelegateeRewards uint64

	// Percentage of rewards to auto-restake at the end of each cycle, expressed in millionths (percentage * 10,000).
	// Range [0..1_000_000]:
	//   0         = restake principal only; withdraw 100% of rewards
	//   300_000   = restake 30% of rewards; withdraw 70%
	//   1_000_000 = restake 100% of rewards; withdraw 0%
	AutoRestakeShares uint32

	// ContinuationPeriod is used by continuous stakers.
	// ContinuationPeriod > 0  => running continuous staker
	// ContinuationPeriod == 0 => a stopped continuous staker OR a fixed staker, we don't care since we will stop at EndTime.
	ContinuationPeriod time.Duration
}

type Validator struct {
	// DelegateeReward is the portion of delegator rewards earned by a validator (their commission).
	DelegateeReward uint64
}

type Staker struct {
	Validator
	ContinuousValidator

	TxID            ids.ID
	NodeID          ids.NodeID
	PublicKey       *bls.PublicKey
	SubnetID        ids.ID
	Weight          uint64 // it includes [AccruedRewards] and [AccruedDelegateeRewards]
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

func NewCurrentValidator(
	txID ids.ID,
	staker txs.FixedStaker,
	startTime time.Time,
	potentialReward uint64,
	delegateeReward uint64,
) (*Staker, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return nil, err
	}

	return &Staker{
		Validator: Validator{
			DelegateeReward: delegateeReward,
		},
		TxID:            txID,
		NodeID:          staker.NodeID(),
		PublicKey:       publicKey,
		SubnetID:        staker.SubnetID(),
		Weight:          staker.Weight(),
		StartTime:       startTime,
		EndTime:         staker.EndTime(),
		PotentialReward: potentialReward,
		NextTime:        staker.EndTime(),
		Priority:        staker.CurrentPriority(),
	}, nil
}

func NewCurrentDelegator(
	txID ids.ID,
	staker txs.FixedStaker,
	startTime time.Time,
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
		EndTime:         staker.EndTime(),
		PotentialReward: potentialReward,
		NextTime:        staker.EndTime(),
		Priority:        staker.CurrentPriority(),
	}, nil
}

func NewContinuousStaker(
	txID ids.ID,
	staker txs.Staker,
	startTime time.Time,
	potentialReward uint64,
	delegateeReward uint64,
	accruedRewards uint64,
	accruedDelegateeRewards uint64,
	autoRestakeShares uint32,
	continuationPeriod time.Duration,
) (*Staker, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return nil, err
	}

	endTime := startTime.Add(continuationPeriod)
	return &Staker{
		Validator: Validator{
			DelegateeReward: delegateeReward,
		},
		ContinuousValidator: ContinuousValidator{
			AccruedRewards:          accruedRewards,
			AccruedDelegateeRewards: accruedDelegateeRewards,
			AutoRestakeShares:       autoRestakeShares,
			ContinuationPeriod:      continuationPeriod,
		},
		TxID:            txID,
		NodeID:          staker.NodeID(),
		PublicKey:       publicKey,
		SubnetID:        staker.SubnetID(),
		Weight:          staker.Weight() + accruedRewards + accruedDelegateeRewards,
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

func (s *Staker) ValidateMutation(ms *Staker) error {
	if !ms.EndTime.Equal(ms.NextTime) {
		return errEndTimeAndNextTimeMismatch
	}

	if !s.immutableFieldsAreUnmodified(ms) {
		return errImmutableFieldsModified
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

	return nil
}

func (s *Staker) resetContinuousStakerCycle(weight, potentialReward, totalAccruedRewards, totalAccruedDelegateeRewards uint64) error {
	if s.ContinuationPeriod == 0 {
		return errContinuationPeriodIsZero
	}

	endTime := s.EndTime.Add(s.ContinuationPeriod)

	s.StartTime = s.EndTime
	s.EndTime = endTime
	s.NextTime = endTime
	s.PotentialReward = potentialReward
	s.DelegateeReward = 0
	s.AccruedRewards = totalAccruedRewards
	s.AccruedDelegateeRewards = totalAccruedDelegateeRewards
	s.Weight = weight

	return nil
}

func (s Staker) immutableFieldsAreUnmodified(ms *Staker) bool {
	publicKeysEqual := (s.PublicKey == nil && ms.PublicKey == nil) ||
		(s.PublicKey != nil && s.PublicKey.Equals(ms.PublicKey))

	return s.TxID == ms.TxID &&
		s.NodeID == ms.NodeID &&
		publicKeysEqual &&
		s.SubnetID == ms.SubnetID &&
		s.Priority == ms.Priority
}
