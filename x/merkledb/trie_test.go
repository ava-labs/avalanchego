// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"math/rand"
	"strconv"
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/utils/maybe"

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

		return closestNode.value.Value(), nil
	}
	if asDatabases, ok := t.(*merkleDB); ok {
		view, err := asDatabases.NewView(nil)
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

		return closestNode.value.Value(), nil
	}
	return nil, nil
}

func Test_GetValue_Safety(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	trieView, err := db.NewView([]database.BatchOp{{Key: []byte{0}, Value: []byte{0}}})
	require.NoError(err)

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

	trieView, err := db.NewView([]database.BatchOp{{Key: []byte{0}, Value: []byte{0}}})
	require.NoError(err)

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

	trieIntf, err := db.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, trieIntf)
	trie := trieIntf.(*trieView)

	path, err := trie.getPathTo(newPath(nil))
	require.NoError(err)

	// Just the root
	require.Len(path, 1)
	require.Equal(trie.root, path[0])

	// Insert a key
	key1 := []byte{0}
	trieIntf, err = trie.NewView([]database.BatchOp{{Key: key1, Value: []byte("value")}})
	require.NoError(err)
	require.IsType(&trieView{}, trieIntf)
	trie = trieIntf.(*trieView)
	require.NoError(trie.calculateNodeIDs(context.Background()))

	path, err = trie.getPathTo(newPath(key1))
	require.NoError(err)

	// Root and 1 value
	require.Len(path, 2)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key1), path[1].key)

	// Insert another key which is a child of the first
	key2 := []byte{0, 1}
	trieIntf, err = trie.NewView([]database.BatchOp{{Key: key2, Value: []byte("value")}})
	require.NoError(err)
	require.IsType(&trieView{}, trieIntf)
	trie = trieIntf.(*trieView)
	require.NoError(trie.calculateNodeIDs(context.Background()))

	path, err = trie.getPathTo(newPath(key2))
	require.NoError(err)
	require.Len(path, 3)
	require.Equal(trie.root, path[0])
	require.Equal(newPath(key1), path[1].key)
	require.Equal(newPath(key2), path[2].key)

	// Insert a key which shares no prefix with the others
	key3 := []byte{255}
	trieIntf, err = trie.NewView([]database.BatchOp{{Key: key3, Value: []byte("value")}})
	require.NoError(err)
	require.IsType(&trieView{}, trieIntf)
	trie = trieIntf.(*trieView)
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

	committedTrie, err := dbTrie.NewView([]database.BatchOp{{Key: []byte{0}, Value: []byte{0}}})
	require.NoError(err)

	require.NoError(committedTrie.CommitToDB(context.Background()))

	newView, err := committedTrie.NewView([]database.BatchOp{{Key: []byte{1}, Value: []byte{1}}})
	require.NoError(err)
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

	trie2, err := dbTrie.NewView([]database.BatchOp{{Key: []byte("key"), Value: []byte("value")}})
	require.NoError(err)

	trie3, err := trie2.NewView([]database.BatchOp{{Key: []byte("key1"), Value: []byte("value1")}})
	require.NoError(err)

	trie4, err := trie3.NewView([]database.BatchOp{{Key: []byte("key2"), Value: []byte("value2")}})
	require.NoError(err)

	_, err = trie4.NewView([]database.BatchOp{{Key: []byte("key3"), Value: []byte("value3")}})
	require.NoError(err)

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

	trieIntf, err := dbTrie.NewView(nil)
	require.NoError(err)
	trie := trieIntf.(*trieView)

	// value hasn't been inserted so shouldn't exist
	value, err := trie.GetValue(context.Background(), []byte("key"))
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(value)

	trieIntf, err = trie.NewView([]database.BatchOp{{Key: []byte("key"), Value: []byte("value")}})
	require.NoError(err)
	trie = trieIntf.(*trieView)

	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value"), value)

	require.NoError(trie.CommitToDB(context.Background()))

	p := newPath([]byte("key"))
	rawBytes, err := dbTrie.nodeDB.Get(p.Bytes())
	require.NoError(err)

	node, err := parseNode(p, rawBytes)
	require.NoError(err)
	require.Equal([]byte("value"), node.value.Value())
}

