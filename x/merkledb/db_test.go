// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
)

// newDB returns a new merkle database with the underlying type so that tests can access unexported fields
func newDB(ctx context.Context, db database.Database, config Config) (*merkleDB, error) {
	db, err := New(ctx, db, config)
	if err != nil {
		return nil, err
	}
	return db.(*merkleDB), nil
}

func Test_MerkleDB_Get_Safety(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	keyBytes := []byte{0}
	require.NoError(db.Put(keyBytes, []byte{0, 1, 2}))

	val, err := db.Get(keyBytes)
	require.NoError(err)

	n, err := db.getNode(ToKey(keyBytes), true)
	require.NoError(err)

	// node's value shouldn't be affected by the edit
	originalVal := slices.Clone(val)
	val[0]++
	require.Equal(originalVal, n.value.Value())
}

func Test_MerkleDB_GetValues_Safety(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	keyBytes := []byte{0}
	value := []byte{0, 1, 2}
	require.NoError(db.Put(keyBytes, value))

	gotValues, errs := db.GetValues(context.Background(), [][]byte{keyBytes})
	require.Len(errs, 1)
	require.NoError(errs[0])
	require.Equal(value, gotValues[0])
	gotValues[0][0]++

	// editing the value array shouldn't affect the db
	gotValues, errs = db.GetValues(context.Background(), [][]byte{keyBytes})
	require.Len(errs, 1)
	require.NoError(errs[0])
	require.Equal(value, gotValues[0])
}

func Test_MerkleDB_DB_Interface(t *testing.T) {
	for _, bf := range validBranchFactors {
		for name, test := range dbtest.Tests {
			t.Run(fmt.Sprintf("%s_%d", name, bf), func(t *testing.T) {
				db, err := getBasicDBWithBranchFactor(bf)
				require.NoError(t, err)
				test(t, db)
			})
		}
	}
}

func Benchmark_MerkleDB_DBInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bf := range validBranchFactors {
			for name, bench := range dbtest.Benchmarks {
				b.Run(fmt.Sprintf("merkledb_%d_%d_pairs_%d_keys_%d_values_%s", bf, size[0], size[1], size[2], name), func(b *testing.B) {
					db, err := getBasicDBWithBranchFactor(bf)
					require.NoError(b, err)
					bench(b, db, keys, values)
				})
			}
		}
	}
}

func Test_MerkleDB_DB_Load_Root_From_DB(t *testing.T) {
	require := require.New(t)
	baseDB := memdb.New()
	defer baseDB.Close()

	db, err := New(
		context.Background(),
		baseDB,
		NewConfig(),
	)
	require.NoError(err)

	// Populate initial set of key-value pairs
	keyCount := 100
	ops := make([]database.BatchOp, 0, keyCount)
	require.NoError(err)
	for i := 0; i < keyCount; i++ {
		k := []byte(strconv.Itoa(i))
		ops = append(ops, database.BatchOp{
			Key:   k,
			Value: hashing.ComputeHash256(k),
		})
	}
	view, err := db.NewView(context.Background(), ViewChanges{BatchOps: ops})
	require.NoError(err)
	require.NoError(view.CommitToDB(context.Background()))

	root, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.NoError(db.Close())

	// reloading the db should set the root back to the one that was saved to [baseDB]
	db, err = New(
		context.Background(),
		baseDB,
		NewConfig(),
	)
	require.NoError(err)

	reloadedRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(root, reloadedRoot)
}

func Test_MerkleDB_DB_Rebuild(t *testing.T) {
	require := require.New(t)

	initialSize := 5_000

	config := NewConfig()
	config.ValueNodeCacheSize = uint(initialSize)
	config.IntermediateNodeCacheSize = uint(initialSize)

	db, err := newDB(
		context.Background(),
		memdb.New(),
		config,
	)
	require.NoError(err)

	// Populate initial set of keys
	ops := make([]database.BatchOp, 0, initialSize)
	require.NoError(err)
	for i := 0; i < initialSize; i++ {
		k := []byte(strconv.Itoa(i))
		ops = append(ops, database.BatchOp{
			Key:   k,
			Value: hashing.ComputeHash256(k),
		})
	}
	view, err := db.NewView(context.Background(), ViewChanges{BatchOps: ops})
	require.NoError(err)
	require.NoError(view.CommitToDB(context.Background()))

	// Get root
	root, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	// Rebuild
	require.NoError(db.rebuild(context.Background(), initialSize))

	// Assert root is the same after rebuild
	rebuiltRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(root, rebuiltRoot)

	// add variation where root has a value
	require.NoError(db.Put(nil, []byte{}))

	root, err = db.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.NoError(db.rebuild(context.Background(), initialSize))

	rebuiltRoot, err = db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(root, rebuiltRoot)
}

func Test_MerkleDB_Failed_Batch_Commit(t *testing.T) {
	require := require.New(t)

	memDB := memdb.New()
	db, err := New(
		context.Background(),
		memDB,
		NewConfig(),
	)
	require.NoError(err)

	_ = memDB.Close()

	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("1")))
	require.NoError(batch.Put([]byte("key2"), []byte("2")))
	require.NoError(batch.Put([]byte("key3"), []byte("3")))
	err = batch.Write()
	require.ErrorIs(err, database.ErrClosed)
}

func Test_MerkleDB_Value_Cache(t *testing.T) {
	require := require.New(t)

	memDB := memdb.New()
	db, err := New(
		context.Background(),
		memDB,
		NewConfig(),
	)
	require.NoError(err)

	batch := db.NewBatch()
	key1, key2 := []byte("key1"), []byte("key2")
	require.NoError(batch.Put(key1, []byte("1")))
	require.NoError(batch.Put([]byte("key2"), []byte("2")))
	require.NoError(batch.Write())

	batch = db.NewBatch()
	// force key2 to be inserted into the cache as not found
	require.NoError(batch.Delete(key2))
	require.NoError(batch.Write())

	require.NoError(memDB.Close())

	// still works because key1 is read from cache
	value, err := db.Get(key1)
	require.NoError(err)
	require.Equal([]byte("1"), value)

	// still returns missing instead of closed because key2 is read from cache
	_, err = db.Get(key2)
	require.ErrorIs(err, database.ErrNotFound)
}

