// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

var _ btree.LessFunc[*Staker] = (*Staker).Less

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
	Priority platform.Priority
}

// Equals returns true if this staker is equal to the provided staker.
// If s.Less(other) and other.Less(s) are both false, then it doesn't mean that s.Equals(other) is true.
func (s *Staker) Equals(other *Staker) bool {
	if s == nil && other == nil {
		return true
	}

	if other == nil || s == nil {
		return false
	}

	equalPKs := (s.PublicKey == nil && other.PublicKey == nil) ||
		(s.PublicKey != nil && other.PublicKey != nil && s.PublicKey.Equals(other.PublicKey))

	return s.TxID == other.TxID &&
		s.NodeID == other.NodeID &&
		equalPKs &&
		s.SubnetID == other.SubnetID &&
		s.Weight == other.Weight &&
		s.StartTime.Equal(other.StartTime) &&
		s.EndTime.Equal(other.EndTime) &&
		s.PotentialReward == other.PotentialReward &&
		s.NextTime.Equal(other.NextTime) &&
		s.Priority == other.Priority
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

// NewCurrentStaker returns a current-priority Staker built from [platform.Staker]
// with the provided start time, end time, weight, and potential reward.
func NewCurrentStaker(
	txID ids.ID,
	staker platform.Staker,
	startTime time.Time,
	endTime time.Time,
	weight uint64,
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
		Weight:          weight,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: potentialReward,
		NextTime:        endTime,
		Priority:        staker.CurrentPriority(),
	}, nil
}

