// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestRequests(t *testing.T) {
	req := Requests{}

	length := req.Len()
	require.Equal(t, 0, length, "should have had no outstanding requests")

	_, removed := req.Remove(ids.EmptyNodeID, 0)
	require.False(t, removed, "shouldn't have removed the request")

	removed = req.RemoveAny(ids.Empty)
	require.False(t, removed, "shouldn't have removed the request")

	constains := req.Contains(ids.Empty)
	require.False(t, constains, "shouldn't contain this request")

	req.Add(ids.EmptyNodeID, 0, ids.Empty)

	length = req.Len()
	require.Equal(t, 1, length, "should have had one outstanding request")

	_, removed = req.Remove(ids.EmptyNodeID, 1)
	require.False(t, removed, "shouldn't have removed the request")

	_, removed = req.Remove(ids.NodeID{1}, 0)
	require.False(t, removed, "shouldn't have removed the request")

	constains = req.Contains(ids.Empty)
	require.True(t, constains, "should contain this request")

	length = req.Len()
	require.Equal(t, 1, length, "should have had one outstanding request")

	req.Add(ids.EmptyNodeID, 10, ids.Empty.Prefix(0))

	length = req.Len()
	require.Equal(t, 2, length, "should have had two outstanding requests")

	_, removed = req.Remove(ids.EmptyNodeID, 1)
	require.False(t, removed, "shouldn't have removed the request")

	_, removed = req.Remove(ids.NodeID{1}, 0)
	require.False(t, removed, "shouldn't have removed the request")

	constains = req.Contains(ids.Empty)
	require.True(t, constains, "should contain this request")

	length = req.Len()
	require.Equal(t, 2, length, "should have had two outstanding requests")

	removedID, removed := req.Remove(ids.EmptyNodeID, 0)
	require.Equal(t, ids.Empty, removedID, "should have removed the requested ID")
	require.True(t, removed, "should have removed the request")

	removedID, removed = req.Remove(ids.EmptyNodeID, 10)
	require.Equal(t, ids.Empty.Prefix(0), removedID, "should have removed the requested ID")
	require.True(t, removed, "should have removed the request")

	length = req.Len()
	require.Equal(t, 0, length, "should have had no outstanding requests")

	req.Add(ids.EmptyNodeID, 0, ids.Empty)

	length = req.Len()
	require.Equal(t, 1, length, "should have had one outstanding request")

	removed = req.RemoveAny(ids.Empty)
	require.True(t, removed, "should have removed the request")

	length = req.Len()
	require.Equal(t, 0, length, "should have had no outstanding requests")

	removed = req.RemoveAny(ids.Empty)
	require.False(t, removed, "shouldn't have removed the request")

	length = req.Len()
	require.Equal(t, 0, length, "should have had no outstanding requests")
}
