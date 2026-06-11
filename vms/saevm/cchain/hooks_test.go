// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
)

const (
	testTimestampSeconds      uint64 = 1_700_000_000
	testTimestampMilliseconds int64  = 1_700_000_000_123
	testFallbackMilliseconds  int64  = 1_700_000_000_000
)

func TestBlockTime(t *testing.T) {
	tests := []struct {
		name   string
		header *types.Header
		wantMS int64
	}{
		{
			name: "reads_milliseconds_from_extra",
			header: customtypes.WithHeaderExtra(
				&types.Header{Time: testTimestampSeconds},
				&customtypes.HeaderExtra{
					TimeMilliseconds: utils.PointerTo(uint64(testTimestampMilliseconds)),
				},
			),
			wantMS: testTimestampMilliseconds,
		},
		{
			name:   "falls_back_to_seconds_when_unset",
			header: &types.Header{Time: testTimestampSeconds},
			wantMS: testFallbackMilliseconds,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&hooks{}).BlockTime(tt.header)
			require.Equal(t, time.UnixMilli(tt.wantMS), got, "hooks.BlockTime(%d)", tt.wantMS)
			require.Equal(t, tt.wantMS, got.UnixMilli(), "hooks.BlockTime(%d).UnixMilli()", tt.wantMS)
		})
	}
}

func TestBuildHeaderEncodesMilliseconds(t *testing.T) {
	b := &builder{
		now: func() time.Time {
			return time.UnixMilli(testTimestampMilliseconds)
		},
	}

	header, err := b.BuildHeader(&types.Header{Number: big.NewInt(41)})
	require.NoError(t, err, "builder.BuildHeader(nowMS=%d)", testTimestampMilliseconds)

	require.Equal(t, uint64(testTimestampMilliseconds/1000), header.Time, "builder.BuildHeader(nowMS=%d).Time", testTimestampMilliseconds)

	extra := customtypes.GetHeaderExtra(header)
	require.NotNil(t, extra.TimeMilliseconds, "customtypes.GetHeaderExtra(BuildHeader(nowMS=%d)).TimeMilliseconds", testTimestampMilliseconds)
	require.Equal(t, uint64(testTimestampMilliseconds), *extra.TimeMilliseconds, "builder.BuildHeader(nowMS=%d).TimeMilliseconds", testTimestampMilliseconds)

	// Invariant enforced by customheader.VerifyTime.
	require.Equal(t, header.Time, *extra.TimeMilliseconds/1000, "builder.BuildHeader(nowMS=%d) time invariant", testTimestampMilliseconds)
}

func TestBuildHeaderBlockTimeRoundTrip(t *testing.T) {
	want := time.UnixMilli(testTimestampMilliseconds)
	b := &builder{
		now: func() time.Time {
			return want
		},
	}

	header, err := b.BuildHeader(&types.Header{Number: big.NewInt(1)})
	require.NoError(t, err, "builder.BuildHeader(now=%d)", want.UnixMilli())

	got := (&hooks{}).BlockTime(header)
	require.Equal(t, want.UnixMilli(), got.UnixMilli(), "hooks.BlockTime(builder.BuildHeader(now=%d))", want.UnixMilli())
}

func TestAncestorInputIDs(t *testing.T) {
	var (
		w       = newWallet(txtest.NewKey(t), snowtest.Context(t, snowtest.CChainID), nil)
		genesis = common.Hash(ids.GenerateTestID())
		tx1     = w.newMinimalTx(t)
		block1  = cchaintest.NewBlock(t, 1, genesis, tx1)
		tx2     = w.newMinimalTx(t)
		block2  = cchaintest.NewBlock(t, 2, block1.Hash(), tx2)
		tx3     = w.newMinimalTx(t)
		block3  = cchaintest.NewBlock(t, 3, block2.Hash(), tx3)
		block4  = cchaintest.NewBlock(t, 4, block3.Hash())
	)

	tests := []struct {
		name    string
		header  *types.Header
		settled common.Hash
		want    set.Set[ids.ID]
		wantErr error
	}{
		{
			name:    "empty_range",
			header:  block1.Header(),
			settled: genesis,
			want:    nil,
		},
		{
			name:    "single_ancestor",
			header:  block2.Header(),
			settled: genesis,
			want:    tx1.InputIDs(),
		},
		{
			name:    "multiple_ancestors",
			header:  block4.Header(),
			settled: genesis,
			want:    set.UnionOf(tx1.InputIDs(), tx2.InputIDs(), tx3.InputIDs()),
		},
		{
			name:    "stops_at_settled",
			header:  block4.Header(),
			settled: block1.Hash(),
			want:    set.UnionOf(tx2.InputIDs(), tx3.InputIDs()),
		},
		{
			name:    "missing_block",
			header:  block2.Header(),
			settled: common.Hash(ids.GenerateTestID()), // never matches
			wantErr: errMissingBlock,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := func(hash common.Hash, number uint64) (*types.Block, bool) {
				for _, b := range []*types.Block{block1, block2, block3} {
					if b.Hash() == hash && b.NumberU64() == number {
						return b, true
					}
				}
				return nil, false
			}

			got, err := ancestorInputIDs(tt.header, tt.settled, source)
			require.ErrorIs(t, err, tt.wantErr, "ancestorInputIDs()")
			assert.Equal(t, tt.want, got, "ancestorInputIDs()")
		})
	}
}
