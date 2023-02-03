// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func getNodeValue(t ReadOnlyTrie, key string) ([]byte, error) {
	if asTrieView, ok := t.(*trieView); ok {
		if err := asTrieView.CalculateIDs(context.Background()); err != nil {
			return nil, err
		}
		closestNode, exact, err := asTrieView.getClosestNode(context.Background(), newPath([]byte(key)))
		if err != nil {
			return nil, err
		}
		if !exact || closestNode == nil {
			return nil, database.ErrNotFound
		}

		return closestNode.value.value, nil
	}
	if asDatabases, ok := t.(*Database); ok {
		view, err := asDatabases.NewView(context.Background())
		if err != nil {
			return nil, err
		}
		closestNode, exact, err := view.(*trieView).getClosestNode(context.Background(), newPath([]byte(key)))
		if err != nil {
			return nil, err
		}
		if !exact || closestNode == nil {
			return nil, database.ErrNotFound
		}

		return closestNode.value.value, nil
	}
	return nil, nil
}

func Test_Trie_Partial_Commit_Leaves_Valid_Tries(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)

	trie2, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)
	err = trie2.Insert(context.Background(), []byte("key"), []byte("value"))
	require.NoError(t, err)

	trie3, err := trie2.NewView(context.Background())
	require.NoError(t, err)
	err = trie3.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)

	trie4, err := trie3.NewView(context.Background())
	require.NoError(t, err)
	err = trie4.Insert(context.Background(), []byte("key2"), []byte("value2"))
	require.NoError(t, err)

	trie5, err := trie4.NewView(context.Background())
	require.NoError(t, err)
	err = trie5.Insert(context.Background(), []byte("key3"), []byte("value3"))
	require.NoError(t, err)

	err = trie3.Commit(context.Background())
	require.NoError(t, err)

	root, err := trie3.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	dbRoot, err := dbTrie.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	require.Equal(t, root, dbRoot)
}

func Test_Trie_Collapse_After_Commit(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie1 := Trie(dbTrie)

	trie2, err := trie1.NewView(context.Background())
	require.NoError(t, err)
	err = trie2.Insert(context.Background(), []byte("key"), []byte("value"))
	require.NoError(t, err)
	trie2Root, err := trie2.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	trie3, err := trie2.NewView(context.Background())
	require.NoError(t, err)
	err = trie3.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)
	trie3Root, err := trie3.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	trie4, err := trie3.NewView(context.Background())
	require.NoError(t, err)
	err = trie4.Insert(context.Background(), []byte("key2"), []byte("value2"))
	require.NoError(t, err)
	trie4Root, err := trie4.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	err = trie4.Commit(context.Background())
	require.NoError(t, err)

	root, err := trie4.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	dbRoot, err := dbTrie.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	require.Equal(t, root, dbRoot)

	// ensure each root is in the history
	require.Contains(t, dbTrie.history.lastChanges, trie2Root)
	require.Contains(t, dbTrie.history.lastChanges, trie3Root)
	require.Contains(t, dbTrie.history.lastChanges, trie4Root)

	// ensure that they are in the correct order
	_, _ = dbTrie.history.history.DeleteMin() // First one is root; ignore
	got, ok := dbTrie.history.history.DeleteMin()
	require.True(t, ok)
	require.Equal(t, trie2Root, got.rootID)
	got, ok = dbTrie.history.history.DeleteMin()
	require.True(t, ok)
	require.Equal(t, trie3Root, got.rootID)
	got, ok = dbTrie.history.history.DeleteMin()
	require.True(t, ok)
	require.Equal(t, trie4Root, got.rootID)
}

func Test_Trie_WriteToDB(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)

	// value hasn't been inserted so shouldn't exist
	value, err := trie.GetValue(context.Background(), []byte("key"))
	require.Error(t, err)
	require.Equal(t, database.ErrNotFound, err)
	require.Nil(t, value)

	err = trie.Insert(context.Background(), []byte("key"), []byte("value"))
	require.NoError(t, err)

	value, err = getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value)

	err = trie.Commit(context.Background())
	require.NoError(t, err)
	p := newPath([]byte("key"))
	rawBytes, err := dbTrie.nodeDB.Get(p.Bytes())
	require.NoError(t, err)
	node, err := parseNode(p, rawBytes)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), node.value.value)
}

