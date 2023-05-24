// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Proposer list constants
const (
	MaxWindows     = 6
	WindowDuration = 5 * time.Second
	MaxDelay       = MaxWindows * WindowDuration
)

var _ Windower = (*windower)(nil)

type Windower interface {
	// Proposers returns the proposer list for building a block at [chainHeight]
	// when the validator set is defined at [pChainHeight]. The list is returned
	// in order. The minimum delay of a validator is the index they appear times
	// [WindowDuration].
	Proposers(
		ctx context.Context,
		chainHeight,
		pChainHeight uint64,
	) ([]ids.NodeID, error)
	// Delay returns the amount of time that [validatorID] must wait before
	// building a block at [chainHeight] when the validator set is defined at
	// [pChainHeight].
	Delay(
		ctx context.Context,
		chainHeight,
		pChainHeight uint64,
		validatorID ids.NodeID,
	) (time.Duration, error)
}

// windower interfaces with P-Chain and it is responsible for calculating the
// delay for the block submission window of a given validator
type windower struct {
	state       validators.State
	subnetID    ids.ID
	chainSource uint64
	sampler     sampler.WeightedWithoutReplacement
}

func New(state validators.State, subnetID, chainID ids.ID) Windower {
	w := wrappers.Packer{Bytes: chainID[:]}
	return &windower{
		state:       state,
		subnetID:    subnetID,
		chainSource: w.UnpackLong(),
		sampler:     sampler.NewDeterministicWeightedWithoutReplacement(),
	}
}

func (w *windower) Proposers(ctx context.Context, chainHeight, pChainHeight uint64) ([]ids.NodeID, error) {
	// get the validator set by the p-chain height
	validatorsMap, err := w.state.GetValidatorSet(ctx, pChainHeight, w.subnetID)
	if err != nil {
		return nil, err
	}

	// convert the map of validators to a slice
	validators := make([]validatorData, 0, len(validatorsMap))
	weight := uint64(0)
	for k, v := range validatorsMap {
		validators = append(validators, validatorData{
			id:     k,
			weight: v.Weight,
		})
		newWeight, err := math.Add64(weight, v.Weight)
		if err != nil {
			return nil, err
		}
		weight = newWeight
	}

	// canonically sort validators
	// Note: validators are sorted by ID, sorting by weight would not create a
	// canonically sorted list
	utils.Sort(validators)

	// convert the slice of validators to a slice of weights
	validatorWeights := make([]uint64, len(validators))
	for i, v := range validators {
		validatorWeights[i] = v.weight
	}

	if err := w.sampler.Initialize(validatorWeights); err != nil {
		return nil, err
	}

	numToSample := MaxWindows
	if weight < uint64(numToSample) {
		numToSample = int(weight)
	}

	seed := chainHeight ^ w.chainSource
	w.sampler.Seed(int64(seed))

	indices, err := w.sampler.Sample(numToSample)
	if err != nil {
		return nil, err
	}

	nodeIDs := make([]ids.NodeID, numToSample)
	for i, index := range indices {
		nodeIDs[i] = validators[index].id
	}
	return nodeIDs, nil
}

func (w *windower) Delay(ctx context.Context, chainHeight, pChainHeight uint64, validatorID ids.NodeID) (time.Duration, error) {
	if validatorID == ids.EmptyNodeID {
		return MaxDelay, nil
	}

	proposers, err := w.Proposers(ctx, chainHeight, pChainHeight)
	if err != nil {
		return 0, err
	}

	delay := time.Duration(0)
	for _, nodeID := range proposers {
		if nodeID == validatorID {
			return delay, nil
		}
		delay += WindowDuration
	}
	return delay, nil
}
