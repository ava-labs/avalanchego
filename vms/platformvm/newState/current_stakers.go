// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var errNotEnoughStakersToRemove = errors.New("not enough stakers to remove")

type CurrentStakers interface {
	UpdateStakers(
		addValidators []*Staker,
		addDelegators []*Staker,
		addSubnetValidators []*txs.Tx,
		numTxsToRemove int,
	) (CurrentStakers, error)
	DeleteNextStaker() (CurrentStakers, error)

	Apply(State)

	// Return the current validator set of [subnetID].
	ValidatorSet(subnetID ids.ID) (validators.Set, error)
}

// currentStakers is a copy on write implementation for versioning the validator
// set. None of the slices, maps, or pointers should be modified after
// initialization.
type currentStakers struct {
	ValidatorsByNodeID map[ids.NodeID]*PrimaryNetworkValidator
	StakersByTxID      map[ids.ID]*Staker

	// Stakers is the list of current stakers in order of their removal from the
	// staker set
	Stakers []*Staker

	// NextRewardedStaker contains the first staker in [Stakers] that has a
	// non-zero [PotentialReward].
	NextRewardedStaker *Staker

	AddedStakers   []*Staker
	DeletedStakers []*Staker
}

// UpdateStakers
func (c *currentStakers) UpdateStakers(
	addedStakers []*Staker,
	numStakersToRemove int,
) *currentStakers {
	newCS := &currentStakers{
		ValidatorsByNodeID: make(map[ids.NodeID]*PrimaryNetworkValidator, len(c.ValidatorsByNodeID)),
		StakersByTxID:      make(map[ids.ID]*Staker, len(c.StakersByTxID)+len(addedStakers)-numStakersToRemove),
		Stakers:            c.Stakers[numStakersToRemove:], // sorted in order of removal

		AddedStakers:   addedStakers,
		DeletedStakers: c.Stakers[:numStakersToRemove],
	}

	for nodeID, vdr := range c.ValidatorsByNodeID {
		newCS.ValidatorsByNodeID[nodeID] = vdr
	}

	for txID, staker := range c.StakersByTxID {
		newCS.StakersByTxID[txID] = staker
	}

	if numAdded := len(addedStakers); numAdded != 0 {
		numCurrent := len(newCS.Stakers)
		newSize := numCurrent + numAdded
		newStakers := make([]*Staker, newSize)
		copy(newStakers, newCS.Stakers)
		copy(newStakers[numCurrent:], addedStakers)

		SortCurrentStakers(newStakers)
		newCS.Stakers = newStakers

		for _, staker := range addedStakers {
			newCS.StakersByTxID[staker.TxID] = staker

			vdr, vdrExists := newCS.ValidatorsByNodeID[staker.NodeID]
			if !vdrExists {
				newCS.ValidatorsByNodeID[staker.NodeID] = NewPrimaryNetworkValidator(staker)
			} else {
				newCS.ValidatorsByNodeID[staker.NodeID] = vdr.AddStaker(staker, SortCurrentStakers)
			}
		}
	}

	for i := 0; i < numStakersToRemove; i++ {
		removed := c.Stakers[i]
		delete(newCS.StakersByTxID, removed.TxID)

		vdr := newCS.ValidatorsByNodeID[removed.NodeID]
		if newVDR := vdr.RemoveStaker(removed); newVDR == nil {
			delete(newCS.ValidatorsByNodeID, removed.NodeID)
		} else {
			newCS.ValidatorsByNodeID[removed.NodeID] = newVDR
		}
	}

	newCS.setNextRewardedStaker()
	return newCS
}

