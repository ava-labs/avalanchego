// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	nodeID0 = ids.GenerateTestNodeID()
	nodeID1 = ids.GenerateTestNodeID()
	nodeID2 = ids.GenerateTestNodeID()

	blkID0 = ids.GenerateTestID()
	blkID1 = ids.GenerateTestID()
)

func TestNew(t *testing.T) {
	bootstrapper := New(
		logging.NoLog{}, // log
		set.Of(nodeID0), // frontierNodes
		map[ids.NodeID]uint64{
			nodeID0: 1,
			nodeID1: 1,
		}, // nodeWeights
		2, // maxOutstanding
	)

	expectedBootstrapper := &Majority{
		log: logging.NoLog{},
		nodeWeights: map[ids.NodeID]uint64{
			nodeID0: 1,
			nodeID1: 1,
		},
		maxOutstanding:              2,
		pendingSendAcceptedFrontier: set.Of(nodeID0),
		pendingSendAccepted:         set.Of(nodeID0, nodeID1),
		receivedAccepted:            make(map[ids.ID]uint64),
	}
	require.Equal(t, expectedBootstrapper, bootstrapper)
}

func TestMajorityGetAcceptedFrontiersToSend(t *testing.T) {
	tests := []struct {
		name          string
		bootstrapper  Bootstrapper
		expectedState Bootstrapper
		expectedPeers set.Set[ids.NodeID]
	}{
		{
			name: "max outstanding",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				pendingSendAcceptedFrontier: set.Of(nodeID0),
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				pendingSendAcceptedFrontier: set.Of(nodeID0),
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedPeers: nil,
		},
		{
			name: "send until max outstanding",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              2,
				pendingSendAcceptedFrontier: set.Of(nodeID0, nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              2,
				pendingSendAcceptedFrontier: set.Set[ids.NodeID]{},
				outstandingAcceptedFrontier: set.Of(nodeID0, nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedPeers: set.Of(nodeID0, nodeID1),
		},
		{
			name: "send until no more to send",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
				},
				maxOutstanding:              2,
				pendingSendAcceptedFrontier: set.Of(nodeID0),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
				},
				maxOutstanding:              2,
				pendingSendAcceptedFrontier: set.Set[ids.NodeID]{},
				outstandingAcceptedFrontier: set.Of(nodeID0),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedPeers: set.Of(nodeID0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			peers := test.bootstrapper.GetAcceptedFrontiersToSend(context.Background())
			require.Equal(test.expectedState, test.bootstrapper)
			require.Equal(test.expectedPeers, peers)
		})
	}
}

func TestMajorityRecordAcceptedFrontier(t *testing.T) {
	tests := []struct {
		name          string
		bootstrapper  Bootstrapper
		nodeID        ids.NodeID
		blkIDs        []ids.ID
		expectedState Bootstrapper
	}{
		{
			name: "unexpected response",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				pendingSendAcceptedFrontier: set.Of(nodeID0),
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			nodeID: nodeID0,
			blkIDs: nil,
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				pendingSendAcceptedFrontier: set.Of(nodeID0),
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
		},
		{
			name: "unfinished after response",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				pendingSendAcceptedFrontier: set.Of(nodeID0),
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			nodeID: nodeID1,
			blkIDs: []ids.ID{blkID0},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				pendingSendAcceptedFrontier: set.Of(nodeID0),
				outstandingAcceptedFrontier: set.Set[ids.NodeID]{},
				receivedAcceptedFrontierSet: set.Of(blkID0),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
		},
		{
			name: "finished after response",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			nodeID: nodeID1,
			blkIDs: []ids.ID{blkID0},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				outstandingAcceptedFrontier: set.Set[ids.NodeID]{},
				receivedAcceptedFrontierSet: set.Of(blkID0),
				receivedAcceptedFrontier:    []ids.ID{blkID0},
				receivedAccepted:            make(map[ids.ID]uint64),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.bootstrapper.RecordAcceptedFrontier(context.Background(), test.nodeID, test.blkIDs...)
			require.Equal(t, test.expectedState, test.bootstrapper)
		})
	}
}

func TestMajorityGetAcceptedFrontier(t *testing.T) {
	tests := []struct {
		name                     string
		bootstrapper             Bootstrapper
		expectedAcceptedFrontier []ids.ID
		expectedFinalized        bool
	}{
		{
			name: "not finalized",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAcceptedFrontier:    nil,
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedAcceptedFrontier: nil,
			expectedFinalized:        false,
		},
		{
			name: "finalized",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:           1,
				receivedAcceptedFrontier: []ids.ID{blkID0},
			},
			expectedAcceptedFrontier: []ids.ID{blkID0},
			expectedFinalized:        true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			acceptedFrontier, finalized := test.bootstrapper.GetAcceptedFrontier(context.Background())
			require.Equal(test.expectedAcceptedFrontier, acceptedFrontier)
			require.Equal(test.expectedFinalized, finalized)
		})
	}
}

