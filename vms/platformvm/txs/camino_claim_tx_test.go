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

func TestClaimTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()
	owner1 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{0, 0, 1}}}
	depositTxID := ids.ID{0, 1}
	claimableOwnerID1 := ids.ID{0, 2}
	claimableOwnerID2 := ids.ID{0, 3}
	claimableOwnerID3 := ids.ID{0, 4}

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
		"No claimables": {
			tx: &ClaimTx{
				BaseTx: baseTx,
			},
			expectedErr: errNoClaimables,
		},
		"Zero claimed amount": {
			tx: &ClaimTx{
				BaseTx: baseTx,
				Claimables: []ClaimAmount{{
					ID:        claimableOwnerID1,
					Type:      ClaimTypeExpiredDepositReward,
					OwnerAuth: &secp256k1fx.Input{},
				}},
			},
			expectedErr: errZeroClaimedAmount,
		},
		"Not unique claimable id": {
			tx: &ClaimTx{
				BaseTx: baseTx,
				Claimables: []ClaimAmount{
					{
						ID:        claimableOwnerID1,
						Type:      ClaimTypeExpiredDepositReward,
						Amount:    1,
						OwnerAuth: &secp256k1fx.Input{},
					},
					{
						ID:        claimableOwnerID1,
						Type:      ClaimTypeValidatorReward,
						Amount:    2,
						OwnerAuth: &secp256k1fx.Input{},
					},
				},
			},
			expectedErr: errNonUniqueClaimableID,
		},
		"Wrong claim type": {
			tx: &ClaimTx{
				BaseTx: baseTx,
				Claimables: []ClaimAmount{{
					ID:        claimableOwnerID1,
					Type:      ClaimTypeActiveDepositReward + 1,
					Amount:    1,
					OwnerAuth: &secp256k1fx.Input{},
				}},
			},
			expectedErr: ErrWrongClaimType,
		},
		"Bad claimable auth": {
			tx: &ClaimTx{
				BaseTx: baseTx,
				Claimables: []ClaimAmount{{
					ID:        claimableOwnerID1,
					Type:      ClaimTypeActiveDepositReward,
					Amount:    1,
					OwnerAuth: (*secp256k1fx.Input)(nil),
				}},
			},
			expectedErr: errBadClaimableAuth,
		},
		"Locked base tx input": {
			tx: &ClaimTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.In(ctx.AVAXAssetID, 1, depositTxID, ids.Empty, []uint32{0}),
					},
				}},
				Claimables: []ClaimAmount{{
					Type:      ClaimTypeAllTreasury,
					Amount:    1,
					OwnerAuth: &secp256k1fx.Input{},
				}},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Locked base tx output": {
			tx: &ClaimTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generate.Out(ctx.AVAXAssetID, 1, owner1, depositTxID, ids.Empty),
					},
				}},
				Claimables: []ClaimAmount{{
					Type:      ClaimTypeAllTreasury,
					Amount:    1,
					OwnerAuth: &secp256k1fx.Input{},
				}},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"Stakable base tx input": {
			tx: &ClaimTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.StakeableIn(ctx.AVAXAssetID, 1, 1, []uint32{0}),
					},
				}},
				Claimables: []ClaimAmount{{
					Type:      ClaimTypeAllTreasury,
					Amount:    1,
					OwnerAuth: &secp256k1fx.Input{},
				}},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakable base tx output": {
			tx: &ClaimTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generate.StakeableOut(ctx.AVAXAssetID, 1, 1, owner1),
					},
				}},
				Claimables: []ClaimAmount{{
					Type:      ClaimTypeAllTreasury,
					Amount:    1,
					OwnerAuth: &secp256k1fx.Input{},
				}},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"OK": {
			tx: &ClaimTx{
				BaseTx: baseTx,
				Claimables: []ClaimAmount{
					{
						ID:        depositTxID,
						Type:      ClaimTypeActiveDepositReward,
						Amount:    1,
						OwnerAuth: &secp256k1fx.Input{},
					},
					{
						ID:        claimableOwnerID1,
						Type:      ClaimTypeExpiredDepositReward,
						Amount:    1,
						OwnerAuth: &secp256k1fx.Input{},
					},
					{
						ID:        claimableOwnerID2,
						Type:      ClaimTypeValidatorReward,
						Amount:    1,
						OwnerAuth: &secp256k1fx.Input{},
					},
					{
						ID:        claimableOwnerID3,
						Type:      ClaimTypeAllTreasury,
						Amount:    1,
						OwnerAuth: &secp256k1fx.Input{},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
