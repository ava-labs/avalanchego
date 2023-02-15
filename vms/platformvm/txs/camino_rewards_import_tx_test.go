// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestRewardsImportTxSyntacticVerify(t *testing.T) {
	ctx := snow.DefaultContextTest()
	ctx.AVAXAssetID = ids.GenerateTestID()
	otherOwner := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	tests := map[string]struct {
		tx          *RewardsImportTx
		expectedErr error
	}{
		"OK": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 2, *treasury.Owner, ids.Empty, ids.Empty),
				},
			}}},
		},
		"Zero outs": {
			tx:          &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{}}},
			expectedErr: errWrongOutsNumber,
		},
		"More than one out": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Outs: make([]*avax.TransferableOutput, 2),
			}}},
			expectedErr: errWrongOutsNumber,
		},
		"Out has wrong asset": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Outs: []*avax.TransferableOutput{
					generateTestOut(ids.GenerateTestID(), 1, *treasury.Owner, ids.Empty, ids.Empty),
				},
			}}},
			expectedErr: errNotAVAXAsset,
		},
		"Not secp output": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.GenerateTestID(), ids.Empty),
				},
			}}},
			expectedErr: locked.ErrWrongOutType,
		},
		"Output owner isn't treasury owner": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, otherOwner, ids.Empty, ids.Empty),
				},
			}}},
			expectedErr: errNotTreasuryOwner,
		},
		"Input has wrong asset": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ids.GenerateTestID(), 1, ids.Empty, ids.Empty, []uint32{}),
				},
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 2, *treasury.Owner, ids.Empty, ids.Empty),
				},
			}}},
			expectedErr: errNotAVAXAsset,
		},
		"Not secp input": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.GenerateTestID(), ids.Empty, []uint32{}),
				},
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
				},
			}}},
			expectedErr: locked.ErrWrongInType,
		},
		"Produced less than consumed": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
				},
			}}},
			expectedErr: errProducedNotEqualConsumed,
		},
		"Produced more than consumed": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 3, *treasury.Owner, ids.Empty, ids.Empty),
				},
			}}},
			expectedErr: errProducedNotEqualConsumed,
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
