// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
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
	TxID      ids.ID
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	SubnetID  ids.ID

	// Following ContinuousStakingFork, weight can change across staking cycles, due to
	// restaking of a fraction of rewards accrued by the staker
	Weight uint64

	// StartTime is the time this staker enters the current validators set.
	// Pre ContinuousStakingFork, StartTime is set by the Add*Tx creating the staker.
	// Post ContinuousStakingFork StartTime is initially set to chain time when Add*Tx is accepted;
	// Upon restaking, StartTime is moved ahead by StakingDuration.
	StartTime time.Time

	// StakingPeriod is the time the staker will stake.
	// Note that it's not necessarily true that StakingPeriod == EndTime - StartTime.
	// StakingPeriod does not change during a staker lifetime.
	StakingPeriod time.Duration

	// EndTime is the time this staker exits the current validators set.
	// Pre ContinuousStakingFork, EndTime is set by the Add*Tx creating the staker.
	// Post ContinuousStakingFork EndTime is set initially to mockable.MaxTime. An
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

// Following the introduction of continuous staking, this is not necessarily
// equal to s.StakingPeriod, which is the staking period set upon creation.
func (s *Staker) CurrentStakingPeriod() time.Duration {
	return s.NextTime.Sub(s.StartTime)
}

// Continuous stakers have their NextTime set prior to EndTime. NextTime is always
// a finite time, while EndTime is mockable.MaxTime for continuous stakers not yet stopped
// and a finite time for continuous stakers required to stop at some time in the future.
func (s *Staker) ShouldRestake() bool {
	return s.NextTime.Before(s.EndTime)
}

func NewCurrentStaker(
	txID ids.ID,
	stakerTx txs.Staker,
	startTime time.Time,
	endTime time.Time,
	potentialReward uint64,
) (*Staker, error) {
	publicKey, _, err := stakerTx.PublicKey()
	if err != nil {
		return nil, err
	}

	stakingPeriod := stakerTx.StakingPeriod()
	nextTime := startTime.Add(stakingPeriod)

	return &Staker{
		TxID:            txID,
		NodeID:          stakerTx.NodeID(),
		PublicKey:       publicKey,
		SubnetID:        stakerTx.SubnetID(),
		Weight:          stakerTx.Weight(),
		StartTime:       startTime,
		StakingPeriod:   stakingPeriod,
		EndTime:         endTime,
		PotentialReward: potentialReward,
		NextTime:        nextTime,
		Priority:        stakerTx.CurrentPriority(),
	}, nil
}

// ShiftStakerAheadInPlace should be called only if a.ShouldRestake is true
func ShiftStakerAheadInPlace(s *Staker, newStartTime time.Time) {
	if s.Priority.IsPending() {
		return // never shift pending stakers. Consider erroring here.
	}
	if !newStartTime.After(s.StartTime) {
		return // never shift stakers backward. Consider erroring here.
	}
	if !s.ShouldRestake() {
		return // can't shift, staker reached EOL
	}

	currentStakingPeriod := s.CurrentStakingPeriod()
	s.StartTime = newStartTime
	s.NextTime = newStartTime.Add(currentStakingPeriod)
}

// Note that UpdateStakingPeriodInPlace does not modify s.StakingPeriod which is required
// to hold the default staking period, as defined in the tx creating the staker object
func UpdateStakingPeriodInPlace(s *Staker, newStakingPeriod time.Duration) {
	if s.Priority.IsPending() {
		return // never shift pending stakers. Consider erroring here.
	}
	if newStakingPeriod <= 0 {
		return // Never shorten staking period to zero. Consider erroring here.
	}

	s.NextTime = s.StartTime.Add(newStakingPeriod)
}

func MarkStakerForRemovalInPlaceBeforeTime(s *Staker, stopTime time.Time) {
	if !stopTime.Before(s.EndTime) {
		return
	}
	if stopTime.Equal(mockable.MaxTime) {
		s.EndTime = mockable.MaxTime // used in loading on staker from disk upon startup
	}

	end := s.NextTime
	for ; end.Before(stopTime); end = end.Add(s.StakingPeriod) {
	}
	s.EndTime = end
}

func IncreaseStakerWeightInPlace(s *Staker, newStakerWeight uint64) {
	if s.Priority.IsPending() {
		return // never shift pending stakers. Consider erroring here.
	}
	if newStakerWeight <= s.Weight {
		return // only increase staker weight. Consider erroring here.
	}

	s.Weight = newStakerWeight
}

func CalculateEvaluationPeriod(s *Staker, minStakingDuration time.Duration) time.Duration {
	if !s.Priority.IsContinuousValidator() {
		// evaluation period concept does not apply. Consider erroing
		return minStakingDuration
	}

	var (
		divisor    = int64(1)
		evalPeriod = s.StakingPeriod
	)

	// evaluation period is the smallest power of 2 divisor of s.StakingPeriod
	// which is larger than minStakingDuration
	for {
		div := time.Second * time.Duration(divisor)
		if s.StakingPeriod%div != 0 {
			break
		}
		candidateEvalPeriod := s.StakingPeriod / time.Duration(divisor)
		if candidateEvalPeriod < minStakingDuration {
			break
		}
		divisor *= 2
		evalPeriod = candidateEvalPeriod
	}

	return evalPeriod
}

func NewPendingStaker(txID ids.ID, stakerTx txs.PreContinuousStakingStaker) (*Staker, error) {
	publicKey, _, err := stakerTx.PublicKey()
	if err != nil {
		return nil, err
	}
	var (
		startTime = stakerTx.StartTime()
		endTime   = stakerTx.EndTime()
		duration  = endTime.Sub(startTime)
	)

	return &Staker{
		TxID:          txID,
		NodeID:        stakerTx.NodeID(),
		PublicKey:     publicKey,
		SubnetID:      stakerTx.SubnetID(),
		Weight:        stakerTx.Weight(),
		StartTime:     startTime,
		EndTime:       endTime,
		StakingPeriod: duration,
		NextTime:      startTime,
		Priority:      stakerTx.PendingPriority(),
	}, nil
}
