// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestBaseFeeProposalCreateProposalState(t *testing.T) {
	tests := map[string]struct {
		proposal              *BaseFeeProposal
		allowedVoters         []ids.ShortID
		expectedProposalState ProposalState
		expectedProposal      *BaseFeeProposal
	}{
		"OK: even number of allowed voters": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{123, 555, 7},
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
			expectedProposalState: &BaseFeeProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 123},
						{Value: 555},
						{Value: 7},
					},
				},
				TotalAllowedVoters: 4,
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{123, 555, 7},
			},
		},
		"OK: odd number of allowed voters": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{123, 555, 7},
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
			expectedProposalState: &BaseFeeProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 123},
						{Value: 555},
						{Value: 7},
					},
				},
				TotalAllowedVoters: 5,
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{123, 555, 7},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			proposalState := tt.proposal.CreateProposalState(tt.allowedVoters)
			require.Equal(t, tt.expectedProposal, tt.proposal)
			require.Equal(t, tt.expectedProposalState, proposalState)
		})
	}
}

func TestBaseFeeProposalStateAddVote(t *testing.T) {
	voterAddr1 := ids.ShortID{1}
	voterAddr2 := ids.ShortID{1}
	voterAddr3 := ids.ShortID{1}

	tests := map[string]struct {
		proposal                 *BaseFeeProposalState
		voterAddr                ids.ShortID
		vote                     Vote
		expectedUpdatedProposal  ProposalState
		expectedOriginalProposal *BaseFeeProposalState
		expectedErr              error
	}{
		"Wrong vote type": {
			proposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &DummyVote{}, // not *SimpleVote
			expectedOriginalProposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			expectedErr: ErrWrongVote,
		},
		"Wrong vote option index": {
			proposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 3},
			expectedOriginalProposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			expectedErr: ErrWrongVote,
		},
		"Not allowed to vote on this proposal": {
			proposal: &BaseFeeProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{{Value: 1}},
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 0},
			expectedOriginalProposal: &BaseFeeProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{{Value: 1}},
				},
			},
			expectedErr: ErrNotAllowedToVoteOnProposal,
		},
		"OK: adding vote to not voted option": {
			proposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &SimpleVote{OptionIndex: 1},
			expectedUpdatedProposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 1}, // 1
						{Value: 30, Weight: 1}, // 2
					},
				},
			},
			expectedOriginalProposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
		},
		"OK: adding vote to already voted option": {
			proposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &SimpleVote{OptionIndex: 2},
			expectedUpdatedProposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 2}, // 2
					},
				},
			},
			expectedOriginalProposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 2}, // 0
						{Value: 20, Weight: 0}, // 1
						{Value: 30, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
		},
		"OK: voter addr in the middle of allowedVoters array": {
			proposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr2, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{{Value: 1}},
				},
			},
			voterAddr: voterAddr2,
			vote:      &SimpleVote{OptionIndex: 0},
			expectedUpdatedProposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{{Value: 1, Weight: 1}},
				},
			},
			expectedOriginalProposal: &BaseFeeProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr2, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{{Value: 1}},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			updatedProposal, err := tt.proposal.AddVote(tt.voterAddr, tt.vote)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedUpdatedProposal, updatedProposal)
			require.Equal(t, tt.expectedOriginalProposal, tt.proposal)
		})
	}
}
