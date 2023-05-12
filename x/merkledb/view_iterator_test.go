// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/stretchr/testify/require"
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

func Test_View_Iteration2(t *testing.T) {
	key0 := []byte{0}
	value0 := []byte{0}
	key1 := []byte{1}
	value1 := []byte{1}
	key2 := []byte{2}
	value2 := []byte{2}
	key3 := []byte{3}
	value3 := []byte{3}
	key4 := []byte{4}
	value4 := []byte{4}

	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	require.NoError(t, dbTrie.Insert(context.Background(), key0, value0))
	require.NoError(t, dbTrie.Insert(context.Background(), key1, value1))

	trie1, err := dbTrie.NewView()
	require.NoError(t, err)
	it := trie1.NewIterator()
	values := make([]KeyValue, 0, 4)
	for it.Next() {
		values = append(values, KeyValue{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	require.Equal(t, len(values), 2)
	require.Equal(t, KeyValue{Key: key0, Value: value0}, values[0])
	require.Equal(t, KeyValue{Key: key1, Value: value1}, values[1])

	// remove key0 from trie1, but key1 still exists in dbTrie
	require.NoError(t, trie1.Remove(context.Background(), key0))
	trie11, err := dbTrie.NewView()
	require.NoError(t, err)
	it = trie11.NewIterator()
	values = make([]KeyValue, 0, 4)
	for it.Next() {
		values = append(values, KeyValue{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	require.Equal(t, len(values), 2)
	require.Equal(t, KeyValue{Key: key0, Value: value0}, values[0])
	require.Equal(t, KeyValue{Key: key1, Value: value1}, values[1])

	// key0 doesn't exist in trie1
	it = trie1.NewIterator()
	values = make([]KeyValue, 0, 4)
	for it.Next() {
		values = append(values, KeyValue{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	require.Equal(t, len(values), 1)
	require.Equal(t, KeyValue{Key: key1, Value: value1}, values[0])

	// key0, key1 still exist in dbTrie
	trie2, err := dbTrie.NewView()
	require.NoError(t, err)
	require.NoError(t, trie2.Insert(context.Background(), key1, value2))
	require.NoError(t, trie2.Insert(context.Background(), key2, value2))
	require.NoError(t, trie2.Insert(context.Background(), key3, value3))
	it = trie2.NewIterator()
	values = make([]KeyValue, 0, 4)
	for it.Next() {
		values = append(values, KeyValue{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	require.Equal(t, len(values), 4)
	require.Equal(t, KeyValue{Key: key0, Value: value0}, values[0])
	require.Equal(t, KeyValue{Key: key1, Value: value2}, values[1])
	require.Equal(t, KeyValue{Key: key2, Value: value2}, values[2])
	require.Equal(t, KeyValue{Key: key3, Value: value3}, values[3])

	// key0, key1, key2, key3 exist in trie2
	trie3, err := trie2.NewView()
	require.NoError(t, err)
	require.NoError(t, trie3.Remove(context.Background(), key3))
	require.NoError(t, trie3.Insert(context.Background(), key4, value4))
	it = trie3.NewIterator()
	values = make([]KeyValue, 0, 4)
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

	// Teh dbTrie contains {key0, value0}, {key1, value1}
	// the dbTrie do not change while its views got changed
	trie1, err = dbTrie.NewView()
	require.NoError(t, err)
	it = trie1.NewIterator()
	values = make([]KeyValue, 0, 4)
	for it.Next() {
		values = append(values, KeyValue{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	require.Equal(t, len(values), 2)
	require.Equal(t, KeyValue{Key: key0, Value: value0}, values[0])
	require.Equal(t, KeyValue{Key: key1, Value: value1}, values[1])

	// After commit trie3 to dbTrie, dbTrie will contains changes of trie3
	trie3.commitToDB(context.Background())
	trie1, err = dbTrie.NewView()
	require.NoError(t, err)
	it = trie1.NewIterator()
	values = make([]KeyValue, 0, 4)
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

func Test_View_Iteration_getValues(t *testing.T) {
	dbTrie, err := getBasicDB()

	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{0}, []byte{0}))
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{0, 1}, []byte{1}))
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{1, 0}, []byte{2}))
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{1, 1}, []byte{3}))
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{2, 0}, []byte{4}))
	require.NoError(t, dbTrie.Insert(context.Background(), []byte{2, 1}, []byte{5}))

	trieView, verr := newTrieView(dbTrie, dbTrie, dbTrie.root.clone(), 10)
	require.NoError(t, verr)
	require.NotNil(t, trieView)

	// if maxLength is zero, get nothing
	kv, kverr := trieView.getKeyValues([]byte{0, 1}, []byte{1, 1},
		0, /*maxLength*/
		set.Set[string]{},
		false /*lock*/)
	require.NotNil(t, kverr)

	// There are 3 outputs
	kv, kverr = trieView.getKeyValues([]byte{0, 1}, []byte{1, 1},
		10, /*maxLength*/
		set.Set[string]{},
		false /*lock*/)
	require.Nil(t, kverr)
	require.Equal(t, 3, len(kv))
	require.Equal(t, KeyValue{Key: []byte{0, 1}, Value: []byte{1}}, kv[0])
	require.Equal(t, KeyValue{Key: []byte{1, 0}, Value: []byte{2}}, kv[1])
	require.Equal(t, KeyValue{Key: []byte{1, 1}, Value: []byte{3}}, kv[2])

	// There are 5 outputs
	kv, kverr = trieView.getKeyValues([]byte{0, 1}, []byte{2, 1},
		10, /*maxLength*/
		set.Set[string]{},
		false /*lock*/)
	require.Nil(t, kverr)
	require.Equal(t, 5, len(kv))
	require.Equal(t, KeyValue{Key: []byte{0, 1}, Value: []byte{1}}, kv[0])
	require.Equal(t, KeyValue{Key: []byte{1, 0}, Value: []byte{2}}, kv[1])
	require.Equal(t, KeyValue{Key: []byte{1, 1}, Value: []byte{3}}, kv[2])
	require.Equal(t, KeyValue{Key: []byte{2, 0}, Value: []byte{4}}, kv[3])
	require.Equal(t, KeyValue{Key: []byte{2, 1}, Value: []byte{5}}, kv[4])

	// there are 2 outputs
	kv, kverr = trieView.getKeyValues([]byte{0, 1}, []byte{2, 1},
		2, /*maxLength*/
		set.Set[string]{},
		false /*lock*/)
	require.Nil(t, kverr)
	require.Equal(t, 2, len(kv))
	require.Equal(t, KeyValue{Key: []byte{0, 1}, Value: []byte{1}}, kv[0])
	require.Equal(t, KeyValue{Key: []byte{1, 0}, Value: []byte{2}}, kv[1])
}
