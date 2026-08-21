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
	return stakingPeriod(staker), isAdded
}

// NewAdapter returns a typed adapter over Chain.
func NewAdapter(chain Chain) Adapter {
	return Adapter{chain: chain}
}

func (s Adapter) GetCurrentPrimaryNetworkValidator(nodeID ids.NodeID) (CurrentPrimaryNetworkValidator, error) {
	staker, err := s.chain.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return CurrentPrimaryNetworkValidator{}, err
	}
	return currentPrimaryNetworkValidator(staker), nil
}

// GetCurrentAutoRenewedValidator returns the current auto-renewed validator
// with nodeID. It returns [ErrNotAutoRenewedValidator] if the validator is
// bounded.
func (s Adapter) GetCurrentAutoRenewedValidator(nodeID ids.NodeID) (AutoRenewedValidator, error) {
	validator, err := s.GetCurrentPrimaryNetworkValidator(nodeID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}

	tx, _, err := s.chain.GetTx(validator.TxID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}
	if _, ok := tx.Unsigned.(*platform.AddAutoRenewedValidatorTx); !ok {
		return AutoRenewedValidator{}, ErrNotAutoRenewedValidator
	}

	stakingInfo, err := s.chain.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}
	return AutoRenewedValidator{
		CurrentPrimaryNetworkValidator: validator,
		AutoRenewedValidatorMetadata: AutoRenewedValidatorMetadata{
			AccruedValidationRewards: stakingInfo.AccruedValidationRewards,
			AccruedDelegateeRewards:  stakingInfo.AccruedDelegateeRewards,
			AutoCompoundRewardShares: stakingInfo.AutoCompoundRewardShares,
			NextPeriod:               stakingInfo.NextPeriod,
		},
	}, nil
}

func (s Adapter) GetCurrentSubnetValidator(subnetID ids.ID, nodeID ids.NodeID) (CurrentSubnetValidator, error) {
	staker, err := s.chain.GetCurrentValidator(subnetID, nodeID)
	if err != nil {
		return CurrentSubnetValidator{}, err
	}
	return currentSubnetValidator(staker), nil
}

// GetStakerTx returns the signed staking transaction with txID.
func (s Adapter) GetStakerTx(txID ids.ID) (*platform.Tx, error) {
	tx, _, err := s.chain.GetTx(txID)
	if err != nil {
		return nil, err
	}
	if _, err := unsignedStaker(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

func (s Adapter) PutCurrentPrimaryNetworkValidator(tx *platform.Tx, validator CurrentPrimaryNetworkValidator) error {
	staker, err := unsignedStaker(tx)
	if err != nil {
		return err
	}
	return s.chain.PutCurrentValidator(currentStaker(
		withTxID(validator.StakingPeriod, tx.ID()),
		publicKey(staker),
		validator.PotentialReward,
	))
}

// PutAutoRenewedValidator adds an auto-renewed validator.
func (s Adapter) PutAutoRenewedValidator(tx *platform.Tx, validator AutoRenewedValidator) error {
	staker, err := unsignedStaker(tx)
	if err != nil {
		return err
	}
	if _, ok := staker.(*platform.AddAutoRenewedValidatorTx); !ok {
		return fmt.Errorf("%w: %T", errUnexpectedStaker, staker)
	}
	if err := s.PutCurrentPrimaryNetworkValidator(tx, validator.CurrentPrimaryNetworkValidator); err != nil {
		return err
	}
	return s.chain.SetStakingInfo(
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
func (s Adapter) SetCurrentAutoRenewedValidatorMetadata(nodeID ids.NodeID, metadata AutoRenewedValidatorMetadata) error {
	if _, err := s.GetCurrentAutoRenewedValidator(nodeID); err != nil {
		return err
	}

	stakingInfo, err := s.chain.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return err
	}
	stakingInfo.AccruedValidationRewards = metadata.AccruedValidationRewards
	stakingInfo.AccruedDelegateeRewards = metadata.AccruedDelegateeRewards
	stakingInfo.AutoCompoundRewardShares = metadata.AutoCompoundRewardShares
	stakingInfo.NextPeriod = metadata.NextPeriod
	return s.chain.SetStakingInfo(constants.PrimaryNetworkID, nodeID, stakingInfo)
}

func (s Adapter) PutCurrentSubnetValidator(tx *platform.Tx, validator CurrentSubnetValidator) error {
	if _, err := unsignedStaker(tx); err != nil {
		return err
	}
	return s.chain.PutCurrentValidator(currentStaker(
		withTxID(validator.StakingPeriod, tx.ID()),
		nil,
		validator.PotentialReward,
	))
}

func (s Adapter) DeleteCurrentPrimaryNetworkValidator(nodeID ids.NodeID) error {
	return s.deleteCurrentValidator(constants.PrimaryNetworkID, nodeID)
}

func (s Adapter) DeleteCurrentSubnetValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	return s.deleteCurrentValidator(subnetID, nodeID)
}

func (s Adapter) deleteCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	staker, err := s.chain.GetCurrentValidator(subnetID, nodeID)
	if err != nil {
		return err
	}
	return s.chain.DeleteCurrentValidator(staker)
}

// TODO: use iter package
func (s Adapter) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[CurrentDelegator], error) {
	it, err := s.chain.GetCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	return currentDelegatorIterator{Iterator: it}, nil
}

