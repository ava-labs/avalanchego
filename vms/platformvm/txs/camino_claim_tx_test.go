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

func TestClaimTxSyntacticVerify(t *testing.T) {
	ctx := snow.DefaultContextTest()
	ctx.AVAXAssetID = ids.GenerateTestID()
	owner1 := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	owner2 := secp256k1fx.OutputOwners{
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
		tx                    *ClaimTx
		dontSortClaimableIns  bool
		dontSortClaimableOuts bool
		expectedErr           error
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
				ClaimedAmount:     []uint64{1, 2},
			},
			expectedErr: errWrongClaimedAmount,
		},
		"Claimed amount is 0": {
			tx: &ClaimTx{
				BaseTx:            baseTx,
				ClaimableOwnerIDs: []ids.ID{claimableOwnerID},
				ClaimedAmount:     []uint64{0},
			},
			expectedErr: errWrongClaimedAmount,
		},
		"No claimble owner ids, but have claimable inputs": {
			tx: &ClaimTx{
				BaseTx:     baseTx,
				DepositTxs: []ids.ID{depositTxID},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
			},
			expectedErr: errWrongClaimedAmount,
		},
		"Not unique depositTxs": {
			tx: &ClaimTx{
				BaseTx:     baseTx,
				DepositTxs: []ids.ID{depositTxID, depositTxID},
			},
			expectedErr: errNonUniqueDepositTxID,
		},
		"Not unique ownerIDs": {
			tx: &ClaimTx{
				BaseTx:            baseTx,
				ClaimableOwnerIDs: []ids.ID{claimableOwnerID, claimableOwnerID},
				ClaimedAmount:     []uint64{1, 1},
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
				DepositTxs:          []ids.ID{depositTxID},
				DepositRewardsOwner: &owner1,
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
				DepositTxs:          []ids.ID{depositTxID},
				DepositRewardsOwner: &owner1,
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"Claimable input has wrong asset": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ids.GenerateTestID(), 1, ids.Empty, ids.Empty, []uint32{}),
				},
			},
			expectedErr: errNotAVAXAsset,
		},
		"Claimable output has wrong asset": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ids.GenerateTestID(), 1, owner1, ids.Empty, ids.Empty),
				},
			},
			expectedErr: errNotAVAXAsset,
		},
		"Multiple treasury outs": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 2, ids.Empty, ids.Empty, []uint32{}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
				},
			},
			expectedErr: errMultipleTreasuryOuts,
		},
		"Claimed amount is less than produced claimed amount": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 3, ids.Empty, ids.Empty, []uint32{}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 2, owner1, ids.Empty, ids.Empty),
				},
			},
			expectedErr: errWrongProducedClaimedAmount,
		},
		"Claimed amount is greater than produced claimed amount": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{3},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 2, ids.Empty, ids.Empty, []uint32{}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
				},
			},
			expectedErr: errWrongProducedClaimedAmount,
		},
		"Consumed from treasury less than produced": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 2, *treasury.Owner, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
				},
			},
			expectedErr: errProducedNotEqualConsumed,
		},
		"Consumed from treasury more than produced": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
				},
			},
			expectedErr: errProducedNotEqualConsumed,
		},
		"Claimable inputs not sorted": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{TxID: ids.ID{2}},
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.ID{1}},
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{}},
						},
					},
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
				},
			},
			dontSortClaimableIns: true,
			expectedErr:          errInputsNotSortedUnique,
		},
		"Claimable inputs not unique": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{TxID: ids.ID{1}},
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{}},
						},
					},
					{
						UTXOID: avax.UTXOID{TxID: ids.ID{1}},
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{}},
						},
					},
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
				},
			},
			expectedErr: errInputsNotSortedUnique,
		},
		"Claimable outputs not sorted": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 3, ids.Empty, ids.Empty, []uint32{0}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          2,
							OutputOwners: *treasury.Owner,
						},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          1,
							OutputOwners: owner1,
						},
					},
				},
			},
			dontSortClaimableOuts: true,
			expectedErr:           errOutputsNotSorted,
		},
		"Locked claimable input": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.GenerateTestID(), ids.Empty, []uint32{0}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
				},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Locked claimable output": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{1},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{0}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.GenerateTestID(), ids.Empty),
				},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"OK with deposits": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositTxs:          []ids.ID{depositTxID},
				DepositRewardsOwner: &owner1,
			},
		},
		"OK with claimables": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{20},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 10, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 10, ids.Empty, ids.Empty, []uint32{}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 12, owner1, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 8, owner2, ids.Empty, ids.Empty),
				},
			},
		},
		"OK with claimables and deposits": {
			tx: &ClaimTx{
				BaseTx:              baseTx,
				DepositTxs:          []ids.ID{depositTxID},
				DepositRewardsOwner: &owner1,
				ClaimableOwnerIDs:   []ids.ID{claimableOwnerID},
				ClaimedAmount:       []uint64{20},
				ClaimableIns: []*avax.TransferableInput{
					generateTestIn(ctx.AVAXAssetID, 10, ids.Empty, ids.Empty, []uint32{}),
					generateTestIn(ctx.AVAXAssetID, 10, ids.Empty, ids.Empty, []uint32{}),
				},
				ClaimableOuts: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 12, owner1, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 8, owner2, ids.Empty, ids.Empty),
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.tx != nil {
				if !tt.dontSortClaimableIns {
					avax.SortTransferableInputs(tt.tx.ClaimableIns)
				}
				if !tt.dontSortClaimableOuts {
					avax.SortTransferableOutputs(tt.tx.ClaimableOuts, Codec)
				}
			}
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
