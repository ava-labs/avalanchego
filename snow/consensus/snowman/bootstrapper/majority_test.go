// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestNewMajority(t *testing.T) {
	majority := NewMajority(
		logging.NoLog{}, // log
		map[ids.NodeID]uint64{
			nodeID0: 1,
			nodeID1: 1,
		}, // nodeWeights
		2, // maxOutstanding
	)

	expectedMajority := &Majority{
		requests: requests{
			maxOutstanding: 2,
			pendingSend:    set.Of(nodeID0, nodeID1),
		},
		log: logging.NoLog{},
		nodeWeights: map[ids.NodeID]uint64{
			nodeID0: 1,
			nodeID1: 1,
		},
		received: make(map[ids.ID]uint64),
	}
	require.Equal(t, expectedMajority, majority)
}

func TestMajorityGetPeers(t *testing.T) {
	tests := []struct {
		name          string
		majority      Poll
		expectedState Poll
		expectedPeers set.Set[ids.NodeID]
	}{
		{
			name: "max outstanding",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				received: make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				received: make(map[ids.ID]uint64),
			},
			expectedPeers: nil,
		},
		{
			name: "send until max outstanding",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Of(nodeID0, nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				received: make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Set[ids.NodeID]{},
					outstanding:    set.Of(nodeID0, nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				received: make(map[ids.ID]uint64),
			},
			expectedPeers: set.Of(nodeID0, nodeID1),
		},
		{
			name: "send until no more to send",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Of(nodeID0),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
				},
				received: make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Set[ids.NodeID]{},
					outstanding:    set.Of(nodeID0),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
				},
				received: make(map[ids.ID]uint64),
			},
			expectedPeers: set.Of(nodeID0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			peers := test.majority.GetPeers(t.Context())
			require.Equal(test.expectedState, test.majority)
			require.Equal(test.expectedPeers, peers)
		})
	}
}

func TestMajorityRecordOpinion(t *testing.T) {
	tests := []struct {
		name          string
		majority      Poll
		nodeID        ids.NodeID
		blkIDs        set.Set[ids.ID]
		expectedState Poll
		expectedErr   error
	}{
		{
			name: "unexpected response",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				received: make(map[ids.ID]uint64),
			},
			nodeID: nodeID0,
			blkIDs: nil,
			expectedState: &Majority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				received: make(map[ids.ID]uint64),
			},
			expectedErr: nil,
		},
		{
			name: "unfinished after response",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 2,
					nodeID1: 3,
				},
				received: make(map[ids.ID]uint64),
			},
			nodeID: nodeID1,
			blkIDs: set.Of(blkID0),
			expectedState: &Majority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Set[ids.NodeID]{},
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 2,
					nodeID1: 3,
				},
				received: map[ids.ID]uint64{
					blkID0: 3,
				},
			},
			expectedErr: nil,
		},
		{
			name: "overflow during response",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: math.MaxUint64,
				},
				received: map[ids.ID]uint64{
					blkID0: 1,
				},
			},
			nodeID: nodeID1,
			blkIDs: set.Of(blkID0),
			expectedState: &Majority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Set[ids.NodeID]{},
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: math.MaxUint64,
				},
				received: map[ids.ID]uint64{
					blkID0: 1,
				},
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "overflow during final response",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: math.MaxUint64,
				},
				received: make(map[ids.ID]uint64),
			},
			nodeID: nodeID1,
			blkIDs: set.Of(blkID0),
			expectedState: &Majority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Set[ids.NodeID]{},
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: math.MaxUint64,
				},
				received: map[ids.ID]uint64{
					blkID0: math.MaxUint64,
				},
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "finished after response",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Of(nodeID2),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
					nodeID2: 1,
				},
				received: map[ids.ID]uint64{
					blkID0: 1,
					blkID1: 1,
				},
			},
			nodeID: nodeID2,
			blkIDs: set.Of(blkID1),
			expectedState: &Majority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Set[ids.NodeID]{},
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
					nodeID2: 1,
				},
				received: map[ids.ID]uint64{
					blkID0: 1,
					blkID1: 2,
				},
				accepted: []ids.ID{blkID1},
			},
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.majority.RecordOpinion(t.Context(), test.nodeID, test.blkIDs)
			require.Equal(test.expectedState, test.majority)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestMajorityResult(t *testing.T) {
	tests := []struct {
		name              string
		majority          Poll
		expectedAccepted  []ids.ID
		expectedFinalized bool
	}{
		{
			name: "not finalized",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Of(nodeID1),
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				received: make(map[ids.ID]uint64),
				accepted: nil,
			},
			expectedAccepted:  nil,
			expectedFinalized: false,
		},
		{
			name: "finalized",
			majority: &Majority{
				requests: requests{
					maxOutstanding: 1,
				},
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				received: map[ids.ID]uint64{
					blkID0: 2,
				},
				accepted: []ids.ID{blkID0},
			},
			expectedAccepted:  []ids.ID{blkID0},
			expectedFinalized: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			accepted, finalized := test.majority.Result(t.Context())
			require.Equal(test.expectedAccepted, accepted)
			require.Equal(test.expectedFinalized, finalized)
		})
	}
}
