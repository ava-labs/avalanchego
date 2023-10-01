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
	root := newNode(nil, emptyPath(BranchFactor16))
	require.NotNil(t, root)

	fullpath := NewPath([]byte("key"), BranchFactor16)
	childNode := newNode(root, fullpath)
	childNode.setValue(maybe.Some([]byte("value")))
	require.NotNil(t, childNode)

	childNode.calculateID(&mockMetrics{})
	root.addChild(childNode)

	data := root.bytes()
	rootParsed, err := parseNode(NewPath([]byte(""), BranchFactor16), data)
	require.NoError(t, err)
	require.Len(t, rootParsed.children, 1)

	rootIndex := getSingleChildPath(root).Token(root.key.tokensLength)
	parsedIndex := getSingleChildPath(rootParsed).Token(rootParsed.key.tokensLength)
	rootChildEntry := root.children[rootIndex]
	parseChildEntry := rootParsed.children[parsedIndex]
	require.Equal(t, rootChildEntry.id, parseChildEntry.id)
}

func Test_Node_Marshal_Errors(t *testing.T) {
	root := newNode(nil, emptyPath(BranchFactor16))
	require.NotNil(t, root)

	fullpath := NewPath([]byte{255}, BranchFactor16)
	childNode1 := newNode(root, fullpath)
	childNode1.setValue(maybe.Some([]byte("value1")))
	require.NotNil(t, childNode1)

	childNode1.calculateID(&mockMetrics{})
	root.addChild(childNode1)

	fullpath = NewPath([]byte{237}, BranchFactor16)
	childNode2 := newNode(root, fullpath)
	childNode2.setValue(maybe.Some([]byte("value2")))
	require.NotNil(t, childNode2)

	childNode2.calculateID(&mockMetrics{})
	root.addChild(childNode2)

	data := root.bytes()

	for i := 1; i < len(data); i++ {
		broken := data[:i]
		_, err := parseNode(NewPath([]byte(""), BranchFactor16), broken)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}