func Test_Trie_InsertAndRetrieve(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	// value hasn't been inserted so shouldn't exist
	value, err := dbTrie.Get([]byte("key"))
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(value)

	require.NoError(dbTrie.Put([]byte("key"), []byte("value")))

	value, err = getNodeValue(dbTrie, "key")
	require.NoError(err)
	require.Equal([]byte("value"), value)
}

func Test_Trie_Overwrite(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie, err := dbTrie.NewView([]database.BatchOp{
		{Key: []byte("key"), Value: []byte("value0")},
		{Key: []byte("key"), Value: []byte("value1")},
	})
	require.NoError(err)
	value, err := getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value1"), value)

	trie, err = dbTrie.NewView([]database.BatchOp{
		{Key: []byte("key"), Value: []byte("value2")},
	})
	require.NoError(err)
	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value2"), value)
}

func Test_Trie_Delete(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	trie, err := dbTrie.NewView([]database.BatchOp{{Key: []byte("key"), Value: []byte("value0")}})
	require.NoError(err)

	value, err := getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value0"), value)

	trie, err = dbTrie.NewView([]database.BatchOp{{Key: []byte("key"), Delete: true}})
	require.NoError(err)

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
	trieIntf, err := dbTrie.NewView([]database.BatchOp{{Key: []byte("key"), Value: []byte("value0")}})
	require.NoError(err)
	trie := trieIntf.(*trieView)

	value, err := getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value0"), value)

	trieIntf, err = trie.NewView([]database.BatchOp{{Key: []byte("key1"), Value: []byte("value1")}})
	require.NoError(err)
	trie = trieIntf.(*trieView)

	value, err = getNodeValue(trie, "key")
	require.NoError(err)
	require.Equal([]byte("value0"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(err)
	require.Equal([]byte("value1"), value)

	trieIntf, err = trie.NewView([]database.BatchOp{{Key: []byte("key12"), Value: []byte("value12")}})
	require.NoError(err)
	trie = trieIntf.(*trieView)

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
	trieIntf, err := dbTrie.NewView([]database.BatchOp{{Key: []byte("key12"), Value: []byte("value12")}})
	require.NoError(err)
	trie := trieIntf.(*trieView)

	value, err := getNodeValue(trie, "key12")
	require.NoError(err)
	require.Equal([]byte("value12"), value)

	trieIntf, err = trie.NewView([]database.BatchOp{{Key: []byte("key1"), Value: []byte("value1")}})
	require.NoError(err)
	trie = trieIntf.(*trieView)

	value, err = getNodeValue(trie, "key12")
	require.NoError(err)
	require.Equal([]byte("value12"), value)

	value, err = getNodeValue(trie, "key1")
	require.NoError(err)
	require.Equal([]byte("value1"), value)

	trieIntf, err = trie.NewView([]database.BatchOp{{Key: []byte("key"), Value: []byte("value")}})
	require.NoError(err)
	trie = trieIntf.(*trieView)

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

	// force a new node to generate with common prefix "key1" and have these two nodes as children
	trie, err := dbTrie.NewView([]database.BatchOp{
		{Key: []byte("key12"), Value: []byte("value12")},
		{Key: []byte("key134"), Value: []byte("value134")},
	})
	require.NoError(err)

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
	trieIntf, err := dbTrie.NewView(nil)
	require.NoError(err)
	trie := trieIntf.(*trieView)

	// force a new node to generate with common prefix "key1" and have these two nodes as children
	_, err = trie.insert(newPath([]byte("key12")), maybe.Some([]byte("value12")))
	require.NoError(err)
	require.NoError(trie.calculateNodeIDs(context.Background()))
	oldCount := dbTrie.metrics.(*mockMetrics).hashCount
	_, err = trie.insert(newPath([]byte("key134")), maybe.Some([]byte("value134")))
	require.NoError(err)
	// only hashes the new branch node, the new child node, and root
	// shouldn't hash the existing node
	require.NoError(trie.calculateNodeIDs(context.Background()))
	require.Equal(oldCount+3, dbTrie.metrics.(*mockMetrics).hashCount)
}

func Test_Trie_HashCountOnDelete(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)

	trie, err := dbTrie.NewView([]database.BatchOp{
		{Key: []byte("k"), Value: []byte("value0")},
		{Key: []byte("ke"), Value: []byte("value1")},
		{Key: []byte("key"), Value: []byte("value2")},
		{Key: []byte("key1"), Value: []byte("value3")},
		{Key: []byte("key2"), Value: []byte("value4")},
	})
	require.NoError(err)
	require.NotNil(trie)

	require.NoError(trie.CommitToDB(context.Background()))
	oldCount := dbTrie.metrics.(*mockMetrics).hashCount

	// delete the middle values
	view, err := trie.NewView([]database.BatchOp{
		{Key: []byte("k"), Delete: true},
		{Key: []byte("ke"), Delete: true},
		{Key: []byte("key"), Delete: true},
	})
	require.NoError(err)
	require.NoError(view.CommitToDB(context.Background()))

	// the root is the only updated node so only one new hash
	require.Equal(oldCount+1, dbTrie.metrics.(*mockMetrics).hashCount)
}

func Test_Trie_NoExistingResidual(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	trie, err := dbTrie.NewView([]database.BatchOp{
		{Key: []byte("k"), Value: []byte("1")},
		{Key: []byte("ke"), Value: []byte("2")},
		{Key: []byte("key1"), Value: []byte("3")},
		{Key: []byte("key123"), Value: []byte("4")},
	})
	require.NoError(err)
	require.NotNil(trie)

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

	view1Intf, err := db.NewView([]database.BatchOp{{Key: []byte{1}, Value: []byte{1}}})
	require.NoError(err)
	require.IsType(&trieView{}, view1Intf)
	view1 := view1Intf.(*trieView)

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
	require.NoError(view1.commitChanges(context.Background(), nil))
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
	view2Intf, err := view1.NewView([]database.BatchOp{
		{Key: []byte{2}, Value: []byte{2}},
		{Key: []byte{1}, Delete: true},
	})
	require.NoError(err)
	require.IsType(&trieView{}, view2Intf)
	view2 := view2Intf.(*trieView)

	view2Root, err := view2.getMerkleRoot(context.Background())
	require.NoError(err)

	// view1 has 1 --> 1
	// view2 has 2 --> 2

	view3Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view3Intf)
	view3 := view3Intf.(*trieView)

	view4Intf, err := view2.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view4Intf)
	view4 := view4Intf.(*trieView)

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

	trie, err := dbTrie.NewView([]database.BatchOp{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key12"), Value: []byte("value12")},
		{Key: []byte("key134"), Value: []byte("value134")},
		{Key: []byte("key1"), Delete: true},
	})
	require.NoError(err)
	require.NotNil(trie)

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
	newTrie, err := trie.NewView([]database.BatchOp{
		{Key: []byte("k"), Value: []byte("value0")},
		{Key: []byte("ke"), Value: []byte("value1")},
		{Key: []byte("key"), Value: []byte("value2")},
		{Key: []byte("key1"), Value: []byte("value3")},
	})
	require.NoError(err)

	require.NoError(newTrie.(*trieView).calculateNodeIDs(context.Background()))
	root, err := newTrie.getEditableNode(EmptyPath)
	require.NoError(err)
	require.Len(root.children, 1)

	newTrie, err = newTrie.NewView([]database.BatchOp{
		{Key: []byte("k"), Delete: true},
		{Key: []byte("ke"), Delete: true},
		{Key: []byte("key"), Delete: true},
		{Key: []byte("key1"), Delete: true},
	})
	require.NoError(err)
	require.NoError(newTrie.(*trieView).calculateNodeIDs(context.Background()))
	root, err = newTrie.getEditableNode(EmptyPath)
	require.NoError(err)
	// since all values have been deleted, the nodes should have been cleaned up
	require.Empty(root.children)
}

