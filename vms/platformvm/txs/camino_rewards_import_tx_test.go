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
			tx: &RewardsImportTx{
				Out: generateTestOut(ctx.AVAXAssetID, 2, *treasury.Owner, ids.Empty, ids.Empty),
				ImportedInputs: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
			},
		},
		"Nil tx": {
			tx:          nil,
			expectedErr: ErrNilTx,
		},
		"Not secp output": {
			tx: &RewardsImportTx{
				Out: generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.GenerateTestID(), ids.Empty),
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"Output owner isn't treasury owner": {
			tx: &RewardsImportTx{
				Out: generateTestOut(ctx.AVAXAssetID, 1, otherOwner, ids.Empty, ids.Empty),
			},
			expectedErr: errNotTreasuryOwner,
		},
		"Not secp input": {
			tx: &RewardsImportTx{
				Out: generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
				ImportedInputs: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.GenerateTestID(), ids.Empty, []uint32{}),
				},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Produced less than consumed": {
			tx: &RewardsImportTx{
				Out: generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
				ImportedInputs: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
			},
			expectedErr: errProducedNotEqualConsumed,
		},
		"Produced more than consumed": {
			tx: &RewardsImportTx{
				Out: generateTestOut(ctx.AVAXAssetID, 3, *treasury.Owner, ids.Empty, ids.Empty),
				ImportedInputs: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
			},
			expectedErr: errProducedNotEqualConsumed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.tx != nil {
				avax.SortTransferableInputs(tt.tx.ImportedInputs)
			}
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
