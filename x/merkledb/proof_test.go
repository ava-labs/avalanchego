// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func Test_Proof_Empty(t *testing.T) {
	proof := &Proof{}
	err := proof.Verify(context.Background(), ids.Empty, 4, DefaultHasher)
	require.ErrorIs(t, err, ErrEmptyProof)
}

func Test_Proof_Simple(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	ctx := context.Background()
	require.NoError(db.PutContext(ctx, []byte{}, []byte{1}))
	require.NoError(db.PutContext(ctx, []byte{0}, []byte{2}))

	expectedRoot, err := db.GetMerkleRoot(ctx)
	require.NoError(err)

	proof, err := db.GetProof(ctx, []byte{})
	require.NoError(err)

	require.NoError(proof.Verify(ctx, expectedRoot, db.tokenSize, db.hasher))
}

func Test_Proof_Verify_Bad_Data(t *testing.T) {
	type test struct {
		name        string
		malform     func(proof *Proof)
		expectedErr error
	}

	tests := []test{
		{
			name:        "happyPath",
			malform:     func(*Proof) {},
			expectedErr: nil,
		},
		{
			name: "empty",
			malform: func(proof *Proof) {
				proof.Path = nil
			},
			expectedErr: ErrEmptyProof,
		},
		{
			name: "odd length key path with value",
			malform: func(proof *Proof) {
				proof.Path[0].ValueOrHash = maybe.Some([]byte{1, 2})
			},
			expectedErr: ErrPartialByteLengthWithValue,
		},
		{
			name: "last proof node has missing value",
			malform: func(proof *Proof) {
				proof.Path[len(proof.Path)-1].ValueOrHash = maybe.Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "missing value on proof",
			malform: func(proof *Proof) {
				proof.Value = maybe.Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "mismatched value on proof",
			malform: func(proof *Proof) {
				proof.Value = maybe.Some([]byte{10})
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "value of exclusion proof",
			malform: func(proof *Proof) {
				// remove the value node to make it look like it is an exclusion proof
				proof.Path = proof.Path[:len(proof.Path)-1]
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			db, err := getBasicDB()
			require.NoError(err)

			writeBasicBatch(t, db)

			proof, err := db.GetProof(context.Background(), []byte{2})
			require.NoError(err)
			require.NotNil(proof)

			tt.malform(proof)

			err = proof.Verify(context.Background(), db.getMerkleRoot(), db.tokenSize, db.hasher)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func Test_Proof_ValueOrHashMatches(t *testing.T) {
	require := require.New(t)

	require.True(valueOrHashMatches(SHA256Hasher, maybe.Some([]byte{0}), maybe.Some([]byte{0})))
	require.False(valueOrHashMatches(SHA256Hasher, maybe.Nothing[[]byte](), maybe.Some(hashing.ComputeHash256([]byte{0}))))
	require.True(valueOrHashMatches(SHA256Hasher, maybe.Nothing[[]byte](), maybe.Nothing[[]byte]()))

	require.False(valueOrHashMatches(SHA256Hasher, maybe.Some([]byte{0}), maybe.Nothing[[]byte]()))
	require.False(valueOrHashMatches(SHA256Hasher, maybe.Nothing[[]byte](), maybe.Some([]byte{0})))
	require.False(valueOrHashMatches(SHA256Hasher, maybe.Nothing[[]byte](), maybe.Some(hashing.ComputeHash256([]byte{1}))))
	require.False(valueOrHashMatches(SHA256Hasher, maybe.Some(hashing.ComputeHash256([]byte{0})), maybe.Nothing[[]byte]()))
}

func Test_RangeProof_Extra_Value(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	writeBasicBatch(t, db)

	val, err := db.Get([]byte{2})
	require.NoError(err)
	require.Equal([]byte{2}, val)

	proof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte{1}), maybe.Some([]byte{5, 5}), 10)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(proof.Verify(
		context.Background(),
		maybe.Some([]byte{1}),
		maybe.Some([]byte{5, 5}),
		db.rootID,
		db.tokenSize,
		db.hasher,
	))

	proof.KeyValues = append(proof.KeyValues, KeyValue{Key: []byte{5}, Value: []byte{5}})

	err = proof.Verify(
		context.Background(),
		maybe.Some([]byte{1}),
		maybe.Some([]byte{5, 5}),
		db.rootID,
		db.tokenSize,
		db.hasher,
	)
	require.ErrorIs(err, ErrInvalidProof)
}

func Test_RangeProof_Verify_Bad_Data(t *testing.T) {
	type test struct {
		name        string
		malform     func(proof *RangeProof)
		expectedErr error
	}

	tests := []test{
		{
			name:        "happyPath",
			malform:     func(*RangeProof) {},
			expectedErr: nil,
		},
		{
			name: "empty",
			malform: func(proof *RangeProof) {
				proof.KeyValues = nil
				proof.StartProof = nil
				proof.EndProof = nil
			},
			expectedErr: ErrEmptyProof,
		},
		{
			name: "StartProof: last proof node has missing value",
			malform: func(proof *RangeProof) {
				proof.StartProof[len(proof.StartProof)-1].ValueOrHash = maybe.Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "EndProof: odd length key path with value",
			malform: func(proof *RangeProof) {
				proof.EndProof[0].ValueOrHash = maybe.Some([]byte{1, 2})
			},
			expectedErr: ErrPartialByteLengthWithValue,
		},
		{
			name: "EndProof: last proof node has missing value",
			malform: func(proof *RangeProof) {
				proof.EndProof[len(proof.EndProof)-1].ValueOrHash = maybe.Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "missing key/value",
			malform: func(proof *RangeProof) {
				proof.KeyValues = proof.KeyValues[1:]
			},
			expectedErr: ErrProofNodeHasUnincludedValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			db, err := getBasicDB()
			require.NoError(err)
			writeBasicBatch(t, db)

			proof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte{2}), maybe.Some([]byte{3, 0}), 50)
			require.NoError(err)
			require.NotNil(proof)

			tt.malform(proof)

			err = proof.Verify(context.Background(), maybe.Some([]byte{2}), maybe.Some([]byte{3, 0}), db.getMerkleRoot(), db.tokenSize, db.hasher)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func Test_RangeProof_MaxLength(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie, err := dbTrie.NewView(context.Background(), ViewChanges{})
	require.NoError(err)

	_, err = trie.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), -1)
	require.ErrorIs(err, ErrInvalidMaxLength)

	_, err = trie.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 0)
	require.ErrorIs(err, ErrInvalidMaxLength)
}

func Test_Proof(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie, err := dbTrie.NewView(
		context.Background(),
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte("key"), Value: []byte("value")},
				{Key: []byte("key0"), Value: []byte("value0")},
				{Key: []byte("key1"), Value: []byte("value1")},
				{Key: []byte("key2"), Value: []byte("value2")},
				{Key: []byte("key3"), Value: []byte("value3")},
				{Key: []byte("key4"), Value: []byte("value4")},
			},
		},
	)
	require.NoError(err)

	_, err = trie.GetMerkleRoot(context.Background())
	require.NoError(err)
	proof, err := trie.GetProof(context.Background(), []byte("key1"))
	require.NoError(err)
	require.NotNil(proof)

	require.Len(proof.Path, 3)

	require.Equal(ToKey([]byte("key")), proof.Path[0].Key)
	require.Equal(maybe.Some([]byte("value")), proof.Path[0].ValueOrHash)
	require.Equal(ToKey([]byte("key0")).Take(28), proof.Path[1].Key)
	require.True(proof.Path[1].ValueOrHash.IsNothing()) // intermediate node
	require.Equal(ToKey([]byte("key1")), proof.Path[2].Key)
	require.Equal(maybe.Some([]byte("value1")), proof.Path[2].ValueOrHash)

	expectedRootID, err := trie.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.NoError(proof.Verify(context.Background(), expectedRootID, dbTrie.tokenSize, dbTrie.hasher))

	proof.Path[0].Key = ToKey([]byte("key1"))
	err = proof.Verify(context.Background(), expectedRootID, dbTrie.tokenSize, dbTrie.hasher)
	require.ErrorIs(err, ErrProofNodeNotForKey)
}

func Test_RangeProof_Syntactic_Verify(t *testing.T) {
	type test struct {
		name        string
		start       maybe.Maybe[[]byte]
		end         maybe.Maybe[[]byte]
		proof       *RangeProof
		expectedErr error
	}

	tests := []test{
		{
			name:        "start > end",
			start:       maybe.Some([]byte{1}),
			end:         maybe.Some([]byte{0}),
			proof:       &RangeProof{},
			expectedErr: ErrStartAfterEnd,
		},
		{
			name:        "empty",
			start:       maybe.Some([]byte{1}),
			end:         maybe.Nothing[[]byte](),
			proof:       &RangeProof{},
			expectedErr: ErrEmptyProof,
		},
		{
			name:  "unexpected end proof",
			start: maybe.Some([]byte{1}),
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				StartProof: []ProofNode{{}},
				EndProof:   []ProofNode{{}},
			},
			expectedErr: ErrUnexpectedEndProof,
		},
		{
			name:  "no end proof; has end bound",
			start: maybe.Some([]byte{1}),
			end:   maybe.Some([]byte{1}),
			proof: &RangeProof{
				StartProof: []ProofNode{{}},
			},
			expectedErr: ErrNoEndProof,
		},
		{
			name:  "no end proof; has key-values",
			start: maybe.Some([]byte{1}),
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{{}},
			},
			expectedErr: ErrNoEndProof,
		},
		{
			name:  "unsorted key values",
			start: maybe.Some([]byte{1}),
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1}, Value: []byte{1}},
					{Key: []byte{0}, Value: []byte{0}},
				},
				EndProof: []ProofNode{{Key: Key{}}},
			},
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name:  "key lower than start",
			start: maybe.Some([]byte{1}),
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{0}, Value: []byte{0}},
				},
				EndProof: []ProofNode{{Key: Key{}}},
			},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name:  "key greater than end",
			start: maybe.Some([]byte{1}),
			end:   maybe.Some([]byte{1}),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{2}, Value: []byte{0}},
				},
				EndProof: []ProofNode{{Key: Key{}}},
			},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name:  "start proof nodes in wrong order",
			start: maybe.Some([]byte{1, 2}),
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}, Value: []byte{1}},
				},
				StartProof: []ProofNode{
					{
						Key: ToKey([]byte{2}),
					},
					{
						Key: ToKey([]byte{1}),
					},
				},
				EndProof: []ProofNode{{Key: Key{}}},
			},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name:  "start proof has node for wrong key",
			start: maybe.Some([]byte{1, 2}),
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}, Value: []byte{1}},
				},
				StartProof: []ProofNode{
					{
						Key: ToKey([]byte{1}),
					},
					{
						Key: ToKey([]byte{1, 2, 3}), // Not a prefix of [1, 2]
					},
					{
						Key: ToKey([]byte{1, 2, 3, 4}),
					},
				},
				EndProof: []ProofNode{{Key: Key{}}},
			},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name:  "end proof nodes in wrong order",
			start: maybe.Nothing[[]byte](),
			end:   maybe.Some([]byte{1, 2}),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}, Value: []byte{1}},
				},
				EndProof: []ProofNode{
					{
						Key: ToKey([]byte{2}),
					},
					{
						Key: ToKey([]byte{1}),
					},
				},
			},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name:  "end proof has node for wrong key",
			start: maybe.Nothing[[]byte](),
			end:   maybe.Some([]byte{1, 2}),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}, Value: []byte{1}},
				},
				EndProof: []ProofNode{
					{
						Key: ToKey([]byte{1}),
					},
					{
						Key: ToKey([]byte{1, 2, 3}), // Not a prefix of [1, 2]
					},
					{
						Key: ToKey([]byte{1, 2, 3, 4}),
					},
				},
			},
			expectedErr: ErrProofNodeNotForKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.proof.Verify(context.Background(), tt.start, tt.end, ids.Empty, 4, DefaultHasher)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func Test_RangeProof(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	writeBasicBatch(t, db)

	proof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte{1}), maybe.Some([]byte{3, 5}), 10)
	require.NoError(err)
	require.NotNil(proof)
	require.Len(proof.KeyValues, 3)

	require.Equal([]byte{1}, proof.KeyValues[0].Key)
	require.Equal([]byte{2}, proof.KeyValues[1].Key)
	require.Equal([]byte{3}, proof.KeyValues[2].Key)

	require.Equal([]byte{1}, proof.KeyValues[0].Value)
	require.Equal([]byte{2}, proof.KeyValues[1].Value)
	require.Equal([]byte{3}, proof.KeyValues[2].Value)

	require.Len(proof.EndProof, 2)
	require.Equal([]byte{0}, proof.EndProof[0].Key.Bytes())
	require.Len(proof.EndProof[0].Children, 5) // 0,1,2,3,4
	require.Equal([]byte{3}, proof.EndProof[1].Key.Bytes())

	// only a single node here since others are duplicates in endproof
	require.Equal([]byte{1}, proof.StartProof[0].Key.Bytes())

	require.NoError(proof.Verify(
		context.Background(),
		maybe.Some([]byte{1}),
		maybe.Some([]byte{3, 5}),
		db.rootID,
		db.tokenSize,
		db.hasher,
	))
}

