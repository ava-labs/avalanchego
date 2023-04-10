// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func getBasicDB() (*Database, error) {
	return newDatabase(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 1000,
			NodeCacheSize: 1000,
		},
		&mockMetrics{},
	)
}

func writeBasicBatch(t *testing.T, db *Database) {
	batch := db.NewBatch()
	require.NoError(t, batch.Put([]byte{0}, []byte{0}))
	require.NoError(t, batch.Put([]byte{1}, []byte{1}))
	require.NoError(t, batch.Put([]byte{2}, []byte{2}))
	require.NoError(t, batch.Put([]byte{3}, []byte{3}))
	require.NoError(t, batch.Put([]byte{4}, []byte{4}))
	require.NoError(t, batch.Write())
}

func Test_Proof_Marshal(t *testing.T) {
	require := require.New(t)
	dbTrie, err := getBasicDB()
	require.NoError(err)
	require.NotNil(dbTrie)
	writeBasicBatch(t, dbTrie)

	proof, err := dbTrie.GetProof(context.Background(), []byte{1})
	require.NoError(err)
	require.NotNil(proof)

	proofBytes, err := Codec.EncodeProof(Version, proof)
	require.NoError(err)

	parsedProof := &Proof{}
	_, err = Codec.DecodeProof(proofBytes, parsedProof)
	require.NoError(err)

	verifyPath(t, proof.Path, parsedProof.Path)
	require.Equal([]byte{1}, proof.Value.value)
}

func Test_Proof_Empty(t *testing.T) {
	proof := &Proof{}
	err := proof.Verify(context.Background(), ids.Empty)
	require.ErrorIs(t, err, ErrNoProof)
}

func Test_Proof_MissingValue(t *testing.T) {
	trie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, trie)

	require.NoError(t, trie.Insert(context.Background(), []byte{1}, []byte{0}))
	require.NoError(t, trie.Insert(context.Background(), []byte{1, 2}, []byte{0}))
	require.NoError(t, trie.Insert(context.Background(), []byte{1, 2, 4}, []byte{0}))
	require.NoError(t, trie.Insert(context.Background(), []byte{1, 3}, []byte{0}))

	// get a proof for a value not in the db
	proof, err := trie.GetProof(context.Background(), []byte{1, 2, 3})
	require.NoError(t, err)
	require.NotNil(t, proof)

	require.True(t, proof.Value.IsNothing())

	proofBytes, err := Codec.EncodeProof(Version, proof)
	require.NoError(t, err)

	parsedProof := &Proof{}
	_, err = Codec.DecodeProof(proofBytes, parsedProof)
	require.NoError(t, err)

	verifyPath(t, proof.Path, parsedProof.Path)
}

func Test_Proof_Marshal_Errors(t *testing.T) {
	trie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, trie)

	writeBasicBatch(t, trie)

	proof, err := trie.GetProof(context.Background(), []byte{1})
	require.NoError(t, err)
	require.NotNil(t, proof)

	proofBytes, err := Codec.EncodeProof(Version, proof)
	require.NoError(t, err)

	for i := 1; i < len(proofBytes); i++ {
		broken := proofBytes[:i]
		parsed := &Proof{}
		_, err = Codec.DecodeProof(broken, parsed)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}

	// add a child at an invalid index
	proof.Path[0].Children[255] = ids.Empty
	_, err = Codec.EncodeProof(Version, proof)
	require.ErrorIs(t, err, errChildIndexTooLarge)
}

