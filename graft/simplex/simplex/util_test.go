// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/ava-labs/simplex/common"
	. "github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"

	"github.com/stretchr/testify/require"
)

func TestRetrieveFromStorage(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	block := testutil.NewTestBlock(ProtocolMetadata{Seq: 0}, emptyBlacklist)
	finalization, _ := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{N: len(nodes)}, block, nodes[:Quorum(len(nodes))])
	normalStorage := testutil.NewInMemStorage()
	err := normalStorage.Index(context.Background(), block, finalization)
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
				Finalization:  finalization,
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

func (u *unverifiableQC) Verify(Nodes) error {
	return fmt.Errorf("invalid QC")
}

func TestVerifyQC(t *testing.T) {
	var nodeIDs []NodeID
	var nodes Nodes
	for _, nodeID := range []NodeID{{1}, {2}, {3}, {4}, {5}} {
		nodes = append(nodes, Node{Id: nodeID})
		nodeIDs = append(nodeIDs, nodeID)
	}

	validatorsToPks := make(map[string][]byte)
	for _, n := range nodes {
		validatorsToPks[string(n.Id)] = []byte{}
	}
	quorumSize := Quorum(len(nodes))
	signatureAggregator := &testutil.TestSignatureAggregator{N: len(nodes)}
	// Test
	tests := []struct {
		name         string
		finalization Finalization
		quorumSize   int
		expectedErr  error
		msgInvalid   bool
	}{
		{
			name: "valid finalization",
			finalization: func() Finalization {
				block := testutil.NewTestBlock(ProtocolMetadata{}, emptyBlacklist)
				finalization, _ := testutil.NewFinalizationRecord(t, signatureAggregator, block, nodeIDs[:quorumSize])
				return finalization
			}(),
			quorumSize: quorumSize,
		}, {
			name: "not enough signers",
			finalization: func() Finalization {
				block := testutil.NewTestBlock(ProtocolMetadata{}, emptyBlacklist)
				finalization, _ := testutil.NewFinalizationRecord(t, signatureAggregator, block, nodeIDs[:quorumSize-1])
				return finalization
			}(),
			quorumSize:  quorumSize,
			expectedErr: fmt.Errorf("quorum certificate signed by insufficient (3) nodes"),
		},
		{
			name: "signer signed twice",
			finalization: func() Finalization {
				block := testutil.NewTestBlock(ProtocolMetadata{}, emptyBlacklist)
				doubleNodes := []NodeID{{1}, {2}, {3}, {4}, {4}}
				finalization, _ := testutil.NewFinalizationRecord(t, signatureAggregator, block, doubleNodes)
				return finalization
			}(),
			quorumSize:  quorumSize,
			expectedErr: fmt.Errorf("quorum certificate is signed by the same node (0400000000000000) more than once"),
		},
		{
			name:         "quorum certificate not in finalization",
			finalization: Finalization{Finalization: ToBeSignedFinalization{}},
			quorumSize:   quorumSize,
			expectedErr:  fmt.Errorf("nil QuorumCertificate"),
		},
		{
			name: "nodes are not eligible signers",
			finalization: func() Finalization {
				block := testutil.NewTestBlock(ProtocolMetadata{}, emptyBlacklist)
				signers := []NodeID{{1}, {2}, {3}, {4}, {6}}
				finalization, _ := testutil.NewFinalizationRecord(t, signatureAggregator, block, signers)
				return finalization
			}(), quorumSize: quorumSize,
			expectedErr: fmt.Errorf("quorum certificate contains an unknown signer (0600000000000000)"),
		},
		{
			name: "invalid QC",
			finalization: func() Finalization {
				block := testutil.NewTestBlock(ProtocolMetadata{}, emptyBlacklist)
				finalization, _ := testutil.NewFinalizationRecord(t, signatureAggregator, block, nodeIDs[:quorumSize])
				return finalization
			}(),
			quorumSize:  quorumSize,
			msgInvalid:  true,
			expectedErr: fmt.Errorf("invalid QC"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isQuorum := func(signers []NodeID) bool {
				return len(signers) >= tt.quorumSize
			}
			if tt.msgInvalid {
				err := VerifyQC(tt.finalization.QC, isQuorum, validatorsToPks, &unverifiableQC{}, nodes)
				require.EqualError(t, err, tt.expectedErr.Error())
			} else {
				err := VerifyQC(tt.finalization.QC, isQuorum, validatorsToPks, &tt.finalization, nodes)
				if tt.expectedErr != nil {
					require.EqualError(t, err, tt.expectedErr.Error())
				} else {
					require.NoError(t, err)
				}
			}
		})
	}
}

func TestGetHighestQuorumRound(t *testing.T) {
	// Test
	nodes := []NodeID{{1}, {2}, {3}, {4}, {5}}
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

	// seq 10
	block10 := testutil.NewTestBlock(ProtocolMetadata{Seq: 10, Round: 10}, emptyBlacklist)
	notarization10, err := testutil.NewNotarization(l, signatureAggregator, block10, nodes)
	require.NoError(t, err)

	tests := []struct {
		name       string
		round      *Round
		eNote      *EmptyNotarization
		lastBlock  *VerifiedFinalizedBlock
		expectedQr *VerifiedQuorumRound
	}{
		{
			name:  "only empty notarization",
			eNote: testutil.NewEmptyNotarization(nodes, 1),
			expectedQr: &VerifiedQuorumRound{
				EmptyNotarization: testutil.NewEmptyNotarization(nodes, 1),
			},
		},
		{
			name:       "nothing",
			expectedQr: nil,
		},
		{
			name:  "round with finalization",
			round: SetRound(block1, nil, &finalization1),
			expectedQr: &VerifiedQuorumRound{
				VerifiedBlock: block1,
				Finalization:  &finalization1,
			},
		},
		{
			name:  "round with notarization",
			round: SetRound(block1, &notarization1, nil),
			expectedQr: &VerifiedQuorumRound{
				VerifiedBlock: block1,
				Notarization:  &notarization1,
			},
		},
		{
			name:  "higher round than empty notarization",
			round: SetRound(block10, &notarization10, nil),
			eNote: testutil.NewEmptyNotarization(nodes, 1),
			expectedQr: &VerifiedQuorumRound{
				VerifiedBlock: block10,
				Notarization:  &notarization10,
			},
		},
		{
			name:  "higher empty notarization",
			eNote: testutil.NewEmptyNotarization(nodes, 100),
			round: SetRound(block10, &notarization10, nil),
			expectedQr: &VerifiedQuorumRound{
				EmptyNotarization: testutil.NewEmptyNotarization(nodes, 100),
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
	nodes := []NodeID{{1}, {2}, {3}, {4}, {5}}
	votes := make([]*Vote, len(nodes))
	for i, n := range nodes {
		votes[i] = &Vote{Signature: Signature{Signer: n}}
	}

	result := NodeIDsFromVotes(votes)
	require.Equal(t, nodes[1:4], result[1:4])
	require.Equal(t, nodes[:1], result[:1])
	require.Equal(t, nodes, result)
}
