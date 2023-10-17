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
	root := newNode(Key{})
	require.NotNil(t, root)

	fullKey := ToKey([]byte("key"))
	childNode := newNode(fullKey)
	root.addChild(BranchFactor16TokenConfig, childNode)
	childNode.setValue(maybe.Some([]byte("value")))
	require.NotNil(t, childNode)

	childNode.calculateID(&mockMetrics{})
	root.addChild(BranchFactor16TokenConfig, childNode)

	data := root.bytes()
	rootParsed, err := parseNode(BranchFactor16TokenConfig, ToKey([]byte("")), data)
	require.NoError(t, err)
	require.Len(t, rootParsed.children, 1)

	rootIndex := getSingleChildKey(BranchFactor16TokenConfig, root).Token(BranchFactor16TokenConfig, 0)
	parsedIndex := getSingleChildKey(BranchFactor16TokenConfig, rootParsed).Token(BranchFactor16TokenConfig, 0)
	rootChildEntry := root.children[rootIndex]
	parseChildEntry := rootParsed.children[parsedIndex]
	require.Equal(t, rootChildEntry.id, parseChildEntry.id)
}

func Test_Node_Marshal_Errors(t *testing.T) {
	root := newNode(Key{})
	require.NotNil(t, root)

	fullKey := ToKey([]byte{255})
	childNode1 := newNode(fullKey)
	root.addChild(BranchFactor16TokenConfig, childNode1)
	childNode1.setValue(maybe.Some([]byte("value1")))
	require.NotNil(t, childNode1)

	childNode1.calculateID(&mockMetrics{})
	root.addChild(BranchFactor16TokenConfig, childNode1)

	fullKey = ToKey([]byte{237})
	childNode2 := newNode(fullKey)
	root.addChild(BranchFactor16TokenConfig, childNode2)
	childNode2.setValue(maybe.Some([]byte("value2")))
	require.NotNil(t, childNode2)

	childNode2.calculateID(&mockMetrics{})
	root.addChild(BranchFactor16TokenConfig, childNode2)

	data := root.bytes()

	for i := 1; i < len(data); i++ {
		broken := data[:i]
		_, err := parseNode(BranchFactor16TokenConfig, ToKey([]byte("")), broken)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}
