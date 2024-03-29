// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	childNode.setValue(maybe.Some([]byte("value")))
	require.NotNil(t, childNode)

	childNode.calculateID(&mockMetrics{})
	root.addChild(childNode, 4)

	data := root.bytes()
	rootParsed, err := parseNode(ToKey([]byte("")), data)
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
	childNode1.setValue(maybe.Some([]byte("value1")))
	require.NotNil(t, childNode1)

	childNode1.calculateID(&mockMetrics{})
	root.addChild(childNode1, 4)

	fullKey = ToKey([]byte{237})
	childNode2 := newNode(fullKey)
	root.addChild(childNode2, 4)
	childNode2.setValue(maybe.Some([]byte("value2")))
	require.NotNil(t, childNode2)

	childNode2.calculateID(&mockMetrics{})
	root.addChild(childNode2, 4)

	data := root.bytes()

	for i := 1; i < len(data); i++ {
		broken := data[:i]
		_, err := parseNode(ToKey([]byte("")), broken)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}

func Benchmark_CalculateID(b *testing.B) {
	benchmarks := []struct {
		name string
		n    *node
	}{
		{
			name: "empty node",
			n:    newNode(Key{}),
		},
		{
			name: "has value",
			n: func() *node {
				n := newNode(Key{})
				n.setValue(maybe.Some([]byte("value1")))
				return n
			}(),
		},
		{
			name: "1 child",
			n: func() *node {
				n := newNode(Key{})
				childNode := newNode(ToKey([]byte{255}))
				n.addChild(childNode, 4)
				childNode.setValue(maybe.Some([]byte("value1")))
				return n
			}(),
		},
		{
			name: "2 children",
			n: func() *node {
				n := newNode(Key{})

				childNode1 := newNode(ToKey([]byte{255}))
				childNode1.setValue(maybe.Some([]byte("value1")))

				childNode2 := newNode(ToKey([]byte{237}))
				childNode2.setValue(maybe.Some([]byte("value2")))

				n.addChild(childNode1, 4)
				n.addChild(childNode2, 4)
				return n
			}(),
		},
		{
			name: "16 children",
			n: func() *node {
				n := newNode(Key{})

				for i := byte(0); i < 16; i++ {
					childNode := newNode(ToKey([]byte{i << 4}))
					childNode.setValue(maybe.Some([]byte("some value")))

					n.addChild(childNode, 4)
				}
				return n
			}(),
		},
	}
	for _, benchmark := range benchmarks {
		ignoredMetrics := &mockMetrics{}
		b.Run(benchmark.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				benchmark.n.calculateID(ignoredMetrics)
			}
		})
	}
}
