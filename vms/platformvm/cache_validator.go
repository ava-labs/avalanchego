// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type immutableCurrentStakerChainState interface {
	// MutableCurrentStakerChainState returns a current staker chain state
	// object that can be modifed. The current object will not be modified by
	// updates made by the returned state.
	MutableCurrentStakerChainState() currentStakerChainState

	GetNextStaker() (addStakerTx *Tx, potentialReward uint64, err error)
	GetStaker(txID ids.ID) (addStakerTx *Tx, potentialReward uint64, err error)

	GetValidatorByNodeID(nodeID ids.ShortID) (addValidatorTx *Tx, potentialReward uint64, err error)
	GetDelegatorsByNodeID(nodeID ids.ShortID) ([]*Tx, error)
}

type currentStakerChainState interface {
	immutableCurrentStakerChainState

	AddStaker(addStakerTx *Tx, potentialReward uint64) error
	DeleteStaker(txID ids.ID) error
}

type currentStakerState interface {
	currentStakerChainState

	GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error)
	SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error
}

type immutablePendingStakerChainState interface {
	// MutablePendingStakerChainState returns a pending staker chain state
	// object that can be modifed. The current object will not be modified by
	// updates made by the returned state.
	MutablePendingStakerChainState() pendingStakerChainState

	GetNextStaker() (addStakerTx *Tx, err error)
	GetStaker(txID ids.ID) (addStakerTx *Tx, err error)

	GetValidatorByNodeID(nodeID ids.ShortID) (addValidatorTx *Tx, err error)
	GetDelegatorsByNodeID(nodeID ids.ShortID) ([]*Tx, error)
}

type pendingStakerChainState interface {
	immutablePendingStakerChainState

	AddStaker(addStakerTx *Tx) error
	DeleteStaker(txID ids.ID) error
}

// type currentStakerStateImpl struct {
// 	stakersByTxID   map[ids.ID]ids.ShortID // txID -> nodeID
// 	stakersByNodeID map[ids.ShortID]int    // nodeID -> index in stakers array
// 	stakers         []*currentValidator    // heap of current validators
// }

// func (cs *currentStakerStateImpl) MutableCurrentStakerChainState() currentStakerChainState {
// 	newCurrentStakerChainState := currentStakerStateImpl{
// 		currentValidators:       make(map[ids.ShortID]*currentValidator, len(cs.currentValidators)),
// 		currentValidatorsByTxID: make(map[ids.ID]ids.ShortID, len(vs.currentValidatorsByTxID)),
// 	}
// 	for nodeID, validator := range vs.currentValidators {
// 		newValidator := &currentValidator{
// 			addValidatorTx:    validator.addValidatorTx,
// 			potentialReward:   validator.potentialReward,
// 			upDuration:        validator.upDuration,
// 			lastUpdated:       validator.lastUpdated,
// 			currentDelegators: make(map[ids.ID]*currentDelegator, len(validator.currentDelegators)),
// 			pendingDelegators: make(map[ids.ID]*Tx, len(validator.pendingDelegators)),
// 		}
// 		for txID, delegator := range validator.currentDelegators {
// 			newValidator.currentDelegators[txID] = &currentDelegator{
// 				addDelegatorTx:  delegator.addDelegatorTx,
// 				potentialReward: delegator.potentialReward,
// 			}
// 		}
// 		for txID, delegator := range validator.pendingDelegators {
// 			newValidator.pendingDelegators[txID] = delegator
// 		}
// 		newValidatorChainState.currentValidators[nodeID] = newValidator
// 	}
// 	for txID, nodeID := range vs.currentValidatorsByTxID {
// 		newValidatorChainState.currentValidatorsByTxID[txID] = nodeID
// 	}
// 	return newCurrentStakerChainState
// 	/*
// 		for nodeID, validator := range vs.pendingValidators {
// 			newValidator := &pendingValidator{
// 				addValidatorTx:    validator.addValidatorTx,
// 				pendingDelegators: make(map[ids.ID]*Tx, len(validator.pendingDelegators)),
// 			}
// 			for txID, delegator := range validator.pendingDelegators {
// 				newValidator.pendingDelegators[txID] = delegator
// 			}
// 			newValidatorChainState.pendingValidators[nodeID] = newValidator
// 		}
// 		for txID, nodeID := range vs.pendingValidatorsByTxID {
// 			newValidatorChainState.pendingValidatorsByTxID[txID] = nodeID
// 		}
// 		return &newValidatorChainState*/
// }

// func (vs *validatorStateImpl) GetCurrentValidator(txID ids.ID) (addValidatorTx *Tx, potentialReward uint64, err error) {
// 	nodeID, exists := vs.currentValidatorsByTxID[txID]
// 	if !exists {
// 		return nil, 0, database.ErrNotFound
// 	}
// 	return vs.GetCurrentValidatorByNodeID(nodeID)
// }

// func (vs *validatorStateImpl) GetCurrentValidatorByNodeID(nodeID ids.ShortID) (addValidatorTx *Tx, potentialReward uint64, err error) {
// 	validator, exists := vs.currentValidators[nodeID]
// 	if !exists {
// 		return nil, 0, database.ErrNotFound
// 	}
// 	return validator.addValidatorTx, validator.potentialReward, nil
// }

// // TODO: Implement
// func (vs *validatorStateImpl) AddCurrentValidator(addValidatorTx *Tx, potentialReward uint64) {}

// // TODO: Implement
// func (vs *validatorStateImpl) DeleteCurrentValidator(txID ids.ID) {}

// // TODO: Implement
// func (vs *validatorStateImpl) GetCurrentDelegator(txID ids.ID) (addDelegatorTx *Tx, potentialReward uint64, err error) {
// 	return nil, 0, nil
// }

// // TODO: Implement
// func (vs *validatorStateImpl) GetCurrentDelegatorsByNodeID(nodeID ids.ShortID) ([]*Tx, error) {
// 	return nil, nil
// }

// // TODO: Implement
// func (vs *validatorStateImpl) AddCurrentDelegator(addDelegatorTx *Tx, potentialReward uint64) error {
// 	return nil
// }

// // TODO: Implement
// func (vs *validatorStateImpl) DeleteCurrentDelegator(txID ids.ID) {}

// // TODO: Implement
// func (vs *validatorStateImpl) GetPendingValidator(txID ids.ID) (*Tx, error) { return nil, nil }

// // TODO: Implement
// func (vs *validatorStateImpl) GetPendingValidatorByNodeID(nodeID ids.ShortID) (*Tx, error) {
// 	return nil, nil
// }

// // TODO: Implement
// func (vs *validatorStateImpl) AddPendingValidator(*Tx) {}

// // TODO: Implement
// func (vs *validatorStateImpl) DeletePendingValidator(txID ids.ID) {}

// // TODO: Implement
// func (vs *validatorStateImpl) GetPendingDelegator(txID ids.ID) (*Tx, error) { return nil, nil }

// // TODO: Implement
// func (vs *validatorStateImpl) GetPendingDelegatorsByNodeID(nodeID ids.ShortID) ([]*Tx, error) {
// 	return nil, nil
// }

// // TODO: Implement
// func (vs *validatorStateImpl) AddPendingDelegator(*Tx) error { return nil }

// // TODO: Implement
// func (vs *validatorStateImpl) DeletePendingDelegator(txID ids.ID) {}

// // TODO: Implement
// func (vs *validatorStateImpl) GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error) {
// 	return 0, time.Time{}, nil
// }

// // TODO: Implement
// func (vs *validatorStateImpl) SetUptime(upDuration time.Duration, lastUpdated time.Time) error {
// 	return nil
// }
