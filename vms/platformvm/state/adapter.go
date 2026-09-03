// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// Adapter provides typed access to the current and pending staker sets.
// It adapts the legacy native staker APIs without changing their storage or
// ordering behavior. Both [*State] and [*Diff] implement [Stakers]. Writes
// flow through diffs by executor convention rather than by type constraint,
// because reads legitimately run over parent state.
type Adapter struct {
	legacy Stakers
}

// NewAdapter returns a typed adapter over the staking slice of chain.
func NewAdapter(ls Stakers) Adapter {
	return Adapter{legacy: ls}
}

// GetCurrentValidator returns the current validator on subnetID with nodeID.
// It returns [database.ErrNotFound] if the validator is not in the current
// validator set.
func (a Adapter) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (CurrentValidator, error) {
	v, err := a.legacy.GetCurrentValidator(subnetID, nodeID)
	if err != nil {
		return CurrentValidator{}, err
	}

	return currentValidatorFromStaker(v), nil
}

// PutCurrentValidator adds validator to the current validator set. The record
// is self-contained: its TxID and BLS key were captured from the adding
// transaction at construction, so re-insertion (e.g. auto-renewal) needs no tx.
func (a Adapter) PutCurrentValidator(v CurrentValidator) error {
	return a.legacy.PutCurrentValidator(currentStaker(v.StakingPeriod, v.Reward()))
}

// DeleteCurrentValidator removes the current validator on subnetID with
// nodeID from the current validator set.
func (a Adapter) DeleteCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	v, err := a.legacy.GetCurrentValidator(subnetID, nodeID)
	if err != nil {
		return err
	}
	return a.legacy.DeleteCurrentValidator(v)
}

// RestakedRewards is the reward state a restaking validator carries across
// cycles: amounts earned in previous cycles and restaked rather than paid.
// Execution rewrites it only at a cycle boundary, alongside the staking
// period it restakes. It is the zero value for a validator that has never
// restaked.
type RestakedRewards struct {
	// Validation is the sum of validation rewards restaked from previous
	// cycles.
	Validation uint64
	// Delegatee is the sum of delegatee rewards restaked from previous
	// cycles.
	Delegatee uint64
}

// GetRestakedRewards returns the rewards restaked in previous cycles by the
// validator on subnetID with nodeID, or an error wrapping
// [database.ErrNotFound] if the validator is not in the current validator
// set. Rewards that were never set read as the zero value, indistinguishable
// from rewards explicitly set to zero: the validator has never restaked.
func (a Adapter) GetRestakedRewards(subnetID ids.ID, nodeID ids.NodeID) (RestakedRewards, error) {
	si, err := a.legacy.GetStakingInfo(subnetID, nodeID)
	if err != nil {
		return RestakedRewards{}, err
	}

	return RestakedRewards{
		Validation: si.AccruedValidationRewards,
		Delegatee:  si.AccruedDelegateeRewards,
	}, nil
}

// SetRestakedRewards sets the rewards restaked in previous cycles by the
// validator on subnetID with nodeID. It returns an error wrapping
// [database.ErrNotFound] if the validator is not in the current validator
// set.
func (a Adapter) SetRestakedRewards(subnetID ids.ID, nodeID ids.NodeID, restaked RestakedRewards) error {
	si, err := a.legacy.GetStakingInfo(subnetID, nodeID)
	if err != nil {
		return err
	}

	si.AccruedValidationRewards = restaked.Validation
	si.AccruedDelegateeRewards = restaked.Delegatee

	return a.legacy.SetStakingInfo(subnetID, nodeID, si)
}

// GetDelegateeReward returns the delegatee reward accrued during the current
// staking period by the validator on subnetID with nodeID, or an error
// wrapping [database.ErrNotFound] if the validator is not in the current
// validator set. A reward that was never set reads as zero,
// indistinguishable from a reward explicitly set to zero: no commission is
// pending.
func (a Adapter) GetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID) (uint64, error) {
	si, err := a.legacy.GetStakingInfo(subnetID, nodeID)
	if err != nil {
		return 0, err
	}

	return si.DelegateeReward, nil
}

