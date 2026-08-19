// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

var (
	_ Chain    = (*Diff)(nil)
	_ Versions = stateGetter{}

	ErrMissingParentState = errors.New("missing parent state")
)

// Diff is a copy-on-write layer on top of a parent [Chain]. It records
// mutations locally and applies them to the parent via [Diff.Apply].
type Diff struct {
	parentID      ids.ID
	stateVersions Versions

	timestamp                   time.Time
	feeState                    gas.State
	l1ValidatorExcess           gas.Gas
	accruedFees                 uint64
	parentNumActiveL1Validators int

	// Subnet ID --> supply of native asset of the subnet
	currentSupply map[ids.ID]uint64

	expiryDiff       *expiryDiff
	l1ValidatorsDiff *l1ValidatorsDiff

	// map of subnetID -> nodeID -> staking info
	modifiedStakingInfo map[ids.ID]map[ids.NodeID]StakingInfo
	currentStakerDiffs  diffStakers
	pendingStakerDiffs  diffStakers

	addedSubnetIDs []ids.ID
	// Subnet ID --> Owner of the subnet
	subnetOwners map[ids.ID]fx.Owner
	// Subnet ID --> Conversion of the subnet
	subnetToL1Conversions map[ids.ID]SubnetToL1Conversion
	// Subnet ID --> Tx that transforms the subnet
	transformedSubnets map[ids.ID]*platform.Tx

	addedChains map[ids.ID][]*platform.Tx

	addedRewardUTXOs map[ids.ID][]*avax.UTXO

	addedTxs map[ids.ID]*txAndStatus

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*avax.UTXO
}

// NewDiff returns a new [Diff] whose parent is identified by parentID within
// stateVersions.
func NewDiff(
	parentID ids.ID,
	stateVersions Versions,
	allowAddingStakerAfterDeletion StakerAdditionAfterDeletionLegality,
) (*Diff, error) {
	parentState, ok := stateVersions.GetState(parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, parentID)
	}
	return &Diff{
		modifiedStakingInfo: make(map[ids.ID]map[ids.NodeID]StakingInfo),
		currentStakerDiffs: diffStakers{
			isAdditionAfterDeletionAllowed: allowAddingStakerAfterDeletion,
		},
		pendingStakerDiffs: diffStakers{
			isAdditionAfterDeletionAllowed: allowAddingStakerAfterDeletion,
		},
		parentID:                    parentID,
		stateVersions:               stateVersions,
		timestamp:                   parentState.GetTimestamp(),
		feeState:                    parentState.GetFeeState(),
		l1ValidatorExcess:           parentState.GetL1ValidatorExcess(),
		accruedFees:                 parentState.GetAccruedFees(),
		parentNumActiveL1Validators: parentState.NumActiveL1Validators(),
		expiryDiff:                  newExpiryDiff(),
		l1ValidatorsDiff:            newL1ValidatorsDiff(),
		subnetOwners:                make(map[ids.ID]fx.Owner),
		subnetToL1Conversions:       make(map[ids.ID]SubnetToL1Conversion),
	}, nil
}

type stateGetter struct {
	state Chain
}

func (s stateGetter) GetState(ids.ID) (Chain, bool) {
	return s.state, true
}

func NewDiffOn(parentState Chain, allowAddingStakerAfterDeletion StakerAdditionAfterDeletionLegality) (*Diff, error) {
	return NewDiff(ids.Empty, stateGetter{
		state: parentState,
	}, allowAddingStakerAfterDeletion)
}

func (d *Diff) GetTimestamp() time.Time {
	return d.timestamp
}

func (d *Diff) SetTimestamp(timestamp time.Time) {
	d.timestamp = timestamp
}

func (d *Diff) GetFeeState() gas.State {
	return d.feeState
}

func (d *Diff) SetFeeState(feeState gas.State) {
	d.feeState = feeState
}

func (d *Diff) GetL1ValidatorExcess() gas.Gas {
	return d.l1ValidatorExcess
}

func (d *Diff) SetL1ValidatorExcess(excess gas.Gas) {
	d.l1ValidatorExcess = excess
}

func (d *Diff) GetAccruedFees() uint64 {
	return d.accruedFees
}