func (s Adapter) PutCurrentDelegator(tx *platform.Tx, delegator CurrentDelegator) error {
	if _, err := unsignedStaker(tx); err != nil {
		return err
	}
	return s.chain.PutCurrentDelegator(currentStaker(
		withTxID(delegator.StakingPeriod, tx.ID()),
		nil,
		delegator.PotentialReward,
	))
}

func (s Adapter) DeleteCurrentDelegator(txID ids.ID) error {
	tx, err := s.GetStakerTx(txID)
	if err != nil {
		return err
	}
	staker, err := unsignedStaker(tx)
	if err != nil {
		return err
	}
	it, err := s.chain.GetCurrentDelegatorIterator(staker.SubnetID(), staker.NodeID())
	if err != nil {
		return err
	}
	nativeStaker, err := getStakerByTxID(it, txID)
	if err != nil {
		return err
	}
	return s.chain.DeleteCurrentDelegator(nativeStaker)
}

// TODO: use iter package
func (s Adapter) GetCurrentStakerIterator() (iterator.Iterator[CurrentStaker], error) {
	it, err := s.chain.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}
	return currentStakerIterator{Iterator: it}, nil
}

func (s Adapter) GetPendingPrimaryNetworkValidator(nodeID ids.NodeID) (PendingPrimaryNetworkValidator, error) {
	staker, err := s.chain.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return PendingPrimaryNetworkValidator{}, err
	}
	return pendingPrimaryNetworkValidator(staker), nil
}

func (s Adapter) GetPendingSubnetValidator(subnetID ids.ID, nodeID ids.NodeID) (PendingSubnetValidator, error) {
	staker, err := s.chain.GetPendingValidator(subnetID, nodeID)
	if err != nil {
		return PendingSubnetValidator{}, err
	}
	return pendingSubnetValidator(staker), nil
}

func (s Adapter) PutPendingPrimaryNetworkValidator(tx *platform.Tx, validator PendingPrimaryNetworkValidator) error {
	staker, err := unsignedStaker(tx)
	if err != nil {
		return err
	}
	return s.chain.PutPendingValidator(pendingStaker(
		withTxID(validator.StakingPeriod, tx.ID()),
		publicKey(staker),
	))
}

func (s Adapter) PutPendingSubnetValidator(tx *platform.Tx, validator PendingSubnetValidator) error {
	if _, err := unsignedStaker(tx); err != nil {
		return err
	}
	return s.chain.PutPendingValidator(pendingStaker(
		withTxID(validator.StakingPeriod, tx.ID()),
		nil,
	))
}

func (s Adapter) DeletePendingPrimaryNetworkValidator(nodeID ids.NodeID) error {
	return s.deletePendingValidator(constants.PrimaryNetworkID, nodeID)
}

func (s Adapter) DeletePendingSubnetValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	return s.deletePendingValidator(subnetID, nodeID)
}

func (s Adapter) deletePendingValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	staker, err := s.chain.GetPendingValidator(subnetID, nodeID)
	if err != nil {
		return err
	}
	s.chain.DeletePendingValidator(staker)
	return nil
}

// TODO: use iter package
func (s Adapter) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[PendingDelegator], error) {
	it, err := s.chain.GetPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	return pendingDelegatorIterator{Iterator: it}, nil
}

func (s Adapter) PutPendingDelegator(tx *platform.Tx, delegator PendingDelegator) error {
	if _, err := unsignedStaker(tx); err != nil {
		return err
	}
	s.chain.PutPendingDelegator(pendingStaker(
		withTxID(delegator.StakingPeriod, tx.ID()),
		nil,
	))
	return nil
}

func (s Adapter) DeletePendingDelegator(txID ids.ID) error {
	tx, err := s.GetStakerTx(txID)
	if err != nil {
		return err
	}
	staker, err := unsignedStaker(tx)
	if err != nil {
		return err
	}
	it, err := s.chain.GetPendingDelegatorIterator(staker.SubnetID(), staker.NodeID())
	if err != nil {
		return err
	}
	nativeStaker, err := getStakerByTxID(it, txID)
	if err != nil {
		return err
	}
	s.chain.DeletePendingDelegator(nativeStaker)
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
func (s Adapter) GetPendingStakerIterator() (iterator.Iterator[PendingStaker], error) {
	it, err := s.chain.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}
	return pendingStakerIterator{Iterator: it}, nil
}

func publicKey(staker platform.Staker) *bls.PublicKey {
	publicKey, _, _ := staker.PublicKey()
	return publicKey
}

func unsignedStaker(tx *platform.Tx) (platform.Staker, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: nil transaction", errUnexpectedStaker)
	}
	staker, ok := tx.Unsigned.(platform.Staker)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errUnexpectedStaker, tx.Unsigned)
	}
	return staker, nil
}

func withTxID(period StakingPeriod, txID ids.ID) StakingPeriod {
	period.TxID = txID
	return period
}
