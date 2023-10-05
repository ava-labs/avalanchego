// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestAddProposalIDToFinish(t *testing.T) {
	proposalID1 := ids.ID{1}
	proposalID2 := ids.ID{2}
	proposalID3 := ids.ID{3}

	tests := map[string]struct {
		caminoState         *caminoState
		proposalID          ids.ID
		expectedCaminoState *caminoState
	}{
		"OK": {
			proposalID: proposalID3,
			caminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID1: true,
						proposalID2: true,
					},
				},
			},
			expectedCaminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID1: true,
						proposalID2: true,
						proposalID3: true,
					},
				},
			},
		},
		"OK: already exist": {
			proposalID: proposalID2,
			caminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID1: true,
						proposalID2: true,
					},
				},
			},
			expectedCaminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID1: true,
						proposalID2: true,
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.caminoState.AddProposalIDToFinish(tt.proposalID)
			require.Equal(t, tt.expectedCaminoState, tt.caminoState)
		})
	}
}

func TestRemoveProposalIDToFinish(t *testing.T) {
	proposalID1 := ids.ID{1}
	proposalID2 := ids.ID{2}
	proposalID3 := ids.ID{3}

	tests := map[string]struct {
		caminoState         *caminoState
		proposalID          ids.ID
		expectedCaminoState *caminoState
	}{
		"OK": {
			proposalID: proposalID3,
			caminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID1: false,
						proposalID2: false,
					},
				},
			},
			expectedCaminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID1: false,
						proposalID2: false,
						proposalID3: false,
					},
				},
			},
		},
		"OK: not exist": {
			proposalID: proposalID2,
			caminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID1: false,
						proposalID2: false,
					},
				},
			},
			expectedCaminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID1: false,
						proposalID2: false,
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.caminoState.RemoveProposalIDToFinish(tt.proposalID)
			require.Equal(t, tt.expectedCaminoState, tt.caminoState)
		})
	}
}

func TestGetProposalIDsToFinish(t *testing.T) {
	proposalID1 := ids.ID{1}
	proposalID2 := ids.ID{2}
	proposalID3 := ids.ID{3}
	proposalID4 := ids.ID{4}
	proposalID5 := ids.ID{5}
	proposalID6 := ids.ID{6}

	tests := map[string]struct {
		caminoState                 *caminoState
		expectedCaminoState         *caminoState
		expectedProposalIDsToFinish []ids.ID
		expectedErr                 error
	}{
		"OK: no proposals to finish": {
			caminoState:                 &caminoState{caminoDiff: &caminoDiff{}},
			expectedCaminoState:         &caminoState{caminoDiff: &caminoDiff{}},
			expectedProposalIDsToFinish: nil,
		},
		"OK: no new proposals to finish": {
			caminoState: &caminoState{
				caminoDiff:          &caminoDiff{},
				proposalIDsToFinish: []ids.ID{proposalID1, proposalID2},
			},
			expectedCaminoState: &caminoState{
				caminoDiff:          &caminoDiff{},
				proposalIDsToFinish: []ids.ID{proposalID1, proposalID2},
			},
			expectedProposalIDsToFinish: []ids.ID{proposalID1, proposalID2},
		},
		"OK: only new proposals to finish": {
			caminoState: &caminoState{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: true,
					proposalID2: true,
				},
			}},
			expectedCaminoState: &caminoState{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: true,
					proposalID2: true,
				},
			}},
			expectedProposalIDsToFinish: []ids.ID{proposalID1, proposalID2},
		},
		"OK": {
			caminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID2: false,
						proposalID4: true,
						proposalID5: false,
						proposalID6: true,
					},
				},
				proposalIDsToFinish: []ids.ID{proposalID1, proposalID2, proposalID3},
			},
			expectedCaminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedProposalIDsToFinish: map[ids.ID]bool{
						proposalID2: false,
						proposalID4: true,
						proposalID5: false,
						proposalID6: true,
					},
				},
				proposalIDsToFinish: []ids.ID{proposalID1, proposalID2, proposalID3},
			},
			expectedProposalIDsToFinish: []ids.ID{proposalID1, proposalID3, proposalID4, proposalID6},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			proposalIDsToFinish, err := tt.caminoState.GetProposalIDsToFinish()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedProposalIDsToFinish, proposalIDsToFinish)
			require.Equal(t, tt.expectedCaminoState, tt.caminoState)
		})
	}
}
