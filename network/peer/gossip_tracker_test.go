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
	v1 = ids.GenerateTestNodeID()
	v2 = ids.GenerateTestNodeID()
	v3 = ids.GenerateTestNodeID()

	limit = 100
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
			r := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			r.NoError(err)

			for _, add := range test.track {
				r.True(g.StartTrackingPeer(add))
			}

			r.Equal(test.expected, g.Tracked(test.contains))
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
			r := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			r.NoError(err)

			for i, p := range test.toStartTracking {
				r.Equal(test.expected[i], g.StartTrackingPeer(p))
				r.True(g.Tracked(p))
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
			r := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			r.NoError(err)

			for _, add := range test.toStartTracking {
				r.True(g.StartTrackingPeer(add))
				r.True(g.Tracked(add))
			}

			for i, p := range test.toStopTracking {
				r.Equal(test.expectedStopTracking[i], g.StopTrackingPeer(p))
			}
		})
	}
}

func TestGossipTracker_AddValidator(t *testing.T) {
	type args struct {
		id ids.NodeID
	}

	tests := []struct {
		name       string
		validators []ids.NodeID
		args       args
		expected   bool
	}{
		{
			name:       "not present",
			validators: []ids.NodeID{},
			args:       args{id: p1},
			expected:   true,
		},
		{
			name:       "already present",
			validators: []ids.NodeID{p1},
			args:       args{id: p1},
			expected:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			r.NoError(err)

			for _, v := range test.validators {
				r.True(g.AddValidator(v))
			}

			r.Equal(test.expected, g.AddValidator(test.args.id))
		})
	}
}

func TestGossipTracker_RemoveValidator(t *testing.T) {
	type args struct {
		id ids.NodeID
	}

	tests := []struct {
		name       string
		validators []ids.NodeID
		args       args
		expected   bool
	}{
		{
			name:       "not already present",
			validators: []ids.NodeID{},
			args:       args{id: p1},
			expected:   false,
		},
		{
			name:       "already present",
			validators: []ids.NodeID{p1},
			args:       args{id: p1},
			expected:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			r.NoError(err)

			for _, v := range test.validators {
				r.True(g.AddValidator(v))
			}

			r.Equal(test.expected, g.RemoveValidator(test.args.id))
		})
	}
}

func TestGossipTracker_AddKnown(t *testing.T) {
	type args struct {
		peerID       ids.NodeID
		validatorIDs []ids.NodeID
	}

	tests := []struct {
		name         string
		trackedPeers []ids.NodeID
		validators   []ids.NodeID
		args         args
		expected     bool
	}{
		{
			// We should not be able to update an untracked peer
			name:         "untracked peer - empty",
			trackedPeers: []ids.NodeID{},
			validators:   []ids.NodeID{},
			args:         args{peerID: p1, validatorIDs: []ids.NodeID{}},
			expected:     false,
		},
		{
			// We should not be able to update an untracked peer
			name:         "untracked peer - populated",
			trackedPeers: []ids.NodeID{p2, p3},
			validators:   []ids.NodeID{},
			args:         args{peerID: p1, validatorIDs: []ids.NodeID{}},
			expected:     false,
		},
		{
			// We shouldn't be able to look up a peer that isn't tracked
			name:         "untracked peer - unknown validator",
			trackedPeers: []ids.NodeID{},
			validators:   []ids.NodeID{},
			args:         args{peerID: p1, validatorIDs: []ids.NodeID{v1}},
			expected:     false,
		},
		{
			// We shouldn't fail on a validator that's not registered
			name:         "tracked peer  - unknown validator",
			trackedPeers: []ids.NodeID{p1},
			validators:   []ids.NodeID{},
			args:         args{peerID: p1, validatorIDs: []ids.NodeID{v1}},
			expected:     true,
		},
		{
			// We should be able to update a tracked validator
			name:         "update tracked validator",
			trackedPeers: []ids.NodeID{p1, p2, p3},
			validators:   []ids.NodeID{v1},
			args:         args{peerID: p1, validatorIDs: []ids.NodeID{v1}},
			expected:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			r.NoError(err)

			for _, p := range test.trackedPeers {
				r.True(g.StartTrackingPeer(p))
				r.True(g.Tracked(p))
			}

			for _, v := range test.validators {
				r.True(g.AddValidator(v))
			}

			r.Equal(test.expected, g.AddKnown(test.args.peerID, test.args.validatorIDs))
		})
	}
}

