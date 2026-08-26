// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/params"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

//nolint:testableexamples // Output is meaningless
func ExampleRange() {
	parent := blockBuildingPreference()
	settle, ok, err := LastToSettleAt(time.Now().Add(-params.Tau), parent)
	if err != nil {
		// Due to a malformed input to block verification.
		return // err
	}
	if !ok {
		return // execution is lagging; please come back soon
	}

	// Returns the (possibly empty) slice of blocks that would be settled by the
	// block being built.
	_ = Range(parent.LastSettled(), settle)
	// Returns the (possibly empty) slice of blocks that would be left unsettled
	// by the block being built.
	_ = Range(settle, parent)
}

// blockBuildingPreference exists only to allow examples to build.
func blockBuildingPreference() *Block { return nil }

func TestSettlementInvariants(t *testing.T) {
	lastSettled := newBlock(
		t,
		newSynchronousEthBlock(t, 3, 3, nil),
		nil, nil,
	)
	parent := newBlock(
		t,
		newEthBlock(t, 4, 9, lastSettled.EthBlock(), lastSettled),
		lastSettled, lastSettled,
	)
	b := newBlock(
		t,
		newEthBlock(t, 5, 10, parent.EthBlock(), lastSettled),
		parent, lastSettled,
	)

	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()
	for _, b := range []*Block{b, parent, lastSettled} {
		tm := mustNewGasTime(t, time.Unix(int64(b.Header().Time), 0), 1, 0, gastime.DefaultGasPriceConfig()) //#nosec G115 -- block time is hard-coded above.
		b.markExecutedForTests(t, db, xdb, tm)
	}

	t.Run("before_MarkSettled", func(t *testing.T) {
		require.False(t, b.Settled(), "Settled()")
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		require.ErrorIs(t, b.WaitUntilSettled(ctx), context.DeadlineExceeded, "WaitUntilSettled()")

		if diff := cmp.Diff(parent, b.ParentBlock(), CmpOpt()); diff != "" {
			t.Errorf("ParentBlock() diff (-constructor arg +got):\n%s", diff)
		}
		if diff := cmp.Diff(lastSettled, b.LastSettled(), CmpOpt()); diff != "" {
			t.Errorf("LastSettled() diff (-constructor arg +got):\n%s", diff)
		}
		assert.NoError(t, b.CheckInvariants(Executed), "CheckInvariants(Executed)")
	})
	if t.Failed() {
		t.FailNow()
	}

	var lastSettledPtr atomic.Pointer[Block]
	require.NoError(t, b.MarkSettled(&lastSettledPtr), "first call to MarkSettled()")

	t.Run("after_MarkSettled", func(t *testing.T) {
		assert.Equal(t, b, lastSettledPtr.Load(), "Atomic pointer to last-settled block")
		require.True(t, b.Settled(), "Settled()")
		assert.NoError(t, b.WaitUntilSettled(t.Context()), "WaitUntilSettled()")
		assert.NoError(t, b.CheckInvariants(Settled), "CheckInvariants(Settled)")

		rec := loggingtest.NewRecorder(logging.Warn)
		b.log = rec
		assertNumErrorLogs := func(t *testing.T, want int) {
			t.Helper()
			assert.Len(t, rec.At(logging.Error), want, "Number of ERROR")
		}

		assert.Nil(t, b.ParentBlock(), "ParentBlock()")
		assertNumErrorLogs(t, 1)
		assert.Nil(t, b.LastSettled(), "LastSettled()")
		assertNumErrorLogs(t, 2)
		require.ErrorIs(t, b.MarkSettled(&lastSettledPtr), errBlockResettled, "second call to MarkSettled()")
		assertNumErrorLogs(t, 3)
		if t.Failed() {
			t.FailNow()
		}

		want := []*loggingtest.Record{
			{
				Level: logging.Error,
				Msg:   getParentOfSettledErrMsg,
			},
			{
				Level: logging.Error,
				Msg:   getSettledOfSettledErrMsg,
			},
			{
				Level: logging.Error,
				Msg:   errBlockResettled.Error(),
			},
		}
		if diff := cmp.Diff(want, rec.AtLeast(logging.Error)); diff != "" {
			t.Errorf("ERROR + FATAL logs diff (-want +got):\n%s", diff)
		}
	})
}

