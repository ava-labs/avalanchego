// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

func Test_History_Simple(t *testing.T) {
	require := require.New(t)

	db, err := newDB(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
	)
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value")))
	require.NoError(batch.Write())

	val, err := db.Get([]byte("key"))
	require.NoError(err)
	require.Equal([]byte("value"), val)

	origProof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.rootID
	require.NoError(origProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value0")))
	require.NoError(batch.Write())
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value1")))
	require.NoError(batch.Put([]byte("key8"), []byte("value8")))
	require.NoError(batch.Write())
	newProof, err = db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("k"), []byte("v")))
	require.NoError(batch.Write())
	newProof, err = db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Delete([]byte("k")))
	require.NoError(batch.Delete([]byte("ke")))
	require.NoError(batch.Delete([]byte("key")))
	require.NoError(batch.Delete([]byte("key1")))
	require.NoError(batch.Put([]byte("key2"), []byte("value2")))
	require.NoError(batch.Delete([]byte("key3")))
	require.NoError(batch.Delete([]byte("key4")))
	require.NoError(batch.Delete([]byte("key5")))
	require.NoError(batch.Delete([]byte("key8")))
	require.NoError(batch.Write())
	newProof, err = db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))
}

func Test_History_Large(t *testing.T) {
	require := require.New(t)

	numIters := 250

	for i := 1; i < 5; i++ {
		config := newDefaultConfig()
		// History must be large enough to get the change proof
		// after this loop.
		config.HistoryLength = uint(numIters)
		db, err := New(
			context.Background(),
			memdb.New(),
			config,
		)
		require.NoError(err)
		roots := []ids.ID{}

		now := time.Now().UnixNano()
		t.Logf("seed for iter %d: %d", i, now)
		r := rand.New(rand.NewSource(now)) // #nosec G404
		// make sure they stay in sync
		for x := 0; x < numIters; x++ {
			batch := db.NewBatch()
			addkey := make([]byte, r.Intn(50))
			_, err := r.Read(addkey)
			require.NoError(err)
			val := make([]byte, r.Intn(50))
			_, err = r.Read(val)
			require.NoError(err)

			require.NoError(batch.Put(addkey, val))

			addNilkey := make([]byte, r.Intn(50))
			_, err = r.Read(addNilkey)
			require.NoError(err)
			require.NoError(batch.Put(addNilkey, nil))

			deleteKeyStart := make([]byte, r.Intn(50))
			_, err = r.Read(deleteKeyStart)
			require.NoError(err)

			it := db.NewIteratorWithStart(deleteKeyStart)
			if it.Next() {
				require.NoError(batch.Delete(it.Key()))
			}
			require.NoError(it.Error())
			it.Release()

			require.NoError(batch.Write())
			root, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)
			roots = append(roots, root)
		}

		for i := 0; i < numIters; i += numIters / 10 {
			proof, err := db.GetRangeProofAtRoot(context.Background(), roots[i], maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 10)
			require.NoError(err)
			require.NotNil(proof)

			require.NoError(proof.Verify(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), roots[i], BranchFactorToTokenSize[config.BranchFactor], config.Hasher))
		}
	}
}

func Test_History_Bad_GetValueChanges_Input(t *testing.T) {
	require := require.New(t)

	config := newDefaultConfig()
	config.HistoryLength = 5

	db, err := newDB(
		context.Background(),
		memdb.New(),
		config,
	)
	require.NoError(err)

	// Do 5 puts (i.e. the history length)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value")))
	require.NoError(batch.Write())

	root1 := db.getMerkleRoot()

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value0")))
	require.NoError(batch.Write())

	root2 := db.getMerkleRoot()

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value0")))
	require.NoError(batch.Write())

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value1")))
	require.NoError(batch.Write())

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key2"), []byte("value3")))
	require.NoError(batch.Write())

	root3 := db.getMerkleRoot()

	// ensure these start as valid calls
	_, err = db.history.getValueChanges(root1, root3, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1)
	require.NoError(err)
	_, err = db.history.getValueChanges(root2, root3, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1)
	require.NoError(err)

	_, err = db.history.getValueChanges(root2, root3, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), -1)
	require.ErrorIs(err, ErrInvalidMaxLength)

	_, err = db.history.getValueChanges(root3, root2, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1)
	require.ErrorIs(err, ErrInsufficientHistory)

	// Cause root1 to be removed from the history
	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key2"), []byte("value4")))
	require.NoError(batch.Write())

	_, err = db.history.getValueChanges(root1, root3, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 1)
	require.ErrorIs(err, ErrInsufficientHistory)

	// same start/end roots should yield an empty changelist
	changes, err := db.history.getValueChanges(root3, root3, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 10)
	require.NoError(err)
	require.Empty(changes)
}

