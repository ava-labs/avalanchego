// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

type immutableValidatorChainState interface {
	// MutableValidatorChainState returns a validator chain state object that
	// can be modifed. The current object will not be modified by updates made
	// by the returned state.
	MutableValidatorChainState() validatorChainState

	GetCurrentValidator(txID ids.ID) (addValidatorTx *Tx, potentialReward uint64, err error)
	GetCurrentValidatorByNodeID(nodeID ids.ShortID) (addValidatorTx *Tx, potentialReward uint64, err error)

	GetCurrentDelegator(txID ids.ID) (addDelegatorTx *Tx, potentialReward uint64, err error)
	GetCurrentDelegatorsByNodeID(nodeID ids.ShortID) ([]*Tx, error)

	GetPendingValidator(txID ids.ID) (*Tx, error)
	GetPendingValidatorByNodeID(nodeID ids.ShortID) (*Tx, error)

	GetPendingDelegator(txID ids.ID) (*Tx, error)
	GetPendingDelegatorsByNodeID(nodeID ids.ShortID) ([]*Tx, error)
}

type validatorChainState interface {
	immutableValidatorChainState

	AddCurrentValidator(addValidatorTx *Tx, potentialReward uint64)
	DeleteCurrentValidator(txID ids.ID)

	AddCurrentDelegator(addDelegatorTx *Tx, potentialReward uint64) error
	DeleteCurrentDelegator(txID ids.ID)

	AddPendingValidator(*Tx)
	DeletePendingValidator(txID ids.ID)

	AddPendingDelegator(*Tx) error
	DeletePendingDelegator(txID ids.ID)
}

type validatorState interface {
	validatorChainState

	GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error)
	SetUptime(upDuration time.Duration, lastUpdated time.Time) error
}

type validatorStateImpl struct {
	currentValidators       map[ids.ShortID]*currentValidator // nodeID -> tx
	currentValidatorsByTxID map[ids.ID]ids.ShortID            // txID -> nodeID
	pendingValidators       map[ids.ShortID]*pendingValidator // nodeID -> tx
	pendingValidatorsByTxID map[ids.ID]ids.ShortID            // txID -> nodeID
}

func (vs *validatorStateImpl) MutableValidatorChainState() validatorChainState {
	newValidatorChainState := validatorStateImpl{
		currentValidators:       make(map[ids.ShortID]*currentValidator, len(vs.currentValidators)),
		currentValidatorsByTxID: make(map[ids.ID]ids.ShortID, len(vs.currentValidatorsByTxID)),
		pendingValidators:       make(map[ids.ShortID]*pendingValidator, len(vs.pendingValidators)),
		pendingValidatorsByTxID: make(map[ids.ID]ids.ShortID, len(vs.pendingValidatorsByTxID)),
	}
	for nodeID, validator := range vs.currentValidators {
		newValidator := &currentValidator{
			addValidatorTx:    validator.addValidatorTx,
			potentialReward:   validator.potentialReward,
			upDuration:        validator.upDuration,
			lastUpdated:       validator.lastUpdated,
			currentDelegators: make(map[ids.ID]*currentDelegator, len(validator.currentDelegators)),
			pendingDelegators: make(map[ids.ID]*Tx, len(validator.pendingDelegators)),
		}
		for txID, delegator := range validator.currentDelegators {
			newValidator.currentDelegators[txID] = &currentDelegator{
				addDelegatorTx:  delegator.addDelegatorTx,
				potentialReward: delegator.potentialReward,
			}
		}
		for txID, delegator := range validator.pendingDelegators {
			newValidator.pendingDelegators[txID] = delegator
		}
		newValidatorChainState.currentValidators[nodeID] = newValidator
	}
	for txID, nodeID := range vs.currentValidatorsByTxID {
		newValidatorChainState.currentValidatorsByTxID[txID] = nodeID
	}
	for nodeID, validator := range vs.pendingValidators {
		newValidator := &pendingValidator{
			addValidatorTx:    validator.addValidatorTx,
			pendingDelegators: make(map[ids.ID]*Tx, len(validator.pendingDelegators)),
		}
		for txID, delegator := range validator.pendingDelegators {
			newValidator.pendingDelegators[txID] = delegator
		}
		newValidatorChainState.pendingValidators[nodeID] = newValidator
	}
	for txID, nodeID := range vs.pendingValidatorsByTxID {
		newValidatorChainState.pendingValidatorsByTxID[txID] = nodeID
	}
	return &newValidatorChainState
}

func (vs *validatorStateImpl) GetCurrentValidator(txID ids.ID) (addValidatorTx *Tx, potentialReward uint64, err error) {
	nodeID, exists := vs.currentValidatorsByTxID[txID]
	if !exists {
		return nil, 0, database.ErrNotFound
	}
	return vs.GetCurrentValidatorByNodeID(nodeID)
}

func (vs *validatorStateImpl) GetCurrentValidatorByNodeID(nodeID ids.ShortID) (addValidatorTx *Tx, potentialReward uint64, err error) {
	validator, exists := vs.currentValidators[nodeID]
	if !exists {
		return nil, 0, database.ErrNotFound
	}
	return validator.addValidatorTx, validator.potentialReward, nil
}

// TODO: Implement
func (vs *validatorStateImpl) AddCurrentValidator(addValidatorTx *Tx, potentialReward uint64) {}

// TODO: Implement
func (vs *validatorStateImpl) DeleteCurrentValidator(txID ids.ID) {}

// TODO: Implement
func (vs *validatorStateImpl) GetCurrentDelegator(txID ids.ID) (addDelegatorTx *Tx, potentialReward uint64, err error) {
	return nil, 0, nil
}

// TODO: Implement
func (vs *validatorStateImpl) GetCurrentDelegatorsByNodeID(nodeID ids.ShortID) ([]*Tx, error) {
	return nil, nil
}

// TODO: Implement
func (vs *validatorStateImpl) AddCurrentDelegator(addDelegatorTx *Tx, potentialReward uint64) error {
	return nil
}

// TODO: Implement
func (vs *validatorStateImpl) DeleteCurrentDelegator(txID ids.ID) {}

// TODO: Implement
func (vs *validatorStateImpl) GetPendingValidator(txID ids.ID) (*Tx, error) { return nil, nil }

// TODO: Implement
func (vs *validatorStateImpl) GetPendingValidatorByNodeID(nodeID ids.ShortID) (*Tx, error) {
	return nil, nil
}

// TODO: Implement
func (vs *validatorStateImpl) AddPendingValidator(*Tx) {}

// TODO: Implement
func (vs *validatorStateImpl) DeletePendingValidator(txID ids.ID) {}

// TODO: Implement
func (vs *validatorStateImpl) GetPendingDelegator(txID ids.ID) (*Tx, error) { return nil, nil }

// TODO: Implement
func (vs *validatorStateImpl) GetPendingDelegatorsByNodeID(nodeID ids.ShortID) ([]*Tx, error) {
	return nil, nil
}

// TODO: Implement
func (vs *validatorStateImpl) AddPendingDelegator(*Tx) error { return nil }

// TODO: Implement
func (vs *validatorStateImpl) DeletePendingDelegator(txID ids.ID) {}

// TODO: Implement
func (vs *validatorStateImpl) GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	return 0, time.Time{}, nil
}

// TODO: Implement
func (vs *validatorStateImpl) SetUptime(upDuration time.Duration, lastUpdated time.Time) error {
	return nil
}
