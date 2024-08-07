// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestGeneralProposalVerify(t *testing.T) {
	tests := map[string]struct {
		proposal         *GeneralProposal
		expectedProposal *GeneralProposal
		expectedErr      error
	}{
		"No options": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     101,
				Options: [][]byte{},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     101,
				Options: [][]byte{},
			},
			expectedErr: errNoOptions,
		},
		"To many options": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     101,
				Options: [][]byte{{1}, {2}, {3}, {4}},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     101,
				Options: [][]byte{{1}, {2}, {3}, {4}},
			},
			expectedErr: errWrongOptionsCount,
		},
		"End-time is equal to start-time": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     100,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     100,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedErr: errEndNotAfterStart,
		},
		"End-time is less than start-time": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     99,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     99,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedErr: errEndNotAfterStart,
		},
		"To small duration": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration - 1,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration - 1,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedErr: errWrongDuration,
		},
		"To big duration": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     100 + generalProposalMaxDuration + 1,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     100 + generalProposalMaxDuration + 1,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedErr: errWrongDuration,
		},
		"Option is bigger than allowed": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration,
				Options: [][]byte{make([]byte, generalProposalMaxOptionSize+1)},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration,
				Options: [][]byte{make([]byte, generalProposalMaxOptionSize+1)},
			},
			expectedErr: errGeneralProposalOptionIsToBig,
		},
		"Not unique option": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration,
				Options: [][]byte{{1}, {2}, {1}},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration,
				Options: [][]byte{{1}, {2}, {1}},
			},
			expectedErr: errNotUniqueOption,
		},
		"OK: 1 option": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration,
				Options: [][]byte{make([]byte, generalProposalMaxOptionSize)},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration,
				Options: [][]byte{make([]byte, generalProposalMaxOptionSize)},
			},
		},
		"OK: 3 options": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration,
				Options: [][]byte{{1}, {2}, {3}},
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     100 + GeneralProposalMinDuration,
				Options: [][]byte{{1}, {2}, {3}},
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