func Test_History_Trigger_History_Queue_Looping(t *testing.T) {
	require := require.New(t)

	config := newDefaultConfig()
	config.HistoryLength = 2

	db, err := newDB(
		context.Background(),
		memdb.New(),
		config,
	)
	require.NoError(err)

	// Do 2 puts (i.e. the history length)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value")))
	require.NoError(batch.Write())
	origRootID := db.getMerkleRoot()

	origProof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(origProof)
	require.NoError(origProof.Verify(
		context.Background(),
		maybe.Some([]byte("k")),
		maybe.Some([]byte("key3")),
		origRootID,
		db.tokenSize,
		db.hasher,
	))

	// write a new value into the db, now there should be 2 roots in the history
	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value0")))
	require.NoError(batch.Write())

	// ensure that previous root is still present and generates a valid proof
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(
		context.Background(),
		maybe.Some([]byte("k")),
		maybe.Some([]byte("key3")),
		origRootID,
		db.tokenSize,
		db.hasher,
	))

	// trigger a new root to be added to the history, which should cause rollover since there can only be 2
	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value1")))
	require.NoError(batch.Write())

	// proof from first root shouldn't be generatable since it should have been removed from the history
	_, err = db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.ErrorIs(err, ErrInsufficientHistory)
}

func Test_History_Values_Lookup_Over_Queue_Break(t *testing.T) {
	require := require.New(t)

	config := newDefaultConfig()
	config.HistoryLength = 4
	db, err := newDB(
		context.Background(),
		memdb.New(),
		config,
	)
	require.NoError(err)

	// Do 4 puts (i.e. the history length)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value")))
	require.NoError(batch.Write())

	// write a new value into the db
	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value0")))
	require.NoError(batch.Write())

	startRoot := db.getMerkleRoot()

	// write a new value into the db
	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value0")))
	require.NoError(batch.Write())

	// write a new value into the db that overwrites key1
	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value1")))
	require.NoError(batch.Write())

	// trigger a new root to be added to the history, which should cause rollover since there can only be 3
	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key2"), []byte("value3")))
	require.NoError(batch.Write())

	endRoot := db.getMerkleRoot()

	// changes should still be collectable even though the history has had to loop due to hitting max size
	changes, err := db.history.getValueChanges(startRoot, endRoot, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 10)
	require.NoError(err)

	require.Equal([]*keyChange{
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value1")),
			},
			key: ToKey([]byte("key1")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value3")),
			},
			key: ToKey([]byte("key2")),
		},
	}, changes)
}

func Test_History_RepeatedRoot(t *testing.T) {
	require := require.New(t)

	db, err := newDB(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
	)
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value1")))
	require.NoError(batch.Put([]byte("key2"), []byte("value2")))
	require.NoError(batch.Put([]byte("key3"), []byte("value3")))
	require.NoError(batch.Write())

	origProof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.rootID
	require.NoError(origProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("other")))
	require.NoError(batch.Put([]byte("key2"), []byte("other")))
	require.NoError(batch.Put([]byte("key3"), []byte("other")))
	require.NoError(batch.Write())
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	// revert state to be the same as in orig proof
	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value1")))
	require.NoError(batch.Put([]byte("key2"), []byte("value2")))
	require.NoError(batch.Put([]byte("key3"), []byte("value3")))
	require.NoError(batch.Write())

	newProof, err = db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))
}

func Test_History_ExcessDeletes(t *testing.T) {
	require := require.New(t)

	db, err := newDB(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
	)
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value")))
	require.NoError(batch.Write())

	origProof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.rootID
	require.NoError(origProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Delete([]byte("key1")))
	require.NoError(batch.Delete([]byte("key2")))
	require.NoError(batch.Delete([]byte("key3")))
	require.NoError(batch.Delete([]byte("key4")))
	require.NoError(batch.Delete([]byte("key5")))
	require.NoError(batch.Write())
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))
}

func Test_History_DontIncludeAllNodes(t *testing.T) {
	require := require.New(t)

	db, err := newDB(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
	)
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value")))
	require.NoError(batch.Write())

	origProof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.rootID
	require.NoError(origProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("z"), []byte("z")))
	require.NoError(batch.Write())
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))
}

