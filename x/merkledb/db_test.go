// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const minCacheSize = 1000

func newNoopTracer() trace.Tracer {
	tracer, _ := trace.New(trace.Config{Enabled: false})
	return tracer
}

func Test_MerkleDB_DB_Interface(t *testing.T) {
	for _, test := range database.Tests {
		db, err := New(
			context.Background(),
			memdb.New(),
			Config{
				Tracer:         newNoopTracer(),
				HistoryLength:  300,
				ValueCacheSize: minCacheSize,
				NodeCacheSize:  minCacheSize,
			},
		)
		require.NoError(t, err)
		test(t, db)
	}
}

func Benchmark_MerkleDB_DBInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			db, err := New(
				context.Background(),
				memdb.New(),
				Config{
					Tracer:         newNoopTracer(),
					HistoryLength:  300,
					ValueCacheSize: minCacheSize,
					NodeCacheSize:  minCacheSize,
				},
			)
			require.NoError(b, err)
			bench(b, db, "merkledb", keys, values)
		}
	}
}

func Test_MerkleDB_DB_Load_Root_From_DB(t *testing.T) {
	require := require.New(t)
	rdb := memdb.New()
	defer rdb.Close()

	db, err := New(
		context.Background(),
		rdb,
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: 100,
			NodeCacheSize:  100,
		},
	)
	require.NoError(err)

	// Populate initial set of keys
	view, err := db.NewView(context.Background())
	require.NoError(err)
	for i := 0; i < 100; i++ {
		k := []byte(strconv.Itoa(i))
		require.NoError(view.Insert(context.Background(), k, hashing.ComputeHash256(k)))
	}
	require.NoError(view.Commit(context.Background()))

	root, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.NoError(db.Close())

	// reloading the DB, should set the root back to the one that was saved to the memdb
	db, err = New(
		context.Background(),
		rdb,
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: 100,
			NodeCacheSize:  100,
		},
	)
	require.NoError(err)
	reloadedRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(root, reloadedRoot)
}

func Test_MerkleDB_DB_Rebuild(t *testing.T) {
	require := require.New(t)

	rdb := memdb.New()
	defer rdb.Close()

	initialSize := 10_000

	db, err := New(
		context.Background(),
		rdb,
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: initialSize,
			NodeCacheSize:  initialSize,
		},
	)
	require.NoError(err)

	// Populate initial set of keys
	view, err := db.NewView(context.Background())
	require.NoError(err)
	for i := 0; i < initialSize; i++ {
		k := []byte(strconv.Itoa(i))
		require.NoError(view.Insert(context.Background(), k, hashing.ComputeHash256(k)))
	}
	require.NoError(view.Commit(context.Background()))

	root, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.NoError(db.rebuild(context.Background()))

	rebuiltRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(root, rebuiltRoot)
}

func Test_MerkleDB_Failed_Batch_Commit(t *testing.T) {
	memDB := memdb.New()
	db, err := New(
		context.Background(),
		memDB,
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
		},
	)
	require.NoError(t, err)

	_ = memDB.Close()

	batch := db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key2"), []byte("2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key3"), []byte("3"))
	require.NoError(t, err)
	err = batch.Write()
	// batch fails
	require.ErrorIs(t, err, database.ErrClosed)
}

func Test_MerkleDB_Value_Cache(t *testing.T) {
	memDB := memdb.New()
	db, err := New(
		context.Background(),
		memDB,
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)

	batch := db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("1"))
	require.NoError(t, err)

	err = batch.Put([]byte("key2"), []byte("2"))
	require.NoError(t, err)

	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	batch = db.NewBatch()
	// force key2 to be inserted into the cache as not found
	err = batch.Delete([]byte("key2"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	_ = memDB.Close()

	// still works because key1 is read from cache
	value, err := db.Get([]byte("key1"))
	require.NoError(t, err)
	require.Equal(t, []byte("1"), value)

	// still returns missing instead of closed because key2 is read from cache
	_, err = db.Get([]byte("key2"))
	require.ErrorIs(t, err, database.ErrNotFound)
}

func Test_MerkleDB_Commit_Proof_To_Empty_Trie(t *testing.T) {
	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key2"), []byte("2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key3"), []byte("3"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	proof, err := db.GetRangeProof(context.Background(), []byte("key1"), []byte("key3"), 10)
	require.NoError(t, err)

	freshDB, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)

	err = freshDB.CommitRangeProof(context.Background(), []byte("key1"), proof)
	require.NoError(t, err)

	value, err := freshDB.Get([]byte("key2"))
	require.NoError(t, err)
	require.Equal(t, []byte("2"), value)

	freshRoot, err := freshDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)
	oldRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)
	require.Equal(t, oldRoot, freshRoot)
}

