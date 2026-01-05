// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package setmap

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
)

func TestSetMapPut(t *testing.T) {
	tests := []struct {
		name            string
		state           *SetMap[int, int]
		key             int
		value           set.Set[int]
		expectedRemoved []Entry[int, int]
		expectedState   *SetMap[int, int]
	}{
		{
			name:            "none removed",
			state:           New[int, int](),
			key:             1,
			value:           set.Of(2),
			expectedRemoved: nil,
			expectedState: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(2),
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
		},
		{
			name: "key removed",
			state: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(2),
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			key:   1,
			value: set.Of(3),
			expectedRemoved: []Entry[int, int]{
				{
					Key: 1,
					Set: set.Of(2),
				},
			},
			expectedState: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(3),
				},
				valueToKey: map[int]int{
					3: 1,
				},
			},
		},
		{
			name: "value removed",
			state: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(2),
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			key:   3,
			value: set.Of(2),
			expectedRemoved: []Entry[int, int]{
				{
					Key: 1,
					Set: set.Of(2),
				},
			},
			expectedState: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					3: set.Of(2),
				},
				valueToKey: map[int]int{
					2: 3,
				},
			},
		},
		{
			name: "key and value removed",
			state: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(2),
					3: set.Of(4),
				},
				valueToKey: map[int]int{
					2: 1,
					4: 3,
				},
			},
			key:   1,
			value: set.Of(4),
			expectedRemoved: []Entry[int, int]{
				{
					Key: 1,
					Set: set.Of(2),
				},
				{
					Key: 3,
					Set: set.Of(4),
				},
			},
			expectedState: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(4),
				},
				valueToKey: map[int]int{
					4: 1,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			removed := test.state.Put(test.key, test.value)
			require.ElementsMatch(test.expectedRemoved, removed)
			require.Equal(test.expectedState, test.state)
		})
	}
}

func TestSetMapHasValueAndGetKeyAndSetOverlaps(t *testing.T) {
	m := New[int, int]()
	require.Empty(t, m.Put(1, set.Of(2)))

	tests := []struct {
		name           string
		value          int
		expectedKey    int
		expectedExists bool
	}{
		{
			name:           "fetch unknown",
			value:          3,
			expectedKey:    0,
			expectedExists: false,
		},
		{
			name:           "fetch known value",
			value:          2,
			expectedKey:    1,
			expectedExists: true,
		},
		{
			name:           "fetch known key",
			value:          1,
			expectedKey:    0,
			expectedExists: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			exists := m.HasValue(test.value)
			require.Equal(test.expectedExists, exists)

			key, exists := m.GetKey(test.value)
			require.Equal(test.expectedKey, key)
			require.Equal(test.expectedExists, exists)
		})
	}
}

func TestSetMapHasOverlap(t *testing.T) {
	m := New[int, int]()
	require.Empty(t, m.Put(1, set.Of(2)))
	require.Empty(t, m.Put(2, set.Of(3, 4)))

	tests := []struct {
		name             string
		set              set.Set[int]
		expectedOverlaps bool
	}{
		{
			name:             "small fetch unknown",
			set:              set.Of(5),
			expectedOverlaps: false,
		},
		{
			name:             "large fetch unknown",
			set:              set.Of(5, 6, 7, 8),
			expectedOverlaps: false,
		},
		{
			name:             "small fetch known",
			set:              set.Of(3),
			expectedOverlaps: true,
		},
		{
			name:             "large fetch known",
			set:              set.Of(3, 5, 6, 7, 8),
			expectedOverlaps: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overlaps := m.HasOverlap(test.set)
			require.Equal(t, test.expectedOverlaps, overlaps)
		})
	}
}

func TestSetMapHasKeyAndGetSet(t *testing.T) {
	m := New[int, int]()
	require.Empty(t, m.Put(1, set.Of(2)))

	tests := []struct {
		name           string
		key            int
		expectedValue  set.Set[int]
		expectedExists bool
	}{
		{
			name:           "fetch unknown",
			key:            3,
			expectedValue:  nil,
			expectedExists: false,
		},
		{
			name:           "fetch known key",
			key:            1,
			expectedValue:  set.Of(2),
			expectedExists: true,
		},
		{
			name:           "fetch known value",
			key:            2,
			expectedValue:  nil,
			expectedExists: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			exists := m.HasKey(test.key)
			require.Equal(test.expectedExists, exists)

			value, exists := m.GetSet(test.key)
			require.Equal(test.expectedValue, value)
			require.Equal(test.expectedExists, exists)
		})
	}
}

