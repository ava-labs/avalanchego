// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/libevm/extstate"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/message"

	. "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
)

// A precompileTest is a call to the Warp precompile and its expected outcome.
type precompileTest struct {
	Name string
	// Caller is the address of the precompile caller
	Caller common.Address
	// Input is the raw input bytes to the precompile
	Input []byte
	// SuppliedGas is the amount of gas supplied to the precompile
	SuppliedGas uint64
	// ReadOnly is whether the precompile should be called in read only
	// mode. If true, the precompile should not modify the state.
	ReadOnly bool
	// WarpMessages that the precompile should have access to.
	WarpMessages []message.Packed
	// WarpMessageFailures is a list of indices of Warp messages that failed
	// verification.
	WarpMessageFailures []int
	// AfterHook is called after the precompile is called.
	AfterHook func(t testing.TB, state contract.StateDB)
	// ExpectedRes is the expected raw byte result returned by the precompile
	ExpectedRes []byte
	// ExpectedErr is the expected error returned by the precompile
	ExpectedErr error
}

func (test precompileTest) run(t *testing.T, precompile contract.StatefulPrecompiledContract) {
	state, call := test.prepare(t, precompile)
	ret, remainingGas, err := call()
	test.check(t, state, ret, remainingGas, err)
}

func (test precompileTest) bench(b *testing.B, precompile contract.StatefulPrecompiledContract) {
	state, call := test.prepare(b, precompile)
	snapshot := state.Snapshot()

	b.ReportAllocs()
	for b.Loop() {
		// Revert to the previous snapshot and take a new snapshot, so we can reset the state after execution
		state.RevertToSnapshot(snapshot)
		snapshot = state.Snapshot()

		_, _, _ = call()
	}
	b.ReportMetric(float64(test.SuppliedGas), "gas/op")

	// Check one final call, so the benchmark fails if reverting to the
	// snapshot stopped resetting the state between iterations.
	state.RevertToSnapshot(snapshot)
	ret, remainingGas, err := call()
	test.check(b, state, ret, remainingGas, err)
}

// prepare returns the state precompile runs against and a function that calls
// precompile with the supplied gas.
func (test precompileTest) prepare(t testing.TB, precompile contract.StatefulPrecompiledContract) (*testStateDB, func() ([]byte, uint64, error)) {
	state := newTestStateDB(t, test.WarpMessages)
	env := &testEnv{
		readOnly:     test.ReadOnly,
		state:        state,
		caller:       test.Caller,
		snowContext:  snowtest.Context(t, snowtest.CChainID),
		warpFailures: set.NewBits(test.WarpMessageFailures...),
	}
	return state, func() ([]byte, uint64, error) {
		env.gas = test.SuppliedGas
		ret, err := precompile.Run(env, test.Input)
		return ret, env.gas, err
	}
}

func (test precompileTest) check(t testing.TB, state *testStateDB, ret []byte, remainingGas uint64, err error) {
	t.Helper()
	require.ErrorIs(t, err, test.ExpectedErr, "precompile.Run()")
	require.Zero(t, remainingGas, "precompile.Run() remaining gas")
	require.Equal(t, test.ExpectedRes, ret, "precompile.Run()")

	if test.AfterHook != nil {
		test.AfterHook(t, state)
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

func (e *testEnv) ReadOnly() bool                    { return e.readOnly }
func (e *testEnv) StateDB() vm.StateDB               { return e.state }
func (e *testEnv) ReadOnlyState() libevm.StateReader { return e.state }
func (*testEnv) BlockNumber() *big.Int               { return new(big.Int) }
func (e *testEnv) Caller() common.Address            { return e.caller }
func (e *testEnv) SnowContext() *snow.Context        { return e.snowContext }

func (e *testEnv) WarpMessageResults(common.Hash) set.Bits {
	return e.warpFailures
}

// testStateDB allows for mocking the Warp messages without calling Prepare on
// the statedb.
type testStateDB struct {
	*extstate.StateDB

	warpMessages []message.Packed
}

func newTestStateDB(t testing.TB, warpMessages []message.Packed) *testStateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	require.NoError(t, err)
	return &testStateDB{
		StateDB:      extstate.New(statedb),
		warpMessages: warpMessages,
	}
}

func (s *testStateDB) GetWarpMessage(index int) (message.Packed, bool) {
	if index < 0 || index >= len(s.warpMessages) {
		return nil, false
	}
	return s.warpMessages[index], true
}

// A messageTest is a Warp message and its expected gas and verification
// outcome.
type messageTest struct {
	Name string

	VerifyContext *message.VerifyContext

	Packed      message.Packed
	GasConfig   GasConfig
	Gas         uint64
	GasErr      error
	ExpectedErr error
}

func (test messageTest) run(t testing.TB) {
	t.Helper()
	require := require.New(t)
	gas, gasErr := test.GasConfig.MessageGas(test.Packed)
	require.ErrorIs(gasErr, test.GasErr, "MessageGas()")
	if test.GasErr != nil {
		return
	}

	require.Equal(test.Gas, gas, "MessageGas()")

	err := VerifyMessage(test.VerifyContext, test.Packed)
	require.ErrorIs(err, test.ExpectedErr, "VerifyMessage()")
}

func (test messageTest) bench(b *testing.B) {
	test.run(b)

	b.ReportAllocs()
	for b.Loop() {
		_, _ = test.GasConfig.MessageGas(test.Packed)
		_ = VerifyMessage(test.VerifyContext, test.Packed)
	}
	b.ReportMetric(float64(test.Gas), "gas/op")
}

func runMessageTests(t *testing.T, tests []messageTest) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.run(t)
		})
	}
}