func Test_MerkleDB_Commit_Proof_To_Filled_Trie(t *testing.T) {
	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key2"), []byte("2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key3"), []byte("3"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	proof, err := db.GetRangeProof(context.Background(), []byte("key1"), []byte("key3"), 10)
	require.NoError(t, err)

	freshDB, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)
	batch = freshDB.NewBatch()
	err = batch.Put([]byte("key1"), []byte("3"))
	require.NoError(t, err)
	err = batch.Put([]byte("key2"), []byte("4"))
	require.NoError(t, err)
	err = batch.Put([]byte("key3"), []byte("5"))
	require.NoError(t, err)
	err = batch.Put([]byte("key25"), []byte("5"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	err = freshDB.CommitRangeProof(context.Background(), []byte("key1"), proof)
	require.NoError(t, err)

	value, err := freshDB.Get([]byte("key2"))
	require.NoError(t, err)
	require.Equal(t, []byte("2"), value)

	freshRoot, err := freshDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)
	oldRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)
	require.Equal(t, oldRoot, freshRoot)
}

func Test_MerkleDB_InsertNil(t *testing.T) {
	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key0"), nil)
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	value, err := db.Get([]byte("key0"))
	require.NoError(t, err)
	require.Nil(t, value)

	value, err = getNodeValue(db, "key0")
	require.NoError(t, err)
	require.Nil(t, value)
}

func Test_MerkleDB_InsertAndRetrieve(t *testing.T) {
	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)

	// value hasn't been inserted so shouldn't exist
	value, err := db.Get([]byte("key"))
	require.Error(t, err)
	require.Equal(t, database.ErrNotFound, err)
	require.Nil(t, value)

	err = db.Put([]byte("key"), []byte("value"))
	require.NoError(t, err)

	value, err = db.Get([]byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value)
}

func Test_MerkleDB_HealthCheck(t *testing.T) {
	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)
	val, err := db.HealthCheck(context.Background())
	require.NoError(t, err)
	require.Nil(t, val)
}

func Test_MerkleDB_Overwrite(t *testing.T) {
	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)

	err = db.Put([]byte("key"), []byte("value0"))
	require.NoError(t, err)

	value, err := db.Get([]byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("value0"), value)

	err = db.Put([]byte("key"), []byte("value1"))
	require.NoError(t, err)

	value, err = db.Get([]byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), value)
}

func Test_MerkleDB_Delete(t *testing.T) {
	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)

	err = db.Put([]byte("key"), []byte("value0"))
	require.NoError(t, err)

	value, err := db.Get([]byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("value0"), value)

	err = db.Delete([]byte("key"))
	require.NoError(t, err)

	value, err = db.Get([]byte("key"))
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, value)
}

func Test_MerkleDB_DeleteMissingKey(t *testing.T) {
	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(t, err)

	err = db.Delete([]byte("key"))
	require.NoError(t, err)
}

