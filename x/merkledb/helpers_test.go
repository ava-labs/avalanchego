// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

func getBasicDB() (*merkleDB, error) {
	return newDatabase(
		context.Background(),
		memdb.New(),
		NewConfig(),
		&mockMetrics{},
	)
}

func getBasicDBWithBranchFactor(bf BranchFactor) (*merkleDB, error) {
	config := NewConfig()
	config.BranchFactor = bf

	return newDatabase(
		context.Background(),
		memdb.New(),
		config,
		&mockMetrics{},
	)
}

// Writes []byte{i} -> []byte{i} for i in [0, 4]
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

func newRandomProofNode(r *rand.Rand) ProofNode {
	key := make([]byte, r.Intn(32)) // #nosec G404
	_, _ = r.Read(key)              // #nosec G404
	serializedKey := ToKey(key)

	val := make([]byte, r.Intn(64)) // #nosec G404
	_, _ = r.Read(val)              // #nosec G404

	children := map[byte]ids.ID{}
	for j := 0; j < 16; j++ {
		if r.Float64() < 0.5 {
			var childID ids.ID
			_, _ = r.Read(childID[:]) // #nosec G404
			children[byte(j)] = childID
		}
	}

	hasValue := rand.Intn(2) == 1 // #nosec G404
	var valueOrHash maybe.Maybe[[]byte]
	if hasValue {
		// use the hash instead when length is greater than the hash length
		if len(val) >= HashLength {
			val = hashing.ComputeHash256(val)
		} else if len(val) == 0 {
			// We do this because when we encode a value of []byte{} we will later
			// decode it as nil.
			// Doing this prevents inconsistency when comparing the encoded and
			// decoded values.
			val = nil
		}
		valueOrHash = maybe.Some(val)
	}

	return ProofNode{
		Key:         serializedKey,
		ValueOrHash: valueOrHash,
		Children:    children,
	}
}
