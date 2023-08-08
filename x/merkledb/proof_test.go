// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func getBasicDB() (*merkleDB, error) {
	return newDatabase(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
		&mockMetrics{},
	)
}

func writeBasicBatch(t *testing.T, db *merkleDB) {
	require := require.New(t)

	batch := db.NewBatch()
	require.NoError(batch.Put([]byte{0}, []byte{0}))
	require.NoError(batch.Put([]byte{1}, []byte{1}))
	require.NoError(batch.Put([]byte{2}, []byte{2}))
	require.NoError(batch.Put([]byte{3}, []byte{3}))
	require.NoError(batch.Put([]byte{4}, []byte{4}))
	require.NoError(batch.Write())
}

func Test_Proof_Empty(t *testing.T) {
	proof := &Proof{}
	err := proof.Verify(context.Background(), ids.Empty)
	require.ErrorIs(t, err, ErrNoProof)
}

func Test_Proof_Simple(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	ctx := context.Background()
	require.NoError(db.Insert(ctx, []byte{}, []byte{1}))
	require.NoError(db.Insert(ctx, []byte{0}, []byte{2}))

	expectedRoot, err := db.GetMerkleRoot(ctx)
	require.NoError(err)

	proof, err := db.GetProof(ctx, []byte{})
	require.NoError(err)

	require.NoError(proof.Verify(ctx, expectedRoot))
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
			malform:     func(proof *Proof) {},
			expectedErr: nil,
		},
		{
			name: "odd length key path with value",
			malform: func(proof *Proof) {
				proof.Path[1].ValueOrHash = maybe.Some([]byte{1, 2})
			},
			expectedErr: ErrOddLengthWithValue,
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

			err = proof.Verify(context.Background(), db.getMerkleRoot())
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func Test_Proof_ValueOrHashMatches(t *testing.T) {
	require := require.New(t)

	require.True(valueOrHashMatches(maybe.Some([]byte{0}), maybe.Some([]byte{0})))
	require.False(valueOrHashMatches(maybe.Nothing[[]byte](), maybe.Some(hashing.ComputeHash256([]byte{0}))))
	require.True(valueOrHashMatches(maybe.Nothing[[]byte](), maybe.Nothing[[]byte]()))

	require.False(valueOrHashMatches(maybe.Some([]byte{0}), maybe.Nothing[[]byte]()))
	require.False(valueOrHashMatches(maybe.Nothing[[]byte](), maybe.Some([]byte{0})))
	require.False(valueOrHashMatches(maybe.Nothing[[]byte](), maybe.Some(hashing.ComputeHash256([]byte{1}))))
	require.False(valueOrHashMatches(maybe.Some(hashing.ComputeHash256([]byte{0})), maybe.Nothing[[]byte]()))
}

func Test_RangeProof_Extra_Value(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	writeBasicBatch(t, db)

	val, err := db.Get([]byte{2})
	require.NoError(err)
	require.Equal([]byte{2}, val)

	proof, err := db.GetRangeProof(context.Background(), []byte{1}, maybe.Some([]byte{5, 5}), 10)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(proof.Verify(
		context.Background(),
		[]byte{1},
		maybe.Some([]byte{5, 5}),
		db.root.id,
	))

	proof.KeyValues = append(proof.KeyValues, KeyValue{Key: []byte{5}, Value: []byte{5}})

	err = proof.Verify(
		context.Background(),
		[]byte{1},
		maybe.Some([]byte{5, 5}),
		db.root.id,
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
			malform:     func(proof *RangeProof) {},
			expectedErr: nil,
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
				proof.EndProof[1].ValueOrHash = maybe.Some([]byte{1, 2})
			},
			expectedErr: ErrOddLengthWithValue,
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

			proof, err := db.GetRangeProof(context.Background(), []byte{2}, maybe.Some([]byte{3, 0}), 50)
			require.NoError(err)
			require.NotNil(proof)

			tt.malform(proof)

			err = proof.Verify(context.Background(), []byte{2}, maybe.Some([]byte{3, 0}), db.getMerkleRoot())
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func Test_RangeProof_MaxLength(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie, err := dbTrie.NewView()
	require.NoError(err)

	_, err = trie.GetRangeProof(context.Background(), nil, maybe.Nothing[[]byte](), -1)
	require.ErrorIs(err, ErrInvalidMaxLength)

	_, err = trie.GetRangeProof(context.Background(), nil, maybe.Nothing[[]byte](), 0)
	require.ErrorIs(err, ErrInvalidMaxLength)
}

func Test_Proof(t *testing.T) {
	require := require.New(t)

	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	trie, err := dbTrie.NewView()
	require.NoError(err)

	require.NoError(trie.Insert(context.Background(), []byte("key0"), []byte("value0")))
	require.NoError(trie.Insert(context.Background(), []byte("key1"), []byte("value1")))
	require.NoError(trie.Insert(context.Background(), []byte("key2"), []byte("value2")))
	require.NoError(trie.Insert(context.Background(), []byte("key3"), []byte("value3")))
	require.NoError(trie.Insert(context.Background(), []byte("key4"), []byte("value4")))

	_, err = trie.GetMerkleRoot(context.Background())
	require.NoError(err)
	proof, err := trie.GetProof(context.Background(), []byte("key1"))
	require.NoError(err)
	require.NotNil(proof)

	require.Len(proof.Path, 3)

	require.Equal(newPath([]byte("key1")).Serialize(), proof.Path[2].KeyPath)
	require.Equal(maybe.Some([]byte("value1")), proof.Path[2].ValueOrHash)

	require.Equal(newPath([]byte{}).Serialize(), proof.Path[0].KeyPath)
	require.True(proof.Path[0].ValueOrHash.IsNothing())

	expectedRootID, err := trie.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.NoError(proof.Verify(context.Background(), expectedRootID))

	proof.Path[0].ValueOrHash = maybe.Some([]byte("value2"))

	err = proof.Verify(context.Background(), expectedRootID)
	require.ErrorIs(err, ErrInvalidProof)
}

func Test_RangeProof_Syntactic_Verify(t *testing.T) {
	type test struct {
		name        string
		start       []byte
		end         maybe.Maybe[[]byte]
		proof       *RangeProof
		expectedErr error
	}

	tests := []test{
		{
			name:        "start > end",
			start:       []byte{1},
			end:         maybe.Some([]byte{0}),
			proof:       &RangeProof{},
			expectedErr: ErrStartAfterEnd,
		},
		{
			name:        "empty", // Also tests start can be > end if end is nil
			start:       []byte{1},
			end:         maybe.Nothing[[]byte](),
			proof:       &RangeProof{},
			expectedErr: ErrNoMerkleProof,
		},
		{
			name:  "unexpected end proof",
			start: []byte{1},
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				StartProof: []ProofNode{{}},
				EndProof:   []ProofNode{{}},
			},
			expectedErr: ErrUnexpectedEndProof,
		},
		{
			name:  "should just be root",
			start: nil,
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				EndProof: []ProofNode{{}, {}},
			},
			expectedErr: ErrShouldJustBeRoot,
		},
		{
			name:  "no end proof",
			start: []byte{1},
			end:   maybe.Some([]byte{1}),
			proof: &RangeProof{
				StartProof: []ProofNode{{}},
			},
			expectedErr: ErrNoEndProof,
		},
		{
			name:  "unsorted key values",
			start: []byte{1},
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1}, Value: []byte{1}},
					{Key: []byte{0}, Value: []byte{0}},
				},
			},
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name:  "key lower than start",
			start: []byte{1},
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{0}, Value: []byte{0}},
				},
			},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name:  "key greater than end",
			start: []byte{1},
			end:   maybe.Some([]byte{1}),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{2}, Value: []byte{0}},
				},
				EndProof: []ProofNode{{}},
			},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name:  "start proof nodes in wrong order",
			start: []byte{1, 2},
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}, Value: []byte{1}},
				},
				StartProof: []ProofNode{
					{
						KeyPath: newPath([]byte{2}).Serialize(),
					},
					{
						KeyPath: newPath([]byte{1}).Serialize(),
					},
				},
			},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name:  "start proof has node for wrong key",
			start: []byte{1, 2},
			end:   maybe.Nothing[[]byte](),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}, Value: []byte{1}},
				},
				StartProof: []ProofNode{
					{
						KeyPath: newPath([]byte{1}).Serialize(),
					},
					{
						KeyPath: newPath([]byte{1, 2, 3}).Serialize(), // Not a prefix of [1, 2]
					},
					{
						KeyPath: newPath([]byte{1, 2, 3, 4}).Serialize(),
					},
				},
			},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name:  "end proof nodes in wrong order",
			start: nil,
			end:   maybe.Some([]byte{1, 2}),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}, Value: []byte{1}},
				},
				EndProof: []ProofNode{
					{
						KeyPath: newPath([]byte{2}).Serialize(),
					},
					{
						KeyPath: newPath([]byte{1}).Serialize(),
					},
				},
			},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name:  "end proof has node for wrong key",
			start: nil,
			end:   maybe.Some([]byte{1, 2}),
			proof: &RangeProof{
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}, Value: []byte{1}},
				},
				EndProof: []ProofNode{
					{
						KeyPath: newPath([]byte{1}).Serialize(),
					},
					{
						KeyPath: newPath([]byte{1, 2, 3}).Serialize(), // Not a prefix of [1, 2]
					},
					{
						KeyPath: newPath([]byte{1, 2, 3, 4}).Serialize(),
					},
				},
			},
			expectedErr: ErrProofNodeNotForKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.proof.Verify(context.Background(), tt.start, tt.end, ids.Empty)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func Test_RangeProof(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	writeBasicBatch(t, db)

	proof, err := db.GetRangeProof(context.Background(), []byte{1}, maybe.Some([]byte{3, 5}), 10)
	require.NoError(err)
	require.NotNil(proof)
	require.Len(proof.KeyValues, 3)

	require.Equal([]byte{1}, proof.KeyValues[0].Key)
	require.Equal([]byte{2}, proof.KeyValues[1].Key)
	require.Equal([]byte{3}, proof.KeyValues[2].Key)

	require.Equal([]byte{1}, proof.KeyValues[0].Value)
	require.Equal([]byte{2}, proof.KeyValues[1].Value)
	require.Equal([]byte{3}, proof.KeyValues[2].Value)

	require.Equal([]byte{}, proof.EndProof[0].KeyPath.Value)
	require.Equal([]byte{0}, proof.EndProof[1].KeyPath.Value)
	require.Equal([]byte{3}, proof.EndProof[2].KeyPath.Value)

	// only a single node here since others are duplicates in endproof
	require.Equal([]byte{1}, proof.StartProof[0].KeyPath.Value)

	require.NoError(proof.Verify(
		context.Background(),
		[]byte{1},
		maybe.Some([]byte{3, 5}),
		db.root.id,
	))
}

