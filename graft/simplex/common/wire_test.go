// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common_test

import (
	"encoding/asn1"
	"testing"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/graft/simplex/testutil"

	"github.com/stretchr/testify/require"
)

type testQCDeserializer struct{}

func (testQCDeserializer) DeserializeQuorumCertificate(b []byte) (common.QuorumCertificate, error) {
	var qc []common.Signature
	if _, err := asn1.Unmarshal(b, &qc); err != nil {
		return nil, err
	}
	return testutil.TestQC(qc), nil
}

// toVerified converts the inbound (unverified) message variants into their
// outbound (verified) counterparts so a decoded message can be re-encoded.
func toVerified(t *testing.T, msg *common.Message) *common.Message {
	toVerifiedQR := func(qr *common.QuorumRound) *common.VerifiedQuorumRound {
		if qr == nil {
			return nil
		}
		vqr := &common.VerifiedQuorumRound{
			Notarization:      qr.Notarization,
			Finalization:      qr.Finalization,
			EmptyNotarization: qr.EmptyNotarization,
		}
		if qr.Block != nil {
			vqr.VerifiedBlock = qr.Block.(common.VerifiedBlock)
		}
		return vqr
	}

	switch {
	case msg.BlockMessage != nil:
		block, ok := msg.BlockMessage.Block.(common.VerifiedBlock)
		require.True(t, ok)
		return &common.Message{
			VerifiedBlockMessage: &common.VerifiedBlockMessage{
				VerifiedBlock: block,
				Vote:          msg.BlockMessage.Vote,
			},
		}
	case msg.ReplicationResponse != nil:
		resp := &common.VerifiedReplicationResponse{
			LatestRound:        toVerifiedQR(msg.ReplicationResponse.LatestRound),
			LatestFinalizedSeq: toVerifiedQR(msg.ReplicationResponse.LatestSeq),
		}
		for i := range msg.ReplicationResponse.Data {
			resp.Data = append(resp.Data, *toVerifiedQR(&msg.ReplicationResponse.Data[i]))
		}
		return &common.Message{VerifiedReplicationResponse: resp}
	default:
		return msg
	}
}

