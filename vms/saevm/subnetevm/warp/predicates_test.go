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

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"

	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
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

func newAccessListTx(accessList types.AccessList) *types.Transaction {
	return types.NewTx(&types.DynamicFeeTx{AccessList: accessList})
}

func TestPredicateBytes(t *testing.T) {
	var (
		addr0 = common.Address{0}
		addr1 = common.Address{1}

		validTx = newAccessListTx(types.AccessList{
			{Address: addr0, StorageKeys: validPredicate},
		})
		invalidTx = newAccessListTx(types.AccessList{
			{Address: addr0, StorageKeys: invalidPredicate},
		})
		mixedTx = newAccessListTx(types.AccessList{
			{Address: addr0, StorageKeys: validPredicate},
			{Address: addr1, StorageKeys: invalidPredicate},
			{Address: addr0, StorageKeys: invalidPredicate},
			{Address: addr0, StorageKeys: invalidPredicate},
			{Address: addr1, StorageKeys: validPredicate},
			{Address: addr1, StorageKeys: validPredicate},
			{Address: addr1, StorageKeys: invalidPredicate},
			{Address: addr1, StorageKeys: validPredicate},
		})
	)
	tests := []struct {
		name         string
		contracts    []common.Address
		blockContext *block.Context
		txs          []*types.Transaction
		want         predicate.BlockResults
		// wantNilBytes asserts the nil short-circuit taken when no
		// predicaters are registered at all, which is distinguishable from
		// the marshalled empty [predicate.BlockResults].
		wantNilBytes bool
		wantErr      error
	}{
		{
			name:         "no_predicaters",
			txs:          []*types.Transaction{validTx},
			wantNilBytes: true,
		},
		{
			name:      "no_predicates",
			contracts: []common.Address{addr0},
			txs:       []*types.Transaction{newAccessListTx(nil)},
		},
		{
			name:      "filtered_predicates",
			contracts: []common.Address{addr0},
			txs: []*types.Transaction{newAccessListTx(types.AccessList{
				{Address: addr1, StorageKeys: validPredicate},
			})},
		},
		{
			name:      "no_block_context",
			contracts: []common.Address{addr0},
			txs:       []*types.Transaction{validTx},
			wantErr:   saewarp.ErrNoBlockContext,
		},
		{
			name:         "one_tx_one_address_one_predicate",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs:          []*types.Transaction{validTx},
			want: predicate.BlockResults{
				validTx.Hash(): {
					addr0: set.NewBits(),
				},
			},
		},
		{
			name:         "one_tx_one_address_one_invalid_predicate",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs:          []*types.Transaction{invalidTx},
			want: predicate.BlockResults{
				invalidTx.Hash(): {
					addr0: set.NewBits(0),
				},
			},
		},
		{
			name:         "two_addresses_mixed_predicates",
			contracts:    []common.Address{addr0, addr1},
			blockContext: &block.Context{},
			txs:          []*types.Transaction{mixedTx},
			want: predicate.BlockResults{
				mixedTx.Hash(): {
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
			want: predicate.BlockResults{
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
			got, err := PredicateBytes(snowContext, test.blockContext, newRules(test.contracts...), test.txs)
			require.ErrorIs(t, err, test.wantErr, "PredicateBytes()")
			if test.wantErr != nil {
				return
			}
			if test.wantNilBytes {
				require.Nil(t, got, "PredicateBytes()")
				return
			}

			want, err := test.want.Bytes()
			require.NoError(t, err, "predicate.BlockResults.Bytes()")
			require.Equal(t, want, got, "PredicateBytes()")
		})
	}
}