func TestGossipTracker_GetUnknown(t *testing.T) {
	type args struct {
		peerID ids.NodeID
		limit  int
	}

	tests := []struct {
		name            string
		args            args
		peersToTrack    []ids.NodeID
		validators      []ids.NodeID
		expectedUnknown []ids.NodeID
		expectedOk      bool
	}{
		{
			name:            "non tracked peer",
			args:            args{peerID: p1, limit: 100},
			validators:      []ids.NodeID{p2},
			peersToTrack:    []ids.NodeID{},
			expectedUnknown: nil,
			expectedOk:      false,
		},
		{
			name:            "only validators",
			args:            args{peerID: p1, limit: 100},
			validators:      []ids.NodeID{p2},
			peersToTrack:    []ids.NodeID{p1},
			expectedUnknown: []ids.NodeID{p2},
			expectedOk:      true,
		},
		{
			name:            "only non-validators",
			args:            args{peerID: p1, limit: 100},
			validators:      []ids.NodeID{},
			peersToTrack:    []ids.NodeID{p1, p2},
			expectedUnknown: []ids.NodeID{},
			expectedOk:      true,
		},
		{
			name:            "validators and non-validators",
			args:            args{peerID: p1, limit: 100},
			validators:      []ids.NodeID{p2},
			peersToTrack:    []ids.NodeID{p1, p3},
			expectedUnknown: []ids.NodeID{p2},
			expectedOk:      true,
		},
		{
			name:            "empty limit",
			args:            args{peerID: p1, limit: 0},
			validators:      []ids.NodeID{p2},
			peersToTrack:    []ids.NodeID{p1, p3},
			expectedUnknown: nil,
			expectedOk:      false,
		},
		{
			name:            "less than limit",
			args:            args{peerID: p1, limit: 2},
			validators:      []ids.NodeID{p2},
			peersToTrack:    []ids.NodeID{p1},
			expectedUnknown: []ids.NodeID{p2},
			expectedOk:      true,
		},
		{
			name:            "same as limit",
			args:            args{peerID: p1, limit: 2},
			validators:      []ids.NodeID{p2, p3},
			peersToTrack:    []ids.NodeID{p1},
			expectedUnknown: []ids.NodeID{p2, p3},
			expectedOk:      true,
		},
		// this test is disabled because of non-determinism
		// {
		// 	name:            "greater than limit",
		// 	args:            args{peerID: p1, limit: 1},
		// 	validators:      []ids.NodeID{p2, p3},
		// 	peersToTrack:    []ids.NodeID{p1},
		// 	expectedUnknown: []ids.NodeID{p2},
		// 	expectedOk:      true,
		// },
	}

	for _, test := range tests {
		r := require.New(t)
		t.Run(test.name, func(t *testing.T) {
			g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
			r.NoError(err)

			// add our validators
			for _, validator := range test.validators {
				r.True(g.AddValidator(validator))
			}

			// start tracking our peers
			for _, nonValidator := range test.peersToTrack {
				r.True(g.StartTrackingPeer(nonValidator))
				r.True(g.Tracked(nonValidator))
			}

			// get the unknown peers for this peer
			result, ok, err := g.GetUnknown(test.args.peerID, test.args.limit)
			r.NoError(err)
			r.Equal(test.expectedOk, ok)
			r.Len(result, len(test.expectedUnknown))
			for _, v := range test.expectedUnknown {
				r.Contains(result, v)
			}
		})
	}
}

