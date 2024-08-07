// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
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

func TestFinishProposalsTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()
	owner1 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{0, 0, 1}}}

	proposalID1 := ids.ID{1}
	proposalID2 := ids.ID{2}
	proposalID3 := ids.ID{3}
	proposalID4 := ids.ID{4}
	proposalID5 := ids.ID{5}
	proposalID6 := ids.ID{6}
	proposalID7 := ids.ID{7}
	proposalID8 := ids.ID{8}

	baseTx := BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
	}}

	tests := map[string]struct {
		tx          *FinishProposalsTx
		expectedErr error
	}{
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"No proposals": {
			tx: &FinishProposalsTx{
				BaseTx: baseTx,
			},
			expectedErr: errNoFinishedProposals,
		},
		"Not sorted proposals in EarlyFinishedSuccessfulProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                             baseTx,
				EarlyFinishedSuccessfulProposalIDs: []ids.ID{proposalID2, proposalID1},
			},
			expectedErr: errNotSortedOrUniqueProposalIDs,
		},
		"Not sorted proposals in EarlyFinishedFailedProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                         baseTx,
				EarlyFinishedFailedProposalIDs: []ids.ID{proposalID2, proposalID1},
			},
			expectedErr: errNotSortedOrUniqueProposalIDs,
		},
		"Not sorted proposals in ExpiredSuccessfulProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                       baseTx,
				ExpiredSuccessfulProposalIDs: []ids.ID{proposalID2, proposalID1},
			},
			expectedErr: errNotSortedOrUniqueProposalIDs,
		},
		"Not sorted proposals in ExpiredFailedProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                   baseTx,
				ExpiredFailedProposalIDs: []ids.ID{proposalID2, proposalID1},
			},
			expectedErr: errNotSortedOrUniqueProposalIDs,
		},
		"Not unique proposals in EarlyFinishedSuccessfulProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                             baseTx,
				EarlyFinishedSuccessfulProposalIDs: []ids.ID{proposalID1, proposalID1},
			},
			expectedErr: errNotSortedOrUniqueProposalIDs,
		},
		"Not unique proposals in EarlyFinishedFailedProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                         baseTx,
				EarlyFinishedFailedProposalIDs: []ids.ID{proposalID1, proposalID1},
			},
			expectedErr: errNotSortedOrUniqueProposalIDs,
		},
		"Not unique proposals in ExpiredSuccessfulProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                       baseTx,
				ExpiredSuccessfulProposalIDs: []ids.ID{proposalID1, proposalID1},
			},
			expectedErr: errNotSortedOrUniqueProposalIDs,
		},
		"Not unique proposals in ExpiredFailedProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                   baseTx,
				ExpiredFailedProposalIDs: []ids.ID{proposalID1, proposalID1},
			},
			expectedErr: errNotSortedOrUniqueProposalIDs,
		},
		"Not unique proposals in EarlyFinishedSuccessfulProposalIDs and EarlyFinishedFailedProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                             baseTx,
				EarlyFinishedSuccessfulProposalIDs: []ids.ID{proposalID1},
				EarlyFinishedFailedProposalIDs:     []ids.ID{proposalID1},
			},
			expectedErr: errNotUniqueProposalID,
		},
		"Not unique proposals in EarlyFinishedSuccessfulProposalIDs and ExpiredSuccessfulProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                             baseTx,
				EarlyFinishedSuccessfulProposalIDs: []ids.ID{proposalID1},
				ExpiredSuccessfulProposalIDs:       []ids.ID{proposalID1},
			},
			expectedErr: errNotUniqueProposalID,
		},
		"Not unique proposals in EarlyFinishedSuccessfulProposalIDs and ExpiredFailedProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                             baseTx,
				EarlyFinishedSuccessfulProposalIDs: []ids.ID{proposalID1},
				ExpiredFailedProposalIDs:           []ids.ID{proposalID1},
			},
			expectedErr: errNotUniqueProposalID,
		},
		"Not unique proposals in EarlyFinishedFailedProposalIDs and ExpiredSuccessfulProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                         baseTx,
				EarlyFinishedFailedProposalIDs: []ids.ID{proposalID1},
				ExpiredSuccessfulProposalIDs:   []ids.ID{proposalID1},
			},
			expectedErr: errNotUniqueProposalID,
		},
		"Not unique proposals in EarlyFinishedFailedProposalIDs and ExpiredFailedProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                         baseTx,
				EarlyFinishedFailedProposalIDs: []ids.ID{proposalID1},
				ExpiredFailedProposalIDs:       []ids.ID{proposalID1},
			},
			expectedErr: errNotUniqueProposalID,
		},
		"Not unique proposals in ExpiredSuccessfulProposalIDs and ExpiredFailedProposalIDs": {
			tx: &FinishProposalsTx{
				BaseTx:                       baseTx,
				ExpiredSuccessfulProposalIDs: []ids.ID{proposalID1},
				ExpiredFailedProposalIDs:     []ids.ID{proposalID1},
			},
			expectedErr: errNotUniqueProposalID,
		},
		"Stakable base tx input": {
			tx: &FinishProposalsTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.StakeableIn(ctx.AVAXAssetID, 1, 1, []uint32{0}),
					},
				}},
				EarlyFinishedSuccessfulProposalIDs: []ids.ID{proposalID1},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakable base tx output": {
			tx: &FinishProposalsTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generate.StakeableOut(ctx.AVAXAssetID, 1, 1, owner1),
					},
				}},
				EarlyFinishedSuccessfulProposalIDs: []ids.ID{proposalID1},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"OK": {
			tx: &FinishProposalsTx{
				BaseTx:                             baseTx,
				EarlyFinishedSuccessfulProposalIDs: []ids.ID{proposalID1, proposalID2},
				EarlyFinishedFailedProposalIDs:     []ids.ID{proposalID3, proposalID4},
				ExpiredSuccessfulProposalIDs:       []ids.ID{proposalID5, proposalID6},
				ExpiredFailedProposalIDs:           []ids.ID{proposalID7, proposalID8},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
