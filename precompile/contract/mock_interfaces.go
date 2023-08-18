// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package contract

import (
	"math/big"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
)

// TODO: replace with gomock library

var (
	_ BlockContext    = &mockBlockContext{}
	_ AccessibleState = &mockAccessibleState{}
)

type mockBlockContext struct {
	blockNumber *big.Int
	timestamp   uint64
}

func NewMockBlockContext(blockNumber *big.Int, timestamp uint64) *mockBlockContext {
	return &mockBlockContext{
		blockNumber: blockNumber,
		timestamp:   timestamp,
	}
}

func (mb *mockBlockContext) Number() *big.Int  { return mb.blockNumber }
func (mb *mockBlockContext) Timestamp() uint64 { return mb.timestamp }

type mockAccessibleState struct {
	state        StateDB
	blockContext *mockBlockContext
	snowContext  *snow.Context
	chainConfig  precompileconfig.ChainConfig
}

func NewMockAccessibleState(state StateDB, blockContext *mockBlockContext, snowContext *snow.Context, chainConfig precompileconfig.ChainConfig) *mockAccessibleState {
	return &mockAccessibleState{
		state:        state,
		blockContext: blockContext,
		snowContext:  snowContext,
		chainConfig:  chainConfig,
	}
}

func (m *mockAccessibleState) GetStateDB() StateDB { return m.state }

func (m *mockAccessibleState) GetBlockContext() BlockContext { return m.blockContext }

func (m *mockAccessibleState) GetSnowContext() *snow.Context { return m.snowContext }

func (m *mockAccessibleState) GetChainConfig() precompileconfig.ChainConfig { return m.chainConfig }
