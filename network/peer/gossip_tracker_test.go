// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	// peers
	p1 = ids.GenerateTestNodeID()
	p2 = ids.GenerateTestNodeID()
	p3 = ids.GenerateTestNodeID()

	// validators
	v1 = ValidatorID{
		NodeID: ids.GenerateTestNodeID(),
		TxID:   ids.GenerateTestID(),
	}
	v2 = ValidatorID{
		NodeID: ids.GenerateTestNodeID(),
		TxID:   ids.GenerateTestID(),
	}
	v3 = ValidatorID{
		NodeID: ids.GenerateTestNodeID(),
		TxID:   ids.GenerateTestID(),
	}
)

func TestGossipTracker_Contains(t *testing.T) {
	tests := []struct {
		name     string
		track    []ids.NodeID
		contains ids.NodeID
		expected bool
	}{
		{
			name:     "empty",
			track:    []ids.NodeID{},
			contains: p1,
			expected: false,
		},
		{
			name:     "populated - does not contain",
			track:    []ids.NodeID{p1, p2},
			contains: p3,
			expected: false,
		},
		{
			name:     "populated - contains",
			track:    []ids.NodeID{p1, p2, p3},
			contains: p3,
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			require.NoError(err)

			for _, add := range test.track {
				require.True(g.StartTrackingPeer(add))
			}

			require.Equal(test.expected, g.Tracked(test.contains))
		})
	}
}

func TestGossipTracker_StartTrackingPeer(t *testing.T) {
	tests := []struct {
		name            string
		toStartTracking []ids.NodeID
		expected        []bool
	}{
		{
			// Tracking new peers always works
			name:            "unique adds",
			toStartTracking: []ids.NodeID{p1, p2, p3},
			expected:        []bool{true, true, true},
		},
		{
			// We shouldn't be able to track a peer more than once
			name:            "duplicate adds",
			toStartTracking: []ids.NodeID{p1, p1, p1},
			expected:        []bool{true, false, false},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			require.NoError(err)

			for i, p := range test.toStartTracking {
				require.Equal(test.expected[i], g.StartTrackingPeer(p))
				require.True(g.Tracked(p))
			}
		})
	}
}

func TestGossipTracker_StopTrackingPeer(t *testing.T) {
	tests := []struct {
		name                  string
		toStartTracking       []ids.NodeID
		expectedStartTracking []bool
		toStopTracking        []ids.NodeID
		expectedStopTracking  []bool
	}{
		{
			// We should be able to stop tracking that we are tracking
			name:                 "stop tracking tracked peers",
			toStartTracking:      []ids.NodeID{p1, p2, p3},
			toStopTracking:       []ids.NodeID{p1, p2, p3},
			expectedStopTracking: []bool{true, true, true},
		},
		{
			// We shouldn't be able to stop tracking peers we've stopped tracking
			name:                 "stop tracking twice",
			toStartTracking:      []ids.NodeID{p1},
			toStopTracking:       []ids.NodeID{p1, p1},
			expectedStopTracking: []bool{true, false},
		},
		{
			// We shouldn't be able to stop tracking peers we were never tracking
			name:                  "remove non-existent elements",
			toStartTracking:       []ids.NodeID{},
			expectedStartTracking: []bool{},
			toStopTracking:        []ids.NodeID{p1, p2, p3},
			expectedStopTracking:  []bool{false, false, false},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			require.NoError(err)

			for _, add := range test.toStartTracking {
				require.True(g.StartTrackingPeer(add))
				require.True(g.Tracked(add))
			}

			for i, p := range test.toStopTracking {
				require.Equal(test.expectedStopTracking[i], g.StopTrackingPeer(p))
			}
		})
	}
}

