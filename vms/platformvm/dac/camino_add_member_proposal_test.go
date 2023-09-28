// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestAddMemberProposalCreateProposalState(t *testing.T) {
	tests := map[string]struct {
		proposal              *AddMemberProposal
		allowedVoters         []ids.ShortID
		expectedProposalState ProposalState
		expectedProposal      *AddMemberProposal
	}{
		"OK: even number of allowed voters": {
			proposal: &AddMemberProposal{
				Start: 100,
				End:   101,
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
			expectedProposalState: &AddMemberProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true},
						{Value: false},
					},
				},
				TotalAllowedVoters: 4,
			},
			expectedProposal: &AddMemberProposal{
				Start: 100,
				End:   101,
			},
		},
		"OK: odd number of allowed voters": {
			proposal: &AddMemberProposal{
				Start: 100,
				End:   101,
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
			expectedProposalState: &AddMemberProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true},
						{Value: false},
					},
				},
				TotalAllowedVoters: 5,
			},
			expectedProposal: &AddMemberProposal{
				Start: 100,
				End:   101,
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

func TestAddMemberProposalStateAddVote(t *testing.T) {
	voterAddr1 := ids.ShortID{1}
	voterAddr2 := ids.ShortID{1}
	voterAddr3 := ids.ShortID{1}

	tests := map[string]struct {
		proposal                 *AddMemberProposalState
		voterAddr                ids.ShortID
		vote                     Vote
		expectedUpdatedProposal  ProposalState
		expectedOriginalProposal *AddMemberProposalState
		expectedErr              error
	}{
		"Wrong vote type": {
			proposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 1}, // 1
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &DummyVote{}, // not *SimpleVote
			expectedOriginalProposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 1}, // 1
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			expectedErr: ErrWrongVote,
		},
		"Wrong vote option index": {
			proposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 1}, // 1
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 2},
			expectedOriginalProposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 1}, // 1
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			expectedErr: ErrWrongVote,
		},
		"Not allowed to vote on this proposal": {
			proposal: &AddMemberProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{{}},
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 0},
			expectedOriginalProposal: &AddMemberProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{{}},
				},
			},
			expectedErr: ErrNotAllowedToVoteOnProposal,
		},
		"OK: adding vote to not voted option": {
			proposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 0}, // 1
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &SimpleVote{OptionIndex: 1},
			expectedUpdatedProposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 1}, // 1
					},
				},
			},
			expectedOriginalProposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 0}, // 1
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
		},
		"OK: adding vote to already voted option": {
			proposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 1}, // 1
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &SimpleVote{OptionIndex: 1},
			expectedUpdatedProposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 2}, // 1
					},
				},
			},
			expectedOriginalProposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 2},  // 0
						{Value: false, Weight: 1}, // 1
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
		},
		"OK: voter addr in the middle of allowedVoters array": {
			proposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr2, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{{}},
				},
			},
			voterAddr: voterAddr2,
			vote:      &SimpleVote{OptionIndex: 0},
			expectedUpdatedProposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{{Weight: 1}},
				},
			},
			expectedOriginalProposal: &AddMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr2, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{{}},
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
