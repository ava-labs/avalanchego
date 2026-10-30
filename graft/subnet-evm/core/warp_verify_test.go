// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/warptest"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// newWarpMessages returns a Warp message signed by the validators in the
// returned context, and the same message with an invalid signature.
func newWarpMessages(t *testing.T) (*warpmsg.VerifyContext, warpmsg.Packed, warpmsg.Packed) {
	t.Helper()

	addressedCall, err := payload.NewAddressedCall([]byte{1}, []byte{2})
	require.NoError(t, err, "payload.NewAddressedCall()")
	msg, err := avalancheWarp.NewUnsignedMessage(constants.UnitTestID, snowtest.XChainID, addressedCall.Bytes())
	require.NoError(t, err, "avalancheWarp.NewUnsignedMessage()")

	vdrs := warptest.NewValidators(t, warptest.WithMinimum(2))
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	warptest.SetValidators(t, snowCtx, vdrs)
	verifyCtx := &warpmsg.VerifyContext{
		SnowCtx:            snowCtx,
		ProposerVMBlockCtx: &block.Context{},
	}
	return verifyCtx,
		warpmsg.Pack(vdrs.Sign(t, msg).Bytes()),
		warpmsg.Pack(warptest.IncorrectlySign(t, msg).Bytes())
}

// newRules returns rules with Warp enabled.
func newRules() params.Rules {
	rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
	params.GetRulesExtra(rules).WarpConfig = warp.NewDefaultConfig(utils.PointerTo[uint64](0))
	return rules
}

func TestVerifyBlockWarpMessages(t *testing.T) {
	verifyCtx, validMsg, invalidMsg := newWarpMessages(t)
	rules := newRules()
	var (
		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validMsg},
			},
			Gas: math.MaxUint64,
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: invalidMsg},
			},
			Gas: math.MaxUint64,
		})
	)
	tests := []struct {
		name         string
		nilVerifyCtx bool
		txs          []*types.Transaction
		want         warpmsg.BlockResults
		wantErr      error
	}{
		{
			name:         "nil_verify_context",
			nilVerifyCtx: true,
			txs:          []*types.Transaction{validTx},
			wantErr:      ErrMissingVerifyContext,
		},
		{
			name: "one_tx_valid",
			txs:  []*types.Transaction{validTx},
			want: warpmsg.BlockResults{
				validTx.Hash(): set.NewBits(),
			},
		},
		{
			name: "one_tx_invalid",
			txs:  []*types.Transaction{invalidTx},
			want: warpmsg.BlockResults{
				invalidTx.Hash(): set.NewBits(0),
			},
		},
		{
			name: "multiple_txs",
			txs:  []*types.Transaction{validTx, invalidTx},
			want: warpmsg.BlockResults{
				validTx.Hash():   set.NewBits(),
				invalidTx.Hash(): set.NewBits(0),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := verifyCtx
			if tt.nilVerifyCtx {
				ctx = nil
			}
			got, err := VerifyBlockWarpMessages(rules, ctx, tt.txs)
			require.ErrorIs(t, err, tt.wantErr, "VerifyBlockWarpMessages()")
			require.Equal(t, tt.want, got, "VerifyBlockWarpMessages()")
		})
	}
}

func TestVerifyTxWarpMessages(t *testing.T) {
	verifyCtx, validMsg, invalidMsg := newWarpMessages(t)
	tests := []struct {
		name            string
		warpDisabled    bool
		nilBlockContext bool
		nilVerifyCtx    bool
		insufficientGas bool
		accessList      types.AccessList
		want            set.Bits
		wantOK          bool
		wantErr         error
	}{
		{
			name:            "insufficient_intrinsic_gas",
			insufficientGas: true,
			accessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validMsg},
			},
			wantErr: ErrIntrinsicGas,
		},
		{
			name:         "warp_disabled",
			warpDisabled: true,
			nilVerifyCtx: true,
			accessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validMsg},
			},
		},
		{
			name:         "no_warp_messages",
			nilVerifyCtx: true,
		},
		{
			name:         "filtered_access_list",
			nilVerifyCtx: true,
			accessList: types.AccessList{
				{Address: common.Address{1}, StorageKeys: invalidMsg},
			},
		},
		{
			name:            "nil_block_context",
			nilBlockContext: true,
			accessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validMsg},
			},
			wantErr: ErrMissingVerifyContext,
		},
		{
			name:         "nil_verify_context",
			nilVerifyCtx: true,
			accessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validMsg},
			},
			wantErr: ErrMissingVerifyContext,
		},
		{
			name: "one_valid",
			accessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validMsg},
			},
			want:   set.NewBits(),
			wantOK: true,
		},
		{
			name: "one_invalid",
			accessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: invalidMsg},
			},
			want:   set.NewBits(0),
			wantOK: true,
		},
		{
			name: "two_invalid",
			accessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: invalidMsg},
				{Address: warpmsg.ContractAddress, StorageKeys: invalidMsg},
			},
			want:   set.NewBits(0, 1),
			wantOK: true,
		},
		{
			name: "mixed_with_other_addresses",
			accessList: types.AccessList{
				{Address: warpmsg.ContractAddress, StorageKeys: validMsg},
				{Address: common.Address{1}, StorageKeys: invalidMsg},
				{Address: warpmsg.ContractAddress, StorageKeys: invalidMsg},
				{Address: warpmsg.ContractAddress, StorageKeys: invalidMsg},
				{Address: warpmsg.ContractAddress, StorageKeys: validMsg},
			},
			want:   set.NewBits(1, 2),
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := newRules()
			if tt.warpDisabled {
				params.GetRulesExtra(rules).WarpConfig = nil
			}

			c := *verifyCtx
			ctx := &c
			if tt.nilBlockContext {
				ctx.ProposerVMBlockCtx = nil
			}
			if tt.nilVerifyCtx {
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
			got, gotOK, err := VerifyTxWarpMessages(rules, ctx, tx)
			require.ErrorIs(t, err, tt.wantErr, "VerifyTxWarpMessages()")
			assert.Equal(t, tt.want, got, "VerifyTxWarpMessages()")
			assert.Equal(t, tt.wantOK, gotOK, "VerifyTxWarpMessages() ok")
		})
	}
}
