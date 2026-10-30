// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/warptest"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

func newHash(tb testing.TB) (*avalanchewarp.UnsignedMessage, *payload.Hash) {
	p, err := payload.NewHash(
		ids.GenerateTestID(),
	)
	require.NoError(tb, err, "payload.NewHash()")

	m, err := avalanchewarp.NewUnsignedMessage(constants.UnitTestID, snowtest.XChainID, p.Bytes())
	require.NoError(tb, err, "warp.NewUnsignedMessage()")
	return m, p
}

func newAddressedCall(tb testing.TB) (*avalanchewarp.UnsignedMessage, *payload.AddressedCall) {
	p, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		[]byte("test"),
	)
	require.NoError(tb, err, "payload.NewAddressedCall()")

	m, err := avalanchewarp.NewUnsignedMessage(constants.UnitTestID, snowtest.XChainID, p.Bytes())
	require.NoError(tb, err, "warp.NewUnsignedMessage()")
	return m, p
}

// newSendWarpMessageLog returns the log emitted by the warp precompile when
// sending msg.
func newSendWarpMessageLog(tb testing.TB, msg []byte) *types.Log {
	topics, data, err := corethwarp.PackSendWarpMessageEvent(common.Address{}, common.Hash{}, msg)
	require.NoErrorf(tb, err, "warp.PackSendWarpMessageEvent(..., %d bytes)", len(msg))
	return &types.Log{
		Address: warpmsg.ContractAddress,
		Topics:  topics,
		Data:    data,
	}
}

func TestFromReceipts(t *testing.T) {
	var (
		hash, _ = newHash(t)
		call, _ = newAddressedCall(t)

		warpHash = newSendWarpMessageLog(t, hash.Bytes())
		warpCall = newSendWarpMessageLog(t, call.Bytes())
		otherLog = &types.Log{
			Address: common.Address{1},
			Data:    []byte("not a warp message"),
		}
	)
	tests := []struct {
		name    string
		logs    [][]*types.Log
		want    []*avalanchewarp.UnsignedMessage
		wantErr error
	}{
		{
			name: "no_receipts",
		},
		{
			name: "no_logs",
			logs: [][]*types.Log{
				{},
			},
		},
		{
			name: "ignores_other_addresses",
			logs: [][]*types.Log{
				{otherLog},
			},
		},
		{
			name: "single_message",
			logs: [][]*types.Log{
				{warpHash},
			},
			want: []*avalanchewarp.UnsignedMessage{hash},
		},
		{
			name: "multiple_messages_in_order",
			logs: [][]*types.Log{
				{warpCall, otherLog, warpHash},
				{otherLog, warpCall},
			},
			want: []*avalanchewarp.UnsignedMessage{call, hash, call},
		},
		{
			name: "invalid_log_data",
			logs: [][]*types.Log{
				{
					// nil is not a valid warp message format.
					newSendWarpMessageLog(t, nil),
				},
			},
			wantErr: codec.ErrCantUnpackVersion,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receipts := make([]*types.Receipt, len(test.logs))
			for i, logs := range test.logs {
				receipts[i] = &types.Receipt{
					Logs: logs,
				}
			}
			msgs, err := FromReceipts(receipts)
			require.ErrorIs(t, err, test.wantErr, "FromReceipts()")
			require.Equal(t, test.want, msgs, "FromReceipts()")
		})
	}
}

// newRules returns rules with Durango, and therefore Warp, active iff
// isDurango.
func newRules(isDurango bool) *corethparams.RulesExtra {
	return &corethparams.RulesExtra{IsDurango: isDurango}
}

