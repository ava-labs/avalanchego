// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/cmputils"
	"github.com/ava-labs/strevm/gastime"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/hook/hookstest"
	"github.com/ava-labs/strevm/params"
	"github.com/ava-labs/strevm/proxytime"
	"github.com/ava-labs/strevm/saetest"
)

//nolint:testableexamples // Output is meaningless
func ExampleRange() {
	parent := blockBuildingPreference()
	settle, ok, err := LastToSettleAt(vmHooks(), time.Now().Add(-params.Tau), parent)
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

// blockBuildingPreference and vmHooks exist only to allow examples to build.
func blockBuildingPreference() *Block { return nil }
func vmHooks() hook.Points            { return nil }

func TestSettlementInvariants(t *testing.T) {
	parent := newBlock(t, newEthBlock(5, 5, nil), nil, nil)
	lastSettled := newBlock(t, newEthBlock(3, 3, nil), nil, nil)

	b := newBlock(t, newEthBlock(6, 10, parent.EthBlock()), parent, lastSettled)

	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()
	for _, b := range []*Block{b, parent, lastSettled} {
		tm := mustNewGasTime(t, preciseTime(b.Header(), 0), 1, 0, gastime.DefaultGasPriceConfig())
		b.markExecutedForTests(t, db, xdb, tm)
	}

	t.Run("before_MarkSettled", func(t *testing.T) {
		require.False(t, b.Settled(), "Settled()")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		assert.ErrorIs(t, b.WaitUntilSettled(ctx), context.DeadlineExceeded, "WaitUntilSettled()")

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
		assert.NoError(t, b.WaitUntilSettled(context.Background()), "WaitUntilSettled()")
		assert.NoError(t, b.CheckInvariants(Settled), "CheckInvariants(Settled)")

		rec := saetest.NewLogRecorder(logging.Warn)
		b.log = rec
		assertNumErrorLogs := func(t *testing.T, want int) {
			t.Helper()
			assert.Len(t, rec.At(logging.Error), want, "Number of ERROR")
		}

		assert.Nil(t, b.ParentBlock(), "ParentBlock()")
		assertNumErrorLogs(t, 1)
		assert.Nil(t, b.LastSettled(), "LastSettled()")
		assertNumErrorLogs(t, 2)
		assert.ErrorIs(t, b.MarkSettled(&lastSettledPtr), errBlockResettled, "second call to MarkSettled()")
		assertNumErrorLogs(t, 3)
		if t.Failed() {
			t.FailNow()
		}

		want := []*saetest.LogRecord{
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
	blocks := newChain(t, rawdb.NewMemoryDatabase(), saetest.NewExecutionResultsDB(), 0, 10, lastSettledAtHeight)

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

func TestLastToSettleAt(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()
	blocks := newChain(t, db, xdb, 0, 30, nil)
	t.Run("helper_invariants", func(t *testing.T) {
		for i, b := range blocks {
			require.Equal(t, uint64(i), b.Height()) //nolint:gosec // Slice index won't overflow
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

		partiallyExecutedAt := proxytime.New[gas.Gas](27, 100)
		partiallyExecutedAt.Tick(1)
		blocks[25].SetInterimExecutionTime(partiallyExecutedAt)

		tests = append(tests, testCase{
			settleAt: 26,
			parent:   blocks[25],
			wantOK:   true,
			want:     blocks[24],
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settleAt := time.Unix(int64(tt.settleAt), 0) //nolint:gosec // Hard-coded, non-overflowing values
			got, gotOK, err := LastToSettleAt(hookstest.NewStub(0), settleAt, tt.parent)
			if err != nil || gotOK != tt.wantOK {
				t.Fatalf("LastToSettleAt(%d, [parent height %d]) got (_, %t, %v); want (_, %t, nil)", tt.settleAt, tt.parent.Height(), gotOK, err, tt.wantOK)
			}
			if tt.wantOK {
				require.Equal(t, tt.want.Height(), got.Height(), "LastToSettleAt(%d, [parent height %d])", tt.settleAt, tt.parent.Height())
			}
		})
	}
}
