// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Node_Marshal(t *testing.T) {
	require := require.New(t)

	root := newNode(nil, EmptyPath)
	require.NotNil(root)

	fullpath := newPath([]byte("key"))
	childNode := newNode(root, fullpath)
	childNode.setValue(Some([]byte("value")))
	require.NotNil(childNode)

	require.NoError(childNode.calculateID(&mockMetrics{}))
	root.addChild(childNode)

	data, err := root.marshal()
	require.NoError(err)
	rootParsed, err := parseNode(newPath([]byte("")), data)
	require.NoError(err)
	require.Len(rootParsed.children, 1)

	rootIndex := root.getSingleChildPath()[len(root.key)]
	parsedIndex := rootParsed.getSingleChildPath()[len(rootParsed.key)]
	rootChildEntry := root.children[rootIndex]
	parseChildEntry := rootParsed.children[parsedIndex]
	require.Equal(rootChildEntry.id, parseChildEntry.id)
}

func Test_Node_Marshal_Errors(t *testing.T) {
	require := require.New(t)

	root := newNode(nil, EmptyPath)
	require.NotNil(root)

	fullpath := newPath([]byte{255})
	childNode1 := newNode(root, fullpath)
	childNode1.setValue(Some([]byte("value1")))
	require.NotNil(childNode1)

	require.NoError(childNode1.calculateID(&mockMetrics{}))
	root.addChild(childNode1)

	fullpath = newPath([]byte{237})
	childNode2 := newNode(root, fullpath)
	childNode2.setValue(Some([]byte("value2")))
	require.NotNil(childNode2)

	require.NoError(childNode2.calculateID(&mockMetrics{}))
	root.addChild(childNode2)

	data, err := root.marshal()
	require.NoError(err)

	for i := 1; i < len(data); i++ {
		broken := data[:i]
		_, err = parseNode(newPath([]byte("")), broken)
		require.ErrorIs(err, io.ErrUnexpectedEOF)
	}
}
