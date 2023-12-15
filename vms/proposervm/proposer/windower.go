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

	MaxLookAheadSlots  = 720
	MaxLookAheadWindow = MaxLookAheadSlots * WindowDuration // 1 hour
)

var (
	_ Windower = (*windower)(nil)

	ErrNoProposersAvailable = errors.New("no proposers available")
)

type Windower interface {
	// Proposers returns the proposer list for building a block at [blockHeight]
	// when the validator set is defined at [pChainHeight]. The list is returned
	// in order. The minimum delay of a validator is the index they appear times
	// [WindowDuration].
	Proposers(
		ctx context.Context,
		blockHeight,
		pChainHeight uint64,
		maxWindows int,
	) ([]ids.NodeID, error)

	// Delay returns the amount of time that [validatorID] must wait before
	// building a block at [blockHeight] when the validator set is defined at
	// [pChainHeight].
	Delay(
		ctx context.Context,
		blockHeight,
		pChainHeight uint64,
		validatorID ids.NodeID,
		maxWindows int,
	) (time.Duration, error)

	// In the Post-Durango windowing scheme, every validator active at
	// [pChainHeight] gets specific slots it can propose in (instead of being
	// able to propose from a given time on as it happens Pre-Durango).
	// [ExpectedProposer] calculates which nodeID is scheduled to propose a
	// block of height [blockHeight] at [slot].
	ExpectedProposer(
		ctx context.Context,
		blockHeight,
		pChainHeight,
		slot uint64,
	) (ids.NodeID, error)

	// In the Post-Durango windowing scheme, every validator active at
	// [pChainHeight] gets specific slots it can propose in (instead of being
	// able to propose from a given time on as it happens Pre-Durango).
	// [MinDelayForProposer] specifies how long [nodeID] needs to wait for its
	// slot to start. Delay is specified as starting from slot zero start.
	// (which is parent timestamp). For efficiency reasons, we cap the slot
	// search to [MaxLookAheadWindow] slots.
	MinDelayForProposer(
		ctx context.Context,
		blockHeight,
		pChainHeight uint64,
		nodeID ids.NodeID,
		startSlot uint64,
	) (time.Duration, error)
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

func (w *windower) Proposers(ctx context.Context, blockHeight, pChainHeight uint64, maxWindows int) ([]ids.NodeID, error) {
	validators, err := w.sortedValidators(ctx, pChainHeight)
	if err != nil {
		return nil, err
	}

	// Note: The 32-bit prng is used here for legacy reasons. All other usages
	// of a prng in this file should use the 64-bit version.
	var (
		seed             = preDurangoSeed(w.chainSource, blockHeight)
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

func (w *windower) Delay(ctx context.Context, blockHeight, pChainHeight uint64, validatorID ids.NodeID, maxWindows int) (time.Duration, error) {
	if validatorID == ids.EmptyNodeID {
		return time.Duration(maxWindows) * WindowDuration, nil
	}

	proposers, err := w.Proposers(ctx, blockHeight, pChainHeight, maxWindows)
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
	blockHeight,
	pChainHeight,
	slot uint64,
) (ids.NodeID, error) {
	validators, err := w.sortedValidators(ctx, pChainHeight)
	if err != nil {
		return ids.EmptyNodeID, err
	}

	var (
		seed             = postDurangoSeed(w.chainSource, blockHeight, slot)
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
	blockHeight,
	pChainHeight uint64,
	nodeID ids.NodeID,
	startSlot uint64,
) (time.Duration, error) {
	validators, err := w.sortedValidators(ctx, pChainHeight)
	if err != nil {
		return 0, err
	}

	var (
		maxSlot = startSlot + MaxLookAheadSlots

		source           = prng.NewMT19937_64()
		validatorWeights = validatorsToWeight(validators)
		sampler          = sampler.NewDeterministicWeightedWithoutReplacement(source)
	)

	if err := sampler.Initialize(validatorWeights); err != nil {
		return 0, err
	}

	for slot := startSlot; slot < maxSlot; slot++ {
		seed := postDurangoSeed(w.chainSource, blockHeight, slot)
		source.Seed(seed)
		indices, err := sampler.Sample(1)
		if err != nil {
			return 0, fmt.Errorf("%w, %w", err, ErrNoProposersAvailable)
		}

		if validators[indices[0]].id == nodeID {
			return time.Duration(slot) * WindowDuration, nil
		}
	}

	// no slots scheduled for the max window we inspect. Return max delay
	return time.Duration(maxSlot) * WindowDuration, nil
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

func TimeToSlot(baseTime, targetTime time.Time) uint64 {
	if targetTime.Before(baseTime) {
		return 0
	}
	return uint64(targetTime.Sub(baseTime) / WindowDuration)
}

func validatorsToWeight(validators []validatorData) []uint64 {
	weights := make([]uint64, len(validators))
	for i, validator := range validators {
		weights[i] = validator.weight
	}
	return weights
}

func preDurangoSeed(chainSource, height uint64) uint64 {
	return chainSource ^ height
}

func postDurangoSeed(chainSource, height, slot uint64) uint64 {
	// Slot is reversed to utilize a different state space in the seed than the
	// height. If the slot was not reversed the state space would collide,
	// biasing the seed generation. For example, without reversing the slot
	// postDurangoSeed(0,0,1) would equal postDurangoSeed(0,1,0).
	return chainSource ^ height ^ bits.Reverse64(slot)
}
