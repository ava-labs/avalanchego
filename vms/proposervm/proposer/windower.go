// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	maxWindows     = 5
	WindowDuration = 3 * time.Second
	MaxDelay       = maxWindows * WindowDuration
)

var _ Windower = &windower{}

type Windower interface {
	PChainHeight() (uint64, error)
	Delay(
		chainHeight,
		pChainHeight uint64,
		validatorID ids.ShortID,
	) (time.Duration, error)
}

// windower interfaces with P-Chain and it is responsible for calculating the
// delay for the block submission window of a given validator
type windower struct {
	ctx         *snow.Context
	subnetID    ids.ID
	chainID     ids.ID
	chainSource uint64
	sampler     sampler.WeightedWithoutReplacement
}

func New(ctx *snow.Context, subnetID, chainID ids.ID) Windower {
	w := wrappers.Packer{Bytes: chainID[:]}
	return &windower{
		ctx:         ctx,
		subnetID:    subnetID,
		chainID:     chainID,
		chainSource: w.UnpackLong(),
		sampler:     sampler.NewDeterministicWeightedWithoutReplacement(),
	}
}

func (w *windower) PChainHeight() (uint64, error) {
	if w.subnetID != constants.PrimaryNetworkID || w.chainID != constants.PlatformChainID {
		w.ctx.Lock.Lock()
		defer w.ctx.Lock.Unlock()
	}
	return w.ctx.ValidatorVM.GetCurrentHeight()
}

func (w *windower) Delay(chainHeight, pChainHeight uint64, validatorID ids.ShortID) (time.Duration, error) {
	// get the validator set by the p-chain height
	if w.subnetID != constants.PrimaryNetworkID || w.chainID != constants.PlatformChainID {
		w.ctx.Lock.Lock()
		defer w.ctx.Lock.Unlock()
	}
	validatorsMap, err := w.ctx.ValidatorVM.GetValidatorSet(pChainHeight, w.subnetID)
	if err != nil {
		return 0, err
	}

	// convert the map of validators to a slice
	validators := make(validatorsSlice, 0, len(validatorsMap))
	weight := uint64(0)
	for k, v := range validatorsMap {
		validators = append(validators, validatorData{
			id:     k,
			weight: v,
		})
		newWeight, err := math.Add64(weight, v)
		if err != nil {
			return 0, err
		}
		weight = newWeight
	}

	// canonically sort validators
	// Note: validators are sorted by ID, sorting by weight would not create a
	// canonically sorted list
	sort.Sort(validators)

	// convert the slice of validators to a slice of weights
	validatorWeights := make([]uint64, len(validators))
	for i, v := range validators {
		validatorWeights[i] = v.weight
	}

	if err := w.sampler.Initialize(validatorWeights); err != nil {
		return 0, err
	}

	numToSample := maxWindows
	if weight < uint64(numToSample) {
		numToSample = int(weight)
	}

	seed := chainHeight ^ w.chainSource
	w.sampler.Seed(int64(seed))

	indices, err := w.sampler.Sample(numToSample)
	if err != nil {
		return 0, err
	}

	delay := time.Duration(0)
	for _, index := range indices {
		nodeID := validators[index].id
		if nodeID == validatorID {
			return delay, nil
		}
		delay += WindowDuration
	}
	return delay, nil
}
