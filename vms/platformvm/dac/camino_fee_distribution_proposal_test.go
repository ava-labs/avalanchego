// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestFeeDistributionProposalVerify(t *testing.T) {
	tests := map[string]struct {
		proposal         *FeeDistributionProposal
		expectedProposal *FeeDistributionProposal
		expectedErr      error
	}{
		"No options": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{},
			},
			expectedErr: errNoOptions,
		},
		"To many options": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {300000, 300000, 400000}, {400000, 400000, 200000}},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {300000, 300000, 400000}, {400000, 400000, 200000}},
			},
			expectedErr: errWrongOptionsCount,
		},
		"End-time is equal to start-time": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     100,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {300000, 300000, 400000}},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     100,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {300000, 300000, 400000}},
			},
			expectedErr: errEndNotAfterStart,
		},
		"End-time is less than start-time": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     99,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {300000, 300000, 400000}},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     99,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {300000, 300000, 400000}},
			},
			expectedErr: errEndNotAfterStart,
		},
		"Fee distribution sum is less, than 100%": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{400000}, {200000}, {390000}},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{400000}, {200000}, {390000}},
			},
			expectedErr: errWrongFeeDistribution,
		},
		"Fee distribution sum is greater, than 100%": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{400000}, {220000}, {390000}},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{400000}, {220000}, {390000}},
			},
			expectedErr: errWrongFeeDistribution,
		},
		"Not unique fee option": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {100000, 100000, 800000}},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {100000, 100000, 800000}},
			},
			expectedErr: errNotUniqueOption,
		},
		"OK: 1 option": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}},
			},
		},
		"OK: 3 options": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {300000, 300000, 400000}},
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{100000, 100000, 800000}, {200000, 200000, 600000}, {300000, 300000, 400000}},
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

func TestFeeDistributionProposalCreateProposalState(t *testing.T) {
	tests := map[string]struct {
		proposal              *FeeDistributionProposal
		allowedVoters         []ids.ShortID
		expectedProposalState ProposalState
		expectedProposal      *FeeDistributionProposal
	}{
		"OK: even number of allowed voters": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}, {0, 100, 0}},
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
			expectedProposalState: &FeeDistributionProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{40, 30, 30}},
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}},
						{Value: [FeeDistributionFractionsCount]uint64{0, 100, 0}},
					},
				},
				TotalAllowedVoters: 4,
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}, {0, 100, 0}},
			},
		},
		"OK: odd number of allowed voters": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}, {0, 100, 0}},
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
			expectedProposalState: &FeeDistributionProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{40, 30, 30}},
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}},
						{Value: [FeeDistributionFractionsCount]uint64{0, 100, 0}},
					},
				},
				TotalAllowedVoters: 5,
			},
			expectedProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}, {0, 100, 0}},
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

func TestFeeDistributionProposalStateAddVote(t *testing.T) {
	voterAddr1 := ids.ShortID{1}
	voterAddr2 := ids.ShortID{2}
	voterAddr3 := ids.ShortID{3}

	tests := map[string]struct {
		proposal                 *FeeDistributionProposalState
		voterAddr                ids.ShortID
		vote                     Vote
		expectedUpdatedProposal  ProposalState
		expectedOriginalProposal *FeeDistributionProposalState
		expectedErr              error
	}{
		"Wrong vote type": {
			proposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &DummyVote{}, // not *SimpleVote
			expectedOriginalProposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			expectedErr: ErrWrongVote,
		},
		"Wrong vote option index": {
			proposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 3},
			expectedOriginalProposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			expectedErr: ErrWrongVote,
		},
		"Not allowed to vote on this proposal": {
			proposal: &FeeDistributionProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{{Value: [FeeDistributionFractionsCount]uint64{100, 0, 0}}},
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 0},
			expectedOriginalProposal: &FeeDistributionProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{{Value: [FeeDistributionFractionsCount]uint64{100, 0, 0}}},
				},
			},
			expectedErr: ErrNotAllowedToVoteOnProposal,
		},
		"OK: adding vote to not voted option": {
			proposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &SimpleVote{OptionIndex: 1},
			expectedUpdatedProposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 1}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
				},
			},
			expectedOriginalProposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
		},
		"OK: adding vote to already voted option": {
			proposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &SimpleVote{OptionIndex: 2},
			expectedUpdatedProposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 2}, // 2
					},
				},
			},
			expectedOriginalProposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{10, 10, 80}, Weight: 2}, // 0
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 0}, // 1
						{Value: [FeeDistributionFractionsCount]uint64{30, 30, 40}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
		},
		"OK: voter addr in the middle of allowedVoters array": {
			proposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr2, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{{Value: [FeeDistributionFractionsCount]uint64{100, 0, 0}}},
				},
			},
			voterAddr: voterAddr2,
			vote:      &SimpleVote{OptionIndex: 0},
			expectedUpdatedProposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{{Value: [FeeDistributionFractionsCount]uint64{100, 0, 0}, Weight: 1}},
				},
			},
			expectedOriginalProposal: &FeeDistributionProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr2, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{{Value: [FeeDistributionFractionsCount]uint64{100, 0, 0}}},
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