func TestGossipTracker_E2E(t *testing.T) {
	r := require.New(t)

	g, err := NewGossipTracker(prometheus.NewRegistry(), "foobar")
	r.NoError(err)

	// [v1, v2, v3] are validators
	r.True(g.AddValidator(v1))
	r.True(g.AddValidator(v2))

	// we should get an empty unknown since we're not tracking anything
	unknown, ok, err := g.GetUnknown(p1, limit)
	r.NoError(err)
	r.False(ok)
	r.Nil(unknown)

	// we should get a unknown of [v1, v2] since v1 and v2 are registered
	r.True(g.StartTrackingPeer(p1))
	r.True(g.Tracked(p1))

	// check p1's unknown
	unknown, ok, err = g.GetUnknown(p1, limit)
	r.NoError(err)
	r.True(ok)
	r.Contains(unknown, v1)
	r.Contains(unknown, v2)
	r.Len(unknown, 2)

	// Check p2's unknown. We should get nothing since we're not tracking it
	// yet.
	unknown, ok, err = g.GetUnknown(p2, limit)
	r.NoError(err)
	r.False(ok)
	r.Nil(unknown)

	// Start tracking p2
	r.True(g.StartTrackingPeer(p2))

	// check p2's unknown
	unknown, ok, err = g.GetUnknown(p2, limit)
	r.NoError(err)
	r.True(ok)
	r.Contains(unknown, v1)
	r.Contains(unknown, v2)
	r.Len(unknown, 2)

	// p1 now knows about v1, but not v2, so it should see [v2] in its unknown
	// p2 still knows nothing, so it should see both
	r.True(g.AddKnown(p1, []ids.NodeID{v1}))

	// p1 should have an unknown of [v2], since it knows v1
	unknown, ok, err = g.GetUnknown(p1, limit)
	r.NoError(err)
	r.True(ok)
	r.Contains(unknown, v2)
	r.Len(unknown, 1)

	// p2 should have a unknown of [v1, v2], since it knows nothing
	unknown, ok, err = g.GetUnknown(p2, limit)
	r.NoError(err)
	r.True(ok)
	r.Contains(unknown, v1)
	r.Contains(unknown, v2)
	r.Len(unknown, 2)

	// Add v3
	r.True(g.AddValidator(v3))

	// track p3, who knows of v1, v2, and v3
	// p1 and p2 still don't know of v3
	r.True(g.StartTrackingPeer(p3))
	r.True(g.AddKnown(p3, []ids.NodeID{v1, v2, v3}))

	// p1 doesn't know about [v2, v3]
	unknown, ok, err = g.GetUnknown(p1, limit)
	r.NoError(err)
	r.True(ok)
	r.Contains(unknown, v2)
	r.Contains(unknown, v3)
	r.Len(unknown, 2)

	// p2 doesn't know about [v1, v2, v3]
	unknown, ok, err = g.GetUnknown(p2, limit)
	r.NoError(err)
	r.True(ok)
	r.Contains(unknown, v1)
	r.Contains(unknown, v2)
	r.Contains(unknown, v3)
	r.Len(unknown, 3)

	// p3 knows about everyone
	unknown, ok, err = g.GetUnknown(p3, limit)
	r.NoError(err)
	r.True(ok)
	r.Empty(unknown)

	// stop tracking p2
	r.True(g.StopTrackingPeer(p2))
	unknown, ok, err = g.GetUnknown(p2, limit)
	r.NoError(err)
	r.False(ok)
	r.Nil(unknown)

	// p1 doesn't know about [v2, v3] because v2 is still registered as
	// a validator
	unknown, ok, err = g.GetUnknown(p1, limit)
	r.NoError(err)
	r.True(ok)
	r.Contains(unknown, v2)
	r.Contains(unknown, v3)
	r.Len(unknown, 2)

	// Remove p2 from the validator set
	r.True(g.RemoveValidator(v2))

	// p1 doesn't know about [v3] since v2 left the validator set
	unknown, ok, err = g.GetUnknown(p1, limit)
	r.NoError(err)
	r.True(ok)
	r.Contains(unknown, v3)
	r.Len(unknown, 1)

	// p3 knows about everyone since it learned about v1 and v3 earlier.
	unknown, ok, err = g.GetUnknown(p3, limit)
	r.NoError(err)
	r.Empty(unknown)
	r.True(ok)
}
