// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserveg.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	id0   = ids.GenerateTestNodeID()
	id1   = ids.GenerateTestNodeID()
	id2   = ids.GenerateTestNodeID()
	limit = 100
)

func TestGossipTracker_Contains(t *testing.T) {
	tests := []struct {
		name     string
		add      []ids.NodeID
		contains ids.NodeID
		expected bool
	}{
		{
			name:     "empty",
			add:      []ids.NodeID{},
			contains: id0,
			expected: false,
		},
		{
			name:     "populated - does not contain",
			add:      []ids.NodeID{id0, id1},
			contains: id2,
			expected: false,
		},
		{
			name:     "populated - contains",
			add:      []ids.NodeID{id0, id1, id2},
			contains: id2,
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			cfg := GossipTrackerConfig{
				ValidatorManager: validators.NewManager(),
				Registerer:       prometheus.NewRegistry(),
				Namespace:        "foobar",
			}

			g, err := NewGossipTracker(cfg)
			r.NoError(err)

			for _, add := range test.add {
				r.True(g.Add(add))
			}

			r.Equal(test.expected, g.Contains(test.contains))
		})
	}
}

func TestGossipTracker_Add(t *testing.T) {
	tests := []struct {
		name     string
		toAdd    []ids.NodeID
		expected []bool
	}{
		{
			// Adding new elements always works
			name:     "unique adds",
			toAdd:    []ids.NodeID{id0, id1, id2},
			expected: []bool{true, true, true},
		},
		{
			// We shouldn't be able to add an item more than once
			name:     "duplicate adds",
			toAdd:    []ids.NodeID{id0, id0, id0},
			expected: []bool{true, false, false},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			cfg := GossipTrackerConfig{
				ValidatorManager: validators.NewManager(),
				Registerer:       prometheus.NewRegistry(),
				Namespace:        "foobar",
			}

			g, err := NewGossipTracker(cfg)
			r.NoError(err)

			for i, p := range test.toAdd {
				r.Equal(test.expected[i], g.Add(p))
				r.True(g.Contains(p))
			}
		})
	}
}

func TestGossipTracker_Remove(t *testing.T) {
	tests := []struct {
		name           string
		toAdd          []ids.NodeID
		expectedAdd    []bool
		toRemove       []ids.NodeID
		expectedRemove []bool
	}{
		{
			// We should be able to remove things that we are tracking
			name:           "remove existing elements",
			toAdd:          []ids.NodeID{id0, id1, id2},
			toRemove:       []ids.NodeID{id0, id1, id2},
			expectedRemove: []bool{true, true, true},
		},
		{
			// We shouldn't be able to remove something once it's gone
			name:           "duplicate remove",
			toAdd:          []ids.NodeID{id0},
			toRemove:       []ids.NodeID{id0, id0},
			expectedRemove: []bool{true, false},
		},
		{
			// We shouldn't be able to remove elements we aren't tracking
			name:           "remove non-existent elements",
			toAdd:          []ids.NodeID{},
			expectedAdd:    []bool{},
			toRemove:       []ids.NodeID{id0, id1, id2},
			expectedRemove: []bool{false, false, false},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			cfg := GossipTrackerConfig{
				ValidatorManager: validators.NewManager(),
				Registerer:       prometheus.NewRegistry(),
				Namespace:        "foobar",
			}

			g, err := NewGossipTracker(cfg)
			r.NoError(err)

			for _, add := range test.toAdd {
				r.True(g.Add(add))
				r.True(g.Contains(add))
			}

			for i, p := range test.toRemove {
				r.Equal(test.expectedRemove[i], g.Remove(p))
			}
		})
	}
}

