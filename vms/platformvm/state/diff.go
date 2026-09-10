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

	// Subnet ID --> Owner of the subnet
	subnetOwners map[ids.ID]fx.Owner
	// Subnet ID --> Conversion of the subnet
	subnetToL1Conversions map[ids.ID]SubnetToL1Conversion
	// Subnet ID --> Tx that transforms the subnet
	transformedSubnets map[ids.ID]*platform.Tx

	addedTxs map[ids.ID]*txAndStatus

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*avax.UTXO

	// applyOps records mutations in the order they were issued. [Diff.Apply]
	// replays them against the base state.
	applyOps []func(Chain) error
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
	d.recordOp(func(c Chain) error {
		c.SetTimestamp(timestamp)
		return nil
	})
}

func (d *Diff) GetFeeState() gas.State {
	return d.feeState
}

func (d *Diff) SetFeeState(feeState gas.State) {
	d.feeState = feeState
	d.recordOp(func(c Chain) error {
		c.SetFeeState(feeState)
		return nil
	})
}

func (d *Diff) GetL1ValidatorExcess() gas.Gas {
	return d.l1ValidatorExcess
}

func (d *Diff) SetL1ValidatorExcess(excess gas.Gas) {
	d.l1ValidatorExcess = excess
	d.recordOp(func(c Chain) error {
		c.SetL1ValidatorExcess(excess)
		return nil
	})
}

func (d *Diff) GetAccruedFees() uint64 {
	return d.accruedFees
}

func (d *Diff) SetAccruedFees(accruedFees uint64) {
	d.accruedFees = accruedFees
	d.recordOp(func(c Chain) error {
		c.SetAccruedFees(accruedFees)
		return nil
	})
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
	d.recordOp(func(c Chain) error {
		c.SetCurrentSupply(subnetID, currentSupply)
		return nil
	})
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
	d.recordOp(func(c Chain) error {
		c.PutExpiry(entry)
		return nil
	})
}

func (d *Diff) DeleteExpiry(entry ExpiryEntry) {
	d.expiryDiff.DeleteExpiry(entry)
	d.recordOp(func(c Chain) error {
		c.DeleteExpiry(entry)
		return nil
	})
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
	if err := d.l1ValidatorsDiff.putL1Validator(d, l1Validator); err != nil {
		return err
	}
	d.recordOp(func(c Chain) error {
		return c.PutL1Validator(l1Validator)
	})
	return nil
}

// GetCurrentValidator returns a native current validator.
//
// Deprecated: use [NewAdapter] and the typed validator accessors.
func (d *Diff) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
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
		return parentState.GetCurrentValidator(subnetID, nodeID)
	}
}

func (d *Diff) SetStakingInfo(subnetID ids.ID, nodeID ids.NodeID, stakingInfo StakingInfo) error {
	if _, err := d.GetCurrentValidator(subnetID, nodeID); err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}

	d.setStakingInfo(subnetID, nodeID, stakingInfo)
	d.recordOp(func(c Chain) error {
		return c.SetStakingInfo(subnetID, nodeID, stakingInfo)
	})
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

// PutCurrentValidator adds a native current validator.
//
// Deprecated: use [NewAdapter] and the typed validator accessors.
func (d *Diff) PutCurrentValidator(staker *Staker) error {
	if _, err := d.GetCurrentValidator(staker.SubnetID, staker.NodeID); err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("getting current validator: %w", err)
	} else if err == nil {
		return fmt.Errorf("%w: %s", errUnexpectedStaker, staker.NodeID)
	}

	if err := d.currentStakerDiffs.PutValidator(staker); err != nil {
		return fmt.Errorf("putting validator: %w", err)
	}

	d.setStakingInfo(staker.SubnetID, staker.NodeID, StakingInfo{})

	d.recordOp(func(c Chain) error {
		return c.PutCurrentValidator(staker)
	})
	return nil
}

// DeleteCurrentValidator removes a native current validator.
//
// Deprecated: use [NewAdapter] and the typed validator accessors.
func (d *Diff) DeleteCurrentValidator(staker *Staker) error {
	if _, err := d.GetCurrentValidator(staker.SubnetID, staker.NodeID); err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}

	if err := verifyNoDelegators(d, staker.SubnetID, staker.NodeID); err != nil {
		return err
	}

	d.currentStakerDiffs.DeleteValidator(staker)
	delete(d.modifiedStakingInfo[staker.SubnetID], staker.NodeID)

	d.recordOp(func(c Chain) error {
		return c.DeleteCurrentValidator(staker)
	})
	return nil
}

// GetCurrentDelegatorIterator returns native current delegators.
//
// Deprecated: use [Adapter.GetCurrentDelegatorIterator].
func (d *Diff) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
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