func Test_MerkleDB_Invalidate_Siblings_On_Commit(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)

	viewToCommit, err := dbTrie.NewView(
		context.Background(),
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte{0}, Value: []byte{0}},
			},
		},
	)
	require.NoError(err)

	// Create siblings of viewToCommit
	sibling1, err := dbTrie.NewView(context.Background(), ViewChanges{})
	require.NoError(err)
	sibling2, err := dbTrie.NewView(context.Background(), ViewChanges{})
	require.NoError(err)

	require.False(sibling1.(*view).isInvalid())
	require.False(sibling2.(*view).isInvalid())

	// Committing viewToCommit should invalidate siblings
	require.NoError(viewToCommit.CommitToDB(context.Background()))

	require.True(sibling1.(*view).isInvalid())
	require.True(sibling2.(*view).isInvalid())
	require.False(viewToCommit.(*view).isInvalid())
}

func Test_MerkleDB_CommitRangeProof_DeletesValuesInRange(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// value that shouldn't be deleted
	require.NoError(db.Put([]byte("key6"), []byte("3")))

	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	// Get an empty proof
	proof, err := db.GetRangeProof(
		context.Background(),
		maybe.Nothing[[]byte](),
		maybe.Some([]byte("key3")),
		10,
	)
	require.NoError(err)

	// confirm there are no key.values in the proof
	require.Empty(proof.KeyChanges)

	// add values to be deleted by proof commit
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("1")))
	require.NoError(batch.Put([]byte("key2"), []byte("2")))
	require.NoError(batch.Put([]byte("key3"), []byte("3")))
	require.NoError(batch.Write())

	// despite having no key/values in it, committing this proof should delete key1-key3.
	_, err = db.CommitRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Some([]byte("key3")), proof)
	require.NoError(err)

	afterCommitRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.Equal(startRoot, afterCommitRoot)
}

func Test_MerkleDB_CommitRangeProof_EmptyTrie(t *testing.T) {
	require := require.New(t)

	// Populate [db1] with 3 key-value pairs.
	db1, err := getBasicDB()
	require.NoError(err)
	batch := db1.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("1")))
	require.NoError(batch.Put([]byte("key2"), []byte("2")))
	require.NoError(batch.Put([]byte("key3"), []byte("3")))
	require.NoError(batch.Write())

	// Get a proof for the range [key1, key3].
	proof, err := db1.GetRangeProof(
		context.Background(),
		maybe.Some([]byte("key1")),
		maybe.Some([]byte("key3")),
		10,
	)
	require.NoError(err)

	// Commit the proof to a fresh database.
	db2, err := getBasicDB()
	require.NoError(err)

	_, err = db2.CommitRangeProof(context.Background(), maybe.Some([]byte("key1")), maybe.Some([]byte("key3")), proof)
	require.NoError(err)

	// [db2] should have the same key-value pairs as [db1].
	db2Root, err := db2.GetMerkleRoot(context.Background())
	require.NoError(err)

	db1Root, err := db1.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.Equal(db1Root, db2Root)
}

func Test_MerkleDB_CommitRangeProof_TrieWithInitialValues(t *testing.T) {
	require := require.New(t)

	// Populate [db1] with 3 key-value pairs.
	db1, err := getBasicDB()
	require.NoError(err)
	batch := db1.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("1")))
	require.NoError(batch.Put([]byte("key2"), []byte("2")))
	require.NoError(batch.Put([]byte("key3"), []byte("3")))
	require.NoError(batch.Write())

	// Get a proof for the range [key1, key3].
	proof, err := db1.GetRangeProof(
		context.Background(),
		maybe.Some([]byte("key1")),
		maybe.Some([]byte("key3")),
		10,
	)
	require.NoError(err)

	// Populate [db2] with key-value pairs where some of the keys
	// have different values than in [db1].
	db2, err := getBasicDB()
	require.NoError(err)
	batch = db2.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("3")))
	require.NoError(batch.Put([]byte("key2"), []byte("4")))
	require.NoError(batch.Put([]byte("key3"), []byte("5")))
	require.NoError(batch.Put([]byte("key25"), []byte("5")))
	require.NoError(batch.Write())

	// Commit the proof from [db1] to [db2]
	_, err = db2.CommitRangeProof(
		context.Background(),
		maybe.Some([]byte("key1")),
		maybe.Some([]byte("key3")),
		proof,
	)
	require.NoError(err)

	// [db2] should have the same key-value pairs as [db1].
	// Note that "key25" was in the range covered by the proof,
	// so it's deleted from [db2].
	db2Root, err := db2.GetMerkleRoot(context.Background())
	require.NoError(err)

	db1Root, err := db1.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.Equal(db1Root, db2Root)
}

func Test_MerkleDB_GetValues(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	writeBasicBatch(t, db)
	keys := [][]byte{{0}, {1}, {2}, {10}}
	values, errors := db.GetValues(context.Background(), keys)
	require.Len(values, len(keys))
	require.Len(errors, len(keys))

	// first 3 have values
	// last was not found
	require.NoError(errors[0])
	require.NoError(errors[1])
	require.NoError(errors[2])
	require.ErrorIs(errors[3], database.ErrNotFound)

	require.Equal([]byte{0}, values[0])
	require.Equal([]byte{1}, values[1])
	require.Equal([]byte{2}, values[2])
	require.Nil(values[3])
}

func Test_MerkleDB_InsertNil(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	batch := db.NewBatch()
	key := []byte("key0")
	require.NoError(batch.Put(key, nil))
	require.NoError(batch.Write())

	value, err := db.Get(key)
	require.NoError(err)
	require.Empty(value)

	value, err = getNodeValue(db, string(key))
	require.NoError(err)
	require.Empty(value)
}

func Test_MerkleDB_HealthCheck(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	val, err := db.HealthCheck(context.Background())
	require.NoError(err)
	require.Nil(val)
}

