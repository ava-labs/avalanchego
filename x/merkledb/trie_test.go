// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"

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
	require.NoError(trie.Insert(context.Background(), key1, []byte("value")))
	require.NoError(trie.calculateNodeIDs(context.Background()))

	path, err = trie.getPathTo(newPath(key1))
	require.NoError(err)

	// Root and 1 value
	require.Len(path, 2)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key1), path[1].key)

	// Insert another key which is a child of the first
	key2 := []byte{0, 1}
	require.NoError(trie.Insert(context.Background(), key2, []byte("value")))
	require.NoError(trie.calculateNodeIDs(context.Background()))

	path, err = trie.getPathTo(newPath(key2))
	require.NoError(err)
	require.Len(path, 3)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key1), path[1].key)
	require.Equal(newPath(key2), path[2].key)

	// Insert a key which shares no prefix with the others
	key3 := []byte{255}
	require.NoError(trie.Insert(context.Background(), key3, []byte("value")))
	require.NoError(trie.calculateNodeIDs(context.Background()))

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
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	committedTrie, err := dbTrie.NewView()
	require.NoError(err)
	require.NoError(committedTrie.Insert(context.Background(), []byte{0}, []byte{0}))

	require.NoError(committedTrie.CommitToDB(context.Background()))

	newView, err := committedTrie.NewView()
	require.NoError(err)

	require.NoError(newView.Insert(context.Background(), []byte{1}, []byte{1}))
	require.NoError(newView.CommitToDB(context.Background()))

	val0, err := dbTrie.GetValue(context.Background(), []byte{0})
	require.NoError(err)
	require.Equal([]byte{0}, val0)
	val1, err := dbTrie.GetValue(context.Background(), []byte{1})
	require.NoError(err)
	require.Equal([]byte{1}, val1)
}

func Test_Trie_Partial_Commit_Leaves_Valid_Tries(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	trie2, err := dbTrie.NewView()
	require.NoError(err)
	require.NoError(trie2.Insert(context.Background(), []byte("key"), []byte("value")))

	trie3, err := trie2.NewView()
	require.NoError(err)
	require.NoError(trie3.Insert(context.Background(), []byte("key1"), []byte("value1")))

	trie4, err := trie3.NewView()
	require.NoError(err)
	require.NoError(trie4.Insert(context.Background(), []byte("key2"), []byte("value2")))

	trie5, err := trie4.NewView()
	require.NoError(err)
	require.NoError(trie5.Insert(context.Background(), []byte("key3"), []byte("value3")))

	require.NoError(trie3.CommitToDB(context.Background()))

	root, err := trie3.GetMerkleRoot(context.Background())
	require.NoError(err)

	dbRoot, err := dbTrie.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.Equal(root, dbRoot)
}

func Test_Trie_WriteToDB(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	trie, err := dbTrie.NewView()
	require.NoError(err)

	// value hasn't been inserted so shouldn't exist
	value, err := trie.GetValue(context.Background(), []byte("key"))
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(value)

	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value")))

	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value"), value)

	require.NoError(trie.CommitToDB(context.Background()))

	p := newPath([]byte("key"))
	rawBytes, err := dbTrie.nodeDB.Get(p.Bytes())
	require.NoError(err)

	node, err := parseNode(p, rawBytes)
	require.NoError(err)
	require.Equal([]byte("value"), node.value.value)
}

func Test_Trie_InsertAndRetrieve(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	trie := Trie(dbTrie)

	// value hasn't been inserted so shouldn't exist
	value, err := dbTrie.Get([]byte("key"))
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(value)

	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value")))

	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value"), value)
}

func Test_Trie_Overwrite(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie := Trie(dbTrie)

	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value0")))

	value, err := getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value0"), value)

	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value1")))

	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value1"), value)
}

func Test_Trie_Delete(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie := Trie(dbTrie)

	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value0")))

	value, err := getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value0"), value)

	require.NoError(trie.Remove(context.Background(), []byte("key")))

	value, err = getNodeValue(trie, "key")
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(value)
}

