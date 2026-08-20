// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
)

type blockBuilder struct {
	hooks    *hookstest.Stub
	hookTime *time.Time
}

func newBlockBuilder() *blockBuilder {
	hookTime := new(time.Time)
	return &blockBuilder{
		hooks: hookstest.NewStub(1e9, hookstest.WithNow(func() time.Time {
			return *hookTime
		})),
		hookTime: hookTime,
	}
}

// new creates a [Block] using [New].
func (bb *blockBuilder) new(tb testing.TB, ethB *types.Block, parent, lastSettled *Block) (*Block, error) {
	tb.Helper()
	return New(ethB, parent, lastSettled, bb.hooks, loggingtest.New(tb, logging.Warn))
}

// mustNew is like [blockBuilder.new] but fails the test on error.
func (bb *blockBuilder) mustNew(tb testing.TB, ethB *types.Block, parent, lastSettled *Block) *Block {
	tb.Helper()

	b, err := bb.new(tb, ethB, parent, lastSettled)
	require.NoErrorf(tb, err, "New() for block %d", ethB.NumberU64())
	return b
}

// settledBy returns the [hook.Settled] to encode in an asynchronous block with
// the given last-settled block, which MAY be nil. Synchronous blocks MUST
// encode the zero value instead; see [hook.Synchronous].
func settledBy(lastSettled *Block) hook.Settled {
	s := hook.Settled{
		// Guaranteed non-zero so that a block without a last-settled pointer,
		// or one settling the genesis (height 0), isn't mistaken for a
		// synchronous block.
		GasNumerator: 1,
	}
	if lastSettled != nil {
		s.Height = lastSettled.Height()
	}
	return s
}

// newFromHooks constructs an asynchronous [Block] whose header is built by the
// stub hooks, encoding [settledBy] of `lastSettled`.
func (bb *blockBuilder) newFromHooks(tb testing.TB, num, sec uint64, parent, lastSettled *Block) *Block {
	tb.Helper()
	return bb.build(tb, num, sec, parent, lastSettled, settledBy(lastSettled))
}

func (bb *blockBuilder) build(tb testing.TB, num, sec uint64, parent, lastSettled *Block, settled hook.Settled) *Block {
	tb.Helper()

	*bb.hookTime = time.Unix(int64(sec), 0) //#nosec G115 -- Hard-coded test values won't overflow
	var ethHdr *types.Header
	if parent != nil {
		var err error
		ethHdr, err = bb.hooks.BuildHeader(parent.Header())
		require.NoErrorf(tb, err, "%T.BuildHeader() for block %d", bb.hooks, num)
	} else {
		// A root block (e.g. genesis) has no parent from which to build, so
		// its height and time are set directly.
		ethHdr = &types.Header{
			Number:  new(big.Int).SetUint64(num),
			Time:    sec,
			BaseFee: big.NewInt(1),
		}
	}
	ethB, err := bb.hooks.BuildBlock(
		ethHdr,
		nil, nil, nil, nil,
		settled,
	)
	require.NoErrorf(tb, err, "%T.BuildBlock() for block %d", bb.hooks, num)

	return bb.mustNew(tb, ethB, parent, lastSettled)
}

// newChain returns a chain of blocks with heights in [startHeight,
// startHeight+total) and build times equal to their heights. The
// lastSettledAtHeight map determines each block's last-settled block; a
// self-settling entry (only valid for the genesis) results in a synchronous
// block that is marked as settled.
func (bb *blockBuilder) newChain(tb testing.TB, startHeight, total uint64, lastSettledAtHeight map[uint64]uint64) []*Block {
	tb.Helper()

	var (
		parent    *Block
		blocks    []*Block
		blackhole atomic.Pointer[Block]
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

		var b *Block
		if synchronous {
			b = bb.build(tb, n, n, nil, nil, hook.Settled{})
			require.NoErrorf(tb, b.MarkSettled(&blackhole), "MarkSettled()")
		} else {
			b = bb.newFromHooks(tb, n, n /*time*/, parent, settle)
		}
		byNum[n] = b
		blocks = append(blocks, b)
		parent = b
	}

	return blocks
}

func TestSetAncestors(t *testing.T) {
	bb := newBlockBuilder()
	parent := bb.newFromHooks(t, 5, 5, nil, nil)
	lastSettled := bb.newFromHooks(t, 3, 0, nil, nil)
	source := bb.newFromHooks(t, 6, 6, parent, lastSettled)
	child := source.EthBlock()

	t.Run("incorrect_parent", func(t *testing.T) {
		// Note that the arguments to [New] are inverted.
		_, err := bb.new(t, child, lastSettled, parent)
		require.ErrorIs(t, err, errParentHashMismatch, "New() with inverted parent and last-settled blocks")
	})

	dest := bb.mustNew(t, child, nil, nil)

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
		dest := bb.newFromHooks(t, source.Height(), source.BuildTime()+1 /*hash mismatch*/, parent, lastSettled)
		require.ErrorIs(t, dest.CopyAncestorsFrom(source), errHashMismatch)
	})

	t.Run("not_incrementing_height", func(t *testing.T) {
		ethHdr, err := bb.hooks.BuildHeader(parent.Header())
		require.NoErrorf(t, err, "%T.BuildHeader()", bb.hooks)
		ethHdr.Number = parent.Number() // not incrementing
		ethB, err := bb.hooks.BuildBlock(ethHdr, nil, nil, nil, nil, hook.Settled{})
		require.NoErrorf(t, err, "%T.BuildBlock()", bb.hooks)
		_, err = bb.new(t, ethB, parent, nil)
		require.ErrorIs(t, err, errBlockHeightNotIncrementing)
	})
}
