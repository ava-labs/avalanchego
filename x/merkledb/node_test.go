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
	root := newNode()
	require.NotNil(t, root)

	fullKey := ToKey([]byte("key"))
	childNode := newNode()
	root.setChildEntry(fullKey.Token(0, 4), child{compressedKey: fullKey.Skip(4)})
	childNode.setValue(maybe.Some([]byte("value")))
	require.NotNil(t, childNode)

	root.setChildEntry(fullKey.Token(0, 4), child{
		compressedKey: fullKey.Skip(4),
		id:            childNode.calculateID(fullKey, &mockMetrics{})},
	)

	data := root.bytes()
	rootParsed, err := parseNode(data)
	require.NoError(t, err)
	require.Len(t, rootParsed.children, 1)

	rootIndex := getSingleChildKey(Key{}, root, 4).Token(0, 4)
	parsedIndex := getSingleChildKey(Key{}, rootParsed, 4).Token(0, 4)
	rootChildEntry := root.children[rootIndex]
	parseChildEntry := rootParsed.children[parsedIndex]
	require.Equal(t, rootChildEntry.id, parseChildEntry.id)
}

func Test_Node_Marshal_Errors(t *testing.T) {
	root := newNode()
	require.NotNil(t, root)

	fullKey := ToKey([]byte{255})
	childNode1 := newNode()
	childNode1.setValue(maybe.Some([]byte("value1")))

	root.setChildEntry(fullKey.Token(0, 4), child{
		compressedKey: fullKey.Skip(4),
		id:            childNode1.calculateID(fullKey, &mockMetrics{}),
		hasValue:      true,
	},
	)

	fullKey = ToKey([]byte{237})
	childNode2 := newNode()
	childNode2.setValue(maybe.Some([]byte("value2")))

	root.setChildEntry(fullKey.Token(0, 4), child{
		compressedKey: fullKey.Skip(4),
		id:            childNode2.calculateID(fullKey, &mockMetrics{}),
		hasValue:      true,
	},
	)

	data := root.bytes()

	for i := 1; i < len(data); i++ {
		broken := data[:i]
		_, err := parseNode(broken)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}
