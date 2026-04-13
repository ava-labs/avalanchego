// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

func newEthBlock(num, time uint64, parent *types.Block) *types.Block {
	hdr := &types.Header{
		Number: new(big.Int).SetUint64(num),
		Time:   time,
	}
	if parent != nil {
		hdr.ParentHash = parent.Hash()
	}
	return types.NewBlockWithHeader(hdr)
}

func newBlock(tb testing.TB, eth *types.Block, parent, lastSettled *Block) *Block {
	tb.Helper()
	b, err := New(eth, parent, lastSettled, saetest.NewTBLogger(tb, logging.Warn))
	require.NoError(tb, err, "New()")
	return b
}

func newChain(tb testing.TB, db ethdb.Database, xdb saetypes.ExecutionResults, startHeight, total uint64, lastSettledAtHeight map[uint64]uint64) []*Block {
	tb.Helper()

	var (
		ethParent *types.Block
		parent    *Block
		blocks    []*Block
	)
	byNum := make(map[uint64]*Block)

	for i := range total {
		n := startHeight + i

		var (
			settle      *Block
			synchronous bool
		)
		if s, ok := lastSettledAtHeight[n]; ok {
			if s == n {
				require.Zero(tb, s, "Only genesis block is self-settling")
				synchronous = true
			} else {
				require.Less(tb, s, n, "Last-settled height MUST be <= current height")
				settle = byNum[s]
			}
		}

		b := newBlock(tb, newEthBlock(n, n /*time*/, ethParent), parent, settle)
		byNum[n] = b
		blocks = append(blocks, b)
		if synchronous {
			// The target and excess are irrelevant for the purposes of
			// [newChain], and non-zero sub-second time for genesis is
			// unnecessary.
			h := hookstest.NewStub(1)
			require.NoError(tb, b.MarkSynchronous(h, db, xdb, 0), "MarkSynchronous()")
		}

		parent = byNum[n]
		ethParent = parent.EthBlock()
	}

	return blocks
}

func TestSetAncestors(t *testing.T) {
	parent := newBlock(t, newEthBlock(5, 5, nil), nil, nil)
	lastSettled := newBlock(t, newEthBlock(3, 0, nil), nil, nil)
	child := newEthBlock(6, 6, parent.EthBlock())

	t.Run("incorrect_parent", func(t *testing.T) {
		// Note that the arguments to [New] are inverted.
		_, err := New(child, lastSettled, parent, saetest.NewTBLogger(t, logging.Warn))
		require.ErrorIs(t, err, errParentHashMismatch, "New() with inverted parent and last-settled blocks")
	})

	source := newBlock(t, child, parent, lastSettled)
	dest := newBlock(t, child, nil, nil)

	t.Run("destination_before_copy", func(t *testing.T) {
		assert.Nilf(t, dest.ParentBlock(), "%T.ParentBlock()", dest)
		assert.Nilf(t, dest.LastSettled(), "%T.LastSettled()", dest)
	})
	if t.Failed() {
		t.FailNow()
	}

	require.NoError(t, dest.CopyAncestorsFrom(source), "CopyAncestorsFrom()")
	if diff := cmp.Diff(source, dest, CmpOpt()); diff != "" {
		t.Errorf("After %T.CopyAncestorsFrom(); diff (-want +got):\n%s", dest, diff)
	}

	t.Run("incompatible_destination_block", func(t *testing.T) {
		ethB := newEthBlock(source.Height(), source.BuildTime()+1 /*hash mismatch*/, parent.EthBlock())
		dest := newBlock(t, ethB, nil, nil)
		require.ErrorIs(t, dest.CopyAncestorsFrom(source), errHashMismatch)
	})

	t.Run("not_incrementing_height", func(t *testing.T) {
		ethB := newEthBlock(parent.Height() /*not incrementing*/, parent.BuildTime(), parent.EthBlock())
		_, err := New(ethB, parent, nil, nil)
		require.ErrorIs(t, err, errBlockHeightNotIncrementing)
	})
}