func (c *currentStakers) DeleteNextStaker() (CurrentStakers, error) {
	removedTx, _, err := c.GetNextStaker()
	if err != nil {
		return nil, err
	}
	removedTxID := removedTx.ID()

	newCS := &currentStakers{
		validatorsByNodeID: make(map[ids.NodeID]*currentValidator, len(c.validatorsByNodeID)),
		validatorsByTxID:   make(map[ids.ID]*ValidatorReward, len(c.validatorsByTxID)-1),
		validators:         c.validators[1:], // sorted in order of removal

		deletedStakers: []*txs.Tx{removedTx},
	}

	switch tx := removedTx.Unsigned.(type) {
	case *txs.AddValidatorTx:
		for nodeID, vdr := range c.validatorsByNodeID {
			if nodeID != tx.Validator.NodeID {
				newCS.validatorsByNodeID[nodeID] = vdr
			}
		}
	case *txs.AddDelegatorTx:
		for nodeID, vdr := range c.validatorsByNodeID {
			if nodeID != tx.Validator.NodeID {
				newCS.validatorsByNodeID[nodeID] = vdr
			} else {
				newCS.validatorsByNodeID[nodeID] = &currentValidator{
					validatorModifications: validatorModifications{
						delegators: vdr.delegators[1:], // sorted in order of removal
						subnets:    vdr.subnets,
					},
					addValidator: ValidatorAndID{
						Tx:   vdr.addValidator.Tx,
						TxID: vdr.addValidator.TxID,
					},
					delegatorWeight: vdr.delegatorWeight - tx.Validator.Wght,
					potentialReward: vdr.potentialReward,
				}
			}
		}
	default:
		return nil, fmt.Errorf("expected tx type *txs.AddValidatorTx or *txs.AddDelegatorTx but got %T", removedTx.Unsigned)
	}

	for txID, vdr := range c.validatorsByTxID {
		if txID != removedTxID {
			newCS.validatorsByTxID[txID] = vdr
		}
	}

	newCS.setNextStaker()
	return newCS, nil
}

func (c *currentStakers) Apply(baseState State) {
	for _, added := range c.addedStakers {
		baseState.AddCurrentStaker(added.AddStakerTx, added.PotentialReward)
	}
	for _, deleted := range c.deletedStakers {
		baseState.DeleteCurrentStaker(deleted)
	}
	baseState.SetCurrentStakers(c)

	// Validator changes should only be applied once.
	c.addedStakers = nil
	c.deletedStakers = nil
}

func (c *currentStakers) ValidatorSet(subnetID ids.ID) (validators.Set, error) {
	if subnetID == constants.PrimaryNetworkID {
		return c.primaryValidatorSet()
	}
	return c.subnetValidatorSet(subnetID)
}

func (c *currentStakers) primaryValidatorSet() (validators.Set, error) {
	vdrs := validators.NewSet()
	for nodeID, vdr := range c.ValidatorsByNodeID {
		if err := vdrs.AddWeight(nodeID, vdr.Validator.Validator.Weight); err != nil {
			return nil, err
		}
		if err := vdrs.AddWeight(nodeID, vdr.Validator.TotalDelegatorWeight); err != nil {
			return nil, err
		}

		vdrWeight := vdr.Validator.TotalDelegatorWeight
		vdrWeight, err = math.Add64(vdrWeight, vdr.delegatorWeight)
		if err != nil {
			return nil, err
		}
		if err := vdrs.AddWeight(nodeID, vdrWeight); err != nil {
			return nil, err
		}
	}

	return vdrs, nil
}

func (c *currentStakers) subnetValidatorSet(subnetID ids.ID) (validators.Set, error) {
	vdrs := validators.NewSet()

	for nodeID, vdr := range c.validatorsByNodeID {
		subnetVDR, exists := vdr.subnets[subnetID]
		if !exists {
			continue
		}
		if err := vdrs.AddWeight(nodeID, subnetVDR.Tx.Validator.Wght); err != nil {
			return nil, err
		}
	}

	return vdrs, nil
}

// setNextRewardedStaker to the first staker in Stakers with a non-zero
// PotentialReward.
func (c *currentStakers) setNextRewardedStaker() {
	for _, staker := range c.Stakers {
		if staker.PotentialReward > 0 {
			c.NextRewardedStaker = staker
			return
		}
	}
}
