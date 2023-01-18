package proposer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	NumProposers = 6
)

type Retriever interface {
	GetCurrentProposers() (set.Set[ids.NodeID], error)
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

func (r *retriever) GetCurrentProposers() (set.Set[ids.NodeID], error) {
	proposers := set.NewSet[ids.NodeID](NumProposers)

	// compute current proposers

	return proposers, nil
}