// Test that untracked views aren't tracked in [db.childViews].
func TestDatabaseNewUntrackedView(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create a new untracked view.
	view, err := newView(
		db,
		db,
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte{1}, Value: []byte{1}},
			},
		},
	)
	require.NoError(err)
	require.Empty(db.childViews)

	// Commit the view
	require.NoError(view.CommitToDB(context.Background()))

	// The untracked view should not be tracked by the parent database.
	require.Empty(db.childViews)
}

// Test that tracked views are persisted to [db.childViews].
func TestDatabaseNewViewFromBatchOpsTracked(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create a new tracked view.
	view, err := db.NewView(
		context.Background(),
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte{1}, Value: []byte{1}},
			},
		},
	)
	require.NoError(err)
	require.Len(db.childViews, 1)

	// Commit the view
	require.NoError(view.CommitToDB(context.Background()))

	// The view should be tracked by the parent database.
	require.Contains(db.childViews, view)
	require.Len(db.childViews, 1)
}

func TestDatabaseCommitChanges(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	dbRoot := db.getMerkleRoot()

	// Committing a nil view should be a no-op.
	require.NoError(db.CommitToDB(context.Background()))
	require.Equal(dbRoot, db.getMerkleRoot()) // Root didn't change

	// Committing an invalid view should fail.
	invalidView, err := db.NewView(context.Background(), ViewChanges{})
	require.NoError(err)
	invalidView.(*view).invalidate()
	err = invalidView.CommitToDB(context.Background())
	require.ErrorIs(err, ErrInvalid)

	// Add key-value pairs to the database
	key1, key2, key3 := []byte{1}, []byte{2}, []byte{3}
	value1, value2, value3 := []byte{1}, []byte{2}, []byte{3}
	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))

	// Make a view and insert/delete a key-value pair.
	view1Intf, err := db.NewView(
		context.Background(),
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: key3, Value: value3}, // New k-v pair
				{Key: key1, Delete: true},  // Delete k-v pair
			},
		},
	)
	require.NoError(err)
	require.IsType(&view{}, view1Intf)
	view1 := view1Intf.(*view)
	view1Root, err := view1.GetMerkleRoot(context.Background())
	require.NoError(err)

	// Make a second view
	view2Intf, err := db.NewView(context.Background(), ViewChanges{})
	require.NoError(err)
	require.IsType(&view{}, view2Intf)
	view2 := view2Intf.(*view)

	// Make a view atop a view
	view3Intf, err := view1.NewView(context.Background(), ViewChanges{})
	require.NoError(err)
	require.IsType(&view{}, view3Intf)
	view3 := view3Intf.(*view)

	// view3
	//  |
	// view1   view2
	//     \  /
	//      db

	// Commit view1
	require.NoError(view1.commitToDB(context.Background()))

	// Make sure the key-value pairs are correct.
	_, err = db.Get(key1)
	require.ErrorIs(err, database.ErrNotFound)
	gotValue, err := db.Get(key2)
	require.NoError(err)
	require.Equal(value2, gotValue)
	gotValue, err = db.Get(key3)
	require.NoError(err)
	require.Equal(value3, gotValue)

	// Make sure the root is right
	require.Equal(view1Root, db.getMerkleRoot())

	// Make sure view2 is invalid and view1 and view3 is valid.
	require.False(view1.invalidated)
	require.True(view2.invalidated)
	require.False(view3.invalidated)

	// Make sure view2 isn't tracked by the database.
	require.NotContains(db.childViews, view2)

	// Make sure view1 and view3 is tracked by the database.
	require.Contains(db.childViews, view1)
	require.Contains(db.childViews, view3)

	// Make sure view3 is now a child of db.
	require.Equal(db, view3.parentTrie)
}

func TestDatabaseInvalidateChildrenExcept(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// Create children
	view1Intf, err := db.NewView(context.Background(), ViewChanges{})
	require.NoError(err)
	require.IsType(&view{}, view1Intf)
	view1 := view1Intf.(*view)

	view2Intf, err := db.NewView(context.Background(), ViewChanges{})
	require.NoError(err)
	require.IsType(&view{}, view2Intf)
	view2 := view2Intf.(*view)

	view3Intf, err := db.NewView(context.Background(), ViewChanges{})
	require.NoError(err)
	require.IsType(&view{}, view3Intf)
	view3 := view3Intf.(*view)

	db.invalidateChildrenExcept(view1)

	// Make sure view1 is valid and view2 and view3 are invalid.
	require.False(view1.invalidated)
	require.True(view2.invalidated)
	require.True(view3.invalidated)
	require.Contains(db.childViews, view1)
	require.Len(db.childViews, 1)

	db.invalidateChildrenExcept(nil)

	// Make sure all views are invalid.
	require.True(view1.invalidated)
	require.True(view2.invalidated)
	require.True(view3.invalidated)
	require.Empty(db.childViews)

	// Calling with an untracked view doesn't add the untracked view
	db.invalidateChildrenExcept(view1)
	require.Empty(db.childViews)
}

