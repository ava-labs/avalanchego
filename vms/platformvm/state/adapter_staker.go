// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// StakingPeriod is the state a staker accumulates over a single staking period.
//
// Immutable transaction data is duplicated onto this type only where callers
// need it to perform lookups for this type.
type StakingPeriod struct {
	// txID is the unique hash of the transaction that added this staker to
	// the staker set.
	txID ids.ID
	// subnetID is the subnet that nodeID is a validator of.
	subnetID ids.ID
	// nodeID is the nodeID of the validator to stake on.
	nodeID ids.NodeID
	// weight is the AVAX being staked for the staker associated with txID.
	weight uint64
	// start is the beginning of the staking period.
	start time.Time
	// end is the end of the staking period.
	end time.Time
	// publicKey is nil for non-primary network validators.
	publicKey *bls.PublicKey

	// TODO: remove priority from this struct
	priority platform.Priority
}

// TxID returns the unique hash of the transaction that added this staker to
// the staker set.
func (p StakingPeriod) TxID() ids.ID {
	return p.txID
}

// SubnetID returns the subnet the staker validates.
func (p StakingPeriod) SubnetID() ids.ID {
	return p.subnetID
}

// NodeID returns the node performing the validation.
func (p StakingPeriod) NodeID() ids.NodeID {
	return p.nodeID
}

// Weight returns the AVAX being staked over this staking period.
func (p StakingPeriod) Weight() uint64 {
	return p.weight
}

// Start returns the beginning of the staking period.
func (p StakingPeriod) Start() time.Time {
	return p.start
}

// End returns the end of the staking period.
func (p StakingPeriod) End() time.Time {
	return p.end
}

// IsPermissionedValidator returns if the period represents a permissioned validator.
// TODO: remove. This can be derived from looking up the tx associated with [StakingPeriod.TxID]
func (p StakingPeriod) IsPermissionedValidator() bool {
	return p.priority.IsPermissionedValidator()
}

func newCurrentStakingPeriod(
	txID ids.ID,
	staker platform.Staker,
	start, end time.Time,
	weight uint64,
) StakingPeriod {
	return StakingPeriod{
		txID:     txID,
		subnetID: staker.SubnetID(),
		nodeID:   staker.NodeID(),
		weight:   weight,
		start:    start,
		end:      end,
		priority: staker.CurrentPriority(),
	}
}

func newPendingStakingPeriod(txID ids.ID, staker platform.ScheduledStaker) StakingPeriod {
	return StakingPeriod{
		txID:     txID,
		subnetID: staker.SubnetID(),
		nodeID:   staker.NodeID(),
		weight:   staker.Weight(),
		start:    staker.StartTime(),
		end:      staker.EndTime(),
		priority: staker.PendingPriority(),
	}
}

func stakingPeriodFromStaker(s *Staker) StakingPeriod {
	return StakingPeriod{
		txID:      s.TxID,
		subnetID:  s.SubnetID,
		nodeID:    s.NodeID,
		weight:    s.Weight,
		start:     s.StartTime,
		end:       s.EndTime,
		priority:  s.Priority,
		publicKey: s.PublicKey,
	}
}

func currentStaker(period StakingPeriod, potentialReward uint64) *Staker {
	return &Staker{
		TxID:            period.txID,
		SubnetID:        period.subnetID,
		NodeID:          period.nodeID,
		PublicKey:       period.publicKey,
		Weight:          period.weight,
		StartTime:       period.start,
		EndTime:         period.end,
		PotentialReward: potentialReward,
		NextTime:        period.end,
		Priority:        period.priority,
	}
}

func pendingStaker(period StakingPeriod) *Staker {
	return &Staker{
		TxID:      period.txID,
		SubnetID:  period.subnetID,
		NodeID:    period.nodeID,
		PublicKey: period.publicKey,
		Weight:    period.weight,
		StartTime: period.start,
		EndTime:   period.end,
		NextTime:  period.start,
		Priority:  period.priority,
	}
}

// CurrentStaker is the sealed sum of [CurrentValidator] and [CurrentDelegator].
// Validator and delegator stay distinct because their legal operations differ;
// the Primary Network and subnets do not.
type CurrentStaker interface {
	StakingPeriod() StakingPeriod
	PotentialReward() uint64
	currentStaker()
}

// newCurrentStaker converts a native record to its public variant. The switch
// is exhaustive: an unhandled priority must not be silently misclassified,
// which would route the record into the wrong collection.
func newCurrentStaker(s *Staker) CurrentStaker {
	switch s.Priority {
	case platform.PrimaryNetworkDelegatorCurrentPriority,
		platform.SubnetPermissionlessDelegatorCurrentPriority:
		return currentDelegatorFromStaker(s)
	case platform.PrimaryNetworkValidatorCurrentPriority,
		platform.SubnetPermissionedValidatorCurrentPriority,
		platform.SubnetPermissionlessValidatorCurrentPriority:
		return currentValidatorFromStaker(s)
	default:
		panic(fmt.Sprintf("unexpected current staker priority %d", s.Priority))
	}
}

func currentDelegatorFromStaker(s *Staker) CurrentDelegator {
	return CurrentDelegator{
		period:          stakingPeriodFromStaker(s),
		potentialReward: s.PotentialReward,
	}
}

// PendingStaker is the sealed sum of [PendingValidator] and [PendingDelegator].
type PendingStaker interface {
	StakingPeriod() StakingPeriod
	pendingStaker()
}