func TestGeneralProposalCreateProposalState(t *testing.T) {
	tests := map[string]struct {
		proposal              *GeneralProposal
		allowedVoters         []ids.ShortID
		expectedProposalState ProposalState
		expectedProposal      *GeneralProposal
	}{
		"OK: even number of allowed voters": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     101,
				Options: [][]byte{{1}, {2}, {3}},
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
			expectedProposalState: &GeneralProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}},
						{Value: []byte{2}},
						{Value: []byte{3}},
					},
				},
				TotalAllowedVoters: 4,
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     101,
				Options: [][]byte{{1}, {2}, {3}},
			},
		},
		"OK: odd number of allowed voters": {
			proposal: &GeneralProposal{
				Start:   100,
				End:     101,
				Options: [][]byte{{1}, {2}, {3}},
			},
			allowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
			expectedProposalState: &GeneralProposalState{
				Start:         100,
				End:           101,
				AllowedVoters: []ids.ShortID{{1}, {2}, {3}, {4}, {5}},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}},
						{Value: []byte{2}},
						{Value: []byte{3}},
					},
				},
				TotalAllowedVoters: 5,
			},
			expectedProposal: &GeneralProposal{
				Start:   100,
				End:     101,
				Options: [][]byte{{1}, {2}, {3}},
			},
		},
		"OK: configured thresholds and early finish": {
			proposal: &GeneralProposal{
				Start:                        100,
				End:                          101,
				Options:                      [][]byte{{1}, {2}, {3}},
				TotalVotedThresholdNominator: 390000,
				MostVotedThresholdNominator:  680000,
				AllowEarlyFinish:             true,
			},
			allowedVoters: make([]ids.ShortID, 100),
			expectedProposalState: &GeneralProposalState{
				Start: 100,
				End:   101,
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}},
						{Value: []byte{2}},
						{Value: []byte{3}},
					},
				},
				AllowedVoters:               make([]ids.ShortID, 100),
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         39,
				MostVotedThresholdNominator: 680000,
				AllowEarlyFinish:            true,
			},
			expectedProposal: &GeneralProposal{
				Start:                        100,
				End:                          101,
				Options:                      [][]byte{{1}, {2}, {3}},
				TotalVotedThresholdNominator: 390000,
				MostVotedThresholdNominator:  680000,
				AllowEarlyFinish:             true,
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

func TestGeneralProposalStateAddVote(t *testing.T) {
	voterAddr1 := ids.ShortID{1}
	voterAddr2 := ids.ShortID{2}
	voterAddr3 := ids.ShortID{3}

	tests := map[string]struct {
		proposal                 *GeneralProposalState
		voterAddr                ids.ShortID
		vote                     Vote
		expectedUpdatedProposal  ProposalState
		expectedOriginalProposal *GeneralProposalState
		expectedErr              error
	}{
		"Wrong vote type": {
			proposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &DummyVote{}, // not *SimpleVote
			expectedOriginalProposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			expectedErr: ErrWrongVote,
		},
		"Wrong vote option index": {
			proposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 3},
			expectedOriginalProposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			expectedErr: ErrWrongVote,
		},
		"Not allowed to vote on this proposal": {
			proposal: &GeneralProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{{Value: []byte{1}}},
				},
			},
			voterAddr: ids.ShortID{3},
			vote:      &SimpleVote{OptionIndex: 0},
			expectedOriginalProposal: &GeneralProposalState{
				AllowedVoters: []ids.ShortID{{1}, {2}},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{{Value: []byte{1}}},
				},
			},
			expectedErr: ErrNotAllowedToVoteOnProposal,
		},
		"OK: adding vote to not voted option": {
			proposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &SimpleVote{OptionIndex: 1},
			expectedUpdatedProposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 1}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
				},
			},
			expectedOriginalProposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
		},
		"OK: adding vote to already voted option": {
			proposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
			voterAddr: voterAddr1,
			vote:      &SimpleVote{OptionIndex: 2},
			expectedUpdatedProposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 2}, // 2
					},
				},
			},
			expectedOriginalProposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 2}, // 0
						{Value: []byte{2}, Weight: 0}, // 1
						{Value: []byte{3}, Weight: 1}, // 2
					},
					mostVotedWeight:      2,
					mostVotedOptionIndex: 0,
					unambiguous:          true,
				},
			},
		},
		"OK: voter addr in the middle of allowedVoters array": {
			proposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr2, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{{Value: []byte{1}}},
				},
			},
			voterAddr: voterAddr2,
			vote:      &SimpleVote{OptionIndex: 0},
			expectedUpdatedProposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{{Value: []byte{1}, Weight: 1}},
				},
			},
			expectedOriginalProposal: &GeneralProposalState{
				Start:              100,
				End:                101,
				TotalAllowedVoters: 555,
				AllowedVoters:      []ids.ShortID{voterAddr1, voterAddr2, voterAddr3},
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{{Value: []byte{1}}},
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

func TestGeneralProposalStateIsSuccessful(t *testing.T) {
	tests := map[string]struct {
		proposal                 *GeneralProposalState
		expectedSuccessful       bool
		expectedOriginalProposal *GeneralProposalState
	}{
		"Not successful: most voted weight is less, than most voted threshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 25},
						{Value: []byte{2}, Weight: 26},
					},
				},
				TotalVotedThreshold:         50,
				MostVotedThresholdNominator: 510000, // 50%
			},
			expectedSuccessful: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 25},
						{Value: []byte{2}, Weight: 26},
					},
				},
				TotalVotedThreshold:         50,
				MostVotedThresholdNominator: 510000, // 50%
			},
		},
		"Not successful: total voted weight is less, than total voted threshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 25},
						{Value: []byte{2}, Weight: 26},
					},
				},
				TotalVotedThreshold:         51,
				MostVotedThresholdNominator: 500000, // 50%
			},
			expectedSuccessful: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 25},
						{Value: []byte{2}, Weight: 26},
					},
				},
				TotalVotedThreshold:         51,
				MostVotedThresholdNominator: 500000, // 50%
			},
		},
		"Successful": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 25},
						{Value: []byte{2}, Weight: 26},
					},
				},
				TotalVotedThreshold:         50,
				MostVotedThresholdNominator: 500000, // 50%
			},
			expectedSuccessful: true,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 25},
						{Value: []byte{2}, Weight: 26},
					},
				},
				TotalVotedThreshold:         50,
				MostVotedThresholdNominator: 500000, // 50%
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