func Test_MerkleDB_Random_Insert_Ordering(t *testing.T) {
	require := require.New(t)

	var (
		numRuns             = 3
		numShuffles         = 3
		numKeyValues        = 1_000
		prefixProbability   = .1
		nilValueProbability = 0.05
		keys                [][]byte
		keysSet             set.Set[string]
	)

	// Returns a random key.
	// With probability approximately [prefixProbability], the returned key
	// will be a prefix of a previously returned key.
	genKey := func(r *rand.Rand) []byte {
		for {
			var key []byte
			shouldPrefix := r.Float64() < prefixProbability
			if len(keys) > 2 && shouldPrefix {
				// Return a key that is a prefix of a previously returned key.
				prefix := keys[r.Intn(len(keys))]
				key = make([]byte, r.Intn(50)+len(prefix))
				copy(key, prefix)
				_, _ = r.Read(key[len(prefix):])
			} else {
				key = make([]byte, r.Intn(50))
				_, _ = r.Read(key)
			}

			// If the key has already been returned, try again.
			// This test would flake if we allowed duplicate keys
			// because then the order of insertion matters.
			if !keysSet.Contains(string(key)) {
				keysSet.Add(string(key))
				keys = append(keys, key)
				return key
			}
		}
	}

	for i := 0; i < numRuns; i++ {
		now := time.Now().UnixNano()
		t.Logf("seed for iter %d: %d", i, now)
		r := rand.New(rand.NewSource(now)) // #nosec G404

		// Insert key-value pairs into a database.
		ops := make([]database.BatchOp, 0, numKeyValues)
		keys = [][]byte{}
		for x := 0; x < numKeyValues; x++ {
			key := genKey(r)
			value := make([]byte, r.Intn(51))
			if r.Float64() < nilValueProbability {
				value = nil
			} else {
				_, _ = r.Read(value)
			}
			ops = append(ops, database.BatchOp{
				Key:   key,
				Value: value,
			})
		}

		db, err := getBasicDB()
		require.NoError(err)

		view1, err := db.NewView(context.Background(), ViewChanges{BatchOps: ops})
		require.NoError(err)

		// Get the root of the trie after applying [ops].
		view1Root, err := view1.GetMerkleRoot(context.Background())
		require.NoError(err)

		// Assert that the same operations applied in a different order
		// result in the same root. Note this is only true because
		// all keys inserted are unique.
		for shuffleIndex := 0; shuffleIndex < numShuffles; shuffleIndex++ {
			r.Shuffle(numKeyValues, func(i, j int) {
				ops[i], ops[j] = ops[j], ops[i]
			})

			view2, err := db.NewView(context.Background(), ViewChanges{BatchOps: ops})
			require.NoError(err)

			view2Root, err := view2.GetMerkleRoot(context.Background())
			require.NoError(err)

			require.Equal(view1Root, view2Root)
		}
	}
}

func TestMerkleDBClear(t *testing.T) {
	require := require.New(t)

	// Make a database and insert some key-value pairs.
	db, err := getBasicDB()
	require.NoError(err)

	emptyRootID := db.getMerkleRoot()

	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	insertRandomKeyValues(
		require,
		r,
		[]database.Database{db},
		1_000,
		0.25,
	)

	// Clear the database.
	require.NoError(db.Clear())

	// Assert that the database is empty.
	iter := db.NewIterator()
	defer iter.Release()
	require.False(iter.Next())
	require.Equal(ids.Empty, db.getMerkleRoot())
	require.True(db.root.IsNothing())

	// Assert caches are empty.
	require.Zero(db.valueNodeDB.nodeCache.Len())
	require.Zero(db.intermediateNodeDB.writeBuffer.currentSize)

	// Assert history has only the clearing change.
	require.Len(db.history.lastChangesInsertNumber, 1)
	change, ok := db.history.getRootChanges(emptyRootID)
	require.True(ok)
	require.Empty(change.nodes)
	require.Empty(change.keyChanges)
}

func FuzzMerkleDBEmptyRandomizedActions(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			randSeed int64,
			size uint,
		) {
			if size == 0 {
				t.SkipNow()
			}
			require := require.New(t)
			r := rand.New(rand.NewSource(randSeed)) // #nosec G404
			for _, ts := range validTokenSizes {
				runRandDBTest(
					require,
					r,
					generateRandTest(
						require,
						r,
						size,
						0.01, /*checkHashProbability*/
					),
					ts,
				)
			}
		})
}

func FuzzMerkleDBInitialValuesRandomizedActions(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		initialValues uint,
		numSteps uint,
		randSeed int64,
	) {
		if numSteps == 0 {
			t.SkipNow()
		}
		require := require.New(t)
		r := rand.New(rand.NewSource(randSeed)) // #nosec G404
		for _, ts := range validTokenSizes {
			runRandDBTest(
				require,
				r,
				generateInitialValues(
					require,
					r,
					initialValues,
					numSteps,
					0.001, /*checkHashProbability*/
				),
				ts,
			)
		}
	})
}

// randTest performs random trie operations.
// Instances of this test are created by Generate.
type randTest []randTestStep

type randTestStep struct {
	op    int
	key   []byte // for opUpdate, opDelete, opGet
	value []byte // for opUpdate
}

const (
	opUpdate = iota
	opDelete
	opGet
	opWriteBatch
	opGenerateRangeProof
	opGenerateChangeProof
	opCheckhash
	opMax // boundary value, not an actual op
)

