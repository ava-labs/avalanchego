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
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Diff     = (*diff)(nil)
	_ Versions = stateGetter{}

	ErrMissingParentState = errors.New("missing parent state")
)

type Diff interface {
	Chain

	Apply(Chain) error
}

type diff struct {
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

	currentStakerDiffs diffStakers
	// map of subnetID -> nodeID -> total accrued delegatee rewards
	modifiedDelegateeRewards map[ids.ID]map[ids.NodeID]uint64
	pendingStakerDiffs       diffStakers

	addedSubnetIDs []ids.ID
	// Subnet ID --> Owner of the subnet
	subnetOwners map[ids.ID]fx.Owner
	// Subnet ID --> Conversion of the subnet
	subnetToL1Conversions map[ids.ID]SubnetToL1Conversion
	// Subnet ID --> Tx that transforms the subnet
	transformedSubnets map[ids.ID]*txs.Tx

	addedChains map[ids.ID][]*txs.Tx

	addedRewardUTXOs map[ids.ID][]*avax.UTXO

	addedTxs map[ids.ID]*txAndStatus

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*avax.UTXO
}

func NewDiff(
	parentID ids.ID,
	stateVersions Versions,
) (Diff, error) {
	parentState, ok := stateVersions.GetState(parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, parentID)
	}

	return &diff{
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

func NewDiffOn(parentState Chain) (Diff, error) {
	return NewDiff(ids.Empty, stateGetter{
		state: parentState,
	})
}

func (d *diff) GetTimestamp() time.Time {
	return d.timestamp
}

func (d *diff) SetTimestamp(timestamp time.Time) {
	d.timestamp = timestamp
}

func (d *diff) GetFeeState() gas.State {
	return d.feeState
}

func (d *diff) SetFeeState(feeState gas.State) {
	d.feeState = feeState
}

func (d *diff) GetL1ValidatorExcess() gas.Gas {
	return d.l1ValidatorExcess
}

func (d *diff) SetL1ValidatorExcess(excess gas.Gas) {
	d.l1ValidatorExcess = excess
}

func (d *diff) GetAccruedFees() uint64 {
	return d.accruedFees
}

func (d *diff) SetAccruedFees(accruedFees uint64) {
	d.accruedFees = accruedFees
}

func (d *diff) GetCurrentSupply(subnetID ids.ID) (uint64, error) {
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

func (d *diff) SetCurrentSupply(subnetID ids.ID, currentSupply uint64) {
	if d.currentSupply == nil {
		d.currentSupply = map[ids.ID]uint64{
			subnetID: currentSupply,
		}
	} else {
		d.currentSupply[subnetID] = currentSupply
	}
}

func (d *diff) GetExpiryIterator() (iterator.Iterator[ExpiryEntry], error) {
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

func (d *diff) HasExpiry(entry ExpiryEntry) (bool, error) {
	if has, modified := d.expiryDiff.modified[entry]; modified {
		return has, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.HasExpiry(entry)
}

func (d *diff) PutExpiry(entry ExpiryEntry) {
	d.expiryDiff.PutExpiry(entry)
}

func (d *diff) DeleteExpiry(entry ExpiryEntry) {
	d.expiryDiff.DeleteExpiry(entry)
}

func (d *diff) GetActiveL1ValidatorsIterator() (iterator.Iterator[L1Validator], error) {
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

func (d *diff) NumActiveL1Validators() int {
	return d.parentNumActiveL1Validators + d.l1ValidatorsDiff.netAddedActive
}

func (d *diff) WeightOfL1Validators(subnetID ids.ID) (uint64, error) {
	if weight, modified := d.l1ValidatorsDiff.modifiedTotalWeight[subnetID]; modified {
		return weight, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.WeightOfL1Validators(subnetID)
}

func (d *diff) GetL1Validator(validationID ids.ID) (L1Validator, error) {
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

func (d *diff) HasL1Validator(subnetID ids.ID, nodeID ids.NodeID) (bool, error) {
	if has, modified := d.l1ValidatorsDiff.hasL1Validator(subnetID, nodeID); modified {
		return has, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.HasL1Validator(subnetID, nodeID)
}

func (d *diff) PutL1Validator(l1Validator L1Validator) error {
	return d.l1ValidatorsDiff.putL1Validator(d, l1Validator)
}

func (d *diff) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	// If the validator was modified in this diff, return the modified
	// validator.
	newValidator, status := d.currentStakerDiffs.GetValidator(subnetID, nodeID)
	switch status {
	case added, modified:
		return newValidator, nil
	case deleted:
		return nil, database.ErrNotFound
	default:
		// If the validator wasn't modified in this diff, ask the parent state.
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetCurrentValidator(subnetID, nodeID)
	}
}

func (d *diff) SetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID, amount uint64) error {
	if d.modifiedDelegateeRewards == nil {
		d.modifiedDelegateeRewards = make(map[ids.ID]map[ids.NodeID]uint64)
	}
	nodes, ok := d.modifiedDelegateeRewards[subnetID]
	if !ok {
		nodes = make(map[ids.NodeID]uint64)
		d.modifiedDelegateeRewards[subnetID] = nodes
	}
	nodes[nodeID] = amount
	return nil
}

func (d *diff) GetDelegateeReward(subnetID ids.ID, nodeID ids.NodeID) (uint64, error) {
	amount, modified := d.modifiedDelegateeRewards[subnetID][nodeID]
	if modified {
		return amount, nil
	}
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetDelegateeReward(subnetID, nodeID)
}

func (d *diff) PutCurrentValidator(staker *Staker) error {
	return d.currentStakerDiffs.PutValidator(staker)
}

func (d *diff) UpdateCurrentValidator(mutatedValidator *Staker) error {
	oldValidator, err := d.GetCurrentValidator(mutatedValidator.SubnetID, mutatedValidator.NodeID)
	if err != nil {
		return err
	}

	if err := oldValidator.ValidateMutation(mutatedValidator); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidStakerMutation, err)
	}

	d.currentStakerDiffs.UpdateValidator(oldValidator, mutatedValidator)
	return nil
}

func (d *diff) ResetContinuousValidatorCycle(
	validator *Staker,
	weight uint64,
	potentialReward, totalAccruedRewards, totalAccruedDelegateeRewards uint64,
) error {
	mutatedValidator := *validator
	if err := (&mutatedValidator).resetContinuousStakerCycle(weight, potentialReward, totalAccruedRewards, totalAccruedDelegateeRewards); err != nil {
		return err
	}

	if err := validator.ValidateMutation(&mutatedValidator); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidStakerMutation, err)
	}

	d.currentStakerDiffs.UpdateValidator(validator, &mutatedValidator)
	return nil
}

func (d *diff) DeleteCurrentValidator(staker *Staker) {
	d.currentStakerDiffs.DeleteValidator(staker)
}

func (d *diff) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetCurrentDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return d.currentStakerDiffs.GetDelegatorIterator(parentIterator, subnetID, nodeID), nil
}

func (d *diff) PutCurrentDelegator(staker *Staker) {
	d.currentStakerDiffs.PutDelegator(staker)
}

func (d *diff) DeleteCurrentDelegator(staker *Staker) {
	d.currentStakerDiffs.DeleteDelegator(staker)
}

func (d *diff) GetCurrentStakerIterator() (iterator.Iterator[*Staker], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.currentStakerDiffs.GetStakerIterator(parentIterator), nil
}

func (d *diff) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
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
		return parentState.GetPendingValidator(subnetID, nodeID)
	}
}

