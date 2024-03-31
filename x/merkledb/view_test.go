// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func Benchmark_HashChangedNodes(b *testing.B) {
	tests := []struct {
		name    string
		numKeys uint64
	}{
		{
			name:    "1",
			numKeys: 1,
		},
		{
			name:    "10",
			numKeys: 10,
		},
		{
			name:    "100",
			numKeys: 100,
		},
		{
			name:    "1000",
			numKeys: 1000,
		},
		{
			name:    "10000",
			numKeys: 10000,
		},
		{
			name:    "100000",
			numKeys: 100000,
		},
	}
	for _, test := range tests {
		db, err := getBasicDB()
		require.NoError(b, err)

		ops := make([]database.BatchOp, 0, test.numKeys)
		for i := uint64(0); i < test.numKeys; i++ {
			k := binary.AppendUvarint(nil, i)
			ops = append(ops, database.BatchOp{
				Key:   k,
				Value: hashing.ComputeHash256(k),
			})
		}

		ctx := context.Background()
		viewIntf, err := db.NewView(ctx, ViewChanges{BatchOps: ops})
		require.NoError(b, err)

		view := viewIntf.(*view)
		require.NoError(b, view.calculateNodeChanges(ctx))

		b.Run(test.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				view.hashChangedNodes(ctx)
			}
		})
	}
}