func Test_MerkleDB_Random_Insert_Ordering(t *testing.T) {
	totalState := 1000
	var (
		allKeys [][]byte
		keyMap  map[string]struct{}
	)
	genKey := func(r *rand.Rand) []byte {
		count := 0
		for {
			var key []byte
			if len(allKeys) > 2 && r.Intn(100) < 10 {
				// new prefixed key
				prefix := allKeys[r.Intn(len(allKeys))]
				key = make([]byte, r.Intn(50)+len(prefix))
				copy(key, prefix)
				_, err := r.Read(key[len(prefix):])
				require.NoError(t, err)
			} else {
				key = make([]byte, r.Intn(50))
				_, err := r.Read(key)
				require.NoError(t, err)
			}
			if _, ok := keyMap[string(key)]; !ok {
				allKeys = append(allKeys, key)
				keyMap[string(key)] = struct{}{}
				return key
			}
			count++
		}
	}

	for i := 0; i < 3; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404

		ops := make([]*testOperation, 0, totalState)
		allKeys = [][]byte{}
		keyMap = map[string]struct{}{}
		for x := 0; x < totalState; x++ {
			key := genKey(r)
			value := make([]byte, r.Intn(51))
			if len(value) == 51 {
				value = nil
			} else {
				_, err := r.Read(value)
				require.NoError(t, err)
			}
			ops = append(ops, &testOperation{key: key, value: value})
		}
		db, err := New(
			context.Background(),
			memdb.New(),
			Config{
				Tracer:         newNoopTracer(),
				HistoryLength:  300,
				ValueCacheSize: minCacheSize,
				NodeCacheSize:  minCacheSize,
			},
		)
		require.NoError(t, err)
		result, err := applyOperations(db, ops)
		require.NoError(t, err)
		primaryRoot, err := result.GetMerkleRoot(context.Background())
		require.NoError(t, err)
		for shuffleIndex := 0; shuffleIndex < 3; shuffleIndex++ {
			r.Shuffle(totalState, func(i, j int) {
				ops[i], ops[j] = ops[j], ops[i]
			})
			result, err := applyOperations(db, ops)
			require.NoError(t, err)
			newRoot, err := result.GetMerkleRoot(context.Background())
			require.NoError(t, err)
			require.Equal(t, primaryRoot, newRoot)
		}
	}
}

type testOperation struct {
	key    []byte
	value  []byte
	delete bool
}

func applyOperations(t *Database, ops []*testOperation) (Trie, error) {
	view, err := t.NewView(context.Background())
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if op.delete {
			if err := view.Remove(context.Background(), op.key); err != nil {
				return nil, err
			}
		} else {
			if err := view.Insert(context.Background(), op.key, op.value); err != nil {
				return nil, err
			}
		}
	}
	return view, nil
}

func Test_MerkleDB_RandomCases(t *testing.T) {
	require := require.New(t)

	for i := 150; i < 500; i += 10 {
		db, err := New(
			context.Background(),
			memdb.New(),
			Config{
				Tracer:         newNoopTracer(),
				HistoryLength:  300,
				ValueCacheSize: minCacheSize,
				NodeCacheSize:  minCacheSize,
			},
		)
		require.NoError(err)
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		runRandDBTest(require, db, r, generate(require, r, i, .01))
	}
}

func Test_MerkleDB_RandomCases_InitialValues(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  300,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	r := rand.New(rand.NewSource(int64(0))) // #nosec G404
	runRandDBTest(require, db, r, generateInitialValues(require, r, 2000, 2500, 0.0))
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
	opGenerateProof
	opCheckhash
	opMax // boundary value, not an actual op
)