func Test_RangeProof_BadBounds(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	require.NoError(db.Put(nil, nil))

	// non-nil start/end
	proof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte{4}), maybe.Some([]byte{3}), 50)
	require.ErrorIs(err, ErrStartAfterEnd)
	require.Nil(proof)
}

func Test_RangeProof_NilStart(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), []byte("value1")))
	require.NoError(batch.Put([]byte("key2"), []byte("value2")))
	require.NoError(batch.Put([]byte("key3"), []byte("value3")))
	require.NoError(batch.Put([]byte("key4"), []byte("value4")))
	require.NoError(batch.Write())

	val, err := db.Get([]byte("key1"))
	require.NoError(err)
	require.Equal([]byte("value1"), val)

	proof, err := db.GetRangeProof(context.Background(), maybe.Nothing[[]byte](), maybe.Some([]byte("key35")), 2)
	require.NoError(err)
	require.NotNil(proof)

	require.Len(proof.KeyValues, 2)

	require.Equal([]byte("key1"), proof.KeyValues[0].Key)
	require.Equal([]byte("key2"), proof.KeyValues[1].Key)

	require.Equal([]byte("value1"), proof.KeyValues[0].Value)
	require.Equal([]byte("value2"), proof.KeyValues[1].Value)

	require.Equal(ToKey([]byte("key2")), proof.EndProof[1].Key, db.tokenSize)
	require.Equal(ToKey([]byte("key2")).Take(28), proof.EndProof[0].Key)

	require.NoError(proof.Verify(
		context.Background(),
		maybe.Nothing[[]byte](),
		maybe.Some([]byte("key35")),
		db.rootID,
		db.tokenSize,
		db.hasher,
	))
}

