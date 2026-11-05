// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm/extstate"

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
	WarpMessages []warpmsg.Packed
	// SetupBlockContext sets the expected calls on MockBlockContext for the test execution.
	SetupBlockContext func(*contract.MockBlockContext)
	// AfterHook is called after the precompile is called.
	AfterHook func(t testing.TB, state contract.StateDB)
	// ExpectedRes is the expected raw byte result returned by the precompile
	ExpectedRes []byte
	// ExpectedErr is the expected error returned by the precompile
	ExpectedErr error
	// Rules is the rules to use for the precompile's block context
	Rules cparams.Rules
}

func (test precompileTest) run(t *testing.T) {
	state, call := test.prepare(t)
	ret, remainingGas, err := call()
	test.check(t, state, ret, remainingGas, err)
}

func (test precompileTest) bench(b *testing.B) {
	state, call := test.prepare(b)
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

// prepare returns the state the precompile runs against and a function that
// calls the precompile.
func (test precompileTest) prepare(t testing.TB) (*testStateDB, func() ([]byte, uint64, error)) {
	state := newTestStateDB(t, test.WarpMessages)
	accessibleState := test.accessibleState(t, state)
	return state, func() ([]byte, uint64, error) {
		return WarpPrecompile.Run(accessibleState, test.Caller, warpmsg.ContractAddress, test.Input, test.SuppliedGas, test.ReadOnly)
	}
}

func (test precompileTest) check(t testing.TB, state *testStateDB, ret []byte, remainingGas uint64, err error) {
	t.Helper()
	require.ErrorIs(t, err, test.ExpectedErr, "WarpPrecompile.Run()")
	require.Zero(t, remainingGas, "WarpPrecompile.Run() remaining gas")
	require.Equal(t, test.ExpectedRes, ret, "WarpPrecompile.Run()")

	if test.AfterHook != nil {
		test.AfterHook(t, state)
	}
}

func (test precompileTest) accessibleState(t testing.TB, state *testStateDB) contract.AccessibleState {
	ctrl := gomock.NewController(t)

	blockContext := contract.NewMockBlockContext(ctrl)
	blockContext.EXPECT().Timestamp().Return(uint64(time.Now().Unix())).AnyTimes()
	if test.SetupBlockContext != nil {
		test.SetupBlockContext(blockContext)
	} else {
		blockContext.EXPECT().Number().Return(big.NewInt(0)).AnyTimes()
	}

	accessibleState := contract.NewMockAccessibleState(ctrl)
	accessibleState.EXPECT().GetStateDB().Return(state).AnyTimes()
	accessibleState.EXPECT().GetBlockContext().Return(blockContext).AnyTimes()
	accessibleState.EXPECT().GetSnowContext().Return(snowtest.Context(t, snowtest.CChainID)).AnyTimes()
	accessibleState.EXPECT().GetRules().Return(test.Rules).AnyTimes()
	return accessibleState
}

// testStateDB allows for mocking the Warp messages without calling Prepare on
// the statedb.
type testStateDB struct {
	*extstate.StateDB

	warpMessages []warpmsg.Packed
}

func newTestStateDB(t testing.TB, warpMessages []warpmsg.Packed) *testStateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	require.NoError(t, err)
	return &testStateDB{
		StateDB:      extstate.New(statedb),
		warpMessages: warpMessages,
	}
}

func (s *testStateDB) GetWarpMessage(index int) (warpmsg.Packed, bool) {
	if index < 0 || index >= len(s.warpMessages) {
		return nil, false
	}
	return s.warpMessages[index], true
}

// A messageTest is a Warp message and its expected gas and verification
// outcome.
type messageTest struct {
	Name string

	VerifyContext *warpmsg.VerifyContext

	Packed      warpmsg.Packed
	Rules       precompileconfig.Rules
	Gas         uint64
	GasErr      error
	ExpectedErr error
}

func (test messageTest) run(t testing.TB) {
	t.Helper()
	require := require.New(t)
	gas, gasErr := MessageGas(test.Packed, test.Rules)
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
		_, _ = MessageGas(test.Packed, test.Rules)
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
