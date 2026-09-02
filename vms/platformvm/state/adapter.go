// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// Adapter provides typed access to the current and pending staker sets.
// It adapts the legacy native staker APIs without changing their storage or
// ordering behavior.
type Adapter struct {
	chain Chain
}

// DelegatorDiffIterator iterates over current delegator removals and pending
// delegator additions in the order the changes take effect.
// TODO: use iter package
type DelegatorDiffIterator interface {
	Next() bool
	Value() (StakingPeriod, bool)
	Release()
}

type delegatorDiffIterator struct {
	StakerDiffIterator
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

func (it delegatorDiffIterator) Value() (StakingPeriod, bool) {
	staker, isAdded := it.StakerDiffIterator.Value()
	return stakingPeriodFromStaker(staker), isAdded
}

// NewAdapter returns a typed adapter over Chain.
func NewAdapter(chain Chain) Adapter {
	return Adapter{chain: chain}
}

func (a Adapter) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (CurrentValidator, error) {
	staker, err := a.chain.GetCurrentValidator(subnetID, nodeID)
	if err != nil {
		return CurrentValidator{}, err
	}
	return currentValidatorFromStaker(staker), nil
}

// GetCurrentAutoRenewedValidator returns the current auto-renewed validator
// with nodeID. It returns [ErrNotAutoRenewedValidator] if the validator is
// bounded.
func (a Adapter) GetCurrentAutoRenewedValidator(nodeID ids.NodeID) (AutoRenewedValidator, error) {
	validator, err := a.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}

	tx, _, err := a.chain.GetTx(validator.TxID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}
	if _, ok := tx.Unsigned.(*platform.AddAutoRenewedValidatorTx); !ok {
		return AutoRenewedValidator{}, ErrNotAutoRenewedValidator
	}

	stakingInfo, err := a.chain.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}
	return AutoRenewedValidator{
		CurrentValidator: validator,
		AutoRenewedStakingPeriod: AutoRenewedStakingPeriod{
			AccruedValidationRewards: stakingInfo.AccruedValidationRewards,
			AccruedDelegateeRewards:  stakingInfo.AccruedDelegateeRewards,
			AutoCompoundRewardShares: stakingInfo.AutoCompoundRewardShares,
			NextPeriod:               stakingInfo.NextPeriod,
		},
	}, nil
}

// GetStakerTx returns the signed staking transaction with txID.
func (a Adapter) GetStakerTx(txID ids.ID) (*platform.Tx, error) {
	tx, _, err := a.chain.GetTx(txID)
	if err != nil {
		return nil, err
	}
	if _, err := verifyStakerType(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// PutCurrentValidator adds validator to the current validator set. The record
// is self-contained: its TxID and BLS key were captured from the adding
// transaction at construction, so re-insertion (e.g. auto-renewal) needs no tx.
func (a Adapter) PutCurrentValidator(validator CurrentValidator) error {
	return a.chain.PutCurrentValidator(currentStaker(
		validator.StakingPeriod,
		validator.PotentialReward,
	))
}

// PutAutoRenewedValidator adds an auto-renewed validator. Only
// [NewCurrentAutoRenewedValidator] builds one, so the record is known to
// originate from a [platform.AddAutoRenewedValidatorTx].
func (a Adapter) PutAutoRenewedValidator(validator AutoRenewedValidator) error {
	if err := a.PutCurrentValidator(validator.CurrentValidator); err != nil {
		return err
	}
	return a.chain.SetStakingInfo(
		constants.PrimaryNetworkID,
		validator.NodeID(),
		StakingInfo{
			AccruedValidationRewards: validator.AccruedValidationRewards,
			AccruedDelegateeRewards:  validator.AccruedDelegateeRewards,
			AutoCompoundRewardShares: validator.AutoCompoundRewardShares,
			NextPeriod:               validator.NextPeriod,
		},
	)
}

// SetCurrentAutoRenewedValidatorMetadata updates the mutable metadata for the
// auto-renewed validator with nodeID.
func (a Adapter) SetCurrentAutoRenewedValidatorMetadata(nodeID ids.NodeID, metadata AutoRenewedStakingPeriod) error {
	if _, err := a.GetCurrentAutoRenewedValidator(nodeID); err != nil {
		return err
	}

	stakingInfo, err := a.chain.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return err
	}
	stakingInfo.AccruedValidationRewards = metadata.AccruedValidationRewards
	stakingInfo.AccruedDelegateeRewards = metadata.AccruedDelegateeRewards
	stakingInfo.AutoCompoundRewardShares = metadata.AutoCompoundRewardShares
	stakingInfo.NextPeriod = metadata.NextPeriod
	return a.chain.SetStakingInfo(constants.PrimaryNetworkID, nodeID, stakingInfo)
}

func (a Adapter) DeleteCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	staker, err := a.chain.GetCurrentValidator(subnetID, nodeID)
	if err != nil {
		return err
	}
	return a.chain.DeleteCurrentValidator(staker)
}

// TODO: use iter package
func (a Adapter) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[CurrentDelegator], error) {
	it, err := a.chain.GetCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return currentDelegatorIterator{Iterator: it}, nil

}

// PutCurrentDelegator adds delegator to the current delegator set. As with
// [Adapter.PutCurrentValidator], the record carries its own TxID.
func (a Adapter) PutCurrentDelegator(delegator CurrentDelegator) error {
	return a.chain.PutCurrentDelegator(currentStaker(
		delegator.StakingPeriod,
		delegator.PotentialReward,
	))
}

