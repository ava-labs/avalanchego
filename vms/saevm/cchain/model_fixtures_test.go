// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// TestModelFixtures validates the hand-assembled fixture bytecode against a
// plain SUT before the rapid machine depends on it.
func TestModelFixtures(t *testing.T) {
	w := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	sender := w.Addresses()[0]
	ctx, sut := newSUT(t, withMaxAllocFor(sender))

	deploy := func(runtime []byte) common.Address {
		tx := w.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			Gas:       1_000_000,
			GasFeeCap: big.NewInt(txGasFeeCap),
			Data:      deployCode(t, runtime),
		})
		require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, tx), "SendTransaction(deploy)")
		sut.waitForPendingEthTxs(ctx, t, tx)
		blk := sut.runConsensusLoop(ctx, t)
		require.Lenf(t, blk.Receipts(), 1, "deploy block receipts")
		r := blk.Receipts()[0]
		require.Equalf(t, types.ReceiptStatusSuccessful, r.Status, "deploy receipt status")
		return r.ContractAddress
	}

	store := deploy(storeRuntime(t))
	reverter := deploy(reverterRuntime(t))

	t.Run("store_writes_slot_and_logs", func(t *testing.T) {
		key := common.HexToHash("0x01")
		val := common.HexToHash("0xabcd")
		tx := w.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &store,
			Gas:       1_000_000,
			GasFeeCap: big.NewInt(txGasFeeCap),
			Data:      slices.Concat(key[:], val[:]),
		})
		require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, tx), "SendTransaction(store)")
		sut.waitForPendingEthTxs(ctx, t, tx)
		blk := sut.runConsensusLoop(ctx, t)
		r := blk.Receipts()[0]
		require.Equal(t, types.ReceiptStatusSuccessful, r.Status, "store receipt status")
		require.Lenf(t, r.Logs, 1, "store logs")
		require.Equal(t, []common.Hash{val}, r.Logs[0].Topics, "store log topic is the stored value")

		state, err := sut.LastExecutedState()
		require.NoError(t, err, "LastExecutedState()")
		require.Equal(t, val, state.GetState(store, key), "stored slot value")
	})

	t.Run("reverter_status_tracks_parity", func(t *testing.T) {
		for arg, wantStatus := range map[uint64]uint64{
			2: types.ReceiptStatusSuccessful,
			3: types.ReceiptStatusFailed,
		} {
			argHash := common.BigToHash(new(big.Int).SetUint64(arg))
			tx := w.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
				To:        &reverter,
				Gas:       1_000_000,
				GasFeeCap: big.NewInt(txGasFeeCap),
				Data:      argHash[:],
			})
			require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, tx), "SendTransaction(revert arg %d)", arg)
			sut.waitForPendingEthTxs(ctx, t, tx)
			blk := sut.runConsensusLoop(ctx, t)
			require.Equalf(t, wantStatus, blk.Receipts()[0].Status, "reverter status for arg %d", arg)
		}
	})
}

// storeRuntime stores calldata[32:64] at slot calldata[0:32] and emits
// LOG1 with the stored value as its only topic.
func storeRuntime(tb testing.TB) []byte {
	tb.Helper()
	return slices.Concat(
		saetest.Push(tb, []byte{32}), saetest.Ops(vm.CALLDATALOAD), // value (SSTORE operand)
		saetest.Ops(vm.PUSH0, vm.CALLDATALOAD), // key on top
		saetest.Ops(vm.SSTORE),
		saetest.Push(tb, []byte{32}), saetest.Ops(vm.CALLDATALOAD), // topic = value
		saetest.Ops(vm.PUSH0, vm.PUSH0, vm.LOG1), // size, offset on top
		saetest.Ops(vm.STOP),
	)
}

// reverterRuntime reverts iff calldata[0:32] is odd.
func reverterRuntime(tb testing.TB) []byte {
	tb.Helper()
	return slices.Concat(
		saetest.Ops(vm.PUSH0, vm.CALLDATALOAD),
		saetest.Push(tb, []byte{1}), saetest.Ops(vm.AND),
		saetest.Push(tb, []byte{9}), saetest.Ops(vm.JUMPI), // JUMPDEST is at offset 9
		saetest.Ops(vm.STOP),
		saetest.Ops(vm.JUMPDEST, vm.PUSH0, vm.PUSH0, vm.REVERT),
	)
}

// deployCode wraps runtime in minimal initcode: copy the runtime into memory
// and return it. Prefix layout is exactly 12 bytes; the runtime follows.
func deployCode(tb testing.TB, runtime []byte) []byte {
	tb.Helper()
	require.LessOrEqualf(tb, len(runtime), 0xffff, "runtime too large for PUSH2 length")
	length := []byte{byte(len(runtime) >> 8), byte(len(runtime))}
	const runtimeOffset = 12
	return slices.Concat(
		saetest.Push(tb, length), saetest.Push(tb, []byte{runtimeOffset}),
		saetest.Ops(vm.PUSH0, vm.CODECOPY), // codecopy(dst=0, offset=12, len)
		saetest.Push(tb, length), saetest.Ops(vm.PUSH0, vm.RETURN),
		runtime,
	)
}
