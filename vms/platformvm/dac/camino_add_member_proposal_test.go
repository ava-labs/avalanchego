// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestAddMemberProposalVerify(t *testing.T) {
	tests := map[string]struct {
		proposal         *AddMemberProposal
		expectedProposal *AddMemberProposal
		expectedErr      error
	}{
		"End-time is equal to start-time": {
			proposal: &AddMemberProposal{
				Start: 100,
				End:   100,
			},
			expectedProposal: &AddMemberProposal{
				Start: 100,
				End:   100,
			},
			expectedErr: errEndNotAfterStart,
		},
		"End-time is less than start-time": {
			proposal: &AddMemberProposal{
				Start: 100,
				End:   99,
			},
			expectedProposal: &AddMemberProposal{
				Start: 100,
				End:   99,
			},
			expectedErr: errEndNotAfterStart,
		},
		"To small duration": {
			proposal: &AddMemberProposal{
				Start: 100,
				End:   100 + AddMemberProposalDuration - 1,
			},
			expectedProposal: &AddMemberProposal{
				Start: 100,
				End:   100 + AddMemberProposalDuration - 1,
			},
			expectedErr: errWrongDuration,
		},
		"To big duration": {
			proposal: &AddMemberProposal{
				Start: 100,
				End:   100 + AddMemberProposalDuration + 1,
			},
			expectedProposal: &AddMemberProposal{
				Start: 100,
				End:   100 + AddMemberProposalDuration + 1,
			},
			expectedErr: errWrongDuration,
		},
		"OK": {
			proposal: &AddMemberProposal{
				Start: 100,
				End:   100 + AddMemberProposalDuration,
			},
			expectedProposal: &AddMemberProposal{
				Start: 100,
				End:   100 + AddMemberProposalDuration,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.proposal.Verify(), tt.expectedErr)
			require.Equal(t, tt.expectedProposal, tt.proposal)
		})
	}
}

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
	voterAddr2 := ids.ShortID{2}
	voterAddr3 := ids.ShortID{3}

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

func TestAddMemberProposalCreateFinishedProposalState(t *testing.T) {
	applicantAddress := ids.ShortID{1}

	tests := map[string]struct {
		proposal                 *AddMemberProposal
		optionIndex              uint32
		expectedProposalState    ProposalState
		expectedOriginalProposal *AddMemberProposal
		expectedErr              error
	}{
		"Fail: option 2 out of bounds": {
			proposal: &AddMemberProposal{
				Start:            100,
				End:              101,
				ApplicantAddress: applicantAddress,
			},
			optionIndex: 2,
			expectedOriginalProposal: &AddMemberProposal{
				Start:            100,
				End:              101,
				ApplicantAddress: applicantAddress,
			},
			expectedErr: errWrongOptionIndex,
		},
		"OK: option 0": {
			proposal: &AddMemberProposal{
				Start:            100,
				End:              101,
				ApplicantAddress: applicantAddress,
			},
			optionIndex: 0,
			expectedProposalState: &AddMemberProposalState{
				Start:            100,
				End:              101,
				ApplicantAddress: applicantAddress,
				AllowedVoters:    []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 1},
						{Value: false},
					},
				},
			},
			expectedOriginalProposal: &AddMemberProposal{
				Start:            100,
				End:              101,
				ApplicantAddress: applicantAddress,
			},
		},
		"OK: option 1": {
			proposal: &AddMemberProposal{
				Start:            100,
				End:              101,
				ApplicantAddress: applicantAddress,
			},
			optionIndex: 1,
			expectedProposalState: &AddMemberProposalState{
				Start:            100,
				End:              101,
				ApplicantAddress: applicantAddress,
				AllowedVoters:    []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true},
						{Value: false, Weight: 1},
					},
				},
			},
			expectedOriginalProposal: &AddMemberProposal{
				Start:            100,
				End:              101,
				ApplicantAddress: applicantAddress,
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

func TestAddMemberProposalStateIsSuccessful(t *testing.T) {
	tests := map[string]struct {
		proposal                 *AddMemberProposalState
		expectedSuccessful       bool
		expectedOriginalProposal *AddMemberProposalState
	}{
		// Case, when most voted weight is less, than 50% of votes is impossible, cause proposal only has 2 options
		"Not successful: total voted weight is less, than 50% of total allowed voters": {
			proposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 25},
						{Value: false, Weight: 26},
					},
				},
				TotalAllowedVoters: 102,
			},
			expectedSuccessful: false,
			expectedOriginalProposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 25},
						{Value: false, Weight: 26},
					},
				},
				TotalAllowedVoters: 102,
			},
		},
		"Successful": {
			proposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 25},
						{Value: false, Weight: 26},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedSuccessful: true,
			expectedOriginalProposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 25},
						{Value: false, Weight: 26},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expectedSuccessful, tt.proposal.IsSuccessful())
			require.Equal(t, tt.expectedOriginalProposal, tt.proposal)
		})
	}
}

func TestAddMemberProposalStateCanBeFinished(t *testing.T) {
	tests := map[string]struct {
		proposal                 *AddMemberProposalState
		expectedCanBeFinished    bool
		expectedOriginalProposal *AddMemberProposalState
	}{
		"Can not be finished: most voted weight is less than 50% of total allowed voters and not everyone had voted": {
			proposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 50},
						{Value: false},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 50},
						{Value: false},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Can be finished: everyone had voted": {
			proposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 50},
						{Value: false, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 50},
						{Value: false, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Can be finished: most voted weight is greater than 50% of total allowed voters": {
			proposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 51},
						{Value: false},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &AddMemberProposalState{
				SimpleVoteOptions: SimpleVoteOptions[bool]{
					Options: []SimpleVoteOption[bool]{
						{Value: true, Weight: 51},
						{Value: false},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		// We don't have test-case 'no option can reach 50%+ of votes' for this proposal type, cause its impossible with just 2 options
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expectedCanBeFinished, tt.proposal.CanBeFinished())
			require.Equal(t, tt.expectedOriginalProposal, tt.proposal)
		})
	}
}