func (d *Diff) SetAccruedFees(accruedFees uint64) {
	d.accruedFees = accruedFees
}

func (d *Diff) GetCurrentSupply(subnetID ids.ID) (uint64, error) {
	supply, ok := d.currentSupply[subnetID]
	if ok {
		return supply, nil
	}

	// If the subnet supply wasn't modified in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetCurrentSupply(subnetID)
}

func (d *Diff) SetCurrentSupply(subnetID ids.ID, currentSupply uint64) {
	if d.currentSupply == nil {
		d.currentSupply = map[ids.ID]uint64{
			subnetID: currentSupply,
		}
	} else {
		d.currentSupply[subnetID] = currentSupply
	}
}

func (d *Diff) GetExpiryIterator() (iterator.Iterator[ExpiryEntry], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetExpiryIterator()
	if err != nil {
		return nil, err
	}

	return d.expiryDiff.getExpiryIterator(parentIterator), nil
}

func (d *Diff) HasExpiry(entry ExpiryEntry) (bool, error) {
	if has, modified := d.expiryDiff.modified[entry]; modified {
		return has, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.HasExpiry(entry)
}

func (d *Diff) PutExpiry(entry ExpiryEntry) {
	d.expiryDiff.PutExpiry(entry)
}

func (d *Diff) DeleteExpiry(entry ExpiryEntry) {
	d.expiryDiff.DeleteExpiry(entry)
}

func (d *Diff) GetActiveL1ValidatorsIterator() (iterator.Iterator[L1Validator], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetActiveL1ValidatorsIterator()
	if err != nil {
		return nil, err
	}

	return d.l1ValidatorsDiff.getActiveL1ValidatorsIterator(parentIterator), nil
}

func (d *Diff) NumActiveL1Validators() int {
	return d.parentNumActiveL1Validators + d.l1ValidatorsDiff.netAddedActive
}

func (d *Diff) WeightOfL1Validators(subnetID ids.ID) (uint64, error) {
	if weight, modified := d.l1ValidatorsDiff.modifiedTotalWeight[subnetID]; modified {
		return weight, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.WeightOfL1Validators(subnetID)
}

func (d *Diff) GetL1Validator(validationID ids.ID) (L1Validator, error) {
	if l1Validator, modified := d.l1ValidatorsDiff.modified[validationID]; modified {
		if l1Validator.isDeleted() {
			return L1Validator{}, database.ErrNotFound
		}
		return l1Validator, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return L1Validator{}, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetL1Validator(validationID)
}

func (d *Diff) HasL1Validator(subnetID ids.ID, nodeID ids.NodeID) (bool, error) {
	if has, modified := d.l1ValidatorsDiff.hasL1Validator(subnetID, nodeID); modified {
		return has, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.HasL1Validator(subnetID, nodeID)
}

func (d *Diff) PutL1Validator(l1Validator L1Validator) error {
	return d.l1ValidatorsDiff.putL1Validator(d, l1Validator)
}

func (d *Diff) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (CurrentValidator, error) {
	staker, err := d.getCurrentValidator(subnetID, nodeID)
	if err != nil {
		return CurrentValidator{}, err
	}
	return currentValidator(staker), nil
}

func (d *Diff) getCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	// If the validator was modified in this diff, return the modified
	// validator.
	newValidator, status := d.currentStakerDiffs.GetValidator(subnetID, nodeID)
	switch status {
	case added:
		return newValidator, nil
	case deleted:
		return nil, database.ErrNotFound
	default:
		// If the validator wasn't modified in this diff, ask the parent state.
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		period, err := parentState.GetCurrentValidator(subnetID, nodeID)
		if err != nil {
			return nil, err
		}
		return currentStaker(period.StakingPeriod, period.publicKey, period.PotentialReward), nil
	}
}

func (d *Diff) SetStakingInfo(subnetID ids.ID, nodeID ids.NodeID, stakingInfo StakingInfo) error {
	if _, err := d.GetCurrentValidator(subnetID, nodeID); err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}

	d.setStakingInfo(subnetID, nodeID, stakingInfo)
	return nil
}

func (d *Diff) setStakingInfo(subnetID ids.ID, nodeID ids.NodeID, stakingInfo StakingInfo) {
	nodes, ok := d.modifiedStakingInfo[subnetID]
	if !ok {
		nodes = make(map[ids.NodeID]StakingInfo)
		d.modifiedStakingInfo[subnetID] = nodes
	}
	nodes[nodeID] = stakingInfo
}

func (d *Diff) GetStakingInfo(subnetID ids.ID, nodeID ids.NodeID) (StakingInfo, error) {
	// Defensive check to guarantee we are only being called for a validator that we know about.
	if _, err := d.GetCurrentValidator(subnetID, nodeID); err != nil {
		return StakingInfo{}, err
	}

	// See if this was set in the current [Diff].
	if stakingInfo, ok := d.modifiedStakingInfo[subnetID][nodeID]; ok {
		return stakingInfo, nil
	}

	// We do not have this validator -- ask the parent.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return StakingInfo{}, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetStakingInfo(subnetID, nodeID)
}

func (d *Diff) PutCurrentValidator(validator CurrentValidator) error {
	return d.putCurrentValidator(currentStaker(validator.StakingPeriod, validator.publicKey, validator.PotentialReward))
}

func (d *Diff) putCurrentValidator(staker *Staker) error {
	if _, err := d.getCurrentValidator(staker.SubnetID, staker.NodeID); err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("getting current validator: %w", err)
	} else if err == nil {
		return fmt.Errorf("%w: %s", errUnexpectedStaker, staker.NodeID)
	}

	if err := d.currentStakerDiffs.PutValidator(staker); err != nil {
		return fmt.Errorf("putting validator: %w", err)
	}

	d.setStakingInfo(staker.SubnetID, staker.NodeID, StakingInfo{})

	return nil
}

func (d *Diff) DeleteCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	staker, err := d.getCurrentValidator(subnetID, nodeID)
	if err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}
	return d.deleteCurrentValidator(staker)
}

