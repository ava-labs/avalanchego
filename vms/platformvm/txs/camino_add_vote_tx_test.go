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
	"github.com/stretchr/testify/require"
)

func TestAddVoteTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()
	owner1 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{0, 0, 1}}}

	badVote := &VoteWrapper{Vote: &dac.DummyVote{ErrorStr: "test errr"}}
	badVoteBytes, err := Codec.Marshal(Version, badVote)
	require.NoError(t, err)

	vote := &VoteWrapper{Vote: &dac.DummyVote{}}
	voteBytes, err := Codec.Marshal(Version, vote)
	require.NoError(t, err)

	baseTx := BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
	}}

	tests := map[string]struct {
		tx          *AddVoteTx
		expectedErr error
	}{
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"Fail to unmarshal vote": {
			tx: &AddVoteTx{
				BaseTx:      baseTx,
				VotePayload: []byte{},
			},
			expectedErr: errBadVote,
		},
		"Bad vote": {
			tx: &AddVoteTx{
				BaseTx:      baseTx,
				VotePayload: badVoteBytes,
			},
			expectedErr: errBadVote,
		},
		"Bad voter auth": {
			tx: &AddVoteTx{
				BaseTx:      baseTx,
				VotePayload: voteBytes,
				VoterAuth:   (*secp256k1fx.Input)(nil),
			},
			expectedErr: errBadVoterAuth,
		},
		"Locked base tx input": {
			tx: &AddVoteTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generateTestIn(ctx.AVAXAssetID, 1, ids.ID{1}, ids.Empty, []uint32{0}),
					},
				}},
				VotePayload: voteBytes,
				VoterAuth:   &secp256k1fx.Input{},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Locked base tx output": {
			tx: &AddVoteTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.ID{1}, ids.Empty),
					},
				}},
				VotePayload: voteBytes,
				VoterAuth:   &secp256k1fx.Input{},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"Stakable base tx input": {
			tx: &AddVoteTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generateTestStakeableIn(ctx.AVAXAssetID, 1, 1, []uint32{0}),
					},
				}},
				VotePayload: voteBytes,
				VoterAuth:   &secp256k1fx.Input{},
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakable base tx output": {
			tx: &AddVoteTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generateTestStakeableOut(ctx.AVAXAssetID, 1, 1, owner1),
					},
				}},
				VotePayload: voteBytes,
				VoterAuth:   &secp256k1fx.Input{},
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"OK": {
			tx: &AddVoteTx{
				BaseTx:      baseTx,
				VotePayload: voteBytes,
				VoterAuth:   &secp256k1fx.Input{},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}

func TestAddVoteTxVote(t *testing.T) {
	expectedVote := &VoteWrapper{Vote: &dac.DummyVote{ErrorStr: "some data"}}
	voteBytes, err := Codec.Marshal(Version, expectedVote)
	require.NoError(t, err)

	tx := &AddVoteTx{VotePayload: voteBytes}
	txVote, err := tx.Vote()
	require.NoError(t, err)
	require.Equal(t, expectedVote.Vote, txVote)
}