func Test_Trie_InsertAndRetrieve(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie := Trie(dbTrie)

	// value hasn't been inserted so shouldn't exist
	value, err := dbTrie.Get([]byte("key"))
	require.Error(t, err)
	require.Equal(t, database.ErrNotFound, err)
	require.Nil(t, value)

	err = trie.Insert(context.Background(), []byte("key"), []byte("value"))
	require.NoError(t, err)

	value, err = getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value)
}

func Test_Trie_Overwrite(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie := Trie(dbTrie)

	err = trie.Insert(context.Background(), []byte("key"), []byte("value0"))
	require.NoError(t, err)

	value, err := getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value0"), value)

	err = trie.Insert(context.Background(), []byte("key"), []byte("value1"))
	require.NoError(t, err)

	value, err = getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), value)
}

func Test_Trie_Delete(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie := Trie(dbTrie)

	err = trie.Insert(context.Background(), []byte("key"), []byte("value0"))
	require.NoError(t, err)

	value, err := getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value0"), value)

	err = trie.Remove(context.Background(), []byte("key"))
	require.NoError(t, err)

	value, err = getNodeValue(trie, "key")
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, value)
}

func Test_Trie_DeleteMissingKey(t *testing.T) {
	trie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, trie)

	err = trie.Remove(context.Background(), []byte("key"))
	require.NoError(t, err)
}

func Test_Trie_ExpandOnKeyPath(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie := Trie(dbTrie)

	err = trie.Insert(context.Background(), []byte("key"), []byte("value0"))
	require.NoError(t, err)

	value, err := getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value0"), value)

	err = trie.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)

	value, err = getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value0"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), value)

	err = trie.Insert(context.Background(), []byte("key12"), []byte("value12"))
	require.NoError(t, err)

	value, err = getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value0"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), value)

	value, err = getNodeValue(trie, "key12")
	require.NoError(t, err)
	require.Equal(t, []byte("value12"), value)
}

func Test_Trie_CompressedPaths(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie := Trie(dbTrie)

	err = trie.Insert(context.Background(), []byte("key12"), []byte("value12"))
	require.NoError(t, err)

	value, err := getNodeValue(trie, "key12")
	require.NoError(t, err)
	require.Equal(t, []byte("value12"), value)

	err = trie.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)

	value, err = getNodeValue(trie, "key12")
	require.NoError(t, err)
	require.Equal(t, []byte("value12"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), value)

	err = trie.Insert(context.Background(), []byte("key"), []byte("value"))
	require.NoError(t, err)

	value, err = getNodeValue(trie, "key12")
	require.NoError(t, err)
	require.Equal(t, []byte("value12"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), value)

	value, err = getNodeValue(trie, "key")
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value)
}