func (d *Diff) deleteCurrentValidator(staker *Staker) error {
	if _, err := d.getCurrentValidator(staker.SubnetID, staker.NodeID); err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}

	if err := verifyNoDelegators(d, staker.SubnetID, staker.NodeID); err != nil {
		return err
	}

	d.currentStakerDiffs.DeleteValidator(staker)
	delete(d.modifiedStakingInfo[staker.SubnetID], staker.NodeID)

	return nil
}

func (d *Diff) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[CurrentDelegator], error) {
	it, err := d.getCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	return currentDelegatorIterator{Iterator: it}, nil
}

func (d *Diff) getCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	parentStakerIterator := adapterCurrentDelegatorIterator{Iterator: parentIterator}

	return d.currentStakerDiffs.GetDelegatorIterator(
		parentStakerIterator,
		subnetID,
		nodeID,
	), nil
}

func (d *Diff) PutCurrentDelegator(delegator CurrentDelegator) error {
	return d.putCurrentDelegator(currentStaker(delegator.StakingPeriod, nil, delegator.PotentialReward))
}

func (d *Diff) putCurrentDelegator(staker *Staker) error {
	if _, err := d.getCurrentValidator(staker.SubnetID, staker.NodeID); err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}

	d.currentStakerDiffs.PutDelegator(staker)
	return nil
}

func (d *Diff) DeleteCurrentDelegator(subnetID ids.ID, nodeID ids.NodeID, txID ids.ID) error {
	it, err := d.getCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return err
	}
	staker, err := getStakerByTxID(it, txID)
	if err != nil {
		return err
	}
	return d.deleteCurrentDelegator(staker)
}

func (d *Diff) deleteCurrentDelegator(staker *Staker) error {
	if _, err := d.getCurrentValidator(staker.SubnetID, staker.NodeID); err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}

	d.currentStakerDiffs.DeleteDelegator(staker)
	return nil
}

func (d *Diff) GetCurrentStakerIterator() (iterator.Iterator[CurrentStaker], error) {
	it, err := d.getCurrentStakerIterator()
	if err != nil {
		return nil, err
	}
	return currentStakerIterator{Iterator: it}, nil
}

func (d *Diff) getCurrentStakerIterator() (iterator.Iterator[*Staker], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}
	parentStakerIterator := adapterCurrentStakerIterator{Iterator: parentIterator}

	return d.currentStakerDiffs.GetStakerIterator(parentStakerIterator), nil
}

