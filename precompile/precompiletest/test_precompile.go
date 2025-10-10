// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompiletest

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
)

// PrecompileTest is a test case for a precompile
type PrecompileTest struct {
	Name string
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
	Predicates []predicate.Predicate
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
	// Rules is the rules to use for the precompile's block context
	Rules extras.AvalancheRules
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
	state := newTestStateDB(t, map[common.Address][]predicate.Predicate{
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

func (test PrecompileTest) Bench(b *testing.B, module modules.Module) {
	state := newTestStateDB(b, map[common.Address][]predicate.Predicate{
		module.Address: test.Predicates,
	})
	runParams := test.setup(b, module, state)

	if runParams.Input == nil {
		b.Skip("Skipping precompile benchmark due to nil input (used for configuration tests)")
	}

	stateDB := runParams.AccessibleState.GetStateDB()
	snapshot := stateDB.Snapshot()

	ret, remainingGas, err := module.Contract.Run(runParams.AccessibleState, runParams.Caller, runParams.ContractAddress, runParams.Input, runParams.SuppliedGas, runParams.ReadOnly)
	if len(test.ExpectedErr) != 0 {
		require.ErrorContains(b, err, test.ExpectedErr)
	} else {
		require.NoError(b, err)
	}
	require.Equal(b, uint64(0), remainingGas)
	require.Equal(b, test.ExpectedRes, ret)

	if test.AfterHook != nil {
		test.AfterHook(b, state)
	}

	b.ReportAllocs()
	start := time.Now()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Revert to the previous snapshot and take a new snapshot, so we can reset the state after execution
		stateDB.RevertToSnapshot(snapshot)
		snapshot = stateDB.Snapshot()

		// Ignore return values for benchmark
		_, _, _ = module.Contract.Run(runParams.AccessibleState, runParams.Caller, runParams.ContractAddress, runParams.Input, runParams.SuppliedGas, runParams.ReadOnly)
	}
	b.StopTimer()

	elapsed := uint64(time.Since(start))
	if elapsed < 1 {
		elapsed = 1
	}
	gasUsed := runParams.SuppliedGas * uint64(b.N)
	b.ReportMetric(float64(runParams.SuppliedGas), "gas/op")
	// Keep it as uint64, multiply 100 to get two digit float later
	mgasps := (100 * 1000 * gasUsed) / elapsed
	b.ReportMetric(float64(mgasps)/100, "mgas/s")

	// Execute the test one final time to ensure that if our RevertToSnapshot logic breaks such that each run is actually failing or resulting in unexpected behavior
	// the benchmark should catch the error here.
	stateDB.RevertToSnapshot(snapshot)
	ret, remainingGas, err = module.Contract.Run(runParams.AccessibleState, runParams.Caller, runParams.ContractAddress, runParams.Input, runParams.SuppliedGas, runParams.ReadOnly)
	if len(test.ExpectedErr) != 0 {
		require.ErrorContains(b, err, test.ExpectedErr)
	} else {
		require.NoError(b, err)
	}
	require.Equal(b, uint64(0), remainingGas)
	require.Equal(b, test.ExpectedRes, ret)

	if test.AfterHook != nil {
		test.AfterHook(b, state)
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
	blockContext.EXPECT().Timestamp().Return(uint64(time.Now().Unix())).AnyTimes()
	if test.SetupBlockContext != nil {
		test.SetupBlockContext(blockContext)
	} else {
		blockContext.EXPECT().Number().Return(big.NewInt(0)).AnyTimes()
	}
	snowContext := snowtest.Context(t, snowtest.CChainID)

	accessibleState := contract.NewMockAccessibleState(ctrl)
	accessibleState.EXPECT().GetStateDB().Return(state).AnyTimes()
	accessibleState.EXPECT().GetBlockContext().Return(blockContext).AnyTimes()
	accessibleState.EXPECT().GetSnowContext().Return(snowContext).AnyTimes()
	accessibleState.EXPECT().GetRules().Return(test.Rules).AnyTimes()

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

func RunPrecompileTests(t *testing.T, module modules.Module, tests []PrecompileTest) {
	t.Helper()

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t, module)
		})
	}
}

func RunPrecompileBenchmarks(b *testing.B, module modules.Module, tests []PrecompileTest) {
	b.Helper()

	for _, test := range tests {
		b.Run(test.Name, func(b *testing.B) {
			test.Bench(b, module)
		})
	}
}

// testStateDB allows for mocking the predicate storage slots without calling
// Prepare on the statedb.
type testStateDB struct {
	*extstate.StateDB

	predicates map[common.Address][]predicate.Predicate
}

func newTestStateDB(t testing.TB, predicates map[common.Address][]predicate.Predicate) *testStateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	require.NoError(t, err)
	return &testStateDB{
		StateDB:    extstate.New(statedb),
		predicates: predicates,
	}
}

func (s *testStateDB) GetPredicate(address common.Address, index int) (predicate.Predicate, bool) {
	preds := s.predicates[address]
	if index < 0 || index >= len(preds) {
		return nil, false
	}
	return preds[index], true
}