func Test_RangeProof_BadBounds(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	// non-nil start/end
	proof, err := db.GetRangeProof(context.Background(), []byte{4}, maybe.Some([]byte{3}), 50)
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

	proof, err := db.GetRangeProof(context.Background(), nil, maybe.Some([]byte("key35")), 2)
	require.NoError(err)
	require.NotNil(proof)

	require.Len(proof.KeyValues, 2)

	require.Equal([]byte("key1"), proof.KeyValues[0].Key)
	require.Equal([]byte("key2"), proof.KeyValues[1].Key)

	require.Equal([]byte("value1"), proof.KeyValues[0].Value)
	require.Equal([]byte("value2"), proof.KeyValues[1].Value)

	require.Equal(newPath([]byte("key2")).Serialize(), proof.EndProof[2].KeyPath)
	require.Equal(SerializedPath{Value: []uint8{0x6b, 0x65, 0x79, 0x30}, NibbleLength: 7}, proof.EndProof[1].KeyPath)
	require.Equal(newPath([]byte("")).Serialize(), proof.EndProof[0].KeyPath)

	require.NoError(proof.Verify(
		context.Background(),
		nil,
		maybe.Some([]byte("key35")),
		db.root.id,
	))
}

func Test_RangeProof_NilEnd(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	writeBasicBatch(t, db)
	require.NoError(err)

	proof, err := db.GetRangeProof(context.Background(), []byte{1}, maybe.Nothing[[]byte](), 2)
	require.NoError(err)
	require.NotNil(proof)

	require.Len(proof.KeyValues, 2)

	require.Equal([]byte{1}, proof.KeyValues[0].Key)
	require.Equal([]byte{2}, proof.KeyValues[1].Key)

	require.Equal([]byte{1}, proof.KeyValues[0].Value)
	require.Equal([]byte{2}, proof.KeyValues[1].Value)

	require.Equal([]byte{1}, proof.StartProof[0].KeyPath.Value)

	require.Equal([]byte{}, proof.EndProof[0].KeyPath.Value)
	require.Equal([]byte{0}, proof.EndProof[1].KeyPath.Value)
	require.Equal([]byte{2}, proof.EndProof[2].KeyPath.Value)

	require.NoError(proof.Verify(
		context.Background(),
		[]byte{1},
		maybe.Nothing[[]byte](),
		db.root.id,
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

	proof, err := db.GetRangeProof(context.Background(), []byte("key1"), maybe.Some([]byte("key2")), 10)
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
	require.Equal(newPath([]byte("key1")).Serialize(), proof.StartProof[0].KeyPath)

	require.Len(proof.EndProof, 3)
	require.Equal(newPath([]byte("key2")).Serialize(), proof.EndProof[2].KeyPath)
	require.Equal(newPath([]byte{}).Serialize(), proof.EndProof[0].KeyPath)

	require.NoError(proof.Verify(
		context.Background(),
		[]byte("key1"),
		maybe.Some([]byte("key2")),
		db.root.id,
	))
}

func Test_ChangeProof_Missing_History_For_EndRoot(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	proof, err := db.GetChangeProof(context.Background(), startRoot, ids.Empty, nil, maybe.Nothing[[]byte](), 50)
	require.NoError(err)
	require.NotNil(proof)
	require.False(proof.HadRootsInHistory)

	require.NoError(db.VerifyChangeProof(context.Background(), proof, nil, maybe.Nothing[[]byte](), db.getMerkleRoot()))
}

func Test_ChangeProof_BadBounds(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	require.NoError(db.Insert(context.Background(), []byte{0}, []byte{0}))

	endRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	// non-nil start/end
	proof, err := db.GetChangeProof(context.Background(), startRoot, endRoot, []byte("key4"), maybe.Some([]byte("key3")), 50)
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
	proof, err := db.GetChangeProof(context.Background(), startRoot, endRoot, []byte("key21"), maybe.Some([]byte("key30")), 50)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(dbClone.VerifyChangeProof(context.Background(), proof, []byte("key21"), maybe.Some([]byte("key30")), db.getMerkleRoot()))

	// low maxLength
	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, nil, maybe.Nothing[[]byte](), 5)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(dbClone.VerifyChangeProof(context.Background(), proof, nil, maybe.Nothing[[]byte](), db.getMerkleRoot()))

	// nil start/end
	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, nil, maybe.Nothing[[]byte](), 50)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(dbClone.VerifyChangeProof(context.Background(), proof, nil, maybe.Nothing[[]byte](), endRoot))
	require.NoError(dbClone.CommitChangeProof(context.Background(), proof))

	newRoot, err := dbClone.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(endRoot, newRoot)

	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, []byte("key20"), maybe.Some([]byte("key30")), 50)
	require.NoError(err)
	require.NotNil(proof)

	require.NoError(dbClone.VerifyChangeProof(context.Background(), proof, []byte("key20"), maybe.Some([]byte("key30")), db.getMerkleRoot()))
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
			malform:     func(proof *ChangeProof) {},
			expectedErr: nil,
		},
		{
			name: "odd length key path with value",
			malform: func(proof *ChangeProof) {
				proof.EndProof[1].ValueOrHash = maybe.Some([]byte{1, 2})
			},
			expectedErr: ErrOddLengthWithValue,
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

			proof, err := db.GetChangeProof(context.Background(), startRoot, endRoot, []byte{2}, maybe.Some([]byte{3, 0}), 50)
			require.NoError(err)
			require.NotNil(proof)

			tt.malform(proof)

			err = dbClone.VerifyChangeProof(context.Background(), proof, []byte{2}, maybe.Some([]byte{3, 0}), db.getMerkleRoot())
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func Test_ChangeProof_Syntactic_Verify(t *testing.T) {
	type test struct {
		name        string
		proof       *ChangeProof
		start       []byte
		end         maybe.Maybe[[]byte]
		expectedErr error
	}

	tests := []test{
		{
			name:        "start after end",
			proof:       nil,
			start:       []byte{1},
			end:         maybe.Some([]byte{0}),
			expectedErr: ErrStartAfterEnd,
		},
		{
			name: "no roots in history and non-empty key-values",
			proof: &ChangeProof{
				HadRootsInHistory: false,
				KeyChanges:        []KeyChange{{Key: []byte{1}, Value: maybe.Some([]byte{1})}},
			},
			start:       []byte{0},
			end:         maybe.Nothing[[]byte](), // Also tests start can be after end if end is nothing
			expectedErr: ErrDataInMissingRootProof,
		},
		{
			name: "no roots in history and non-empty deleted keys",
			proof: &ChangeProof{
				HadRootsInHistory: false,
				KeyChanges:        []KeyChange{{Key: []byte{1}}},
			},
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrDataInMissingRootProof,
		},
		{
			name: "no roots in history and non-empty start proof",
			proof: &ChangeProof{
				HadRootsInHistory: false,
				StartProof:        []ProofNode{{}},
			},
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrDataInMissingRootProof,
		},
		{
			name: "no roots in history and non-empty end proof",
			proof: &ChangeProof{
				HadRootsInHistory: false,
				EndProof:          []ProofNode{{}},
			},
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrDataInMissingRootProof,
		},
		{
			name: "no roots in history; empty",
			proof: &ChangeProof{
				HadRootsInHistory: false,
			},
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			expectedErr: nil,
		},
		{
			name: "root in history; empty",
			proof: &ChangeProof{
				HadRootsInHistory: true,
			},
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNoMerkleProof,
		},
		{
			name: "no end proof",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				StartProof:        []ProofNode{{}},
			},
			start:       nil,
			end:         maybe.Some([]byte{1}),
			expectedErr: ErrNoEndProof,
		},
		{
			name: "no start proof",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				KeyChanges:        []KeyChange{{Key: []byte{1}}},
			},
			start:       []byte{1},
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNoStartProof,
		},
		{
			name: "non-increasing key-values",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				KeyChanges: []KeyChange{
					{Key: []byte{1}},
					{Key: []byte{0}},
				},
			},
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name: "key-value too low",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				StartProof:        []ProofNode{{}},
				KeyChanges: []KeyChange{
					{Key: []byte{0}},
				},
			},
			start:       []byte{1},
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name: "key-value too great",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				EndProof:          []ProofNode{{}},
				KeyChanges: []KeyChange{
					{Key: []byte{2}},
				},
			},
			start:       nil,
			end:         maybe.Some([]byte{1}),
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name: "duplicate key",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				KeyChanges: []KeyChange{
					{Key: []byte{1}},
					{Key: []byte{1}},
				},
			},
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name: "start proof node has wrong prefix",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				StartProof: []ProofNode{
					{KeyPath: newPath([]byte{2}).Serialize()},
					{KeyPath: newPath([]byte{2, 3}).Serialize()},
				},
			},
			start:       []byte{1, 2, 3},
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "start proof non-increasing",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				StartProof: []ProofNode{
					{KeyPath: newPath([]byte{1}).Serialize()},
					{KeyPath: newPath([]byte{2, 3}).Serialize()},
				},
			},
			start:       []byte{1, 2, 3},
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "end proof node has wrong prefix",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				KeyChanges: []KeyChange{
					{Key: []byte{1, 2}, Value: maybe.Some([]byte{0})},
				},
				EndProof: []ProofNode{
					{KeyPath: newPath([]byte{2}).Serialize()},
					{KeyPath: newPath([]byte{2, 3}).Serialize()},
				},
			},
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "end proof non-increasing",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				KeyChanges: []KeyChange{
					{Key: []byte{1, 2, 3}},
				},
				EndProof: []ProofNode{
					{KeyPath: newPath([]byte{1}).Serialize()},
					{KeyPath: newPath([]byte{2, 3}).Serialize()},
				},
			},
			start:       nil,
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
		start       []byte
		end         maybe.Maybe[[]byte]
		kvs         []KeyValue
		expectedErr error
	}

	tests := []test{
		{
			name:        "empty",
			start:       nil,
			end:         maybe.Nothing[[]byte](),
			kvs:         nil,
			expectedErr: nil,
		},
		{
			name:  "1 key",
			start: nil,
			end:   maybe.Nothing[[]byte](),
			kvs: []KeyValue{
				{Key: []byte{0}},
			},
			expectedErr: nil,
		},
		{
			name:  "non-increasing keys",
			start: nil,
			end:   maybe.Nothing[[]byte](),
			kvs: []KeyValue{
				{Key: []byte{0}},
				{Key: []byte{0}},
			},
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name:  "key before start",
			start: []byte{1, 2},
			end:   maybe.Nothing[[]byte](),
			kvs: []KeyValue{
				{Key: []byte{1}},
				{Key: []byte{1, 2}},
			},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name:  "key after end",
			start: nil,
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
			start: nil,
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
		proofKey    []byte
		expectedErr error
	}

	tests := []test{
		{
			name:        "empty",
			path:        nil,
			proofKey:    nil,
			expectedErr: nil,
		},
		{
			name:        "1 element",
			path:        []ProofNode{{}},
			proofKey:    nil,
			expectedErr: nil,
		},
		{
			name: "non-increasing keys",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 3}).Serialize()},
			},
			proofKey:    []byte{1, 2, 3},
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "invalid key",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 4}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 3}).Serialize()},
			},
			proofKey:    []byte{1, 2, 3},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "extra node inclusion proof",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 3}).Serialize()},
			},
			proofKey:    []byte{1, 2},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "extra node exclusion proof",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 3}).Serialize()},
				{KeyPath: newPath([]byte{1, 3, 4}).Serialize()},
			},
			proofKey:    []byte{1, 2},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "happy path exclusion proof",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 4}).Serialize()},
			},
			proofKey:    []byte{1, 2, 3},
			expectedErr: nil,
		},
		{
			name: "happy path inclusion proof",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 3}).Serialize()},
			},
			proofKey:    []byte{1, 2, 3},
			expectedErr: nil,
		},
		{
			name: "repeat nodes",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 3}).Serialize()},
			},
			proofKey:    []byte{1, 2, 3},
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "repeat nodes 2",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 3}).Serialize()},
			},
			proofKey:    []byte{1, 2, 3},
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "repeat nodes 3",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 3}).Serialize()},
				{KeyPath: newPath([]byte{1, 2, 3}).Serialize()},
			},
			proofKey:    []byte{1, 2, 3},
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "oddLength key with value",
			path: []ProofNode{
				{KeyPath: newPath([]byte{1}).Serialize()},
				{KeyPath: newPath([]byte{1, 2}).Serialize()},
				{KeyPath: SerializedPath{Value: []byte{1, 2, 240}, NibbleLength: 5}, ValueOrHash: maybe.Some([]byte{1})},
			},
			proofKey:    []byte{1, 2, 3},
			expectedErr: ErrOddLengthWithValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyProofPath(tt.path, newPath(tt.proofKey))
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
	protoNode.Children[NodeBranchFactor] = childID[:]

	var unmarshaledNode ProofNode
	err := unmarshaledNode.UnmarshalProto(protoNode)
	require.ErrorIs(t, err, ErrInvalidChildIndex)
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
			expectedErr: ErrNilSerializedPath,
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