func runRandDBTest(require *require.Assertions, r *rand.Rand, rt randTest, tokenSize int) {
	config := NewConfig()
	config.BranchFactor = tokenSizeToBranchFactor[tokenSize]
	db, err := New(context.Background(), memdb.New(), config)
	require.NoError(err)

	maxProofLen := 100
	maxPastRoots := int(config.HistoryLength)

	var (
		values               = make(map[Key][]byte) // tracks content of the trie
		currentBatch         = db.NewBatch()
		uncommittedKeyValues = make(map[Key][]byte)
		uncommittedDeletes   = set.Set[Key]{}
		pastRoots            = []ids.ID{}
	)

	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	for i, step := range rt {
		require.LessOrEqual(i, len(rt))
		switch step.op {
		case opUpdate:
			require.NoError(currentBatch.Put(step.key, step.value))

			uncommittedKeyValues[ToKey(step.key)] = step.value
			uncommittedDeletes.Remove(ToKey(step.key))
		case opDelete:
			require.NoError(currentBatch.Delete(step.key))

			uncommittedDeletes.Add(ToKey(step.key))
			delete(uncommittedKeyValues, ToKey(step.key))
		case opGenerateRangeProof:
			root, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			if len(pastRoots) > 0 {
				root = pastRoots[r.Intn(len(pastRoots))]
			}

			start := maybe.Nothing[[]byte]()
			if len(step.key) > 0 {
				start = maybe.Some(step.key)
			}
			end := maybe.Nothing[[]byte]()
			if len(step.value) > 0 {
				end = maybe.Some(step.value)
			}

			rangeProof, err := db.GetRangeProofAtRoot(context.Background(), root, start, end, maxProofLen)
			if root == ids.Empty {
				require.ErrorIs(err, ErrEmptyProof)
				continue
			}
			require.NoError(err)
			require.LessOrEqual(len(rangeProof.KeyChanges), maxProofLen)

			require.NoError(rangeProof.Verify(
				context.Background(),
				start,
				end,
				root,
				tokenSize,
				config.Hasher,
				maxProofLen,
			))
		case opGenerateChangeProof:
			root, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			if len(pastRoots) > 1 {
				root = pastRoots[r.Intn(len(pastRoots))]
			}

			start := maybe.Nothing[[]byte]()
			if len(step.key) > 0 {
				start = maybe.Some(step.key)
			}

			end := maybe.Nothing[[]byte]()
			if len(step.value) > 0 {
				end = maybe.Some(step.value)
			}

			changeProof, err := db.GetChangeProof(context.Background(), startRoot, root, start, end, maxProofLen)
			if startRoot == root {
				require.ErrorIs(err, errSameRoot)
				continue
			}
			if root == ids.Empty {
				require.ErrorIs(err, ErrEmptyProof)
				continue
			}
			require.NoError(err)
			require.LessOrEqual(len(changeProof.KeyChanges), maxProofLen)

			changeProofDB, err := getBasicDBWithBranchFactor(tokenSizeToBranchFactor[tokenSize])
			require.NoError(err)

			require.NoError(changeProofDB.VerifyChangeProof(
				context.Background(),
				changeProof,
				start,
				end,
				root,
				maxProofLen,
			))
		case opWriteBatch:
			oldRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			require.NoError(currentBatch.Write())
			currentBatch.Reset()

			if len(uncommittedKeyValues) == 0 && len(uncommittedDeletes) == 0 {
				continue
			}

			maps.Copy(values, uncommittedKeyValues)
			clear(uncommittedKeyValues)

			for key := range uncommittedDeletes {
				delete(values, key)
			}
			uncommittedDeletes.Clear()

			newRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			if oldRoot != newRoot {
				pastRoots = append(pastRoots, newRoot)
				if len(pastRoots) > maxPastRoots {
					pastRoots = pastRoots[len(pastRoots)-maxPastRoots:]
				}
			}

		case opGet:
			v, err := db.Get(step.key)
			if err != nil {
				require.ErrorIs(err, database.ErrNotFound)
			}

			want := values[ToKey(step.key)]
			require.True(bytes.Equal(want, v)) // Use bytes.Equal so nil treated equal to []byte{}

			trieValue, err := getNodeValue(db, string(step.key))
			if err != nil {
				require.ErrorIs(err, database.ErrNotFound)
			}

			require.True(bytes.Equal(want, trieValue)) // Use bytes.Equal so nil treated equal to []byte{}
		case opCheckhash:
			// Create a view with the same key-values as [db]
			newDB, err := getBasicDBWithBranchFactor(tokenSizeToBranchFactor[tokenSize])
			require.NoError(err)

			ops := make([]database.BatchOp, 0, len(values))
			for key, value := range values {
				ops = append(ops, database.BatchOp{
					Key:   key.Bytes(),
					Value: value,
				})
			}

			view, err := newDB.NewView(context.Background(), ViewChanges{BatchOps: ops})
			require.NoError(err)

			// Check that the root of the view is the same as the root of [db]
			newRoot, err := view.GetMerkleRoot(context.Background())
			require.NoError(err)

			dbRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)
			require.Equal(dbRoot, newRoot)
		default:
			require.FailNow("unknown op")
		}
	}
}

func generateRandTestWithKeys(
	require *require.Assertions,
	r *rand.Rand,
	allKeys [][]byte,
	size uint,
	checkHashProbability float64,
) randTest {
	const nilEndProbability = 0.1

	genKey := func() []byte {
		if len(allKeys) < 2 || r.Intn(100) < 10 {
			// new key
			key := make([]byte, r.Intn(50))
			_, err := r.Read(key)
			require.NoError(err)
			allKeys = append(allKeys, key)
			return key
		}
		if len(allKeys) > 2 && r.Intn(100) < 10 {
			// new prefixed key
			prefix := allKeys[r.Intn(len(allKeys))]
			key := make([]byte, r.Intn(50)+len(prefix))
			copy(key, prefix)
			_, err := r.Read(key[len(prefix):])
			require.NoError(err)
			allKeys = append(allKeys, key)
			return key
		}
		// use existing key
		return allKeys[r.Intn(len(allKeys))]
	}

	genEnd := func(key []byte) []byte {
		// got is defined because if a rand method is used
		// in an if statement, the nosec directive doesn't work.
		got := r.Float64() // #nosec G404
		if got < nilEndProbability {
			return nil
		}

		endKey := make([]byte, len(key))
		copy(endKey, key)
		for i := 0; i < len(endKey); i += 2 {
			n := r.Intn(len(endKey))
			if endKey[n] < 250 {
				endKey[n] += byte(r.Intn(int(255 - endKey[n])))
			}
		}
		return endKey
	}

	var steps randTest
	for i := uint(0); i < size-1; {
		step := randTestStep{op: r.Intn(opMax)}
		switch step.op {
		case opUpdate:
			step.key = genKey()
			step.value = make([]byte, r.Intn(50))
			if len(step.value) == 51 {
				step.value = nil
			} else {
				_, err := r.Read(step.value)
				require.NoError(err)
			}
		case opGet, opDelete:
			step.key = genKey()
		case opGenerateRangeProof, opGenerateChangeProof:
			step.key = genKey()
			step.value = genEnd(step.key)
		case opCheckhash:
			// this gets really expensive so control how often it happens
			if r.Float64() > checkHashProbability {
				continue
			}
		}
		steps = append(steps, step)
		i++
	}
	// always end with a full hash of the trie
	steps = append(steps, randTestStep{op: opCheckhash})
	return steps
}

