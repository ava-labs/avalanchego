// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestExcludeMemberProposalCreateProposalState(t *testing.T) {
	tests := map[string]struct {
		proposal              *ExcludeMemberProposal
		allowedVoters         []ids.ShortID
		expectedProposalState ProposalState
		expectedProposal      *ExcludeMemberProposal
	}{
		"OK: even number of allowed voters": {
			proposal: &ExcludeMemberProposal{
				Start: 100,
				End:   101,
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
			expectedProposalState: &ExcludeMemberProposalState{
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
			expectedProposal: &ExcludeMemberProposal{
				Start: 100,
				End:   101,
			},
		},
		"OK: odd number of allowed voters": {
			proposal: &ExcludeMemberProposal{
				Start: 100,
				End:   101,
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
			expectedProposalState: &ExcludeMemberProposalState{
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
			expectedProposal: &ExcludeMemberProposal{
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

func TestExcludeMemberProposalStateAddVote(t *testing.T) {
	voterAddr1 := ids.ShortID{1}
	voterAddr2 := ids.ShortID{1}
	voterAddr3 := ids.ShortID{1}

	tests := map[string]struct {
		proposal                 *ExcludeMemberProposalState
		voterAddr                ids.ShortID
		vote                     Vote
		expectedUpdatedProposal  ProposalState
		expectedOriginalProposal *ExcludeMemberProposalState
		expectedErr              error
	}{
		"Wrong vote type": {
			proposal: &ExcludeMemberProposalState{
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
			expectedOriginalProposal: &ExcludeMemberProposalState{
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
			proposal: &ExcludeMemberProposalState{
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
			expectedOriginalProposal: &ExcludeMemberProposalState{
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
			proposal: &ExcludeMemberProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{{}},
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 0},
			expectedOriginalProposal: &ExcludeMemberProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{{}},
				},
			},
			expectedErr: ErrNotAllowedToVoteOnProposal,
		},
		"OK: adding vote to not voted option": {
			proposal: &ExcludeMemberProposalState{
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
			expectedUpdatedProposal: &ExcludeMemberProposalState{
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
			expectedOriginalProposal: &ExcludeMemberProposalState{
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
			proposal: &ExcludeMemberProposalState{
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
			expectedUpdatedProposal: &ExcludeMemberProposalState{
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
			expectedOriginalProposal: &ExcludeMemberProposalState{
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
			proposal: &ExcludeMemberProposalState{
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
			expectedUpdatedProposal: &ExcludeMemberProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{{Weight: 1}},
				},
			},
			expectedOriginalProposal: &ExcludeMemberProposalState{
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

func TestExcludeMemberProposalCreateFinishedProposalState(t *testing.T) {
	memberAddress := ids.ShortID{1}

	tests := map[string]struct {
		proposal                 *ExcludeMemberProposal
		optionIndex              uint32
		expectedProposalState    ProposalState
		expectedOriginalProposal *ExcludeMemberProposal
		expectedErr              error
	}{
		"Fail: option 2 out of bonds": {
			proposal: &ExcludeMemberProposal{
				Start:         100,
				End:           101,
				MemberAddress: memberAddress,
			},
			optionIndex: 2,
			expectedOriginalProposal: &ExcludeMemberProposal{
				Start:         100,
				End:           101,
				MemberAddress: memberAddress,
			},
			expectedErr: errWrongOptionIndex,
		},
		"OK: option 0": {
			proposal: &ExcludeMemberProposal{
				Start:         100,
				End:           101,
				MemberAddress: memberAddress,
			},
			optionIndex: 0,
			expectedProposalState: &ExcludeMemberProposalState{
				Start:         100,
				End:           101,
				MemberAddress: memberAddress,
				AllowedVoters: []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 1},
						{Value: false},
					},
				},
			},
			expectedOriginalProposal: &ExcludeMemberProposal{
				Start:         100,
				End:           101,
				MemberAddress: memberAddress,
			},
		},
		"OK: option 1": {
			proposal: &ExcludeMemberProposal{
				Start:         100,
				End:           101,
				MemberAddress: memberAddress,
			},
			optionIndex: 1,
			expectedProposalState: &ExcludeMemberProposalState{
				Start:         100,
				End:           101,
				MemberAddress: memberAddress,
				AllowedVoters: []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true},
						{Value: false, Weight: 1},
					},
				},
			},
			expectedOriginalProposal: &ExcludeMemberProposal{
				Start:         100,
				End:           101,
				MemberAddress: memberAddress,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			proposalState, err := tt.proposal.CreateFinishedProposalState(tt.optionIndex)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedProposalState, proposalState)
			require.Equal(t, tt.expectedOriginalProposal, tt.proposal)
			if tt.expectedErr == nil {
				require.True(t, proposalState.CanBeFinished())
				require.True(t, proposalState.IsSuccessful())
			}
		})
	}
}
