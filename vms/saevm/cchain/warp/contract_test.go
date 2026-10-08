// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/precompile"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"

	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// stubState is the state the stub environment exposes. Methods not listed
// here panic via the nil embedded interface, which is the intent: the
// contract must not touch anything else.
type stubState struct {
	vm.StateDB // nil; only the methods below are implemented
	txHash     common.Hash
	predicates map[common.Address][]predicate.Predicate
	logs       []*types.Log
}

var (
	_ libevm.StateReader         = (*stubState)(nil)
	_ precompile.PredicateReader = (*stubState)(nil)
)

func (s *stubState) TxHash() common.Hash { return s.txHash }

func (s *stubState) GetPredicate(addr common.Address, i int) (predicate.Predicate, bool) {
	ps := s.predicates[addr]
	if i < 0 || i >= len(ps) {
		return nil, false
	}
	return ps[i], true
}

func (s *stubState) AddLog(l *types.Log) { s.logs = append(s.logs, l) }

// stubEnv implements the parts of [vm.PrecompileEnvironment] the contract
// uses. Anything else panics via the nil embedded interface.
type stubEnv struct {
	vm.PrecompileEnvironment // nil
	gas                      uint64
	callType                 vm.CallType
	readOnly                 bool
	caller                   common.Address
	header                   types.Header
	state                    *stubState
	invalidated              error // last value passed to InvalidateExecution
}

func (e *stubEnv) Gas() uint64 { return e.gas }

func (e *stubEnv) UseGas(g uint64) bool {
	if e.gas < g {
		return false
	}
	e.gas -= g
	return true
}

func (e *stubEnv) InvalidateExecution(err error) { e.invalidated = err }

func (e *stubEnv) IncomingCallType() vm.CallType { return e.callType }
func (e *stubEnv) ReadOnly() bool                { return e.readOnly }

func (e *stubEnv) Addresses() *libevm.AddressContext {
	return &libevm.AddressContext{
		EVMSemantic: libevm.CallerAndSelf{Caller: e.caller, Self: ContractAddress},
	}
}

func (e *stubEnv) BlockHeader() (types.Header, error) { return e.header, nil }
func (e *stubEnv) BlockNumber() *big.Int              { return new(big.Int).Set(e.header.Number) }
func (e *stubEnv) ReadOnlyState() libevm.StateReader  { return e.state }

func (e *stubEnv) StateDB() vm.StateDB {
	if e.readOnly {
		return nil
	}
	return e.state
}

type testFixture struct {
	ctx    *snow.Context
	vdrs   *warptest.Validators
	c      *contract
	txHash common.Hash
}

func newFixture(t *testing.T) *testFixture {
	t.Helper()
	ctx := snowtest.Context(t, snowtest.CChainID)
	vdrs := warptest.NewValidators(t, warptest.WithMinimum(2))
	warptest.SetValidators(t, ctx, vdrs)
	return &testFixture{
		ctx:    ctx,
		vdrs:   vdrs,
		c:      &contract{ctx: ctx},
		txHash: common.Hash{'t', 'x'},
	}
}

// env returns a stub environment carrying preds as this transaction's warp
// predicates and failed as the indices recorded as failing in the header.
func (f *testFixture) env(t *testing.T, gas uint64, preds []predicate.Predicate, failed ...int) *stubEnv {
	t.Helper()
	results := predicate.BlockResults{}
	if len(failed) > 0 {
		bits := set.NewBits(failed...)
		results.Set(f.txHash, predicate.PrecompileResults{ContractAddress: bits})
	}
	extra, err := results.Bytes()
	require.NoError(t, err, "BlockResults.Bytes()")
	return &stubEnv{
		gas:      gas,
		callType: vm.Call,
		caller:   common.Address{'c', 'a', 'l', 'l', 'e', 'r'},
		header:   types.Header{Number: big.NewInt(42), Extra: extra},
		state: &stubState{
			txHash:     f.txHash,
			predicates: map[common.Address][]predicate.Predicate{ContractAddress: preds},
		},
	}
}