func Test_RangeProof_NilEnd(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	writeBasicBatch(t, db)
	require.NoError(err)

	proof, err := db.GetRangeProof( // Should have keys [1], [2]
		context.Background(),
		maybe.Some([]byte{1}),
		maybe.Nothing[[]byte](),
		2,
	)
	require.NoError(err)
	require.NotNil(proof)

	require.Len(proof.KeyValues, 2)

	require.Equal([]byte{1}, proof.KeyValues[0].Key)
	require.Equal([]byte{2}, proof.KeyValues[1].Key)

	require.Equal([]byte{1}, proof.KeyValues[0].Value)
	require.Equal([]byte{2}, proof.KeyValues[1].Value)

	require.Equal([]byte{1}, proof.StartProof[0].Key.Bytes())

	require.Equal(db.root.Value().key, proof.EndProof[0].Key)
	require.Equal([]byte{2}, proof.EndProof[1].Key.Bytes())

	require.NoError(proof.Verify(
		context.Background(),
		maybe.Some([]byte{1}),
		maybe.Nothing[[]byte](),
		db.rootID,
		db.tokenSize,
		db.hasher,
	))
}

func Test_RangeProof_EmptyValues(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key1"), nil))
	require.NoError(batch.Put([]byte("key12"), []byte("value1")))
	require.NoError(batch.Put([]byte("key2"), []byte{}))
	require.NoError(batch.Write())

	val, err := db.Get([]byte("key12"))
	require.NoError(err)
	require.Equal([]byte("value1"), val)

	proof, err := db.GetRangeProof(context.Background(), maybe.Some([]byte("key1")), maybe.Some([]byte("key2")), 10)
	require.NoError(err)
	require.NotNil(proof)

	require.Len(proof.KeyValues, 3)
	require.Equal([]byte("key1"), proof.KeyValues[0].Key)
	require.Empty(proof.KeyValues[0].Value)
	require.Equal([]byte("key12"), proof.KeyValues[1].Key)
	require.Equal([]byte("value1"), proof.KeyValues[1].Value)
	require.Equal([]byte("key2"), proof.KeyValues[2].Key)
	require.Empty(proof.KeyValues[2].Value)

	require.Len(proof.StartProof, 1)
	require.Equal(ToKey([]byte("key1")), proof.StartProof[0].Key)

	require.Len(proof.EndProof, 2)
	require.Equal(ToKey([]byte("key1")).Take(28), proof.EndProof[0].Key, db.tokenSize) // root
	require.Equal(ToKey([]byte("key2")), proof.EndProof[1].Key, db.tokenSize)

	require.NoError(proof.Verify(
		context.Background(),
		maybe.Some([]byte("key1")),
		maybe.Some([]byte("key2")),
		db.rootID,
		db.tokenSize,
		db.hasher,
	))
}