func TestVerifyBlock(t *testing.T) {
	var (
		vdrs = warptest.NewValidators(t, warptest.WithMinimum(2))

		msg, _        = newAddressedCall(t)
		validPacked   = warpmsg.Pack(vdrs.Sign(t, msg).Bytes())
		invalidPacked = warpmsg.Pack(warptest.IncorrectlySign(t, msg).Bytes())

		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validPacked},
			},
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: invalidPacked},
			},
		})
		twoInvalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: invalidPacked},
				{Address: warpmsg.ContractAddress, StorageKeys: invalidPacked},
			},
		})
		mixedTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validPacked},
				{Address: warpmsg.ContractAddress, StorageKeys: invalidPacked},
				{Address: warpmsg.ContractAddress, StorageKeys: invalidPacked},
				{Address: warpmsg.ContractAddress, StorageKeys: validPacked},
			},
		})
	)
	tests := []struct {
		name         string
		isDurango    bool
		blockContext *block.Context
		txs          []*types.Transaction
		want         warpmsg.BlockResults
		wantErr      error
	}{
		{
			name:         "pre_durango",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
		},
		{
			name: "pre_durango_no_context",
			txs: []*types.Transaction{
				validTx,
			},
		},
		{
			name:         "no_messages",
			isDurango:    true,
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{}),
			},
		},
		{
			name:      "no_messages_no_context",
			isDurango: true,
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{}),
			},
		},
		{
			name:         "filtered_messages",
			isDurango:    true,
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{
					AccessList: types.AccessList{
						{Address: common.Address{1}, StorageKeys: validPacked},
					},
				}),
			},
		},
		{
			name:      "missing_block_context",
			isDurango: true,
			txs: []*types.Transaction{
				validTx,
			},
			wantErr: errNoBlockContext,
		},
		{
			name:         "one_tx_one_address_one_message",
			isDurango:    true,
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
			want: warpmsg.BlockResults{
				validTx.Hash(): set.NewBits(),
			},
		},
		{
			name:         "one_tx_one_address_one_invalid_message",
			isDurango:    true,
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				invalidTx,
			},
			want: warpmsg.BlockResults{
				invalidTx.Hash(): set.NewBits(0),
			},
		},
		{
			name:         "one_address_multiple_invalid_messages",
			isDurango:    true,
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				twoInvalidTx,
			},
			want: warpmsg.BlockResults{
				twoInvalidTx.Hash(): set.NewBits(0, 1),
			},
		},
		{
			name:         "one_address_mixed_messages",
			isDurango:    true,
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				mixedTx,
			},
			want: warpmsg.BlockResults{
				mixedTx.Hash(): set.NewBits(1, 2),
			},
		},
		{
			name:         "multiple_txs",
			isDurango:    true,
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
				invalidTx,
			},
			want: warpmsg.BlockResults{
				validTx.Hash():   set.NewBits(),
				invalidTx.Hash(): set.NewBits(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snowContext := snowtest.Context(t, snowtest.CChainID)
			warptest.SetValidators(t, snowContext, vdrs)
			rules := newRules(test.isDurango)

			got, err := VerifyBlock(snowContext, test.blockContext, rules, test.txs)
			require.ErrorIs(t, err, test.wantErr, "VerifyBlock()")
			require.Equal(t, test.want, got, "VerifyBlock()")
		})
	}
}

func BenchmarkVerifyBlock(b *testing.B) {
	// The aggregate signature size barely affects verification time, so the
	// number of signers is held fixed.
	const numSigners = 10

	rules := newRules(true)
	vdrs := warptest.NewValidators(b, warptest.WithMinimum(numSigners))
	snowContext := snowtest.Context(b, snowtest.CChainID)
	warptest.SetValidators(b, snowContext, vdrs)
	blockContext := &block.Context{}

	msg, _ := newAddressedCall(b)
	pred := warpmsg.Pack(vdrs.Sign(b, msg).Bytes())
	for _, numTxs := range []int{1, 10, 100} {
		for _, msgsPerTx := range []int{1, 10, 100} {
			b.Run(fmt.Sprintf("txs=%d/msgs_per_tx=%d", numTxs, msgsPerTx), func(b *testing.B) {
				accessList := make(types.AccessList, msgsPerTx)
				for i := range accessList {
					accessList[i] = types.AccessTuple{
						Address:     warpmsg.ContractAddress,
						StorageKeys: pred,
					}
				}

				txs := make([]*types.Transaction, numTxs)
				for i := range txs {
					// A unique nonce gives every transaction a distinct hash,
					// matching the per-tx work of a real block.
					txs[i] = types.NewTx(&types.DynamicFeeTx{
						Nonce:      uint64(i),
						AccessList: accessList,
					})
				}

				// Confirm the messages verify before timing, so the benchmark
				// measures the success path rather than an early failure.
				results, err := VerifyBlock(snowContext, blockContext, rules, txs)
				require.NoError(b, err, "VerifyBlock()")
				require.Len(b, results, numTxs, "VerifyBlock() results length")

				for txHash, txResults := range results {
					require.Equalf(b, set.NewBits(), txResults, "VerifyBlock()[%s] txResults", txHash)
				}

				for b.Loop() {
					_, _ = VerifyBlock(snowContext, blockContext, rules, txs)
				}

				msgs := numTxs * msgsPerTx
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*msgs), "ns/msg")
			})
		}
	}
}
