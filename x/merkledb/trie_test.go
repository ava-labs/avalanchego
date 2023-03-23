// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
		if err := asTrieView.calculateNodeIDs(context.Background()); err != nil {
			return nil, err
		}
		path := newPath([]byte(key))
		nodePath, err := asTrieView.getPathTo(path)
		if err != nil {
			return nil, err
		}
		closestNode := nodePath[len(nodePath)-1]
		if closestNode.key.Compare(path) != 0 || closestNode == nil {
			return nil, database.ErrNotFound
		}

		return closestNode.value.value, nil
	}
	if asDatabases, ok := t.(*Database); ok {
		view, err := asDatabases.NewView()
		if err != nil {
			return nil, err
		}
		path := newPath([]byte(key))
		nodePath, err := view.(*trieView).getPathTo(path)
		if err != nil {
			return nil, err
		}
		closestNode := nodePath[len(nodePath)-1]
		if closestNode.key.Compare(path) != 0 || closestNode == nil {
			return nil, database.ErrNotFound
		}

		return closestNode.value.value, nil
	}
	return nil, nil
}

func Test_GetValue_Safety(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	trieView, err := db.NewView()
	require.NoError(err)

	require.NoError(trieView.Insert(context.Background(), []byte{0}, []byte{0}))
	trieVal, err := trieView.GetValue(context.Background(), []byte{0})
	require.NoError(err)
	require.Equal([]byte{0}, trieVal)
	trieVal[0] = 1

	// should still be []byte{0} after edit
	trieVal, err = trieView.GetValue(context.Background(), []byte{0})
	require.NoError(err)
	require.Equal([]byte{0}, trieVal)
}

func Test_GetValues_Safety(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	trieView, err := db.NewView()
	require.NoError(err)

	require.NoError(trieView.Insert(context.Background(), []byte{0}, []byte{0}))
	trieVals, errs := trieView.GetValues(context.Background(), [][]byte{{0}})
	require.Len(errs, 1)
	require.NoError(errs[0])
	require.Equal([]byte{0}, trieVals[0])
	trieVals[0][0] = 1
	require.Equal([]byte{1}, trieVals[0])

	// should still be []byte{0} after edit
	trieVals, errs = trieView.GetValues(context.Background(), [][]byte{{0}})
	require.Len(errs, 1)
	require.NoError(errs[0])
	require.Equal([]byte{0}, trieVals[0])
}

func TestTrieViewGetPathTo(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	trieIntf, err := db.NewView()
	require.NoError(err)
	trie, ok := trieIntf.(*trieView)
	require.True(ok)

	path, err := trie.getPathTo(newPath(nil))
	require.NoError(err)

	// Just the root
	require.Len(path, 1)
	require.Equal(trie.root, path[0])

	// Insert a key
	key1 := []byte{0}
	err = trie.Insert(context.Background(), key1, []byte("value"))
	require.NoError(err)
	err = trie.calculateNodeIDs(context.Background())
	require.NoError(err)

	path, err = trie.getPathTo(newPath(key1))
	require.NoError(err)

	// Root and 1 value
	require.Len(path, 2)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key1), path[1].key)

	// Insert another key which is a child of the first
	key2 := []byte{0, 1}
	err = trie.Insert(context.Background(), key2, []byte("value"))
	require.NoError(err)
	err = trie.calculateNodeIDs(context.Background())
	require.NoError(err)

	path, err = trie.getPathTo(newPath(key2))
	require.NoError(err)
	require.Len(path, 3)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key1), path[1].key)
	require.Equal(newPath(key2), path[2].key)

	// Insert a key which shares no prefix with the others
	key3 := []byte{255}
	err = trie.Insert(context.Background(), key3, []byte("value"))
	require.NoError(err)
	err = trie.calculateNodeIDs(context.Background())
	require.NoError(err)

	path, err = trie.getPathTo(newPath(key3))
	require.NoError(err)
	require.Len(path, 2)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key3), path[1].key)

	// Other key paths not affected
	path, err = trie.getPathTo(newPath(key2))
	require.NoError(err)
	require.Len(path, 3)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key1), path[1].key)
	require.Equal(newPath(key2), path[2].key)

	// Gets closest node when key doesn't exist
	key4 := []byte{0, 1, 2}
	path, err = trie.getPathTo(newPath(key4))
	require.NoError(err)
	require.Len(path, 3)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key1), path[1].key)
	require.Equal(newPath(key2), path[2].key)

	// Gets just root when key doesn't exist and no key shares a prefix
	key5 := []byte{128}
	path, err = trie.getPathTo(newPath(key5))
	require.NoError(err)
	require.Len(path, 1)
	require.Equal(trie.root, path[0])
}

