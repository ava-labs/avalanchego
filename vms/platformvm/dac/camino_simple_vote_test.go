// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSimpleVoteOptionsGetMostVoted(t *testing.T) {
	tests := map[string]struct {
		proposal                *SimpleVoteOptions[uint64]
		expectedProposal        *SimpleVoteOptions[uint64]
		expectedMostVotedWeight uint32
		expectedMostVotedIndex  uint32
		expectedUnambiguous     bool
	}{
		"OK: 3 different weights, no cache": {
			proposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 4}, // 0
					{Value: 2, Weight: 7}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 4}, // 0
					{Value: 2, Weight: 7}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedMostVotedWeight: 7,
			expectedMostVotedIndex:  1,
			expectedUnambiguous:     true,
		},
		"OK: 2 equal and 1 higher weight, no cache": {
			proposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 5}, // 0
					{Value: 2, Weight: 7}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 5}, // 0
					{Value: 2, Weight: 7}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedMostVotedWeight: 7,
			expectedMostVotedIndex:  1,
			expectedUnambiguous:     true,
		},
		"OK: 2 equal and 1 lower weight, no cache": {
			proposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 4}, // 0
					{Value: 2, Weight: 5}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 4}, // 0
					{Value: 2, Weight: 5}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedMostVotedWeight: 5,
			expectedMostVotedIndex:  1,
			expectedUnambiguous:     false,
		},
		"OK: 3 equal weights, no cache": {
			proposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 5}, // 0
					{Value: 2, Weight: 5}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 5}, // 0
					{Value: 2, Weight: 5}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedMostVotedWeight: 5,
			expectedMostVotedIndex:  0,
			expectedUnambiguous:     false,
		},
		"OK: 3 different weights, first is highest, no cache": {
			proposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 7}, // 0
					{Value: 2, Weight: 4}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 7}, // 0
					{Value: 2, Weight: 4}, // 1
					{Value: 3, Weight: 5}, // 2
				},
			},
			expectedMostVotedWeight: 7,
			expectedMostVotedIndex:  0,
			expectedUnambiguous:     true,
		},
		"OK: 1 non-zero weight, no cache": {
			proposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 1}, // 0
				},
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 1}, // 0
				},
			},
			expectedMostVotedWeight: 1,
			expectedMostVotedIndex:  0,
			expectedUnambiguous:     true,
		},
		"OK: 1 zero weight, no cache": {
			proposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 0}, // 0
				},
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 0}, // 0
				},
			},
			expectedMostVotedWeight: 0,
			expectedMostVotedIndex:  0,
			expectedUnambiguous:     false,
		},
		"OK: 3 zero weight, no cache": {
			proposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 0}, // 0
					{Value: 2, Weight: 0}, // 0
					{Value: 3, Weight: 0}, // 0
				},
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				Options: []SimpleVoteOption[uint64]{
					{Value: 1, Weight: 0}, // 0
					{Value: 2, Weight: 0}, // 0
					{Value: 3, Weight: 0}, // 0
				},
			},
			expectedMostVotedWeight: 0,
			expectedMostVotedIndex:  0,
			expectedUnambiguous:     false,
		},
		"OK: cached result": {
			proposal: &SimpleVoteOptions[uint64]{
				mostVotedWeight:      5,
				mostVotedOptionIndex: 5,
				unambiguous:          false,
			},
			expectedProposal: &SimpleVoteOptions[uint64]{
				mostVotedWeight:      5,
				mostVotedOptionIndex: 5,
				unambiguous:          false,
			},
			expectedMostVotedWeight: 5,
			expectedMostVotedIndex:  5,
			expectedUnambiguous:     false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mostVotedWeight, mostVotedIndex, unambiguous := tt.proposal.GetMostVoted()
			require.Equal(t, tt.expectedProposal, tt.proposal)
			require.Equal(t, tt.expectedMostVotedWeight, mostVotedWeight)
			require.Equal(t, tt.expectedMostVotedIndex, mostVotedIndex)
			require.Equal(t, tt.expectedUnambiguous, unambiguous)
		})
	}
}