func generateInitialValues(
	require *require.Assertions,
	r *rand.Rand,
	numInitialKeyValues uint,
	size uint,
	percentChanceToFullHash float64,
) randTest {
	const (
		prefixProbability   = 0.1
		nilValueProbability = 0.05
	)

	var allKeys [][]byte
	genKey := func() []byte {
		// new prefixed key
		if len(allKeys) > 2 && r.Float64() < prefixProbability {
			prefix := allKeys[r.Intn(len(allKeys))]
			key := make([]byte, r.Intn(50)+len(prefix))
			copy(key, prefix)
			_, _ = r.Read(key[len(prefix):])
			allKeys = append(allKeys, key)
			return key
		}

		// new key
		key := make([]byte, r.Intn(50))
		_, _ = r.Read(key)
		allKeys = append(allKeys, key)
		return key
	}

	var steps randTest
	for i := uint(0); i < numInitialKeyValues; i++ {
		step := randTestStep{
			op:    opUpdate,
			key:   genKey(),
			value: make([]byte, r.Intn(50)),
		}
		// got is defined because if a rand method is used
		// in an if statement, the nosec directive doesn't work.
		got := r.Float64() // #nosec G404
		if got < nilValueProbability {
			step.value = nil
		} else {
			_, _ = r.Read(step.value)
		}
		steps = append(steps, step)
	}
	steps = append(steps, randTestStep{op: opWriteBatch})
	steps = append(steps, generateRandTestWithKeys(require, r, allKeys, size, percentChanceToFullHash)...)
	return steps
}

func generateRandTest(require *require.Assertions, r *rand.Rand, size uint, percentChanceToFullHash float64) randTest {
	return generateRandTestWithKeys(require, r, [][]byte{}, size, percentChanceToFullHash)
}

// Inserts [n] random key/value pairs into each database.
// Deletes [deletePortion] of the key/value pairs after insertion.
func insertRandomKeyValues(
	require *require.Assertions,
	rand *rand.Rand,
	dbs []database.Database,
	numKeyValues uint,
	deletePortion float64,
) {
	maxKeyLen := units.KiB
	maxValLen := 4 * units.KiB

	require.GreaterOrEqual(deletePortion, float64(0))
	require.LessOrEqual(deletePortion, float64(1))
	for i := uint(0); i < numKeyValues; i++ {
		keyLen := rand.Intn(maxKeyLen)
		key := make([]byte, keyLen)
		_, _ = rand.Read(key)

		valueLen := rand.Intn(maxValLen)
		value := make([]byte, valueLen)
		_, _ = rand.Read(value)
		for _, db := range dbs {
			require.NoError(db.Put(key, value))
		}

		if rand.Float64() < deletePortion {
			for _, db := range dbs {
				require.NoError(db.Delete(key))
			}
		}
	}
}

func TestGetRangeProofAtRootEmptyRootID(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	_, err = db.GetRangeProofAtRoot(
		context.Background(),
		ids.Empty,
		maybe.Nothing[[]byte](),
		maybe.Nothing[[]byte](),
		10,
	)
	require.ErrorIs(err, ErrEmptyProof)
}

func TestGetChangeProofEmptyRootID(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	require.NoError(db.Put([]byte("key"), []byte("value")))

	rootID := db.getMerkleRoot()

	_, err = db.GetChangeProof(
		context.Background(),
		rootID,
		ids.Empty,
		maybe.Nothing[[]byte](),
		maybe.Nothing[[]byte](),
		10,
	)
	require.ErrorIs(err, ErrEmptyProof)
}

func TestCrashRecovery(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	merkleDB, err := newDatabase(
		context.Background(),
		baseDB,
		NewConfig(),
		&mockMetrics{},
	)
	require.NoError(err)

	merkleDBBatch := merkleDB.NewBatch()
	require.NoError(merkleDBBatch.Put([]byte("is this"), []byte("hope")))
	require.NoError(merkleDBBatch.Put([]byte("expected?"), []byte("so")))
	require.NoError(merkleDBBatch.Write())

	expectedRoot, err := merkleDB.GetMerkleRoot(context.Background())
	require.NoError(err)

	// Do not `.Close()` the database to simulate a process crash.

	newMerkleDB, err := newDatabase(
		context.Background(),
		baseDB,
		NewConfig(),
		&mockMetrics{},
	)
	require.NoError(err)

	value, err := newMerkleDB.Get([]byte("is this"))
	require.NoError(err)
	require.Equal([]byte("hope"), value)

	value, err = newMerkleDB.Get([]byte("expected?"))
	require.NoError(err)
	require.Equal([]byte("so"), value)

	rootAfterRecovery, err := newMerkleDB.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(expectedRoot, rootAfterRecovery)
}

func BenchmarkCommitView(b *testing.B) {
	db, err := getBasicDB()
	require.NoError(b, err)

	ops := make([]database.BatchOp, 1_000)
	for i := range ops {
		k := binary.AppendUvarint(nil, uint64(i))
		ops[i] = database.BatchOp{
			Key:   k,
			Value: hashing.ComputeHash256(k),
		}
	}

	ctx := context.Background()
	viewIntf, err := db.NewView(ctx, ViewChanges{BatchOps: ops})
	require.NoError(b, err)

	view := viewIntf.(*view)
	require.NoError(b, view.applyValueChanges(ctx))

	b.Run("apply and commit changes", func(b *testing.B) {
		require := require.New(b)

		for i := 0; i < b.N; i++ {
			db.baseDB = memdb.New() // Keep each iteration independent

			valueNodeBatch := db.baseDB.NewBatch()
			require.NoError(db.applyChanges(ctx, valueNodeBatch, view.changes))
			require.NoError(db.commitValueChanges(ctx, valueNodeBatch))
		}
	})
}

func BenchmarkIteration(b *testing.B) {
	db, err := getBasicDB()
	require.NoError(b, err)

	ops := make([]database.BatchOp, 1_000)
	for i := range ops {
		k := binary.AppendUvarint(nil, uint64(i))
		ops[i] = database.BatchOp{
			Key:   k,
			Value: hashing.ComputeHash256(k),
		}
	}

	ctx := context.Background()
	view, err := db.NewView(ctx, ViewChanges{BatchOps: ops})
	require.NoError(b, err)

	require.NoError(b, view.CommitToDB(ctx))

	b.Run("create iterator", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			it := db.NewIterator()
			it.Release()
		}
	})

	b.Run("iterate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			it := db.NewIterator()
			for it.Next() {
			}
			it.Release()
		}
	})
}

