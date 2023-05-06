// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_View_Iteration(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{0}, []byte{0}))
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{1}, []byte{1}))

	trie2, err := dbTrie.NewView()
	require.NoError(t, err)
	require.NoError(t, trie2.Insert(context.Background(), []byte{1}, []byte{2}))
	require.NoError(t, trie2.Insert(context.Background(), []byte{2}, []byte{2}))
	require.NoError(t, trie2.Insert(context.Background(), []byte{3}, []byte{3}))

	trie3, err := trie2.NewView()
	require.NoError(t, err)
	require.NoError(t, trie3.Remove(context.Background(), []byte{3}))
	require.NoError(t, trie3.Insert(context.Background(), []byte{4}, []byte{4}))

	it := trie3.NewIterator()
	values := make([]KeyValue, 0, 4)
	for it.Next() {
		values = append(values, KeyValue{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	require.Len(t, values, 4)
	require.Equal(t, KeyValue{Key: []byte{0}, Value: []byte{0}}, values[0])
	require.Equal(t, KeyValue{Key: []byte{1}, Value: []byte{2}}, values[1])
	require.Equal(t, KeyValue{Key: []byte{2}, Value: []byte{2}}, values[2])
	require.Equal(t, KeyValue{Key: []byte{4}, Value: []byte{4}}, values[3])
}

func Test_View_Iteration_Start_Prefix(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{0}, []byte{0}))
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{0, 1}, []byte{1}))
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{1, 1}, []byte{1}))

	trie2, err := dbTrie.NewView()
	require.NoError(t, err)
	require.NoError(t, trie2.Insert(context.Background(), []byte{0, 1}, []byte{2}))
	require.NoError(t, trie2.Insert(context.Background(), []byte{0, 2}, []byte{2}))
	require.NoError(t, trie2.Insert(context.Background(), []byte{0, 3}, []byte{3}))
	require.NoError(t, trie2.Insert(context.Background(), []byte{1, 3}, []byte{3}))

	trie3, err := trie2.NewView()
	require.NoError(t, err)
	require.NoError(t, trie3.Remove(context.Background(), []byte{1, 1}))
	require.NoError(t, trie3.Remove(context.Background(), []byte{0, 3}))
	require.NoError(t, trie3.Insert(context.Background(), []byte{0, 4}, []byte{4}))
	require.NoError(t, trie3.Insert(context.Background(), []byte{1, 4}, []byte{4}))

	it := trie3.NewIteratorWithStartAndPrefix([]byte{0, 2}, []byte{0})
	values := make([]KeyValue, 0, 2)
	for it.Next() {
		values = append(values, KeyValue{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	require.Len(t, values, 2)
	require.Equal(t, KeyValue{Key: []byte{0, 2}, Value: []byte{2}}, values[0])
	require.Equal(t, KeyValue{Key: []byte{0, 4}, Value: []byte{4}}, values[1])
}
