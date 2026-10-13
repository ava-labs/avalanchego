// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
)

func hooks() *hookstest.Stub {
	return hookstest.NewStub(1)
}

func newSynchronousEthBlock(tb testing.TB, num, time uint64, parent *types.Block) *types.Block {
	tb.Helper()
	return buildEthBlock(tb, num, time, parent, nil)
}

func newEthBlock(tb testing.TB, num, time uint64, parent *types.Block, lastSettled *Block) *types.Block {
	tb.Helper()
	require.NotNil(tb, lastSettled, "last-settled-block argument to newEthBlock()")
	return buildEthBlock(tb, num, time, parent, lastSettled)
}

func buildEthBlock(tb testing.TB, num, time uint64, parent *types.Block, lastSettled *Block) *types.Block {
	tb.Helper()

	hdr := &types.Header{
		Number:  new(big.Int).SetUint64(num),
		BaseFee: big.NewInt(1),
		Time:    time,
	}
	if parent != nil {
		hdr.ParentHash = parent.Hash()
	}

	var s hook.Settled
	if ls := lastSettled; ls != nil {
		s.Height = ls.Height()
		s.GasNumerator = 1
	}
	b, err := hookstest.BuildBlock(hdr, nil, nil, nil, nil, s)
	require.NoErrorf(tb, err, "hookstest.BuildBlock(%+v, ..., %+v)", hdr, s)
	return b
}

func newBlock(tb testing.TB, eth *types.Block, parent, lastSettled *Block) *Block {
	tb.Helper()
	b, err := New(eth, parent, lastSettled, hooks(), loggingtest.New(tb, logging.Warn))
	require.NoError(tb, err, "New()")
	return b
}

// newChain returns a slice of contiguous-height blocks. Only the last-settled
// height for the first in the chain is required, and any missing value will
// default to the same as its parent. Blocks that settle their own height are
// considered synchronous.
func newChain(tb testing.TB, startHeight, total uint64, lastSettledAtHeight map[uint64]uint64) []*Block {
	tb.Helper()

	var (
		ethParent         *types.Block
		parent            *Block
		synchronousParent = true
		blocks            []*Block
	)
	byNum := make(map[uint64]*Block)

	for i := range total {
		n := startHeight + i

		var (
			settle      *Block
			synchronous bool
		)
		switch s, ok := lastSettledAtHeight[n]; {
		case ok && s == n:
			if !synchronousParent {
				tb.Fatal("Bad test setup: synchronous block after asynchronous")
			}
			synchronous = true

		case ok && s != n:
			require.Less(tb, s, n, "Last-settled height MUST be <= current height")
			settle = byNum[s]

		case i == 0:
			tb.Fatal("Bad test setup: first block in chain MUST have last-settled height specified")

		default:
			settle = parent.LastSettled()
		}

		var ethB *types.Block
		if synchronous {
			ethB = newSynchronousEthBlock(tb, n, n /*time*/, ethParent)
		} else {
			ethB = newEthBlock(tb, n, n, ethParent, settle)
		}

		b := newBlock(tb, ethB, parent, settle)
		byNum[n] = b
		blocks = append(blocks, b)
		if synchronous {
			var lastSettledPtr atomic.Pointer[Block]
			require.NoErrorf(tb, b.MarkSettled(&lastSettledPtr), "MarkSettled()")
			b.synchronous = true // avoid requiring hooks and DB to mark as synchronous
		}

		parent = byNum[n]
		ethParent = parent.EthBlock()
		synchronousParent = synchronous
	}

	return blocks
}

func TestSetAncestors(t *testing.T) {
	lastSettled := newBlock(
		t,
		newSynchronousEthBlock(t, 3, 0, nil),
		nil, nil,
	)
	parent := newBlock(
		t,
		newEthBlock(t, 4, 5, lastSettled.EthBlock(), lastSettled),
		lastSettled, lastSettled,
	)
	child := newEthBlock(t, 5, 6, parent.EthBlock(), lastSettled)

	t.Run("incorrect_parent", func(t *testing.T) {
		// Note that the arguments to [New] are inverted.
		_, err := New(child, lastSettled, parent, hooks(), loggingtest.New(t, logging.Warn))
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
		ethB := newEthBlock(t, source.Height(), source.BuildTime()+1 /*hash mismatch*/, parent.EthBlock(), lastSettled)
		dest := newBlock(t, ethB, nil, nil)
		require.ErrorIs(t, dest.CopyAncestorsFrom(source), errHashMismatch)
	})

	t.Run("not_incrementing_height", func(t *testing.T) {
		ethB := newEthBlock(t, parent.Height() /*not incrementing*/, parent.BuildTime(), parent.EthBlock(), lastSettled)
		_, err := New(ethB, parent, nil, hooks(), logging.NoLog{})
		require.ErrorIs(t, err, errBlockHeightNotIncrementing)
	})
}