func TestSettles(t *testing.T) {
	lastSettledAtHeight := map[uint64]uint64{
		0: 0, // genesis block is self-settling by definition
		1: 0,
		2: 0,
		3: 0,
		4: 1,
		5: 1,
		6: 3,
		7: 3,
		8: 3,
		9: 7,
	}
	wantSettles := map[uint64][]uint64{
		0: {0},
		1: nil,
		2: nil,
		3: nil,
		4: {1},
		5: nil,
		6: {2, 3},
		7: nil,
		8: nil,
		9: {4, 5, 6, 7},
	}
	blocks := newChain(t, 0, 10, lastSettledAtHeight)

	numsToBlocks := func(nums ...uint64) []*Block {
		bs := make([]*Block, len(nums))
		for i, n := range nums {
			bs[i] = blocks[n]
		}
		return bs
	}

	type testCase struct {
		name      string
		got, want []*Block
	}
	var tests []testCase

	for num, wantNums := range wantSettles {
		tests = append(tests, testCase{
			name: fmt.Sprintf("Block(%d).Settles()", num),
			got:  blocks[num].Settles(),
			want: numsToBlocks(wantNums...),
		})
	}

	for _, b := range blocks[1:] {
		tests = append(tests, testCase{
			name: "Range([identical blocks])",
			got:  Range(b.LastSettled(), b.LastSettled()),
			want: nil,
		})
	}

	tests = append(tests, []testCase{
		{
			got:  Range(blocks[7].LastSettled(), blocks[3]),
			want: nil,
		},
		{
			got:  Range(blocks[7].LastSettled(), blocks[4]),
			want: numsToBlocks(4),
		},
		{
			got:  Range(blocks[7].LastSettled(), blocks[5]),
			want: numsToBlocks(4, 5),
		},
		{
			got:  Range(blocks[7].LastSettled(), blocks[6]),
			want: numsToBlocks(4, 5, 6),
		},
	}...)

	opts := cmp.Options{
		CmpOpt(),
		cmputils.NilSlicesAreEmpty[[]*Block](),
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.got, opts); diff != "" {
				t.Errorf("Settles() diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSettlesAtTransitionBoundary covers a mid-chain transition to SAE, at
// which the last pre-SAE block is restored as a settled, synchronous block by
// [RestoreSettledBlock]. Its ancestry is severed, so the SAE blocks built on
// top of it MUST treat it as the furthest reachable last-settled block.
func TestSettlesAtTransitionBoundary(t *testing.T) {
	// Height of the last pre-SAE block. Deliberately non-zero, unlike the
	// genesis of an SAE-from-inception chain.
	const boundary = 39

	lastSettledAtHeight := map[uint64]uint64{
		boundary:     boundary, // synchronous blocks settle themselves
		boundary + 1: boundary,
		boundary + 2: boundary,
		boundary + 3: boundary,
		boundary + 4: boundary + 1,
		boundary + 5: boundary + 3,
		boundary + 6: boundary + 3,
	}
	wantSettles := map[uint64][]uint64{
		boundary:     {boundary},
		boundary + 1: nil,                          // (39,39]
		boundary + 2: nil,                          // (39,39]
		boundary + 3: nil,                          // (39,39]
		boundary + 4: {boundary + 1},               // (39,40]
		boundary + 5: {boundary + 2, boundary + 3}, // (40,42]
		boundary + 6: nil,                          // (42,42]
	}
	chain := newChain(t, boundary, uint64(len(lastSettledAtHeight)), lastSettledAtHeight)

	blockAt := func(tb testing.TB, height uint64) *Block {
		tb.Helper()
		require.GreaterOrEqual(tb, height, uint64(boundary), "test block height")
		return chain[height-boundary]
	}
	blocksAt := func(tb testing.TB, heights ...uint64) []*Block {
		tb.Helper()
		bs := make([]*Block, len(heights))
		for i, h := range heights {
			bs[i] = blockAt(tb, h)
		}
		return bs
	}

	t.Run("boundary_block", func(t *testing.T) {
		b := blockAt(t, boundary)
		require.True(t, b.Synchronous(), "Synchronous() of last pre-SAE block")
		require.True(t, b.Settled(), "Settled() of last pre-SAE block")
	})

	opts := cmp.Options{
		CmpOpt(),
		cmputils.NilSlicesAreEmpty[[]*Block](),
	}
	for height, want := range wantSettles {
		t.Run(fmt.Sprintf("Block(%d).Settles()", height), func(t *testing.T) {
			got := blockAt(t, height).Settles()
			if diff := cmp.Diff(blocksAt(t, want...), got, opts); diff != "" {
				t.Errorf("Settles() diff (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("last_settled", func(t *testing.T) {
		for height, want := range lastSettledAtHeight {
			got := blockAt(t, height).LastSettled()
			require.NotNilf(t, got, "Block(%d).LastSettled()", height)
			require.Equalf(t, want, got.Height(), "Block(%d).LastSettled().Height()", height)
		}
	})

	t.Run("disjoint_and_contiguous", func(t *testing.T) {
		// Every SAE block settles a disjoint range, which together are
		// contiguous from the block after the boundary. The boundary block
		// itself is excluded as it settles itself.
		var settled []uint64
		for _, b := range chain[1:] {
			for _, s := range b.Settles() {
				settled = append(settled, s.Height())
			}
		}
		want := []uint64{boundary + 1, boundary + 2, boundary + 3}
		require.Equal(t, want, settled, "heights settled by each block of the chain, in order")
	})
}

// TestSettlesWithUnsettledSynchronousParent covers a block building on a
// synchronous block that is yet to be marked as settled, e.g. the last pre-SAE
// block during recovery, before [Block.MarkSettled] is called on it. A
// synchronous block settles itself, so its child settles nothing.
func TestSettlesWithUnsettledSynchronousParent(t *testing.T) {
	// Deliberately non-zero: the height of the genesis is indistinguishable
	// from the zero-valued settlement marker of a synchronous block.
	const boundary = 39

	parent := newBlock(t, newSynchronousEthBlock(t, boundary, boundary, nil), nil, nil)
	parent.synchronous = true // avoid requiring hooks and DB to mark as synchronous
	require.True(t, parent.Synchronous(), "Synchronous() of last pre-SAE block")
	require.False(t, parent.Settled(), "Settled() of block yet to be settled")
	require.Nil(t, parent.ParentBlock(), "ParentBlock() of block without a parent")

	b := newBlock(t, newEthBlock(t, boundary+1, boundary+1, parent.EthBlock(), parent), parent, parent)

	require.Equal(t, parent, b.LastSettled(), "LastSettled() is the synchronous parent")
	// The parent settles itself, i.e. x==y==boundary, so b settles nothing.
	require.Empty(t, b.Settles(), "Settles() of block building on an unsettled synchronous block")

	t.Run("parent_settles_itself", func(t *testing.T) {
		require.Equal(t, []*Block{parent}, parent.Settles(), "Settles() of synchronous block")
	})
}

func TestLastToSettleAt(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()

	// TODO(arr4n): Although [newChain] sets the last-settled block of all
	// asynchronous blocks (in this case to the genesis block), they are
	// irrelevant for the rest of this test and will certainly diverge from the
	// value returned by [LastToSettleAt]. Fixing this requires building the
	// chain manually, and interleaving extension with calls to
	// [LastToSettleAt], which is a major refactor for minimal benefit.
	blocks := newChain(t, 0, 30, map[uint64]uint64{0: 0})

	t.Run("helper_invariants", func(t *testing.T) {
		for i, b := range blocks {
			require.Equal(t, uint64(i), b.Height()) //#nosec G115 -- Slice index won't overflow
			require.Equal(t, b.BuildTime(), b.Height())
		}
	})

	tm := mustNewGasTime(t, time.Unix(0, 0), 5 /*target*/, 0, gastime.DefaultGasPriceConfig())
	require.Equal(t, gas.Gas(10), tm.Rate())

	requireTime := func(t *testing.T, sec uint64, numerator gas.Gas) {
		t.Helper()
		assert.Equalf(t, sec, tm.Unix(), "%T.Unix()", tm)
		wantFrac := proxytime.FractionalSecond[gas.Gas]{
			Numerator:   numerator,
			Denominator: tm.Rate(),
		}
		assert.Equalf(t, wantFrac, tm.Fraction(), "%T.Fraction()", tm)
		if t.Failed() {
			t.FailNow()
		}
	}

	requireTime(t, 0, 0)
	blocks[0].markExecutedForTests(t, db, xdb, tm)

	tm.Tick(13)
	requireTime(t, 1, 3)
	blocks[1].markExecutedForTests(t, db, xdb, tm)

	tm.Tick(20)
	requireTime(t, 3, 3)
	blocks[2].markExecutedForTests(t, db, xdb, tm)

	tm.Tick(5)
	requireTime(t, 3, 8)
	blocks[3].markExecutedForTests(t, db, xdb, tm)

	tm.Tick(23)
	requireTime(t, 6, 1)
	blocks[4].markExecutedForTests(t, db, xdb, tm)

	tm.Tick(9)
	requireTime(t, 7, 0)
	blocks[5].markExecutedForTests(t, db, xdb, tm)

	tm.Tick(10)
	requireTime(t, 8, 0)
	blocks[6].markExecutedForTests(t, db, xdb, tm)

	tm.Tick(1)
	requireTime(t, 8, 1)
	blocks[7].markExecutedForTests(t, db, xdb, tm)

	tm.Tick(50)
	requireTime(t, 13, 1)
	blocks[8].markExecutedForTests(t, db, xdb, tm)

	require.False(
		t, blocks[9].Executed(),
		"Block 9 MUST remain unexecuted", // exercises lagging-execution logic when building on 9
	)

	type testCase struct {
		name     string
		settleAt uint64
		parent   *Block
		wantOK   bool
		want     *Block
	}

	tests := []testCase{
		{
			settleAt: 3,
			parent:   blocks[5],
			wantOK:   true,
			want:     blocks[1],
		},
		{
			settleAt: 4,
			parent:   blocks[9],
			wantOK:   true,
			want:     blocks[3],
		},
		{
			settleAt: 4,
			parent:   blocks[8],
			wantOK:   true,
			want:     blocks[3],
		},
		{
			settleAt: 7,
			parent:   blocks[10],
			wantOK:   true,
			want:     blocks[5],
		},
		{
			settleAt: 9,
			parent:   blocks[8],
			wantOK:   true,
			want:     blocks[7],
		},
		{
			settleAt: 9,
			parent:   blocks[9],
			wantOK:   true,
			want:     blocks[7],
		},
		{
			settleAt: 15,
			parent:   blocks[18],
			wantOK:   false,
		},
	}

	{
		// Scenario:
		//   * Mark block 24 as executed at time 25.1
		//   * Mark block 25 as partially executed by time 27.1
		//   * Settle at time 26 (between them) with 25 as parent
		//
		// If block 25 wasn't marked as partially executed then it could
		// feasibly execute by settlement time (26) so [LastToSettleAt] would
		// return false. As the partial execution time makes it impossible for
		// block 25 to execute in time, we loop to its parent, which is already
		// executed in time and is therefore the expected return value.
		tm.Tick(120)
		require.Equal(t, uint64(25), tm.Unix())
		require.Equal(t, proxytime.FractionalSecond[gas.Gas]{Numerator: 1, Denominator: 10}, tm.Fraction())
		blocks[24].markExecutedForTests(t, db, xdb, tm)

		partiallyExecutedAt := proxytime.New[gas.Gas](27, 1, 100)
		blocks[25].SwapInterimExecutionTime(partiallyExecutedAt)

		tests = append(tests, testCase{
			settleAt: 26,
			parent:   blocks[25],
			wantOK:   true,
			want:     blocks[24],
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settleAt := time.Unix(int64(tt.settleAt), 0) //#nosec G115 -- Hard-coded, non-overflowing values
			got, gotOK, err := LastToSettleAt(settleAt, tt.parent)
			if err != nil || gotOK != tt.wantOK {
				t.Fatalf("LastToSettleAt(%d, [parent height %d]) got (_, %t, %v); want (_, %t, nil)", tt.settleAt, tt.parent.Height(), gotOK, err, tt.wantOK)
			}
			if tt.wantOK {
				require.Equal(t, tt.want.Height(), got.Height(), "LastToSettleAt(%d, [parent height %d])", tt.settleAt, tt.parent.Height())
			}
		})
	}
}
