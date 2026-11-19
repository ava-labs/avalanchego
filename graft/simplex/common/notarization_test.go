// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ava-labs/simplex/common"
	. "github.com/ava-labs/simplex/testutil"

	"github.com/stretchr/testify/require"
)

var errorSigAggregation = errors.New("signature error")

func TestNewNotarization(t *testing.T) {
	l := MakeLogger(t, 1)
	testBlock := &TestBlock{}
	tests := []struct {
		name                 string
		votesForCurrentRound map[string]*common.Vote
		block                common.VerifiedBlock
		expectError          error
		signatureAggregator  common.SignatureAggregator
		expectedSigners      []common.NodeID
	}{
		{
			name: "valid notarization",
			votesForCurrentRound: func() map[string]*common.Vote {
				votes := make(map[string]*common.Vote)
				nodeIds := [][]byte{{1}, {2}, {3}, {4}, {5}}
				for _, nodeId := range nodeIds {
					vote, err := NewTestVote(testBlock, nodeId)
					require.NoError(t, err)
					votes[string(nodeId)] = vote
				}
				return votes
			}(),
			block:               testBlock,
			signatureAggregator: &TestSignatureAggregator{N: 5},
			expectError:         nil,
			expectedSigners: []common.NodeID{
				{1}, {2}, {3}, {4}, {5},
			},
		},
		{
			name: "diverging votes",
			votesForCurrentRound: func() map[string]*common.Vote {
				votes := make(map[string]*common.Vote)
				nodeIds := [][]byte{{1}, {2}, {3}, {4}, {5}}
				for _, nodeId := range nodeIds {
					vote, err := NewTestVote(testBlock, nodeId)
					require.NoError(t, err)
					votes[string(nodeId)] = vote
				}
				votes[string(common.NodeID{2})].Vote.Digest[0]++ // Diverging vote
				return votes
			}(),
			block:               testBlock,
			signatureAggregator: &TestSignatureAggregator{N: 5},
			expectError:         nil,
			expectedSigners: []common.NodeID{
				{1}, {3}, {4}, {5},
			},
		},
		{
			name:                 "no votes",
			votesForCurrentRound: map[string]*common.Vote{},
			block:                testBlock,
			signatureAggregator:  &TestSignatureAggregator{N: 5},
			expectError:          common.ErrorNoVotes,
		},
		{
			name: "error aggregating",
			votesForCurrentRound: func() map[string]*common.Vote {
				votes := make(map[string]*common.Vote)
				nodeIds := [][]byte{{1}, {2}, {3}, {4}, {5}}
				for _, nodeId := range nodeIds {
					vote, err := NewTestVote(testBlock, nodeId)
					require.NoError(t, err)
					votes[string(nodeId)] = vote
				}
				return votes
			}(),
			block:               testBlock,
			signatureAggregator: &TestSignatureAggregator{Err: errorSigAggregation, N: 5},
			expectError:         errorSigAggregation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notarization, err := common.NewNotarization(l, tt.signatureAggregator, tt.votesForCurrentRound, tt.block.BlockHeader())
			require.ErrorIs(t, err, tt.expectError, "expected error, got nil")

			if tt.expectError == nil {
				signers := notarization.QC.Signers()
				require.Equal(t, tt.expectedSigners, signers)

				for i, signer := range signers[1:] {
					require.Negative(t, bytes.Compare(signers[i], signer), "signers not in order")
				}
			}
		})
	}

}

func TestNewFinalization(t *testing.T) {
	tests := []struct {
		name                 string
		finalizeVotes        []*common.FinalizeVote
		signatureAggregator  common.SignatureAggregator
		expectedFinalization *common.ToBeSignedFinalization
		expectedQC           *common.QuorumCertificate
		expectError          error
	}{
		{
			name: "valid finalizations in order",
			finalizeVotes: []*common.FinalizeVote{
				NewTestFinalizeVote(t, &TestBlock{}, []byte{1}),
				NewTestFinalizeVote(t, &TestBlock{}, []byte{2}),
				NewTestFinalizeVote(t, &TestBlock{}, []byte{3}),
			},
			signatureAggregator:  &TestSignatureAggregator{N: 4},
			expectedFinalization: &NewTestFinalizeVote(t, &TestBlock{}, []byte{1}).Finalization,
			expectError:          nil,
		},
		{
			name: "unsorted finalizations",
			finalizeVotes: []*common.FinalizeVote{
				NewTestFinalizeVote(t, &TestBlock{}, []byte{3}),
				NewTestFinalizeVote(t, &TestBlock{}, []byte{1}),
				NewTestFinalizeVote(t, &TestBlock{}, []byte{2}),
			},
			signatureAggregator:  &TestSignatureAggregator{N: 4},
			expectedFinalization: &NewTestFinalizeVote(t, &TestBlock{}, []byte{1}).Finalization,
			expectError:          nil,
		},
		{
			name: "finalizations with different digests",
			finalizeVotes: []*common.FinalizeVote{
				NewTestFinalizeVote(t, &TestBlock{Digest: common.Digest{1}}, []byte{1}),
				NewTestFinalizeVote(t, &TestBlock{Digest: common.Digest{2}}, []byte{2}),
				NewTestFinalizeVote(t, &TestBlock{Digest: common.Digest{3}}, []byte{3}),
			},
			signatureAggregator: &TestSignatureAggregator{N: 4},
			expectError:         common.ErrorInvalidFinalizationDigest,
		},
		{
			name: "signature aggregator errors",
			finalizeVotes: []*common.FinalizeVote{
				NewTestFinalizeVote(t, &TestBlock{}, []byte{1}),
			},
			signatureAggregator: &TestSignatureAggregator{Err: errorSigAggregation, N: 4},
			expectError:         errorSigAggregation,
		},
		{
			name:                "no votes",
			finalizeVotes:       []*common.FinalizeVote{},
			signatureAggregator: &TestSignatureAggregator{N: 4},
			expectError:         common.ErrorNoVotes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalization, err := common.NewFinalization(tt.signatureAggregator, tt.finalizeVotes)
			require.ErrorIs(t, err, tt.expectError, "expected error, got nil")

			if tt.expectError == nil {
				require.Equal(t, finalization.Finalization.Digest, tt.expectedFinalization.Digest, "digests not correct")

				signers := finalization.QC.Signers()
				require.Equal(t, len(signers), len(tt.finalizeVotes), "unexpected amount of signers")

				// ensure the qc signers are in order
				for i, signer := range signers[1:] {
					require.Negative(t, bytes.Compare(signers[i], signer), "signers not in order")
				}
			}
		})
	}
}