func TestEncodeDecodeMessage(t *testing.T) {
	md := common.ProtocolMetadata{
		Version: 1,
		Epoch:   2,
		Round:   3,
		Seq:     4,
		Prev:    common.Digest{1, 2, 3},
	}
	block := testutil.NewTestBlock(md, common.Blacklist{})
	header := block.BlockHeader()
	sig := common.Signature{Signer: []byte("signer"), Value: []byte("sig")}
	qc := testutil.TestQC{sig}
	vote := common.Vote{Vote: common.ToBeSignedVote{BlockHeader: header}, Signature: sig}
	notarization := &common.Notarization{Vote: common.ToBeSignedVote{BlockHeader: header}, QC: qc}
	finalization := &common.Finalization{Finalization: common.ToBeSignedFinalization{BlockHeader: header}, QC: qc}
	emptyNotarization := &common.EmptyNotarization{
		Vote: common.ToBeSignedEmptyVote{EmptyVoteMetadata: common.EmptyVoteMetadata{Epoch: 2, Round: 5}},
		QC:   qc,
	}

	tests := []struct {
		name string
		msg  *common.Message
	}{
		{
			name: "block message",
			msg: &common.Message{
				VerifiedBlockMessage: &common.VerifiedBlockMessage{VerifiedBlock: block, Vote: vote},
			},
		},
		{
			name: "vote",
			msg:  &common.Message{VoteMessage: &vote},
		},
		{
			name: "empty vote",
			msg: &common.Message{
				EmptyVoteMessage: &common.EmptyVote{
					Vote:      common.ToBeSignedEmptyVote{EmptyVoteMetadata: common.EmptyVoteMetadata{Epoch: 2, Round: 5}},
					Signature: sig,
				},
			},
		},
		{
			name: "finalize vote",
			msg: &common.Message{
				FinalizeVote: &common.FinalizeVote{
					Finalization: common.ToBeSignedFinalization{BlockHeader: header},
					Signature:    sig,
				},
			},
		},
		{
			name: "notarization",
			msg:  &common.Message{Notarization: notarization},
		},
		{
			name: "empty notarization",
			msg:  &common.Message{EmptyNotarization: emptyNotarization},
		},
		{
			name: "finalization",
			msg:  &common.Message{Finalization: finalization},
		},
		{
			name: "replication request",
			msg: &common.Message{
				ReplicationRequest: &common.ReplicationRequest{
					Seqs:               []uint64{1, 2},
					Rounds:             []uint64{3, 4},
					LatestRound:        5,
					LatestFinalizedSeq: 6,
				},
			},
		},
		{
			name: "replication response",
			msg: &common.Message{
				VerifiedReplicationResponse: &common.VerifiedReplicationResponse{
					Data: []common.VerifiedQuorumRound{
						{VerifiedBlock: block, Notarization: notarization},
						{VerifiedBlock: block, Finalization: finalization},
						{EmptyNotarization: emptyNotarization},
					},
					LatestRound:        &common.VerifiedQuorumRound{VerifiedBlock: block, Notarization: notarization},
					LatestFinalizedSeq: &common.VerifiedQuorumRound{VerifiedBlock: block, Finalization: finalization},
				},
			},
		},
		{
			name: "empty replication response",
			msg: &common.Message{
				VerifiedReplicationResponse: &common.VerifiedReplicationResponse{},
			},
		},
		{
			name: "block digest request",
			msg: &common.Message{
				BlockDigestRequest: &common.BlockDigestRequest{Seq: 7, Digest: header.Digest},
			},
		},
		{
			name: "auxiliary info",
			msg: &common.Message{
				AuxiliaryInfo: &common.AuxiliaryInfo{Epoch: 8, Version: 9, Data: []byte("data")},
			},
		},
		{
			name: "epoch transition approval",
			msg: &common.Message{
				EpochTransitionApproval: &common.ValidatorSetApproval{
					NodeID:        [20]byte{1, 2, 3},
					AuxInfoDigest: common.Digest{4, 5, 6},
					PChainHeight:  10,
					Signature:     []byte("approval"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			b, err := common.EncodeMessage(tt.msg)
			require.NoError(err)

			decoded, err := common.DecodeMessage(t.Context(), b, &testutil.BlockDeserializer{}, testQCDeserializer{})
			require.NoError(err)

			reencoded, err := common.EncodeMessage(toVerified(t, decoded))
			require.NoError(err)
			require.Equal(b, reencoded)
		})
	}
}

func TestEncodeDecodeReplicationResponseFields(t *testing.T) {
	require := require.New(t)

	block := testutil.NewTestBlock(common.ProtocolMetadata{Round: 1, Seq: 1}, common.Blacklist{})
	header := block.BlockHeader()
	qc := testutil.TestQC{{Signer: []byte("signer"), Value: []byte("sig")}}
	notarization := &common.Notarization{Vote: common.ToBeSignedVote{BlockHeader: header}, QC: qc}
	finalization := &common.Finalization{Finalization: common.ToBeSignedFinalization{BlockHeader: header}, QC: qc}

	b, err := common.EncodeMessage(&common.Message{
		VerifiedReplicationResponse: &common.VerifiedReplicationResponse{
			Data:               []common.VerifiedQuorumRound{{VerifiedBlock: block, Notarization: notarization}},
			LatestRound:        &common.VerifiedQuorumRound{VerifiedBlock: block, Notarization: notarization},
			LatestFinalizedSeq: &common.VerifiedQuorumRound{VerifiedBlock: block, Finalization: finalization},
		},
	})
	require.NoError(err)

	decoded, err := common.DecodeMessage(t.Context(), b, &testutil.BlockDeserializer{}, testQCDeserializer{})
	require.NoError(err)

	resp := decoded.ReplicationResponse
	require.NotNil(resp)
	require.Len(resp.Data, 1)
	requireHeader := func(expected common.BlockHeader, actual common.BlockHeader) {
		require.True(expected.Equals(&actual), "expected %s, got %s", &expected, &actual)
	}
	requireHeader(header, resp.Data[0].Block.BlockHeader())
	requireHeader(header, resp.Data[0].Notarization.Vote.BlockHeader)
	require.Equal(qc, resp.Data[0].Notarization.QC)

	require.NotNil(resp.LatestRound)
	requireHeader(header, resp.LatestRound.Block.BlockHeader())

	require.NotNil(resp.LatestSeq)
	requireHeader(header, resp.LatestSeq.Block.BlockHeader())
	requireHeader(header, resp.LatestSeq.Finalization.Finalization.BlockHeader)
	require.Equal(qc, resp.LatestSeq.Finalization.QC)
	require.Nil(resp.LatestSeq.Notarization)
}

func TestEncodeReplicationResponseSkipsGenesisFinalization(t *testing.T) {
	require := require.New(t)

	block := testutil.NewTestBlock(common.ProtocolMetadata{}, common.Blacklist{})
	genesis := common.VerifiedQuorumRound{
		VerifiedBlock: block,
		Finalization: &common.Finalization{
			Finalization: common.ToBeSignedFinalization{BlockHeader: block.BlockHeader()},
		},
	}

	b, err := common.EncodeMessage(&common.Message{
		VerifiedReplicationResponse: &common.VerifiedReplicationResponse{
			Data:               []common.VerifiedQuorumRound{genesis},
			LatestRound:        &genesis,
			LatestFinalizedSeq: &genesis,
		},
	})
	require.NoError(err)

	decoded, err := common.DecodeMessage(t.Context(), b, &testutil.BlockDeserializer{}, testQCDeserializer{})
	require.NoError(err)
	require.Empty(decoded.ReplicationResponse.Data)
	require.Nil(decoded.ReplicationResponse.LatestRound)
	require.Nil(decoded.ReplicationResponse.LatestSeq)
}

func TestEncodeMessageErrors(t *testing.T) {
	block := testutil.NewTestBlock(common.ProtocolMetadata{}, common.Blacklist{})

	tests := []struct {
		name        string
		msg         *common.Message
		expectedErr error
	}{
		{
			name:        "empty message",
			msg:         &common.Message{},
			expectedErr: common.ErrUnknownMessageType,
		},
		{
			name: "unverified block message",
			msg: &common.Message{
				BlockMessage: &common.BlockMessage{Block: block},
			},
			expectedErr: common.ErrUnknownMessageType,
		},
		{
			name: "notarization without QC",
			msg: &common.Message{
				Notarization: &common.Notarization{},
			},
			expectedErr: common.ErrMissingQC,
		},
		{
			name: "empty notarization without QC",
			msg: &common.Message{
				EmptyNotarization: &common.EmptyNotarization{},
			},
			expectedErr: common.ErrMissingQC,
		},
		{
			name: "finalization without QC",
			msg: &common.Message{
				Finalization: &common.Finalization{},
			},
			expectedErr: common.ErrMissingQC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := common.EncodeMessage(tt.msg)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