func TestSetMapDeleteKey(t *testing.T) {
	tests := []struct {
		name            string
		state           *SetMap[int, int]
		key             int
		expectedValue   set.Set[int]
		expectedRemoved bool
		expectedState   *SetMap[int, int]
	}{
		{
			name:            "none removed",
			state:           New[int, int](),
			key:             1,
			expectedValue:   nil,
			expectedRemoved: false,
			expectedState:   New[int, int](),
		},
		{
			name: "key removed",
			state: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(2),
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			key:             1,
			expectedValue:   set.Of(2),
			expectedRemoved: true,
			expectedState:   New[int, int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			value, removed := test.state.DeleteKey(test.key)
			require.Equal(test.expectedValue, value)
			require.Equal(test.expectedRemoved, removed)
			require.Equal(test.expectedState, test.state)
		})
	}
}

func TestSetMapDeleteValue(t *testing.T) {
	tests := []struct {
		name            string
		state           *SetMap[int, int]
		value           int
		expectedKey     int
		expectedSet     set.Set[int]
		expectedRemoved bool
		expectedState   *SetMap[int, int]
	}{
		{
			name:            "none removed",
			state:           New[int, int](),
			value:           1,
			expectedKey:     0,
			expectedSet:     nil,
			expectedRemoved: false,
			expectedState:   New[int, int](),
		},
		{
			name: "key removed",
			state: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(2),
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			value:           2,
			expectedKey:     1,
			expectedSet:     set.Of(2),
			expectedRemoved: true,
			expectedState:   New[int, int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			key, set, removed := test.state.DeleteValue(test.value)
			require.Equal(test.expectedKey, key)
			require.Equal(test.expectedSet, set)
			require.Equal(test.expectedRemoved, removed)
			require.Equal(test.expectedState, test.state)
		})
	}
}

func TestSetMapDeleteOverlapping(t *testing.T) {
	tests := []struct {
		name            string
		state           *SetMap[int, int]
		set             set.Set[int]
		expectedRemoved []Entry[int, int]
		expectedState   *SetMap[int, int]
	}{
		{
			name:            "none removed",
			state:           New[int, int](),
			set:             set.Of(1),
			expectedRemoved: nil,
			expectedState:   New[int, int](),
		},
		{
			name: "key removed",
			state: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(2),
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			set: set.Of(2),
			expectedRemoved: []Entry[int, int]{
				{
					Key: 1,
					Set: set.Of(2),
				},
			},
			expectedState: New[int, int](),
		},
		{
			name: "multiple keys removed",
			state: &SetMap[int, int]{
				keyToSet: map[int]set.Set[int]{
					1: set.Of(2, 3),
					2: set.Of(4),
				},
				valueToKey: map[int]int{
					2: 1,
					3: 1,
					4: 2,
				},
			},
			set: set.Of(2, 4),
			expectedRemoved: []Entry[int, int]{
				{
					Key: 1,
					Set: set.Of(2, 3),
				},
				{
					Key: 2,
					Set: set.Of(4),
				},
			},
			expectedState: New[int, int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			removed := test.state.DeleteOverlapping(test.set)
			require.ElementsMatch(test.expectedRemoved, removed)
			require.Equal(test.expectedState, test.state)
		})
	}
}

func TestSetMapLen(t *testing.T) {
	require := require.New(t)

	m := New[int, int]()
	require.Zero(m.Len())
	require.Zero(m.LenValues())

	m.Put(1, set.Of(2))
	require.Equal(1, m.Len())
	require.Equal(1, m.LenValues())

	m.Put(2, set.Of(3, 4))
	require.Equal(2, m.Len())
	require.Equal(3, m.LenValues())

	m.Put(1, set.Of(4, 5))
	require.Equal(1, m.Len())
	require.Equal(2, m.LenValues())

	m.DeleteKey(1)
	require.Zero(m.Len())
	require.Zero(m.LenValues())
}