// SetDelegateeReward sets the delegatee reward accrued during the current
// staking period by the validator on subnetID with nodeID. It returns an
// error wrapping [database.ErrNotFound] if the validator is not in the
// current validator set.
func (a Adapter) SetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID, delegateeReward uint64) error {
	si, err := a.legacy.GetStakingInfo(subnetID, nodeID)
	if err != nil {
		return err
	}

	si.DelegateeReward = delegateeReward

	return a.legacy.SetStakingInfo(subnetID, nodeID, si)
}

// RestakeConfig defines how a validator's next staking period is derived when
// its current period ends. The zero value means the validator does not
// restake: state does not distinguish a bounded validator from a restaking
// validator that is gracefully exiting — the adding transaction kind, checked
// by execution, is the discriminator.
type RestakeConfig struct {
	// AutoCompoundRewardShares is the percentage of rewards to restake at the
	// end of a cycle.
	AutoCompoundRewardShares uint32
	// NextPeriod is the next validation cycle duration, in seconds.
	NextPeriod uint64
}

// GetRestakeConfig returns the restake configuration of the validator on
// subnetID with nodeID, or an error wrapping [database.ErrNotFound] if the
// validator is not in the current validator set. A configuration that was
// never set reads as the zero value, indistinguishable from one explicitly
// set to zero: the validator will not restake. Use the adding transaction
// kind, not this value, to decide whether a validator is capable of
// restaking.
func (a Adapter) GetRestakeConfig(subnetID ids.ID, nodeID ids.NodeID) (RestakeConfig, error) {
	si, err := a.legacy.GetStakingInfo(subnetID, nodeID)
	if err != nil {
		return RestakeConfig{}, err
	}

	return RestakeConfig{
		AutoCompoundRewardShares: si.AutoCompoundRewardShares,
		NextPeriod:               si.NextPeriod,
	}, nil
}

// SetRestakeConfig sets the restake configuration of the validator on
// subnetID with nodeID. It returns an error wrapping [database.ErrNotFound]
// if the validator is not in the current validator set: a validator's config
// can only be written after the validator itself is put.
func (a Adapter) SetRestakeConfig(subnetID ids.ID, nodeID ids.NodeID, config RestakeConfig) error {
	si, err := a.legacy.GetStakingInfo(subnetID, nodeID)
	if err != nil {
		return err
	}

	si.AutoCompoundRewardShares = config.AutoCompoundRewardShares
	si.NextPeriod = config.NextPeriod

	return a.legacy.SetStakingInfo(subnetID, nodeID, si)
}

type currentDelegatorIterator struct{ iterator.Iterator[*Staker] }

func (it currentDelegatorIterator) Value() CurrentDelegator {
	return currentDelegatorFromStaker(it.Iterator.Value())
}

// GetCurrentDelegatorIterator returns the current delegators to the validator
// on subnetID with nodeID, ordered by their removal from the current staker
// set.
//
// TODO: use iter package
func (a Adapter) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[CurrentDelegator], error) {
	it, err := a.legacy.GetCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return currentDelegatorIterator{Iterator: it}, nil
}

// PutCurrentDelegator adds delegator to the current delegator set. As with
// [Adapter.PutCurrentValidator], the record carries its own TxID.
func (a Adapter) PutCurrentDelegator(delegator CurrentDelegator) error {
	return a.legacy.PutCurrentDelegator(currentStaker(delegator.StakingPeriod, delegator.Reward()))
}

// DeleteCurrentDelegator removes delegator from the current delegator set. As
// with puts, the record is self-contained: the native record is reconstructed
// from it without a transaction lookup.
func (a Adapter) DeleteCurrentDelegator(delegator CurrentDelegator) error {
	return a.legacy.DeleteCurrentDelegator(currentStaker(delegator.StakingPeriod, delegator.Reward()))
}

