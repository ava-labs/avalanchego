// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thepudds/fzgen/fuzzer"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

func FuzzMarshalDiffKey(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var (
			subnetID ids.ID
			height   uint64
			nodeID   ids.NodeID
		)
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&subnetID, &height, &nodeID)

		key := marshalDiffKeyBySubnet(subnetID, height, nodeID)
		parsedSubnetID, parsedHeight, parsedNodeID, err := unmarshalDiffKeyBySubnet(key)
		require.NoError(err)
		require.Equal(subnetID, parsedSubnetID)
		require.Equal(height, parsedHeight)
		require.Equal(nodeID, parsedNodeID)
	})
}

func FuzzUnmarshalDiffKey(f *testing.F) {
	f.Fuzz(func(t *testing.T, key []byte) {
		require := require.New(t)

		subnetID, height, nodeID, err := unmarshalDiffKeyBySubnet(key)
		if err != nil {
			require.ErrorIs(err, errUnexpectedDiffKeyLength)
			return
		}

		formattedKey := marshalDiffKeyBySubnet(subnetID, height, nodeID)
		require.Equal(key, formattedKey)
	})
}

func TestDiffIteration(t *testing.T) {
	require := require.New(t)

	db := memdb.New()

	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()

	nodeID0 := ids.BuildTestNodeID([]byte{0x00})
	nodeID1 := ids.BuildTestNodeID([]byte{0x01})

	subnetID0Height0NodeID0 := marshalDiffKeyBySubnet(subnetID0, 0, nodeID0)
	subnetID0Height1NodeID0 := marshalDiffKeyBySubnet(subnetID0, 1, nodeID0)
	subnetID0Height1NodeID1 := marshalDiffKeyBySubnet(subnetID0, 1, nodeID1)

	subnetID1Height0NodeID0 := marshalDiffKeyBySubnet(subnetID1, 0, nodeID0)
	subnetID1Height1NodeID0 := marshalDiffKeyBySubnet(subnetID1, 1, nodeID0)
	subnetID1Height1NodeID1 := marshalDiffKeyBySubnet(subnetID1, 1, nodeID1)

	require.NoError(db.Put(subnetID0Height0NodeID0, nil))
	require.NoError(db.Put(subnetID0Height1NodeID0, nil))
	require.NoError(db.Put(subnetID0Height1NodeID1, nil))
	require.NoError(db.Put(subnetID1Height0NodeID0, nil))
	require.NoError(db.Put(subnetID1Height1NodeID0, nil))
	require.NoError(db.Put(subnetID1Height1NodeID1, nil))

	{
		it := db.NewIteratorWithStartAndPrefix(marshalStartDiffKey(subnetID0, 0), subnetID0[:])
		defer it.Release()

		expectedKeys := [][]byte{
			subnetID0Height0NodeID0,
		}
		for _, expectedKey := range expectedKeys {
			require.True(it.Next())
			require.Equal(expectedKey, it.Key())
		}
		require.False(it.Next())
		require.NoError(it.Error())
	}

	{
		it := db.NewIteratorWithStartAndPrefix(marshalStartDiffKey(subnetID0, 1), subnetID0[:])
		defer it.Release()

		expectedKeys := [][]byte{
			subnetID0Height1NodeID0,
			subnetID0Height1NodeID1,
			subnetID0Height0NodeID0,
		}
		for _, expectedKey := range expectedKeys {
			require.True(it.Next())
			require.Equal(expectedKey, it.Key())
		}
		require.False(it.Next())
		require.NoError(it.Error())
	}
}