func (d *diff) PutPendingValidator(staker *Staker) error {
	return d.pendingStakerDiffs.PutValidator(staker)
}

func (d *diff) DeletePendingValidator(staker *Staker) {
	d.pendingStakerDiffs.DeleteValidator(staker)
}

func (d *diff) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetPendingDelegatorIterator(subnetID, nodeID)
	if err != nil {
		return nil, err
	}

	return d.pendingStakerDiffs.GetDelegatorIterator(parentIterator, subnetID, nodeID), nil
}

func (d *diff) PutPendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.PutDelegator(staker)
}

func (d *diff) DeletePendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.DeleteDelegator(staker)
}

func (d *diff) GetPendingStakerIterator() (iterator.Iterator[*Staker], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.pendingStakerDiffs.GetStakerIterator(parentIterator), nil
}

func (d *diff) AddSubnet(subnetID ids.ID) {
	d.addedSubnetIDs = append(d.addedSubnetIDs, subnetID)
}

func (d *diff) GetSubnetOwner(subnetID ids.ID) (fx.Owner, error) {
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

func (d *diff) SetSubnetOwner(subnetID ids.ID, owner fx.Owner) {
	d.subnetOwners[subnetID] = owner
}

func (d *diff) GetSubnetToL1Conversion(subnetID ids.ID) (SubnetToL1Conversion, error) {
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

func (d *diff) SetSubnetToL1Conversion(subnetID ids.ID, c SubnetToL1Conversion) {
	d.subnetToL1Conversions[subnetID] = c
}

func (d *diff) GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error) {
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

func (d *diff) AddSubnetTransformation(transformSubnetTxIntf *txs.Tx) {
	transformSubnetTx := transformSubnetTxIntf.Unsigned.(*txs.TransformSubnetTx)
	if d.transformedSubnets == nil {
		d.transformedSubnets = map[ids.ID]*txs.Tx{
			transformSubnetTx.Subnet: transformSubnetTxIntf,
		}
	} else {
		d.transformedSubnets[transformSubnetTx.Subnet] = transformSubnetTxIntf
	}
}

func (d *diff) AddChain(createChainTx *txs.Tx) {
	tx := createChainTx.Unsigned.(*txs.CreateChainTx)
	if d.addedChains == nil {
		d.addedChains = map[ids.ID][]*txs.Tx{
			tx.SubnetID: {createChainTx},
		}
	} else {
		d.addedChains[tx.SubnetID] = append(d.addedChains[tx.SubnetID], createChainTx)
	}
}

func (d *diff) GetTx(txID ids.ID) (*txs.Tx, status.Status, error) {
	if tx, exists := d.addedTxs[txID]; exists {
		return tx.tx, tx.status, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, status.Unknown, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetTx(txID)
}

func (d *diff) AddTx(tx *txs.Tx, status status.Status) {
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

func (d *diff) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	if d.addedRewardUTXOs == nil {
		d.addedRewardUTXOs = make(map[ids.ID][]*avax.UTXO)
	}
	d.addedRewardUTXOs[txID] = append(d.addedRewardUTXOs[txID], utxo)
}

func (d *diff) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
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

func (d *diff) AddUTXO(utxo *avax.UTXO) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*avax.UTXO{
			utxo.InputID(): utxo,
		}
	} else {
		d.modifiedUTXOs[utxo.InputID()] = utxo
	}
}

func (d *diff) DeleteUTXO(utxoID ids.ID) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*avax.UTXO{
			utxoID: nil,
		}
	} else {
		d.modifiedUTXOs[utxoID] = nil
	}
}