func Test_Trie_ViewOnCommitedView(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)

	committedTrie, err := dbTrie.NewView()
	require.NoError(t, err)
	err = committedTrie.Insert(context.Background(), []byte{0}, []byte{0})
	require.NoError(t, err)

	require.NoError(t, committedTrie.CommitToDB(context.Background()))

	newView, err := committedTrie.NewView()
	require.NoError(t, err)

	err = newView.Insert(context.Background(), []byte{1}, []byte{1})
	require.NoError(t, err)
	require.NoError(t, newView.CommitToDB(context.Background()))

	val0, err := dbTrie.GetValue(context.Background(), []byte{0})
	require.NoError(t, err)
	require.Equal(t, []byte{0}, val0)
	val1, err := dbTrie.GetValue(context.Background(), []byte{1})
	require.NoError(t, err)
	require.Equal(t, []byte{1}, val1)
}

func Test_Trie_Partial_Commit_Leaves_Valid_Tries(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)

	trie2, err := dbTrie.NewView()
	require.NoError(t, err)
	err = trie2.Insert(context.Background(), []byte("key"), []byte("value"))
	require.NoError(t, err)

	trie3, err := trie2.NewView()
	require.NoError(t, err)
	err = trie3.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)

	trie4, err := trie3.NewView()
	require.NoError(t, err)
	err = trie4.Insert(context.Background(), []byte("key2"), []byte("value2"))
	require.NoError(t, err)

	trie5, err := trie4.NewView()
	require.NoError(t, err)
	err = trie5.Insert(context.Background(), []byte("key3"), []byte("value3"))
	require.NoError(t, err)

	err = trie3.CommitToDB(context.Background())
	require.NoError(t, err)

	root, err := trie3.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	dbRoot, err := dbTrie.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	require.Equal(t, root, dbRoot)
}

func Test_Trie_WriteToDB(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView()
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

	err = trie.CommitToDB(context.Background())
	require.NoError(t, err)
	p := newPath([]byte("key"))
	rawBytes, err := dbTrie.nodeDB.Get(p.Bytes())
	require.NoError(t, err)
	node, err := parseNode(p, rawBytes)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), node.value.value)
}

func Test_Trie_InsertAndRetrieve(t *testing.T) {
	dbTrie, err := getBasicDB()
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
	dbTrie, err := getBasicDB()
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
	dbTrie, err := getBasicDB()
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
	trie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, trie)

	err = trie.Remove(context.Background(), []byte("key"))
	require.NoError(t, err)
}

func Test_Trie_ExpandOnKeyPath(t *testing.T) {
	dbTrie, err := getBasicDB()
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
	dbTrie, err := getBasicDB()
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
	dbTrie, err := getBasicDB()
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
	dbTrie, err := getBasicDB()
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
	trie, err := getBasicDB()
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
	view, err := trie.NewView()
	require.NoError(t, err)
	err = view.Remove(context.Background(), []byte("k"))
	require.NoError(t, err)
	err = view.Remove(context.Background(), []byte("ke"))
	require.NoError(t, err)
	err = view.Remove(context.Background(), []byte("key"))
	require.NoError(t, err)
	err = view.CommitToDB(context.Background())
	require.NoError(t, err)

	// the root is the only updated node so only one new hash
	require.Equal(t, oldCount+1, trie.metrics.(*mockMetrics).hashCount)
}

