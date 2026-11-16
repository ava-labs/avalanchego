// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/ava-labs/avalanchego/graft/simplex/common"
	. "github.com/ava-labs/avalanchego/graft/simplex/simplex"
	"github.com/ava-labs/avalanchego/graft/simplex/testutil"
	"github.com/ava-labs/avalanchego/ids"

	"github.com/stretchr/testify/require"
)

func TestRetrieveFromStorage(t *testing.T) {
	nodes := []NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}}
	block := testutil.NewTestBlock(ProtocolMetadata{Seq: 0}, emptyBlacklist)
	finalization, _ := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{N: len(nodes)}, block, nodes[:Quorum(len(nodes))])
	normalStorage := testutil.NewInMemStorage()
	verifiedFinalization := testutil.MustVerifyFinalization(t, finalization)
	err := normalStorage.Index(context.Background(), block, verifiedFinalization)
	require.NoError(t, err)

	for _, testCase := range []struct {
		description           string
		storage               Storage
		expectedErr           error
		expectedVerifiedBlock *VerifiedFinalizedBlock
	}{
		{
			description: "no blocks in storage",
			storage:     testutil.NewInMemStorage(),
		},

		{
			description: "normal storage",
			storage:     normalStorage,
			expectedVerifiedBlock: &VerifiedFinalizedBlock{
				VerifiedBlock: block,
				Finalization:  verifiedFinalization,
			},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			lastBlock, err := RetrieveLastIndexFromStorage(testCase.storage)
			require.ErrorIs(t, err, testCase.expectedErr)

			require.Equal(t, testCase.expectedVerifiedBlock, lastBlock)
		})
	}
}

type unverifiableQC struct{}

func (u *unverifiableQC) Verify(Nodes) (VerifiedQuorumCertificate, error) {
	return nil, fmt.Errorf("invalid QC")
}

// TestVerifyQC checks VerifyQC rejects a nil QC and otherwise returns what the message's Verify returns.
// Checking the signers of a QC is up to the QuorumCertificate implementation.
func TestVerifyQC(t *testing.T) {
	var nodeIDs []NodeID
	var nodes Nodes
	for _, nodeID := range []NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}, {ShortID: ids.ShortID{5}}} {
		nodes = append(nodes, Node{Id: nodeID})
		nodeIDs = append(nodeIDs, nodeID)
	}

	quorumSize := Quorum(len(nodes))
	signatureAggregator := &testutil.TestSignatureAggregator{N: len(nodes)}
	block := testutil.NewTestBlock(ProtocolMetadata{}, emptyBlacklist)
	validFinalization, _ := testutil.NewFinalizationRecord(t, signatureAggregator, block, nodeIDs[:quorumSize])

	tests := []struct {
		name string
		qc   QuorumCertificate
		msg  interface {
			Verify(Nodes) (VerifiedQuorumCertificate, error)
		}
		expectedErr error
	}{
		{
			name: "valid finalization",
			qc:   validFinalization.QC,
			msg:  &validFinalization,
		},
		{
			name:        "quorum certificate not in finalization",
			msg:         &Finalization{Finalization: ToBeSignedFinalization{}},
			expectedErr: fmt.Errorf("nil QuorumCertificate"),
		},
		{
			name:        "invalid QC",
			qc:          validFinalization.QC,
			msg:         &unverifiableQC{},
			expectedErr: fmt.Errorf("invalid QC"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qc, err := VerifyQC(tt.qc, tt.msg, nodes)
			if tt.expectedErr != nil {
				require.EqualError(t, err, tt.expectedErr.Error())
				require.Nil(t, qc)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, qc)
		})
	}
}

