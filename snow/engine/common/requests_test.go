// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestRequests(t *testing.T) {
	require := require.New(t)

	req := Requests{}

	require.Empty(req)

	_, removed := req.Remove(ids.EmptyNodeID, 0)
	require.False(removed)

	require.False(req.RemoveAny(ids.Empty))
	require.False(req.Contains(ids.Empty))

	req.Add(ids.EmptyNodeID, 0, ids.Empty)
	require.Equal(1, req.Len())

	_, removed = req.Remove(ids.EmptyNodeID, 1)
	require.False(removed)

	_, removed = req.Remove(ids.BuildTestNodeID([]byte{0x01}), 0)
	require.False(removed)

	require.True(req.Contains(ids.Empty))
	require.Equal(1, req.Len())

	req.Add(ids.EmptyNodeID, 10, ids.Empty.Prefix(0))
	require.Equal(2, req.Len())

	_, removed = req.Remove(ids.EmptyNodeID, 1)
	require.False(removed)

	_, removed = req.Remove(ids.BuildTestNodeID([]byte{0x01}), 0)
	require.False(removed)

	require.True(req.Contains(ids.Empty))
	require.Equal(2, req.Len())

	removedID, removed := req.Remove(ids.EmptyNodeID, 0)
	require.True(removed)
	require.Equal(ids.Empty, removedID)

	removedID, removed = req.Remove(ids.EmptyNodeID, 10)
	require.True(removed)
	require.Equal(ids.Empty.Prefix(0), removedID)

	require.Zero(req.Len())

	req.Add(ids.EmptyNodeID, 0, ids.Empty)
	require.Equal(1, req.Len())

	require.True(req.RemoveAny(ids.Empty))
	require.Zero(req.Len())

	require.False(req.RemoveAny(ids.Empty))
	require.Zero(req.Len())
}