// NewPendingStaker returns a pending Staker built from a [platform.ScheduledStaker]
// transaction.
func NewPendingStaker(txID ids.ID, staker platform.ScheduledStaker) (*Staker, error) {
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

// StakingPeriod contains the common state of a pending or current staker.
type StakingPeriod struct {
	TxID      ids.ID
	Weight    uint64
	StartTime time.Time
	EndTime   time.Time
	priority  platform.Priority
	subnetID  ids.ID
	nodeID    ids.NodeID
}

// SubnetID returns the subnet the staker validates.
func (p StakingPeriod) SubnetID() ids.ID {
	return p.subnetID
}

// NodeID returns the node performing the validation.
func (p StakingPeriod) NodeID() ids.NodeID {
	return p.nodeID
}

// Validator contains state shared by pending and current validators.
type Validator struct {
	StakingPeriod
	publicKey *bls.PublicKey
}

// PublicKey returns the validator's public key.
func (v Validator) PublicKey() *bls.PublicKey {
	return v.publicKey
}

// PendingValidator is a validator waiting to become current.
type PendingValidator struct {
	Validator
}

// NewPendingValidator returns a pending validator built from staker.
func NewPendingValidator(txID ids.ID, staker platform.ScheduledStaker) (PendingValidator, error) {
	period, publicKey, err := newPendingStakingPeriod(txID, staker)
	if err != nil {
		return PendingValidator{}, err
	}
	return PendingValidator{
		Validator: Validator{
			StakingPeriod: period,
			publicKey:     publicKey,
		},
	}, nil
}

// Promote returns the current validator corresponding to v.
func (v PendingValidator) Promote(potentialReward uint64) CurrentValidator {
	v.priority = platform.PendingToCurrentPriorities[v.priority]
	return CurrentValidator{
		Validator:       v.Validator,
		PotentialReward: potentialReward,
	}
}

// CurrentValidator is an active validator.
type CurrentValidator struct {
	Validator
	PotentialReward uint64
}

// NewCurrentValidator returns a current validator built from staker.
func NewCurrentValidator(
	txID ids.ID,
	staker platform.Staker,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (CurrentValidator, error) {
	period, publicKey, err := newCurrentStakingPeriod(txID, staker, startTime, endTime, weight)
	if err != nil {
		return CurrentValidator{}, err
	}
	return CurrentValidator{
		Validator: Validator{
			StakingPeriod: period,
			publicKey:     publicKey,
		},
		PotentialReward: potentialReward,
	}, nil
}

// PendingDelegator is a delegation waiting to become current.
type PendingDelegator struct {
	StakingPeriod
}

// NewPendingDelegator returns a pending delegator built from staker.
func NewPendingDelegator(txID ids.ID, staker platform.ScheduledStaker) (PendingDelegator, error) {
	period, _, err := newPendingStakingPeriod(txID, staker)
	return PendingDelegator{StakingPeriod: period}, err
}

// Promote returns the current delegator corresponding to d.
func (d PendingDelegator) Promote(potentialReward uint64) CurrentDelegator {
	d.priority = platform.PendingToCurrentPriorities[d.priority]
	return CurrentDelegator{
		StakingPeriod:   d.StakingPeriod,
		PotentialReward: potentialReward,
	}
}

// CurrentDelegator is an active delegation.
type CurrentDelegator struct {
	StakingPeriod
	PotentialReward uint64
}

// NewCurrentDelegator returns a current delegator built from staker.
func NewCurrentDelegator(
	txID ids.ID,
	staker platform.Staker,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) (CurrentDelegator, error) {
	period, _, err := newCurrentStakingPeriod(txID, staker, startTime, endTime, weight)
	return CurrentDelegator{
		StakingPeriod:   period,
		PotentialReward: potentialReward,
	}, err
}

// CurrentStaker is a validator or delegator in the current staker set.
type CurrentStaker interface {
	Period() StakingPeriod
	Reward() uint64
	currentStaker()
}

func (v CurrentValidator) Period() StakingPeriod { return v.StakingPeriod }
func (v CurrentValidator) Reward() uint64        { return v.PotentialReward }
func (CurrentValidator) currentStaker()          {}

func (d CurrentDelegator) Period() StakingPeriod { return d.StakingPeriod }
func (d CurrentDelegator) Reward() uint64        { return d.PotentialReward }
func (CurrentDelegator) currentStaker()          {}

// PendingStaker is a validator or delegator in the pending staker set.
type PendingStaker interface {
	Period() StakingPeriod
	pendingStaker()
}

func (v PendingValidator) Period() StakingPeriod { return v.StakingPeriod }
func (PendingValidator) pendingStaker()          {}

func (d PendingDelegator) Period() StakingPeriod { return d.StakingPeriod }
func (PendingDelegator) pendingStaker()          {}

func newPendingStakingPeriod(txID ids.ID, staker platform.ScheduledStaker) (StakingPeriod, *bls.PublicKey, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return StakingPeriod{}, nil, err
	}
	return StakingPeriod{
		TxID:      txID,
		Weight:    staker.Weight(),
		StartTime: staker.StartTime(),
		EndTime:   staker.EndTime(),
		priority:  staker.PendingPriority(),
		subnetID:  staker.SubnetID(),
		nodeID:    staker.NodeID(),
	}, publicKey, nil
}

func newCurrentStakingPeriod(
	txID ids.ID,
	staker platform.Staker,
	startTime, endTime time.Time,
	weight uint64,
) (StakingPeriod, *bls.PublicKey, error) {
	publicKey, _, err := staker.PublicKey()
	if err != nil {
		return StakingPeriod{}, nil, err
	}
	return StakingPeriod{
		TxID:      txID,
		Weight:    weight,
		StartTime: startTime,
		EndTime:   endTime,
		priority:  staker.CurrentPriority(),
		subnetID:  staker.SubnetID(),
		nodeID:    staker.NodeID(),
	}, publicKey, nil
}

func pendingStaker(period StakingPeriod, publicKey *bls.PublicKey) *Staker {
	return &Staker{
		TxID:      period.TxID,
		SubnetID:  period.SubnetID(),
		NodeID:    period.NodeID(),
		PublicKey: publicKey,
		Weight:    period.Weight,
		StartTime: period.StartTime,
		EndTime:   period.EndTime,
		NextTime:  period.StartTime,
		Priority:  period.priority,
	}
}

func currentStaker(period StakingPeriod, publicKey *bls.PublicKey, potentialReward uint64) *Staker {
	return &Staker{
		TxID:            period.TxID,
		SubnetID:        period.SubnetID(),
		NodeID:          period.NodeID(),
		PublicKey:       publicKey,
		Weight:          period.Weight,
		StartTime:       period.StartTime,
		EndTime:         period.EndTime,
		PotentialReward: potentialReward,
		NextTime:        period.EndTime,
		Priority:        period.priority,
	}
}

func pendingValidator(staker *Staker) PendingValidator {
	return PendingValidator{
		Validator: Validator{
			StakingPeriod: stakingPeriod(staker),
			publicKey:     staker.PublicKey,
		},
	}
}

func pendingDelegator(staker *Staker) PendingDelegator {
	return PendingDelegator{StakingPeriod: stakingPeriod(staker)}
}

func currentValidator(staker *Staker) CurrentValidator {
	return CurrentValidator{
		Validator: Validator{
			StakingPeriod: stakingPeriod(staker),
			publicKey:     staker.PublicKey,
		},
		PotentialReward: staker.PotentialReward,
	}
}

func currentDelegator(staker *Staker) CurrentDelegator {
	return CurrentDelegator{
		StakingPeriod:   stakingPeriod(staker),
		PotentialReward: staker.PotentialReward,
	}
}

func stakingPeriod(staker *Staker) StakingPeriod {
	return StakingPeriod{
		TxID:      staker.TxID,
		Weight:    staker.Weight,
		StartTime: staker.StartTime,
		EndTime:   staker.EndTime,
		priority:  staker.Priority,
		subnetID:  staker.SubnetID,
		nodeID:    staker.NodeID,
	}
}

func newCurrentStaker(staker *Staker) CurrentStaker {
	if staker.Priority.IsValidator() {
		return currentValidator(staker)
	}
	return currentDelegator(staker)
}

func newPendingStaker(staker *Staker) PendingStaker {
	if staker.Priority.IsValidator() {
		return pendingValidator(staker)
	}
	return pendingDelegator(staker)
}

type currentValidatorIterator struct{ iterator.Iterator[*Staker] }

func (it currentValidatorIterator) Value() CurrentValidator {
	return currentValidator(it.Iterator.Value())
}

type currentDelegatorIterator struct{ iterator.Iterator[*Staker] }

func (it currentDelegatorIterator) Value() CurrentDelegator {
	return currentDelegator(it.Iterator.Value())
}

type pendingDelegatorIterator struct{ iterator.Iterator[*Staker] }

func (it pendingDelegatorIterator) Value() PendingDelegator {
	return pendingDelegator(it.Iterator.Value())
}

type currentStakerIterator struct{ iterator.Iterator[*Staker] }

func (it currentStakerIterator) Value() CurrentStaker {
	return newCurrentStaker(it.Iterator.Value())
}

type pendingStakerIterator struct{ iterator.Iterator[*Staker] }

func (it pendingStakerIterator) Value() PendingStaker {
	return newPendingStaker(it.Iterator.Value())
}

type adapterCurrentStakerIterator struct {
	iterator.Iterator[CurrentStaker]
}

func (it adapterCurrentStakerIterator) Value() *Staker {
	switch staker := it.Iterator.Value().(type) {
	case CurrentValidator:
		return currentStaker(staker.StakingPeriod, staker.publicKey, staker.PotentialReward)
	case CurrentDelegator:
		return currentStaker(staker.StakingPeriod, nil, staker.PotentialReward)
	default:
		panic("unexpected current staker type")
	}
}

type adapterCurrentDelegatorIterator struct {
	iterator.Iterator[CurrentDelegator]
}

func (it adapterCurrentDelegatorIterator) Value() *Staker {
	delegator := it.Iterator.Value()
	return currentStaker(delegator.StakingPeriod, nil, delegator.PotentialReward)
}

type adapterPendingStakerIterator struct {
	iterator.Iterator[PendingStaker]
}

func (it adapterPendingStakerIterator) Value() *Staker {
	switch staker := it.Iterator.Value().(type) {
	case PendingValidator:
		return pendingStaker(staker.StakingPeriod, staker.publicKey)
	case PendingDelegator:
		return pendingStaker(staker.StakingPeriod, nil)
	default:
		panic("unexpected pending staker type")
	}
}

type adapterPendingDelegatorIterator struct {
	iterator.Iterator[PendingDelegator]
}

func (it adapterPendingDelegatorIterator) Value() *Staker {
	return pendingStaker(it.Iterator.Value().StakingPeriod, nil)
}

func getStakerByTxID(it iterator.Iterator[*Staker], txID ids.ID) (*Staker, error) {
	defer it.Release()
	for it.Next() {
		if staker := it.Value(); staker.TxID == txID {
			return staker, nil
		}
	}
	return nil, database.ErrNotFound
}