func TestGossipTracker_AddValidator(t *testing.T) {
	type args struct {
		validator ValidatorID
	}

	tests := []struct {
		name       string
		validators []ValidatorID
		args       args
		expected   bool
	}{
		{
			name:       "not present",
			validators: []ValidatorID{},
			args:       args{validator: v1},
			expected:   true,
		},
		{
			name:       "already present txID but with different nodeID",
			validators: []ValidatorID{v1},
			args: args{validator: ValidatorID{
				NodeID: ids.GenerateTestNodeID(),
				TxID:   v1.TxID,
			}},
			expected: false,
		},
		{
			name:       "already present nodeID but with different txID",
			validators: []ValidatorID{v1},
			args: args{validator: ValidatorID{
				NodeID: v1.NodeID,
				TxID:   ids.GenerateTestID(),
			}},
			expected: false,
		},
		{
			name:       "already present validatorID",
			validators: []ValidatorID{v1},
			args:       args{validator: v1},
			expected:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			require.NoError(err)

			for _, v := range test.validators {
				require.True(g.AddValidator(v))
			}

			require.Equal(test.expected, g.AddValidator(test.args.validator))
		})
	}
}

func TestGossipTracker_RemoveValidator(t *testing.T) {
	type args struct {
		id ids.NodeID
	}

	tests := []struct {
		name       string
		validators []ValidatorID
		args       args
		expected   bool
	}{
		{
			name:       "not already present",
			validators: []ValidatorID{},
			args:       args{id: v1.NodeID},
			expected:   false,
		},
		{
			name:       "already present",
			validators: []ValidatorID{v1},
			args:       args{id: v1.NodeID},
			expected:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			require.NoError(err)

			for _, v := range test.validators {
				require.True(g.AddValidator(v))
			}

			require.Equal(test.expected, g.RemoveValidator(test.args.id))
		})
	}
}

func TestGossipTracker_ResetValidator(t *testing.T) {
	type args struct {
		id ids.NodeID
	}

	tests := []struct {
		name       string
		validators []ValidatorID
		args       args
		expected   bool
	}{
		{
			name:       "non-existent validator",
			validators: []ValidatorID{},
			args:       args{id: v1.NodeID},
			expected:   false,
		},
		{
			name:       "existing validator",
			validators: []ValidatorID{v1},
			args:       args{id: v1.NodeID},
			expected:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			require.NoError(err)

			require.True(g.StartTrackingPeer(p1))

			for _, v := range test.validators {
				require.True(g.AddValidator(v))
				g.AddKnown(p1, []ids.ID{v.TxID}, nil)

				unknown, ok := g.GetUnknown(p1)
				require.True(ok)
				require.NotContains(unknown, v)
			}

			require.Equal(test.expected, g.ResetValidator(test.args.id))

			for _, v := range test.validators {
				unknown, ok := g.GetUnknown(p1)
				require.True(ok)
				require.Contains(unknown, v)
			}
		})
	}
}

func TestGossipTracker_AddKnown(t *testing.T) {
	type args struct {
		peerID ids.NodeID
		txIDs  []ids.ID
	}

	tests := []struct {
		name          string
		trackedPeers  []ids.NodeID
		validators    []ValidatorID
		args          args
		expectedTxIDs []ids.ID
		expectedOk    bool
	}{
		{
			// We should not be able to update an untracked peer
			name:          "untracked peer - empty",
			trackedPeers:  []ids.NodeID{},
			validators:    []ValidatorID{},
			args:          args{peerID: p1, txIDs: []ids.ID{}},
			expectedTxIDs: nil,
			expectedOk:    false,
		},
		{
			// We should not be able to update an untracked peer
			name:          "untracked peer - populated",
			trackedPeers:  []ids.NodeID{p2, p3},
			validators:    []ValidatorID{},
			args:          args{peerID: p1, txIDs: []ids.ID{}},
			expectedTxIDs: nil,
			expectedOk:    false,
		},
		{
			// We shouldn't be able to look up a peer that isn't tracked
			name:          "untracked peer - unknown validator",
			trackedPeers:  []ids.NodeID{},
			validators:    []ValidatorID{},
			args:          args{peerID: p1, txIDs: []ids.ID{v1.TxID}},
			expectedTxIDs: nil,
			expectedOk:    false,
		},
		{
			// We shouldn't fail on a validator that's not registered
			name:          "tracked peer  - unknown validator",
			trackedPeers:  []ids.NodeID{p1},
			validators:    []ValidatorID{},
			args:          args{peerID: p1, txIDs: []ids.ID{v1.TxID}},
			expectedTxIDs: []ids.ID{},
			expectedOk:    true,
		},
		{
			// We should be able to update a tracked validator
			name:          "update tracked validator",
			trackedPeers:  []ids.NodeID{p1, p2, p3},
			validators:    []ValidatorID{v1},
			args:          args{peerID: p1, txIDs: []ids.ID{v1.TxID}},
			expectedTxIDs: []ids.ID{v1.TxID},
			expectedOk:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			require.NoError(err)

			for _, p := range test.trackedPeers {
				require.True(g.StartTrackingPeer(p))
				require.True(g.Tracked(p))
			}

			for _, v := range test.validators {
				require.True(g.AddValidator(v))
			}

			txIDs, ok := g.AddKnown(test.args.peerID, test.args.txIDs, test.args.txIDs)
			require.Equal(test.expectedOk, ok)
			require.Equal(test.expectedTxIDs, txIDs)
		})
	}
}