func Test_Trie_DeleteMissingKey(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)

	require.NoError(trie.Remove(context.Background(), []byte("key")))
}

func Test_Trie_ExpandOnKeyPath(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie := Trie(dbTrie)

	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value0")))

	value, err := getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value0"), value)

	require.NoError(trie.Insert(context.Background(), []byte("key1"), []byte("value1")))

	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value0"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(err)
	require.Equal([]byte("value1"), value)

	require.NoError(trie.Insert(context.Background(), []byte("key12"), []byte("value12")))

	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value0"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(err)
	require.Equal([]byte("value1"), value)

	value, err = getNodeValue(trie, "key12")
	require.NoError(err)
	require.Equal([]byte("value12"), value)
}

func Test_Trie_CompressedPaths(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie := Trie(dbTrie)

	require.NoError(trie.Insert(context.Background(), []byte("key12"), []byte("value12")))

	value, err := getNodeValue(trie, "key12")
	require.NoError(err)
	require.Equal([]byte("value12"), value)

	require.NoError(trie.Insert(context.Background(), []byte("key1"), []byte("value1")))

	value, err = getNodeValue(trie, "key12")
	require.NoError(err)
	require.Equal([]byte("value12"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(err)
	require.Equal([]byte("value1"), value)

	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value")))

	value, err = getNodeValue(trie, "key12")
	require.NoError(err)
	require.Equal([]byte("value12"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(err)
	require.Equal([]byte("value1"), value)

	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value"), value)
}

func Test_Trie_SplitBranch(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie := Trie(dbTrie)

	// force a new node to generate with common prefix "key1" and have these two nodes as children
	require.NoError(trie.Insert(context.Background(), []byte("key12"), []byte("value12")))
	require.NoError(trie.Insert(context.Background(), []byte("key134"), []byte("value134")))

	value, err := getNodeValue(trie, "key12")
	require.NoError(err)
	require.Equal([]byte("value12"), value)

	value, err = getNodeValue(trie, "key134")
	require.NoError(err)
	require.Equal([]byte("value134"), value)
}

func Test_Trie_HashCountOnBranch(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie := Trie(dbTrie)

	// force a new node to generate with common prefix "key1" and have these two nodes as children
	require.NoError(trie.Insert(context.Background(), []byte("key12"), []byte("value12")))
	oldCount := dbTrie.metrics.(*mockMetrics).hashCount
	require.NoError(trie.Insert(context.Background(), []byte("key134"), []byte("value134")))
	// only hashes the new branch node, the new child node, and root
	// shouldn't hash the existing node
	require.Equal(oldCount+3, dbTrie.metrics.(*mockMetrics).hashCount)
}

func Test_Trie_HashCountOnDelete(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)

	require.NoError(trie.Insert(context.Background(), []byte("k"), []byte("value0")))
	require.NoError(trie.Insert(context.Background(), []byte("ke"), []byte("value1")))
	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value2")))
	require.NoError(trie.Insert(context.Background(), []byte("key1"), []byte("value3")))
	require.NoError(trie.Insert(context.Background(), []byte("key2"), []byte("value4")))

	oldCount := trie.metrics.(*mockMetrics).hashCount

	// delete the middle values
	view, err := trie.NewView()
	require.NoError(err)
	require.NoError(view.Remove(context.Background(), []byte("k")))
	require.NoError(view.Remove(context.Background(), []byte("ke")))
	require.NoError(view.Remove(context.Background(), []byte("key")))
	require.NoError(view.CommitToDB(context.Background()))

	// the root is the only updated node so only one new hash
	require.Equal(oldCount+1, trie.metrics.(*mockMetrics).hashCount)
}

func Test_Trie_NoExistingResidual(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie := Trie(dbTrie)

	require.NoError(trie.Insert(context.Background(), []byte("k"), []byte("1")))
	require.NoError(trie.Insert(context.Background(), []byte("ke"), []byte("2")))
	require.NoError(trie.Insert(context.Background(), []byte("key1"), []byte("3")))
	require.NoError(trie.Insert(context.Background(), []byte("key123"), []byte("4")))

	value, err := getNodeValue(trie, "k")
	require.NoError(err)
	require.Equal([]byte("1"), value)

	value, err = getNodeValue(trie, "ke")
	require.NoError(err)
	require.Equal([]byte("2"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(err)
	require.Equal([]byte("3"), value)

	value, err = getNodeValue(trie, "key123")
	require.NoError(err)
	require.Equal([]byte("4"), value)
}

func Test_Trie_CommitChanges(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	view1Intf, err := db.NewView()
	require.NoError(err)
	view1, ok := view1Intf.(*trieView)
	require.True(ok)

	require.NoError(view1.Insert(context.Background(), []byte{1}, []byte{1}))

	// view1
	//   |
	//  db

	// Case: Committing to an invalid view
	view1.invalidated = true
	require.ErrorIs(view1.commitChanges(context.Background(), &trieView{}), ErrInvalid)
	view1.invalidated = false // Reset

	// Case: Committing a nil view is a no-op
	oldRoot, err := view1.getMerkleRoot(context.Background())
	require.NoError(err)
	require.NoError(view1.commitChanges(context.Background(), nil))
	newRoot, err := view1.getMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(oldRoot, newRoot)

	// Case: Committing a view with the wrong parent.
	require.ErrorIs(view1.commitChanges(context.Background(), &trieView{}), ErrViewIsNotAChild)

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

	require.NoError(view2.Insert(context.Background(), []byte{2}, []byte{2}))
	require.NoError(view2.Remove(context.Background(), []byte{1}))

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
	require.NoError(view1.commitChanges(context.Background(), view2))

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
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	trie, err := dbTrie.NewView()
	require.NoError(err)

	require.NoError(trie.Insert(context.Background(), []byte("key1"), []byte("value1")))
	require.NoError(trie.Insert(context.Background(), []byte("key12"), []byte("value12")))
	require.NoError(trie.Insert(context.Background(), []byte("key134"), []byte("value134")))
	require.NoError(trie.Remove(context.Background(), []byte("key1")))

	value, err := getNodeValue(trie, "key12")
	require.NoError(err)
	require.Equal([]byte("value12"), value)

	value, err = getNodeValue(trie, "key134")
	require.NoError(err)
	require.Equal([]byte("value134"), value)

	_, err = getNodeValue(trie, "key1")
	require.ErrorIs(err, database.ErrNotFound)
}

func Test_Trie_ChainDeletion(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)
	newTrie, err := trie.NewView()
	require.NoError(err)

	require.NoError(newTrie.Insert(context.Background(), []byte("k"), []byte("value0")))
	require.NoError(newTrie.Insert(context.Background(), []byte("ke"), []byte("value1")))
	require.NoError(newTrie.Insert(context.Background(), []byte("key"), []byte("value2")))
	require.NoError(newTrie.Insert(context.Background(), []byte("key1"), []byte("value3")))
	require.NoError(newTrie.(*trieView).calculateNodeIDs(context.Background()))
	root, err := newTrie.getEditableNode(EmptyPath)
	require.NoError(err)
	require.Len(root.children, 1)

	require.NoError(newTrie.Remove(context.Background(), []byte("k")))
	require.NoError(newTrie.Remove(context.Background(), []byte("ke")))
	require.NoError(newTrie.Remove(context.Background(), []byte("key")))
	require.NoError(newTrie.Remove(context.Background(), []byte("key1")))
	require.NoError(newTrie.(*trieView).calculateNodeIDs(context.Background()))
	root, err = newTrie.getEditableNode(EmptyPath)
	require.NoError(err)
	// since all values have been deleted, the nodes should have been cleaned up
	require.Empty(root.children)
}

func Test_Trie_Invalidate_Children_On_Edits(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	trie, err := dbTrie.NewView()
	require.NoError(err)

	childTrie1, err := trie.NewView()
	require.NoError(err)
	childTrie2, err := trie.NewView()
	require.NoError(err)
	childTrie3, err := trie.NewView()
	require.NoError(err)

	require.False(childTrie1.(*trieView).isInvalid())
	require.False(childTrie2.(*trieView).isInvalid())
	require.False(childTrie3.(*trieView).isInvalid())

	require.NoError(trie.Insert(context.Background(), []byte{0}, []byte{0}))

	require.True(childTrie1.(*trieView).isInvalid())
	require.True(childTrie2.(*trieView).isInvalid())
	require.True(childTrie3.(*trieView).isInvalid())
}

func Test_Trie_Invalidate_Siblings_On_Commit(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	baseView, err := dbTrie.NewView()
	require.NoError(err)

	viewToCommit, err := baseView.NewView()
	require.NoError(err)

	sibling1, err := baseView.NewView()
	require.NoError(err)
	sibling2, err := baseView.NewView()
	require.NoError(err)

	require.False(sibling1.(*trieView).isInvalid())
	require.False(sibling2.(*trieView).isInvalid())

	require.NoError(viewToCommit.Insert(context.Background(), []byte{0}, []byte{0}))
	require.NoError(viewToCommit.CommitToDB(context.Background()))

	require.True(sibling1.(*trieView).isInvalid())
	require.True(sibling2.(*trieView).isInvalid())
	require.False(viewToCommit.(*trieView).isInvalid())
}

func Test_Trie_NodeCollapse(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie, err := dbTrie.NewView()
	require.NoError(err)

	require.NoError(trie.Insert(context.Background(), []byte("k"), []byte("value0")))
	require.NoError(trie.Insert(context.Background(), []byte("ke"), []byte("value1")))
	require.NoError(trie.Insert(context.Background(), []byte("key"), []byte("value2")))
	require.NoError(trie.Insert(context.Background(), []byte("key1"), []byte("value3")))
	require.NoError(trie.Insert(context.Background(), []byte("key2"), []byte("value4")))

	require.NoError(trie.(*trieView).calculateNodeIDs(context.Background()))
	root, err := trie.getEditableNode(EmptyPath)
	require.NoError(err)
	require.Len(root.children, 1)

	root, err = trie.getEditableNode(EmptyPath)
	require.NoError(err)
	require.Len(root.children, 1)

	firstNode, err := trie.getEditableNode(root.getSingleChildPath())
	require.NoError(err)
	require.Len(firstNode.children, 1)

	// delete the middle values
	require.NoError(trie.Remove(context.Background(), []byte("k")))
	require.NoError(trie.Remove(context.Background(), []byte("ke")))
	require.NoError(trie.Remove(context.Background(), []byte("key")))

	require.NoError(trie.(*trieView).calculateNodeIDs(context.Background()))

	root, err = trie.getEditableNode(EmptyPath)
	require.NoError(err)
	require.Len(root.children, 1)

	firstNode, err = trie.getEditableNode(root.getSingleChildPath())
	require.NoError(err)
	require.Len(firstNode.children, 2)
}

func Test_Trie_MultipleStates(t *testing.T) {
	require := require.New(t)

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
			require.NoError(err)
			defer db.Close()

			initialSet := 1000
			// Populate initial set of keys
			root, err := db.NewView()
			require.NoError(err)
			kv := [][]byte{}
			for i := 0; i < initialSet; i++ {
				k := []byte(strconv.Itoa(i))
				kv = append(kv, k)
				require.NoError(root.Insert(context.Background(), k, hashing.ComputeHash256(k)))
			}

			// Get initial root
			_, err = root.GetMerkleRoot(context.Background())
			require.NoError(err)

			if commitApproach == "before" {
				require.NoError(root.CommitToDB(context.Background()))
			}

			// Populate additional states
			concurrentStates := []Trie{}
			for i := 0; i < 5; i++ {
				newState, err := root.NewView()
				require.NoError(err)
				concurrentStates = append(concurrentStates, newState)
			}

			if commitApproach == "after" {
				require.NoError(root.CommitToDB(context.Background()))
			}

			// Process ops
			newStart := initialSet
			for i := 0; i < 100; i++ {
				if r.Intn(100) < 20 {
					// New Key
					for _, state := range concurrentStates {
						k := []byte(strconv.Itoa(newStart))
						require.NoError(state.Insert(context.Background(), k, hashing.ComputeHash256(k)))
					}
					newStart++
				} else {
					// Fetch and update old
					selectedKey := kv[r.Intn(len(kv))]
					var pastV []byte
					for _, state := range concurrentStates {
						v, err := state.GetValue(context.Background(), selectedKey)
						require.NoError(err)
						if pastV == nil {
							pastV = v
						} else {
							require.Equal(pastV, v, "lookup mismatch")
						}
						require.NoError(state.Insert(context.Background(), selectedKey, hashing.ComputeHash256(v)))
					}
				}
			}

			// Generate roots
			var pastRoot ids.ID
			for _, state := range concurrentStates {
				mroot, err := state.GetMerkleRoot(context.Background())
				require.NoError(err)
				if pastRoot == ids.Empty {
					pastRoot = mroot
				} else {
					require.Equal(pastRoot, mroot, "root mismatch")
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

	require.NoError(view1.Insert(context.Background(), []byte{1}, []byte{1}))

	// Commit the view
	require.NoError(view1.CommitToDB(context.Background()))

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
	require.NoError(view2.CommitToDB(context.Background()))

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
	require.NoError(view3.CommitToDB(context.Background()))

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
	require.NoError(view1.CommitToDB(context.Background()))

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

func Test_Trie_CommitToParentView_Concurrent(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 5000; i++ {
		dbTrie, err := getBasicDB()
		require.NoError(err)
		require.NotNil(dbTrie)

		baseView, err := dbTrie.NewView()
		require.NoError(err)

		parentView, err := baseView.NewView()
		require.NoError(err)
		require.NoError(parentView.Insert(context.Background(), []byte{0}, []byte{0}))

		childView1, err := parentView.NewView()
		require.NoError(err)
		require.NoError(childView1.Insert(context.Background(), []byte{1}, []byte{1}))

		childView2, err := childView1.NewView()
		require.NoError(err)
		require.NoError(childView2.Insert(context.Background(), []byte{2}, []byte{2}))

		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			require.NoError(parentView.CommitToParent(context.Background()))
		}()
		go func() {
			defer wg.Done()
			require.NoError(childView1.CommitToParent(context.Background()))
		}()
		go func() {
			defer wg.Done()
			require.NoError(childView2.CommitToParent(context.Background()))
		}()

		wg.Wait()

		val0, err := baseView.GetValue(context.Background(), []byte{0})
		require.NoError(err)
		require.Equal([]byte{0}, val0)

		val1, err := baseView.GetValue(context.Background(), []byte{1})
		require.NoError(err)
		require.Equal([]byte{1}, val1)

		val2, err := baseView.GetValue(context.Background(), []byte{2})
		require.NoError(err)
		require.Equal([]byte{2}, val2)
	}
}

func Test_Trie_CommitToParentDB_Concurrent(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 5000; i++ {
		dbTrie, err := getBasicDB()
		require.NoError(err)
		require.NotNil(dbTrie)

		parentView, err := dbTrie.NewView()
		require.NoError(err)
		require.NoError(parentView.Insert(context.Background(), []byte{0}, []byte{0}))

		childView1, err := parentView.NewView()
		require.NoError(err)
		require.NoError(childView1.Insert(context.Background(), []byte{1}, []byte{1}))

		childView2, err := childView1.NewView()
		require.NoError(err)
		require.NoError(childView2.Insert(context.Background(), []byte{2}, []byte{2}))

		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			require.NoError(parentView.CommitToParent(context.Background()))
		}()
		go func() {
			defer wg.Done()
			require.NoError(childView1.CommitToParent(context.Background()))
		}()
		go func() {
			defer wg.Done()
			require.NoError(childView2.CommitToParent(context.Background()))
		}()

		wg.Wait()

		val0, err := dbTrie.GetValue(context.Background(), []byte{0})
		require.NoError(err)
		require.Equal([]byte{0}, val0)

		val1, err := dbTrie.GetValue(context.Background(), []byte{1})
		require.NoError(err)
		require.Equal([]byte{1}, val1)

		val2, err := dbTrie.GetValue(context.Background(), []byte{2})
		require.NoError(err)
		require.Equal([]byte{2}, val2)
	}
}

func Test_Trie_ConcurrentReadWrite(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)
	newTrie, err := trie.NewView()
	require.NoError(err)

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(newTrie.Insert(context.Background(), []byte("key"), []byte("value")))
	}()

	require.Eventually(
		func() bool {
			value, err := newTrie.GetValue(context.Background(), []byte("key"))

			if errors.Is(err, database.ErrNotFound) {
				return false
			}

			require.NoError(err)
			require.Equal([]byte("value"), value)
			return true
		},
		time.Second,
		time.Millisecond,
	)
}

func Test_Trie_ConcurrentNewViewAndCommit(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)

	newTrie, err := trie.NewView()
	require.NoError(err)
	require.NoError(newTrie.Insert(context.Background(), []byte("key"), []byte("value0")))

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(newTrie.CommitToDB(context.Background()))
	}()

	newView, err := newTrie.NewView()
	require.NoError(err)
	require.NotNil(newView)
}

func Test_Trie_ConcurrentDeleteAndMerkleRoot(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)

	newTrie, err := trie.NewView()
	require.NoError(err)
	require.NoError(newTrie.Insert(context.Background(), []byte("key"), []byte("value0")))

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(newTrie.Remove(context.Background(), []byte("key")))
	}()

	rootID, err := newTrie.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.NotZero(rootID)
}

