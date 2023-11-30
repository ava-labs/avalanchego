// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"

	"github.com/stretchr/testify/require"
)

func TestTrieViewWithRootPrefix(t *testing.T) {
	ctx := context.Background()
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	root, err := db.GetMerkleRoot(ctx)
	require.NoError(err)
	hashCount := db.metrics.(*mockMetrics).hashCount
	fmt.Printf("root: %v, hashCount: %d\n", root, hashCount)

	tvs := []TrieView{db}

	prefixes := [][]byte{[]byte("prefix1"), []byte("prefix2")}
	prefixAltRoots := []ids.ID{}

	numKeys := 1000
	keys := make([][]byte, numKeys)
	vals := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = []byte(fmt.Sprintf("key%d", i))
		vals[i] = []byte(fmt.Sprintf("val%d", i))
	}

	for _, prefix := range prefixes {
		parent := tvs[len(tvs)-1]
		batchOps := []database.BatchOp{}
		for i, key := range keys {
			batchOps = append(
				batchOps,
				database.BatchOp{
					Key:   append(prefix, key...),
					Value: vals[i],
				})
		}

		tv, err := parent.NewViewWithRootPrefix(
			ctx, ViewChanges{BatchOps: batchOps}, ToKey(prefix))
		require.NoError(err)
		root, err := tv.GetMerkleRoot(ctx)
		require.NoError(err)
		altRoot, err := tv.GetAltMerkleRoot(ctx)
		require.NoError(err)
		hashCount := db.metrics.(*mockMetrics).hashCount
		fmt.Printf("root: %v, altRoot: %v, hashCount: %d\n", root, altRoot, hashCount)

		tvs = append(tvs, tv)
		prefixAltRoots = append(prefixAltRoots, altRoot)
	}

	// Ensure that the alt roots are the same
	for i := 1; i < len(prefixes); i++ {
		require.Equal(prefixAltRoots[i], prefixAltRoots[i-1])
	}

	parent := tvs[len(tvs)-1]
	batchOps := []database.BatchOp{}
	for i, prefix := range prefixes {
		batchOps = append(
			batchOps,
			database.BatchOp{
				Key:   prefix,
				Value: []byte(fmt.Sprintf("prefixVal%d", i)),
			})
	}
	tv, err := parent.NewView(ctx, ViewChanges{BatchOps: batchOps})
	require.NoError(err)
	tvs = append(tvs, tv)

	for _, tv := range tvs {
		require.NoError(tv.CommitToDB(ctx))
	}
	{
		root, err := db.GetMerkleRoot(ctx)
		require.NoError(err)
		hashCount := db.metrics.(*mockMetrics).hashCount
		fmt.Printf("root: %v, hashCount: %d\n", root, hashCount)
	}
}
