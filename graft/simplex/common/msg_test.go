// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common_test

import (
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"

	"github.com/stretchr/testify/require"
)

func TestIsReplicationMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      common.Message
		expected bool
	}{
		{
			name:     "empty message",
			msg:      common.Message{},
			expected: false,
		},
		{
			name:     "replication request",
			msg:      common.Message{ReplicationRequest: &common.ReplicationRequest{}},
			expected: true,
		},
		{
			name:     "replication response",
			msg:      common.Message{ReplicationResponse: &common.ReplicationResponse{}},
			expected: true,
		},
		{
			name:     "verified replication response",
			msg:      common.Message{VerifiedReplicationResponse: &common.VerifiedReplicationResponse{}},
			expected: true,
		},
		{
			name:     "block message",
			msg:      common.Message{BlockMessage: &common.BlockMessage{}},
			expected: false,
		},
		{
			name:     "verified block message",
			msg:      common.Message{VerifiedBlockMessage: &common.VerifiedBlockMessage{}},
			expected: false,
		},
		{
			name:     "empty notarization",
			msg:      common.Message{EmptyNotarization: &common.EmptyNotarization{}},
			expected: false,
		},
		{
			name:     "vote",
			msg:      common.Message{VoteMessage: &common.Vote{}},
			expected: false,
		},
		{
			name:     "empty vote",
			msg:      common.Message{EmptyVoteMessage: &common.EmptyVote{}},
			expected: false,
		},
		{
			name:     "notarization",
			msg:      common.Message{Notarization: &common.Notarization{}},
			expected: false,
		},
		{
			name:     "finalize vote",
			msg:      common.Message{FinalizeVote: &common.FinalizeVote{}},
			expected: false,
		},
		{
			name:     "finalization",
			msg:      common.Message{Finalization: &common.Finalization{}},
			expected: false,
		},
		{
			// A block digest request is a request but not a replication message.
			name:     "block digest request",
			msg:      common.Message{BlockDigestRequest: &common.BlockDigestRequest{}},
			expected: true,
		},
		{
			// When several fields are set, any replication field makes it a replication message.
			name: "block message and replication request",
			msg: common.Message{
				BlockMessage:       &common.BlockMessage{},
				ReplicationRequest: &common.ReplicationRequest{},
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, test.msg.IsReplicationMessage())
		})
	}
}

