// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
)

// newRules returns rules with the warp precompile registered at each of the
// given addresses.
func newRules(contracts ...common.Address) *extras.Rules {
	contract := warp.NewDefaultConfig(utils.PointerTo[uint64](0))

	predicaters := make(map[common.Address]precompileconfig.Predicater, len(contracts))
	for _, addr := range contracts {
		predicaters[addr] = contract
	}
	return &extras.Rules{
		Predicaters: predicaters,
	}
}

func TestBlockPredicates(t *testing.T) {
	msg, _ := newAddressedCall(t)
	var (
		vdrs           = warptest.NewValidators(t, 2)
		sourceSubnetID = ids.GenerateTestID()

		validPredicate   = predicate.New(vdrs.Sign(t, msg).Bytes())
		invalidPredicate = predicate.New(warptest.FakeSign(t, msg).Bytes())

		addr0 = common.Address{0}
		addr1 = common.Address{1}

		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
			},
		})
		twoInvalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
			},
		})
		mixedTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: validPredicate},
			},
		})
		twoAddressTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr1, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: invalidPredicate},
				{Address: addr1, StorageKeys: validPredicate},
			},
		})
	)
	tests := []struct {
		name         string
		contracts    []common.Address
		blockContext *block.Context
		txs          []*types.Transaction
		expected     predicate.BlockResults
		expectedErr  error
	}{
		{
			name:         "no_predicaters",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
		},
		{
			name:         "no_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{}),
			},
		},
		{
			name:         "filtered_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{
					AccessList: types.AccessList{
						{Address: addr1, StorageKeys: validPredicate},
					},
				}),
			},
		},
		{
			name:      "no_block_context",
			contracts: []common.Address{addr0},
			txs: []*types.Transaction{
				validTx,
			},
			expectedErr: errNoBlockContext,
		},
		{
			name:         "one_tx_one_address_one_predicate",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
			expected: predicate.BlockResults{
				validTx.Hash(): {
					addr0: set.NewBits(),
				},
			},
		},
		{
			name:         "one_tx_one_address_one_invalid_predicate",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				invalidTx,
			},
			expected: predicate.BlockResults{
				invalidTx.Hash(): {
					addr0: set.NewBits(0),
				},
			},
		},
		{
			name:         "one_address_multiple_invalid_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				twoInvalidTx,
			},
			expected: predicate.BlockResults{
				twoInvalidTx.Hash(): {
					addr0: set.NewBits(0, 1),
				},
			},
		},
		{
			name:         "one_address_mixed_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				mixedTx,
			},
			expected: predicate.BlockResults{
				mixedTx.Hash(): {
					addr0: set.NewBits(1, 2),
				},
			},
		},
		{
			name:         "two_addresses_mixed_predicates",
			contracts:    []common.Address{addr0, addr1},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				twoAddressTx,
			},
			expected: predicate.BlockResults{
				twoAddressTx.Hash(): {
					addr0: set.NewBits(1, 2),
					addr1: set.NewBits(0, 3),
				},
			},
		},
		{
			name:         "multiple_txs",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
				invalidTx,
			},
			expected: predicate.BlockResults{
				validTx.Hash(): {
					addr0: set.NewBits(),
				},
				invalidTx.Hash(): {
					addr0: set.NewBits(0),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snowContext := snowtest.Context(t, snowtest.CChainID)
			snowContext.ValidatorState = vdrs.State(sourceSubnetID)
			rules := newRules(test.contracts...)

			actual, err := VerifyBlock(snowContext, test.blockContext, rules, test.txs)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expected, actual)
		})
	}
}

// BenchmarkBlockPredicates measures predicate verification of a block using the
// production warp precompile, so each predicate performs a real BLS aggregate
// signature verification. The matrix varies the block size (txs) and the number
// of predicates per transaction.
//
// It only depends on verifyBlock's stable signature, so the same benchmark
// can be run on the sae-devnet-5 branch and compared with benchstat.
func BenchmarkBlockPredicates(b *testing.B) {
	// The aggregate signature size barely affects verification time, so the
	// number of signers is held fixed.
	const numSigners = 10

	rules := newRules(warp.ContractAddress)
	vdrs := warptest.NewValidators(b, numSigners)
	snowContext := snowtest.Context(b, snowtest.CChainID)
	snowContext.ValidatorState = vdrs.State(ids.GenerateTestID())
	blockContext := &block.Context{}

	msg, _ := newAddressedCall(b)
	pred := predicate.New(vdrs.Sign(b, msg).Bytes())
	for _, numTxs := range []int{1, 10, 100} {
		for _, predicatesPerTx := range []int{1, 10, 100} {
			b.Run(fmt.Sprintf("txs=%d/predicates_per_tx=%d", numTxs, predicatesPerTx), func(b *testing.B) {
				accessList := make(types.AccessList, predicatesPerTx)
				for i := range accessList {
					accessList[i] = types.AccessTuple{
						Address:     warp.ContractAddress,
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
				results, err := VerifyBlock(snowContext, blockContext, rules, txs)
				require.NoError(b, err)
				require.Len(b, results, numTxs)

				wantTxResults := predicate.PrecompileResults{
					warp.ContractAddress: set.NewBits(),
				}
				for _, txResults := range results {
					require.Equal(b, wantTxResults, txResults)
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