func TestGeneralProposalStateCanBeFinished(t *testing.T) {
	tests := map[string]struct {
		proposal                 *GeneralProposalState
		expectedCanBeFinished    bool
		expectedOriginalProposal *GeneralProposalState
	}{
		"No: mostVotedWeight <= remainingVotes + secondMostVotedWeight, while mostVotedWeight > mostVotedThreshold AND voted > p.TotalVotedThreshold; not everyone had voted and some option can reach mostVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 18},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         78,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            true,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 18},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         78,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            true,
			},
		},
		"No: mostVotedWeight <= mostVotedThreshold, while mostVotedWeight > remainingVotes + secondMostVotedWeight AND voted > p.TotalVotedThreshold; not everyone had voted and some option can reach mostVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 20},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				}, // 81 * k = 41
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         80,
				MostVotedThresholdNominator: 506173,
				AllowEarlyFinish:            true,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 20},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         80,
				MostVotedThresholdNominator: 506173,
				AllowEarlyFinish:            true,
			},
		},
		"No: voted <= p.TotalVotedThreshold, while mostVotedWeight > remainingVotes + secondMostVotedWeight AND mostVotedWeight > mostVotedThreshold; not everyone had voted and some option can reach mostVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 20},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         81,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            true,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 20},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         81,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            true,
			},
		},
		"No: just one option, but voted <= p.TotalVotedThreshold and not everyone had voted": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 21},
					},
				},
				TotalAllowedVoters:  100,
				TotalVotedThreshold: 21,
				AllowEarlyFinish:    true,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 21},
					},
				},
				TotalAllowedVoters:  100,
				TotalVotedThreshold: 21,
				AllowEarlyFinish:    true,
			},
		},
		"Not allowed: no option can reach mostVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 29},
						{Value: []byte{2}, Weight: 29},
						{Value: []byte{3}, Weight: 29},
					},
				},
				TotalAllowedVoters:          100,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            false,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 29},
						{Value: []byte{2}, Weight: 29},
						{Value: []byte{3}, Weight: 29},
					},
				},
				TotalAllowedVoters:          100,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            false,
			},
		},
		"Not allowed: everyone had voted": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 50},
						{Value: []byte{2}, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
				AllowEarlyFinish:   false,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 50},
						{Value: []byte{2}, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
				AllowEarlyFinish:   false,
			},
		},
		"Not allowed: mostVotedWeight > remainingVotes + secondMostVotedWeight AND mostVotedWeight > mostVotedThreshold AND voted > p.TotalVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 20},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         80,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            false,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 20},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         80,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            false,
			},
		},
		"Not allowed: just one option AND voted > p.TotalVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 21},
					},
				},
				TotalAllowedVoters:  100,
				TotalVotedThreshold: 20,
				AllowEarlyFinish:    false,
			},
			expectedCanBeFinished: false,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 21},
					},
				},
				TotalAllowedVoters:  100,
				TotalVotedThreshold: 20,
				AllowEarlyFinish:    false,
			},
		},
		"Yes: no option can reach mostVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 29},
						{Value: []byte{2}, Weight: 29},
						{Value: []byte{3}, Weight: 29},
					},
				},
				TotalAllowedVoters:          100,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            true,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 29},
						{Value: []byte{2}, Weight: 29},
						{Value: []byte{3}, Weight: 29},
					},
				},
				TotalAllowedVoters:          100,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            true,
			},
		},
		"Yes: everyone had voted": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 50},
						{Value: []byte{2}, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
				AllowEarlyFinish:   true,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 50},
						{Value: []byte{2}, Weight: 50},
					},
				},
				TotalAllowedVoters: 100,
				AllowEarlyFinish:   true,
			},
		},
		"Yes: mostVotedWeight > remainingVotes + secondMostVotedWeight AND mostVotedWeight > mostVotedThreshold AND voted > p.TotalVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 20},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         80,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            true,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 20},
						{Value: []byte{2}, Weight: 41},
						{Value: []byte{3}, Weight: 20},
					},
				},
				TotalAllowedVoters:          100,
				TotalVotedThreshold:         80,
				MostVotedThresholdNominator: 500000,
				AllowEarlyFinish:            true,
			},
		},
		"Yes: just one option AND voted > p.TotalVotedThreshold": {
			proposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 21},
					},
				},
				TotalAllowedVoters:  100,
				TotalVotedThreshold: 20,
				AllowEarlyFinish:    true,
			},
			expectedCanBeFinished: true,
			expectedOriginalProposal: &GeneralProposalState{
				SimpleVoteOptions: SimpleVoteOptions[[]byte]{
					Options: []SimpleVoteOption[[]byte]{
						{Value: []byte{1}, Weight: 21},
					},
				},
				TotalAllowedVoters:  100,
				TotalVotedThreshold: 20,
				AllowEarlyFinish:    true,
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