func TestQuorumRoundMalformed(t *testing.T) {
	tests := []struct {
		name        string
		qr          common.QuorumRound
		expectedErr bool
	}{
		{
			name: "empty notarization",
			qr: common.QuorumRound{
				EmptyNotarization: &common.EmptyNotarization{},
			},
			expectedErr: false,
		}, {
			name: "all nil",
			qr: common.QuorumRound{
				EmptyNotarization: nil,
				Block:             nil,
				Notarization:      nil,
				Finalization:      nil,
			},
			expectedErr: true,
		}, {
			name: "block and notarization",
			qr: common.QuorumRound{
				Block:        &testutil.TestBlock{},
				Notarization: &common.Notarization{},
			},
			expectedErr: false,
		}, {
			name: "block and finalization",
			qr: common.QuorumRound{
				Block:        &testutil.TestBlock{},
				Finalization: &common.Finalization{},
			},
			expectedErr: false,
		}, {
			name: "block and empty notarization",
			qr: common.QuorumRound{
				Block:             &testutil.TestBlock{},
				EmptyNotarization: &common.EmptyNotarization{},
			},
			expectedErr: true,
		},
		{
			name: "block and notarization and finalization",
			qr: common.QuorumRound{
				Block:        &testutil.TestBlock{},
				Notarization: &common.Notarization{},
				Finalization: &common.Finalization{},
			},
			expectedErr: false,
		},
		{
			name: "notarization and no block",
			qr: common.QuorumRound{
				Notarization: &common.Notarization{},
			},
			expectedErr: true,
		},
		{
			name: "finalization and no block",
			qr: common.QuorumRound{
				Finalization: &common.Finalization{},
			},
			expectedErr: true,
		},
		{
			name: "just block",
			qr: common.QuorumRound{
				Block: &testutil.TestBlock{},
			},
			expectedErr: true,
		},
		{
			name: "block and notarization and empty notarization",
			qr: common.QuorumRound{
				Block:             &testutil.TestBlock{},
				Notarization:      &common.Notarization{},
				EmptyNotarization: &common.EmptyNotarization{},
			},
			expectedErr: false,
		},
		{
			name: "block and finalization and empty notarization",
			qr: common.QuorumRound{
				Block:             &testutil.TestBlock{},
				Finalization:      &common.Finalization{},
				EmptyNotarization: &common.EmptyNotarization{},
			},
			expectedErr: false,
		},
		{
			name: "block and notarization and finalization of different epochs",
			qr: common.QuorumRound{
				Block:        &testutil.TestBlock{},
				Notarization: &common.Notarization{Vote: common.ToBeSignedVote{BlockHeader: common.BlockHeader{ProtocolMetadata: common.ProtocolMetadata{Epoch: 1}}}},
				Finalization: &common.Finalization{Finalization: common.ToBeSignedFinalization{BlockHeader: common.BlockHeader{ProtocolMetadata: common.ProtocolMetadata{Epoch: 2}}}},
			},
			expectedErr: true,
		},
		{
			name: "block and notarization and empty notarization of different epochs",
			qr: common.QuorumRound{
				Block:             &testutil.TestBlock{},
				Notarization:      &common.Notarization{Vote: common.ToBeSignedVote{BlockHeader: common.BlockHeader{ProtocolMetadata: common.ProtocolMetadata{Epoch: 1}}}},
				EmptyNotarization: &common.EmptyNotarization{Vote: common.ToBeSignedEmptyVote{EmptyVoteMetadata: common.EmptyVoteMetadata{Epoch: 2}}},
			},
			expectedErr: true,
		},
		{
			name: "block and finalization and empty notarization of different epochs",
			qr: common.QuorumRound{
				Block:             &testutil.TestBlock{},
				Finalization:      &common.Finalization{Finalization: common.ToBeSignedFinalization{BlockHeader: common.BlockHeader{ProtocolMetadata: common.ProtocolMetadata{Epoch: 1}}}},
				EmptyNotarization: &common.EmptyNotarization{Vote: common.ToBeSignedEmptyVote{EmptyVoteMetadata: common.EmptyVoteMetadata{Epoch: 2}}},
			},
			expectedErr: true,
		},
		{
			name: "empty notarization and finalization, no block",
			qr: common.QuorumRound{
				EmptyNotarization: &common.EmptyNotarization{},
				Finalization:      &common.Finalization{},
			},
			expectedErr: true,
		},
		{
			name: "empty notarization and notarization, no block",
			qr: common.QuorumRound{
				EmptyNotarization: &common.EmptyNotarization{},
				Notarization:      &common.Notarization{},
			},
			expectedErr: true,
		},
		{
			name: "empty notarization and notarization and finalization, no block",
			qr: common.QuorumRound{
				EmptyNotarization: &common.EmptyNotarization{},
				Notarization:      &common.Notarization{},
				Finalization:      &common.Finalization{},
			},
			expectedErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.qr.IsWellFormed()
			if err != nil {
				require.True(t, test.expectedErr)
				return
			}
			require.False(t, test.expectedErr)
		})
	}

}

func TestSizeMatchesBytes(t *testing.T) {
	qc := testutil.TestQC{
		{Signer: common.NodeID{1}, Value: []byte("signature1")},
		{Signer: common.NodeID{2}, Value: []byte("signature2")},
		{Signer: common.NodeID{3}, Value: []byte("signature3")},
	}

	bh := common.BlockHeader{
		ProtocolMetadata: common.ProtocolMetadata{
			Version: 1,
			Epoch:   2,
			Round:   3,
			Seq:     4,
			Prev:    common.Digest{3},
		},
		Digest: common.Digest{6},
	}
	require.Equal(t, len(bh.Bytes()), bh.Size())

	emptyVote := common.ToBeSignedEmptyVote{
		EmptyVoteMetadata: common.EmptyVoteMetadata{Round: 7, Epoch: 8},
	}
	require.Equal(t, len(emptyVote.Bytes()), emptyVote.Size())

	notarization := common.Notarization{Vote: common.ToBeSignedVote{BlockHeader: bh}, QC: qc}
	require.Equal(t, len(notarization.Vote.Bytes())+len(notarization.QC.Bytes()), notarization.Size())

	finalization := common.Finalization{Finalization: common.ToBeSignedFinalization{BlockHeader: bh}, QC: qc}
	require.Equal(t, len(finalization.Finalization.Bytes())+len(finalization.QC.Bytes()), finalization.Size())

	emptyNotarization := common.EmptyNotarization{Vote: emptyVote, QC: qc}
	require.Equal(t, len(emptyNotarization.Vote.Bytes())+len(emptyNotarization.QC.Bytes()), emptyNotarization.Size())

}

