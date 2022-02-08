// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"github.com/ava-labs/subnet-evm/consensus"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/core/types"
)

type (
	OnFinalizeAndAssembleCallbackType = func(header *types.Header, state *state.StateDB, txs []*types.Transaction) (err error)
	OnExtraStateChangeType            = func(block *types.Block, statedb *state.StateDB) (err error)

	ConsensusCallbacks struct {
		OnFinalizeAndAssemble OnFinalizeAndAssembleCallbackType
		OnExtraStateChange    OnExtraStateChangeType
	}

	DummyEngineCB struct {
		cb *ConsensusCallbacks
		DummyEngine
	}
)

func NewTestConsensusCB(cb *ConsensusCallbacks) *DummyEngineCB {
	return &DummyEngineCB{
		cb: cb,
	}
}

func (self *DummyEngineCB) Finalize(chain consensus.ChainHeaderReader, block *types.Block, parent *types.Header, state *state.StateDB, receipts []*types.Receipt) error {
	if self.cb.OnExtraStateChange != nil {
		err := self.cb.OnExtraStateChange(block, state)
		if err != nil {
			return err
		}
	}

	return self.DummyEngine.Finalize(chain, block, parent, state, receipts)
}

func (self *DummyEngineCB) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	if self.cb.OnFinalizeAndAssemble != nil {
		err := self.cb.OnFinalizeAndAssemble(header, state, txs)
		if err != nil {
			return nil, err
		}
	}
	return self.DummyEngine.FinalizeAndAssemble(chain, header, parent, state, txs, uncles, receipts)
}
