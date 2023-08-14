// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/maybe"
)

func Test_Node_Marshal(t *testing.T) {
	root := newNode(nil, EmptyPath)
	require.NotNil(t, root)

	fullpath := newPath([]byte("key"))
	childNode := newNode(root, fullpath)
	childNode.setValue(maybe.Some([]byte("value")))
	require.NotNil(t, childNode)

	require.NoError(t, childNode.calculateID(&mockMetrics{}))
	root.addChild(childNode)

	data, err := root.marshal()
	require.NoError(t, err)
	rootParsed, err := parseNode(newPath([]byte("")), data)
	require.NoError(t, err)
	require.Len(t, rootParsed.children, 1)

	rootIndex := root.getSingleChildPath()[len(root.key)]
	parsedIndex := rootParsed.getSingleChildPath()[len(rootParsed.key)]
	rootChildEntry := root.children[rootIndex]
	parseChildEntry := rootParsed.children[parsedIndex]
	require.Equal(t, rootChildEntry.id, parseChildEntry.id)
}

func Test_Node_Marshal_Errors(t *testing.T) {
	root := newNode(nil, EmptyPath)
	require.NotNil(t, root)

	fullpath := newPath([]byte{255})
	childNode1 := newNode(root, fullpath)
	childNode1.setValue(maybe.Some([]byte("value1")))
	require.NotNil(t, childNode1)

	require.NoError(t, childNode1.calculateID(&mockMetrics{}))
	root.addChild(childNode1)

	fullpath = newPath([]byte{237})
	childNode2 := newNode(root, fullpath)
	childNode2.setValue(maybe.Some([]byte("value2")))
	require.NotNil(t, childNode2)

	require.NoError(t, childNode2.calculateID(&mockMetrics{}))
	root.addChild(childNode2)

	data, err := root.marshal()
	require.NoError(t, err)

	for i := 1; i < len(data); i++ {
		broken := data[:i]
		_, err = parseNode(newPath([]byte("")), broken)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}