// GetPendingValidator returns the pending validator on subnetID with nodeID.
// It returns [database.ErrNotFound] if the validator is not in the pending
// validator set.
func (a Adapter) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (PendingValidator, error) {
	v, err := a.legacy.GetPendingValidator(subnetID, nodeID)
	if err != nil {
		return PendingValidator{}, err
	}

	return pendingValidatorFromStaker(v), nil
}

// PutPendingValidator adds the validator that tx registers to the pending set.
// A pending validator accumulates no state, so its record is derived entirely
// from tx.
func (a Adapter) PutPendingValidator(tx *platform.Tx) error {
	s, err := scheduledStaker(tx)
	if err != nil {
		return err
	}

	v, ok := s.(platform.ValidatorStaker)
	if !ok {
		return fmt.Errorf("%w: %T does not register a validator", errUnexpectedStaker, s)
	}

	period := newPendingStakingPeriod(tx.ID(), s)
	period.publicKey, err = getPublicKey(v)
	if err != nil {
		return err
	}

	return a.legacy.PutPendingValidator(pendingStaker(period))
}

// DeletePendingValidator removes the pending validator on subnetID with
// nodeID from the pending validator set.
func (a Adapter) DeletePendingValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	v, err := a.legacy.GetPendingValidator(subnetID, nodeID)
	if err != nil {
		return err
	}

	a.legacy.DeletePendingValidator(v)
	return nil
}

type pendingDelegatorIterator struct{ iterator.Iterator[*Staker] }

func (it pendingDelegatorIterator) Value() PendingDelegator {
	return pendingDelegatorFromStaker(it.Iterator.Value())
}

// GetPendingDelegatorIterator returns the pending delegators to the validator
// on subnetID with nodeID, ordered by their removal from the pending staker
// set.
//
// TODO: use iter package
func (a Adapter) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[PendingDelegator], error) {
	it, err := a.legacy.GetPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return pendingDelegatorIterator{Iterator: it}, nil
}

// PutPendingDelegator adds the delegation that tx registers to the pending set.
// A pending delegator accumulates no state, so its record is derived entirely
// from tx. Delegators never carry a BLS key.
func (a Adapter) PutPendingDelegator(tx *platform.Tx) error {
	s, err := scheduledStaker(tx)
	if err != nil {
		return err
	}

	if _, err := delegatorStaker(tx); err != nil {
		return err
	}

	a.legacy.PutPendingDelegator(pendingStaker(newPendingStakingPeriod(tx.ID(), s)))
	return nil
}

// DeletePendingDelegator removes delegator from the pending delegator set, as
// in [Adapter.DeleteCurrentDelegator].
func (a Adapter) DeletePendingDelegator(delegator PendingDelegator) {
	a.legacy.DeletePendingDelegator(pendingStaker(delegator.StakingPeriod))
}

type currentStakerIterator struct{ iterator.Iterator[*Staker] }

func (it currentStakerIterator) Value() CurrentStaker {
	return newCurrentStaker(it.Iterator.Value())
}

// GetCurrentStakerIterator returns all current stakers, ordered by their
// removal from the current staker set.
//
// TODO: use iter package
func (a Adapter) GetCurrentStakerIterator() (iterator.Iterator[CurrentStaker], error) {
	it, err := a.legacy.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}

	return currentStakerIterator{Iterator: it}, nil
}

type pendingStakerIterator struct{ iterator.Iterator[*Staker] }

func (it pendingStakerIterator) Value() PendingStaker {
	return newPendingStaker(it.Iterator.Value())
}

// GetPendingStakerIterator returns all pending stakers, ordered by their
// removal from the pending staker set.
//
// TODO: use iter package
func (a Adapter) GetPendingStakerIterator() (iterator.Iterator[PendingStaker], error) {
	it, err := a.legacy.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}

	return pendingStakerIterator{Iterator: it}, nil
}