func (a Adapter) DeleteCurrentDelegator(txID ids.ID) error {
	tx, err := a.GetStakerTx(txID)
	if err != nil {
		return err
	}
	staker, err := delegatorStaker(tx)
	if err != nil {
		return err
	}
	it, err := a.chain.GetCurrentDelegatorIterator(staker.SubnetID(), staker.NodeID())
	if err != nil {
		return err
	}
	nativeStaker, err := getStakerByTxID(it, txID)
	if err != nil {
		return err
	}
	return a.chain.DeleteCurrentDelegator(nativeStaker)
}

// TODO: use iter package
func (a Adapter) GetCurrentStakerIterator() (iterator.Iterator[CurrentStaker], error) {
	it, err := a.chain.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}
	return currentStakerIterator{Iterator: it}, nil
}

func (a Adapter) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (PendingValidator, error) {
	staker, err := a.chain.GetPendingValidator(subnetID, nodeID)
	if err != nil {
		return PendingValidator{}, err
	}
	return pendingValidatorFromStaker(staker), nil
}

// PutPendingValidator adds the validator that tx registers to the pending set.
// A pending validator accumulates no state, so its record is derived entirely
// from tx.
func (a Adapter) PutPendingValidator(tx *platform.Tx) error {
	staker, err := scheduledStaker(tx)
	if err != nil {
		return err
	}
	validator, ok := staker.(platform.ValidatorStaker)
	if !ok {
		return fmt.Errorf("%w: %T does not register a validator", errUnexpectedStaker, staker)
	}
	period, err := newPendingStakingPeriod(tx.ID(), staker)
	if err != nil {
		return err
	}
	period.publicKey, err = validatorPublicKey(validator, validator.SubnetID())
	if err != nil {
		return err
	}
	return a.chain.PutPendingValidator(pendingStaker(period))
}

func (a Adapter) DeletePendingValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	staker, err := a.chain.GetPendingValidator(subnetID, nodeID)
	if err != nil {
		return err
	}
	a.chain.DeletePendingValidator(staker)
	return nil
}

// TODO: use iter package
func (a Adapter) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[PendingDelegator], error) {
	it, err := a.chain.GetPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	return pendingDelegatorIterator{Iterator: it}, nil
}

// PutPendingDelegator adds the delegation that tx registers to the pending set.
// A pending delegator accumulates no state, so its record is derived entirely
// from tx. Delegators never carry a BLS key.
func (a Adapter) PutPendingDelegator(tx *platform.Tx) error {
	staker, err := scheduledStaker(tx)
	if err != nil {
		return err
	}
	if _, err := delegatorStaker(tx); err != nil {
		return err
	}
	period, err := newPendingStakingPeriod(tx.ID(), staker)
	if err != nil {
		return err
	}
	a.chain.PutPendingDelegator(pendingStaker(period))
	return nil
}

func (a Adapter) DeletePendingDelegator(txID ids.ID) error {
	tx, err := a.GetStakerTx(txID)
	if err != nil {
		return err
	}
	staker, err := delegatorStaker(tx)
	if err != nil {
		return err
	}
	it, err := a.chain.GetPendingDelegatorIterator(staker.SubnetID(), staker.NodeID())
	if err != nil {
		return err
	}
	nativeStaker, err := getStakerByTxID(it, txID)
	if err != nil {
		return err
	}
	a.chain.DeletePendingDelegator(nativeStaker)
	return nil
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

// TODO: use iter package
func (a Adapter) GetPendingStakerIterator() (iterator.Iterator[PendingStaker], error) {
	it, err := a.chain.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}
	return pendingStakerIterator{Iterator: it}, nil
}

// validatorPublicKey returns the BLS key to store for a validator on subnetID.
// Only Primary Network validators carry one, so a subnet validator gets nil
// without consulting the transaction.
func validatorPublicKey(staker platform.ValidatorStaker, subnetID ids.ID) (*bls.PublicKey, error) {
	if subnetID != constants.PrimaryNetworkID {
		return nil, nil
	}
	keyed, ok := staker.(platform.KeyedStaker)
	if !ok {
		return nil, fmt.Errorf("%w: %T does not register a public key", errUnexpectedStaker, staker)
	}
	publicKey, _, _ := keyed.PublicKey()
	return publicKey, nil
}

// scheduledStaker asserts that tx sets a start time. Only pre-Durango stakers
// do, which is also the only case that inserts into the pending set.
func scheduledStaker(tx *platform.Tx) (platform.ScheduledStaker, error) {
	staker, err := verifyStakerType(tx)
	if err != nil {
		return nil, err
	}
	scheduled, ok := staker.(platform.ScheduledStaker)
	if !ok {
		return nil, fmt.Errorf("%w: %T has no start time", errUnexpectedStaker, staker)
	}
	return scheduled, nil
}

func verifyStakerType(tx *platform.Tx) (platform.Staker, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: nil transaction", errUnexpectedStaker)
	}
	staker, ok := tx.Unsigned.(platform.Staker)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errUnexpectedStaker, tx.Unsigned)
	}
	return staker, nil
}

// delegatorStaker asserts that tx registers a delegation.
func delegatorStaker(tx *platform.Tx) (platform.DelegatorStaker, error) {
	staker, err := verifyStakerType(tx)
	if err != nil {
		return nil, err
	}
	delegator, ok := staker.(platform.DelegatorStaker)
	if !ok {
		return nil, fmt.Errorf("%w: %T does not register a delegation", errUnexpectedStaker, staker)
	}
	return delegator, nil
}