func TestGossipTracker_GetUnknown(t *testing.T) {
	tests := []struct {
		name            string
		peerID          ids.NodeID
		peersToTrack    []ids.NodeID
		validators      []ValidatorID
		expectedUnknown []ValidatorID
		expectedOk      bool
	}{
		{
			name:            "non tracked peer",
			peerID:          p1,
			validators:      []ValidatorID{v2},
			peersToTrack:    []ids.NodeID{},
			expectedUnknown: nil,
			expectedOk:      false,
		},
		{
			name:            "only validators",
			peerID:          p1,
			peersToTrack:    []ids.NodeID{p1},
			validators:      []ValidatorID{v2},
			expectedUnknown: []ValidatorID{v2},
			expectedOk:      true,
		},
		{
			name:            "only non-validators",
			peerID:          p1,
			peersToTrack:    []ids.NodeID{p1, p2},
			validators:      []ValidatorID{},
			expectedUnknown: []ValidatorID{},
			expectedOk:      true,
		},
		{
			name:            "validators and non-validators",
			peerID:          p1,
			peersToTrack:    []ids.NodeID{p1, p3},
			validators:      []ValidatorID{v2},
			expectedUnknown: []ValidatorID{v2},
			expectedOk:      true,
		},
		{
			name:            "same as limit",
			peerID:          p1,
			peersToTrack:    []ids.NodeID{p1},
			validators:      []ValidatorID{v2, v3},
			expectedUnknown: []ValidatorID{v2, v3},
			expectedOk:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			require.NoError(err)

			// add our validators
			for _, validator := range test.validators {
				require.True(g.AddValidator(validator))
			}

			// start tracking our peers
			for _, nonValidator := range test.peersToTrack {
				require.True(g.StartTrackingPeer(nonValidator))
				require.True(g.Tracked(nonValidator))
			}

			// get the unknown peers for this peer
			result, ok := g.GetUnknown(test.peerID)
			require.Equal(test.expectedOk, ok)
			require.Len(result, len(test.expectedUnknown))
			for _, v := range test.expectedUnknown {
				require.Contains(result, v)
			}
		})
	}
}

