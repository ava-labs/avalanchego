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

func TestRegisterNodeTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()
	owner1 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{0, 1}}}
	depositTxID := ids.ID{1}

	baseTx := BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
	}}

	tests := map[string]struct {
		tx          *RegisterNodeTx
		expectedErr error
	}{
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"Old and new nodeIDs empty": {
			tx:          &RegisterNodeTx{BaseTx: baseTx},
			expectedErr: errNoNodeID,
		},
		"Consortium member address empty": {
			tx: &RegisterNodeTx{
				BaseTx:    baseTx,
				OldNodeID: ids.NodeID{1},
				NewNodeID: ids.NodeID{2},
			},
			expectedErr: errConsortiumMemberAddrEmpty,
		},
		"Locked base tx input": {
			tx: &RegisterNodeTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.In(ctx.AVAXAssetID, 1, depositTxID, ids.Empty, []uint32{0}),
					},
				}},
				OldNodeID:        ids.NodeID{1},
				NewNodeID:        ids.NodeID{2},
				NodeOwnerAddress: ids.ShortID{3},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Locked base tx output": {
			tx: &RegisterNodeTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generate.Out(ctx.AVAXAssetID, 1, owner1, depositTxID, ids.Empty),
					},
				}},
				OldNodeID:        ids.NodeID{1},
				NewNodeID:        ids.NodeID{2},
				NodeOwnerAddress: ids.ShortID{3},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"Stakable base tx input": {
			tx: &RegisterNodeTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.StakeableIn(ctx.AVAXAssetID, 1, 0, []uint32{0}),
					},
				}},
				OldNodeID:        ids.NodeID{1},
				NewNodeID:        ids.NodeID{2},
				NodeOwnerAddress: ids.ShortID{3},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakable base tx output": {
			tx: &RegisterNodeTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generate.StakeableOut(ctx.AVAXAssetID, 1, 0, owner1),
					},
				}},
				OldNodeID:        ids.NodeID{1},
				NewNodeID:        ids.NodeID{2},
				NodeOwnerAddress: ids.ShortID{3},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"Bad consortium member auth": {
			tx: &RegisterNodeTx{
				BaseTx:           baseTx,
				OldNodeID:        ids.NodeID{1},
				NewNodeID:        ids.NodeID{2},
				NodeOwnerAddress: ids.ShortID{3},
				NodeOwnerAuth:    (*secp256k1fx.Input)(nil),
			},
			expectedErr: errBadConsortiumMemberAuth,
		},
		"OK": {
			tx: &RegisterNodeTx{
				BaseTx:           baseTx,
				OldNodeID:        ids.NodeID{1},
				NewNodeID:        ids.NodeID{2},
				NodeOwnerAuth:    &secp256k1fx.Input{},
				NodeOwnerAddress: ids.ShortID{3},
			},
		},
		"OK: old nodeID empty": {
			tx: &RegisterNodeTx{
				BaseTx:           baseTx,
				OldNodeID:        ids.EmptyNodeID,
				NewNodeID:        ids.NodeID{1},
				NodeOwnerAuth:    &secp256k1fx.Input{},
				NodeOwnerAddress: ids.ShortID{2},
			},
		},
		"OK: new nodeID empty": {
			tx: &RegisterNodeTx{
				BaseTx:           baseTx,
				OldNodeID:        ids.NodeID{1},
				NewNodeID:        ids.EmptyNodeID,
				NodeOwnerAuth:    &secp256k1fx.Input{},
				NodeOwnerAddress: ids.ShortID{2},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
