// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestBaseFeeProposalVerify(t *testing.T) {
	tests := map[string]struct {
		proposal         *BaseFeeProposal
		expectedProposal *BaseFeeProposal
		expectedErr      error
	}{
		"No options": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{},
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{},
			},
			expectedErr: errNoOptions,
		},
		"To many options": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1, 2, 3, 4},
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1, 2, 3, 4},
			},
			expectedErr: errWrongOptionsCount,
		},
		"End-time is equal to start-time": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     100,
				Options: []uint64{1, 2, 3},
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     100,
				Options: []uint64{1, 2, 3},
			},
			expectedErr: errEndNotAfterStart,
		},
		"End-time is less than start-time": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     99,
				Options: []uint64{1, 2, 3},
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     99,
				Options: []uint64{1, 2, 3},
			},
			expectedErr: errEndNotAfterStart,
		},
		"Zero fee option": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1, 0, 3},
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1, 0, 3},
			},
			expectedErr: errZeroFee,
		},
		"Not unique fee option": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1, 2, 1},
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1, 2, 1},
			},
			expectedErr: errNotUniqueOption,
		},
		"OK: 1 option": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1},
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1},
			},
		},
		"OK: 3 options": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1, 2, 3},
			},
			expectedProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{1, 2, 3},
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

func TestBaseFeeProposalCreateFinishedProposalState(t *testing.T) {
	tests := map[string]struct {
		proposal                 *BaseFeeProposal
		optionIndex              uint32
		expectedProposalState    ProposalState
		expectedOriginalProposal *BaseFeeProposal
		expectedErr              error
	}{
		"Fail: option 2 out of bounds": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{10, 20},
			},
			optionIndex: 2,
			expectedOriginalProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{10, 20},
			},
			expectedErr: errWrongOptionIndex,
		},
		"OK: option 0": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{10, 20},
			},
			optionIndex: 0,
			expectedProposalState: &BaseFeeProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10, Weight: 1},
						{Value: 20},
					},
				},
			},
			expectedOriginalProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{10, 20},
			},
		},
		"OK: option 1": {
			proposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{10, 20},
			},
			optionIndex: 1,
			expectedProposalState: &BaseFeeProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 10},
						{Value: 20, Weight: 1},
					},
				},
			},
			expectedOriginalProposal: &BaseFeeProposal{
				Start:   100,
				End:     101,
				Options: []uint64{10, 20},
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

func TestBaseFeeProposalStateIsSuccessful(t *testing.T) {
	tests := map[string]struct {
		proposal                 *BaseFeeProposalState
		expectedSuccessful       bool
		expectedOriginalProposal *BaseFeeProposalState
	}{
		"Not successful: most voted weight is less, than 50% of votes": {
			proposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 20},
						{Value: 2, Weight: 21},
						{Value: 3, Weight: 10},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedSuccessful: false,
			expectedOriginalProposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 20},
						{Value: 2, Weight: 21},
						{Value: 3, Weight: 10},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Not successful: total voted weight is less, than 50% of total allowed voters": {
			proposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 25},
						{Value: 2, Weight: 26},
					},
				},
				TotalAllowedVoters: 102,
			},
			expectedSuccessful: false,
			expectedOriginalProposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 25},
						{Value: 2, Weight: 26},
					},
				},
				TotalAllowedVoters: 102,
			},
		},
		"Successful": {
			proposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 25},
						{Value: 2, Weight: 26},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedSuccessful: true,
			expectedOriginalProposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 25},
						{Value: 2, Weight: 26},
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

func TestBaseFeeProposalStateCanBeFinished(t *testing.T) {
	tests := map[string]struct {
		proposal                 *BaseFeeProposalState
		expectedCanBeFinished    bool
		expectedOriginalProposal *BaseFeeProposalState
	}{
		"Can not be finished: most voted weight is less than 50% of total allowed voters, not everyone had voted and some options could still reach 50%+ of votes": {
			proposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Can be finished: everyone had voted": {
			proposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 50},
						{Value: 2, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 50},
						{Value: 2, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Can be finished: most voted weight is greater than 50% of total allowed voters": {
			proposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 51},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 51},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Can be finished: no option can reach 50%+ of votes": {
			proposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 30},
						{Value: 2, Weight: 30},
						{Value: 3, Weight: 30},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &BaseFeeProposalState{
				SimpleVoteOptions: SimpleVoteOptions[uint64]{
					Options: []SimpleVoteOption[uint64]{
						{Value: 1, Weight: 30},
						{Value: 2, Weight: 30},
						{Value: 3, Weight: 30},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expectedCanBeFinished, tt.proposal.CanBeFinished())
			require.Equal(t, tt.expectedOriginalProposal, tt.proposal)
		})
	}
}