func (d *Diff) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (PendingValidator, error) {
	staker, err := d.getPendingValidator(subnetID, nodeID)
	if err != nil {
		return PendingValidator{}, err
	}
	return pendingValidator(staker), nil
}

func (d *Diff) getPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	// If the validator was modified in this diff, return the modified
	// validator.
	newValidator, status := d.pendingStakerDiffs.GetValidator(subnetID, nodeID)
	switch status {
	case added:
		return newValidator, nil
	case deleted:
		return nil, database.ErrNotFound
	default:
		// If the validator wasn't modified in this diff, ask the parent state.
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		period, err := parentState.GetPendingValidator(subnetID, nodeID)
		if err != nil {
			return nil, err
		}
		return pendingStaker(period.StakingPeriod, period.publicKey), nil
	}
}

func (d *Diff) PutPendingValidator(validator PendingValidator) error {
	return d.putPendingValidator(pendingStaker(validator.StakingPeriod, validator.publicKey))
}

func (d *Diff) putPendingValidator(staker *Staker) error {
	return d.pendingStakerDiffs.PutValidator(staker)
}

func (d *Diff) DeletePendingValidator(subnetID ids.ID, nodeID ids.NodeID) error {
	staker, err := d.getPendingValidator(subnetID, nodeID)
	if err != nil {
		return err
	}
	d.deletePendingValidator(staker)
	return nil
}

func (d *Diff) deletePendingValidator(staker *Staker) {
	d.pendingStakerDiffs.DeleteValidator(staker)
}

func (d *Diff) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[PendingDelegator], error) {
	it, err := d.getPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	return pendingDelegatorIterator{Iterator: it}, nil
}

func (d *Diff) getPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}
	parentStakerIterator := adapterPendingDelegatorIterator{Iterator: parentIterator}

	return d.pendingStakerDiffs.GetDelegatorIterator(
		parentStakerIterator,
		subnetID,
		nodeID,
	), nil
}

func (d *Diff) PutPendingDelegator(delegator PendingDelegator) error {
	d.putPendingDelegator(pendingStaker(delegator.StakingPeriod, nil))
	return nil
}

func (d *Diff) putPendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.PutDelegator(staker)
}

func (d *Diff) DeletePendingDelegator(subnetID ids.ID, nodeID ids.NodeID, txID ids.ID) error {
	it, err := d.getPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return err
	}
	staker, err := getStakerByTxID(it, txID)
	if err != nil {
		return err
	}
	d.deletePendingDelegator(staker)
	return nil
}

func (d *Diff) deletePendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.DeleteDelegator(staker)
}

func (d *Diff) GetPendingStakerIterator() (iterator.Iterator[PendingStaker], error) {
	it, err := d.getPendingStakerIterator()
	if err != nil {
		return nil, err
	}
	return pendingStakerIterator{Iterator: it}, nil
}

func (d *Diff) getPendingStakerIterator() (iterator.Iterator[*Staker], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}
	parentStakerIterator := adapterPendingStakerIterator{Iterator: parentIterator}

	return d.pendingStakerDiffs.GetStakerIterator(parentStakerIterator), nil
}

func (d *Diff) AddSubnet(subnetID ids.ID) {
	d.addedSubnetIDs = append(d.addedSubnetIDs, subnetID)
}

func (d *Diff) GetSubnetOwner(subnetID ids.ID) (fx.Owner, error) {
	owner, exists := d.subnetOwners[subnetID]
	if exists {
		return owner, nil
	}

	// If the subnet owner was not assigned in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, ErrMissingParentState
	}
	return parentState.GetSubnetOwner(subnetID)
}

func (d *Diff) SetSubnetOwner(subnetID ids.ID, owner fx.Owner) {
	d.subnetOwners[subnetID] = owner
}

func (d *Diff) GetSubnetToL1Conversion(subnetID ids.ID) (SubnetToL1Conversion, error) {
	if c, ok := d.subnetToL1Conversions[subnetID]; ok {
		return c, nil
	}

	// If the subnet conversion was not assigned in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return SubnetToL1Conversion{}, ErrMissingParentState
	}
	return parentState.GetSubnetToL1Conversion(subnetID)
}