func Test_ChangeProof_Missing_History_For_EndRoot(t *testing.T) {
	require := require.New(t)
	seed := time.Now().UnixNano()
	t.Logf("Seed: %d", seed)
	rand := rand.New(rand.NewSource(seed)) // #nosec G404

	db, err := getBasicDB()
	require.NoError(err)

	roots := []ids.ID{}
	for i := 0; i < defaultHistoryLength+1; i++ {
		key := make([]byte, 16)
		_, _ = rand.Read(key)
		require.NoError(db.Put(key, nil))
		root, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)
		roots = append(roots, root)
	}

	_, err = db.GetChangeProof(
		context.Background(),
		roots[len(roots)-1],
		ids.GenerateTestID(),
		maybe.Nothing[[]byte](),
		maybe.Nothing[[]byte](),
		50,
	)
	require.ErrorIs(err, ErrNoEndRoot)
	require.ErrorIs(err, ErrInsufficientHistory)

	_, err = db.GetChangeProof(
		context.Background(),
		roots[0],
		roots[len(roots)-1],
		maybe.Nothing[[]byte](),
		maybe.Nothing[[]byte](),
		50,
	)
	require.NotErrorIs(err, ErrNoEndRoot)
	require.ErrorIs(err, ErrInsufficientHistory)

	_, err = db.GetChangeProof(
		context.Background(),
		roots[1],
		roots[len(roots)-1],
		maybe.Nothing[[]byte](),
		maybe.Nothing[[]byte](),
		50,
	)
	require.NoError(err)
}

func Test_ChangeProof_BadBounds(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.NoError(db.PutContext(context.Background(), []byte{0}, []byte{0}))

	endRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	// non-nil start/end
	proof, err := db.GetChangeProof(context.Background(), startRoot, endRoot, maybe.Some([]byte("key4")), maybe.Some([]byte("key3")), 50)
	require.ErrorIs(err, ErrStartAfterEnd)
	require.Nil(proof)
}

func Test_ChangeProof_Verify(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	batch := db.NewBatch()
	require.NoError(batch.Put([]byte("key20"), []byte("value0")))
	require.NoError(batch.Put([]byte("key21"), []byte("value1")))
	require.NoError(batch.Put([]byte("key22"), []byte("value2")))
	require.NoError(batch.Put([]byte("key23"), []byte("value3")))
	require.NoError(batch.Put([]byte("key24"), []byte("value4")))
	require.NoError(batch.Write())
	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	// create a second db that has "synced" to the start root
	dbClone, err := getBasicDB()
	require.NoError(err)
	batch = dbClone.NewBatch()
	require.NoError(batch.Put([]byte("key20"), []byte("value0")))
	require.NoError(batch.Put([]byte("key21"), []byte("value1")))
	require.NoError(batch.Put([]byte("key22"), []byte("value2")))
	require.NoError(batch.Put([]byte("key23"), []byte("value3")))
	require.NoError(batch.Put([]byte("key24"), []byte("value4")))
	require.NoError(batch.Write())

	// the second db has started to sync some of the range outside of the range proof
	batch = dbClone.NewBatch()
	require.NoError(batch.Put([]byte("key31"), []byte("value1")))
	require.NoError(batch.Write())

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key25"), []byte("value0")))
	require.NoError(batch.Put([]byte("key26"), []byte("value1")))
	require.NoError(batch.Put([]byte("key27"), []byte("value2")))
	require.NoError(batch.Put([]byte("key28"), []byte("value3")))
	require.NoError(batch.Put([]byte("key29"), []byte("value4")))
	require.NoError(batch.Write())

	batch = db.NewBatch()
	require.NoError(batch.Put([]byte("key30"), []byte("value0")))
	require.NoError(batch.Put([]byte("key31"), []byte("value1")))
	require.NoError(batch.Put([]byte("key32"), []byte("value2")))
	require.NoError(batch.Delete([]byte("key21")))
	require.NoError(batch.Delete([]byte("key22")))
	require.NoError(batch.Write())

	endRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	// non-nil start/end
	proof, err := db.GetChangeProof(context.Background(), startRoot, endRoot, maybe.Some([]byte("key21")), maybe.Some([]byte("key30")), 50)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(dbClone.VerifyChangeProof(context.Background(), proof, maybe.Some([]byte("key21")), maybe.Some([]byte("key30")), db.getMerkleRoot()))

	// low maxLength
	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 5)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(dbClone.VerifyChangeProof(context.Background(), proof, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), db.getMerkleRoot()))

	// nil start/end
	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), 50)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(dbClone.VerifyChangeProof(context.Background(), proof, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), endRoot))
	require.NoError(dbClone.CommitChangeProof(context.Background(), proof))

	newRoot, err := dbClone.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(endRoot, newRoot)

	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, maybe.Some([]byte("key20")), maybe.Some([]byte("key30")), 50)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(dbClone.VerifyChangeProof(context.Background(), proof, maybe.Some([]byte("key20")), maybe.Some([]byte("key30")), db.getMerkleRoot()))
}