func verifyPath(t *testing.T, path1, path2 []ProofNode) {
	require.Equal(t, len(path1), len(path2))
	for i := range path1 {
		require.True(t, bytes.Equal(path1[i].KeyPath.Value, path2[i].KeyPath.Value))
		require.Equal(t, path1[i].KeyPath.hasOddLength(), path2[i].KeyPath.hasOddLength())
		require.True(t, bytes.Equal(path1[i].ValueOrHash.value, path2[i].ValueOrHash.value))
		for childIndex := range path1[i].Children {
			require.Equal(t, path1[i].Children[childIndex], path2[i].Children[childIndex])
		}
	}
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
				proof.Path[1].ValueOrHash = Some([]byte{1, 2})
			},
			expectedErr: ErrOddLengthWithValue,
		},
		{
			name: "last proof node has missing value",
			malform: func(proof *Proof) {
				proof.Path[len(proof.Path)-1].ValueOrHash = Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "missing value on proof",
			malform: func(proof *Proof) {
				proof.Value = Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "mismatched value on proof",
			malform: func(proof *Proof) {
				proof.Value = Some([]byte{10})
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
			db, err := getBasicDB()
			require.NoError(t, err)

			writeBasicBatch(t, db)

			proof, err := db.GetProof(context.Background(), []byte{2})
			require.NoError(t, err)
			require.NotNil(t, proof)

			tt.malform(proof)

			err = proof.Verify(context.Background(), db.getMerkleRoot())
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func Test_Proof_ValueOrHashMatches(t *testing.T) {
	require.True(t, valueOrHashMatches(Some([]byte{0}), Some([]byte{0})))
	require.False(t, valueOrHashMatches(Nothing[[]byte](), Some(hashing.ComputeHash256([]byte{0}))))
	require.True(t, valueOrHashMatches(Nothing[[]byte](), Nothing[[]byte]()))

	require.False(t, valueOrHashMatches(Some([]byte{0}), Nothing[[]byte]()))
	require.False(t, valueOrHashMatches(Nothing[[]byte](), Some([]byte{0})))
	require.False(t, valueOrHashMatches(Nothing[[]byte](), Some(hashing.ComputeHash256([]byte{1}))))
	require.False(t, valueOrHashMatches(Some(hashing.ComputeHash256([]byte{0})), Nothing[[]byte]()))
}

func Test_RangeProof_Extra_Value(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	writeBasicBatch(t, db)

	val, err := db.Get([]byte{2})
	require.NoError(t, err)
	require.Equal(t, []byte{2}, val)

	proof, err := db.GetRangeProof(context.Background(), []byte{1}, []byte{5, 5}, 10)
	require.NoError(t, err)
	require.NotNil(t, proof)

	err = proof.Verify(
		context.Background(),
		[]byte{1},
		[]byte{5, 5},
		db.root.id,
	)
	require.NoError(t, err)

	proof.KeyValues = append(proof.KeyValues, KeyValue{Key: []byte{5}, Value: []byte{5}})

	err = proof.Verify(
		context.Background(),
		[]byte{1},
		[]byte{5, 5},
		db.root.id,
	)
	require.ErrorIs(t, err, ErrInvalidProof)
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
				proof.StartProof[len(proof.StartProof)-1].ValueOrHash = Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "EndProof: odd length key path with value",
			malform: func(proof *RangeProof) {
				proof.EndProof[1].ValueOrHash = Some([]byte{1, 2})
			},
			expectedErr: ErrOddLengthWithValue,
		},
		{
			name: "EndProof: last proof node has missing value",
			malform: func(proof *RangeProof) {
				proof.EndProof[len(proof.EndProof)-1].ValueOrHash = Nothing[[]byte]()
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
			db, err := getBasicDB()
			require.NoError(t, err)
			writeBasicBatch(t, db)

			proof, err := db.GetRangeProof(context.Background(), []byte{2}, []byte{3, 0}, 50)
			require.NoError(t, err)
			require.NotNil(t, proof)

			tt.malform(proof)

			err = proof.Verify(context.Background(), []byte{2}, []byte{3, 0}, db.getMerkleRoot())
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func Test_RangeProof_MaxLength(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView()
	require.NoError(t, err)

	_, err = trie.GetRangeProof(context.Background(), nil, nil, -1)
	require.ErrorIs(t, err, ErrInvalidMaxLength)

	_, err = trie.GetRangeProof(context.Background(), nil, nil, 0)
	require.ErrorIs(t, err, ErrInvalidMaxLength)
}

func Test_Proof(t *testing.T) {
	dbTrie, err := getBasicDB()
	require.NoError(t, err)
	require.NotNil(t, dbTrie)
	trie, err := dbTrie.NewView()
	require.NoError(t, err)

	err = trie.Insert(context.Background(), []byte("key0"), []byte("value0"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key1"), []byte("value1"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key2"), []byte("value2"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key3"), []byte("value3"))
	require.NoError(t, err)
	err = trie.Insert(context.Background(), []byte("key4"), []byte("value4"))
	require.NoError(t, err)

	_, err = trie.GetMerkleRoot(context.Background())
	require.NoError(t, err)
	proof, err := trie.GetProof(context.Background(), []byte("key1"))
	require.NoError(t, err)
	require.NotNil(t, proof)

	require.Len(t, proof.Path, 3)

	require.Equal(t, newPath([]byte("key1")).Serialize(), proof.Path[2].KeyPath)
	require.Equal(t, Some([]byte("value1")), proof.Path[2].ValueOrHash)

	require.Equal(t, newPath([]byte{}).Serialize(), proof.Path[0].KeyPath)
	require.True(t, proof.Path[0].ValueOrHash.IsNothing())

	expectedRootID, err := trie.GetMerkleRoot(context.Background())
	require.NoError(t, err)
	err = proof.Verify(context.Background(), expectedRootID)
	require.NoError(t, err)

	proof.Path[0].ValueOrHash = Some([]byte("value2"))

	err = proof.Verify(context.Background(), expectedRootID)
	require.ErrorIs(t, err, ErrInvalidProof)
}

func Test_RangeProof_Syntactic_Verify(t *testing.T) {
	type test struct {
		name        string
		start       []byte
		end         []byte
		proof       *RangeProof
		expectedErr error
	}

	tests := []test{
		{
			name:        "start > end",
			start:       []byte{1},
			end:         []byte{0},
			proof:       &RangeProof{},
			expectedErr: ErrStartAfterEnd,
		},
		{
			name:        "empty", // Also tests start can be > end if end is nil
			start:       []byte{1},
			end:         nil,
			proof:       &RangeProof{},
			expectedErr: ErrNoMerkleProof,
		},
		{
			name:  "should just be root",
			start: nil,
			end:   nil,
			proof: &RangeProof{
				EndProof: []ProofNode{{}, {}},
			},
			expectedErr: ErrShouldJustBeRoot,
		},
		{
			name:  "no end proof",
			start: []byte{1},
			end:   []byte{1},
			proof: &RangeProof{
				KeyValues: []KeyValue{{Key: []byte{1}, Value: []byte{1}}},
			},
			expectedErr: ErrNoEndProof,
		},
		{
			name:  "unsorted key values",
			start: []byte{1},
			end:   nil,
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
			end:   nil,
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
			end:   []byte{1},
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
			end:   nil,
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
			end:   nil,
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
			end:   []byte{1, 2},
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
			end:   []byte{1, 2},
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
			require := require.New(t)

			err := tt.proof.Verify(context.Background(), tt.start, tt.end, ids.Empty)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func Test_RangeProof(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	writeBasicBatch(t, db)

	proof, err := db.GetRangeProof(context.Background(), []byte{1}, []byte{3, 5}, 10)
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

	err = proof.Verify(
		context.Background(),
		[]byte{1},
		[]byte{3, 5},
		db.root.id,
	)
	require.NoError(err)
}

func Test_RangeProof_BadBounds(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	// non-nil start/end
	proof, err := db.GetRangeProof(context.Background(), []byte{4}, []byte{3}, 50)
	require.ErrorIs(t, err, ErrStartAfterEnd)
	require.Nil(t, proof)
}

func Test_RangeProof_NilStart(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key2"), []byte("value2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key3"), []byte("value3"))
	require.NoError(t, err)
	err = batch.Put([]byte("key4"), []byte("value4"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	val, err := db.Get([]byte("key1"))
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), val)

	proof, err := db.GetRangeProof(context.Background(), nil, []byte("key35"), 2)
	require.NoError(t, err)
	require.NotNil(t, proof)

	require.Len(t, proof.KeyValues, 2)

	require.Equal(t, []byte("key1"), proof.KeyValues[0].Key)
	require.Equal(t, []byte("key2"), proof.KeyValues[1].Key)

	require.Equal(t, []byte("value1"), proof.KeyValues[0].Value)
	require.Equal(t, []byte("value2"), proof.KeyValues[1].Value)

	require.Equal(t, newPath([]byte("key2")).Serialize(), proof.EndProof[2].KeyPath)
	require.Equal(t, SerializedPath{Value: []uint8{0x6b, 0x65, 0x79, 0x30}, NibbleLength: 7}, proof.EndProof[1].KeyPath)
	require.Equal(t, newPath([]byte("")).Serialize(), proof.EndProof[0].KeyPath)

	err = proof.Verify(
		context.Background(),
		nil,
		[]byte("key35"),
		db.root.id,
	)
	require.NoError(t, err)
}

func Test_RangeProof_NilEnd(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	writeBasicBatch(t, db)
	require.NoError(t, err)

	proof, err := db.GetRangeProof(context.Background(), []byte{1}, nil, 2)
	require.NoError(t, err)
	require.NotNil(t, proof)

	require.Len(t, proof.KeyValues, 2)

	require.Equal(t, []byte{1}, proof.KeyValues[0].Key)
	require.Equal(t, []byte{2}, proof.KeyValues[1].Key)

	require.Equal(t, []byte{1}, proof.KeyValues[0].Value)
	require.Equal(t, []byte{2}, proof.KeyValues[1].Value)

	require.Equal(t, []byte{1}, proof.StartProof[0].KeyPath.Value)

	require.Equal(t, []byte{}, proof.EndProof[0].KeyPath.Value)
	require.Equal(t, []byte{0}, proof.EndProof[1].KeyPath.Value)
	require.Equal(t, []byte{2}, proof.EndProof[2].KeyPath.Value)

	err = proof.Verify(
		context.Background(),
		[]byte{1},
		nil,
		db.root.id,
	)
	require.NoError(t, err)
}

func Test_RangeProof_EmptyValues(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key1"), nil)
	require.NoError(t, err)
	err = batch.Put([]byte("key12"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key2"), []byte{})
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	val, err := db.Get([]byte("key12"))
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), val)

	proof, err := db.GetRangeProof(context.Background(), []byte("key1"), []byte("key2"), 10)
	require.NoError(t, err)
	require.NotNil(t, proof)

	require.Len(t, proof.KeyValues, 3)
	require.Equal(t, []byte("key1"), proof.KeyValues[0].Key)
	require.Empty(t, proof.KeyValues[0].Value)
	require.Equal(t, []byte("key12"), proof.KeyValues[1].Key)
	require.Equal(t, []byte("value1"), proof.KeyValues[1].Value)
	require.Equal(t, []byte("key2"), proof.KeyValues[2].Key)
	require.Empty(t, proof.KeyValues[2].Value)

	require.Len(t, proof.StartProof, 1)
	require.Equal(t, newPath([]byte("key1")).Serialize(), proof.StartProof[0].KeyPath)

	require.Len(t, proof.EndProof, 3)
	require.Equal(t, newPath([]byte("key2")).Serialize(), proof.EndProof[2].KeyPath)
	require.Equal(t, newPath([]byte{}).Serialize(), proof.EndProof[0].KeyPath)

	err = proof.Verify(
		context.Background(),
		[]byte("key1"),
		[]byte("key2"),
		db.root.id,
	)
	require.NoError(t, err)
}

func Test_RangeProof_Marshal_Nil(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	writeBasicBatch(t, db)

	val, err := db.Get([]byte{1})
	require.NoError(t, err)
	require.Equal(t, []byte{1}, val)

	proof, err := db.GetRangeProof(context.Background(), []byte("key1"), []byte("key35"), 10)
	require.NoError(t, err)
	require.NotNil(t, proof)

	proofBytes, err := Codec.EncodeRangeProof(Version, proof)
	require.NoError(t, err)

	parsedProof := &RangeProof{}
	_, err = Codec.DecodeRangeProof(proofBytes, parsedProof)
	require.NoError(t, err)

	verifyPath(t, proof.StartProof, parsedProof.StartProof)
	verifyPath(t, proof.EndProof, parsedProof.EndProof)

	for index, kv := range proof.KeyValues {
		require.True(t, bytes.Equal(kv.Key, parsedProof.KeyValues[index].Key))
		require.True(t, bytes.Equal(kv.Value, parsedProof.KeyValues[index].Value))
	}
}

func Test_RangeProof_Marshal(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writeBasicBatch(t, db)

	val, err := db.Get([]byte{1})
	require.NoError(t, err)
	require.Equal(t, []byte{1}, val)

	proof, err := db.GetRangeProof(context.Background(), nil, nil, 10)
	require.NoError(t, err)
	require.NotNil(t, proof)

	proofBytes, err := Codec.EncodeRangeProof(Version, proof)
	require.NoError(t, err)

	parsedProof := &RangeProof{}
	_, err = Codec.DecodeRangeProof(proofBytes, parsedProof)
	require.NoError(t, err)

	verifyPath(t, proof.StartProof, parsedProof.StartProof)
	verifyPath(t, proof.EndProof, parsedProof.EndProof)

	for index, state := range proof.KeyValues {
		require.True(t, bytes.Equal(state.Key, parsedProof.KeyValues[index].Key))
		require.True(t, bytes.Equal(state.Value, parsedProof.KeyValues[index].Value))
	}
}

func Test_RangeProof_Marshal_Errors(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	writeBasicBatch(t, db)

	proof, err := db.GetRangeProof(context.Background(), nil, nil, 10)
	require.NoError(t, err)
	require.NotNil(t, proof)

	proofBytes, err := Codec.EncodeRangeProof(Version, proof)
	require.NoError(t, err)

	for i := 1; i < len(proofBytes); i++ {
		broken := proofBytes[:i]
		parsedProof := &RangeProof{}
		_, err = Codec.DecodeRangeProof(broken, parsedProof)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}

func TestChangeProofGetLargestKey(t *testing.T) {
	type test struct {
		name     string
		proof    ChangeProof
		end      []byte
		expected []byte
	}

	tests := []test{
		{
			name:     "empty proof",
			proof:    ChangeProof{},
			end:      []byte{0},
			expected: []byte{0},
		},
		{
			name: "1 KV no deleted keys",
			proof: ChangeProof{
				KeyValues: []KeyValue{
					{
						Key: []byte{1},
					},
				},
			},
			end:      []byte{0},
			expected: []byte{1},
		},
		{
			name: "2 KV no deleted keys",
			proof: ChangeProof{
				KeyValues: []KeyValue{
					{
						Key: []byte{1},
					},
					{
						Key: []byte{2},
					},
				},
			},
			end:      []byte{0},
			expected: []byte{2},
		},
		{
			name: "no KVs 1 deleted key",
			proof: ChangeProof{
				DeletedKeys: [][]byte{{1}},
			},
			end:      []byte{0},
			expected: []byte{1},
		},
		{
			name: "no KVs 2 deleted keys",
			proof: ChangeProof{
				DeletedKeys: [][]byte{{1}, {2}},
			},
			end:      []byte{0},
			expected: []byte{2},
		},
		{
			name: "KV and deleted keys; KV larger",
			proof: ChangeProof{
				KeyValues: []KeyValue{
					{
						Key: []byte{1},
					},
					{
						Key: []byte{3},
					},
				},
				DeletedKeys: [][]byte{{0}, {2}},
			},
			end:      []byte{5},
			expected: []byte{3},
		},
		{
			name: "KV and deleted keys; deleted key larger",
			proof: ChangeProof{
				KeyValues: []KeyValue{
					{
						Key: []byte{0},
					},
					{
						Key: []byte{2},
					},
				},
				DeletedKeys: [][]byte{{1}, {3}},
			},
			end:      []byte{5},
			expected: []byte{3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.proof.getLargestKey(tt.end))
		})
	}
}

func Test_ChangeProof_Marshal(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key0"), []byte("value0"))
	require.NoError(t, err)
	err = batch.Put([]byte("key1"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key2"), []byte("value2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key3"), []byte("value3"))
	require.NoError(t, err)
	err = batch.Put([]byte("key4"), []byte("value4"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)
	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key4"), []byte("value0"))
	require.NoError(t, err)
	err = batch.Put([]byte("key5"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key6"), []byte("value2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key7"), []byte("value3"))
	require.NoError(t, err)
	err = batch.Put([]byte("key8"), []byte("value4"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key9"), []byte("value0"))
	require.NoError(t, err)
	err = batch.Put([]byte("key10"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key11"), []byte("value2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key12"), []byte("value3"))
	require.NoError(t, err)
	err = batch.Put([]byte("key13"), []byte("value4"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)
	endroot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	proof, err := db.GetChangeProof(context.Background(), startRoot, endroot, nil, nil, 50)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.True(t, proof.HadRootsInHistory)

	proofBytes, err := Codec.EncodeChangeProof(Version, proof)
	require.NoError(t, err)

	parsedProof := &ChangeProof{}
	_, err = Codec.DecodeChangeProof(proofBytes, parsedProof)
	require.NoError(t, err)

	verifyPath(t, proof.StartProof, parsedProof.StartProof)
	verifyPath(t, proof.EndProof, parsedProof.EndProof)

	for index, kv := range proof.KeyValues {
		require.True(t, bytes.Equal(kv.Key, parsedProof.KeyValues[index].Key))
		require.True(t, bytes.Equal(kv.Value, parsedProof.KeyValues[index].Value))
	}
}

func Test_ChangeProof_Marshal_Errors(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	writeBasicBatch(t, db)
	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	batch := db.NewBatch()
	require.NoError(t, batch.Put([]byte{5}, []byte{5}))
	require.NoError(t, batch.Put([]byte{6}, []byte{6}))
	require.NoError(t, batch.Put([]byte{7}, []byte{7}))
	require.NoError(t, batch.Put([]byte{8}, []byte{8}))
	require.NoError(t, batch.Delete([]byte{0}))
	require.NoError(t, batch.Write())

	batch = db.NewBatch()
	require.NoError(t, batch.Put([]byte{9}, []byte{9}))
	require.NoError(t, batch.Put([]byte{10}, []byte{10}))
	require.NoError(t, batch.Put([]byte{11}, []byte{11}))
	require.NoError(t, batch.Put([]byte{12}, []byte{12}))
	require.NoError(t, batch.Delete([]byte{1}))
	require.NoError(t, batch.Write())
	endroot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	proof, err := db.GetChangeProof(context.Background(), startRoot, endroot, nil, nil, 50)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.True(t, proof.HadRootsInHistory)
	require.Len(t, proof.KeyValues, 8)
	require.Len(t, proof.DeletedKeys, 2)

	proofBytes, err := Codec.EncodeChangeProof(Version, proof)
	require.NoError(t, err)

	for i := 1; i < len(proofBytes); i++ {
		broken := proofBytes[:i]
		parsedProof := &ChangeProof{}
		_, err = Codec.DecodeChangeProof(broken, parsedProof)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}

func Test_ChangeProof_Missing_History_For_EndRoot(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	proof, err := db.GetChangeProof(context.Background(), startRoot, ids.Empty, nil, nil, 50)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.False(t, proof.HadRootsInHistory)

	require.NoError(t, proof.Verify(context.Background(), db, nil, nil, db.getMerkleRoot()))
}

func Test_ChangeProof_BadBounds(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	require.NoError(t, db.Insert(context.Background(), []byte{0}, []byte{0}))

	endRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	// non-nil start/end
	proof, err := db.GetChangeProof(context.Background(), startRoot, endRoot, []byte("key4"), []byte("key3"), 50)
	require.ErrorIs(t, err, ErrStartAfterEnd)
	require.Nil(t, proof)
}

func Test_ChangeProof_Verify(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	batch := db.NewBatch()
	err = batch.Put([]byte("key20"), []byte("value0"))
	require.NoError(t, err)
	err = batch.Put([]byte("key21"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key22"), []byte("value2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key23"), []byte("value3"))
	require.NoError(t, err)
	err = batch.Put([]byte("key24"), []byte("value4"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)
	startRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	// create a second db that has "synced" to the start root
	dbClone, err := getBasicDB()
	require.NoError(t, err)
	batch = dbClone.NewBatch()
	err = batch.Put([]byte("key20"), []byte("value0"))
	require.NoError(t, err)
	err = batch.Put([]byte("key21"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key22"), []byte("value2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key23"), []byte("value3"))
	require.NoError(t, err)
	err = batch.Put([]byte("key24"), []byte("value4"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	// the second db has started to sync some of the range outside of the range proof
	batch = dbClone.NewBatch()
	err = batch.Put([]byte("key31"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key25"), []byte("value0"))
	require.NoError(t, err)
	err = batch.Put([]byte("key26"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key27"), []byte("value2"))
	require.NoError(t, err)
	err = batch.Put([]byte("key28"), []byte("value3"))
	require.NoError(t, err)
	err = batch.Put([]byte("key29"), []byte("value4"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	batch = db.NewBatch()
	err = batch.Put([]byte("key30"), []byte("value0"))
	require.NoError(t, err)
	err = batch.Put([]byte("key31"), []byte("value1"))
	require.NoError(t, err)
	err = batch.Put([]byte("key32"), []byte("value2"))
	require.NoError(t, err)
	err = batch.Delete([]byte("key21"))
	require.NoError(t, err)
	err = batch.Delete([]byte("key22"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	endRoot, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	// non-nil start/end
	proof, err := db.GetChangeProof(context.Background(), startRoot, endRoot, []byte("key21"), []byte("key30"), 50)
	require.NoError(t, err)
	require.NotNil(t, proof)

	err = proof.Verify(context.Background(), dbClone, []byte("key21"), []byte("key30"), db.getMerkleRoot())
	require.NoError(t, err)

	// low maxLength
	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, nil, nil, 5)
	require.NoError(t, err)
	require.NotNil(t, proof)

	err = proof.Verify(context.Background(), dbClone, nil, nil, db.getMerkleRoot())
	require.NoError(t, err)

	// nil start/end
	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, nil, nil, 50)
	require.NoError(t, err)
	require.NotNil(t, proof)

	err = proof.Verify(context.Background(), dbClone, nil, nil, endRoot)
	require.NoError(t, err)

	err = dbClone.CommitChangeProof(context.Background(), proof)
	require.NoError(t, err)

	newRoot, err := dbClone.GetMerkleRoot(context.Background())
	require.NoError(t, err)
	require.Equal(t, endRoot, newRoot)

	proof, err = db.GetChangeProof(context.Background(), startRoot, endRoot, []byte("key20"), []byte("key30"), 50)
	require.NoError(t, err)
	require.NotNil(t, proof)

	err = proof.Verify(context.Background(), dbClone, []byte("key20"), []byte("key30"), db.getMerkleRoot())
	require.NoError(t, err)
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
				proof.EndProof[1].ValueOrHash = Some([]byte{1, 2})
			},
			expectedErr: ErrOddLengthWithValue,
		},
		{
			name: "last proof node has missing value",
			malform: func(proof *ChangeProof) {
				proof.EndProof[len(proof.EndProof)-1].ValueOrHash = Nothing[[]byte]()
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
		{
			name: "missing key/value",
			malform: func(proof *ChangeProof) {
				proof.KeyValues = proof.KeyValues[1:]
			},
			expectedErr: ErrProofValueDoesntMatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := getBasicDB()
			require.NoError(t, err)

			startRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(t, err)

			writeBasicBatch(t, db)

			endRoot, err := db.GetMerkleRoot(context.Background())
			require.NoError(t, err)

			// create a second db that will be synced to the first db
			dbClone, err := getBasicDB()
			require.NoError(t, err)

			proof, err := db.GetChangeProof(context.Background(), startRoot, endRoot, []byte{2}, []byte{3, 0}, 50)
			require.NoError(t, err)
			require.NotNil(t, proof)

			tt.malform(proof)

			err = proof.Verify(context.Background(), dbClone, []byte{2}, []byte{3, 0}, db.getMerkleRoot())
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func Test_ChangeProof_Syntactic_Verify(t *testing.T) {
	type test struct {
		name        string
		proof       *ChangeProof
		start       []byte
		end         []byte
		expectedErr error
	}

	tests := []test{
		{
			name:        "start after end",
			proof:       nil,
			start:       []byte{1},
			end:         []byte{0},
			expectedErr: ErrStartAfterEnd,
		},
		{
			name: "no roots in history and non-empty key-values",
			proof: &ChangeProof{
				HadRootsInHistory: false,
				KeyValues:         []KeyValue{{Key: []byte{1}, Value: []byte{1}}},
			},
			start:       []byte{0},
			end:         nil, // Also tests start can be after end if end is nil
			expectedErr: ErrDataInMissingRootProof,
		},
		{
			name: "no roots in history and non-empty deleted keys",
			proof: &ChangeProof{
				HadRootsInHistory: false,
				DeletedKeys:       [][]byte{{1}},
			},
			start:       nil,
			end:         nil,
			expectedErr: ErrDataInMissingRootProof,
		},
		{
			name: "no roots in history and non-empty start proof",
			proof: &ChangeProof{
				HadRootsInHistory: false,
				StartProof:        []ProofNode{{}},
			},
			start:       nil,
			end:         nil,
			expectedErr: ErrDataInMissingRootProof,
		},
		{
			name: "no roots in history and non-empty end proof",
			proof: &ChangeProof{
				HadRootsInHistory: false,
				EndProof:          []ProofNode{{}},
			},
			start:       nil,
			end:         nil,
			expectedErr: ErrDataInMissingRootProof,
		},
		{
			name: "no roots in history; empty",
			proof: &ChangeProof{
				HadRootsInHistory: false,
			},
			start:       nil,
			end:         nil,
			expectedErr: nil,
		},
		{
			name: "root in history; empty",
			proof: &ChangeProof{
				HadRootsInHistory: true,
			},
			start:       nil,
			end:         nil,
			expectedErr: ErrNoMerkleProof,
		},
		{
			name: "no end proof",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				StartProof:        []ProofNode{{}},
			},
			start:       nil,
			end:         []byte{1},
			expectedErr: ErrNoEndProof,
		},
		{
			name: "no start proof",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				DeletedKeys:       [][]byte{{1}},
			},
			start:       []byte{1},
			end:         nil,
			expectedErr: ErrNoStartProof,
		},
		{
			name: "non-increasing key-values",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				KeyValues: []KeyValue{
					{Key: []byte{1}},
					{Key: []byte{0}},
				},
			},
			start:       nil,
			end:         nil,
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name: "key-value too low",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				StartProof:        []ProofNode{{}},
				KeyValues: []KeyValue{
					{Key: []byte{0}},
				},
			},
			start:       []byte{1},
			end:         nil,
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name: "key-value too great",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				EndProof:          []ProofNode{{}},
				KeyValues: []KeyValue{
					{Key: []byte{2}},
				},
			},
			start:       nil,
			end:         []byte{1},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name: "non-increasing deleted keys",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				DeletedKeys: [][]byte{
					{1},
					{1},
				},
			},
			start:       nil,
			end:         nil,
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name: "deleted key too low",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				StartProof:        []ProofNode{{}},
				DeletedKeys: [][]byte{
					{0},
				},
			},
			start:       []byte{1},
			end:         nil,
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name: "deleted key too great",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				EndProof:          []ProofNode{{}},
				DeletedKeys: [][]byte{
					{1},
				},
			},
			start:       nil,
			end:         []byte{0},
			expectedErr: ErrStateFromOutsideOfRange,
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
			end:         nil,
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
			end:         nil,
			expectedErr: ErrNonIncreasingProofNodes,
		},
		{
			name: "end proof node has wrong prefix",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				KeyValues: []KeyValue{
					{Key: []byte{1, 2}}, // Also tests [end] set to greatest key-value/deleted key
				},
				EndProof: []ProofNode{
					{KeyPath: newPath([]byte{2}).Serialize()},
					{KeyPath: newPath([]byte{2, 3}).Serialize()},
				},
			},
			start:       nil,
			end:         nil,
			expectedErr: ErrProofNodeNotForKey,
		},
		{
			name: "end proof non-increasing",
			proof: &ChangeProof{
				HadRootsInHistory: true,
				DeletedKeys: [][]byte{
					{1, 2, 3}, // Also tests [end] set to greatest key-value/deleted key
				},
				EndProof: []ProofNode{
					{KeyPath: newPath([]byte{1}).Serialize()},
					{KeyPath: newPath([]byte{2, 3}).Serialize()},
				},
			},
			start:       nil,
			end:         nil,
			expectedErr: ErrNonIncreasingProofNodes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := getBasicDB()
			require.NoError(t, err)
			err = tt.proof.Verify(context.Background(), db, tt.start, tt.end, ids.Empty)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestVerifyKeyValues(t *testing.T) {
	type test struct {
		name        string
		start       []byte
		end         []byte
		kvs         []KeyValue
		expectedErr error
	}

	tests := []test{
		{
			name:        "empty",
			start:       nil,
			end:         nil,
			kvs:         nil,
			expectedErr: nil,
		},
		{
			name:  "1 key",
			start: nil,
			end:   nil,
			kvs: []KeyValue{
				{Key: []byte{0}},
			},
			expectedErr: nil,
		},
		{
			name:  "non-increasing keys",
			start: nil,
			end:   nil,
			kvs: []KeyValue{
				{Key: []byte{0}},
				{Key: []byte{0}},
			},
			expectedErr: ErrNonIncreasingValues,
		},
		{
			name:  "key before start",
			start: []byte{1, 2},
			end:   nil,
			kvs: []KeyValue{
				{Key: []byte{1}},
				{Key: []byte{1, 2}},
			},
			expectedErr: ErrStateFromOutsideOfRange,
		},
		{
			name:  "key after end",
			start: nil,
			end:   []byte{1, 2},
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
			end:   []byte{1, 2, 3},
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
				{KeyPath: SerializedPath{Value: []byte{1, 2, 240}, NibbleLength: 5}, ValueOrHash: Some([]byte{1})},
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