func Test_FindNextKey_InSync(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	now := time.Now().UnixNano()

	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 1000)
	require.NoError(err)

	db, err := New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	// sync db to the same state as dbToSync
	proof, err := dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	proof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)

	// the two dbs should be in sync, so next key should be nil
	lastKey := proof.KeyChanges[len(proof.KeyChanges)-1].Key
	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), proof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	// add an extra value to sync db past the last key returned
	newKeyVal := make([]byte, len(lastKey))
	copy(newKeyVal, lastKey)
	newKeyVal = append(newKeyVal, 16) // make sure new key is after last key
	require.NoError(db.Put(newKeyVal, []byte{1}))

	// create a range endpoint that is before the newly added key, but after the last key
	endPointBeforeNewKey := make([]byte, len(newKeyVal))
	copy(endPointBeforeNewKey, newKeyVal)
	endPointBeforeNewKey[len(endPointBeforeNewKey)-1] = 8

	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some(endPointBeforeNewKey), proof)
	require.NoError(err)

	// next key would be after the end of the range, so it returns Nothing instead
	require.True(nextKey.IsNothing())
}

func Test_FindNextKey_Deleted(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	dbToSync, err := New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(dbToSync.Put([]byte{0x10}, []byte{1}))
	require.NoError(dbToSync.Put([]byte{0x11, 0x11}, []byte{2}))

	// Create empty DB to commit to one key
	db, err := New(ctx, memdb.New(), newDefaultDBConfig())
	require.NoError(err)
	require.NoError(db.Put([]byte{0x13}, []byte{3}))

	// 0x12 was "deleted" and there should be no extra node in the proof since there was nothing with a common prefix
	rangeProof, err := dbToSync.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Some([]byte{0x12}), 100)
	require.NoError(err)

	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), rangeProof)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x13}), nextKey)

	// 0x11 was "deleted" and 0x11.0x11 should be in the exclusion proof
	extraNodeProof, err := dbToSync.GetProof(context.Background(), []byte{0x11})
	require.NoError(err)
	rangeProof.EndProof = extraNodeProof.Path

	// Reset the db and commit new proof
	require.NoError(db.Clear())
	require.NoError(db.Put([]byte{0x13}, []byte{3}))
	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), rangeProof)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x13}), nextKey)
}

func Test_FindNextKey_BranchInLocal(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	db, err := New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x11, 0x11}, []byte{2}))

	rangeProof, err := db.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), 100)
	require.NoError(err)

	require.NoError(db.Put([]byte{0x11, 0x15}, []byte{4}))

	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), rangeProof)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x11, 0x15}), nextKey)
}

func Test_FindNextKey_BranchInReceived(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	db, err := New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	require.NoError(db.Put([]byte{0x11}, []byte{1}))
	require.NoError(db.Put([]byte{0x12}, []byte{2}))
	require.NoError(db.Put([]byte{0x12, 0xA0}, []byte{4}))

	rangeProof, err := db.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x12}), 100)
	require.NoError(err)

	require.NoError(db.Delete([]byte{0x12, 0xA0}))

	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some([]byte{0x20}), rangeProof)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x12, 0xA0}), nextKey)
}

func Test_FindNextKey_ExtraValues(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 1000)
	require.NoError(err)

	// Make a matching DB
	db, err := New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	rangeProof, err := dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	// Get a new partial range proof
	rangeProof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 500)
	require.NoError(err)

	// add an extra value to local db
	lastKey := rangeProof.KeyChanges[len(rangeProof.KeyChanges)-1].Key
	afterKeyVal := make([]byte, len(lastKey))
	copy(afterKeyVal, lastKey)
	afterKeyVal = append(afterKeyVal, 16) // make sure new key is after last key

	require.NoError(db.Put(afterKeyVal, []byte{1}))

	// next key at prefix of newly added point
	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.HasValue())
	require.True(isPrefix(afterKeyVal, nextKey.Value()))

	require.NoError(db.Delete(afterKeyVal))

	require.NoError(dbToSync.Put(afterKeyVal, []byte{1}))

	rangeProof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some(lastKey), 500)
	require.NoError(err)

	// next key at prefix of newly added point
	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.HasValue())

	// deal with odd length key
	require.True(isPrefix(afterKeyVal, nextKey.Value()))
}

func isPrefix(data []byte, prefix []byte) bool {
	if prefix[len(prefix)-1]%16 == 0 {
		index := 0
		for ; index < len(prefix)-1; index++ {
			if data[index] != prefix[index] {
				return false
			}
		}
		return data[index]>>4 == prefix[index]>>4
	}
	return bytes.HasPrefix(data, prefix)
}

func Test_FindNextKey_DifferentChild(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	dbToSync, err := generateTrie(t, r, 500)
	require.NoError(err)

	// Make a matching DB
	db, err := New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)
	rangeProof, err := dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1000)
	require.NoError(err)
	nextKey, err := db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.IsNothing())

	rangeProof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 100)
	require.NoError(err)
	lastKey := rangeProof.KeyChanges[len(rangeProof.KeyChanges)-1].Key

	// local db has a different child than remote db
	lastKey = append(lastKey, 16)
	require.NoError(db.Put(lastKey, []byte{1}))

	require.NoError(dbToSync.Put(lastKey, []byte{2}))

	rangeProof, err = dbToSync.GetRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Some(rangeProof.KeyChanges[len(rangeProof.KeyChanges)-1].Key), 100)
	require.NoError(err)

	nextKey, err = db.CommitRangeProof(ctx, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), rangeProof)
	require.NoError(err)
	require.True(nextKey.HasValue())
	require.Equal(lastKey, nextKey.Value())
}

