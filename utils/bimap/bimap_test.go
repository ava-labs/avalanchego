// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bimap

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBiMapPut(t *testing.T) {
	tests := []struct {
		name            string
		state           *BiMap[int, int]
		key             int
		value           int
		expectedRemoved []Entry[int, int]
		expectedState   *BiMap[int, int]
	}{
		{
			name:            "none removed",
			state:           New[int, int](),
			key:             1,
			value:           2,
			expectedRemoved: nil,
			expectedState: &BiMap[int, int]{
				keyToValue: map[int]int{
					1: 2,
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
		},
		{
			name: "key removed",
			state: &BiMap[int, int]{
				keyToValue: map[int]int{
					1: 2,
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			key:   1,
			value: 3,
			expectedRemoved: []Entry[int, int]{
				{
					Key:   1,
					Value: 2,
				},
			},
			expectedState: &BiMap[int, int]{
				keyToValue: map[int]int{
					1: 3,
				},
				valueToKey: map[int]int{
					3: 1,
				},
			},
		},
		{
			name: "value removed",
			state: &BiMap[int, int]{
				keyToValue: map[int]int{
					1: 2,
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			key:   3,
			value: 2,
			expectedRemoved: []Entry[int, int]{
				{
					Key:   1,
					Value: 2,
				},
			},
			expectedState: &BiMap[int, int]{
				keyToValue: map[int]int{
					3: 2,
				},
				valueToKey: map[int]int{
					2: 3,
				},
			},
		},
		{
			name: "key and value removed",
			state: &BiMap[int, int]{
				keyToValue: map[int]int{
					1: 2,
					3: 4,
				},
				valueToKey: map[int]int{
					2: 1,
					4: 3,
				},
			},
			key:   1,
			value: 4,
			expectedRemoved: []Entry[int, int]{
				{
					Key:   1,
					Value: 2,
				},
				{
					Key:   3,
					Value: 4,
				},
			},
			expectedState: &BiMap[int, int]{
				keyToValue: map[int]int{
					1: 4,
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
			require.Equal(test.expectedRemoved, removed)
			require.Equal(test.expectedState, test.state)
		})
	}
}

func TestBiMapHasValueAndGetKey(t *testing.T) {
	m := New[int, int]()
	require.Empty(t, m.Put(1, 2))

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

func TestBiMapHasKeyAndGetValue(t *testing.T) {
	m := New[int, int]()
	require.Empty(t, m.Put(1, 2))

	tests := []struct {
		name           string
		key            int
		expectedValue  int
		expectedExists bool
	}{
		{
			name:           "fetch unknown",
			key:            3,
			expectedValue:  0,
			expectedExists: false,
		},
		{
			name:           "fetch known key",
			key:            1,
			expectedValue:  2,
			expectedExists: true,
		},
		{
			name:           "fetch known value",
			key:            2,
			expectedValue:  0,
			expectedExists: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			exists := m.HasKey(test.key)
			require.Equal(test.expectedExists, exists)

			value, exists := m.GetValue(test.key)
			require.Equal(test.expectedValue, value)
			require.Equal(test.expectedExists, exists)
		})
	}
}

func TestBiMapDeleteKey(t *testing.T) {
	tests := []struct {
		name            string
		state           *BiMap[int, int]
		key             int
		expectedValue   int
		expectedRemoved bool
		expectedState   *BiMap[int, int]
	}{
		{
			name:            "none removed",
			state:           New[int, int](),
			key:             1,
			expectedValue:   0,
			expectedRemoved: false,
			expectedState:   New[int, int](),
		},
		{
			name: "key removed",
			state: &BiMap[int, int]{
				keyToValue: map[int]int{
					1: 2,
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			key:             1,
			expectedValue:   2,
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

func TestBiMapDeleteValue(t *testing.T) {
	tests := []struct {
		name            string
		state           *BiMap[int, int]
		value           int
		expectedKey     int
		expectedRemoved bool
		expectedState   *BiMap[int, int]
	}{
		{
			name:            "none removed",
			state:           New[int, int](),
			value:           1,
			expectedKey:     0,
			expectedRemoved: false,
			expectedState:   New[int, int](),
		},
		{
			name: "key removed",
			state: &BiMap[int, int]{
				keyToValue: map[int]int{
					1: 2,
				},
				valueToKey: map[int]int{
					2: 1,
				},
			},
			value:           2,
			expectedKey:     1,
			expectedRemoved: true,
			expectedState:   New[int, int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			key, removed := test.state.DeleteValue(test.value)
			require.Equal(test.expectedKey, key)
			require.Equal(test.expectedRemoved, removed)
			require.Equal(test.expectedState, test.state)
		})
	}
}

func TestBiMapLenAndLists(t *testing.T) {
	require := require.New(t)

	m := New[int, int]()
	require.Zero(m.Len())
	require.Empty(m.Keys())
	require.Empty(m.Values())

	m.Put(1, 2)
	require.Equal(1, m.Len())
	require.ElementsMatch([]int{1}, m.Keys())
	require.ElementsMatch([]int{2}, m.Values())

	m.Put(2, 3)
	require.Equal(2, m.Len())
	require.ElementsMatch([]int{1, 2}, m.Keys())
	require.ElementsMatch([]int{2, 3}, m.Values())

	m.Put(1, 3)
	require.Equal(1, m.Len())
	require.ElementsMatch([]int{1}, m.Keys())
	require.ElementsMatch([]int{3}, m.Values())

	m.DeleteKey(1)
	require.Zero(m.Len())
	require.Empty(m.Keys())
	require.Empty(m.Values())
}

func TestBiMapJSON(t *testing.T) {
	require := require.New(t)

	expectedMap := New[int, int]()
	expectedMap.Put(1, 2)
	expectedMap.Put(2, 3)

	jsonBytes, err := json.Marshal(expectedMap)
	require.NoError(err)

	expectedJSONBytes := []byte(`{"1":2,"2":3}`)
	require.JSONEq(string(expectedJSONBytes), string(jsonBytes))

	var unmarshalledMap BiMap[int, int]
	require.NoError(json.Unmarshal(jsonBytes, &unmarshalledMap))
	require.Equal(expectedMap, &unmarshalledMap)
}

func TestBiMapInvalidJSON(t *testing.T) {
	require := require.New(t)

	invalidJSONBytes := []byte(`{"1":2,"2":2}`)
	var unmarshalledMap BiMap[int, int]
	err := json.Unmarshal(invalidJSONBytes, &unmarshalledMap)
	require.ErrorIs(err, errNotBijective)
}