func TestVerifyQCConsistentWithBlock(t *testing.T) {
	block := testutil.NewTestBlock(common.ProtocolMetadata{
		Version: 1,
		Epoch:   2,
		Round:   5,
		Seq:     4,
		Prev:    common.Digest{7},
	}, common.Blacklist{})
	bh := block.BlockHeader()

	finalization := func(bh common.BlockHeader) *common.Finalization {
		return &common.Finalization{Finalization: common.ToBeSignedFinalization{BlockHeader: bh}}
	}
	notarization := func(bh common.BlockHeader) *common.Notarization {
		return &common.Notarization{Vote: common.ToBeSignedVote{BlockHeader: bh}}
	}
	emptyNotarization := func(round, epoch uint64) *common.EmptyNotarization {
		return &common.EmptyNotarization{Vote: common.ToBeSignedEmptyVote{EmptyVoteMetadata: common.EmptyVoteMetadata{Round: round, Epoch: epoch}}}
	}
	// mutateHeader returns the block's header with one field changed, so the digest still matches the block.
	mutateHeader := func(mutate func(*common.BlockHeader)) common.BlockHeader {
		mutated := bh
		mutate(&mutated)
		return mutated
	}

	tests := []struct {
		name        string
		qr          common.QuorumRound
		expectedErr bool
	}{
		{
			name: "finalization matches block",
			qr:   common.QuorumRound{Block: block, Finalization: finalization(bh)},
		},
		{
			name: "notarization matches block",
			qr:   common.QuorumRound{Block: block, Notarization: notarization(bh)},
		},
		{
			name: "notarization and empty notarization of the block's round",
			qr:   common.QuorumRound{Block: block, Notarization: notarization(bh), EmptyNotarization: emptyNotarization(bh.Round, bh.Epoch)},
		},
		{
			name: "empty notarization without block",
			qr:   common.QuorumRound{EmptyNotarization: emptyNotarization(bh.Round, bh.Epoch)},
		},
		{
			name:        "malformed: finalization without block",
			qr:          common.QuorumRound{Finalization: finalization(bh)},
			expectedErr: true,
		},
		{
			name:        "finalization digest mismatch",
			qr:          common.QuorumRound{Block: block, Finalization: finalization(mutateHeader(func(h *common.BlockHeader) { h.Digest = common.Digest{9} }))},
			expectedErr: true,
		},
		{
			name:        "finalization round mismatch",
			qr:          common.QuorumRound{Block: block, Finalization: finalization(mutateHeader(func(h *common.BlockHeader) { h.Round++ }))},
			expectedErr: true,
		},
		{
			name:        "finalization seq mismatch",
			qr:          common.QuorumRound{Block: block, Finalization: finalization(mutateHeader(func(h *common.BlockHeader) { h.Seq++ }))},
			expectedErr: true,
		},
		{
			name:        "finalization epoch mismatch",
			qr:          common.QuorumRound{Block: block, Finalization: finalization(mutateHeader(func(h *common.BlockHeader) { h.Epoch++ }))},
			expectedErr: true,
		},
		{
			name:        "finalization prev mismatch",
			qr:          common.QuorumRound{Block: block, Finalization: finalization(mutateHeader(func(h *common.BlockHeader) { h.Prev = common.Digest{8} }))},
			expectedErr: true,
		},
		{
			name:        "finalization version mismatch",
			qr:          common.QuorumRound{Block: block, Finalization: finalization(mutateHeader(func(h *common.BlockHeader) { h.Version++ }))},
			expectedErr: true,
		},
		{
			name:        "notarization digest mismatch",
			qr:          common.QuorumRound{Block: block, Notarization: notarization(mutateHeader(func(h *common.BlockHeader) { h.Digest = common.Digest{9} }))},
			expectedErr: true,
		},
		{
			name:        "notarization round mismatch",
			qr:          common.QuorumRound{Block: block, Notarization: notarization(mutateHeader(func(h *common.BlockHeader) { h.Round++ }))},
			expectedErr: true,
		},
		{
			name:        "notarization seq mismatch",
			qr:          common.QuorumRound{Block: block, Notarization: notarization(mutateHeader(func(h *common.BlockHeader) { h.Seq++ }))},
			expectedErr: true,
		},
		{
			name:        "empty notarization round mismatch",
			qr:          common.QuorumRound{Block: block, Notarization: notarization(bh), EmptyNotarization: emptyNotarization(bh.Round+1, bh.Epoch)},
			expectedErr: true,
		},
		{
			name: "finalization and empty notarization of the block's round",
			qr:   common.QuorumRound{Block: block, Finalization: finalization(bh), EmptyNotarization: emptyNotarization(bh.Round, bh.Epoch)},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.qr.VerifyQCConsistentWithBlock()
			if test.expectedErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
