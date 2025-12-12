// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

var (
	validPredicate      = predicate.New([]byte{0})
	invalidPredicate    = predicate.New([]byte{1})
	errInvalidPredicate = errors.New("invalid predicate")
)

func verifyMockPredicate(_ *precompileconfig.PredicateContext, pred predicate.Predicate) error {
	if slices.Equal(pred, validPredicate) {
		return nil
	}
	return errInvalidPredicate
}

func TestCheckBlockPredicates(t *testing.T) {
	var (
		addr    = common.Address{0}
		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr, StorageKeys: validPredicate},
			},
			Gas: math.MaxUint64,
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr, StorageKeys: invalidPredicate},
			},
			Gas: math.MaxUint64,
		})
	)
	tests := []struct {
		name                string
		nilPredicateContext bool
		txs                 []*types.Transaction
		expected            predicate.BlockResults
		expectedErr         error
	}{
		{
			name:                "invalid",
			nilPredicateContext: true,
			txs: []*types.Transaction{
				validTx,
			},
			expectedErr: ErrMissingPredicateContext,
		},
		{
			name: "one_tx_one_address_one_predicate",
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
			name: "one_tx_one_address_one_invalid_predicate",
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
			name: "multiple_txs",
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
			require := require.New(t)

			rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
			predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
			predicater.EXPECT().PredicateGas(gomock.Any(), gomock.Any()).Return(uint64(0), nil).AnyTimes()
			predicater.EXPECT().VerifyPredicate(gomock.Any(), gomock.Any()).DoAndReturn(verifyMockPredicate).AnyTimes()

			rulesExtra := params.GetRulesExtra(rules)
			rulesExtra.Predicaters[addr] = predicater

			predicateContext := &precompileconfig.PredicateContext{
				ProposerVMBlockCtx: &block.Context{},
			}
			if test.nilPredicateContext {
				predicateContext = nil
			}

			actual, err := CheckBlockPredicates(rules, predicateContext, test.txs)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

func TestCheckTxPredicates(t *testing.T) {
	var (
		addr0 = common.Address{0}
		addr1 = common.Address{1}
	)
	tests := []struct {
		name                string
		predicates          set.Set[common.Address]
		predicateGas        uint64
		nilBlockContext     bool
		nilPredicateContext bool
		insufficientGas     bool
		accessList          types.AccessList
		expected            predicate.PrecompileResults
		expectedErr         error
	}{
		{
			name:         "invalid_intrinsic_gas",
			predicates:   set.Of(addr0),
			predicateGas: math.MaxUint64,
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
			expectedErr: ErrGasUintOverflow,
		},
		{
			name:            "insufficient_intrinsic_gas",
			predicates:      set.Of(addr0),
			insufficientGas: true,
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
			expectedErr: ErrIntrinsicGas,
		},
		{
			name: "no_predicaters",
		},
		{
			name:       "no_predicates",
			predicates: set.Of(addr0),
		},
		{
			name:       "filtered_predicates",
			predicates: set.Of(addr0),
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: validPredicate},
			},
		},
		{
			name:            "nil_block_context",
			predicates:      set.Of(addr0),
			nilBlockContext: true,
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
			expectedErr: ErrMissingPredicateContext,
		},
		{
			name:                "nil_predicate_context",
			predicates:          set.Of(addr0),
			nilPredicateContext: true,
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
			expectedErr: ErrMissingPredicateContext,
		},
		{
			name:                "nil_predicate_with_filtered_predicates",
			predicates:          set.Of(addr0),
			nilPredicateContext: true,
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: validPredicate},
			},
		},
		{
			name:       "one_address_one_predicate",
			predicates: set.Of(addr0),
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(),
			},
		},
		{
			name:       "one_address_one_invalid_predicate",
			predicates: set.Of(addr0),
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(0),
			},
		},
		{
			name:       "one_address_two_invalid_predicates",
			predicates: set.Of(addr0),
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(0, 1),
			},
		},
		{
			name:       "one_address_two_mixed_predicates",
			predicates: set.Of(addr0),
			accessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
			},
			expected: predicate.PrecompileResults{
				addr0: set.NewBits(1),
			},
		},
		{
			name:       "one_address_mixed_predicates",
			predicates: set.Of(addr0),
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
			name:       "two_addresses_mixed_predicates",
			predicates: set.Of(addr0, addr1),
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
			name:       "two_addresses_all_valid_predicates",
			predicates: set.Of(addr0, addr1),
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
			name:       "two_addresses_all_invalid_predicates",
			predicates: set.Of(addr0, addr1),
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
			require := require.New(t)

			rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
			predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
			predicater.EXPECT().PredicateGas(gomock.Any(), gomock.Any()).Return(test.predicateGas, nil).AnyTimes()
			predicater.EXPECT().VerifyPredicate(gomock.Any(), gomock.Any()).DoAndReturn(verifyMockPredicate).AnyTimes()

			rulesExtra := params.GetRulesExtra(rules)
			for addr := range test.predicates {
				rulesExtra.Predicaters[addr] = predicater
			}

			predicateContext := &precompileconfig.PredicateContext{
				ProposerVMBlockCtx: &block.Context{},
			}
			if test.nilBlockContext {
				predicateContext.ProposerVMBlockCtx = nil
			}
			if test.nilPredicateContext {
				predicateContext = nil
			}

			gas := uint64(math.MaxUint64)
			if test.insufficientGas {
				gas = 0
			}
			tx := types.NewTx(&types.DynamicFeeTx{
				AccessList: test.accessList,
				Gas:        gas,
			})
			actual, err := CheckTxPredicates(rules, predicateContext, tx)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}
