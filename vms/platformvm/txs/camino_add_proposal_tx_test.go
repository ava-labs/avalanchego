// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
	"github.com/stretchr/testify/require"
)

func TestAddProposalTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()
	owner1 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{0, 0, 1}}}

	badProposal := &ProposalWrapper{Proposal: &dac.BaseFeeProposal{Options: []uint64{}}}
	badProposalBytes, err := Codec.Marshal(Version, badProposal)
	require.NoError(t, err)

	proposal := &ProposalWrapper{Proposal: &dac.BaseFeeProposal{
		End:     1,
		Options: []uint64{1},
	}}
	proposalBytes, err := Codec.Marshal(Version, proposal)
	require.NoError(t, err)

	baseTx := BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
	}}

	tests := map[string]struct {
		tx          *AddProposalTx
		expectedErr error
	}{
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"Too big proposal description": {
			tx: &AddProposalTx{
				ProposalDescription: make(types.JSONByteSlice, maxProposalDescriptionSize+1),
			},
			expectedErr: errTooBigProposalDescription,
		},
		"Fail to unmarshal proposal": {
			tx: &AddProposalTx{
				BaseTx:          baseTx,
				ProposalPayload: []byte{},
			},
			expectedErr: errBadProposal,
		},
		"Bad proposal": {
			tx: &AddProposalTx{
				BaseTx:          baseTx,
				ProposalPayload: badProposalBytes,
			},
			expectedErr: errBadProposal,
		},
		"Bad proposer auth": {
			tx: &AddProposalTx{
				BaseTx:          baseTx,
				ProposalPayload: proposalBytes,
				ProposerAuth:    (*secp256k1fx.Input)(nil),
			},
			expectedErr: errBadProposerAuth,
		},
		"Stakable base tx input": {
			tx: &AddProposalTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generateTestStakeableIn(ctx.AVAXAssetID, 1, 1, []uint32{0}),
					},
				}},
				ProposalPayload: proposalBytes,
				ProposerAuth:    &secp256k1fx.Input{},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakable base tx output": {
			tx: &AddProposalTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generateTestStakeableOut(ctx.AVAXAssetID, 1, 1, owner1),
					},
				}},
				ProposalPayload: proposalBytes,
				ProposerAuth:    &secp256k1fx.Input{},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"OK: no proposal description": {
			tx: &AddProposalTx{
				BaseTx:          baseTx,
				ProposalPayload: proposalBytes,
				ProposerAuth:    &secp256k1fx.Input{},
			},
		},
		"OK": {
			tx: &AddProposalTx{
				BaseTx:              baseTx,
				ProposalDescription: make(types.JSONByteSlice, maxProposalDescriptionSize),
				ProposalPayload:     proposalBytes,
				ProposerAuth:        &secp256k1fx.Input{},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}

func TestAddProposalTxProposal(t *testing.T) {
	expectedProposal := &ProposalWrapper{Proposal: &dac.BaseFeeProposal{
		Start: 11, End: 12,
		Options: []uint64{555, 123, 7},
	}}
	proposalBytes, err := Codec.Marshal(Version, expectedProposal)
	require.NoError(t, err)

	tx := &AddProposalTx{
		ProposalPayload: proposalBytes,
	}
	txProposal, err := tx.Proposal()
	require.NoError(t, err)
	require.Equal(t, expectedProposal.Proposal, txProposal)
}