// Test findNextKey by computing the expected result in a naive, inefficient
// way and comparing it to the actual result
func TestFindNextKeyRandom(t *testing.T) {
	now := time.Now().UnixNano()
	ctx := context.Background()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404
	require := require.New(t)

	// Create a "remote" database and "local" database
	remoteDB, err := New(
		ctx,
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(err)

	config := newDefaultDBConfig()
	localDB, err := New(
		ctx,
		memdb.New(),
		config,
	)
	require.NoError(err)

	var (
		numProofsToTest  = 250
		numKeyValues     = 250
		maxKeyLen        = 256
		maxValLen        = 256
		maxRangeStartLen = 8
		maxRangeEndLen   = 8
		maxProofLen      = 128
	)

	// Put random keys into the databases
	for _, db := range []database.Database{remoteDB, localDB} {
		for i := 0; i < numKeyValues; i++ {
			key := make([]byte, rand.Intn(maxKeyLen))
			_, _ = rand.Read(key)
			val := make([]byte, rand.Intn(maxValLen))
			_, _ = rand.Read(val)
			require.NoError(db.Put(key, val))
		}
	}

	// Repeatedly generate end proofs from the remote database and compare
	// the result of findNextKey to the expected result.
	for proofIndex := 0; proofIndex < numProofsToTest; proofIndex++ {
		// Generate a proof for a random key
		var (
			rangeStart []byte
			rangeEnd   []byte
		)
		// Generate a valid range start and end
		for rangeStart == nil || bytes.Compare(rangeStart, rangeEnd) == 1 {
			rangeStart = make([]byte, rand.Intn(maxRangeStartLen)+1)
			_, _ = rand.Read(rangeStart)
			rangeEnd = make([]byte, rand.Intn(maxRangeEndLen)+1)
			_, _ = rand.Read(rangeEnd)
		}

		startKey := maybe.Nothing[[]byte]()
		if len(rangeStart) > 0 {
			startKey = maybe.Some(rangeStart)
		}
		endKey := maybe.Nothing[[]byte]()
		if len(rangeEnd) > 0 {
			endKey = maybe.Some(rangeEnd)
		}

		remoteProof, err := remoteDB.GetRangeProof(
			ctx,
			startKey,
			endKey,
			rand.Intn(maxProofLen)+1,
		)
		require.NoError(err)

		if len(remoteProof.KeyChanges) == 0 {
			continue
		}
		lastReceivedKey := remoteProof.KeyChanges[len(remoteProof.KeyChanges)-1].Key

		// Commit the proof to the local database as we do
		// in the actual syncer.
		_, err = localDB.CommitRangeProof(
			ctx,
			startKey,
			endKey,
			remoteProof,
		)
		require.NoError(err)

		localProof, err := localDB.GetProof(
			context.Background(),
			lastReceivedKey,
		)
		require.NoError(err)

		type keyAndID struct {
			key Key
			id  ids.ID
		}

		// Set of key prefix/ID pairs proven by the remote database's end proof.
		remoteKeyIDs := []keyAndID{}
		for _, node := range remoteProof.EndProof {
			for childIdx, childID := range node.Children {
				remoteKeyIDs = append(remoteKeyIDs, keyAndID{
					key: node.Key.Extend(ToToken(childIdx, BranchFactorToTokenSize[config.BranchFactor])),
					id:  childID,
				})
			}
		}

		// Set of key prefix/ID pairs proven by the local database's proof.
		localKeyIDs := []keyAndID{}
		for _, node := range localProof.Path {
			for childIdx, childID := range node.Children {
				localKeyIDs = append(localKeyIDs, keyAndID{
					key: node.Key.Extend(ToToken(childIdx, BranchFactorToTokenSize[config.BranchFactor])),
					id:  childID,
				})
			}
		}

		// Sort in ascending order by key prefix.
		serializedPathCompare := func(i, j keyAndID) int {
			return i.key.Compare(j.key)
		}
		slices.SortFunc(remoteKeyIDs, serializedPathCompare)
		slices.SortFunc(localKeyIDs, serializedPathCompare)

		// Filter out keys that are before the last received key
		findBounds := func(keyIDs []keyAndID) (int, int) {
			var (
				firstIdxInRange      = len(keyIDs)
				firstIdxInRangeFound = false
				firstIdxOutOfRange   = len(keyIDs)
			)
			for i, keyID := range keyIDs {
				if !firstIdxInRangeFound && bytes.Compare(keyID.key.Bytes(), lastReceivedKey) > 0 {
					firstIdxInRange = i
					firstIdxInRangeFound = true
					continue
				}
				if bytes.Compare(keyID.key.Bytes(), rangeEnd) > 0 {
					firstIdxOutOfRange = i
					break
				}
			}
			return firstIdxInRange, firstIdxOutOfRange
		}

		remoteFirstIdxAfterLastReceived, remoteFirstIdxAfterEnd := findBounds(remoteKeyIDs)
		remoteKeyIDs = remoteKeyIDs[remoteFirstIdxAfterLastReceived:remoteFirstIdxAfterEnd]

		localFirstIdxAfterLastReceived, localFirstIdxAfterEnd := findBounds(localKeyIDs)
		localKeyIDs = localKeyIDs[localFirstIdxAfterLastReceived:localFirstIdxAfterEnd]

		// Find smallest difference between the set of key/ID pairs proven by
		// the remote/local proofs for key/ID pairs after the last received key.
		var (
			smallestDiffKey Key
			foundDiff       bool
		)
		for i := 0; i < len(remoteKeyIDs) && i < len(localKeyIDs); i++ {
			// See if the keys are different.
			smaller, bigger := remoteKeyIDs[i], localKeyIDs[i]
			if serializedPathCompare(localKeyIDs[i], remoteKeyIDs[i]) == -1 {
				smaller, bigger = localKeyIDs[i], remoteKeyIDs[i]
			}

			if smaller.key != bigger.key || smaller.id != bigger.id {
				smallestDiffKey = smaller.key
				foundDiff = true
				break
			}
		}
		if !foundDiff {
			// All the keys were equal. The smallest diff is the next key
			// in the longer of the lists (if they're not same length.)
			if len(remoteKeyIDs) < len(localKeyIDs) {
				smallestDiffKey = localKeyIDs[len(remoteKeyIDs)].key
			} else if len(remoteKeyIDs) > len(localKeyIDs) {
				smallestDiffKey = remoteKeyIDs[len(localKeyIDs)].key
			}
		}

		// Get the actual value from the syncer
		gotFirstDiff, err := localDB.CommitRangeProof(ctx, maybe.Nothing[[]byte](), endKey, remoteProof)
		require.NoError(err)

		if bytes.Compare(smallestDiffKey.Bytes(), rangeEnd) >= 0 {
			// The smallest key which differs is after the range end so the
			// next key to get should be nil because we're done fetching the range.
			require.True(gotFirstDiff.IsNothing())
		} else {
			require.Equal(smallestDiffKey.Bytes(), gotFirstDiff.Value())
		}
	}
}