func TestGossipTracker_E2E(t *testing.T) {
	require := require.New(t)

	g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
	require.NoError(err)

	// [v1, v2, v3] are validators
	require.True(g.AddValidator(v1))
	require.True(g.AddValidator(v2))

	// we should get an empty unknown since we're not tracking anything
	unknown, ok := g.GetUnknown(p1)
	require.False(ok)
	require.Nil(unknown)

	// we should get a unknown of [v1, v2] since v1 and v2 are registered
	require.True(g.StartTrackingPeer(p1))
	require.True(g.Tracked(p1))

	// check p1's unknown
	unknown, ok = g.GetUnknown(p1)
	require.True(ok)
	require.Contains(unknown, v1)
	require.Contains(unknown, v2)
	require.Len(unknown, 2)

	// Check p2's unknown. We should get nothing since we're not tracking it
	// yet.
	unknown, ok = g.GetUnknown(p2)
	require.False(ok)
	require.Nil(unknown)

	// Start tracking p2
	require.True(g.StartTrackingPeer(p2))

	// check p2's unknown
	unknown, ok = g.GetUnknown(p2)
	require.True(ok)
	require.Contains(unknown, v1)
	require.Contains(unknown, v2)
	require.Len(unknown, 2)

	// p1 now knows about v1, but not v2, so it should see [v2] in its unknown
	// p2 still knows nothing, so it should see both
	txIDs, ok := g.AddKnown(p1, []ids.ID{v1.TxID}, []ids.ID{v1.TxID})
	require.True(ok)
	require.Equal([]ids.ID{v1.TxID}, txIDs)

	// p1 should have an unknown of [v2], since it knows v1
	unknown, ok = g.GetUnknown(p1)
	require.True(ok)
	require.Contains(unknown, v2)
	require.Len(unknown, 1)

	// p2 should have a unknown of [v1, v2], since it knows nothing
	unknown, ok = g.GetUnknown(p2)
	require.True(ok)
	require.Contains(unknown, v1)
	require.Contains(unknown, v2)
	require.Len(unknown, 2)

	// Add v3
	require.True(g.AddValidator(v3))

	// track p3, who knows of v1, v2, and v3
	// p1 and p2 still don't know of v3
	require.True(g.StartTrackingPeer(p3))

	txIDs, ok = g.AddKnown(p3, []ids.ID{v1.TxID, v2.TxID, v3.TxID}, []ids.ID{v1.TxID, v2.TxID, v3.TxID})
	require.True(ok)
	require.Equal([]ids.ID{v1.TxID, v2.TxID, v3.TxID}, txIDs)

	// p1 doesn't know about [v2, v3]
	unknown, ok = g.GetUnknown(p1)
	require.True(ok)
	require.Contains(unknown, v2)
	require.Contains(unknown, v3)
	require.Len(unknown, 2)

	// p2 doesn't know about [v1, v2, v3]
	unknown, ok = g.GetUnknown(p2)
	require.True(ok)
	require.Contains(unknown, v1)
	require.Contains(unknown, v2)
	require.Contains(unknown, v3)
	require.Len(unknown, 3)

	// p3 knows about everyone
	unknown, ok = g.GetUnknown(p3)
	require.True(ok)
	require.Empty(unknown)

	// stop tracking p2
	require.True(g.StopTrackingPeer(p2))
	unknown, ok = g.GetUnknown(p2)
	require.False(ok)
	require.Nil(unknown)

	// p1 doesn't know about [v2, v3] because v2 is still registered as
	// a validator
	unknown, ok = g.GetUnknown(p1)
	require.True(ok)
	require.Contains(unknown, v2)
	require.Contains(unknown, v3)
	require.Len(unknown, 2)

	// Remove p2 from the validator set
	require.True(g.RemoveValidator(v2.NodeID))

	// p1 doesn't know about [v3] since v2 left the validator set
	unknown, ok = g.GetUnknown(p1)
	require.True(ok)
	require.Contains(unknown, v3)
	require.Len(unknown, 1)

	// p3 knows about everyone since it learned about v1 and v3 earlier.
	unknown, ok = g.GetUnknown(p3)
	require.Empty(unknown)
	require.True(ok)
}

func TestGossipTracker_Regression_IncorrectTxIDDeletion(t *testing.T) {
	require := require.New(t)

	g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
	require.NoError(err)

	require.True(g.AddValidator(v1))
	require.True(g.AddValidator(v2))

	require.True(g.RemoveValidator(v1.NodeID))

	require.False(g.AddValidator(ValidatorID{
		NodeID: ids.GenerateTestNodeID(),
		TxID:   v2.TxID,
	}))
}
