// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

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
type DelegatorDiffIterator interface {
	Next() bool
	Value() (StakingPeriod, bool)
	Release()
}

type delegatorDiffIterator struct {
	StakerDiffIterator
}

// NewDelegatorDiffIterator returns a typed delegator-diff iterator.
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

// GetCurrentContinuousPrimaryNetworkValidator returns the current continuous
// Primary Network validator with nodeID. It returns
// [ErrNotContinuousPrimaryNetworkValidator] if the validator is bounded.
func (s Adapter) GetCurrentContinuousPrimaryNetworkValidator(nodeID ids.NodeID) (AutoRenewedValidator, error) {
	validator, err := s.GetCurrentPrimaryNetworkValidator(nodeID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}

	tx, _, err := s.chain.GetTx(validator.TxID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}
	if _, ok := tx.Unsigned.(*platform.AddAutoRenewedValidatorTx); !ok {
		return AutoRenewedValidator{}, ErrNotContinuousPrimaryNetworkValidator
	}

	stakingInfo, err := s.chain.GetStakingInfo(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return AutoRenewedValidator{}, err
	}
	return AutoRenewedValidator{
		Validator: validator,
		ContinuousValidatorMetadata: ContinuousValidatorMetadata{
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

// GetStakerTx returns the staking transaction with txID.
func (s Adapter) GetStakerTx(txID ids.ID) (platform.Staker, error) {
	tx, _, err := s.chain.GetTx(txID)
	if err != nil {
		return nil, err
	}
	staker, ok := tx.Unsigned.(platform.Staker)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errUnexpectedStaker, tx.Unsigned)
	}
	return staker, nil
}

func (s Adapter) PutCurrentPrimaryNetworkValidator(tx platform.Staker, validator CurrentPrimaryNetworkValidator) error {
	return s.chain.PutCurrentValidator(currentStaker(
		validator.StakingPeriod,
		publicKey(tx),
		validator.PotentialReward,
	))
}

// PutAutoRenewedValidator adds an auto-renewed validator.
func (s Adapter) PutAutoRenewedValidator(tx *platform.AddAutoRenewedValidatorTx, validator AutoRenewedValidator) error {
	if err := s.PutCurrentPrimaryNetworkValidator(tx, validator.Validator); err != nil {
		return err
	}
	return s.chain.SetStakingInfo(
		constants.PrimaryNetworkID,
		validator.Validator.NodeID(),
		StakingInfo{
			AccruedValidationRewards: validator.AccruedValidationRewards,
			AccruedDelegateeRewards:  validator.AccruedDelegateeRewards,
			AutoCompoundRewardShares: validator.AutoCompoundRewardShares,
			NextPeriod:               validator.NextPeriod,
		},
	)
}

// SetCurrentContinuousPrimaryNetworkValidatorMetadata updates the mutable
// continuous metadata for the validator with nodeID.
func (s Adapter) SetCurrentContinuousPrimaryNetworkValidatorMetadata(nodeID ids.NodeID, metadata ContinuousValidatorMetadata) error {
	if _, err := s.GetCurrentContinuousPrimaryNetworkValidator(nodeID); err != nil {
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

func (s Adapter) PutCurrentSubnetValidator(_ platform.Staker, validator CurrentSubnetValidator) error {
	return s.chain.PutCurrentValidator(currentStaker(
		validator.StakingPeriod,
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

func (s Adapter) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[CurrentDelegator], error) {
	it, err := s.chain.GetCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	return currentDelegatorIterator{Iterator: it}, nil
}

func (s Adapter) PutCurrentDelegator(_ platform.Staker, delegator CurrentDelegator) error {
	return s.chain.PutCurrentDelegator(currentStaker(
		delegator.StakingPeriod,
		nil,
		delegator.PotentialReward,
	))
}

func (s Adapter) DeleteCurrentDelegator(txID ids.ID) error {
	tx, err := s.GetStakerTx(txID)
	if err != nil {
		return err
	}
	it, err := s.chain.GetCurrentDelegatorIterator(tx.SubnetID(), tx.NodeID())
	if err != nil {
		return err
	}
	staker, err := getStakerByTxID(it, txID)
	if err != nil {
		return err
	}
	return s.chain.DeleteCurrentDelegator(staker)
}

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

func (s Adapter) PutPendingPrimaryNetworkValidator(tx platform.Staker, validator PendingPrimaryNetworkValidator) error {
	return s.chain.PutPendingValidator(pendingStaker(
		validator.StakingPeriod,
		publicKey(tx),
	))
}

func (s Adapter) PutPendingSubnetValidator(_ platform.Staker, validator PendingSubnetValidator) error {
	return s.chain.PutPendingValidator(pendingStaker(
		validator.StakingPeriod,
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

func (s Adapter) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[PendingDelegator], error) {
	it, err := s.chain.GetPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	return pendingDelegatorIterator{Iterator: it}, nil
}

func (s Adapter) PutPendingDelegator(_ platform.Staker, delegator PendingDelegator) error {
	s.chain.PutPendingDelegator(pendingStaker(
		delegator.StakingPeriod,
		nil,
	))
	return nil
}

func (s Adapter) DeletePendingDelegator(txID ids.ID) error {
	tx, err := s.GetStakerTx(txID)
	if err != nil {
		return err
	}
	it, err := s.chain.GetPendingDelegatorIterator(tx.SubnetID(), tx.NodeID())
	if err != nil {
		return err
	}
	staker, err := getStakerByTxID(it, txID)
	if err != nil {
		return err
	}
	s.chain.DeletePendingDelegator(staker)
	return nil
}

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