// must returns a helper that unwraps a ([]byte, error) pair, failing t if err
// is non-nil. It is curried so that a multi-valued call, such as a Pack*
// helper, can be passed through without an intermediate variable: Go does not
// allow splatting a multi-valued call into a function alongside other
// arguments (only as the sole argument), so must(t)(f()) is used instead of
// must(t, f()).
func must(t *testing.T) func([]byte, error) []byte {
	t.Helper()
	return func(b []byte, err error) []byte {
		require.NoError(t, err)
		return b
	}
}

func TestRunCallTypeAndSelector(t *testing.T) {
	f := newFixture(t)
	const gas = 1_000_000
	getID := must(t)(PackGetBlockchainID())

	tests := []struct {
		name       string
		callType   vm.CallType
		input      []byte
		wantErr    error
		wantGasAll bool // all gas consumed by the contract itself
	}{
		{name: "delegatecall_reverts", callType: vm.DelegateCall, input: getID, wantErr: vm.ErrExecutionReverted, wantGasAll: true},
		{name: "callcode_reverts", callType: vm.CallCode, input: getID, wantErr: vm.ErrExecutionReverted, wantGasAll: true},
		{name: "staticcall_allowed", callType: vm.StaticCall, input: getID},
		{name: "empty_input", callType: vm.Call, input: nil, wantErr: errMissingSelector},
		{name: "short_input", callType: vm.Call, input: []byte{1, 2, 3}, wantErr: errMissingSelector},
		{name: "unknown_selector", callType: vm.Call, input: []byte{0xde, 0xad, 0xbe, 0xef}, wantErr: errUnknownSelector},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := f.env(t, gas, nil)
			env.callType = tt.callType
			env.readOnly = tt.callType == vm.StaticCall
			_, err := f.c.run(env, tt.input)
			require.ErrorIs(t, err, tt.wantErr, "run()")
			if tt.wantGasAll {
				require.Zero(t, env.gas, "remaining gas after revert")
			}
		})
	}
}

func TestGetBlockchainID(t *testing.T) {
	f := newFixture(t)
	env := f.env(t, Gas.GetBlockchainID, nil)
	got, err := f.c.run(env, must(t)(PackGetBlockchainID()))
	require.NoError(t, err, "run(getBlockchainID)")
	require.Equal(t, must(t)(PackGetBlockchainIDOutput(common.Hash(f.ctx.ChainID))), got, "run(getBlockchainID)")
	require.Zero(t, env.gas, "remaining gas")

	env = f.env(t, Gas.GetBlockchainID-1, nil)
	_, err = f.c.run(env, must(t)(PackGetBlockchainID()))
	require.ErrorIs(t, err, vm.ErrOutOfGas, "run(getBlockchainID) underfunded")
}

