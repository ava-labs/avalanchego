// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/generate"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestUnlockDepositTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()

	tests := map[string]struct {
		tx          *UnlockDepositTx
		expectedErr error
	}{
		"OK": {
			tx: &UnlockDepositTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				Ins: []*avax.TransferableInput{
					generate.In(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generate.In(ctx.AVAXAssetID, 1, ids.ID{1}, ids.Empty, []uint32{}),
				},
				Outs: []*avax.TransferableOutput{},
			}}},
		},
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"Stakable input": {
			tx: &UnlockDepositTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				Ins: []*avax.TransferableInput{
					generate.StakeableIn(ctx.AVAXAssetID, 1, 1, []uint32{0}),
				},
			}}},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakable output": {
			tx: &UnlockDepositTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				Outs: []*avax.TransferableOutput{
					generate.StakeableOut(ctx.AVAXAssetID, 1, 1, secp256k1fx.OutputOwners{}),
				},
			}}},
			expectedErr: locked.ErrWrongOutType,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.tx != nil {
				avax.SortTransferableInputs(tt.tx.Ins)
			}
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
