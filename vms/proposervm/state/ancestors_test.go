// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

// serialWalk reproduces the parent-chasing walk that GetAncestorBytes replaces,
// so the two can be compared directly.
func serialWalk(bs BlockState, from block.Block, maxNum, maxSize int) [][]byte {
	var (
		res     = make([][]byte, 0, maxNum)
		byteLen = 0
		blkID   = from.ID()
	)
	for len(res) < maxNum {
		blkBytes, parentID, err := bs.GetBlockBytesAndParent(blkID)
		if err != nil {
			break
		}
		byteLen += wrappers.IntLen + len(blkBytes)
		if len(res) > 0 && byteLen > maxSize {
			break
		}
		res = append(res, blkBytes)
		blkID = parentID
	}
	return res
}

// TestGetAncestorBytesMatchesSerialWalk is the correctness bar for the indexed
// path: for an accepted block it must return exactly what parent-chasing does.
func TestGetAncestorBytesMatchesSerialWalk(t *testing.T) {
	const chainLen = 60

	blks := buildChain(t, chainLen, 512)
	st := newIndexedState(t, blks)
	deadline := time.Now().Add(time.Hour)

	for _, test := range []struct {
		name        string
		top         int
		maxNum      int
		maxSize     int
		concurrency int
	}{
		{"full_range", chainLen - 1, chainLen, 1 << 20, 16},
		{"count_limited", chainLen - 1, 7, 1 << 20, 16},
		{"size_limited", chainLen - 1, chainLen, 4096, 16},
		{"single_worker", chainLen - 1, chainLen, 1 << 20, 1},
		{"wave_boundary", chainLen - 1, 32, 1 << 20, 8},
		{"concurrency_exceeds_range", 5, chainLen, 1 << 20, 64},
		{"from_genesis", 0, chainLen, 1 << 20, 16},
	} {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			want := serialWalk(st, blks[test.top], test.maxNum, test.maxSize)
			got, err := GetAncestorBytes(
				st, st,
				uint64(test.top),
				test.maxNum,
				test.maxSize,
				deadline,
				time.Now,
				test.concurrency,
			)
			require.NoError(err)
			require.Equal(want, got)

			// And the response really is newest-first, contiguous by parent.
			for i := 0; i+1 < len(got); i++ {
				parentID, err := block.ParentID(got[i])
				require.NoError(err)
				require.Equal(blks[test.top-i-1].ID(), parentID)
			}
		})
	}
}

// TestGetAncestorBytesStopsAtForkHeight checks the walk does not run below the
// fork, where blocks are not in this VM's store.
func TestGetAncestorBytesStopsAtForkHeight(t *testing.T) {
	require := require.New(t)

	const chainLen = 30
	blks := buildChain(t, chainLen, 256)

	vdb := versiondb.New(memdb.New())
	st := New(vdb)
	for i, blk := range blks {
		require.NoError(st.PutBlock(blk))
		require.NoError(st.SetBlockIDAtHeight(uint64(i), blk.ID()))
	}
	const forkHeight = 10
	require.NoError(st.SetForkHeight(forkHeight))
	require.NoError(vdb.Commit())

	got, err := GetAncestorBytes(
		st, st,
		chainLen-1,
		chainLen,
		1<<20,
		time.Now().Add(time.Hour),
		time.Now,
		8,
	)
	require.NoError(err)
	require.Len(got, chainLen-forkHeight)
	require.Equal(blks[forkHeight].Bytes(), got[len(got)-1])
}

func TestGetAncestorBytesRespectsDeadline(t *testing.T) {
	require := require.New(t)

	blks := buildChain(t, 40, 256)
	st := newIndexedState(t, blks)

	// A deadline already in the past still returns the first wave, so that a
	// slow node answers with something rather than nothing.
	got, err := GetAncestorBytes(
		st, st,
		39,
		40,
		1<<20,
		time.Now().Add(-time.Second),
		time.Now,
		4,
	)
	require.NoError(err)
	require.NotEmpty(got)
	require.Len(got, 4)
}