func Test_ChangeProof_Verify_Bad_Data(t *testing.T) {
	type test struct {
		name        string
		malform     func(proof *ChangeProof)
		expectedErr error
	}

	tests := []test{
		{
			name:        "happyPath",
			malform:     func(*ChangeProof) {},
			expectedErr: nil,
		},
		{
			name: "odd length key path with value",
			malform: func(proof *ChangeProof) {
				proof.EndProof[0].ValueOrHash = maybe.Some([]byte{1, 2})
			},
			expectedErr: ErrPartialByteLengthWithValue,
		},
		{
			name: "last proof node has missing value",
			malform: func(proof *ChangeProof) {
				proof.EndProof[len(proof.EndProof)-1].ValueOrHash = maybe.Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "missing key/value",
			malform: func(proof *ChangeProof) {
				proof.KeyChanges = proof.KeyChanges[1:]
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			db, err := getBasicDB()
			require.NoError(err)

			startRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			writeBasicBatch(t, db)

			endRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			// create a second db that will be synced to the first db
			dbClone, err := getBasicDB()
			require.NoError(err)

			proof, err := db.GetChangeProof(
				context.Background(),
				startRoot,
				endRoot,
				maybe.Some([]byte{2}),
				maybe.Some([]byte{3, 0}),
				50,
			)
			require.NoError(err)
			require.NotNil(proof)

			tt.malform(proof)

			err = dbClone.VerifyChangeProof(
				context.Background(),
				proof,
				maybe.Some([]byte{2}),
				maybe.Some([]byte{3, 0}),
				db.getMerkleRoot(),
			)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func Test_ChangeProof_Syntactic_Verify(t *testing.T) {
	type test struct {
		name        string
		proof       *ChangeProof
		start       maybe.Maybe[[]byte]
		end         maybe.Maybe[[]byte]
		expectedErr error
	}

	tests := []test{
		{
			name:        "start after end",
			proof:       nil,
			start:       maybe.Some([]byte{1}),
			end:         maybe.Some([]byte{0}),
			expectedErr: ErrStartAfterEnd,
		},
		{
			name:        "empty",
			proof:       &ChangeProof{},
			start:       maybe.Nothing[[]byte](),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrEmptyProof,
		},
		{
			name: "no end proof",
			proof: &ChangeProof{
				StartProof: []ProofNode{{}},
			},
			start:       maybe.Nothing[[]byte](),
			end:         maybe.Some([]byte{1}),
			expectedErr: ErrNoEndProof,
		},
		{
			name: "no start proof",
			proof: &ChangeProof{
				KeyChanges: []KeyChange{{Key: []byte{1}}},
			},
			start:       maybe.Some([]byte{1}),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNoStartProof,
		},
		{
			name: "non-increasing key-values",
			proof: &ChangeProof{
				KeyChanges: []KeyChange{
					{Key: []byte{1}},
					{Key: []byte{0}},
				},
			},
			start:       maybe.Nothing[[]byte](),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name: "key-value too low",
			proof: &ChangeProof{
				StartProof: []ProofNode{{}},
				KeyChanges: []KeyChange{
					{Key: []byte{0}},
				},
			},
			start:       maybe.Some([]byte{1}),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name: "key-value too great",
			proof: &ChangeProof{
				EndProof: []ProofNode{{}},
				KeyChanges: []KeyChange{
					{Key: []byte{2}},
				},
			},
			start:       maybe.Nothing[[]byte](),
			end:         maybe.Some([]byte{1}),
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name: "duplicate key",
			proof: &ChangeProof{
				KeyChanges: []KeyChange{
					{Key: []byte{1}},
					{Key: []byte{1}},
				},
			},
			start:       maybe.Nothing[[]byte](),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name: "start proof node has wrong prefix",
			proof: &ChangeProof{
				StartProof: []ProofNode{
					{Key: ToKey([]byte{2})},
					{Key: ToKey([]byte{2, 3})},
				},
			},
			start:       maybe.Some([]byte{1, 2, 3}),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "start proof non-increasing",
			proof: &ChangeProof{
				StartProof: []ProofNode{
					{Key: ToKey([]byte{1})},
					{Key: ToKey([]byte{2, 3})},
				},
			},
			start:       maybe.Some([]byte{1, 2, 3}),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "end proof node has wrong prefix",
			proof: &ChangeProof{
				KeyChanges: []KeyChange{
					{Key: []byte{1, 2}, Value: maybe.Some([]byte{0})},
				},
				EndProof: []ProofNode{
					{Key: ToKey([]byte{2})},
					{Key: ToKey([]byte{2, 3})},
				},
			},
			start:       maybe.Nothing[[]byte](),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "end proof non-increasing",
			proof: &ChangeProof{
				KeyChanges: []KeyChange{
					{Key: []byte{1, 2, 3}},
				},
				EndProof: []ProofNode{
					{Key: ToKey([]byte{1})},
					{Key: ToKey([]byte{2, 3})},
				},
			},
			start:       maybe.Nothing[[]byte](),
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNonIncreasingProofNodes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			db, err := getBasicDB()
			require.NoError(err)
			err = db.VerifyChangeProof(context.Background(), tt.proof, tt.start, tt.end, ids.Empty)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestVerifyKeyValues(t *testing.T) {
	type test struct {
		name        string
		start       maybe.Maybe[[]byte]
		end         maybe.Maybe[[]byte]
		kvs         []KeyValue
		expectedErr error
	}

	tests := []test{
		{
			name:        "empty",
			start:       maybe.Nothing[[]byte](),
			end:         maybe.Nothing[[]byte](),
			kvs:         nil,
			expectedErr: nil,
		},
		{
			name:  "1 key",
			start: maybe.Nothing[[]byte](),
			end:   maybe.Nothing[[]byte](),
			kvs: []KeyValue{
				{Key: []byte{0}},
			},
			expectedErr: nil,
		},
		{
			name:  "non-increasing keys",
			start: maybe.Nothing[[]byte](),
			end:   maybe.Nothing[[]byte](),
			kvs: []KeyValue{
				{Key: []byte{0}},
				{Key: []byte{0}},
			},
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name:  "key before start",
			start: maybe.Some([]byte{1, 2}),
			end:   maybe.Nothing[[]byte](),
			kvs: []KeyValue{
				{Key: []byte{1}},
				{Key: []byte{1, 2}},
			},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name:  "key after end",
			start: maybe.Nothing[[]byte](),
			end:   maybe.Some([]byte{1, 2}),
			kvs: []KeyValue{
				{Key: []byte{1}},
				{Key: []byte{1, 2}},
				{Key: []byte{1, 2, 3}},
			},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name:  "happy path",
			start: maybe.Nothing[[]byte](),
			end:   maybe.Some([]byte{1, 2, 3}),
			kvs: []KeyValue{
				{Key: []byte{1}},
				{Key: []byte{1, 2}},
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyKeyValues(tt.kvs, tt.start, tt.end)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestVerifyProofPath(t *testing.T) {
	type test struct {
		name        string
		path        []ProofNode
		proofKey    maybe.Maybe[Key]
		expectedErr error
	}

	tests := []test{
		{
			name:        "empty",
			path:        nil,
			proofKey:    maybe.Nothing[Key](),
			expectedErr: nil,
		},
		{
			name:        "1 element",
			path:        []ProofNode{{Key: ToKey([]byte{1})}},
			proofKey:    maybe.Nothing[Key](),
			expectedErr: nil,
		},
		{
			name: "non-increasing keys",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 3})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2, 3})),
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "invalid key",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 2, 4})},
				{Key: ToKey([]byte{1, 2, 3})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2, 3})),
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "extra node inclusion proof",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 2, 3})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2})),
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "extra node exclusion proof",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 3})},
				{Key: ToKey([]byte{1, 3, 4})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2})),
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "happy path exclusion proof",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 2, 4})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2, 3})),
			expectedErr: nil,
		},
		{
			name: "happy path inclusion proof",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 2, 3})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2, 3})),
			expectedErr: nil,
		},
		{
			name: "repeat nodes",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 2, 3})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2, 3})),
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "repeat nodes 2",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 2, 3})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2, 3})),
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "repeat nodes 3",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{Key: ToKey([]byte{1, 2, 3})},
				{Key: ToKey([]byte{1, 2, 3})},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2, 3})),
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "oddLength key with value",
			path: []ProofNode{
				{Key: ToKey([]byte{1})},
				{Key: ToKey([]byte{1, 2})},
				{
					Key: Key{
						value:  string([]byte{1, 2, 240}),
						length: 20,
					},
					ValueOrHash: maybe.Some([]byte{1}),
				},
			},
			proofKey:    maybe.Some(ToKey([]byte{1, 2, 3})),
			expectedErr: ErrPartialByteLengthWithValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyProofPath(tt.path, tt.proofKey)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProofNodeUnmarshalProtoInvalidMaybe(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	node := newRandomProofNode(rand)
	protoNode := node.ToProto()

	// It's invalid to have a value and be nothing.
	protoNode.ValueOrHash = &pb.MaybeBytes{
		Value:     []byte{1, 2, 3},
		IsNothing: true,
	}

	var unmarshaledNode ProofNode
	err := unmarshaledNode.UnmarshalProto(protoNode)
	require.ErrorIs(t, err, ErrInvalidMaybe)
}