func TestGossipTracker_UpdateKnown(t *testing.T) {
	type args struct {
		id    ids.NodeID
		known []ids.NodeID
	}

	tests := []struct {
		name     string
		add      []ids.NodeID
		args     args
		expected bool
	}{
		{
			// We should not be able to update an untracked peer
			name:     "update untracked peer - empty",
			add:      []ids.NodeID{},
			args:     args{id0, []ids.NodeID{}},
			expected: false,
		},
		{
			// We should not be able to update an untracked peer
			name:     "update untracked peer - populated",
			add:      []ids.NodeID{id1, id2},
			args:     args{id0, []ids.NodeID{}},
			expected: false,
		},
		{
			// We shouldn't be able to know about peers that aren't tracked
			name:     "update untracked peer - unknown peer",
			add:      []ids.NodeID{},
			args:     args{id0, []ids.NodeID{id1}},
			expected: false,
		},
		{
			// We shouldn't be able to know about peers that aren't tracked
			name:     "update tracked peer - unknown peer",
			add:      []ids.NodeID{id0},
			args:     args{id0, []ids.NodeID{id1}},
			expected: false,
		},
		{
			// We should be able to update a tracked peer
			name:     "update tracked peer",
			add:      []ids.NodeID{id0, id1, id2},
			args:     args{id0, []ids.NodeID{}},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			cfg := GossipTrackerConfig{
				ValidatorManager: validators.NewManager(),
				Registerer:       prometheus.NewRegistry(),
				Namespace:        "foobar",
			}

			g, err := NewGossipTracker(cfg)
			r.NoError(err)

			for _, add := range test.add {
				r.True(g.Add(add))
				r.True(g.Contains(add))
			}

			r.Equal(test.expected, g.UpdateKnown(test.args.id, test.args.known))
		})
	}
}

func TestGossipTracker_GetUnknown(t *testing.T) {
	tests := []struct {
		name          string
		peer          ids.NodeID
		validators    []ids.NodeID
		nonValidators []ids.NodeID
		limit         int
		expected      []ids.NodeID
	}{
		{
			name:          "only validators",
			peer:          id0,
			validators:    []ids.NodeID{id1},
			nonValidators: []ids.NodeID{},
			limit:         100,
			expected:      []ids.NodeID{id1},
		},
		{
			name:          "only non-validators",
			peer:          id0,
			validators:    []ids.NodeID{},
			nonValidators: []ids.NodeID{id1},
			limit:         100,
			expected:      []ids.NodeID{},
		},
		{
			name:          "validators and non-validators",
			peer:          id0,
			validators:    []ids.NodeID{id1},
			nonValidators: []ids.NodeID{id2},
			limit:         100,
			expected:      []ids.NodeID{id1},
		},
		{
			name:          "less than limit",
			peer:          id0,
			validators:    []ids.NodeID{id1},
			nonValidators: []ids.NodeID{},
			limit:         2,
			expected:      []ids.NodeID{id1},
		},
		{
			name:          "same as limit",
			peer:          id0,
			validators:    []ids.NodeID{id1, id2},
			nonValidators: []ids.NodeID{},
			limit:         2,
			expected:      []ids.NodeID{id1, id2},
		},
		{
			name:          "greater than limit",
			peer:          id0,
			validators:    []ids.NodeID{id1, id2},
			nonValidators: []ids.NodeID{},
			limit:         1,
			expected:      []ids.NodeID{id1},
		},
	}

	for _, test := range tests {
		r := require.New(t)
		t.Run(test.name, func(t *testing.T) {
			cfg := GossipTrackerConfig{
				ValidatorManager: validators.NewManager(),
				Registerer:       prometheus.NewRegistry(),
				Namespace:        "foobar",
			}

			g, err := NewGossipTracker(cfg)
			r.NoError(err)

			// start tracking our validators
			for _, validator := range test.validators {
				r.NoError(cfg.ValidatorManager.AddWeight(constants.PrimaryNetworkID, validator, 1))
				r.True(g.Add(validator))
				r.True(g.Contains(validator))
			}

			// start tracking our non validators
			for _, nonValidator := range test.nonValidators {
				r.True(g.Add(nonValidator))
				r.True(g.Contains(nonValidator))
			}

			// start tracking our peer
			r.True(g.Add(test.peer))
			r.True(g.Contains(test.peer))

			result, ok := g.GetUnknown(test.peer, test.limit)
			r.True(ok)
			r.EqualValues(test.expected, result)
		})
	}
}