func Test_Trie_Invalidate_Siblings_On_Commit(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	baseView, err := dbTrie.NewView(nil)
	require.NoError(err)

	viewToCommit, err := baseView.NewView([]database.BatchOp{{Key: []byte{0}, Value: []byte{0}}})
	require.NoError(err)

	sibling1, err := baseView.NewView(nil)
	require.NoError(err)
	sibling2, err := baseView.NewView(nil)
	require.NoError(err)

	require.False(sibling1.(*trieView).isInvalid())
	require.False(sibling2.(*trieView).isInvalid())

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

	trie, err := dbTrie.NewView([]database.BatchOp{
		{Key: []byte("k"), Value: []byte("value0")},
		{Key: []byte("ke"), Value: []byte("value1")},
		{Key: []byte("key"), Value: []byte("value2")},
		{Key: []byte("key1"), Value: []byte("value3")},
		{Key: []byte("key2"), Value: []byte("value4")},
	})
	require.NoError(err)

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
	trie, err = trie.NewView([]database.BatchOp{
		{Key: []byte("k"), Delete: true},
		{Key: []byte("ke"), Delete: true},
		{Key: []byte("key"), Delete: true},
	})
	require.NoError(err)
	require.NoError(trie.(*trieView).calculateNodeIDs(context.Background()))

	root, err = trie.getEditableNode(EmptyPath)
	require.NoError(err)
	require.Len(root.children, 1)

	firstNode, err = trie.getEditableNode(root.getSingleChildPath())
	require.NoError(err)
	require.Len(firstNode.children, 2)
}