func TestSendWarpMessage(t *testing.T) {
	f := newFixture(t)
	payloadData := []byte{1, 2, 3}
	input := must(t)(PackSendWarpMessage(payloadData))
	// Priced on the input after the selector, matching coreth: the
	// dispatcher strips the 4-byte selector before the input reaches the
	// gas-charging step.
	cost := Gas.SendWarpMessageBase + Gas.PerWarpMessageByte*(uint64(len(input))-selectorLen)

	t.Run("success", func(t *testing.T) {
		env := f.env(t, cost, nil)
		got, err := f.c.run(env, input)
		require.NoError(t, err, "run(sendWarpMessage)")
		require.Zero(t, env.gas, "remaining gas")

		addressed, err := payload.NewAddressedCall(env.caller.Bytes(), payloadData)
		require.NoError(t, err, "payload.NewAddressedCall()")
		unsigned, err := avalanchewarp.NewUnsignedMessage(f.ctx.NetworkID, f.ctx.ChainID, addressed.Bytes())
		require.NoError(t, err, "avalanchewarp.NewUnsignedMessage()")
		require.Equal(t, must(t)(PackSendWarpMessageOutput(common.Hash(unsigned.ID()))), got, "run(sendWarpMessage) output")

		topics, data, err := PackSendWarpMessageEvent(env.caller, common.Hash(unsigned.ID()), unsigned.Bytes())
		require.NoError(t, err, "PackSendWarpMessageEvent()")
		require.Equal(t, []*types.Log{{
			Address:     ContractAddress,
			Topics:      topics,
			Data:        data,
			BlockNumber: 42,
		}}, env.state.logs, "emitted logs")
	})

	t.Run("underfunded_by_one", func(t *testing.T) {
		env := f.env(t, cost-1, nil)
		_, err := f.c.run(env, input)
		require.ErrorIs(t, err, vm.ErrOutOfGas, "run(sendWarpMessage)")
		require.Empty(t, env.state.logs, "emitted logs")
	})

	t.Run("read_only", func(t *testing.T) {
		env := f.env(t, cost, nil)
		env.readOnly = true
		env.callType = vm.StaticCall
		_, err := f.c.run(env, input)
		require.ErrorIs(t, err, vm.ErrWriteProtection, "run(sendWarpMessage)")
		require.Zero(t, env.gas, "gas is charged before the write check, as in coreth")
	})

	t.Run("bad_input", func(t *testing.T) {
		env := f.env(t, 1_000_000, nil)
		// Only the selector, no packed []byte argument: ABI decoding fails.
		_, err := f.c.run(env, input[:selectorLen])
		require.ErrorIs(t, err, errInvalidSendInput, "run(sendWarpMessage)")
	})
}

