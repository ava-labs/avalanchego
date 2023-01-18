package proposer

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	NumProposers = 6
)

type Retriever interface {
	GetCurrentProposers(context.Context) (set.Set[ids.NodeID], error)
	SetChainHeight(uint64)
	SetPChainHeight(uint64)
}

type retriever struct {
	state validators.State
	subnetID ids.ID
	chainSource uint64
	sampler sampler.WeightedWithoutReplacement

	// Updated by the VM
	pChainHeight uint64
	chainHeight uint64
}

func NewRetriever(state validators.State, subnetID ids.ID, chainID ids.ID) Retriever {
	w := wrappers.Packer{Bytes: chainID[:]}
	return &retriever{
		state:       state,
		subnetID:    subnetID,
		chainSource: w.UnpackLong(),
		sampler:     sampler.NewDeterministicWeightedWithoutReplacement(),
		pChainHeight: 0, 
		chainHeight: 0,
	}
}

func (r *retriever) SetPChainHeight(pChainHeight uint64){
	r.pChainHeight = pChainHeight
}

func (r *retriever) SetChainHeight(chainHeight uint64){
	r.chainHeight = chainHeight
}

func (r *retriever) GetCurrentProposers(ctx context.Context) (set.Set[ids.NodeID], error) {
	proposers := set.NewSet[ids.NodeID](NumProposers)

 	// get the validator set by the p-chain height
	 validatorsMap, err := r.state.GetValidatorSet(ctx, r.pChainHeight, r.subnetID)
	 if err != nil {
		 return proposers, err
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
 
	if err := r.sampler.Initialize(validatorWeights); err != nil {
		return nil, err
	}
	
	 numToSample := NumProposers
	 if weight < uint64(numToSample) {
		 numToSample = int(weight)
	 }
 
	 seed := r.chainHeight ^ r.chainSource
	 r.sampler.Seed(int64(seed))
 
	 indices, err := r.sampler.Sample(numToSample)
	 if err != nil {
		 return nil, err
	 }
 
	 for _, index := range indices {
		 proposerNodeID := validators[index].id
		 proposers.Add(proposerNodeID)
	 }

	return proposers, nil
}