func runRandDBTest(require *require.Assertions, db *Database, r *rand.Rand, rt randTest) {
	values := make(map[path][]byte) // tracks content of the trie
	currentBatch := db.NewBatch()
	currentValues := make(map[path][]byte)
	deleteValues := make(map[path]struct{})
	pastRoots := []ids.ID{}

	for _, step := range rt {
		switch step.op {
		case opUpdate:
			err := currentBatch.Put(step.key, step.value)
			require.NoError(err)
			currentValues[newPath(step.key)] = step.value
			delete(deleteValues, newPath(step.key))
		case opDelete:
			err := currentBatch.Delete(step.key)
			require.NoError(err)
			deleteValues[newPath(step.key)] = struct{}{}
			delete(currentValues, newPath(step.key))
		case opGenerateProof:
			root, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)
			if len(pastRoots) > 0 {
				root = pastRoots[r.Intn(len(pastRoots))]
			}
			rangeProof, err := db.GetRangeProofAtRoot(context.Background(), root, step.key, step.value, 100)
			require.NoError(err)
			err = rangeProof.Verify(
				context.Background(),
				step.key,
				step.value,
				root,
			)
			require.NoError(err)
		case opWriteBatch:
			oldRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)
			err = currentBatch.Write()
			require.NoError(err)
			for key, value := range currentValues {
				values[key] = value
			}
			for key := range deleteValues {
				delete(values, key)
			}

			if len(currentValues) == 0 && len(deleteValues) == 0 {
				continue
			}
			newRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)
			if oldRoot != newRoot {
				pastRoots = append(pastRoots, newRoot)
				if len(pastRoots) > 300 {
					pastRoots = pastRoots[len(pastRoots)-300:]
				}
			}
			currentValues = map[path][]byte{}
			deleteValues = map[path]struct{}{}
			currentBatch = db.NewBatch()
		case opGet:
			v, err := db.Get(step.key)
			if err != nil {
				require.ErrorIs(err, database.ErrNotFound)
			}
			want := values[newPath(step.key)]
			require.True(bytes.Equal(want, v)) // Use bytes.Equal so nil treated equal to []byte{}
			trieValue, err := getNodeValue(db, string(step.key))
			if err != nil {
				require.ErrorIs(err, database.ErrNotFound)
			}
			require.True(bytes.Equal(want, trieValue)) // Use bytes.Equal so nil treated equal to []byte{}
		case opCheckhash:
			dbTrie, err := newDatabase(
				context.Background(),
				memdb.New(),
				Config{
					Tracer:         newNoopTracer(),
					ValueCacheSize: minCacheSize,
					HistoryLength:  0,
					NodeCacheSize:  minCacheSize,
				},
				&mockMetrics{},
			)
			require.NoError(err)
			localTrie := Trie(dbTrie)
			for key, value := range values {
				err := localTrie.Insert(context.Background(), key.Serialize().Value, value)
				require.NoError(err)
			}
			calculatedRoot, err := localTrie.GetMerkleRoot(context.Background())
			require.NoError(err)
			dbRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)
			require.Equal(dbRoot, calculatedRoot)
		}
	}
}

func generateWithKeys(require *require.Assertions, allKeys [][]byte, r *rand.Rand, size int, percentChanceToFullHash float64) randTest {
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
		shouldBeNil := r.Intn(10)
		if shouldBeNil == 0 {
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
	for i := 0; i < size-1; {
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
		case opGenerateProof:
			step.key = genKey()
			step.value = genEnd(step.key)
		case opCheckhash:
			// this gets really expensive so control how often it happens
			if r.Float64() >= percentChanceToFullHash {
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

func generateInitialValues(require *require.Assertions, r *rand.Rand, initialValues int, size int, percentChanceToFullHash float64) randTest {
	var allKeys [][]byte
	genKey := func() []byte {
		// new prefixed key
		if len(allKeys) > 2 && r.Intn(100) < 10 {
			prefix := allKeys[r.Intn(len(allKeys))]
			key := make([]byte, r.Intn(50)+len(prefix))
			copy(key, prefix)
			_, err := r.Read(key[len(prefix):])
			require.NoError(err)
			allKeys = append(allKeys, key)
			return key
		}

		// new key
		key := make([]byte, r.Intn(50))
		_, err := r.Read(key)
		require.NoError(err)
		allKeys = append(allKeys, key)
		return key
	}

	var steps randTest
	for i := 0; i < initialValues; i++ {
		step := randTestStep{op: opUpdate}
		step.key = genKey()
		step.value = make([]byte, r.Intn(51))
		if len(step.value) == 51 {
			step.value = nil
		} else {
			_, err := r.Read(step.value)
			require.NoError(err)
		}
		steps = append(steps, step)
	}
	steps = append(steps, randTestStep{op: opWriteBatch})
	steps = append(steps, generateWithKeys(require, allKeys, r, size, percentChanceToFullHash)...)
	return steps
}

func generate(require *require.Assertions, r *rand.Rand, size int, percentChanceToFullHash float64) randTest {
	var allKeys [][]byte
	return generateWithKeys(require, allKeys, r, size, percentChanceToFullHash)
}