func Test_History_Branching2Nodes(t *testing.T) {
	require := require.New(t)

	db, err := newDB(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
	)
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value")))
	require.NoError(batch.Write())

	origProof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.rootID
	require.NoError(origProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("k"), []byte("v")))
	require.NoError(batch.Write())
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))
}

func Test_History_Branching3Nodes(t *testing.T) {
	require := require.New(t)

	db, err := newDB(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
	)
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key123"), []byte("value123")))
	require.NoError(batch.Write())

	origProof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(origProof)
	origRootID := db.rootID
	require.NoError(origProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key321"), []byte("value321")))
	require.NoError(batch.Write())
	newProof, err := db.GetRangeProofAtRoot(context.Background(), origRootID, maybe.Some([]byte("k")), maybe.Some([]byte("key3")), 10)
	require.NoError(err)
	require.NotNil(newProof)
	require.NoError(newProof.Verify(context.Background(), maybe.Some([]byte("k")), maybe.Some([]byte("key3")), origRootID, db.tokenSize, db.hasher))
}

func Test_History_MaxLength(t *testing.T) {
	require := require.New(t)

	config := newDefaultConfig()
	config.HistoryLength = 2
	db, err := newDB(
		context.Background(),
		memdb.New(),
		config,
	)
	require.NoError(err)

	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key"), []byte("value")))
	require.NoError(batch.Write())

	oldRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("k"), []byte("v")))
	require.NoError(batch.Write())

	require.Contains(db.history.lastChanges, oldRoot)

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("k1"), []byte("v2"))) // Overwrites oldest element in history
	require.NoError(batch.Write())

	require.NotContains(db.history.lastChanges, oldRoot)
}

func Test_Change_List(t *testing.T) {
	require := require.New(t)

	db, err := newDB(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
	)
	require.NoError(err)

	emptyRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key20"), []byte("value20")))
	require.NoError(batch.Put([]byte("key21"), []byte("value21")))
	require.NoError(batch.Put([]byte("key22"), []byte("value22")))
	require.NoError(batch.Put([]byte("key23"), []byte("value23")))
	require.NoError(batch.Put([]byte("key24"), []byte("value24")))
	require.NoError(batch.Write())
	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	changes, err := db.history.getValueChanges(emptyRoot, startRoot, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 100)
	require.NoError(err)
	require.Equal([]*keyChange{
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value20")),
			},
			key: ToKey([]byte("key20")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value21")),
			},
			key: ToKey([]byte("key21")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value22")),
			},
			key: ToKey([]byte("key22")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value23")),
			},
			key: ToKey([]byte("key23")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value24")),
			},
			key: ToKey([]byte("key24")),
		},
	}, changes)

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key25"), []byte("value25")))
	require.NoError(batch.Put([]byte("key26"), []byte("value26")))
	require.NoError(batch.Put([]byte("key27"), []byte("value27")))
	require.NoError(batch.Put([]byte("key28"), []byte("value28")))
	require.NoError(batch.Put([]byte("key29"), []byte("value29")))
	require.NoError(batch.Write())

	endRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	changes, err = db.history.getValueChanges(startRoot, endRoot, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 100)
	require.NoError(err)
	require.Equal([]*keyChange{
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value25")),
			},
			key: ToKey([]byte("key25")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value26")),
			},
			key: ToKey([]byte("key26")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value27")),
			},
			key: ToKey([]byte("key27")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value28")),
			},
			key: ToKey([]byte("key28")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value29")),
			},
			key: ToKey([]byte("key29")),
		},
	}, changes)

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key30"), []byte{}))
	require.NoError(batch.Put([]byte("key31"), []byte("value31")))
	require.NoError(batch.Put([]byte("key32"), []byte("value32")))
	require.NoError(batch.Delete([]byte("key21")))
	require.NoError(batch.Delete([]byte("key22")))
	require.NoError(batch.Put([]byte("key24"), []byte("value24new")))
	require.NoError(batch.Write())

	endRoot, err = db.GetMerkleRoot(context.Background())
	require.NoError(err)

	changes, err = db.history.getValueChanges(startRoot, endRoot, maybe.Some[[]byte]([]byte("key22")), maybe.Some[[]byte]([]byte("key31")), 8)
	require.NoError(err)

	require.Equal([]*keyChange{
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Some([]byte("value22")),
				after:  maybe.Nothing[[]byte](),
			},
			key: ToKey([]byte("key22")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Some([]byte("value24")),
				after:  maybe.Some([]byte("value24new")),
			},
			key: ToKey([]byte("key24")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value25")),
			},
			key: ToKey([]byte("key25")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value26")),
			},
			key: ToKey([]byte("key26")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value27")),
			},
			key: ToKey([]byte("key27")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value28")),
			},
			key: ToKey([]byte("key28")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte("value29")),
			},
			key: ToKey([]byte("key29")),
		},
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Nothing[[]byte](),
				after:  maybe.Some([]byte{}),
			},
			key: ToKey([]byte("key30")),
		},
	}, changes)
}