func TestGetVerifiedWarpMessage(t *testing.T) {
	f := newFixture(t)
	sourceAddress := common.Address{9}
	addressed, err := payload.NewAddressedCall(sourceAddress.Bytes(), []byte("payload"))
	require.NoError(t, err, "payload.NewAddressedCall()")
	unsigned, err := avalanchewarp.NewUnsignedMessage(f.ctx.NetworkID, snowtest.XChainID, addressed.Bytes())
	require.NoError(t, err, "avalanchewarp.NewUnsignedMessage()")
	pred := predicate.New(f.vdrs.Sign(t, unsigned).Bytes())

	hashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err, "payload.NewHash()")
	hashUnsigned, err := avalanchewarp.NewUnsignedMessage(f.ctx.NetworkID, snowtest.XChainID, hashPayload.Bytes())
	require.NoError(t, err, "avalanchewarp.NewUnsignedMessage(hash)")
	hashPred := predicate.New(f.vdrs.Sign(t, hashUnsigned).Bytes())

	valid := must(t)(PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
		Message: WarpMessage{
			SourceChainID:       common.Hash(snowtest.XChainID),
			OriginSenderAddress: sourceAddress,
			Payload:             []byte("payload"),
		},
		Valid: true,
	}))
	invalid := must(t)(PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false}))

	readCost := func(p predicate.Predicate) uint64 {
		return Gas.GetVerifiedWarpMessageBase + Gas.PerWarpMessageChunk*uint64(len(p))
	}

	tests := []struct {
		name    string
		preds   []predicate.Predicate
		failed  []int
		index   uint32
		gas     uint64
		want    []byte
		wantGas uint64 // remaining
		wantErr error
	}{
		{name: "valid", preds: []predicate.Predicate{pred}, index: 0, gas: readCost(pred), want: valid},
		{name: "failed_predicate", preds: []predicate.Predicate{pred}, failed: []int{0}, index: 0, gas: 1_000_000, want: invalid, wantGas: 1_000_000 - Gas.GetVerifiedWarpMessageBase},
		{name: "index_out_of_range", preds: []predicate.Predicate{pred}, index: 1, gas: 1_000_000, want: invalid, wantGas: 1_000_000 - Gas.GetVerifiedWarpMessageBase},
		{name: "no_predicates", preds: nil, index: 0, gas: 1_000_000, want: invalid, wantGas: 1_000_000 - Gas.GetVerifiedWarpMessageBase},
		{name: "second_of_two", preds: []predicate.Predicate{hashPred, pred}, failed: []int{0}, index: 1, gas: readCost(pred), want: valid},
		{name: "underfunded_base", preds: []predicate.Predicate{pred}, index: 0, gas: Gas.GetVerifiedWarpMessageBase - 1, wantErr: vm.ErrOutOfGas},
		{name: "underfunded_read", preds: []predicate.Predicate{pred}, index: 0, gas: readCost(pred) - 1, wantErr: vm.ErrOutOfGas},
		{name: "wrong_payload_type", preds: []predicate.Predicate{hashPred}, index: 0, gas: 1_000_000, wantErr: errInvalidAddressedPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := f.env(t, tt.gas, tt.preds, tt.failed...)
			got, err := f.c.run(env, must(t)(PackGetVerifiedWarpMessage(tt.index)))
			require.ErrorIs(t, err, tt.wantErr, "run(getVerifiedWarpMessage)")
			require.NoError(t, env.invalidated, "run(getVerifiedWarpMessage) must not invalidate execution")
			if err != nil {
				return
			}
			require.Equal(t, tt.want, got, "run(getVerifiedWarpMessage) output")
			require.Equal(t, tt.wantGas, env.gas, "remaining gas")
		})
	}

	t.Run("selector_only", func(t *testing.T) {
		env := f.env(t, 1_000_000, []predicate.Predicate{pred})
		_, err := f.c.run(env, ABI.Methods["getVerifiedWarpMessage"].ID)
		require.ErrorIs(t, err, errInvalidIndexInput, "run(getVerifiedWarpMessage) without arguments")
	})

	t.Run("index_above_max_int32", func(t *testing.T) {
		env := f.env(t, 1_000_000, []predicate.Predicate{pred})
		_, err := f.c.run(env, must(t)(PackGetVerifiedWarpMessage(math.MaxInt32+1)))
		require.ErrorIs(t, err, errInvalidIndexInput, "run(getVerifiedWarpMessage) huge index")
	})

	t.Run("tx_absent_from_results", func(t *testing.T) {
		// Results exist for another transaction only; this one has none
		// recorded, so its predicates count as passing.
		env := f.env(t, readCost(pred), []predicate.Predicate{pred})
		other := predicate.BlockResults{}
		other.Set(common.Hash{'o'}, predicate.PrecompileResults{ContractAddress: set.NewBits(0)})
		env.header.Extra = must(t)(other.Bytes())
		got, err := f.c.run(env, must(t)(PackGetVerifiedWarpMessage(0)))
		require.NoError(t, err, "run(getVerifiedWarpMessage)")
		require.Equal(t, valid, got, "run(getVerifiedWarpMessage) output")
		require.NoError(t, env.invalidated, "run(getVerifiedWarpMessage) must not invalidate execution")
	})

	t.Run("empty_extra", func(t *testing.T) {
		env := f.env(t, readCost(pred), []predicate.Predicate{pred})
		env.header.Extra = nil
		got, err := f.c.run(env, must(t)(PackGetVerifiedWarpMessage(0)))
		require.NoError(t, err, "run(getVerifiedWarpMessage)")
		require.Equal(t, valid, got, "run(getVerifiedWarpMessage) output")
		require.NoError(t, env.invalidated, "run(getVerifiedWarpMessage) must not invalidate execution")
	})

	t.Run("corrupt_extra", func(t *testing.T) {
		env := f.env(t, 1_000_000, []predicate.Predicate{pred})
		env.header.Extra = []byte{0xff, 0xff, 0xff}
		_, err := f.c.run(env, must(t)(PackGetVerifiedWarpMessage(0)))
		require.ErrorIs(t, err, errInvalidPredicateResults, "run(getVerifiedWarpMessage)")
		require.ErrorIs(t, env.invalidated, errInvalidPredicateResults, "run(getVerifiedWarpMessage) InvalidateExecution() argument")
	})

	t.Run("state_without_predicates", func(t *testing.T) {
		env := f.env(t, 1_000_000, []predicate.Predicate{pred})
		env.state = nil // ReadOnlyState() returns a typed nil that is not a PredicateReader
		env.PrecompileEnvironment = nil
		plain := &plainReaderEnv{stubEnv: env}
		_, err := f.c.run(plain, must(t)(PackGetVerifiedWarpMessage(0)))
		require.ErrorIs(t, err, errNoPredicateReader, "run(getVerifiedWarpMessage)")
		require.ErrorIs(t, env.invalidated, errNoPredicateReader, "run(getVerifiedWarpMessage) InvalidateExecution() argument")
	})
}