func TestProofNodeUnmarshalProtoInvalidChildBytes(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	node := newRandomProofNode(rand)
	protoNode := node.ToProto()

	protoNode.Children = map[uint32][]byte{
		1: []byte("not 32 bytes"),
	}

	var unmarshaledNode ProofNode
	err := unmarshaledNode.UnmarshalProto(protoNode)
	require.ErrorIs(t, err, hashing.ErrInvalidHashLen)
}

func TestProofNodeUnmarshalProtoInvalidChildIndex(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	node := newRandomProofNode(rand)
	protoNode := node.ToProto()

	childID := ids.GenerateTestID()
	protoNode.Children[256] = childID[:]

	var unmarshaledNode ProofNode
	err := unmarshaledNode.UnmarshalProto(protoNode)
	require.ErrorIs(t, err, errChildIndexTooLarge)
}

func TestProofNodeUnmarshalProtoMissingFields(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	type test struct {
		name        string
		nodeFunc    func() *pb.ProofNode
		expectedErr error
	}

	tests := []test{
		{
			name: "nil node",
			nodeFunc: func() *pb.ProofNode {
				return nil
			},
			expectedErr: ErrNilProofNode,
		},
		{
			name: "nil ValueOrHash",
			nodeFunc: func() *pb.ProofNode {
				node := newRandomProofNode(rand)
				protoNode := node.ToProto()
				protoNode.ValueOrHash = nil
				return protoNode
			},
			expectedErr: ErrNilValueOrHash,
		},
		{
			name: "nil key",
			nodeFunc: func() *pb.ProofNode {
				node := newRandomProofNode(rand)
				protoNode := node.ToProto()
				protoNode.Key = nil
				return protoNode
			},
			expectedErr: ErrNilKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var node ProofNode
			err := node.UnmarshalProto(tt.nodeFunc())
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func FuzzProofNodeProtoMarshalUnmarshal(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		randSeed int64,
	) {
		require := require.New(t)
		rand := rand.New(rand.NewSource(randSeed)) // #nosec G404
		node := newRandomProofNode(rand)

		// Marshal and unmarshal it.
		// Assert the unmarshaled one is the same as the original.
		protoNode := node.ToProto()
		var unmarshaledNode ProofNode
		require.NoError(unmarshaledNode.UnmarshalProto(protoNode))
		require.Equal(node, unmarshaledNode)

		// Marshaling again should yield same result.
		protoUnmarshaledNode := unmarshaledNode.ToProto()
		require.Equal(protoNode, protoUnmarshaledNode)
	})
}

func FuzzRangeProofProtoMarshalUnmarshal(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		randSeed int64,
	) {
		require := require.New(t)
		rand := rand.New(rand.NewSource(randSeed)) // #nosec G404

		// Make a random range proof.
		startProofLen := rand.Intn(32)
		startProof := make([]ProofNode, startProofLen)
		for i := 0; i < startProofLen; i++ {
			startProof[i] = newRandomProofNode(rand)
		}

		endProofLen := rand.Intn(32)
		endProof := make([]ProofNode, endProofLen)
		for i := 0; i < endProofLen; i++ {
			endProof[i] = newRandomProofNode(rand)
		}

		numKeyValues := rand.Intn(128)
		keyValues := make([]KeyValue, numKeyValues)
		for i := 0; i < numKeyValues; i++ {
			keyLen := rand.Intn(32)
			key := make([]byte, keyLen)
			_, _ = rand.Read(key)

			valueLen := rand.Intn(32)
			value := make([]byte, valueLen)
			_, _ = rand.Read(value)

			keyValues[i] = KeyValue{
				Key:   key,
				Value: value,
			}
		}

		proof := RangeProof{
			StartProof: startProof,
			EndProof:   endProof,
			KeyValues:  keyValues,
		}

		// Marshal and unmarshal it.
		// Assert the unmarshaled one is the same as the original.
		var unmarshaledProof RangeProof
		protoProof := proof.ToProto()
		require.NoError(unmarshaledProof.UnmarshalProto(protoProof))
		require.Equal(proof, unmarshaledProof)

		// Marshaling again should yield same result.
		protoUnmarshaledProof := unmarshaledProof.ToProto()
		require.Equal(protoProof, protoUnmarshaledProof)
	})
}