// DelegatorDiffIterator iterates over current delegator removals and pending
// delegator additions in the order the changes take effect.
// TODO: use iter package
type DelegatorDiffIterator interface {
	Next() bool
	// Value returns the delegation that is changing, whether it is being
	// added to the current delegator set, and the time the change takes
	// effect.
	Value() (StakingPeriod, bool, time.Time)
	Release()
}

type delegatorDiffIterator struct {
	StakerDiffIterator
}

type adapterCurrentDelegatorIterator struct {
	iterator.Iterator[CurrentDelegator]
}

func (it adapterCurrentDelegatorIterator) Value() *Staker {
	d := it.Iterator.Value()
	return currentStaker(d.StakingPeriod, d.Reward())
}

type adapterPendingDelegatorIterator struct {
	iterator.Iterator[PendingDelegator]
}

func (it adapterPendingDelegatorIterator) Value() *Staker {
	return pendingStaker(it.Iterator.Value().StakingPeriod)
}

// NewDelegatorDiffIterator returns a typed delegator-diff iterator.
// TODO: use iter package
func NewDelegatorDiffIterator(
	currentIterator iterator.Iterator[CurrentDelegator],
	pendingIterator iterator.Iterator[PendingDelegator],
) DelegatorDiffIterator {
	return delegatorDiffIterator{
		StakerDiffIterator: NewStakerDiffIterator(
			adapterCurrentDelegatorIterator{Iterator: currentIterator},
			adapterPendingDelegatorIterator{Iterator: pendingIterator},
		),
	}
}

func (it delegatorDiffIterator) Value() (StakingPeriod, bool, time.Time) {
	s, isAdded := it.StakerDiffIterator.Value()
	return stakingPeriodFromStaker(s), isAdded, s.NextTime
}

// getPublicKey returns the BLS key to store for a validator on subnetID.
// Only Primary Network validators carry one, so a subnet validator gets nil
// without consulting the transaction.
func getPublicKey(validator platform.ValidatorStaker) (*bls.PublicKey, error) {
	// Non-primary network validators never register a public key.
	if validator.SubnetID() != constants.PrimaryNetworkID {
		return nil, nil
	}

	// Primary network validators must register a public key.
	primaryNetworkValidator, ok := validator.(platform.PermissionlessValidator)
	if !ok {
		return nil, fmt.Errorf("%w: %T does not register a public key", errUnexpectedStaker, validator)
	}
	publicKey, _, _ := primaryNetworkValidator.PublicKey()

	return publicKey, nil
}

// scheduledStaker asserts that tx sets a start time. Only pre-Durango stakers
// do, which is also the only case that inserts into the pending set.
func scheduledStaker(tx *platform.Tx) (platform.ScheduledStaker, error) {
	s, err := unsignedStaker(tx)
	if err != nil {
		return nil, err
	}

	scheduled, ok := s.(platform.ScheduledStaker)
	if !ok {
		return nil, fmt.Errorf("%w: %T has no start time", errUnexpectedStaker, s)
	}
	return scheduled, nil
}

// delegatorStaker asserts that tx registers a delegation.
func delegatorStaker(tx *platform.Tx) (platform.Delegator, error) {
	s, err := unsignedStaker(tx)
	if err != nil {
		return nil, err
	}

	d, ok := s.(platform.Delegator)
	if !ok {
		return nil, fmt.Errorf("%w: %T does not register a delegation", errUnexpectedStaker, s)
	}
	return d, nil
}

func unsignedStaker(tx *platform.Tx) (platform.Staker, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: nil transaction", errUnexpectedStaker)
	}

	s, ok := tx.Unsigned.(platform.Staker)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errUnexpectedStaker, tx.Unsigned)
	}
	return s, nil
}
