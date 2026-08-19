// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"bytes"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
)

func TestGetAncestors(t *testing.T) {
	chain := synctest.MakeChain(t, 5)
	db := synctest.NewBlockDB(chain)
	lastAccepted := chain[len(chain)-1]

	// fromAccepted[i] is the byte representation of the ith block before (and
	// including) the last accepted block.
	fromAccepted := make([][]byte, 0, len(chain))
	for _, b := range slices.Backward(chain) {
		fromAccepted = append(fromAccepted, encodeBlockRLP(t, b))
	}

	// A block stored without a canonical hash stands in for a verified but
	// unaccepted block.
	verified := synctest.MakeChain(t, 6)[6]
	rawdb.WriteBlock(db, verified)

	const noLimit = math.MaxInt
	tests := []struct {
		name    string
		blkID   ids.ID
		maxNum  int
		maxSize int
		want    [][]byte
	}{
		{
			name:    "whole_chain",
			blkID:   ids.ID(lastAccepted.Hash()),
			maxNum:  noLimit,
			maxSize: noLimit,
			want:    fromAccepted,
		},
		{
			name:    "exact_num",
			blkID:   ids.ID(lastAccepted.Hash()),
			maxNum:  len(chain),
			maxSize: noLimit,
			want:    fromAccepted,
		},
		{
			name:    "max_num_truncates",
			blkID:   ids.ID(lastAccepted.Hash()),
			maxNum:  3,
			maxSize: noLimit,
			want:    fromAccepted[:3],
		},
		{
			name:    "max_size_truncates",
			blkID:   ids.ID(lastAccepted.Hash()),
			maxNum:  len(chain),
			maxSize: len(fromAccepted[0]) + len(fromAccepted[1]) + 2*wrappers.IntLen, // inclusive; third block exceeds it
			want:    fromAccepted[:2],
		},
		{
			name:    "intlen_overhead",
			blkID:   ids.ID(lastAccepted.Hash()),
			maxNum:  len(chain),
			maxSize: len(fromAccepted[0]) + len(fromAccepted[1]) + wrappers.IntLen, // inclusive bound; second block exceeds it
			want:    fromAccepted[:1],
		},
		{
			name:    "max_size_below_first_block",
			blkID:   ids.ID(lastAccepted.Hash()),
			maxNum:  len(chain),
			maxSize: len(fromAccepted[0]) - 1,
			want:    fromAccepted[:1],
		},
		{
			name:    "unknown_block",
			blkID:   ids.GenerateTestID(),
			maxNum:  noLimit,
			maxSize: noLimit,
		},
		{
			name:    "non_canonical_block", // only accepted blocks are served
			blkID:   ids.ID(verified.Hash()),
			maxNum:  noLimit,
			maxSize: noLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetAncestors(t.Context(), db, tt.blkID, tt.maxNum, tt.maxSize, time.Minute)
			require.NoErrorf(t, err, "GetAncestors(%s, %d, %d)", tt.blkID, tt.maxNum, tt.maxSize)
			require.Equalf(t, tt.want, got, "GetAncestors(%s, %d, %d)", tt.blkID, tt.maxNum, tt.maxSize)
		})
	}
}

// Splicing stored RLP must produce exactly what encoding the block produces,
// since the syncer decodes the result and checks it against the header roots.
func TestGetAncestorsMatchEncodedBlocks(t *testing.T) {
	chain := synctest.MakeChain(t, 8, synctest.WithTxsPerBlock(3))
	tip := chain[len(chain)-1]

	got, err := GetAncestors(t.Context(), synctest.NewBlockDB(chain), ids.ID(tip.Hash()), len(chain), math.MaxInt, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, len(chain))

	for i, raw := range got {
		want := chain[len(chain)-1-i]
		require.Equal(t, encodeBlockRLP(t, want), raw, "spliced bytes differ from the encoded block at height %d", want.NumberU64())
	}
}

func encodeBlockRLP(t *testing.T, b *types.Block) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, b.EncodeRLP(&buf))
	return buf.Bytes()
}

// defaultAncestorsMaxBlockCount is the default maximum number of blocks
// requested by GetAncestors.
//
// TODO(StephenButtolph): This really isn't configurable. We should remove this
// as a flag and just make it a global constant.
const defaultAncestorsMaxBlockCount = 2000

func BenchmarkGetAncestors(b *testing.B) {
	// [synctest.NewBlockDB] is in-memory. To make the benchmark a bit more
	// realistic, we provide a real implementation here.
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
	tip := chain[len(chain)-1]
	tipID := ids.ID(tip.Hash())

	for _, bench := range []struct {
		name string
		get  func() ([][]byte, error)
	}{
		{"batched", func() ([][]byte, error) {
			return GetAncestors(b.Context(), db, tipID, defaultAncestorsMaxBlockCount, constants.MaxContainersLen, time.Minute)
		}},
		{"serial", func() ([][]byte, error) {
			return serialGetAncestors(db, tip.Hash(), tip.NumberU64(), defaultAncestorsMaxBlockCount, constants.MaxContainersLen)
		}},
	} {
		b.Run(bench.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = bench.get()
			}
		})
	}
}

// serialGetAncestors decodes and re-encodes each block, which is what
// [GetAncestors] avoids by splicing the stored RLP.
func serialGetAncestors(db ethdb.Reader, hash common.Hash, num uint64, maxBlocks, maxSize int) ([][]byte, error) {
	blocks := make([][]byte, 0, maxBlocks)
	size := 0
	for range maxBlocks {
		blk := rawdb.ReadBlock(db, hash, num)
		if blk == nil {
			break
		}
		var buf bytes.Buffer
		if err := blk.EncodeRLP(&buf); err != nil {
			return nil, err
		}
		size += buf.Len() + wrappers.IntLen
		if len(blocks) > 0 && size > maxSize {
			break
		}
		blocks = append(blocks, buf.Bytes())
		num--
		hash = blk.ParentHash()
	}
	return blocks, nil
}
