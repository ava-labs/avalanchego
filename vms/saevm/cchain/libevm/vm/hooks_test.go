// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm/extstate"
)

func TestMain(m *testing.M) {
	params.RegisterExtras()
	RegisterExtras()
	os.Exit(m.Run())
}

func TestHooksOverrideNewEVMArgs(t *testing.T) {
	difficulty := big.NewInt(1)
	tests := []struct {
		name           string
		config         *params.ChainConfig
		wantStateDB    vm.StateDB
		wantRandom     *common.Hash
		wantDifficulty *big.Int
	}{
		{
			name:           "pre_shanghai_keeps_difficulty",
			config:         params.WithExtra(&params.ChainConfig{ChainID: big.NewInt(1)}, &extras.ChainConfig{}),
			wantStateDB:    &extstate.StateDB{},
			wantDifficulty: difficulty,
		},
		{
			name:           "shanghai_moves_difficulty_to_random",
			config:         cchaintest.ChainConfig(),
			wantStateDB:    &extstate.StateDB{},
			wantRandom:     new(common.BigToHash(difficulty)),
			wantDifficulty: new(big.Int),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdb, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
			require.NoError(t, err, "state.New()")
			blockCtx := vm.BlockContext{
				BlockNumber: new(big.Int),
				Difficulty:  new(big.Int).Set(difficulty),
			}

			evm := vm.NewEVM(blockCtx, vm.TxContext{}, sdb, tt.config, vm.Config{})
			require.IsType(t, tt.wantStateDB, evm.StateDB, "vm.NewEVM().StateDB")
			require.Equal(t, tt.wantRandom, evm.Context.Random, "vm.NewEVM().Context.Random")
			require.Equal(t, tt.wantDifficulty, evm.Context.Difficulty, "vm.NewEVM().Context.Difficulty")

			evm.Reset(vm.TxContext{}, sdb)
			require.IsType(t, tt.wantStateDB, evm.StateDB, "vm.EVM.Reset() StateDB")
		})
	}
}