func TestProofNodeProtoMarshalUnmarshal(t *testing.T) {
	require := require.New(t)
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	for i := 0; i < 1_000; i++ {
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
	}
}

func TestRangeProofProtoMarshalUnmarshal(t *testing.T) {
	require := require.New(t)
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	for i := 0; i < 500; i++ {
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
	}
}

func TestChangeProofProtoMarshalUnmarshal(t *testing.T) {
	require := require.New(t)
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	for i := 0; i < 500; i++ {
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
	}
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

func TestProofProtoMarshalUnmarshal(t *testing.T) {
	require := require.New(t)
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	for i := 0; i < 500; i++ {
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
			Key:   key,
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
	}
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
	now := time.Now().UnixNano()
	f.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	var (
		numKeyValues  = 2048
		deletePortion = 0.25
	)

	db, err := getBasicDB()
	require.NoError(f, err)

	// Insert a bunch of random key values.
	insertRandomKeyValues(
		require.New(f),
		rand,
		[]database.Database{db},
		numKeyValues,
		deletePortion,
	)

	f.Fuzz(func(
		t *testing.T,
		start []byte,
		endBytes []byte,
		maxProofLen uint,
	) {
		require := require.New(t)

		// Make sure proof bounds are valid
		if len(endBytes) != 0 && bytes.Compare(start, endBytes) > 0 {
			return
		}
		end := maybe.Nothing[[]byte]()
		if len(endBytes) > 0 {
			end = maybe.Some(endBytes)
		}
		// Make sure proof length is valid
		if maxProofLen == 0 {
			return
		}

		rangeProof, err := db.GetRangeProof(
			context.Background(),
			start,
			end,
			int(maxProofLen),
		)
		require.NoError(err)

		// Make sure the start proof doesn't contain any nodes
		// that are in the end proof.
		endProofKeys := set.Set[path]{}
		for _, node := range rangeProof.EndProof {
			path := node.KeyPath.deserialize()
			endProofKeys.Add(path)
		}

		for _, node := range rangeProof.StartProof {
			path := node.KeyPath.deserialize()
			require.NotContains(endProofKeys, path)
		}

		// Make sure the EndProof invariant is maintained
		switch {
		case end.IsNothing():
			if len(rangeProof.KeyValues) == 0 {
				if len(rangeProof.StartProof) == 0 {
					require.Len(rangeProof.EndProof, 1) // Just the root
					require.Empty(rangeProof.EndProof[0].KeyPath)
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
				Key:   endBytes,
				Value: value,
			}

			rootID, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			require.NoError(proof.Verify(context.Background(), rootID))
		default:
			require.NotEmpty(rangeProof.EndProof)

			greatestKV := rangeProof.KeyValues[len(rangeProof.KeyValues)-1]
			// EndProof should be a proof for largest key-value.
			proof := Proof{
				Path:  rangeProof.EndProof,
				Key:   greatestKV.Key,
				Value: maybe.Some(greatestKV.Value),
			}

			rootID, err := db.GetMerkleRoot(context.Background())
			require.NoError(err)

			require.NoError(proof.Verify(context.Background(), rootID))
		}
	})
}

func FuzzProof(f *testing.F) {
	now := time.Now().UnixNano()
	f.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now)) // #nosec G404

	var (
		numKeyValues  = 2048
		deletePortion = 0.25
	)

	db, err := getBasicDB()
	require.NoError(f, err)

	// Insert a bunch of random key values.
	insertRandomKeyValues(
		require.New(f),
		rand,
		[]database.Database{db},
		numKeyValues,
		deletePortion,
	)

	f.Fuzz(func(
		t *testing.T,
		key []byte,
	) {
		require := require.New(t)

		proof, err := db.GetProof(
			context.Background(),
			key,
		)
		require.NoError(err)

		rootID, err := db.GetMerkleRoot(context.Background())
		require.NoError(err)

		require.NoError(proof.Verify(context.Background(), rootID))

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
