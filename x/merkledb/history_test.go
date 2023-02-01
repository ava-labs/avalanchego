// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

func Test_History_Simple(t *testing.T) {
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
	batch := db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	val, err := db.Get([]byte("key"))
	require.NoError(err)
	require.Equal([]byte("value"), val)

	origProof, err := db.GetRangeProof(context.Background(), []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.root.id
	err = origProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value0"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(err)
	err = batch.Put([]byte("key8"), []byte("value8"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err = db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("k"), []byte("v"))
	require.NoError(err)
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err = db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Delete([]byte("k"))
	require.NoError(err)
	err = batch.Delete([]byte("ke"))
	require.NoError(err)
	err = batch.Delete([]byte("key"))
	require.NoError(err)
	err = batch.Delete([]byte("key1"))
	require.NoError(err)
	err = batch.Put([]byte("key2"), []byte("value2"))
	require.NoError(err)
	err = batch.Delete([]byte("key3"))
	require.NoError(err)
	err = batch.Delete([]byte("key4"))
	require.NoError(err)
	err = batch.Delete([]byte("key5"))
	require.NoError(err)
	err = batch.Delete([]byte("key8"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err = db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)
}

func Test_History_Large(t *testing.T) {
	require := require.New(t)

	for i := 1; i < 10; i++ {
		r := rand.New(rand.NewSource(int64(i))) // #nosec G404
		db, err := New(
			context.Background(),
			memdb.New(),
			Config{
				Tracer:         newNoopTracer(),
				HistoryLength:  1500,
				ValueCacheSize: 1000,
				NodeCacheSize:  1000,
			},
		)
		require.NoError(err)
		roots := []ids.ID{}
		// make sure they stay in sync
		for x := 0; x < 500; x++ {
			addkey := make([]byte, r.Intn(50))
			_, err := r.Read(addkey)
			require.NoError(err)
			val := make([]byte, r.Intn(50))
			_, err = r.Read(val)
			require.NoError(err)

			err = db.Put(addkey, val)
			require.NoError(err)

			addNilkey := make([]byte, r.Intn(50))
			_, err = r.Read(addNilkey)
			require.NoError(err)
			err = db.Put(addNilkey, nil)
			require.NoError(err)

			deleteKeyStart := make([]byte, r.Intn(50))
			_, err = r.Read(deleteKeyStart)
			require.NoError(err)

			it := db.NewIteratorWithStart(deleteKeyStart)
			if it.Next() {
				err = db.Delete(it.Key())
				require.NoError(err)
			}
			require.NoError(it.Error())
			it.Release()

			root, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)
			roots = append(roots, root)
		}
		proof, err := db.GetRangeProofAtRoot(context.Background(), roots[0], nil, nil, 10)
		require.NoError(err)
		require.NotNil(proof)

		err = proof.Verify(context.Background(), nil, nil, roots[0])
		require.NoError(err)
	}
}

func Test_History_Bad_GetValueChanges_Input(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  5,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	toBeDeletedRoot := db.getMerkleRoot()

	batch = db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value0"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	startRoot := db.getMerkleRoot()

	batch = db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value0"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key2"), []byte("value3"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	endRoot := db.getMerkleRoot()

	// ensure these start as valid calls
	_, err = db.history.getValueChanges(toBeDeletedRoot, endRoot, nil, nil, 1)
	require.NoError(err)
	_, err = db.history.getValueChanges(startRoot, endRoot, nil, nil, 1)
	require.NoError(err)

	_, err = db.history.getValueChanges(startRoot, endRoot, nil, nil, -1)
	require.Error(err, ErrInvalidMaxLength)

	_, err = db.history.getValueChanges(endRoot, startRoot, nil, nil, 1)
	require.Error(err, ErrStartRootNotFound)

	// trigger the first root to be deleted by exiting the lookback window
	batch = db.NewBatch()
	err = batch.Put([]byte("key2"), []byte("value4"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	// now this root should no lnger be present
	_, err = db.history.getValueChanges(toBeDeletedRoot, endRoot, nil, nil, 1)
	require.Error(err, ErrRootIDNotPresent)

	// same start/end roots should yield an empty changelist
	changes, err := db.history.getValueChanges(endRoot, endRoot, nil, nil, 10)
	require.NoError(err)
	require.Len(changes.values, 0)
}

func Test_History_Trigger_History_Queue_Looping(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  2,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	origRootID := db.getMerkleRoot()

	origProof, err := db.GetRangeProof(context.Background(), []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(origProof)
	err = origProof.Verify(
		context.Background(),
		[]byte("k"),
		[]byte("key3"),
		origRootID,
	)
	require.NoError(err)

	// write a new value into the db, now there should be 2 roots in the history
	batch = db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value0"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	// ensure that previous root is still present and generates a valid proof
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(
		context.Background(),
		[]byte("k"),
		[]byte("key3"),
		origRootID,
	)
	require.NoError(err)

	// trigger a new root to be added to the history, which should cause rollover since there can only be 2
	batch = db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	// proof from first root shouldn't be generatable since it should have been removed from the history
	_, err = db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.Error(err, ErrRootIDNotPresent)
}

func Test_History_Values_Lookup_Over_Queue_Break(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  4,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	// write a new value into the db
	batch = db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value0"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	startRoot := db.getMerkleRoot()

	// write a new value into the db
	batch = db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value0"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	// write a new value into the db that overwrites key1
	batch = db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	// trigger a new root to be added to the history, which should cause rollover since there can only be 3
	batch = db.NewBatch()
	err = batch.Put([]byte("key2"), []byte("value3"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	endRoot := db.getMerkleRoot()

	// changes should still be collectable even though the history has had to loop due to hitting max size
	changes, err := db.history.getValueChanges(startRoot, endRoot, nil, nil, 10)
	require.NoError(err)
	require.Contains(changes.values, newPath([]byte("key1")))
	require.Equal([]byte("value1"), changes.values[newPath([]byte("key1"))].after.value)
	require.Contains(changes.values, newPath([]byte("key2")))
	require.Equal([]byte("value3"), changes.values[newPath([]byte("key2"))].after.value)
}

func Test_History_RepeatedRoot(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(err)
	err = batch.Put([]byte("key2"), []byte("value2"))
	require.NoError(err)
	err = batch.Put([]byte("key3"), []byte("value3"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	origProof, err := db.GetRangeProof(context.Background(), []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.root.id
	err = origProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("other"))
	require.NoError(err)
	err = batch.Put([]byte("key2"), []byte("other"))
	require.NoError(err)
	err = batch.Put([]byte("key3"), []byte("other"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	// revert state to be the same as in orig proof
	batch = db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(err)
	err = batch.Put([]byte("key2"), []byte("value2"))
	require.NoError(err)
	err = batch.Put([]byte("key3"), []byte("value3"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	newProof, err = db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)
}

func Test_History_ExcessDeletes(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	origProof, err := db.GetRangeProof(context.Background(), []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.root.id
	err = origProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Delete([]byte("key1"))
	require.NoError(err)
	err = batch.Delete([]byte("key2"))
	require.NoError(err)
	err = batch.Delete([]byte("key3"))
	require.NoError(err)
	err = batch.Delete([]byte("key4"))
	require.NoError(err)
	err = batch.Delete([]byte("key5"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)
}

func Test_History_DontIncludeAllNodes(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	origProof, err := db.GetRangeProof(context.Background(), []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.root.id
	err = origProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("z"), []byte("z"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)
}

func Test_History_Branching2Nodes(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	origProof, err := db.GetRangeProof(context.Background(), []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.root.id
	err = origProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("k"), []byte("v"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)
}

func Test_History_Branching3Nodes(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key123"), []byte("value123"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	origProof, err := db.GetRangeProof(context.Background(), []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.root.id
	err = origProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key321"), []byte("value321"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, []byte("k"), []byte("key3"), 10)
	require.NoError(err)
	require.NotNil(newProof)
	err = newProof.Verify(context.Background(), []byte("k"), []byte("key3"), origRootID)
	require.NoError(err)
}

func Test_History_MaxLength(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  2,
			ValueCacheSize: 1000,
			NodeCacheSize:  1000,
		},
	)
	require.NoError(err)

	batch := db.NewBatch()
	err = batch.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	oldRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("k"), []byte("v"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	require.Contains(db.history.lastChanges, oldRoot)

	batch = db.NewBatch()
	err = batch.Put([]byte("k1"), []byte("v2"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	require.NotContains(db.history.lastChanges, oldRoot)
}

func Test_Change_List(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  100,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key20"), []byte("value20"))
	require.NoError(err)
	err = batch.Put([]byte("key21"), []byte("value21"))
	require.NoError(err)
	err = batch.Put([]byte("key22"), []byte("value22"))
	require.NoError(err)
	err = batch.Put([]byte("key23"), []byte("value23"))
	require.NoError(err)
	err = batch.Put([]byte("key24"), []byte("value24"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)
	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key25"), []byte("value25"))
	require.NoError(err)
	err = batch.Put([]byte("key26"), []byte("value26"))
	require.NoError(err)
	err = batch.Put([]byte("key27"), []byte("value27"))
	require.NoError(err)
	err = batch.Put([]byte("key28"), []byte("value28"))
	require.NoError(err)
	err = batch.Put([]byte("key29"), []byte("value29"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key30"), []byte("value30"))
	require.NoError(err)
	err = batch.Put([]byte("key31"), []byte("value31"))
	require.NoError(err)
	err = batch.Put([]byte("key32"), []byte("value32"))
	require.NoError(err)
	err = batch.Delete([]byte("key21"))
	require.NoError(err)
	err = batch.Delete([]byte("key22"))
	require.NoError(err)
	err = batch.Write()
	require.NoError(err)

	endRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	changes, err := db.history.getValueChanges(startRoot, endRoot, nil, nil, 8)
	require.NoError(err)
	require.Equal(8, len(changes.values))
}

func TestHistoryRecord(t *testing.T) {
	require := require.New(t)

	maxHistoryLen := 3
	th := newTrieHistory(maxHistoryLen)

	changes := []*changeSummary{}
	for i := 0; i < maxHistoryLen; i++ { // Fill the history
		changes = append(changes, &changeSummary{rootID: ids.GenerateTestID()})

		th.record(changes[i])
		require.Equal(uint64(i+1), th.nextIndex)
		require.Equal(i+1, th.history.Len())
		require.Len(th.lastChanges, i+1)
		require.Contains(th.lastChanges, changes[i].rootID)
		changeAndIndex := th.lastChanges[changes[i].rootID]
		require.Equal(uint64(i), changeAndIndex.index)
		require.True(th.history.Has(changeAndIndex))
	}
	// history is [changes[0], changes[1], changes[2]]

	// Add a new change
	change3 := &changeSummary{rootID: ids.GenerateTestID()}
	th.record(change3)
	// history is [changes[1], changes[2], change3]
	require.Equal(uint64(maxHistoryLen+1), th.nextIndex)
	require.Equal(maxHistoryLen, th.history.Len())
	require.Len(th.lastChanges, maxHistoryLen)
	require.Contains(th.lastChanges, change3.rootID)
	changeAndIndex := th.lastChanges[change3.rootID]
	require.Equal(uint64(maxHistoryLen), changeAndIndex.index)
	require.True(th.history.Has(changeAndIndex))

	// Make sure the oldest change was evicted
	require.NotContains(th.lastChanges, changes[0].rootID)
	minChange, _ := th.history.Min()
	require.Equal(uint64(1), minChange.index)

	// Add another change which was the same root ID as changes[2]
	change4 := &changeSummary{rootID: changes[2].rootID}
	th.record(change4)
	// history is [changes[2], change3, change4]

	change5 := &changeSummary{rootID: ids.GenerateTestID()}
	th.record(change5)
	// history is [change3, change4, change5]

	// Make sure that even though changes[2] was evicted, we still remember
	// that the most recent change resulting in that change's root ID.
	require.Len(th.lastChanges, maxHistoryLen)
	require.Contains(th.lastChanges, changes[2].rootID)
	changeAndIndex = th.lastChanges[changes[2].rootID]
	require.Equal(uint64(maxHistoryLen+1), changeAndIndex.index)

	// Make sure [t.history] is right.
	require.Equal(maxHistoryLen, th.history.Len())
	got, _ := th.history.DeleteMin()
	require.Equal(uint64(maxHistoryLen), got.index)
	require.Equal(change3.rootID, got.rootID)
	got, _ = th.history.DeleteMin()
	require.Equal(uint64(maxHistoryLen+1), got.index)
	require.Equal(change4.rootID, got.rootID)
	got, _ = th.history.DeleteMin()
	require.Equal(uint64(maxHistoryLen+2), got.index)
	require.Equal(change5.rootID, got.rootID)
}

func TestHistoryGetChangesToRoot(t *testing.T) {
	maxHistoryLen := 3
	history := newTrieHistory(maxHistoryLen)

	changes := []*changeSummary{}
	for i := 0; i < maxHistoryLen; i++ { // Fill the history
		changes = append(changes, &changeSummary{
			rootID: ids.GenerateTestID(),
			nodes: map[path]*change[*node]{
				newPath([]byte{byte(i)}): {
					before: &node{id: ids.GenerateTestID()},
					after:  &node{id: ids.GenerateTestID()},
				},
			},
			values: map[path]*change[Maybe[[]byte]]{
				newPath([]byte{byte(i)}): {
					before: Some([]byte{byte(i)}),
					after:  Some([]byte{byte(i + 1)}),
				},
			},
		})
		history.record(changes[i])
	}

	type test struct {
		name         string
		rootID       ids.ID
		start        []byte
		end          []byte
		validateFunc func(*require.Assertions, *changeSummary)
		expectedErr  error
	}

	tests := []test{
		{
			name:        "unknown root ID",
			rootID:      ids.GenerateTestID(),
			expectedErr: ErrRootIDNotPresent,
		},
		{
			name:   "most recent change",
			rootID: changes[maxHistoryLen-1].rootID,
			validateFunc: func(require *require.Assertions, got *changeSummary) {
				require.Equal(newChangeSummary(defaultPreallocationSize), got)
			},
		},
		{
			name:   "second most recent change",
			rootID: changes[maxHistoryLen-2].rootID,
			validateFunc: func(require *require.Assertions, got *changeSummary) {
				// Ensure this is the reverse of the most recent change
				require.Len(got.nodes, 1)
				require.Len(got.values, 1)
				reversedChanges := changes[maxHistoryLen-1]
				removedKey := newPath([]byte{byte(maxHistoryLen - 1)})
				require.Equal(reversedChanges.nodes[removedKey].before, got.nodes[removedKey].after)
				require.Equal(reversedChanges.values[removedKey].before, got.values[removedKey].after)
				require.Equal(reversedChanges.values[removedKey].after, got.values[removedKey].before)
			},
		},
		{
			name:   "third most recent change",
			rootID: changes[maxHistoryLen-3].rootID,
			validateFunc: func(require *require.Assertions, got *changeSummary) {
				require.Len(got.nodes, 2)
				require.Len(got.values, 2)
				reversedChanges1 := changes[maxHistoryLen-1]
				removedKey1 := newPath([]byte{byte(maxHistoryLen - 1)})
				require.Equal(reversedChanges1.nodes[removedKey1].before, got.nodes[removedKey1].after)
				require.Equal(reversedChanges1.values[removedKey1].before, got.values[removedKey1].after)
				require.Equal(reversedChanges1.values[removedKey1].after, got.values[removedKey1].before)
				reversedChanges2 := changes[maxHistoryLen-2]
				removedKey2 := newPath([]byte{byte(maxHistoryLen - 2)})
				require.Equal(reversedChanges2.nodes[removedKey2].before, got.nodes[removedKey2].after)
				require.Equal(reversedChanges2.values[removedKey2].before, got.values[removedKey2].after)
				require.Equal(reversedChanges2.values[removedKey2].after, got.values[removedKey2].before)
			},
		},
		{
			name:   "third most recent change with start filter",
			rootID: changes[maxHistoryLen-3].rootID,
			start:  []byte{byte(maxHistoryLen - 1)}, // Omit values from second most recent change
			validateFunc: func(require *require.Assertions, got *changeSummary) {
				require.Len(got.nodes, 2)
				require.Len(got.values, 1)
				reversedChanges1 := changes[maxHistoryLen-1]
				removedKey1 := newPath([]byte{byte(maxHistoryLen - 1)})
				require.Equal(reversedChanges1.nodes[removedKey1].before, got.nodes[removedKey1].after)
				require.Equal(reversedChanges1.values[removedKey1].before, got.values[removedKey1].after)
				require.Equal(reversedChanges1.values[removedKey1].after, got.values[removedKey1].before)
				reversedChanges2 := changes[maxHistoryLen-2]
				removedKey2 := newPath([]byte{byte(maxHistoryLen - 2)})
				require.Equal(reversedChanges2.nodes[removedKey2].before, got.nodes[removedKey2].after)
			},
		},
		{
			name:   "third most recent change with end filter",
			rootID: changes[maxHistoryLen-3].rootID,
			end:    []byte{byte(maxHistoryLen - 2)}, // Omit values from most recent change
			validateFunc: func(require *require.Assertions, got *changeSummary) {
				require.Len(got.nodes, 2)
				require.Len(got.values, 1)
				reversedChanges1 := changes[maxHistoryLen-1]
				removedKey1 := newPath([]byte{byte(maxHistoryLen - 1)})
				require.Equal(reversedChanges1.nodes[removedKey1].before, got.nodes[removedKey1].after)
				reversedChanges2 := changes[maxHistoryLen-2]
				removedKey2 := newPath([]byte{byte(maxHistoryLen - 2)})
				require.Equal(reversedChanges2.nodes[removedKey2].before, got.nodes[removedKey2].after)
				require.Equal(reversedChanges2.values[removedKey2].before, got.values[removedKey2].after)
				require.Equal(reversedChanges2.values[removedKey2].after, got.values[removedKey2].before)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			got, err := history.getChangesToGetToRoot(tt.rootID, tt.start, tt.end)
			require.ErrorIs(err, tt.expectedErr)
			if err != nil {
				return
			}
			tt.validateFunc(require, got)
		})
	}
}