func TestHistoryRecord(t *testing.T) {
	require := require.New(t)

	maxHistoryLen := 3
	th := newTrieHistory(maxHistoryLen)

	changes := []*changeSummary{}
	for i := 0; i < maxHistoryLen; i++ { // Fill the history
		changes = append(changes, &changeSummary{rootID: ids.GenerateTestID()})

		th.record(changes[i])
		require.Equal(uint64(i+1), th.nextInsertNumber)
		require.Equal(i+1, th.history.Len())
		require.Len(th.lastChanges, i+1)
		require.Contains(th.lastChanges, changes[i].rootID)
		changeAndIndex := th.lastChanges[changes[i].rootID]
		require.Equal(uint64(i), changeAndIndex.insertNumber)
		got, ok := th.history.Index(int(changeAndIndex.insertNumber))
		require.True(ok)
		require.Equal(changes[i], got.changeSummary)
	}
	// history is [changes[0], changes[1], changes[2]]

	// Add a new change
	change3 := &changeSummary{rootID: ids.GenerateTestID()}
	th.record(change3)
	// history is [changes[1], changes[2], change3]
	require.Equal(uint64(maxHistoryLen+1), th.nextInsertNumber)
	require.Equal(maxHistoryLen, th.history.Len())
	require.Len(th.lastChanges, maxHistoryLen)
	require.Contains(th.lastChanges, change3.rootID)
	changeAndIndex := th.lastChanges[change3.rootID]
	require.Equal(uint64(maxHistoryLen), changeAndIndex.insertNumber)
	got, ok := th.history.PeekRight()
	require.True(ok)
	require.Equal(change3, got.changeSummary)

	// // Make sure the oldest change was evicted
	require.NotContains(th.lastChanges, changes[0].rootID)
	oldestChange, ok := th.history.PeekLeft()
	require.True(ok)
	require.Equal(uint64(1), oldestChange.insertNumber)

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
	require.Equal(uint64(maxHistoryLen+1), changeAndIndex.insertNumber)

	// Make sure [t.history] is right.
	require.Equal(maxHistoryLen, th.history.Len())
	got, ok = th.history.PopLeft()
	require.True(ok)
	require.Equal(uint64(maxHistoryLen), got.insertNumber)
	require.Equal(change3.rootID, got.rootID)
	got, ok = th.history.PopLeft()
	require.True(ok)
	require.Equal(uint64(maxHistoryLen+1), got.insertNumber)
	require.Equal(change4.rootID, got.rootID)
	got, ok = th.history.PopLeft()
	require.True(ok)
	require.Equal(uint64(maxHistoryLen+2), got.insertNumber)
	require.Equal(change5.rootID, got.rootID)
}