func Test_Trie_NoExistingResidual(t *testing.T) {
	dbTrie, err := getBasicDB()
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

func Test_Trie_CommitChanges(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	view1Intf, err := db.NewView()
	require.NoError(err)
	view1, ok := view1Intf.(*trieView)
	require.True(ok)

	err = view1.Insert(context.Background(), []byte{1}, []byte{1})
	require.NoError(err)

	// view1
	//   |
	//  db

	// Case: Committing to an invalid view
	view1.invalidated = true
	err = view1.commitChanges(context.Background(), &trieView{})
	require.ErrorIs(err, ErrInvalid)
	view1.invalidated = false // Reset

	// Case: Committing a nil view is a no-op
	oldRoot, err := view1.getMerkleRoot(context.Background())
	require.NoError(err)
	err = view1.commitChanges(context.Background(), nil)
	require.NoError(err)
	newRoot, err := view1.getMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(oldRoot, newRoot)

	// Case: Committing a view with the wrong parent.
	err = view1.commitChanges(context.Background(), &trieView{})
	require.ErrorIs(err, ErrViewIsNotAChild)

	// Case: Committing a view which is invalid
	err = view1.commitChanges(context.Background(), &trieView{
		parentTrie:  view1,
		invalidated: true,
	})
	require.ErrorIs(err, ErrInvalid)

	// Make more views atop the existing one
	view2Intf, err := view1.NewView()
	require.NoError(err)
	view2, ok := view2Intf.(*trieView)
	require.True(ok)

	err = view2.Insert(context.Background(), []byte{2}, []byte{2})
	require.NoError(err)
	err = view2.Remove(context.Background(), []byte{1})
	require.NoError(err)

	view2Root, err := view2.getMerkleRoot(context.Background())
	require.NoError(err)

	// view1 has 1 --> 1
	// view2 has 2 --> 2

	view3Intf, err := view1.NewView()
	require.NoError(err)
	view3, ok := view3Intf.(*trieView)
	require.True(ok)

	view4Intf, err := view2.NewView()
	require.NoError(err)
	view4, ok := view4Intf.(*trieView)
	require.True(ok)

	// view4
	//   |
	// view2  view3
	//   |   /
	// view1
	//   |
	//  db

	// Commit view2 to view1
	err = view1.commitChanges(context.Background(), view2)
	require.NoError(err)

	// All siblings of view2 should be invalidated
	require.True(view3.invalidated)

	// Children of view2 are now children of view1
	require.Equal(view1, view4.parentTrie)
	require.Contains(view1.childViews, view4)

	// Value changes from view2 are reflected in view1
	newView1Root, err := view1.getMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(view2Root, newView1Root)
	_, err = view1.GetValue(context.Background(), []byte{1})
	require.ErrorIs(err, database.ErrNotFound)
	got, err := view1.GetValue(context.Background(), []byte{2})
	require.NoError(err)
	require.Equal([]byte{2}, got)
}

func Test_Trie_BatchApply(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView()
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
	trie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, trie)
	newTrie, err := trie.NewView()
	require.NoError(t, err)

	err = newTrie.Insert(context.Background(), []byte("k"), []byte("value0"))
	require.NoError(t, err)
	err = newTrie.Insert(context.Background(), []byte("ke"), []byte("value1"))
	require.NoError(t, err)
	err = newTrie.Insert(context.Background(), []byte("key"), []byte("value2"))
	require.NoError(t, err)
	err = newTrie.Insert(context.Background(), []byte("key1"), []byte("value3"))
	require.NoError(t, err)
	err = newTrie.(*trieView).calculateNodeIDs(context.Background())
	require.NoError(t, err)
	root, err := newTrie.getEditableNode(EmptyPath)
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
	err = newTrie.(*trieView).calculateNodeIDs(context.Background())
	require.NoError(t, err)
	root, err = newTrie.getEditableNode(EmptyPath)
	require.NoError(t, err)
	// since all values have been deleted, the nodes should have been cleaned up
	require.Equal(t, 0, len(root.children))
}

func Test_Trie_Invalidate_Children_On_Edits(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)

	trie, err := dbTrie.NewView()
	require.NoError(t, err)

	childTrie1, err := trie.NewView()
	require.NoError(t, err)
	childTrie2, err := trie.NewView()
	require.NoError(t, err)
	childTrie3, err := trie.NewView()
	require.NoError(t, err)

	require.False(t, childTrie1.(*trieView).isInvalid())
	require.False(t, childTrie2.(*trieView).isInvalid())
	require.False(t, childTrie3.(*trieView).isInvalid())

	err = trie.Insert(context.Background(), []byte{0}, []byte{0})
	require.NoError(t, err)

	require.True(t, childTrie1.(*trieView).isInvalid())
	require.True(t, childTrie2.(*trieView).isInvalid())
	require.True(t, childTrie3.(*trieView).isInvalid())
}