func TestMajorityGetAcceptedToSend(t *testing.T) {
	tests := []struct {
		name          string
		bootstrapper  Bootstrapper
		expectedState Bootstrapper
		expectedPeers set.Set[ids.NodeID]
	}{
		{
			name: "still fetching frontiers",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:              1,
				outstandingAcceptedFrontier: set.Of(nodeID1),
				receivedAccepted:            make(map[ids.ID]uint64),
			},
			expectedPeers: nil,
		},
		{
			name: "max outstanding",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:      1,
				pendingSendAccepted: set.Of(nodeID0),
				outstandingAccepted: set.Of(nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:      1,
				pendingSendAccepted: set.Of(nodeID0),
				outstandingAccepted: set.Of(nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			expectedPeers: nil,
		},
		{
			name: "send until max outstanding",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:      2,
				pendingSendAccepted: set.Of(nodeID0, nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:      2,
				pendingSendAccepted: set.Set[ids.NodeID]{},
				outstandingAccepted: set.Of(nodeID0, nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			expectedPeers: set.Of(nodeID0, nodeID1),
		},
		{
			name: "send until no more to send",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
				},
				maxOutstanding:      2,
				pendingSendAccepted: set.Of(nodeID0),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
				},
				maxOutstanding:      2,
				pendingSendAccepted: set.Set[ids.NodeID]{},
				outstandingAccepted: set.Of(nodeID0),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			expectedPeers: set.Of(nodeID0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			peers := test.bootstrapper.GetAcceptedToSend(context.Background())
			require.Equal(test.expectedState, test.bootstrapper)
			require.Equal(test.expectedPeers, peers)
		})
	}
}

func TestMajorityRecordAccepted(t *testing.T) {
	tests := []struct {
		name          string
		bootstrapper  Bootstrapper
		nodeID        ids.NodeID
		blkIDs        []ids.ID
		expectedState Bootstrapper
		expectedErr   error
	}{
		{
			name: "unexpected response",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:      1,
				pendingSendAccepted: set.Of(nodeID0),
				outstandingAccepted: set.Of(nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			nodeID: nodeID0,
			blkIDs: nil,
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:      1,
				pendingSendAccepted: set.Of(nodeID0),
				outstandingAccepted: set.Of(nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			expectedErr: nil,
		},
		{
			name: "unfinished after response",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 2,
					nodeID1: 3,
				},
				maxOutstanding:      1,
				pendingSendAccepted: set.Of(nodeID0),
				outstandingAccepted: set.Of(nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			nodeID: nodeID1,
			blkIDs: []ids.ID{blkID0},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 2,
					nodeID1: 3,
				},
				maxOutstanding:      1,
				pendingSendAccepted: set.Of(nodeID0),
				outstandingAccepted: set.Set[ids.NodeID]{},
				receivedAccepted: map[ids.ID]uint64{
					blkID0: 3,
				},
			},
			expectedErr: nil,
		},
		{
			name: "overflow during response",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: math.MaxUint64,
				},
				maxOutstanding:      1,
				outstandingAccepted: set.Of(nodeID1),
				receivedAccepted: map[ids.ID]uint64{
					blkID0: 1,
				},
			},
			nodeID: nodeID1,
			blkIDs: []ids.ID{blkID0},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: math.MaxUint64,
				},
				maxOutstanding:      1,
				outstandingAccepted: set.Set[ids.NodeID]{},
				receivedAccepted: map[ids.ID]uint64{
					blkID0: 1,
				},
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "overflow during final response",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: math.MaxUint64,
				},
				maxOutstanding:      1,
				outstandingAccepted: set.Of(nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
			},
			nodeID: nodeID1,
			blkIDs: []ids.ID{blkID0},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: math.MaxUint64,
				},
				maxOutstanding:      1,
				outstandingAccepted: set.Set[ids.NodeID]{},
				receivedAccepted: map[ids.ID]uint64{
					blkID0: math.MaxUint64,
				},
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "finished after response",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
					nodeID2: 1,
				},
				maxOutstanding:      1,
				outstandingAccepted: set.Of(nodeID2),
				receivedAccepted: map[ids.ID]uint64{
					blkID0: 1,
					blkID1: 1,
				},
			},
			nodeID: nodeID2,
			blkIDs: []ids.ID{blkID1},
			expectedState: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
					nodeID2: 1,
				},
				maxOutstanding:      1,
				outstandingAccepted: set.Set[ids.NodeID]{},
				receivedAccepted: map[ids.ID]uint64{
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

			err := test.bootstrapper.RecordAccepted(context.Background(), test.nodeID, test.blkIDs)
			require.Equal(test.expectedState, test.bootstrapper)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestMajorityGetAccepted(t *testing.T) {
	tests := []struct {
		name              string
		bootstrapper      Bootstrapper
		expectedAccepted  []ids.ID
		expectedFinalized bool
	}{
		{
			name: "not finalized",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding:      1,
				outstandingAccepted: set.Of(nodeID1),
				receivedAccepted:    make(map[ids.ID]uint64),
				accepted:            nil,
			},
			expectedAccepted:  nil,
			expectedFinalized: false,
		},
		{
			name: "finalized",
			bootstrapper: &Majority{
				log: logging.NoLog{},
				nodeWeights: map[ids.NodeID]uint64{
					nodeID0: 1,
					nodeID1: 1,
				},
				maxOutstanding: 1,
				receivedAccepted: map[ids.ID]uint64{
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

			accepted, finalized := test.bootstrapper.GetAccepted(context.Background())
			require.Equal(test.expectedAccepted, accepted)
			require.Equal(test.expectedFinalized, finalized)
		})
	}
}
