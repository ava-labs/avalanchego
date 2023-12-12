// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"time"

	"gonum.org/v1/gonum/mathext/prng"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Proposer list constants
const (
	WindowDuration = 5 * time.Second

	MaxVerifyWindows = 6
	MaxVerifyDelay   = MaxVerifyWindows * WindowDuration // 30 seconds

	MaxBuildWindows = 60
	MaxBuildDelay   = MaxBuildWindows * WindowDuration // 5 minutes

	MaxLookAheadWindow = time.Hour
)

var (
	_ Windower = (*windower)(nil)

	ErrNoProposersAvailable         = errors.New("no proposers available")
	ErrNoSlotsScheduledInNextFuture = errors.New("no slots scheduled in next future")
)

type Windower interface {
	// Proposers returns the proposer list for building a block at [chainHeight]
	// when the validator set is defined at [pChainHeight]. The list is returned
	// in order. The minimum delay of a validator is the index they appear times
	// [WindowDuration].
	Proposers(
		ctx context.Context,
		chainHeight,
		pChainHeight uint64,
		maxWindows int,
	) ([]ids.NodeID, error)

	// Delay returns the amount of time that [validatorID] must wait before
	// building a block at [chainHeight] when the validator set is defined at
	// [pChainHeight].
	Delay(
		ctx context.Context,
		chainHeight,
		pChainHeight uint64,
		validatorID ids.NodeID,
		maxWindows int,
	) (time.Duration, error)

	ExpectedProposer(
		ctx context.Context,
		chainHeight,
		pChainHeight uint64,
		parentBlockTime,
		blockTime time.Time,
	) (ids.NodeID, error)

	MinDelayForProposer(
		ctx context.Context,
		chainHeight,
		pChainHeight uint64,
		parentBlockTime time.Time,
		nodeID ids.NodeID,
		startTime time.Time,
	) (time.Time, error)
}

// windower interfaces with P-Chain and it is responsible for calculating the
// delay for the block submission window of a given validator
type windower struct {
	state       validators.State
	subnetID    ids.ID
	chainSource uint64
}

func New(state validators.State, subnetID, chainID ids.ID) Windower {
	w := wrappers.Packer{Bytes: chainID[:]}
	return &windower{
		state:       state,
		subnetID:    subnetID,
		chainSource: w.UnpackLong(),
	}
}

func (w *windower) Proposers(ctx context.Context, chainHeight, pChainHeight uint64, maxWindows int) ([]ids.NodeID, error) {
	validators, err := w.sortedValidators(ctx, pChainHeight)
	if err != nil {
		return nil, err
	}

	var (
		seed             = chainHeight ^ w.chainSource
		source           = prng.NewMT19937()
		validatorWeights = validatorsToWeight(validators)
	)

	sampler := sampler.NewDeterministicWeightedWithoutReplacement(source)
	if err := sampler.Initialize(validatorWeights); err != nil {
		return nil, err
	}

	var totalWeight uint64
	for _, weight := range validatorWeights {
		totalWeight, err = math.Add64(totalWeight, weight)
		if err != nil {
			return nil, err
		}
	}

	numToSample := int(math.Min(uint64(maxWindows), totalWeight))
	source.Seed(seed)
	indices, err := sampler.Sample(numToSample)
	if err != nil {
		return nil, err
	}

	nodeIDs := make([]ids.NodeID, numToSample)
	for i, index := range indices {
		nodeIDs[i] = validators[index].id
	}
	return nodeIDs, nil
}

func (w *windower) Delay(ctx context.Context, chainHeight, pChainHeight uint64, validatorID ids.NodeID, maxWindows int) (time.Duration, error) {
	if validatorID == ids.EmptyNodeID {
		return time.Duration(maxWindows) * WindowDuration, nil
	}

	proposers, err := w.Proposers(ctx, chainHeight, pChainHeight, maxWindows)
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

func (w *windower) ExpectedProposer(
	ctx context.Context,
	chainHeight,
	pChainHeight uint64,
	parentBlockTime,
	blockTime time.Time,
) (ids.NodeID, error) {
	validators, err := w.sortedValidators(ctx, pChainHeight)
	if err != nil {
		return ids.EmptyNodeID, err
	}

	var (
		slot             = uint64(blockTime.Sub(parentBlockTime) / WindowDuration)
		seed             = chainHeight ^ bits.Reverse64(slot) ^ w.chainSource
		source           = prng.NewMT19937_64()
		validatorWeights = validatorsToWeight(validators)
	)

	sampler := sampler.NewDeterministicWeightedWithoutReplacement(source)
	if err := sampler.Initialize(validatorWeights); err != nil {
		return ids.EmptyNodeID, err
	}

	source.Seed(seed)
	indices, err := sampler.Sample(1)
	if err != nil {
		return ids.EmptyNodeID, fmt.Errorf("%w, %w", err, ErrNoProposersAvailable)
	}
	return validators[indices[0]].id, nil
}

func (w *windower) MinDelayForProposer(
	ctx context.Context,
	chainHeight,
	pChainHeight uint64,
	parentBlockTime time.Time,
	nodeID ids.NodeID,
	startTime time.Time,
) (time.Time, error) {
	validators, err := w.sortedValidators(ctx, pChainHeight)
	if err != nil {
		return time.Time{}, err
	}

	var (
		res              = startTime
		source           = prng.NewMT19937_64()
		validatorWeights = validatorsToWeight(validators)
		sampler          = sampler.NewDeterministicWeightedWithoutReplacement(source)
	)
	if err := sampler.Initialize(validatorWeights); err != nil {
		return time.Time{}, err
	}

	for res.Sub(startTime) < MaxLookAheadWindow {
		var (
			slot = uint64(startTime.Sub(parentBlockTime) / WindowDuration)
			seed = chainHeight ^ bits.Reverse64(slot) ^ w.chainSource
		)

		source.Seed(seed)
		indices, err := sampler.Sample(1)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w, %w", err, ErrNoProposersAvailable)
		}

		if validators[indices[0]].id == nodeID {
			return res, nil
		}
		res = res.Add(WindowDuration)
	}
	return time.Time{}, ErrNoSlotsScheduledInNextFuture
}

func (w *windower) sortedValidators(ctx context.Context, pChainHeight uint64) ([]validatorData, error) {
	// get the validator set by the p-chain height
	validatorsMap, err := w.state.GetValidatorSet(ctx, pChainHeight, w.subnetID)
	if err != nil {
		return nil, err
	}

	// convert the map of validators to a slice
	validators := make([]validatorData, 0, len(validatorsMap))
	for k, v := range validatorsMap {
		validators = append(validators, validatorData{
			id:     k,
			weight: v.Weight,
		})
	}

	// canonically sort validators
	// Note: validators are sorted by ID, sorting by weight would not create a
	// canonically sorted list
	utils.Sort(validators)

	return validators, nil
}

func validatorsToWeight(validators []validatorData) []uint64 {
	weights := make([]uint64, len(validators))
	for i, validator := range validators {
		weights[i] = validator.weight
	}
	return weights
}
