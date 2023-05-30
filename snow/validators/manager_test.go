// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAdd(t *testing.T) {
	require := require.New(t)

	m := NewManager()

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	err := Add(m, subnetID, nodeID, nil, ids.Empty, 1)
	require.ErrorIs(err, errMissingValidators)

	s := NewSet()
	m.Add(subnetID, s)

	require.NoError(Add(m, subnetID, nodeID, nil, ids.Empty, 1))

	require.Equal(uint64(1), s.Weight())
}

func TestAddWeight(t *testing.T) {
	require := require.New(t)

	m := NewManager()

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	err := AddWeight(m, subnetID, nodeID, 1)
	require.ErrorIs(err, errMissingValidators)

	s := NewSet()
	m.Add(subnetID, s)

	err = AddWeight(m, subnetID, nodeID, 1)
	require.ErrorIs(err, errMissingValidator)

	require.NoError(Add(m, subnetID, nodeID, nil, ids.Empty, 1))

	require.NoError(AddWeight(m, subnetID, nodeID, 1))

	require.Equal(uint64(2), s.Weight())
}

func TestRemoveWeight(t *testing.T) {
	require := require.New(t)

	m := NewManager()

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	err := RemoveWeight(m, subnetID, nodeID, 1)
	require.ErrorIs(err, errMissingValidators)

	s := NewSet()
	m.Add(subnetID, s)

	require.NoError(Add(m, subnetID, nodeID, nil, ids.Empty, 2))

	require.NoError(RemoveWeight(m, subnetID, nodeID, 1))

	require.Equal(uint64(1), s.Weight())

	require.NoError(RemoveWeight(m, subnetID, nodeID, 1))

	require.Zero(s.Weight())
}

func TestContains(t *testing.T) {
	require := require.New(t)

	m := NewManager()

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	contains := Contains(m, subnetID, nodeID)
	require.False(contains)

	s := NewSet()
	m.Add(subnetID, s)

	contains = Contains(m, subnetID, nodeID)
	require.False(contains)

	require.NoError(Add(m, subnetID, nodeID, nil, ids.Empty, 1))

	contains = Contains(m, subnetID, nodeID)
	require.True(contains)

	require.NoError(RemoveWeight(m, subnetID, nodeID, 1))

	contains = Contains(m, subnetID, nodeID)
	require.False(contains)
}