func FuzzChangeProofProtoMarshalUnmarshal(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		randSeed int64,
	) {
		require := require.New(t)
		rand := rand.New(rand.NewSource(randSeed)) // #nosec G404

		// Make a random change proof.
		startProofLen := rand.Intn(32)
		startProof := make([]ProofNode, startProofLen)
		for i := 0; i < startProofLen; i++ {
			startProof[i] = newRandomProofNode(rand)
		}

		endProofLen := rand.Intn(32)
		endProof := make([]ProofNode, endProofLen)
		for i := 0; i < endProofLen; i++ {
			endProof[i] = newRandomProofNode(rand)
		}

		numKeyChanges := rand.Intn(128)
		keyChanges := make([]KeyChange, numKeyChanges)
		for i := 0; i < numKeyChanges; i++ {
			keyLen := rand.Intn(32)
			key := make([]byte, keyLen)
			_, _ = rand.Read(key)

			value := maybe.Nothing[[]byte]()
			hasValue := rand.Intn(2) == 0
			if hasValue {
				valueLen := rand.Intn(32)
				valueBytes := make([]byte, valueLen)
				_, _ = rand.Read(valueBytes)
				value = maybe.Some(valueBytes)
			}

			keyChanges[i] = KeyChange{
				Key:   key,
				Value: value,
			}
		}

		proof := ChangeProof{
			StartProof: startProof,
			EndProof:   endProof,
			KeyChanges: keyChanges,
		}

		// Marshal and unmarshal it.
		// Assert the unmarshaled one is the same as the original.
		var unmarshaledProof ChangeProof
		protoProof := proof.ToProto()
		require.NoError(unmarshaledProof.UnmarshalProto(protoProof))
		require.Equal(proof, unmarshaledProof)

		// Marshaling again should yield same result.
		protoUnmarshaledProof := unmarshaledProof.ToProto()
		require.Equal(protoProof, protoUnmarshaledProof)
	})
}

func TestChangeProofUnmarshalProtoNil(t *testing.T) {
	var proof ChangeProof
	err := proof.UnmarshalProto(nil)
	require.ErrorIs(t, err, ErrNilChangeProof)
}

func TestChangeProofUnmarshalProtoNilValue(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	// Make a random change proof.
	startProofLen := rand.Intn(32)
	startProof := make([]ProofNode, startProofLen)
	for i := 0; i < startProofLen; i++ {
		startProof[i] = newRandomProofNode(rand)
	}

	endProofLen := rand.Intn(32)
	endProof := make([]ProofNode, endProofLen)
	for i := 0; i < endProofLen; i++ {
		endProof[i] = newRandomProofNode(rand)
	}

	numKeyChanges := rand.Intn(128) + 1
	keyChanges := make([]KeyChange, numKeyChanges)
	for i := 0; i < numKeyChanges; i++ {
		keyLen := rand.Intn(32)
		key := make([]byte, keyLen)
		_, _ = rand.Read(key)

		value := maybe.Nothing[[]byte]()
		hasValue := rand.Intn(2) == 0
		if hasValue {
			valueLen := rand.Intn(32)
			valueBytes := make([]byte, valueLen)
			_, _ = rand.Read(valueBytes)
			value = maybe.Some(valueBytes)
		}

		keyChanges[i] = KeyChange{
			Key:   key,
			Value: value,
		}
	}

	proof := ChangeProof{
		StartProof: startProof,
		EndProof:   endProof,
		KeyChanges: keyChanges,
	}
	protoProof := proof.ToProto()
	// Make a value nil
	protoProof.KeyChanges[0].Value = nil

	var unmarshaledProof ChangeProof
	err := unmarshaledProof.UnmarshalProto(protoProof)
	require.ErrorIs(t, err, ErrNilMaybeBytes)
}

func TestChangeProofUnmarshalProtoInvalidMaybe(t *testing.T) {
	protoProof := &pb.ChangeProof{
		KeyChanges: []*pb.KeyChange{
			{
				Key: []byte{1},
				Value: &pb.MaybeBytes{
					Value:     []byte{1},
					IsNothing: true,
				},
			},
		},
	}

	var proof ChangeProof
	err := proof.UnmarshalProto(protoProof)
	require.ErrorIs(t, err, ErrInvalidMaybe)
}

func FuzzProofProtoMarshalUnmarshal(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		randSeed int64,
	) {
		require := require.New(t)
		rand := rand.New(rand.NewSource(randSeed)) // #nosec G404

		// Make a random proof.
		proofLen := rand.Intn(32)
		proofPath := make([]ProofNode, proofLen)
		for i := 0; i < proofLen; i++ {
			proofPath[i] = newRandomProofNode(rand)
		}

		keyLen := rand.Intn(32)
		key := make([]byte, keyLen)
		_, _ = rand.Read(key)

		hasValue := rand.Intn(2) == 1
		value := maybe.Nothing[[]byte]()
		if hasValue {
			valueLen := rand.Intn(32)
			valueBytes := make([]byte, valueLen)
			_, _ = rand.Read(valueBytes)
			value = maybe.Some(valueBytes)
		}

		proof := Proof{
			Key:   ToKey(key),
			Value: value,
			Path:  proofPath,
		}

		// Marshal and unmarshal it.
		// Assert the unmarshaled one is the same as the original.
		var unmarshaledProof Proof
		protoProof := proof.ToProto()
		require.NoError(unmarshaledProof.UnmarshalProto(protoProof))
		require.Equal(proof, unmarshaledProof)

		// Marshaling again should yield same result.
		protoUnmarshaledProof := unmarshaledProof.ToProto()
		require.Equal(protoProof, protoUnmarshaledProof)
	})
}