func (d *Diff) SetSubnetToL1Conversion(subnetID ids.ID, c SubnetToL1Conversion) {
	d.subnetToL1Conversions[subnetID] = c
}

func (d *Diff) GetSubnetTransformation(subnetID ids.ID) (*platform.Tx, error) {
	tx, exists := d.transformedSubnets[subnetID]
	if exists {
		return tx, nil
	}

	// If the subnet wasn't transformed in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, ErrMissingParentState
	}
	return parentState.GetSubnetTransformation(subnetID)
}

func (d *Diff) AddSubnetTransformation(transformSubnetTxIntf *platform.Tx) {
	transformSubnetTx := transformSubnetTxIntf.Unsigned.(*platform.TransformSubnetTx)
	if d.transformedSubnets == nil {
		d.transformedSubnets = map[ids.ID]*platform.Tx{
			transformSubnetTx.Subnet: transformSubnetTxIntf,
		}
	} else {
		d.transformedSubnets[transformSubnetTx.Subnet] = transformSubnetTxIntf
	}
}

func (d *Diff) AddChain(createChainTx *platform.Tx) {
	tx := createChainTx.Unsigned.(*platform.CreateChainTx)
	if d.addedChains == nil {
		d.addedChains = map[ids.ID][]*platform.Tx{
			tx.SubnetID: {createChainTx},
		}
	} else {
		d.addedChains[tx.SubnetID] = append(d.addedChains[tx.SubnetID], createChainTx)
	}
}

// TODO update GetTx to not return the tx
// TODO remove tx status
func (d *Diff) GetTx(txID ids.ID) (*platform.Tx, status.Status, error) {
	if tx, exists := d.addedTxs[txID]; exists {
		return tx.tx, tx.status, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, status.Unknown, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetTx(txID)
}

func (d *Diff) AddTx(tx *platform.Tx, status status.Status) {
	txID := tx.ID()
	txStatus := &txAndStatus{
		tx:     tx,
		status: status,
	}
	if d.addedTxs == nil {
		d.addedTxs = map[ids.ID]*txAndStatus{
			txID: txStatus,
		}
	} else {
		d.addedTxs[txID] = txStatus
	}
}

func (d *Diff) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	if d.addedRewardUTXOs == nil {
		d.addedRewardUTXOs = make(map[ids.ID][]*avax.UTXO)
	}
	d.addedRewardUTXOs[txID] = append(d.addedRewardUTXOs[txID], utxo)
}

func (d *Diff) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	utxo, modified := d.modifiedUTXOs[utxoID]
	if !modified {
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetUTXO(utxoID)
	}
	if utxo == nil {
		return nil, database.ErrNotFound
	}
	return utxo, nil
}

func (d *Diff) AddUTXO(utxo *avax.UTXO) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*avax.UTXO{
			utxo.InputID(): utxo,
		}
	} else {
		d.modifiedUTXOs[utxo.InputID()] = utxo
	}
}

func (d *Diff) DeleteUTXO(utxoID ids.ID) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*avax.UTXO{
			utxoID: nil,
		}
	} else {
		d.modifiedUTXOs[utxoID] = nil
	}
}

