// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/stretchr/testify/require"
)

func TestRewardsImportTxSyntacticVerify(t *testing.T) {
	ctx := snow.DefaultContextTest()
	ctx.AVAXAssetID = ids.GenerateTestID()

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
			}}},
		},
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"Input has wrong asset": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ids.GenerateTestID(), 1, ids.Empty, ids.Empty, []uint32{}),
				},
			}}},
			expectedErr: errNotAVAXAsset,
		},
		"Not secp input": {
			tx: &RewardsImportTx{BaseTx: BaseTx{BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.GenerateTestID(), ids.Empty, []uint32{}),
				},
			}}},
			expectedErr: locked.ErrWrongInType,
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
