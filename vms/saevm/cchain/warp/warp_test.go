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
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"

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
		Address: corethwarp.ContractAddress,
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

// newRules returns rules with the warp precompile registered at each of the
// given addresses.
func newRules(contracts ...common.Address) *extras.Rules {
	contract := corethwarp.NewDefaultConfig(utils.PointerTo[uint64](0))

	predicaters := make(map[common.Address]precompileconfig.Predicater, len(contracts))
	for _, addr := range contracts {
		predicaters[addr] = contract
	}
	return &extras.Rules{
		Predicaters: predicaters,
	}
}

func TestVerifyBlock(t *testing.T) {
	var (
		vdrs = warptest.NewValidators(t, 2)

		msg, _           = newAddressedCall(t)
		validPredicate   = predicate.New(vdrs.Sign(t, msg).Bytes())
		invalidPredicate = predicate.New(warptest.IncorrectlySign(t, msg).Bytes())

		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: corethwarp.ContractAddress, StorageKeys: validPredicate},
			},
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: corethwarp.ContractAddress, StorageKeys: invalidPredicate},
			},
		})
		twoInvalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: corethwarp.ContractAddress, StorageKeys: invalidPredicate},
				{Address: corethwarp.ContractAddress, StorageKeys: invalidPredicate},
			},
		})
		mixedTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: corethwarp.ContractAddress, StorageKeys: validPredicate},
				{Address: corethwarp.ContractAddress, StorageKeys: invalidPredicate},
				{Address: corethwarp.ContractAddress, StorageKeys: invalidPredicate},
				{Address: corethwarp.ContractAddress, StorageKeys: validPredicate},
			},
		})
	)
	tests := []struct {
		name         string
		contracts    []common.Address
		blockContext *block.Context
		txs          []*types.Transaction
		want         predicate.BlockResults
		wantErr      error
	}{
		{
			name:         "no_predicaters",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
		},
		{
			name: "no_predicaters_no_context",
			txs: []*types.Transaction{
				validTx,
			},
		},
		{
			name:         "no_predicates",
			contracts:    []common.Address{corethwarp.ContractAddress},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{}),
			},
		},
		{
			name:      "no_predicates_no_context",
			contracts: []common.Address{corethwarp.ContractAddress},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{}),
			},
		},
		{
			name:         "filtered_predicates",
			contracts:    []common.Address{corethwarp.ContractAddress},
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
			name:      "missing_block_context",
			contracts: []common.Address{corethwarp.ContractAddress},
			txs: []*types.Transaction{
				validTx,
			},
			wantErr: errNoBlockContext,
		},
		{
			name:         "one_tx_one_address_one_predicate",
			contracts:    []common.Address{corethwarp.ContractAddress},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
			want: predicate.BlockResults{
				validTx.Hash(): {
					corethwarp.ContractAddress: set.NewBits(),
				},
			},
		},
		{
			name:         "one_tx_one_address_one_invalid_predicate",
			contracts:    []common.Address{corethwarp.ContractAddress},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				invalidTx,
			},
			want: predicate.BlockResults{
				invalidTx.Hash(): {
					corethwarp.ContractAddress: set.NewBits(0),
				},
			},
		},
		{
			name:         "one_address_multiple_invalid_predicates",
			contracts:    []common.Address{corethwarp.ContractAddress},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				twoInvalidTx,
			},
			want: predicate.BlockResults{
				twoInvalidTx.Hash(): {
					corethwarp.ContractAddress: set.NewBits(0, 1),
				},
			},
		},
		{
			name:         "one_address_mixed_predicates",
			contracts:    []common.Address{corethwarp.ContractAddress},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				mixedTx,
			},
			want: predicate.BlockResults{
				mixedTx.Hash(): {
					corethwarp.ContractAddress: set.NewBits(1, 2),
				},
			},
		},
		{
			name:         "multiple_txs",
			contracts:    []common.Address{corethwarp.ContractAddress},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
				invalidTx,
			},
			want: predicate.BlockResults{
				validTx.Hash(): {
					corethwarp.ContractAddress: set.NewBits(),
				},
				invalidTx.Hash(): {
					corethwarp.ContractAddress: set.NewBits(0),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snowContext := snowtest.Context(t, snowtest.CChainID)
			warptest.SetValidators(t, snowContext, vdrs)
			rules := newRules(test.contracts...)

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

	rules := newRules(corethwarp.ContractAddress)
	vdrs := warptest.NewValidators(b, numSigners)
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
						Address:     corethwarp.ContractAddress,
						StorageKeys: pred,
					}
				}

				txs := make([]*types.Transaction, numTxs)
				for i := range txs {
					// A unique nonce gives every transaction a distinct hash,
					// matching the per-tx work of a real block.
					txs[i] = types.NewTx(&types.DynamicFeeTx{
						Nonce:      uint64(i), //#nosec G115 -- Known non-negative
						AccessList: accessList,
					})
				}

				// Confirm the predicates verify before timing, so the benchmark
				// measures the success path rather than an early failure.
				results, err := VerifyBlock(snowContext, blockContext, rules, txs)
				require.NoError(b, err, "VerifyBlock()")
				require.Len(b, results, numTxs, "VerifyBlock() results length")

				wantTxResults := predicate.PrecompileResults{
					corethwarp.ContractAddress: set.NewBits(),
				}
				for txHash, txResults := range results {
					require.Equalf(b, wantTxResults, txResults, "VerifyBlock()[%s] txResults", txHash)
				}

				for b.Loop() {
					_, _ = VerifyBlock(snowContext, blockContext, rules, txs)
				}

				predicates := numTxs * predicatesPerTx
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*predicates), "ns/predicate")
			})
		}
	}
}