func Test_Trie_MultipleStates(t *testing.T) {
	randCount := int64(0)
	for _, commitApproach := range []string{"never", "before", "after"} {
		t.Run(commitApproach, func(t *testing.T) {
			require := require.New(t)

			r := rand.New(rand.NewSource(randCount)) // #nosec G404
			randCount++
			rdb := memdb.New()
			defer rdb.Close()
			db, err := New(
				context.Background(),
				rdb,
				newDefaultConfig(),
			)
			require.NoError(err)
			defer db.Close()

			initialSet := 1000
			// Populate initial set of keys
			ops := make([]database.BatchOp, 0, initialSet)
			require.NoError(err)
			kv := [][]byte{}
			for i := 0; i < initialSet; i++ {
				k := []byte(strconv.Itoa(i))
				kv = append(kv, k)
				ops = append(ops, database.BatchOp{Key: k, Value: hashing.ComputeHash256(k)})
			}
			root, err := db.NewView(ops)
			require.NoError(err)

			// Get initial root
			_, err = root.GetMerkleRoot(context.Background())
			require.NoError(err)

			if commitApproach == "before" {
				require.NoError(root.CommitToDB(context.Background()))
			}

			// Populate additional states
			concurrentStates := []Trie{}
			for i := 0; i < 5; i++ {
				newState, err := root.NewView(nil)
				require.NoError(err)
				concurrentStates = append(concurrentStates, newState)
			}

			if commitApproach == "after" {
				require.NoError(root.CommitToDB(context.Background()))
			}

			// Process ops
			newStart := initialSet
			concurrentOps := make([][]database.BatchOp, len(concurrentStates))
			for i := 0; i < 100; i++ {
				if r.Intn(100) < 20 {
					// New Key
					for index := range concurrentStates {
						k := []byte(strconv.Itoa(newStart))
						concurrentOps[index] = append(concurrentOps[index], database.BatchOp{Key: k, Value: hashing.ComputeHash256(k)})
					}
					newStart++
				} else {
					// Fetch and update old
					selectedKey := kv[r.Intn(len(kv))]
					var pastV []byte
					for index, state := range concurrentStates {
						v, err := state.GetValue(context.Background(), selectedKey)
						require.NoError(err)
						if pastV == nil {
							pastV = v
						} else {
							require.Equal(pastV, v)
						}
						concurrentOps[index] = append(concurrentOps[index], database.BatchOp{Key: selectedKey, Value: hashing.ComputeHash256(v)})
					}
				}
			}
			for index, state := range concurrentStates {
				concurrentStates[index], err = state.NewView(concurrentOps[index])
				require.NoError(err)
			}

			// Generate roots
			var pastRoot ids.ID
			for _, state := range concurrentStates {
				mroot, err := state.GetMerkleRoot(context.Background())
				require.NoError(err)
				if pastRoot == ids.Empty {
					pastRoot = mroot
				} else {
					require.Equal(pastRoot, mroot)
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
	view1Intf, err := db.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view1Intf)
	view1 := view1Intf.(*trieView)

	// view1
	//   |
	//  db

	require.Len(db.childViews, 1)
	require.Contains(db.childViews, view1)
	require.Equal(db, view1.parentTrie)

	_, err = view1.insert(newPath([]byte{1}), maybe.Some([]byte{1}))
	require.NoError(err)

	// Commit the view
	require.NoError(view1.CommitToDB(context.Background()))

	// view1 (committed)
	//   |
	//  db

	require.Len(db.childViews, 1)
	require.Contains(db.childViews, view1)
	require.Equal(db, view1.parentTrie)

	// Create a new view on the committed view
	view2Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view2Intf)
	view2 := view2Intf.(*trieView)

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
	view3Intf, err := view2.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view3Intf)
	view3 := view3Intf.(*trieView)

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
	view1Intf, err := db.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view1Intf)
	view1 := view1Intf.(*trieView)

	// Create a view atop view1
	view2Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view2Intf)
	view2 := view2Intf.(*trieView)

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
	view3Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view3Intf)
	view3 := view3Intf.(*trieView)

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
	_, err = invalidView.NewView(nil)
	require.ErrorIs(err, ErrInvalid)
}