func TestHistoryGetChangesToRootKeyRollback(t *testing.T) {
	keyChangesSets := [][]*keyChange{
		{
			// First changes
			{
				change: &change[maybe.Maybe[[]byte]]{
					before: maybe.Some([]byte{}),
					after:  maybe.Some([]byte("value1a")),
				},
				key: ToKey([]byte("key1")),
			},
			{
				change: &change[maybe.Maybe[[]byte]]{
					before: maybe.Some([]byte{}),
					after:  maybe.Some([]byte("value2a")),
				},
				key: ToKey([]byte("key2")),
			},
		},
		{
			// Second changes
			{
				change: &change[maybe.Maybe[[]byte]]{
					before: maybe.Some([]byte("value1a")),
					after:  maybe.Some([]byte("value1b")),
				},
				key: ToKey([]byte("key1")),
			},
			{
				change: &change[maybe.Maybe[[]byte]]{
					before: maybe.Some([]byte("value2a")),
					after:  maybe.Some([]byte("value2b")),
				},
				key: ToKey([]byte("key2")),
			},
		},
		{
			// Third changes
			{
				change: &change[maybe.Maybe[[]byte]]{
					before: maybe.Some([]byte("value1b")),
					after:  maybe.Some([]byte("value1a")),
				},
				key: ToKey([]byte("key1")),
			},
		},
	}

	maxHistoryLen := len(keyChangesSets)
	history := newTrieHistory(maxHistoryLen)

	changeSummaries := make([]*changeSummary, maxHistoryLen)
	for i := 0; i < maxHistoryLen; i++ {
		changeSummary := &changeSummary{
			rootID: ids.GenerateTestID(),
			rootChange: change[maybe.Maybe[*node]]{
				before: maybe.Some(&node{}),
			},
			nodes:            map[Key]*change[*node]{},
			values:           map[Key]*keyChange{},
			sortedKeyChanges: keyChangesSets[i],
		}

		for _, kc := range keyChangesSets[i] {
			changeSummary.values[kc.key] = kc
		}

		history.record(changeSummary)
		changeSummaries[i] = changeSummary
	}

	combinedChangeSummary, err := history.getChangesToGetToRoot(changeSummaries[0].rootID, maybe.Nothing[[]byte](), maybe.Nothing[[]byte]())
	require.NoError(t, err)

	require.Len(t, combinedChangeSummary.values, 1)
	require.Len(t, combinedChangeSummary.sortedKeyChanges, 1)

	// key2 => value2a => value 2b
	require.Equal(t, []*keyChange{
		{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Some([]byte("value2b")),
				after:  maybe.Some([]byte("value2a")),
			},
			key: ToKey([]byte("key2")),
		},
	}, combinedChangeSummary.sortedKeyChanges)
}

func TestHistoryGetChangesToRootState(t *testing.T) {
	var (
		historyLen        = 3
		changesPerHistory = 5

		// unordered keys, so we can verify the [sortedKeys] field from [changeSummary].
		// the length should be [historyLen] * [changesPerHistory]
		keys = []byte{246, 19, 235, 233, 224, 122, 28, 22, 131, 77, 64, 207, 142, 103, 193}
	)

	history := newTrieHistory(historyLen)

	changeSummaries := make([]*changeSummary, historyLen)
	for i := 0; i < historyLen; i++ {
		keyChanges := make([]*keyChange, changesPerHistory)

		for j := 0; j < changesPerHistory; j++ {
			key := ToKey([]byte{keys[j+i*changesPerHistory]})
			before := []byte{keys[j+i*changesPerHistory]}
			after := []byte{keys[j+i*changesPerHistory] + 1}

			keyChanges[j] = &keyChange{
				change: &change[maybe.Maybe[[]byte]]{
					before: maybe.Some(before),
					after:  maybe.Some(after),
				},
				key: key,
			}
		}

		changeSummary := &changeSummary{
			rootID: ids.GenerateTestID(),
			rootChange: change[maybe.Maybe[*node]]{
				before: maybe.Some(&node{}),
			},
			nodes:            map[Key]*change[*node]{},
			values:           map[Key]*keyChange{},
			sortedKeyChanges: keyChanges,
		}

		for _, kc := range keyChanges {
			changeSummary.values[kc.key] = kc
			changeSummary.nodes[kc.key] = &change[*node]{before: &node{}, after: &node{}}
		}

		history.record(changeSummary)
		changeSummaries[i] = changeSummary
	}

	combinedChangeSummary, err := history.getChangesToGetToRoot(changeSummaries[0].rootID, maybe.Nothing[[]byte](), maybe.Nothing[[]byte]())
	require.NoError(t, err)

	require.Len(t, combinedChangeSummary.values, changesPerHistory*(historyLen-1))
	require.Len(t, combinedChangeSummary.nodes, changesPerHistory*(historyLen-1))
	require.Len(t, combinedChangeSummary.sortedKeyChanges, changesPerHistory*(historyLen-1))

	for _, kc := range combinedChangeSummary.values {
		require.Equal(t, []byte(kc.key.value), kc.after.Value())
		require.Equal(t, []byte{([]byte(kc.key.value))[0] + 1}, kc.before.Value())
	}

	expectedSortedKeys := maps.Keys(combinedChangeSummary.values)
	slices.SortFunc(expectedSortedKeys, func(a, b Key) int {
		return a.Compare(b)
	})

	actualSortedKeys := make([]Key, len(combinedChangeSummary.sortedKeyChanges))
	for i, kc := range combinedChangeSummary.sortedKeyChanges {
		actualSortedKeys[i] = kc.key
	}

	require.Equal(t, expectedSortedKeys, actualSortedKeys)
}

