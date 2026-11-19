// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompiletest

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/libevm/extstate"
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
	// WarpMessages that the precompile should have access to.
	WarpMessages []predicate.Predicate
	// WarpMessageFailures is a list of indices of Warp messages that failed
	// predicate verification.
	WarpMessageFailures []int
	// AfterHook is called after the precompile is called.
	AfterHook func(t testing.TB, state contract.StateDB)
	// ExpectedRes is the expected raw byte result returned by the precompile
	ExpectedRes []byte
	// ExpectedErr is the expected error returned by the precompile
	ExpectedErr error
	// Rules is the rules to use for the precompile
	Rules cparams.Rules
}

func (test PrecompileTest) Run(t *testing.T, precompile contract.StatefulPrecompiledContract) {
	input := test.input(t)

	state := newTestStateDB(t, test.WarpMessages)
	env := test.setup(t, state)

	if input != nil {
		ret, err := precompile.Run(env, input)
		require.ErrorIs(t, err, test.ExpectedErr)
		require.Zero(t, env.Gas(), "remaining gas")
		require.Equal(t, test.ExpectedRes, ret)
	}

	if test.AfterHook != nil {
		test.AfterHook(t, state)
	}
}

func (test PrecompileTest) Bench(b *testing.B, precompile contract.StatefulPrecompiledContract) {
	input := test.input(b)
	if input == nil {
		b.Skip("Skipping precompile benchmark due to nil input (used for configuration tests)")
	}

	state := newTestStateDB(b, test.WarpMessages)
	env := test.setup(b, state)

	ret, err := precompile.Run(env, input)
	require.ErrorIs(b, err, test.ExpectedErr)
	require.Zero(b, env.Gas(), "remaining gas")
	require.Equal(b, test.ExpectedRes, ret)

	if test.AfterHook != nil {
		test.AfterHook(b, state)
	}

	b.ReportAllocs()
	start := time.Now()
	b.ResetTimer()

	snapshot := state.Snapshot()
	for i := 0; i < b.N; i++ {
		// Revert to the previous snapshot and take a new snapshot, so we can reset the state after execution
		state.RevertToSnapshot(snapshot)
		snapshot = state.Snapshot()
		env.gas = test.SuppliedGas

		// Ignore return values for benchmark
		_, _ = precompile.Run(env, input)
	}
	b.StopTimer()

	elapsed := uint64(time.Since(start))
	if elapsed < 1 {
		elapsed = 1
	}
	gasUsed := test.SuppliedGas * uint64(b.N)
	b.ReportMetric(float64(test.SuppliedGas), "gas/op")
	// Keep it as uint64, multiply 100 to get two digit float later
	mgasps := (100 * 1000 * gasUsed) / elapsed
	b.ReportMetric(float64(mgasps)/100, "mgas/s")

	// Execute the test one final time to ensure that if our RevertToSnapshot logic breaks such that each run is actually failing or resulting in unexpected behavior
	// the benchmark should catch the error here.
	state.RevertToSnapshot(snapshot)
	env.gas = test.SuppliedGas
	ret, err = precompile.Run(env, input)
	require.ErrorIs(b, err, test.ExpectedErr)
	require.Zero(b, env.Gas(), "remaining gas")
	require.Equal(b, test.ExpectedRes, ret)

	if test.AfterHook != nil {
		test.AfterHook(b, state)
	}
}

func (test PrecompileTest) input(tb testing.TB) []byte {
	tb.Helper()
	if test.InputFn != nil {
		return test.InputFn(tb)
	}
	return test.Input
}

func (test PrecompileTest) setup(t testing.TB, state *testStateDB) *testEnv {
	t.Helper()

	return &testEnv{
		gas:          test.SuppliedGas,
		readOnly:     test.ReadOnly,
		state:        state,
		caller:       test.Caller,
		snowContext:  snowtest.Context(t, snowtest.CChainID),
		rules:        test.Rules,
		warpFailures: set.NewBits(test.WarpMessageFailures...),
	}
}

func RunPrecompileTests(t *testing.T, precompile contract.StatefulPrecompiledContract, tests []PrecompileTest) {
	t.Helper()

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t, precompile)
		})
	}
}

func RunPrecompileBenchmarks(b *testing.B, precompile contract.StatefulPrecompiledContract, tests []PrecompileTest) {
	b.Helper()

	for _, test := range tests {
		b.Run(test.Name, func(b *testing.B) {
			test.Bench(b, precompile)
		})
	}
}

// testEnv is a [contract.Environment] with a fixed configuration that tracks
// gas usage. All unimplemented methods panic. The gas methods mirror libevm's
// implementation.
type testEnv struct {
	contract.Environment

	gas          uint64
	readOnly     bool
	state        *testStateDB
	caller       common.Address
	snowContext  *snow.Context
	rules        cparams.Rules
	warpFailures set.Bits
}

func (e *testEnv) Gas() uint64 {
	return e.gas
}

func (e *testEnv) UseGas(gas uint64) bool {
	if e.gas < gas {
		return false
	}
	e.gas -= gas
	return true
}

func (e *testEnv) ReadOnly() bool                     { return e.readOnly }
func (e *testEnv) StateDB() vm.StateDB                { return e.state }
func (e *testEnv) ReadOnlyState() libevm.StateReader  { return e.state }
func (*testEnv) BlockNumber() *big.Int                { return new(big.Int) }
func (e *testEnv) Caller() common.Address             { return e.caller }
func (e *testEnv) SnowContext() *snow.Context         { return e.snowContext }
func (e *testEnv) ExtraRules() precompileconfig.Rules { return e.rules }

func (e *testEnv) WarpMessageResults(common.Hash) set.Bits {
	return e.warpFailures
}

// testStateDB allows for mocking the Warp messages without calling Prepare on
// the statedb.
type testStateDB struct {
	*extstate.StateDB

	warpMessages []predicate.Predicate
}

func newTestStateDB(t testing.TB, warpMessages []predicate.Predicate) *testStateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	require.NoError(t, err)
	return &testStateDB{
		StateDB:      extstate.New(statedb),
		warpMessages: warpMessages,
	}
}

func (s *testStateDB) GetWarpMessage(index int) (predicate.Predicate, bool) {
	if index < 0 || index >= len(s.warpMessages) {
		return nil, false
	}
	return s.warpMessages[index], true
}