func TestFeeDistributionProposalCreateFinishedProposalState(t *testing.T) {
	tests := map[string]struct {
		proposal                 *FeeDistributionProposal
		optionIndex              uint32
		expectedProposalState    ProposalState
		expectedOriginalProposal *FeeDistributionProposal
		expectedErr              error
	}{
		"Fail: option 2 out of bounds": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}},
			},
			optionIndex: 2,
			expectedOriginalProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}},
			},
			expectedErr: errWrongOptionIndex,
		},
		"OK: option 0": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}, {0, 100, 0}},
			},
			optionIndex: 0,
			expectedProposalState: &FeeDistributionProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{40, 30, 30}, Weight: 1},
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}},
						{Value: [FeeDistributionFractionsCount]uint64{0, 100, 0}},
					},
				},
			},
			expectedOriginalProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}, {0, 100, 0}},
			},
		},
		"OK: option 1": {
			proposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}, {0, 100, 0}},
			},
			optionIndex: 1,
			expectedProposalState: &FeeDistributionProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{40, 30, 30}},
						{Value: [FeeDistributionFractionsCount]uint64{20, 20, 60}, Weight: 1},
						{Value: [FeeDistributionFractionsCount]uint64{0, 100, 0}},
					},
				},
			},
			expectedOriginalProposal: &FeeDistributionProposal{
				Start:   100,
				End:     101,
				Options: [][FeeDistributionFractionsCount]uint64{{40, 30, 30}, {20, 20, 60}, {0, 100, 0}},
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

func TestFeeDistributionProposalStateIsSuccessful(t *testing.T) {
	tests := map[string]struct {
		proposal                 *FeeDistributionProposalState
		expectedSuccessful       bool
		expectedOriginalProposal *FeeDistributionProposalState
	}{
		"Not successful: most voted weight is less, than 50% of votes": {
			proposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 20},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 21},
						{Value: [FeeDistributionFractionsCount]uint64{3}, Weight: 10},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedSuccessful: false,
			expectedOriginalProposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 20},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 21},
						{Value: [FeeDistributionFractionsCount]uint64{3}, Weight: 10},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Not successful: total voted weight is less, than 50% of total allowed voters": {
			proposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 25},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 26},
					},
				},
				TotalAllowedVoters: 102,
			},
			expectedSuccessful: false,
			expectedOriginalProposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 25},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 26},
					},
				},
				TotalAllowedVoters: 102,
			},
		},
		"Successful": {
			proposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 25},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 26},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedSuccessful: true,
			expectedOriginalProposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 25},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 26},
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

func TestFeeDistributionProposalStateCanBeFinished(t *testing.T) {
	tests := map[string]struct {
		proposal                 *FeeDistributionProposalState
		expectedCanBeFinished    bool
		expectedOriginalProposal *FeeDistributionProposalState
	}{
		"Can not be finished: most voted weight is less than 50% of total allowed voters, not everyone had voted and some options could still reach 50%+ of votes": {
			proposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Can be finished: everyone had voted": {
			proposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 50},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 50},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Can be finished: most voted weight is greater than 50% of total allowed voters": {
			proposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 51},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 51},
					},
				},
				TotalAllowedVoters: 100,
			},
		},
		"Can be finished: no option can reach 50%+ of votes": {
			proposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 30},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 30},
						{Value: [FeeDistributionFractionsCount]uint64{3}, Weight: 30},
					},
				},
				TotalAllowedVoters: 100,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &FeeDistributionProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[FeeDistributionFractionsCount]uint64]{
					Options: []SimpleVoteOption[[FeeDistributionFractionsCount]uint64]{
						{Value: [FeeDistributionFractionsCount]uint64{1}, Weight: 30},
						{Value: [FeeDistributionFractionsCount]uint64{2}, Weight: 30},
						{Value: [FeeDistributionFractionsCount]uint64{3}, Weight: 30},
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