func TestTrieViewInvalidate(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create a view
	view1Intf, err := db.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view1Intf)
	view1 := view1Intf.(*trieView)

	// Create 2 views atop view1
	view2Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view2Intf)
	view2 := view2Intf.(*trieView)

	view3Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view3Intf)
	view3 := view3Intf.(*trieView)

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
	view1Intf, err := db.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view1Intf)
	view1 := view1Intf.(*trieView)

	// Create a view atop view1
	view2Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view2Intf)
	view2 := view2Intf.(*trieView)

	// Create a view atop view2
	view3Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view3Intf)
	view3 := view3Intf.(*trieView)

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
	view1Intf, err := db.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view1Intf)
	view1 := view1Intf.(*trieView)

	// Create 2 views atop view1
	view2Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view2Intf)
	view2 := view2Intf.(*trieView)

	view3Intf, err := view1.NewView(nil)
	require.NoError(err)
	require.IsType(&trieView{}, view3Intf)
	view3 := view3Intf.(*trieView)

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

func Test_Trie_ConcurrentNewViewAndCommit(t *testing.T) {
	require := require.New(t)

	trie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(trie)

	newTrie, err := trie.NewView([]database.BatchOp{
		{Key: []byte("key"), Value: []byte("value0")},
	})
	require.NoError(err)

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(newTrie.CommitToDB(context.Background()))
	}()

	newView, err := newTrie.NewView(nil)
	require.NoError(err)
	require.NotNil(newView)
}
