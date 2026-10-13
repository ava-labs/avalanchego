// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
)

func TestGetAncestors(t *testing.T) {
	// Non-empty bodies prove the spliced header and body RLP matches a real
	// block encoding, which the syncer relies on when it checks the header
	// roots.
	chain := synctest.MakeChain(t, 5, synctest.WithTxsPerBlock(3))
	db := synctest.NewBlockDB(chain)
	tip := chain[len(chain)-1]

	// tipFirst[i] encodes the block i hops below the tip.
	tipFirst := encodeTipFirst(t, chain, len(chain))

	const noLimit = math.MaxInt
	tests := []struct {
		name    string
		num     uint64
		maxNum  int
		maxSize int
		want    [][]byte
	}{
		{
			name:    "whole_chain",
			num:     tip.NumberU64(),
			maxNum:  noLimit,
			maxSize: noLimit,
			want:    tipFirst,
		},
		{
			name:    "exact_num",
			num:     tip.NumberU64(),
			maxNum:  len(chain),
			maxSize: noLimit,
			want:    tipFirst,
		},
		{
			name:    "max_num_truncates",
			num:     tip.NumberU64(),
			maxNum:  3,
			maxSize: noLimit,
			want:    tipFirst[:3],
		},
		{
			name:    "zero_max_num_serves_the_requested_block",
			num:     tip.NumberU64(),
			maxNum:  0,
			maxSize: noLimit,
			want:    tipFirst[:1],
		},
		{
			name:    "max_size_is_inclusive",
			num:     tip.NumberU64(),
			maxNum:  len(chain),
			maxSize: len(tipFirst[0]) + len(tipFirst[1]),
			want:    tipFirst[:2],
		},
		{
			name:    "max_size_truncates",
			num:     tip.NumberU64(),
			maxNum:  len(chain),
			maxSize: len(tipFirst[0]) + len(tipFirst[1]) - 1,
			want:    tipFirst[:1],
		},
		{
			name:    "first_block_exceeds_max_size",
			num:     tip.NumberU64(),
			maxNum:  len(chain),
			maxSize: len(tipFirst[0]) - 1,
			want:    tipFirst[:1],
		},
		{
			name:    "genesis_has_no_ancestors",
			num:     0,
			maxNum:  noLimit,
			maxSize: noLimit,
			want:    tipFirst[len(tipFirst)-1:],
		},
		{
			name:    "unknown_height",
			num:     tip.NumberU64() + 1,
			maxNum:  noLimit,
			maxSize: noLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetAncestors(t.Context(), db, tt.num, tt.maxNum, tt.maxSize)
			require.NoErrorf(t, err, "GetAncestors(%d, %d, %d)", tt.num, tt.maxNum, tt.maxSize)
			require.Equalf(t, tt.want, got, "GetAncestors(%d, %d, %d)", tt.num, tt.maxNum, tt.maxSize)
		})
	}
}

// defaultAncestorsMaxBlockCount matches the node's default for the maximum
// number of blocks served by a GetAncestors request.
const defaultAncestorsMaxBlockCount = 2000

func BenchmarkGetAncestors(b *testing.B) {
	// An on-disk database keeps read costs more realistic, unlike the in-memory
	// [synctest.NewBlockDB].
	db, err := rawdb.NewPebbleDBDatabase(b.TempDir(), 128, 1024, "", false, false)
	require.NoError(b, err, "rawdb.NewPebbleDBDatabase()")
	b.Cleanup(func() {
		require.NoErrorf(b, db.Close(), "%T.Close()", db)
	})

	const numTxs = 10
	chain := synctest.MakeChain(b, defaultAncestorsMaxBlockCount, synctest.WithTxsPerBlock(numTxs))
	for _, blk := range chain {
		rawdb.WriteBlock(db, blk)
		rawdb.WriteCanonicalHash(db, blk.Hash(), blk.NumberU64())
	}

	ctx := b.Context()
	tipNumber := chain[len(chain)-1].NumberU64()

	// Sanity check to ensure the benchmark doesn't measure a failing case.
	blocks, err := GetAncestors(
		ctx,
		db,
		tipNumber,
		defaultAncestorsMaxBlockCount,
		constants.MaxContainersLen,
	)
	require.NoError(b, err)
	require.NotEmpty(b, blocks)

	b.ReportAllocs()
	for b.Loop() {
		_, _ = GetAncestors(
			ctx,
			db,
			tipNumber,
			defaultAncestorsMaxBlockCount,
			constants.MaxContainersLen,
		)
	}
}
