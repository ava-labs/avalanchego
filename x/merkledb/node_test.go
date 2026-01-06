// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	root.addChild(childNode, 4)
	childNode.setValue(DefaultHasher, maybe.Some([]byte("value")))
	require.NotNil(t, childNode)

	root.addChild(childNode, 4)

	data := root.bytes()
	rootParsed, err := parseNode(DefaultHasher, ToKey([]byte("")), data)
	require.NoError(t, err)
	require.Len(t, rootParsed.children, 1)

	rootIndex := getSingleChildKey(root, 4).Token(0, 4)
	parsedIndex := getSingleChildKey(rootParsed, 4).Token(0, 4)
	rootChildEntry := root.children[rootIndex]
	parseChildEntry := rootParsed.children[parsedIndex]
	require.Equal(t, rootChildEntry.id, parseChildEntry.id)
}

func Test_Node_Marshal_Errors(t *testing.T) {
	root := newNode(Key{})
	require.NotNil(t, root)

	fullKey := ToKey([]byte{255})
	childNode1 := newNode(fullKey)
	root.addChild(childNode1, 4)
	childNode1.setValue(DefaultHasher, maybe.Some([]byte("value1")))
	require.NotNil(t, childNode1)

	root.addChild(childNode1, 4)

	fullKey = ToKey([]byte{237})
	childNode2 := newNode(fullKey)
	root.addChild(childNode2, 4)
	childNode2.setValue(DefaultHasher, maybe.Some([]byte("value2")))
	require.NotNil(t, childNode2)

	root.addChild(childNode2, 4)

	data := root.bytes()

	for i := 1; i < len(data); i++ {
		broken := data[:i]
		_, err := parseNode(DefaultHasher, ToKey([]byte("")), broken)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}