func TestHistoryGetChangesToRoot(t *testing.T) {
	maxHistoryLen := 3
	history := newTrieHistory(maxHistoryLen)

	changes := []*changeSummary{}
	for i := 0; i < maxHistoryLen; i++ { // Fill the history
		kc := &keyChange{
			change: &change[maybe.Maybe[[]byte]]{
				before: maybe.Some([]byte{byte(i)}),
				after:  maybe.Some([]byte{byte(i + 1)}),
			},
			key: ToKey([]byte{byte(i)}),
		}

		changes = append(changes, &changeSummary{
			rootID: ids.GenerateTestID(),
			rootChange: change[maybe.Maybe[*node]]{
				before: maybe.Some(&node{}),
			},
			nodes: map[Key]*change[*node]{
				ToKey([]byte{byte(i)}): {
					before: &node{},
					after:  &node{},
				},
			},
			values:           map[Key]*keyChange{kc.key: kc},
			sortedKeyChanges: []*keyChange{kc},
		})
		history.record(changes[i])
	}

	type test struct {
		name         string
		rootID       ids.ID
		start        maybe.Maybe[[]byte]
		end          maybe.Maybe[[]byte]
		validateFunc func(*require.Assertions, *changeSummary)
		expectedErr  error
	}

	tests := []test{
		{
			name:        "unknown root ID",
			rootID:      ids.GenerateTestID(),
			expectedErr: ErrInsufficientHistory,
		},
		{
			name:   "most recent change",
			rootID: changes[maxHistoryLen-1].rootID,
			validateFunc: func(require *require.Assertions, got *changeSummary) {
				expected := newChangeSummary(defaultPreallocationSize)
				require.Equal(expected, got)
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
				removedKey := ToKey([]byte{byte(maxHistoryLen - 1)})
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
				removedKey1 := ToKey([]byte{byte(maxHistoryLen - 1)})
				require.Equal(reversedChanges1.nodes[removedKey1].before, got.nodes[removedKey1].after)
				require.Equal(reversedChanges1.values[removedKey1].before, got.values[removedKey1].after)
				require.Equal(reversedChanges1.values[removedKey1].after, got.values[removedKey1].before)
				reversedChanges2 := changes[maxHistoryLen-2]
				removedKey2 := ToKey([]byte{byte(maxHistoryLen - 2)})
				require.Equal(reversedChanges2.nodes[removedKey2].before, got.nodes[removedKey2].after)
				require.Equal(reversedChanges2.values[removedKey2].before, got.values[removedKey2].after)
				require.Equal(reversedChanges2.values[removedKey2].after, got.values[removedKey2].before)
			},
		},
		{
			name:   "third most recent change with start filter",
			rootID: changes[maxHistoryLen-3].rootID,
			start:  maybe.Some([]byte{byte(maxHistoryLen - 1)}), // Omit values from second most recent change
			validateFunc: func(require *require.Assertions, got *changeSummary) {
				require.Len(got.nodes, 2)
				require.Len(got.values, 1)
				reversedChanges1 := changes[maxHistoryLen-1]
				removedKey1 := ToKey([]byte{byte(maxHistoryLen - 1)})
				require.Equal(reversedChanges1.nodes[removedKey1].before, got.nodes[removedKey1].after)
				require.Equal(reversedChanges1.values[removedKey1].before, got.values[removedKey1].after)
				require.Equal(reversedChanges1.values[removedKey1].after, got.values[removedKey1].before)
				reversedChanges2 := changes[maxHistoryLen-2]
				removedKey2 := ToKey([]byte{byte(maxHistoryLen - 2)})
				require.Equal(reversedChanges2.nodes[removedKey2].before, got.nodes[removedKey2].after)
			},
		},
		{
			name:   "third most recent change with end filter",
			rootID: changes[maxHistoryLen-3].rootID,
			end:    maybe.Some([]byte{byte(maxHistoryLen - 2)}), // Omit values from most recent change
			validateFunc: func(require *require.Assertions, got *changeSummary) {
				require.Len(got.nodes, 2)
				require.Len(got.values, 1)
				reversedChanges1 := changes[maxHistoryLen-1]
				removedKey1 := ToKey([]byte{byte(maxHistoryLen - 1)})
				require.Equal(reversedChanges1.nodes[removedKey1].before, got.nodes[removedKey1].after)
				reversedChanges2 := changes[maxHistoryLen-2]
				removedKey2 := ToKey([]byte{byte(maxHistoryLen - 2)})
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
			if tt.expectedErr != nil {
				return
			}
			tt.validateFunc(require, got)
		})
	}
}
