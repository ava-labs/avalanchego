// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	id0 = ids.GenerateTestNodeID()
	id1 = ids.GenerateTestNodeID()
	id2 = ids.GenerateTestNodeID()
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
			gossipTracker := NewGossipTracker()

			for _, add := range test.add {
				r.True(gossipTracker.Add(add))
			}

			r.Equal(test.expected, gossipTracker.Contains(test.contains))
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
			gossipTracker := NewGossipTracker()

			for i, p := range test.toAdd {
				r.Equal(test.expected[i], gossipTracker.Add(p))
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
			gossipTracker := NewGossipTracker()

			for _, add := range test.toAdd {
				r.True(gossipTracker.Add(add))
			}

			for i, p := range test.toRemove {
				r.Equal(test.expectedRemove[i], gossipTracker.Remove(p))
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
			gossipTracker := NewGossipTracker()

			for _, add := range test.add {
				r.True(gossipTracker.Add(add))
			}

			r.Equal(test.expected, gossipTracker.UpdateKnown(test.args.id, test.args.known))
		})
	}
}

func TestGossipTracker_GetUnknown(t *testing.T) {
	r := require.New(t)
	d := NewGossipTracker()

	// we should get an empty unknown since we're not tracking anything
	unknown, ok := d.GetUnknown(id0)
	r.Empty(unknown)
	r.False(ok)

	// we should get a unknown of [+id0, +id1] since we know about id0 and id1,
	// but id0 and id1 both don't know anything yet
	d.Add(id0)
	d.Add(id1)

	// check id0's unknown
	unknown, ok = d.GetUnknown(id0)
	r.Contains(unknown, id0)
	r.Contains(unknown, id1)
	r.Len(unknown, 2)
	r.True(ok)

	// check id1's unknown
	unknown, ok = d.GetUnknown(id1)
	r.Contains(unknown, id0)
	r.Contains(unknown, id1)
	r.Len(unknown, 2)
	r.True(ok)

	// id0 now knows about id0, but not id1, so it should see [+id1] in its unknown
	// id1 still knows nothing, so it should see both
	p0 := []ids.NodeID{id0}
	r.True(d.UpdateKnown(id0, p0))

	// id0 should have a unknown of [id1], since it knows id0
	unknown, ok = d.GetUnknown(id0)
	r.Contains(unknown, id1)
	r.Len(unknown, 1)
	r.True(ok)

	// id1 should have a unknown of [id0, id1], since it knows nothing
	unknown, ok = d.GetUnknown(id1)
	r.Contains(unknown, id0)
	r.Contains(unknown, id1)
	r.Len(unknown, 2)
	r.True(ok)

	// add id2, who knows of id0, id1, and id2
	// id0 and id1 don't know of id2
	p2 := []ids.NodeID{id0, id1, id2}
	d.Add(id2)
	r.True(d.UpdateKnown(id2, p2))

	// id0 doesn't know about [id1, id2]
	unknown, ok = d.GetUnknown(id0)
	r.Contains(unknown, id1)
	r.Contains(unknown, id2)
	r.Len(unknown, 2)
	r.True(ok)

	// id1 doesn't know about [id0, id1, id2]
	unknown, ok = d.GetUnknown(id1)
	r.Contains(unknown, id0)
	r.Contains(unknown, id1)
	r.Contains(unknown, id2)
	r.Len(unknown, 3)
	r.True(ok)

	// id2 knows about everyone
	unknown, ok = d.GetUnknown(id2)
	r.Empty(unknown)
	r.True(ok)

	// stop tracking id1
	r.True(d.Remove(id1))
	unknown, ok = d.GetUnknown(id1)
	r.Nil(unknown)
	r.False(ok)

	// id0 doesn't know about [id2], we shouldn't care about id1 anymore
	unknown, ok = d.GetUnknown(id0)
	r.Contains(unknown, id2)
	r.Len(unknown, 1)
	r.True(ok)

	// id2 knows everyone
	unknown, ok = d.GetUnknown(id2)
	r.Empty(unknown)
	r.True(ok)
}
