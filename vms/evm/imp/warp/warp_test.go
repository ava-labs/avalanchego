// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/warptest"

	warpcontract "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// newSendWarpMessageLog returns the log emitted by the warp precompile when
// sending msg.
func newSendWarpMessageLog(tb testing.TB, msg []byte) *types.Log {
	topics, data, err := warpcontract.PackSendWarpMessageEvent(common.Address{}, common.Hash{}, msg)
	require.NoErrorf(tb, err, "warp.PackSendWarpMessageEvent(..., %d bytes)", len(msg))
	return &types.Log{
		Address: warpcontract.ContractAddress,
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

func TestVerifyBlock(t *testing.T) {
	var (
		vdrs = warptest.NewValidators(t, warptest.WithMinimum(2))

		msg, _           = newAddressedCall(t)
		validPredicate   = predicate.New(vdrs.Sign(t, msg).Bytes())
		invalidPredicate = predicate.New(warptest.IncorrectlySign(t, msg).Bytes())

		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpcontract.ContractAddress, StorageKeys: validPredicate},
			},
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpcontract.ContractAddress, StorageKeys: invalidPredicate},
			},
		})
		twoInvalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpcontract.ContractAddress, StorageKeys: invalidPredicate},
				{Address: warpcontract.ContractAddress, StorageKeys: invalidPredicate},
			},
		})
		mixedTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpcontract.ContractAddress, StorageKeys: validPredicate},
				{Address: warpcontract.ContractAddress, StorageKeys: invalidPredicate},
				{Address: warpcontract.ContractAddress, StorageKeys: invalidPredicate},
				{Address: warpcontract.ContractAddress, StorageKeys: validPredicate},
			},
		})
	)
	tests := []struct {
		name         string
		blockContext *block.Context
		txs          []*types.Transaction
		want         predicate.BlockResults
		wantErr      error
	}{
		{
			name:         "no_predicates",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{}),
			},
		},
		{
			name: "no_predicates_no_context",
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{}),
			},
		},
		{
			name:         "filtered_predicates",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{
					AccessList: types.AccessList{
						{Address: common.Address{1}, StorageKeys: validPredicate},
					},
				}),
			},
		},
		{
			name: "missing_block_context",
			txs: []*types.Transaction{
				validTx,
			},
			wantErr: ErrNoBlockContext,
		},
		{
			name:         "one_tx_one_address_one_predicate",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
			want: predicate.BlockResults{
				validTx.Hash(): {
					warpcontract.ContractAddress: set.NewBits(),
				},
			},
		},
		{
			name:         "one_tx_one_address_one_invalid_predicate",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				invalidTx,
			},
			want: predicate.BlockResults{
				invalidTx.Hash(): {
					warpcontract.ContractAddress: set.NewBits(0),
				},
			},
		},
		{
			name:         "one_address_multiple_invalid_predicates",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				twoInvalidTx,
			},
			want: predicate.BlockResults{
				twoInvalidTx.Hash(): {
					warpcontract.ContractAddress: set.NewBits(0, 1),
				},
			},
		},
		{
			name:         "one_address_mixed_predicates",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				mixedTx,
			},
			want: predicate.BlockResults{
				mixedTx.Hash(): {
					warpcontract.ContractAddress: set.NewBits(1, 2),
				},
			},
		},
		{
			name:         "multiple_txs",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
				invalidTx,
			},
			want: predicate.BlockResults{
				validTx.Hash(): {
					warpcontract.ContractAddress: set.NewBits(),
				},
				invalidTx.Hash(): {
					warpcontract.ContractAddress: set.NewBits(0),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snowContext := snowtest.Context(t, snowtest.CChainID)
			warptest.SetValidators(t, snowContext, vdrs)

			got, err := VerifyBlock(snowContext, test.blockContext, &warpcontract.Config{}, test.txs)
			require.ErrorIs(t, err, test.wantErr, "VerifyBlock()")
			require.Equal(t, test.want, got, "VerifyBlock()")
		})
	}
}

func BenchmarkVerifyBlock(b *testing.B) {
	// The aggregate signature size barely affects verification time, so the
	// number of signers is held fixed.
	const numSigners = 10

	vdrs := warptest.NewValidators(b, warptest.WithMinimum(numSigners))
	snowContext := snowtest.Context(b, snowtest.CChainID)
	warptest.SetValidators(b, snowContext, vdrs)
	blockContext := &block.Context{}

	msg, _ := newAddressedCall(b)
	pred := predicate.New(vdrs.Sign(b, msg).Bytes())
	for _, numTxs := range []int{1, 10, 100} {
		for _, predicatesPerTx := range []int{1, 10, 100} {
			b.Run(fmt.Sprintf("txs=%d/predicates_per_tx=%d", numTxs, predicatesPerTx), func(b *testing.B) {
				accessList := make(types.AccessList, predicatesPerTx)
				for i := range accessList {
					accessList[i] = types.AccessTuple{
						Address:     warpcontract.ContractAddress,
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

				// Confirm the predicates verify before timing, so the benchmark
				// measures the success path rather than an early failure.
				results, err := VerifyBlock(snowContext, blockContext, &warpcontract.Config{}, txs)
				require.NoError(b, err, "VerifyBlock()")
				require.Len(b, results, numTxs, "VerifyBlock() results length")

				wantTxResults := predicate.PrecompileResults{
					warpcontract.ContractAddress: set.NewBits(),
				}
				for txHash, txResults := range results {
					require.Equalf(b, wantTxResults, txResults, "VerifyBlock()[%s] txResults", txHash)
				}

				for b.Loop() {
					_, _ = VerifyBlock(snowContext, blockContext, &warpcontract.Config{}, txs)
				}

				predicates := numTxs * predicatesPerTx
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*predicates), "ns/predicate")
			})
		}
	}
}