func TestProofProtoUnmarshal(t *testing.T) {
	type test struct {
		name        string
		proof       *pb.Proof
		expectedErr error
	}

	tests := []test{
		{
			name:        "nil",
			proof:       nil,
			expectedErr: ErrNilProof,
		},
		{
			name:        "nil value",
			proof:       &pb.Proof{},
			expectedErr: ErrNilValue,
		},
		{
			name: "invalid maybe",
			proof: &pb.Proof{
				Value: &pb.MaybeBytes{
					Value:     []byte{1},
					IsNothing: true,
				},
			},
			expectedErr: ErrInvalidMaybe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var proof Proof
			err := proof.UnmarshalProto(tt.proof)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func FuzzRangeProofInvariants(f *testing.F) {
	deletePortion := 0.25
	f.Fuzz(func(
		t *testing.T,
		randSeed int64,
		startBytes []byte,
		endBytes []byte,
		maxProofLen uint,
		numKeyValues uint,
	) {
		require := require.New(t)

		// Make sure proof length is valid
		if maxProofLen == 0 {
			t.SkipNow()
		}
		if numKeyValues == 0 {
			t.SkipNow()
		}

		// Make sure proof bounds are valid
		if len(endBytes) != 0 && bytes.Compare(startBytes, endBytes) > 0 {
			t.SkipNow()
		}

		rand := rand.New(rand.NewSource(randSeed)) // #nosec G404

		db, err := getBasicDB()
		require.NoError(err)

		// Insert a bunch of random key values.
		insertRandomKeyValues(
			require,
			rand,
			[]database.Database{db},
			numKeyValues,
			deletePortion,
		)

		start := maybe.Nothing[[]byte]()
		if len(startBytes) != 0 {
			start = maybe.Some(startBytes)
		}

		end := maybe.Nothing[[]byte]()
		if len(endBytes) != 0 {
			end = maybe.Some(endBytes)
		}

		rootID, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)

		rangeProof, err := db.GetRangeProof(
			context.Background(),
			start,
			end,
			int(maxProofLen),
		)
		if rootID == ids.Empty {
			require.ErrorIs(err, ErrEmptyProof)
			return
		}
		require.NoError(err)

		require.NoError(rangeProof.Verify(
			context.Background(),
			start,
			end,
			rootID,
			db.tokenSize,
			db.hasher,
		))

		// Make sure the start proof doesn't contain any nodes
		// that are in the end proof.
		endProofKeys := set.Set[Key]{}
		for _, node := range rangeProof.EndProof {
			endProofKeys.Add(node.Key)
		}

		for _, node := range rangeProof.StartProof {
			require.NotContains(endProofKeys, node.Key)
		}

		// Make sure the EndProof invariant is maintained
		switch {
		case end.IsNothing():
			if len(rangeProof.KeyValues) == 0 {
				if len(rangeProof.StartProof) == 0 {
					require.Len(rangeProof.EndProof, 1) // Just the root
					require.Empty(rangeProof.EndProof[0].Key.Bytes())
				} else {
					require.Empty(rangeProof.EndProof)
				}
			}
		case len(rangeProof.KeyValues) == 0:
			require.NotEmpty(rangeProof.EndProof)

			// EndProof should be a proof for upper range bound.
			value := maybe.Nothing[[]byte]()
			upperRangeBoundVal, err := db.Get(endBytes)
			if err != nil {
				require.ErrorIs(err, database.ErrNotFound)
			} else {
				value = maybe.Some(upperRangeBoundVal)
			}

			proof := Proof{
				Path:  rangeProof.EndProof,
				Key:   ToKey(endBytes),
				Value: value,
			}

			rootID, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			require.NoError(proof.Verify(context.Background(), rootID, db.tokenSize, db.hasher))
		default:
			require.NotEmpty(rangeProof.EndProof)

			greatestKV := rangeProof.KeyValues[len(rangeProof.KeyValues)-1]
			// EndProof should be a proof for largest key-value.
			proof := Proof{
				Path:  rangeProof.EndProof,
				Key:   ToKey(greatestKV.Key),
				Value: maybe.Some(greatestKV.Value),
			}

			rootID, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			require.NoError(proof.Verify(context.Background(), rootID, db.tokenSize, db.hasher))
		}
	})
}

func FuzzProofVerification(f *testing.F) {
	deletePortion := 0.25
	f.Fuzz(func(
		t *testing.T,
		key []byte,
		randSeed int64,
		numKeyValues uint,
	) {
		rand := rand.New(rand.NewSource(randSeed)) // #nosec G404
		require := require.New(t)
		db, err := getBasicDB()
		require.NoError(err)

		// Insert a bunch of random key values.
		insertRandomKeyValues(
			require,
			rand,
			[]database.Database{db},
			numKeyValues,
			deletePortion,
		)

		if db.getMerkleRoot() == ids.Empty {
			return
		}

		proof, err := db.GetProof(
			context.Background(),
			key,
		)

		require.NoError(err)

		rootID, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)

		require.NoError(proof.Verify(context.Background(), rootID, db.tokenSize, db.hasher))

		// Insert a new key-value pair
		newKey := make([]byte, 32)
		_, _ = rand.Read(newKey) // #nosec G404
		newValue := make([]byte, 32)
		_, _ = rand.Read(newValue) // #nosec G404
		require.NoError(db.Put(newKey, newValue))

		// Delete a key-value pair so database doesn't grow unbounded
		iter := db.NewIterator()
		deleteKey := iter.Key()
		iter.Release()

		require.NoError(db.Delete(deleteKey))
	})
}

// Generate change proofs and verify that they are valid.
func FuzzChangeProofVerification(f *testing.F) {
	const (
		numKeyValues  = defaultHistoryLength / 2
		deletePortion = 0.25
	)

	f.Fuzz(func(
		t *testing.T,
		startBytes []byte,
		endBytes []byte,
		maxProofLen uint,
		randSeed int64,
	) {
		require := require.New(t)
		rand := rand.New(rand.NewSource(randSeed)) // #nosec G404

		db, err := getBasicDB()
		require.NoError(err)

		startRootID, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)

		// Insert a bunch of random key values.
		// Don't insert so many that we have insufficient history.
		insertRandomKeyValues(
			require,
			rand,
			[]database.Database{db},
			numKeyValues,
			deletePortion,
		)

		endRootID, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)

		// Make sure proof bounds are valid
		if len(endBytes) != 0 && bytes.Compare(startBytes, endBytes) > 0 {
			return
		}
		// Make sure proof length is valid
		if maxProofLen == 0 {
			return
		}

		start := maybe.Nothing[[]byte]()
		if len(startBytes) != 0 {
			start = maybe.Some(startBytes)
		}

		end := maybe.Nothing[[]byte]()
		if len(endBytes) != 0 {
			end = maybe.Some(endBytes)
		}

		changeProof, err := db.GetChangeProof(
			context.Background(),
			startRootID,
			endRootID,
			start,
			end,
			int(maxProofLen),
		)
		require.NoError(err)

		require.NoError(db.VerifyChangeProof(
			context.Background(),
			changeProof,
			start,
			end,
			endRootID,
		))
	})
}
