// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

var (
	validPredicate      = predicate.New([]byte{0})
	invalidPredicate    = predicate.New([]byte{1})
	errInvalidPredicate = errors.New("invalid predicate")
)

type predicater struct{}

func (predicater) PredicateGas(predicate.Predicate, precompileconfig.Rules) (uint64, error) {
	return 0, nil
}

func (predicater) VerifyPredicate(_ *precompileconfig.PredicateContext, pred predicate.Predicate) error {
	if slices.Equal(pred, validPredicate) {
		return nil
	}
	return errInvalidPredicate
}

func newRules(contracts ...common.Address) *extras.Rules {
	rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
	rulesExtra := params.GetRulesExtra(rules)
	for _, addr := range contracts {
		rulesExtra.Predicaters[addr] = predicater{}
	}
	return rulesExtra
}

func TestBlockPredicates(t *testing.T) {
	var (
		addr    = common.Address{0}
		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr, StorageKeys: validPredicate},
			},
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr, StorageKeys: invalidPredicate},
			},
		})
	)
	tests := []struct {
		name         string
		blockContext *block.Context
		txs          []*types.Transaction
		expected     predicate.BlockResults
		expectedErr  error
	}{
		{
			name: "invalid",
			txs: []*types.Transaction{
				validTx,
			},
			expectedErr: errNoBlockContext,
		},
		{
			name:         "one_tx_one_address_one_predicate",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
			expected: predicate.BlockResults{
				validTx.Hash(): {
					addr: set.NewBits(),
				},
			},
		},
		{
			name:         "one_tx_one_address_one_invalid_predicate",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				invalidTx,
			},
			expected: predicate.BlockResults{
				invalidTx.Hash(): {
					addr: set.NewBits(0),
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
			expected: predicate.BlockResults{
				validTx.Hash(): {
					addr: set.NewBits(),
				},
				invalidTx.Hash(): {
					addr: set.NewBits(0),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				snowContext = snowtest.Context(t, snowtest.CChainID)
				rules       = newRules(addr)
			)
			actual, err := blockPredicates(snowContext, test.blockContext, rules, test.txs)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestTxPredicates(t *testing.T) {
	var (
		addr0 = common.Address{0}
		addr1 = common.Address{1}
	)
	tests := []struct {
		name         string
		contracts    []common.Address
		blockContext *block.Context
		accessList   types.AccessList
		expected     predicate.PrecompileResults
		expectedErr  error
	}{
		{
			name: "no_predicaters",
		},
		{
			name:      "no_predicates",
			contracts: []common.Address{addr0},
		},
		{
			name:      "filtered_predicates",
			contracts: []common.Address{addr0},
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: validPredicate},
			},
		},
		{
			name:      "no_block_context",
			contracts: []common.Address{addr0},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
			expectedErr: errNoBlockContext,
		},
		{
			name:         "one_address_one_predicate",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(),
			},
		},
		{
			name:         "one_address_one_invalid_predicate",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(0),
			},
		},
		{
			name:         "one_address_two_invalid_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(0, 1),
			},
		},
		{
			name:         "one_address_two_mixed_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(1),
			},
		},
		{
			name:         "one_address_mixed_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: validPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(1, 2),
			},
		},
		{
			name:         "two_addresses_mixed_predicates",
			contracts:    []common.Address{addr0, addr1},
			blockContext: &block.Context{},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr1, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: invalidPredicate},
				{Address: addr1, StorageKeys: validPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(1, 2),
				addr1: set.NewBits(0, 3),
			},
		},
		{
			name:         "two_addresses_all_valid_predicates",
			contracts:    []common.Address{addr0, addr1},
			blockContext: &block.Context{},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: validPredicate},
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr0, StorageKeys: validPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(),
				addr1: set.NewBits(),
			},
		},
		{
			name:         "two_addresses_all_invalid_predicates",
			contracts:    []common.Address{addr0, addr1},
			blockContext: &block.Context{},
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr1, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(0, 1, 2),
				addr1: set.NewBits(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				snowContext = snowtest.Context(t, snowtest.CChainID)
				rules       = newRules(test.contracts...)
				tx          = types.NewTx(&types.DynamicFeeTx{
					AccessList: test.accessList,
				})
			)
			actual, err := txPredicates(snowContext, test.blockContext, rules, tx)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expected, actual)
		})
	}
}