func TestGossipTracker_GetUnknown_E2E(t *testing.T) {
	r := require.New(t)

	cfg := GossipTrackerConfig{
		ValidatorManager: validators.NewManager(),
		Registerer:       prometheus.NewRegistry(),
		Namespace:        "foobar",
	}

	// [id0, id1, id2] are validators
	_ = cfg.ValidatorManager.AddWeight(constants.PrimaryNetworkID, id0, 1)
	_ = cfg.ValidatorManager.AddWeight(constants.PrimaryNetworkID, id1, 1)
	_ = cfg.ValidatorManager.AddWeight(constants.PrimaryNetworkID, id2, 1)

	g, err := NewGossipTracker(cfg)

	r.NoError(err)

	// we should get an empty unknown since we're not tracking anything
	unknown, ok := g.GetUnknown(id0, limit)
	r.False(ok)
	r.Empty(unknown)

	// we should get a unknown of [id0, id1] since we know about id0 and id1,
	// but id0 and id1 both don't know anything yet
	g.Add(id0)
	g.Contains(id0)
	g.Add(id1)
	g.Contains(id1)

	// check id0's unknown
	unknown, ok = g.GetUnknown(id0, limit)
	r.True(ok)
	r.Contains(unknown, id0)
	r.Contains(unknown, id1)
	r.Len(unknown, 2)

	// check id1's unknown
	unknown, ok = g.GetUnknown(id1, limit)
	r.True(ok)
	r.Contains(unknown, id0)
	r.Contains(unknown, id1)
	r.Len(unknown, 2)

	// id0 now knows about id0, but not id1, so it should see [id1] in its unknown
	// id1 still knows nothing, so it should see both
	p0 := []ids.NodeID{id0}
	r.True(g.UpdateKnown(id0, p0))

	// id0 should have a unknown of [id1], since it knows id0
	unknown, ok = g.GetUnknown(id0, limit)
	r.True(ok)
	r.Contains(unknown, id1)
	r.Len(unknown, 1)

	// id1 should have a unknown of [id0, id1], since it knows nothing
	unknown, ok = g.GetUnknown(id1, limit)
	r.True(ok)
	r.Contains(unknown, id0)
	r.Contains(unknown, id1)
	r.Len(unknown, 2)

	// add id2, who knows of id0, id1, and id2
	// id0 and id1 don't know of id2
	p2 := []ids.NodeID{id0, id1, id2}
	g.Add(id2)
	r.True(g.Contains(id2))
	r.True(g.UpdateKnown(id2, p2))

	// id0 doesn't know about [id1, id2]
	unknown, ok = g.GetUnknown(id0, limit)
	r.True(ok)
	r.Contains(unknown, id1)
	r.Contains(unknown, id2)
	r.Len(unknown, 2)

	// id1 doesn't know about [id0, id1, id2]
	unknown, ok = g.GetUnknown(id1, limit)
	r.True(ok)
	r.Contains(unknown, id0)
	r.Contains(unknown, id1)
	r.Contains(unknown, id2)
	r.Len(unknown, 3)

	// id2 knows about everyone
	unknown, ok = g.GetUnknown(id2, limit)
	r.True(ok)
	r.Empty(unknown)

	// stop tracking id1
	r.True(g.Remove(id1))
	unknown, ok = g.GetUnknown(id1, limit)
	r.False(ok)
	r.Nil(unknown)

	// id0 doesn't know about [id2], we shouldn't care about id1 anymore
	unknown, ok = g.GetUnknown(id0, limit)
	r.True(ok)
	r.Contains(unknown, id2)
	r.Len(unknown, 1)

	// id2 knows everyone
	unknown, ok = g.GetUnknown(id2, limit)
	r.Empty(unknown)
	r.True(ok)
}