func (d *diff) Apply(baseState Chain) error {
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
			switch validatorDiff.validatorStatus {
			case added:
				if err := baseState.PutCurrentValidator(validatorDiff.validator); err != nil {
					return err
				}
			case deleted:
				baseState.DeleteCurrentValidator(validatorDiff.validator)
			case modified:
				if err := baseState.UpdateCurrentValidator(validatorDiff.validator); err != nil {
					return err
				}
			}

			addedDelegatorIterator := iterator.FromTree(validatorDiff.addedDelegators)
			for addedDelegatorIterator.Next() {
				baseState.PutCurrentDelegator(addedDelegatorIterator.Value())
			}
			addedDelegatorIterator.Release()

			for _, delegator := range validatorDiff.deletedDelegators {
				baseState.DeleteCurrentDelegator(delegator)
			}
		}
	}
	for subnetID, nodes := range d.modifiedDelegateeRewards {
		for nodeID, amount := range nodes {
			if err := baseState.SetDelegateeReward(subnetID, nodeID, amount); err != nil {
				return err
			}
		}
	}
	for _, subnetValidatorDiffs := range d.pendingStakerDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			switch validatorDiff.validatorStatus {
			case added:
				if err := baseState.PutPendingValidator(validatorDiff.validator); err != nil {
					return err
				}
			case deleted:
				baseState.DeletePendingValidator(validatorDiff.validator)
			case modified:
				return fmt.Errorf("pending stakers cannot be modified")
			}

			addedDelegatorIterator := iterator.FromTree(validatorDiff.addedDelegators)
			for addedDelegatorIterator.Next() {
				baseState.PutPendingDelegator(addedDelegatorIterator.Value())
			}
			addedDelegatorIterator.Release()

			for _, delegator := range validatorDiff.deletedDelegators {
				baseState.DeletePendingDelegator(delegator)
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
