// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/warptest"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// newWarpMessages returns a Warp message signed by the validators in the
// returned context, and the same message with an invalid signature.
func newWarpMessages(t *testing.T) (*precompileconfig.PredicateContext, predicate.Predicate, predicate.Predicate) {
	t.Helper()

	addressedCall, err := payload.NewAddressedCall([]byte{1}, []byte{2})
	require.NoError(t, err, "payload.NewAddressedCall()")
	msg, err := avalancheWarp.NewUnsignedMessage(constants.UnitTestID, snowtest.XChainID, addressedCall.Bytes())
	require.NoError(t, err, "avalancheWarp.NewUnsignedMessage()")

	vdrs := warptest.NewValidators(t, warptest.WithMinimum(2))
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	warptest.SetValidators(t, snowCtx, vdrs)
	predicateContext := &precompileconfig.PredicateContext{
		SnowCtx:            snowCtx,
		ProposerVMBlockCtx: &block.Context{},
	}
	return predicateContext,
		predicate.New(vdrs.Sign(t, msg).Bytes()),
		predicate.New(warptest.IncorrectlySign(t, msg).Bytes())
}

// newRules returns rules with Warp enabled.
func newRules() params.Rules {
	rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
	params.GetRulesExtra(rules).WarpConfig = warp.NewDefaultConfig(utils.PointerTo[uint64](0))
	return rules
}

func TestCheckBlockPredicates(t *testing.T) {
	predicateContext, validMsg, invalidMsg := newWarpMessages(t)
	rules := newRules()
	var (
		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: validMsg},
			},
			Gas: math.MaxUint64,
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: invalidMsg},
			},
			Gas: math.MaxUint64,
		})
	)
	tests := []struct {
		name                string
		nilPredicateContext bool
		txs                 []*types.Transaction
		want                predicate.BlockResults
		wantErr             error
	}{
		{
			name:                "nil_predicate_context",
			nilPredicateContext: true,
			txs:                 []*types.Transaction{validTx},
			wantErr:             ErrMissingPredicateContext,
		},
		{
			name: "one_tx_valid",
			txs:  []*types.Transaction{validTx},
			want: predicate.BlockResults{
				validTx.Hash(): {warp.ContractAddress: set.NewBits()},
			},
		},
		{
			name: "one_tx_invalid",
			txs:  []*types.Transaction{invalidTx},
			want: predicate.BlockResults{
				invalidTx.Hash(): {warp.ContractAddress: set.NewBits(0)},
			},
		},
		{
			name: "multiple_txs",
			txs:  []*types.Transaction{validTx, invalidTx},
			want: predicate.BlockResults{
				validTx.Hash():   {warp.ContractAddress: set.NewBits()},
				invalidTx.Hash(): {warp.ContractAddress: set.NewBits(0)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := predicateContext
			if tt.nilPredicateContext {
				ctx = nil
			}
			got, err := CheckBlockPredicates(rules, ctx, tt.txs)
			require.ErrorIs(t, err, tt.wantErr, "CheckBlockPredicates()")
			require.Equal(t, tt.want, got, "CheckBlockPredicates()")
		})
	}
}

func TestCheckTxPredicates(t *testing.T) {
	predicateContext, validMsg, invalidMsg := newWarpMessages(t)
	tests := []struct {
		name                string
		warpDisabled        bool
		nilBlockContext     bool
		nilPredicateContext bool
		insufficientGas     bool
		accessList          types.AccessList
		want                predicate.PrecompileResults
		wantErr             error
	}{
		{
			name:            "insufficient_intrinsic_gas",
			insufficientGas: true,
			accessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: validMsg},
			},
			wantErr: ErrIntrinsicGas,
		},
		{
			name:                "warp_disabled",
			warpDisabled:        true,
			nilPredicateContext: true,
			accessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: validMsg},
			},
		},
		{
			name:                "no_warp_messages",
			nilPredicateContext: true,
		},
		{
			name:                "filtered_access_list",
			nilPredicateContext: true,
			accessList: types.AccessList{
				{Address: common.Address{1}, StorageKeys: invalidMsg},
			},
		},
		{
			name:            "nil_block_context",
			nilBlockContext: true,
			accessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: validMsg},
			},
			wantErr: ErrMissingPredicateContext,
		},
		{
			name:                "nil_predicate_context",
			nilPredicateContext: true,
			accessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: validMsg},
			},
			wantErr: ErrMissingPredicateContext,
		},
		{
			name: "one_valid",
			accessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: validMsg},
			},
			want: predicate.PrecompileResults{
				warp.ContractAddress: set.NewBits(),
			},
		},
		{
			name: "one_invalid",
			accessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: invalidMsg},
			},
			want: predicate.PrecompileResults{
				warp.ContractAddress: set.NewBits(0),
			},
		},
		{
			name: "two_invalid",
			accessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: invalidMsg},
				{Address: warp.ContractAddress, StorageKeys: invalidMsg},
			},
			want: predicate.PrecompileResults{
				warp.ContractAddress: set.NewBits(0, 1),
			},
		},
		{
			name: "mixed_with_other_addresses",
			accessList: types.AccessList{
				{Address: warp.ContractAddress, StorageKeys: validMsg},
				{Address: common.Address{1}, StorageKeys: invalidMsg},
				{Address: warp.ContractAddress, StorageKeys: invalidMsg},
				{Address: warp.ContractAddress, StorageKeys: invalidMsg},
				{Address: warp.ContractAddress, StorageKeys: validMsg},
			},
			want: predicate.PrecompileResults{
				warp.ContractAddress: set.NewBits(1, 2),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := newRules()
			if tt.warpDisabled {
				params.GetRulesExtra(rules).WarpConfig = nil
			}

			c := *predicateContext
			ctx := &c
			if tt.nilBlockContext {
				ctx.ProposerVMBlockCtx = nil
			}
			if tt.nilPredicateContext {
				ctx = nil
			}

			gas := uint64(math.MaxUint64)
			if tt.insufficientGas {
				gas = 0
			}
			tx := types.NewTx(&types.DynamicFeeTx{
				AccessList: tt.accessList,
				Gas:        gas,
			})
			got, err := CheckTxPredicates(rules, ctx, tx)
			require.ErrorIs(t, err, tt.wantErr, "CheckTxPredicates()")
			require.Equal(t, tt.want, got, "CheckTxPredicates()")
		})
	}
}
