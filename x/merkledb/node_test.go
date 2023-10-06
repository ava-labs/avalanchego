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
	root := newNode(nil, emptyKey(BranchFactor16))
	require.NotNil(t, root)

	fullKey := ToKey([]byte("key"), BranchFactor16)
	childNode := newNode(root, fullKey)
	childNode.setValue(maybe.Some([]byte("value")))
	require.NotNil(t, childNode)

	childNode.calculateID(&mockMetrics{})
	root.addChild(childNode)

	data := root.bytes()
	rootParsed, err := parseNode(ToKey([]byte(""), BranchFactor16), data)
	require.NoError(t, err)
	require.Len(t, rootParsed.children, 1)

	rootIndex := getSingleChildKey(root).Token(root.key.tokenLength)
	parsedIndex := getSingleChildKey(rootParsed).Token(rootParsed.key.tokenLength)
	rootChildEntry := root.children[rootIndex]
	parseChildEntry := rootParsed.children[parsedIndex]
	require.Equal(t, rootChildEntry.id, parseChildEntry.id)
}

func Test_Node_Marshal_Errors(t *testing.T) {
	root := newNode(nil, emptyKey(BranchFactor16))
	require.NotNil(t, root)

	fullKey := ToKey([]byte{255}, BranchFactor16)
	childNode1 := newNode(root, fullKey)
	childNode1.setValue(maybe.Some([]byte("value1")))
	require.NotNil(t, childNode1)

	childNode1.calculateID(&mockMetrics{})
	root.addChild(childNode1)

	fullKey = ToKey([]byte{237}, BranchFactor16)
	childNode2 := newNode(root, fullKey)
	childNode2.setValue(maybe.Some([]byte("value2")))
	require.NotNil(t, childNode2)

	childNode2.calculateID(&mockMetrics{})
	root.addChild(childNode2)

	data := root.bytes()

	for i := 1; i < len(data); i++ {
		broken := data[:i]
		_, err := parseNode(ToKey([]byte(""), BranchFactor16), broken)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}