func (d *Diff) Apply(baseState Chain) error {
	baseState.SetTimestamp(d.timestamp)
	baseState.SetFeeState(d.feeState)
	baseState.SetL1ValidatorExcess(d.l1ValidatorExcess)
	baseState.SetAccruedFees(d.accruedFees)
	for subnetID, supply := range d.currentSupply {
		baseState.SetCurrentSupply(subnetID, supply)
	}
	for entry, isAdded := range d.expiryDiff.modified {
		if isAdded {
			baseState.PutExpiry(entry)
		} else {
			baseState.DeleteExpiry(entry)
		}
	}
	// Ensure that all l1Validator deletions happen before any l1Validator
	// additions. This ensures that a subnetID+nodeID pair that was deleted and
	// then re-added in a single diff can't get reordered into the addition
	// happening first; which would return an error.
	for _, l1Validator := range d.l1ValidatorsDiff.modified {
		if !l1Validator.isDeleted() {
			continue
		}
		if err := baseState.PutL1Validator(l1Validator); err != nil {
			return err
		}
	}
	for _, l1Validator := range d.l1ValidatorsDiff.modified {
		if l1Validator.isDeleted() {
			continue
		}
		if err := baseState.PutL1Validator(l1Validator); err != nil {
			return err
		}
	}
	for _, subnetValidatorDiffs := range d.currentStakerDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			// Delegators must be removed before their respective validators
			for _, delegator := range validatorDiff.deletedDelegators {
				if err := baseState.DeleteCurrentDelegator(delegator.SubnetID, delegator.NodeID, delegator.TxID); err != nil {
					return fmt.Errorf("deleting current delegator: %w", err)
				}
			}

			// We might have removed the validator and then added it in the same diff.
			// We therefore first delete and then only after add it.
			if validatorDiff.removed != nil {
				if err := baseState.DeleteCurrentValidator(validatorDiff.removed.SubnetID, validatorDiff.removed.NodeID); err != nil {
					return fmt.Errorf("deleting current validator: %w", err)
				}
			}
			if validatorDiff.added != nil {
				if err := baseState.PutCurrentValidator(currentValidator(validatorDiff.added)); err != nil {
					return err
				}
			}

			// Delegators must be added after validators are added
			addedDelegatorIterator := iterator.FromTree(validatorDiff.addedDelegators)
			for addedDelegatorIterator.Next() {
				if err := baseState.PutCurrentDelegator(currentDelegator(addedDelegatorIterator.Value())); err != nil {
					addedDelegatorIterator.Release()
					return fmt.Errorf("putting current delegator: %w", err)
				}
			}
			addedDelegatorIterator.Release()
		}
	}
	for subnetID, nodes := range d.modifiedStakingInfo {
		for nodeID, stakingInfo := range nodes {
			if err := baseState.SetStakingInfo(subnetID, nodeID, stakingInfo); err != nil {
				return fmt.Errorf("setting staking info: %w", err)
			}
		}
	}
	for _, subnetValidatorDiffs := range d.pendingStakerDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			// We might have removed the validator and then added it in the same diff.
			// We therefore first delete and then only after add it.
			if validatorDiff.removed != nil {
				if err := baseState.DeletePendingValidator(validatorDiff.removed.SubnetID, validatorDiff.removed.NodeID); err != nil {
					return err
				}
			}
			if validatorDiff.added != nil {
				if err := baseState.PutPendingValidator(pendingValidator(validatorDiff.added)); err != nil {
					return err
				}
			}

			addedDelegatorIterator := iterator.FromTree(validatorDiff.addedDelegators)
			for addedDelegatorIterator.Next() {
				if err := baseState.PutPendingDelegator(pendingDelegator(addedDelegatorIterator.Value())); err != nil {
					addedDelegatorIterator.Release()
					return err
				}
			}
			addedDelegatorIterator.Release()

			for _, delegator := range validatorDiff.deletedDelegators {
				if err := baseState.DeletePendingDelegator(delegator.SubnetID, delegator.NodeID, delegator.TxID); err != nil {
					return err
				}
			}
		}
	}
	for _, subnetID := range d.addedSubnetIDs {
		baseState.AddSubnet(subnetID)
	}
	for _, tx := range d.transformedSubnets {
		baseState.AddSubnetTransformation(tx)
	}
	for _, chains := range d.addedChains {
		for _, chain := range chains {
			baseState.AddChain(chain)
		}
	}
	for _, tx := range d.addedTxs {
		baseState.AddTx(tx.tx, tx.status)
	}
	for txID, utxos := range d.addedRewardUTXOs {
		for _, utxo := range utxos {
			baseState.AddRewardUTXO(txID, utxo)
		}
	}
	for utxoID, utxo := range d.modifiedUTXOs {
		if utxo != nil {
			baseState.AddUTXO(utxo)
		} else {
			baseState.DeleteUTXO(utxoID)
		}
	}
	for subnetID, owner := range d.subnetOwners {
		baseState.SetSubnetOwner(subnetID, owner)
	}
	for subnetID, c := range d.subnetToL1Conversions {
		baseState.SetSubnetToL1Conversion(subnetID, c)
	}
	return nil
}