// PutCurrentDelegator adds a native current delegator.
//
// Deprecated: use [Adapter.PutCurrentDelegator].
func (d *Diff) PutCurrentDelegator(staker *Staker) error {
	if _, err := d.GetCurrentValidator(staker.SubnetID, staker.NodeID); err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}

	d.currentStakerDiffs.PutDelegator(staker)
	d.recordOp(func(c Chain) error {
		return c.PutCurrentDelegator(staker)
	})
	return nil
}

// DeleteCurrentDelegator removes a native current delegator.
//
// Deprecated: use [Adapter.DeleteCurrentDelegator].
func (d *Diff) DeleteCurrentDelegator(staker *Staker) error {
	if _, err := d.GetCurrentValidator(staker.SubnetID, staker.NodeID); err != nil {
		return fmt.Errorf("getting current validator: %w", err)
	}

	d.currentStakerDiffs.DeleteDelegator(staker)
	d.recordOp(func(c Chain) error {
		return c.DeleteCurrentDelegator(staker)
	})
	return nil
}

// GetCurrentStakerIterator returns native current stakers.
//
// Deprecated: use [Adapter.GetCurrentStakerIterator].
func (d *Diff) GetCurrentStakerIterator() (iterator.Iterator[*Staker], error) {
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

// GetPendingValidator returns a native pending validator.
//
// Deprecated: use [NewAdapter] and the typed validator accessors.
func (d *Diff) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
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

// PutPendingValidator adds a native pending validator.
//
// Deprecated: use [NewAdapter] and the typed validator accessors.
func (d *Diff) PutPendingValidator(staker *Staker) error {
	if err := d.pendingStakerDiffs.PutValidator(staker); err != nil {
		return err
	}
	d.recordOp(func(c Chain) error {
		return c.PutPendingValidator(staker)
	})
	return nil
}

// DeletePendingValidator removes a native pending validator.
//
// Deprecated: use [NewAdapter] and the typed validator accessors.
func (d *Diff) DeletePendingValidator(staker *Staker) {
	d.pendingStakerDiffs.DeleteValidator(staker)
	d.recordOp(func(c Chain) error {
		c.DeletePendingValidator(staker)
		return nil
	})
}

// GetPendingDelegatorIterator returns native pending delegators.
//
// Deprecated: use [Adapter.GetPendingDelegatorIterator].
func (d *Diff) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
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

// PutPendingDelegator adds a native pending delegator.
//
// Deprecated: use [Adapter.PutPendingDelegator].
func (d *Diff) PutPendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.PutDelegator(staker)
	d.recordOp(func(c Chain) error {
		c.PutPendingDelegator(staker)
		return nil
	})
}

// DeletePendingDelegator removes a native pending delegator.
//
// Deprecated: use [Adapter.DeletePendingDelegator].
func (d *Diff) DeletePendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.DeleteDelegator(staker)
	d.recordOp(func(c Chain) error {
		c.DeletePendingDelegator(staker)
		return nil
	})
}

// GetPendingStakerIterator returns native pending stakers.
//
// Deprecated: use [Adapter.GetPendingStakerIterator].
func (d *Diff) GetPendingStakerIterator() (iterator.Iterator[*Staker], error) {
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

func (d *Diff) AddSubnet(subnetID ids.ID) {
	d.recordOp(func(c Chain) error {
		c.AddSubnet(subnetID)
		return nil
	})
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
	d.recordOp(func(c Chain) error {
		c.SetSubnetOwner(subnetID, owner)
		return nil
	})
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

func (d *Diff) SetSubnetToL1Conversion(subnetID ids.ID, conv SubnetToL1Conversion) {
	d.subnetToL1Conversions[subnetID] = conv
	d.recordOp(func(c Chain) error {
		c.SetSubnetToL1Conversion(subnetID, conv)
		return nil
	})
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
	d.recordOp(func(c Chain) error {
		c.AddSubnetTransformation(transformSubnetTxIntf)
		return nil
	})
}

func (d *Diff) AddChain(createChainTx *platform.Tx) {
	d.recordOp(func(c Chain) error {
		c.AddChain(createChainTx)
		return nil
	})
}

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
	d.recordOp(func(c Chain) error {
		c.AddTx(tx, status)
		return nil
	})
}

func (d *Diff) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	d.recordOp(func(c Chain) error {
		c.AddRewardUTXO(txID, utxo)
		return nil
	})
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
	d.recordOp(func(c Chain) error {
		c.AddUTXO(utxo)
		return nil
	})
}

func (d *Diff) DeleteUTXO(utxoID ids.ID) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*avax.UTXO{
			utxoID: nil,
		}
	} else {
		d.modifiedUTXOs[utxoID] = nil
	}
	d.recordOp(func(c Chain) error {
		c.DeleteUTXO(utxoID)
		return nil
	})
}

func (d *Diff) recordOp(op func(Chain) error) {
	d.applyOps = append(d.applyOps, op)
}

func (d *Diff) Apply(baseState Chain) error {
	for _, op := range d.applyOps {
		if err := op(baseState); err != nil {
			return err
		}
	}
	return nil
}