// plainReaderEnv exposes a state reader that is not a [precompile.PredicateReader].
type plainReaderEnv struct{ *stubEnv }

type plainReader struct{ libevm.StateReader }

func (plainReaderEnv) ReadOnlyState() libevm.StateReader { return plainReader{} }

func TestGetVerifiedWarpBlockHash(t *testing.T) {
	f := newFixture(t)
	blkID := ids.GenerateTestID()
	hashPayload, err := payload.NewHash(blkID)
	require.NoError(t, err, "payload.NewHash()")
	unsigned, err := avalanchewarp.NewUnsignedMessage(f.ctx.NetworkID, snowtest.XChainID, hashPayload.Bytes())
	require.NoError(t, err, "avalanchewarp.NewUnsignedMessage()")
	pred := predicate.New(f.vdrs.Sign(t, unsigned).Bytes())

	valid := must(t)(PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
		WarpBlockHash: WarpBlockHash{SourceChainID: common.Hash(snowtest.XChainID), BlockHash: common.Hash(blkID)},
		Valid:         true,
	}))
	invalid := must(t)(PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false}))
	cost := Gas.GetVerifiedWarpMessageBase + Gas.PerWarpMessageChunk*uint64(len(pred))

	env := f.env(t, cost, []predicate.Predicate{pred})
	got, err := f.c.run(env, must(t)(PackGetVerifiedWarpBlockHash(0)))
	require.NoError(t, err, "run(getVerifiedWarpBlockHash)")
	require.Equal(t, valid, got, "run(getVerifiedWarpBlockHash) output")
	require.Zero(t, env.gas, "remaining gas")

	env = f.env(t, cost, []predicate.Predicate{pred}, 0)
	got, err = f.c.run(env, must(t)(PackGetVerifiedWarpBlockHash(0)))
	require.NoError(t, err, "run(getVerifiedWarpBlockHash) failed predicate")
	require.Equal(t, invalid, got, "run(getVerifiedWarpBlockHash) failed predicate output")

	// An addressed-call message is the wrong payload type for this function.
	addressed, err := payload.NewAddressedCall([]byte{1}, []byte{2})
	require.NoError(t, err, "payload.NewAddressedCall()")
	wrongUnsigned, err := avalanchewarp.NewUnsignedMessage(f.ctx.NetworkID, snowtest.XChainID, addressed.Bytes())
	require.NoError(t, err, "avalanchewarp.NewUnsignedMessage()")
	wrongPred := predicate.New(f.vdrs.Sign(t, wrongUnsigned).Bytes())
	env = f.env(t, 1_000_000, []predicate.Predicate{wrongPred})
	_, err = f.c.run(env, must(t)(PackGetVerifiedWarpBlockHash(0)))
	require.ErrorIs(t, err, errInvalidBlockHashPayload, "run(getVerifiedWarpBlockHash) wrong payload")
}