func Test_Trie_SplitBranch(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie := Trie(dbTrie)

	// force a new node to generate with common prefix "key1" and have these two nodes as children
	err = trie.Insert(context.Background(), []byte("key12"), []byte("value12"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key134"), []byte("value134"))
	require.NoError(t, err)

	value, err := getNodeValue(trie, "key12")
	require.NoError(t, err)
	require.Equal(t, []byte("value12"), value)

	value, err = getNodeValue(trie, "key134")
	require.NoError(t, err)
	require.Equal(t, []byte("value134"), value)
}

func Test_Trie_HashCountOnBranch(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie := Trie(dbTrie)

	// force a new node to generate with common prefix "key1" and have these two nodes as children
	err = trie.Insert(context.Background(), []byte("key12"), []byte("value12"))
	require.NoError(t, err)
	oldCount := dbTrie.metrics.(*mockMetrics).hashCount
	err = trie.Insert(context.Background(), []byte("key134"), []byte("value134"))
	require.NoError(t, err)
	// only hashes the new branch node, the new child node, and root
	// shouldn't hash the existing node
	require.Equal(t, oldCount+3, dbTrie.metrics.(*mockMetrics).hashCount)
}

func Test_Trie_HashCountOnDelete(t *testing.T) {
	trie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, trie)

	err = trie.Insert(context.Background(), []byte("k"), []byte("value0"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("ke"), []byte("value1"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key"), []byte("value2"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key1"), []byte("value3"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key2"), []byte("value4"))
	require.NoError(t, err)

	oldCount := trie.metrics.(*mockMetrics).hashCount

	// delete the middle values
	view, err := trie.NewView(context.Background())
	require.NoError(t, err)
	err = view.Remove(context.Background(), []byte("k"))
	require.NoError(t, err)
	err = view.Remove(context.Background(), []byte("ke"))
	require.NoError(t, err)
	err = view.Remove(context.Background(), []byte("key"))
	require.NoError(t, err)
	err = view.Commit(context.Background())
	require.NoError(t, err)

	// the root is the only updated node so only one new hash
	require.Equal(t, oldCount+1, trie.metrics.(*mockMetrics).hashCount)
}

func Test_Trie_NoExistingResidual(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie := Trie(dbTrie)

	err = trie.Insert(context.Background(), []byte("k"), []byte("1"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("ke"), []byte("2"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key1"), []byte("3"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key123"), []byte("4"))
	require.NoError(t, err)

	value, err := getNodeValue(trie, "k")
	require.NoError(t, err)
	require.Equal(t, []byte("1"), value)

	value, err = getNodeValue(trie, "ke")
	require.NoError(t, err)
	require.Equal(t, []byte("2"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(t, err)
	require.Equal(t, []byte("3"), value)

	value, err = getNodeValue(trie, "key123")
	require.NoError(t, err)
	require.Equal(t, []byte("4"), value)
}

func Test_Trie_BatchApply(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)

	err = trie.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key12"), []byte("value12"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key134"), []byte("value134"))
	require.NoError(t, err)
	err = trie.Remove(context.Background(), []byte("key1"))
	require.NoError(t, err)

	value, err := getNodeValue(trie, "key12")
	require.NoError(t, err)
	require.Equal(t, []byte("value12"), value)

	value, err = getNodeValue(trie, "key134")
	require.NoError(t, err)
	require.Equal(t, []byte("value134"), value)

	_, err = getNodeValue(trie, "key1")
	require.Error(t, err)
	require.Equal(t, database.ErrNotFound, err)
}

func Test_Trie_ChainDeletion(t *testing.T) {
	trie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, trie)
	newTrie, err := trie.NewView(context.Background())
	require.NoError(t, err)

	err = newTrie.Insert(context.Background(), []byte("k"), []byte("value0"))
	require.NoError(t, err)
	err = newTrie.Insert(context.Background(), []byte("ke"), []byte("value1"))
	require.NoError(t, err)
	err = newTrie.Insert(context.Background(), []byte("key"), []byte("value2"))
	require.NoError(t, err)
	err = newTrie.Insert(context.Background(), []byte("key1"), []byte("value3"))
	require.NoError(t, err)
	err = newTrie.(*trieView).CalculateIDs(context.Background())
	require.NoError(t, err)
	root, err := newTrie.getNode(context.Background(), EmptyPath)
	require.NoError(t, err)
	require.Equal(t, 1, len(root.children))

	err = newTrie.Remove(context.Background(), []byte("k"))
	require.NoError(t, err)
	err = newTrie.Remove(context.Background(), []byte("ke"))
	require.NoError(t, err)
	err = newTrie.Remove(context.Background(), []byte("key"))
	require.NoError(t, err)
	err = newTrie.Remove(context.Background(), []byte("key1"))
	require.NoError(t, err)
	err = newTrie.(*trieView).CalculateIDs(context.Background())
	require.NoError(t, err)
	root, err = newTrie.getNode(context.Background(), EmptyPath)
	require.NoError(t, err)
	// since all values have been deleted, the nodes should have been cleaned up
	require.Equal(t, 0, len(root.children))
}

func Test_Trie_NodeCollapse(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)

	err = trie.Insert(context.Background(), []byte("k"), []byte("value0"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("ke"), []byte("value1"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key"), []byte("value2"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key1"), []byte("value3"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key2"), []byte("value4"))
	require.NoError(t, err)

	err = trie.(*trieView).CalculateIDs(context.Background())
	require.NoError(t, err)
	root, err := trie.getNode(context.Background(), EmptyPath)
	require.NoError(t, err)
	require.Equal(t, 1, len(root.children))

	root, err = trie.getNode(context.Background(), EmptyPath)
	require.NoError(t, err)
	require.Equal(t, 1, len(root.children))

	firstNode, err := trie.getNode(context.Background(), root.getSingleChildPath())
	require.NoError(t, err)
	require.Equal(t, 1, len(firstNode.children))

	// delete the middle values
	err = trie.Remove(context.Background(), []byte("k"))
	require.NoError(t, err)
	err = trie.Remove(context.Background(), []byte("ke"))
	require.NoError(t, err)
	err = trie.Remove(context.Background(), []byte("key"))
	require.NoError(t, err)

	err = trie.(*trieView).CalculateIDs(context.Background())
	require.NoError(t, err)

	root, err = trie.getNode(context.Background(), EmptyPath)
	require.NoError(t, err)
	require.Equal(t, 1, len(root.children))

	firstNode, err = trie.getNode(context.Background(), root.getSingleChildPath())
	require.NoError(t, err)
	require.Equal(t, 2, len(firstNode.children))
}

func Test_Trie_Duplicate_Commit(t *testing.T) {
	// create two views with the same changes
	// create a view on top of one of them
	// commit the other duplicate view
	// should still be able to commit the view on top of the uncommitted duplicate

	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)

	err = trie.Insert(context.Background(), []byte("k"), []byte("value0"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("ke"), []byte("value1"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key"), []byte("value2"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key1"), []byte("value3"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key2"), []byte("value4"))
	require.NoError(t, err)

	committedView, err := trie.NewView(context.Background())
	require.NoError(t, err)

	uncommittedView, err := trie.NewView(context.Background())
	require.NoError(t, err)

	err = committedView.Insert(context.Background(), []byte("k2"), []byte("value02"))
	require.NoError(t, err)

	err = uncommittedView.Insert(context.Background(), []byte("k2"), []byte("value02"))
	require.NoError(t, err)

	err = committedView.Commit(context.Background())
	require.NoError(t, err)

	committedRoot, err := committedView.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	uncommittedRoot, err := uncommittedView.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	require.Equal(t, committedRoot, uncommittedRoot)

	newView, err := uncommittedView.NewView(context.Background())
	require.NoError(t, err)

	err = newView.Insert(context.Background(), []byte("k3"), []byte("value03"))
	require.NoError(t, err)

	// ok because uncommittedView's root has already been committed by committedView
	err = newView.Commit(context.Background())
	require.NoError(t, err)
}

func Test_Trie_ChangedRoot(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	err = dbTrie.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)
	trie, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key2"), []byte("value2"))
	require.NoError(t, err)

	err = dbTrie.Insert(context.Background(), []byte("key3"), []byte("value3"))
	require.NoError(t, err)

	_, err = trie.GetValue(context.Background(), []byte("key3"))
	require.ErrorIs(t, err, ErrChangedBaseRoot)
}

func Test_Trie_CommittedView_Validate(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)

	err = trie.Insert(context.Background(), []byte("k"), []byte("value0"))
	require.NoError(t, err)

	committedView, err := trie.NewView(context.Background())
	require.NoError(t, err)

	err = committedView.Commit(context.Background())
	require.NoError(t, err)

	err = committedView.(*trieView).validateDBRoot(context.Background())
	require.NoError(t, err)

	err = dbTrie.Insert(context.Background(), []byte("k2"), []byte("value02"))
	require.NoError(t, err)

	err = committedView.(*trieView).validateDBRoot(context.Background())
	require.ErrorIs(t, err, ErrChangedBaseRoot)
}

func Test_Trie_OtherViewCommitBeforeValidate(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)

	err = trie.Insert(context.Background(), []byte("k"), []byte("value0"))
	require.NoError(t, err)

	committedView, err := trie.NewView(context.Background())
	require.NoError(t, err)

	uncommittedView, err := committedView.NewView(context.Background())
	require.NoError(t, err)

	err = uncommittedView.Insert(context.Background(), []byte("k2"), []byte("value02"))
	require.NoError(t, err)

	err = uncommittedView.Insert(context.Background(), []byte("k3"), []byte("value03"))
	require.NoError(t, err)

	newView, err := uncommittedView.NewView(context.Background())
	require.NoError(t, err)

	err = committedView.Commit(context.Background())
	require.NoError(t, err)

	// should still be valid because db is at root for a view in this view's viewstack
	err = newView.(*trieView).validateDBRoot(context.Background())
	require.NoError(t, err)
}

func Test_Trie_ChangeLock(t *testing.T) {
	dbTrie, err := newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			ValueCacheSize: 1000,
			HistoryLength:  1000,
			NodeCacheSize:  1000,
		},
		&mockMetrics{},
	)
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	err = dbTrie.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)
	trie, err := dbTrie.NewView(context.Background())
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key2"), []byte("value2"))
	require.NoError(t, err)

	higherView, err := trie.NewView(context.Background())
	require.NoError(t, err)

	err = higherView.Insert(context.Background(), []byte("key3"), []byte("value3"))
	require.NoError(t, err)

	err = trie.Insert(context.Background(), []byte("key4"), []byte("value4"))
	require.ErrorIs(t, err, ErrEditLocked)
}

func Test_Trie_MultipleStates(t *testing.T) {
	randCount := int64(0)
	for _, commitApproach := range []string{"never", "before", "after"} {
		t.Run(commitApproach, func(t *testing.T) {
			r := rand.New(rand.NewSource(randCount)) // #nosec G404
			randCount++
			rdb := memdb.New()
			defer rdb.Close()
			db, err := New(
				context.Background(),
				rdb,
				Config{
					Tracer:         newNoopTracer(),
					HistoryLength:  100,
					ValueCacheSize: minCacheSize,
					NodeCacheSize:  100,
				},
			)
			require.NoError(t, err)
			defer db.Close()

			initialSet := 1000
			// Populate initial set of keys
			root, err := db.NewView(context.Background())
			require.NoError(t, err)
			kv := [][]byte{}
			for i := 0; i < initialSet; i++ {
				k := []byte(strconv.Itoa(i))
				kv = append(kv, k)
				require.NoError(t, root.Insert(context.Background(), k, hashing.ComputeHash256(k)))
			}

			// Get initial root
			_, err = root.GetMerkleRoot(context.Background())
			require.NoError(t, err)

			if commitApproach == "before" {
				require.NoError(t, root.Commit(context.Background()))
			}

			// Populate additional states
			concurrentStates := []Trie{}
			for i := 0; i < 5; i++ {
				newState, err := root.NewView(context.Background())
				require.NoError(t, err)
				concurrentStates = append(concurrentStates, newState)
			}

			if commitApproach == "after" {
				require.NoError(t, root.Commit(context.Background()))
			}

			// Process ops
			newStart := initialSet
			for i := 0; i < 100; i++ {
				if r.Intn(100) < 20 {
					// New Key
					for _, state := range concurrentStates {
						k := []byte(strconv.Itoa(newStart))
						require.NoError(t, state.Insert(context.Background(), k, hashing.ComputeHash256(k)))
					}
					newStart++
				} else {
					// Fetch and update old
					selectedKey := kv[r.Intn(len(kv))]
					var pastV []byte
					for _, state := range concurrentStates {
						v, err := state.GetValue(context.Background(), selectedKey)
						require.NoError(t, err)
						if pastV == nil {
							pastV = v
						} else {
							require.Equal(t, pastV, v, "lookup mismatch")
						}
						require.NoError(t, state.Insert(context.Background(), selectedKey, hashing.ComputeHash256(v)))
					}
				}
			}

			// Generate roots
			var pastRoot ids.ID
			for _, state := range concurrentStates {
				mroot, err := state.GetMerkleRoot(context.Background())
				require.NoError(t, err)
				if pastRoot == ids.Empty {
					pastRoot = mroot
				} else {
					require.Equal(t, pastRoot, mroot, "root mismatch")
				}
			}
		})
	}
}
