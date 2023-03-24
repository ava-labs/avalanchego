// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestClaimTxSyntacticVerify(t *testing.T) {
	ctx := snow.DefaultContextTest()
	ctx.AVAXAssetID = ids.GenerateTestID()
	owner1 := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	depositTxID := ids.GenerateTestID()
	claimableOwnerID := ids.GenerateTestID()

	baseTx := BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
	}}

	tests := map[string]struct {
		tx          *ClaimTx
		expectedErr error
	}{
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"No depositTxs and claimableOwnerIDs": {
			tx: &ClaimTx{
				BaseTx: baseTx,
			},
			expectedErr: errNoDepositsOrClaimables,
		},
		"Claimed amounts len not equal to claimable owner ids len": {
			tx: &ClaimTx{
				BaseTx:            baseTx,
				ClaimableOwnerIDs: []ids.ID{claimableOwnerID},
				ClaimedAmounts:    []uint64{1, 2},
			},
			expectedErr: errWrongClaimedAmount,
		},
		"Not unique depositTxs": {
			tx: &ClaimTx{
				BaseTx:       baseTx,
				DepositTxIDs: []ids.ID{depositTxID, depositTxID},
			},
			expectedErr: errNonUniqueDepositTxID,
		},
		"Not unique ownerIDs": {
			tx: &ClaimTx{
				BaseTx:            baseTx,
				ClaimableOwnerIDs: []ids.ID{claimableOwnerID, claimableOwnerID},
				ClaimedAmounts:    []uint64{1, 1},
			},
			expectedErr: errNonUniqueOwnerID,
		},
		"Locked base tx input": {
			tx: &ClaimTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generateTestIn(ctx.AVAXAssetID, 1, ids.GenerateTestID(), ids.Empty, []uint32{0}),
					},
				}},
				DepositTxIDs: []ids.ID{depositTxID},
				ClaimTo:      &owner1,
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Locked base tx output": {
			tx: &ClaimTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.GenerateTestID(), ids.Empty),
					},
				}},
				DepositTxIDs: []ids.ID{depositTxID},
				ClaimTo:      &owner1,
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"OK with deposits": {
			tx: &ClaimTx{
				BaseTx:       baseTx,
				DepositTxIDs: []ids.ID{depositTxID},
				ClaimTo:      &owner1,
			},
		},
		"OK with claimables": {
			tx: &ClaimTx{
				BaseTx:            baseTx,
				ClaimTo:           &owner1,
				ClaimableOwnerIDs: []ids.ID{claimableOwnerID},
				ClaimedAmounts:    []uint64{20},
			},
		},
		"OK with claimables and deposits": {
			tx: &ClaimTx{
				BaseTx:            baseTx,
				DepositTxIDs:      []ids.ID{depositTxID},
				ClaimTo:           &owner1,
				ClaimableOwnerIDs: []ids.ID{claimableOwnerID},
				ClaimedAmounts:    []uint64{20},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