func Test_Trie_Invalidate_Siblings_On_Commit(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)

	baseView, err := dbTrie.NewView()
	require.NoError(t, err)

	viewToCommit, err := baseView.NewView()
	require.NoError(t, err)

	sibling1, err := baseView.NewView()
	require.NoError(t, err)
	sibling2, err := baseView.NewView()
	require.NoError(t, err)

	require.False(t, sibling1.(*trieView).isInvalid())
	require.False(t, sibling2.(*trieView).isInvalid())

	require.NoError(t, viewToCommit.Insert(context.Background(), []byte{0}, []byte{0}))
	require.NoError(t, viewToCommit.CommitToDB(context.Background()))

	require.True(t, sibling1.(*trieView).isInvalid())
	require.True(t, sibling2.(*trieView).isInvalid())
	require.False(t, viewToCommit.(*trieView).isInvalid())
}

func Test_Trie_NodeCollapse(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView()
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

	err = trie.(*trieView).calculateNodeIDs(context.Background())
	require.NoError(t, err)
	root, err := trie.getEditableNode(EmptyPath)
	require.NoError(t, err)
	require.Equal(t, 1, len(root.children))

	root, err = trie.getEditableNode(EmptyPath)
	require.NoError(t, err)
	require.Equal(t, 1, len(root.children))

	firstNode, err := trie.getEditableNode(root.getSingleChildPath())
	require.NoError(t, err)
	require.Equal(t, 1, len(firstNode.children))

	// delete the middle values
	err = trie.Remove(context.Background(), []byte("k"))
	require.NoError(t, err)
	err = trie.Remove(context.Background(), []byte("ke"))
	require.NoError(t, err)
	err = trie.Remove(context.Background(), []byte("key"))
	require.NoError(t, err)

	err = trie.(*trieView).calculateNodeIDs(context.Background())
	require.NoError(t, err)

	root, err = trie.getEditableNode(EmptyPath)
	require.NoError(t, err)
	require.Equal(t, 1, len(root.children))

	firstNode, err = trie.getEditableNode(root.getSingleChildPath())
	require.NoError(t, err)
	require.Equal(t, 2, len(firstNode.children))
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
					Tracer:        newNoopTracer(),
					HistoryLength: 100,
					NodeCacheSize: 100,
				},
			)
			require.NoError(t, err)
			defer db.Close()

			initialSet := 1000
			// Populate initial set of keys
			root, err := db.NewView()
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
				require.NoError(t, root.CommitToDB(context.Background()))
			}

			// Populate additional states
			concurrentStates := []Trie{}
			for i := 0; i < 5; i++ {
				newState, err := root.NewView()
				require.NoError(t, err)
				concurrentStates = append(concurrentStates, newState)
			}

			if commitApproach == "after" {
				require.NoError(t, root.CommitToDB(context.Background()))
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

func TestNewViewOnCommittedView(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create a view
	view1Intf, err := db.NewView()
	require.NoError(err)
	view1, ok := view1Intf.(*trieView)
	require.True(ok)

	// view1
	//   |
	//  db

	require.Len(db.childViews, 1)
	require.Contains(db.childViews, view1)
	require.Equal(db, view1.parentTrie)

	err = view1.Insert(context.Background(), []byte{1}, []byte{1})
	require.NoError(err)

	// Commit the view
	err = view1.CommitToDB(context.Background())
	require.NoError(err)

	// view1 (committed)
	//   |
	//  db

	require.Len(db.childViews, 1)
	require.Contains(db.childViews, view1)
	require.Equal(db, view1.parentTrie)

	// Create a new view on the committed view
	view2Intf, err := view1.NewView()
	require.NoError(err)
	view2, ok := view2Intf.(*trieView)
	require.True(ok)

	// view2
	//   |
	// view1 (committed)
	//   |
	//  db

	require.Equal(db, view2.parentTrie)
	require.Contains(db.childViews, view1)
	require.Contains(db.childViews, view2)
	require.Len(db.childViews, 2)

	// Make sure the new view has the right value
	got, err := view2.GetValue(context.Background(), []byte{1})
	require.NoError(err)
	require.Equal([]byte{1}, got)

	// Make another view
	view3Intf, err := view2.NewView()
	require.NoError(err)
	view3, ok := view3Intf.(*trieView)
	require.True(ok)

	// view3
	//   |
	// view2
	//   |
	// view1 (committed)
	//   |
	//  db

	require.Equal(view2, view3.parentTrie)
	require.Contains(view2.childViews, view3)
	require.Len(view2.childViews, 1)
	require.Contains(db.childViews, view1)
	require.Contains(db.childViews, view2)
	require.Len(db.childViews, 2)

	// Commit view2
	err = view2.CommitToDB(context.Background())
	require.NoError(err)

	// view3
	//   |
	// view2 (committed)
	//   |
	// view1 (committed)
	//   |
	//  db

	// Note that view2 being committed invalidates view1
	require.True(view1.invalidated)
	require.Contains(db.childViews, view2)
	require.Contains(db.childViews, view3)
	require.Len(db.childViews, 2)
	require.Equal(db, view3.parentTrie)

	// Commit view3
	err = view3.CommitToDB(context.Background())
	require.NoError(err)

	// view3 being committed invalidates view2
	require.True(view2.invalidated)
	require.Contains(db.childViews, view3)
	require.Len(db.childViews, 1)
	require.Equal(db, view3.parentTrie)
}

func Test_TrieView_NewView(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create a view
	view1Intf, err := db.NewView()
	require.NoError(err)
	view1, ok := view1Intf.(*trieView)
	require.True(ok)

	// Create a view atop view1
	view2Intf, err := view1.NewView()
	require.NoError(err)
	view2, ok := view2Intf.(*trieView)
	require.True(ok)

	// view2
	//   |
	// view1
	//   |
	//  db

	// Assert view2's parent is view1
	require.Equal(view1, view2.parentTrie)
	require.Contains(view1.childViews, view2)
	require.Len(view1.childViews, 1)

	// Commit view1
	err = view1.CommitToDB(context.Background())
	require.NoError(err)

	// Make another view atop view1
	view3Intf, err := view1.NewView()
	require.NoError(err)
	view3, ok := view3Intf.(*trieView)
	require.True(ok)

	// view3
	//   |
	// view2
	//   |
	// view1
	//   |
	//  db

	// Assert view3's parent is db
	require.Equal(db, view3.parentTrie)
	require.Contains(db.childViews, view3)
	require.NotContains(view1.childViews, view3)

	// Assert that NewPreallocatedView on an invalid view fails
	invalidView := &trieView{invalidated: true}
	_, err = invalidView.NewView()
	require.ErrorIs(err, ErrInvalid)
}

func TestTrieViewInvalidate(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create a view
	view1Intf, err := db.NewView()
	require.NoError(err)
	view1, ok := view1Intf.(*trieView)
	require.True(ok)

	// Create 2 views atop view1
	view2Intf, err := view1.NewView()
	require.NoError(err)
	view2, ok := view2Intf.(*trieView)
	require.True(ok)

	view3Intf, err := view1.NewView()
	require.NoError(err)
	view3, ok := view3Intf.(*trieView)
	require.True(ok)

	// view2  view3
	//   |    /
	//   view1
	//     |
	//     db

	// Invalidate view1
	view1.invalidate()

	require.Empty(view1.childViews)
	require.True(view1.invalidated)
	require.True(view2.invalidated)
	require.True(view3.invalidated)
}

func TestTrieViewMoveChildViewsToView(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create a view
	view1Intf, err := db.NewView()
	require.NoError(err)
	view1, ok := view1Intf.(*trieView)
	require.True(ok)

	// Create a view atop view1
	view2Intf, err := view1.NewView()
	require.NoError(err)
	view2, ok := view2Intf.(*trieView)
	require.True(ok)

	// Create a view atop view2
	view3Intf, err := view1.NewView()
	require.NoError(err)
	view3, ok := view3Intf.(*trieView)
	require.True(ok)

	// view3
	//   |
	// view2
	//   |
	// view1
	//   |
	//   db

	view1.moveChildViewsToView(view2)

	require.Equal(view1, view3.parentTrie)
	require.Contains(view1.childViews, view3)
	require.Contains(view1.childViews, view2)
	require.Len(view1.childViews, 2)
}

func TestTrieViewInvalidChildrenExcept(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create a view
	view1Intf, err := db.NewView()
	require.NoError(err)
	view1, ok := view1Intf.(*trieView)
	require.True(ok)

	// Create 2 views atop view1
	view2Intf, err := view1.NewView()
	require.NoError(err)
	view2, ok := view2Intf.(*trieView)
	require.True(ok)

	view3Intf, err := view1.NewView()
	require.NoError(err)
	view3, ok := view3Intf.(*trieView)
	require.True(ok)

	view1.invalidateChildrenExcept(view2)

	require.False(view2.invalidated)
	require.True(view3.invalidated)
	require.Contains(view1.childViews, view2)
	require.Len(view1.childViews, 1)

	view1.invalidateChildrenExcept(nil)
	require.True(view2.invalidated)
	require.True(view3.invalidated)
	require.Empty(view1.childViews)
}
