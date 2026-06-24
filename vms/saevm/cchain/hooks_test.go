// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
)

// When TimeMilliseconds is unset, BlockTime falls back to Header.Time's seconds.
// The VM always sets TimeMilliseconds, so this legacy decode path is only
// reachable by exercising the hook directly.
func TestBlockTime(t *testing.T) {
	const testTimestampSeconds uint64 = 1_700_000_000

	header := &types.Header{Time: testTimestampSeconds}

	got := (&hooks{}).BlockTime(header)
	require.Equal(t, int64(testTimestampSeconds)*1000, got.UnixMilli(), "hooks.BlockTime(unset TimeMilliseconds).UnixMilli()")
	// Documented invariant: BlockTime(h).Unix() == h.Time.
	require.Equal(t, int64(testTimestampSeconds), got.Unix(), "hooks.BlockTime(unset TimeMilliseconds).Unix()")
}

func TestGasConfigAfter(t *testing.T) {
	tests := []struct {
		name     string
		exponent *dynamic.PriceExponent
		want     gas.Price
	}{
		{
			name: "nil_defaults_to_one_wei",
			want: 1,
		},
		{
			name:     "exponent_returns_its_price",
			exponent: utils.PointerTo(dynamic.DesiredPriceExponent(2)),
			want:     2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := customtypes.WithHeaderExtra(
				&types.Header{},
				&customtypes.HeaderExtra{MinPriceExponent: tt.exponent},
			)
			_, cfg := (*hooks)(nil).GasConfigAfter(header)
			assert.Equalf(t, tt.want, cfg.MinPrice, "GasConfigAfter()")
		})
	}
}

func TestBuildHeaderMinPriceExponent(t *testing.T) {
	const parentExponent dynamic.PriceExponent = 1000
	parentNoExponent := &types.Header{Number: big.NewInt(0)}
	parentWith := customtypes.WithHeaderExtra(
		&types.Header{Number: big.NewInt(0)},
		&customtypes.HeaderExtra{MinPriceExponent: utils.PointerTo(parentExponent)},
	)

	tests := []struct {
		name    string
		parent  *types.Header
		desired *dynamic.PriceExponent
		want    dynamic.PriceExponent
	}{
		{
			name:   "no_parent_no_desired_seeds_initial",
			parent: parentNoExponent,
			want:   initialPriceExponent,
		},
		{
			name:   "parent_set_no_desired_carries",
			parent: parentWith,
			want:   parentExponent,
		},
		{
			name:    "desired_within_cap_applied",
			parent:  parentWith,
			desired: utils.PointerTo(parentExponent + 500),
			want:    parentExponent + 500,
		},
		{
			name:    "desired_above_clamped",
			parent:  parentWith,
			desired: utils.PointerTo(dynamic.PriceExponent(math.MaxUint64)),
			want:    parentExponent.Toward(utils.PointerTo(dynamic.PriceExponent(math.MaxUint64))),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHooks(snowtest.Context(t, snowtest.CChainID), nil, txpool.NewPending(), time.Now, desiredParams{priceExponent: tt.desired})
			header, err := h.BuildHeader(tt.parent)
			require.NoError(t, err)
			got := customtypes.GetHeaderExtra(header).MinPriceExponent
			assert.Equalf(t, &tt.want, got, "BuildHeader()")
		})
	}
}

func TestBlockRebuilderFromMinPriceExponent(t *testing.T) {
	const parentExponent dynamic.PriceExponent = 1000
	parent := customtypes.WithHeaderExtra(
		&types.Header{Number: big.NewInt(0)},
		&customtypes.HeaderExtra{MinPriceExponent: utils.PointerTo(parentExponent)},
	)

	tests := []struct {
		name    string
		claimed dynamic.PriceExponent
		want    dynamic.PriceExponent
	}{
		{
			name:    "honest_claim_within_cap_reproduces",
			claimed: parentExponent + 500,
			want:    parentExponent + 500,
		},
		{
			name:    "cheated_claim_above_step_clamps",
			claimed: math.MaxUint64,
			want:    parentExponent.Toward(utils.PointerTo(dynamic.PriceExponent(math.MaxUint64))),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := cchaintest.NewTestBlock(t,
				cchaintest.WithNumber(1),
				cchaintest.WithParent(parent.Hash()),
				cchaintest.WithMinPriceExponent(tt.claimed),
			)
			h := newHooks(snowtest.Context(t, snowtest.CChainID), nil, txpool.NewPending(), time.Now, desiredParams{})
			rb, err := h.BlockRebuilderFrom(block)
			require.NoError(t, err)
			rebuilt, err := rb.BuildHeader(parent)
			require.NoError(t, err)
			got := customtypes.GetHeaderExtra(rebuilt).MinPriceExponent
			require.NotNil(t, got)
			assert.Equalf(t, tt.want, *got, "rebuilder BuildHeader()")
		})
	}
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
