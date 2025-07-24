// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompiletest

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// PrecompileTest is a test case for a precompile
type PrecompileTest struct {
	// Caller is the address of the precompile caller
	Caller common.Address
	// Input the raw input bytes to the precompile
	Input []byte
	// InputFn is a function that returns the raw input bytes to the precompile
	// If specified, Input will be ignored.
	InputFn func(t testing.TB) []byte
	// SuppliedGas is the amount of gas supplied to the precompile
	SuppliedGas uint64
	// ReadOnly is whether the precompile should be called in read only
	// mode. If true, the precompile should not modify the state.
	ReadOnly bool
	// Config is the config to use for the precompile
	// It should be the same precompile config that is used in the
	// precompile's configurator.
	// If nil, Configure will not be called.
	Config precompileconfig.Config
	// Predicates that the precompile should have access to.
	Predicates [][]byte
	// SetupBlockContext sets the expected calls on MockBlockContext for the test execution.
	SetupBlockContext func(*contract.MockBlockContext)
	// AfterHook is called after the precompile is called.
	AfterHook func(t testing.TB, state contract.StateDB)
	// ExpectedRes is the expected raw byte result returned by the precompile
	ExpectedRes []byte
	// ExpectedErr is the expected error returned by the precompile
	ExpectedErr string
	// ChainConfig is the chain config to use for the precompile's block context
	// If nil, the default chain config will be used.
	ChainConfig precompileconfig.ChainConfig
}

type PrecompileRunparams struct {
	AccessibleState contract.AccessibleState
	Caller          common.Address
	ContractAddress common.Address
	Input           []byte
	SuppliedGas     uint64
	ReadOnly        bool
}

func (test PrecompileTest) Run(t *testing.T, module modules.Module) {
	state := newTestStateDB(t, map[common.Address][][]byte{
		module.Address: test.Predicates,
	})
	runParams := test.setup(t, module, state)

	if runParams.Input != nil {
		ret, remainingGas, err := module.Contract.Run(runParams.AccessibleState, runParams.Caller, runParams.ContractAddress, runParams.Input, runParams.SuppliedGas, runParams.ReadOnly)
		if len(test.ExpectedErr) != 0 {
			require.ErrorContains(t, err, test.ExpectedErr)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, uint64(0), remainingGas)
		require.Equal(t, test.ExpectedRes, ret)
	}

	if test.AfterHook != nil {
		test.AfterHook(t, state)
	}
}

func (test PrecompileTest) setup(t testing.TB, module modules.Module, state *testStateDB) PrecompileRunparams {
	t.Helper()
	contractAddress := module.Address

	ctrl := gomock.NewController(t)

	chainConfig := test.ChainConfig
	if chainConfig == nil {
		mockChainConfig := precompileconfig.NewMockChainConfig(ctrl)
		mockChainConfig.EXPECT().IsDurango(gomock.Any()).AnyTimes().Return(true)
		chainConfig = mockChainConfig
	}

	blockContext := contract.NewMockBlockContext(ctrl)
	if test.SetupBlockContext != nil {
		test.SetupBlockContext(blockContext)
	} else {
		blockContext.EXPECT().Number().Return(big.NewInt(0)).AnyTimes()
		blockContext.EXPECT().Timestamp().Return(uint64(time.Now().Unix())).AnyTimes()
	}
	snowContext := snowtest.Context(t, snowtest.CChainID)

	accessibleState := contract.NewMockAccessibleState(ctrl)
	accessibleState.EXPECT().GetStateDB().Return(state).AnyTimes()
	accessibleState.EXPECT().GetBlockContext().Return(blockContext).AnyTimes()
	accessibleState.EXPECT().GetSnowContext().Return(snowContext).AnyTimes()
	accessibleState.EXPECT().GetChainConfig().Return(chainConfig).AnyTimes()

	if test.Config != nil {
		require.NoError(t, module.Configure(chainConfig, test.Config, state, blockContext))
	}

	input := test.Input
	if test.InputFn != nil {
		input = test.InputFn(t)
	}

	return PrecompileRunparams{
		AccessibleState: accessibleState,
		Caller:          test.Caller,
		ContractAddress: contractAddress,
		Input:           input,
		SuppliedGas:     test.SuppliedGas,
		ReadOnly:        test.ReadOnly,
	}
}

func RunPrecompileTests(t *testing.T, module modules.Module, contractTests map[string]PrecompileTest) {
	t.Helper()

	for name, test := range contractTests {
		t.Run(name, func(t *testing.T) {
			test.Run(t, module)
		})
	}
}

// testStateDB allows for mocking the predicate storage slots without calling
// Prepare on the statedb.
type testStateDB struct {
	*extstate.StateDB

	predicateStorageSlots map[common.Address][][]byte
}

func newTestStateDB(t testing.TB, predicateStorageSlots map[common.Address][][]byte) *testStateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	require.NoError(t, err)
	return &testStateDB{
		StateDB:               extstate.New(statedb),
		predicateStorageSlots: predicateStorageSlots,
	}
}

func (s *testStateDB) GetPredicateStorageSlots(address common.Address, index int) ([]byte, bool) {
	predicates, exists := s.predicateStorageSlots[address]
	if !exists || index >= len(predicates) {
		return nil, false
	}
	return predicates[index], true
}
