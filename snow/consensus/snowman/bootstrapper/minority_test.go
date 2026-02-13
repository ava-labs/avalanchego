// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestNewMinority(t *testing.T) {
	minority := NewMinority(
		logging.NoLog{}, // log
		set.Of(nodeID0), // frontierNodes
		2,               // maxOutstanding
	)

	expectedMinority := &Minority{
		requests: requests{
			maxOutstanding: 2,
			pendingSend:    set.Of(nodeID0),
		},
		log: logging.NoLog{},
	}
	require.Equal(t, expectedMinority, minority)
}

func TestMinorityGetPeers(t *testing.T) {
	tests := []struct {
		name          string
		minority      Poll
		expectedState Poll
		expectedPeers set.Set[ids.NodeID]
	}{
		{
			name: "max outstanding",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
			},
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
			},
			expectedPeers: nil,
		},
		{
			name: "send until max outstanding",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Of(nodeID0, nodeID1),
				},
				log: logging.NoLog{},
			},
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Set[ids.NodeID]{},
					outstanding:    set.Of(nodeID0, nodeID1),
				},
				log: logging.NoLog{},
			},
			expectedPeers: set.Of(nodeID0, nodeID1),
		},
		{
			name: "send until no more to send",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Of(nodeID0),
				},
				log: logging.NoLog{},
			},
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Set[ids.NodeID]{},
					outstanding:    set.Of(nodeID0),
				},
				log: logging.NoLog{},
			},
			expectedPeers: set.Of(nodeID0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			peers := test.minority.GetPeers(t.Context())
			require.Equal(test.expectedState, test.minority)
			require.Equal(test.expectedPeers, peers)
		})
	}
}

func TestMinorityRecordOpinion(t *testing.T) {
	tests := []struct {
		name          string
		minority      Poll
		nodeID        ids.NodeID
		blkIDs        set.Set[ids.ID]
		expectedState Poll
		expectedErr   error
	}{
		{
			name: "unexpected response",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
			},
			nodeID: nodeID0,
			blkIDs: nil,
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
			},
			expectedErr: nil,
		},
		{
			name: "unfinished after response",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
			},
			nodeID: nodeID1,
			blkIDs: set.Of(blkID0),
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Set[ids.NodeID]{},
				},
				log:         logging.NoLog{},
				receivedSet: set.Of(blkID0),
			},
			expectedErr: nil,
		},
		{
			name: "finished after response",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Of(nodeID2),
				},
				log: logging.NoLog{},
			},
			nodeID: nodeID2,
			blkIDs: set.Of(blkID1),
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Set[ids.NodeID]{},
				},
				log:         logging.NoLog{},
				receivedSet: set.Of(blkID1),
				received:    []ids.ID{blkID1},
			},
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.minority.RecordOpinion(t.Context(), test.nodeID, test.blkIDs)
			require.Equal(test.expectedState, test.minority)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestMinorityResult(t *testing.T) {
	tests := []struct {
		name              string
		minority          Poll
		expectedAccepted  []ids.ID
		expectedFinalized bool
	}{
		{
			name: "not finalized",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Of(nodeID1),
				},
				log:      logging.NoLog{},
				received: nil,
			},
			expectedAccepted:  nil,
			expectedFinalized: false,
		},
		{
			name: "finalized",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
				},
				log:         logging.NoLog{},
				receivedSet: set.Of(blkID0),
				received:    []ids.ID{blkID0},
			},
			expectedAccepted:  []ids.ID{blkID0},
			expectedFinalized: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			accepted, finalized := test.minority.Result(t.Context())
			require.Equal(test.expectedAccepted, accepted)
			require.Equal(test.expectedFinalized, finalized)
		})
	}
}
