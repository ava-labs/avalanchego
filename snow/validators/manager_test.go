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

	err = Add(m, subnetID, nodeID, nil, ids.Empty, 1)
	require.NoError(err)

	weight := s.Weight()
	require.EqualValues(1, weight)
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

	err = Add(m, subnetID, nodeID, nil, ids.Empty, 1)
	require.NoError(err)

	err = AddWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	weight := s.Weight()
	require.EqualValues(2, weight)
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

	err = Add(m, subnetID, nodeID, nil, ids.Empty, 2)
	require.NoError(err)

	err = RemoveWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	weight := s.Weight()
	require.EqualValues(1, weight)

	err = RemoveWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	weight = s.Weight()
	require.Zero(weight)
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

	err := Add(m, subnetID, nodeID, nil, ids.Empty, 1)
	require.NoError(err)

	contains = Contains(m, subnetID, nodeID)
	require.True(contains)

	err = RemoveWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	contains = Contains(m, subnetID, nodeID)
	require.False(contains)
}
