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

// TestVerifyBlock covers the subnet-evm glue around
// [saewarp.VerifyBlockPredicates]: predicaters sourced from [extras.Rules] and
// the block-context translation. The filtering and bit-indexing matrix is
// pinned by the shared engine's own tests.
func TestVerifyBlock(t *testing.T) {
	var (
		addr0 = common.Address{0}
		addr1 = common.Address{1}

		validTx = newAccessListTx(types.AccessList{
			{Address: addr0, StorageKeys: validPredicate},
		})
		invalidTx = newAccessListTx(types.AccessList{
			{Address: addr0, StorageKeys: invalidPredicate},
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
			name: "no_predicaters",
			txs:  []*types.Transaction{validTx},
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
			name:         "valid_and_invalid_predicates",
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
			got, err := VerifyBlock(snowContext, test.blockContext, newRules(test.contracts...), test.txs)
			require.ErrorIs(t, err, test.wantErr, "VerifyBlock()")
			if test.wantErr != nil {
				return
			}
			require.Equal(t, test.want, got, "VerifyBlock()")
		})
	}
}
