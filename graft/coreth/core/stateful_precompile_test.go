// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math/big"

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/precompile"
	"github.com/ethereum/go-ethereum/common"
)

var (
	_ precompile.BlockContext              = &mockBlockContext{}
	_ precompile.PrecompileAccessibleState = &mockAccessibleState{}
)

type mockBlockContext struct {
	blockNumber *big.Int
	timestamp   uint64
}

func (mb *mockBlockContext) Number() *big.Int  { return mb.blockNumber }
func (mb *mockBlockContext) Timestamp() uint64 { return mb.timestamp }

type mockAccessibleState struct {
	state        *state.StateDB
	blockContext *mockBlockContext

	// NativeAssetCall return values
	ret          []byte
	remainingGas uint64
	err          error
}

func (m *mockAccessibleState) GetStateDB() precompile.StateDB { return m.state }

func (m *mockAccessibleState) GetBlockContext() precompile.BlockContext { return m.blockContext }

func (m *mockAccessibleState) NativeAssetCall(common.Address, []byte, uint64, uint64, bool) ([]byte, uint64, error) {
	return m.ret, m.remainingGas, m.err
}