func Test_Trie_ConcurrentInsertProveCommit(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)

	newTrie, err := trie.NewView()
	require.NoError(err)

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(newTrie.Insert(context.Background(), []byte("key2"), []byte("value2")))
	}()

	require.Eventually(
		func() bool {
			proof, err := newTrie.GetProof(context.Background(), []byte("key2"))
			require.NoError(err)
			require.NotNil(proof)

			if proof.Value.value == nil {
				// this is an exclusion proof since the value is nil
				// return false to keep waiting for Insert to complete.
				return false
			}
			require.Equal([]byte("value2"), proof.Value.value)

			require.NoError(newTrie.CommitToDB(context.Background()))
			return true
		},
		time.Second,
		time.Millisecond,
	)
}

func Test_Trie_ConcurrentInsertAndRangeProof(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)

	newTrie, err := trie.NewView()
	require.NoError(err)
	require.NoError(newTrie.Insert(context.Background(), []byte("key1"), []byte("value1")))

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(newTrie.Insert(context.Background(), []byte("key2"), []byte("value2")))
		require.NoError(newTrie.Insert(context.Background(), []byte("key3"), []byte("value3")))
	}()

	require.Eventually(
		func() bool {
			rangeProof, err := newTrie.GetRangeProof(context.Background(), []byte("key1"), []byte("key3"), 3)
			require.NoError(err)
			require.NotNil(rangeProof)

			if len(rangeProof.KeyValues) < 3 {
				// Wait for the other goroutine to finish inserting
				return false
			}

			// Make sure we have exactly 3 KeyValues
			require.Len(rangeProof.KeyValues, 3)
			return true
		},
		time.Second,
		time.Millisecond,
	)
}