func TestGetHighestQuorumRound(t *testing.T) {
	// Test
	nodes := []NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}, {ShortID: ids.ShortID{5}}}
	l := testutil.MakeLogger(t, 0)
	signatureAggregator := &testutil.TestSignatureAggregator{N: len(nodes)}

	// seq 1
	block1 := testutil.NewTestBlock(ProtocolMetadata{
		Seq:   1,
		Round: 1,
	}, emptyBlacklist)
	notarization1, err := testutil.NewNotarization(l, signatureAggregator, block1, nodes)
	require.NoError(t, err)
	finalization1, _ := testutil.NewFinalizationRecord(t, signatureAggregator, block1, nodes)
	verifiedNotarization1 := testutil.MustVerifyNotarization(t, notarization1)
	verifiedFinalization1 := testutil.MustVerifyFinalization(t, finalization1)

	// seq 10
	block10 := testutil.NewTestBlock(ProtocolMetadata{Seq: 10, Round: 10}, emptyBlacklist)
	notarization10, err := testutil.NewNotarization(l, signatureAggregator, block10, nodes)
	require.NoError(t, err)
	verifiedNotarization10 := testutil.MustVerifyNotarization(t, notarization10)

	tests := []struct {
		name       string
		round      *Round
		eNote      *VerifiedEmptyNotarization
		lastBlock  *VerifiedFinalizedBlock
		expectedQr *VerifiedQuorumRound
	}{
		{
			name:  "only empty notarization",
			eNote: testutil.MustVerifyEmptyNotarization(t, testutil.NewEmptyNotarization(nodes, 1)),
			expectedQr: &VerifiedQuorumRound{
				EmptyNotarization: testutil.MustVerifyEmptyNotarization(t, testutil.NewEmptyNotarization(nodes, 1)),
			},
		},
		{
			name:       "nothing",
			expectedQr: nil,
		},
		{
			name:  "round with finalization",
			round: SetRound(block1, nil, &verifiedFinalization1),
			expectedQr: &VerifiedQuorumRound{
				Block:        block1,
				Finalization: &verifiedFinalization1,
			},
		},
		{
			name:  "round with notarization",
			round: SetRound(block1, &verifiedNotarization1, nil),
			expectedQr: &VerifiedQuorumRound{
				Block:        block1,
				Notarization: &verifiedNotarization1,
			},
		},
		{
			name:  "higher round than empty notarization",
			round: SetRound(block10, &verifiedNotarization10, nil),
			eNote: testutil.MustVerifyEmptyNotarization(t, testutil.NewEmptyNotarization(nodes, 1)),
			expectedQr: &VerifiedQuorumRound{
				Block:        block10,
				Notarization: &verifiedNotarization10,
			},
		},
		{
			name:  "higher empty notarization",
			eNote: testutil.MustVerifyEmptyNotarization(t, testutil.NewEmptyNotarization(nodes, 100)),
			round: SetRound(block10, &verifiedNotarization10, nil),
			expectedQr: &VerifiedQuorumRound{
				EmptyNotarization: testutil.MustVerifyEmptyNotarization(t, testutil.NewEmptyNotarization(nodes, 100)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qr := GetLatestVerifiedQuorumRound(tt.round, tt.eNote)
			require.Equal(t, tt.expectedQr, qr)
		})
	}
}
func TestBatchSequences(t *testing.T) {
	tests := []struct {
		name     string
		seqs     []uint64
		numNodes int
		maxSize  uint64
		expected [][]uint64
	}{
		{
			name:     "empty input",
			seqs:     []uint64{},
			numNodes: 3,
			maxSize:  10,
			expected: nil,
		},
		{
			name:     "zero nodes",
			seqs:     []uint64{1, 2, 3},
			numNodes: 0,
			maxSize:  10,
			expected: nil,
		},
		{
			name:     "zero max size",
			seqs:     []uint64{1, 2, 3},
			numNodes: 2,
			maxSize:  0,
			expected: nil,
		},
		{
			name:     "single sequence",
			seqs:     []uint64{5},
			numNodes: 3,
			maxSize:  10,
			expected: [][]uint64{{5}},
		},
		{
			name:     "even split among nodes",
			seqs:     []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8},
			numNodes: 3,
			maxSize:  10,
			expected: [][]uint64{
				{0, 1, 2},
				{3, 4, 5},
				{6, 7, 8},
			},
		},
		{
			name:     "even split among nodes with gaps",
			seqs:     []uint64{0, 1, 3, 4, 6, 7, 8, 10, 11},
			numNodes: 3,
			maxSize:  10,
			expected: [][]uint64{
				{0, 1, 3},
				{4, 6, 7},
				{8, 10, 11},
			},
		},
		{
			name:     "remainder goes to first nodes",
			seqs:     []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			numNodes: 3,
			maxSize:  10,
			expected: [][]uint64{
				{0, 1, 2, 3},
				{4, 5, 6, 7},
				{8, 9},
			},
		},
		{
			name:     "gaps split by total not per run",
			seqs:     []uint64{0, 1, 2, 3, 4, 6, 7, 8, 9, 10, 12, 13, 14, 15},
			numNodes: 5,
			maxSize:  40,
			expected: [][]uint64{
				{0, 1, 2},
				{3, 4, 6},
				{7, 8, 9},
				{10, 12, 13},
				{14, 15},
			},
		},
		{
			name:     "unsorted input is sorted first",
			seqs:     []uint64{9, 0, 4, 2, 7},
			numNodes: 2,
			maxSize:  10,
			expected: [][]uint64{
				{0, 2, 4},
				{7, 9},
			},
		},
		{
			name:     "more nodes than sequences",
			seqs:     []uint64{1, 2, 3},
			numNodes: 5,
			maxSize:  10,
			expected: [][]uint64{{1}, {2}, {3}},
		},
		{
			name: "single node share capped at maxSize",
			seqs: []uint64{
				0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
				13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24,
			},
			numNodes: 1,
			maxSize:  10,
			expected: [][]uint64{
				{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
				{10, 11, 12, 13, 14, 15, 16, 17, 18, 19},
				{20, 21, 22, 23, 24},
			},
		},
		{
			name:     "node shares exceed maxSize and are chunked",
			seqs:     []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},
			numNodes: 3,
			maxSize:  3,
			expected: [][]uint64{
				{0, 1, 2}, {3, 4, 5},
				{6, 7, 8}, {9, 10, 11},
				{12, 13, 14},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BatchSequences(tt.seqs, uint64(tt.numNodes), tt.maxSize)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNodeIDsFromVotes(t *testing.T) {
	nodes := []NodeID{{ShortID: ids.ShortID{1}}, {ShortID: ids.ShortID{2}}, {ShortID: ids.ShortID{3}}, {ShortID: ids.ShortID{4}}, {ShortID: ids.ShortID{5}}}
	votes := make([]*Vote, len(nodes))
	for i, n := range nodes {
		votes[i] = &Vote{Signature: Signature{Signer: n}}
	}

	result := NodeIDsFromVotes(votes)
	require.Equal(t, nodes[1:4], result[1:4])
	require.Equal(t, nodes[:1], result[:1])
	require.Equal(t, nodes, result)
}