// newPendingStaker converts a native record to its public variant. Exhaustive,
// as in [newCurrentStaker].
func newPendingStaker(s *Staker) PendingStaker {
	switch s.Priority {
	case platform.PrimaryNetworkDelegatorApricotPendingPriority,
		platform.PrimaryNetworkDelegatorBanffPendingPriority,
		platform.SubnetPermissionlessDelegatorPendingPriority:
		return pendingDelegatorFromStaker(s)
	case platform.PrimaryNetworkValidatorPendingPriority,
		platform.SubnetPermissionedValidatorPendingPriority,
		platform.SubnetPermissionlessValidatorPendingPriority:
		return pendingValidatorFromStaker(s)
	default:
		panic(fmt.Sprintf("unexpected pending staker priority %d", s.Priority))
	}
}

// CurrentValidator is an active validator. The network it validates comes from
// its staking period.
type CurrentValidator struct {
	period          StakingPeriod
	potentialReward uint64
}

var _ CurrentStaker = CurrentValidator{}

// NewCurrentValidator returns a current validator built from validator. The
// period bounds are the caller's: a bounded registration carries its own end
// time, while a restaking registration's end time is computed by execution
// from the configured period.
func NewCurrentValidator(
	txID ids.ID,
	validator platform.ValidatorStaker,
	start, end time.Time,
	weight, potentialReward uint64,
) (CurrentValidator, error) {
	period, err := newCurrentValidatorStakingPeriod(txID, validator, start, end, weight)
	if err != nil {
		return CurrentValidator{}, err
	}

	return CurrentValidator{
		period:          period,
		potentialReward: potentialReward,
	}, nil
}

func newCurrentValidatorStakingPeriod(
	txID ids.ID,
	v platform.ValidatorStaker,
	start, end time.Time,
	weight uint64,
) (StakingPeriod, error) {
	period := newCurrentStakingPeriod(txID, v, start, end, weight)

	publicKey, err := getPublicKey(v)
	if err != nil {
		return StakingPeriod{}, err
	}
	period.publicKey = publicKey

	return period, nil
}

// Restake returns the validator's record for its next staking period. The
// validator's identity, transaction, and BLS key carry over; the caller
// provides the new period bounds, weight, and potential reward it calculated.
func (v CurrentValidator) Restake(start, end time.Time, weight, potentialReward uint64) CurrentValidator {
	period := v.period
	period.start = start
	period.end = end
	period.weight = weight

	return CurrentValidator{
		period:          period,
		potentialReward: potentialReward,
	}
}

func (v CurrentValidator) StakingPeriod() StakingPeriod { return v.period }

// Reward returns the validator's potential reward.
func (v CurrentValidator) PotentialReward() uint64 { return v.potentialReward }

func (CurrentValidator) currentStaker() {}

func currentValidatorFromStaker(s *Staker) CurrentValidator {
	return CurrentValidator{
		period:          stakingPeriodFromStaker(s),
		potentialReward: s.PotentialReward,
	}
}

// PendingValidator is a validator waiting to become current. The network it
// validates comes from its staking period; the Primary Network and subnets
// have no distinct pending representation.
type PendingValidator struct {
	period StakingPeriod
}

var _ PendingStaker = PendingValidator{}

// Promote returns the current validator corresponding to v.
func (v PendingValidator) Promote(potentialReward uint64) CurrentValidator {
	period := v.period
	period.priority = platform.PendingToCurrentPriorities[period.priority]

	return CurrentValidator{
		period:          period,
		potentialReward: potentialReward,
	}
}

// Period returns the validator's staking period.
func (v PendingValidator) StakingPeriod() StakingPeriod { return v.period }

func (PendingValidator) pendingStaker() {}

func pendingValidatorFromStaker(s *Staker) PendingValidator {
	return PendingValidator{
		period: stakingPeriodFromStaker(s),
	}
}

// CurrentDelegator is an active delegation.
type CurrentDelegator struct {
	period          StakingPeriod
	potentialReward uint64
}

var _ CurrentStaker = CurrentDelegator{}

// NewCurrentDelegator returns a current delegator built from staker.
func NewCurrentDelegator(
	txID ids.ID,
	staker platform.Delegator,
	startTime, endTime time.Time,
	weight, potentialReward uint64,
) CurrentDelegator {
	return CurrentDelegator{
		period:          newCurrentStakingPeriod(txID, staker, startTime, endTime, weight),
		potentialReward: potentialReward,
	}
}

// StakingPeriod returns the delegator's staking period.
func (d CurrentDelegator) StakingPeriod() StakingPeriod { return d.period }

// PotentialReward returns the delegator's potential reward.
func (d CurrentDelegator) PotentialReward() uint64 { return d.potentialReward }

func (CurrentDelegator) currentStaker() {}

// PendingDelegator is a delegation waiting to become current.
type PendingDelegator struct {
	period StakingPeriod
}

var _ PendingStaker = PendingDelegator{}

// Promote returns the current delegator corresponding to d.
func (d PendingDelegator) Promote(potentialReward uint64) CurrentDelegator {
	period := d.period
	period.priority = platform.PendingToCurrentPriorities[period.priority]

	return CurrentDelegator{
		period:          period,
		potentialReward: potentialReward,
	}
}

// Period returns the delegator's staking period.
func (d PendingDelegator) StakingPeriod() StakingPeriod { return d.period }

func (PendingDelegator) pendingStaker() {}

func pendingDelegatorFromStaker(s *Staker) PendingDelegator {
	return PendingDelegator{
		period: stakingPeriodFromStaker(s),
	}
}
