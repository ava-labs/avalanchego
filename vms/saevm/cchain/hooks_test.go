// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math"
	"math/big"
	"testing"

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
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/acp283"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// newBlock returns a minimal [*types.Block] whose ExtData encodes txs and
// whose header is configured for ancestor traversal (parent hash + number).
func newBlock(tb testing.TB, number uint64, parent common.Hash, txs ...*tx.Tx) *types.Block {
	tb.Helper()

	extData, err := tx.MarshalSlice(txs)
	require.NoErrorf(tb, err, "tx.MarshalSlice(%d txs)", len(txs))

	return customtypes.NewBlockWithExtData(
		&types.Header{
			ParentHash: parent,
			Number:     new(big.Int).SetUint64(number),
		},
		nil, // txs
		nil, // uncles
		nil, // receipts
		saetest.TrieHasher(),
		extData,
		true, // setExtDataHash
	)
}

func headerWithMinPriceExponent(exp acp283.PriceExponent) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{Number: big.NewInt(0)},
		&customtypes.HeaderExtra{MinPriceExponent: &exp},
	)
}

func clampedToward(parent, target acp283.PriceExponent) acp283.PriceExponent {
	parent.Toward(target)
	return parent
}

func TestGasConfigAfter(t *testing.T) {
	tests := []struct {
		name     string
		exponent *acp283.PriceExponent
		want     gas.Price
	}{
		{
			name: "nil_defaults_to_one_wei",
			want: 1,
		},
		{
			name:     "smallest_exponent_above_one_wei",
			exponent: utils.PointerTo(acp283.DesiredPriceExponent(2)),
			want:     2,
		},
		{
			name:     "saturated_exponent",
			exponent: utils.PointerTo(acp283.DesiredPriceExponent(math.MaxUint64)),
			want:     math.MaxUint64,
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
	const parentExponent acp283.PriceExponent = 1000
	parentNoExponent := &types.Header{Number: big.NewInt(0)}
	parentWith := headerWithMinPriceExponent(parentExponent)

	tests := []struct {
		name    string
		parent  *types.Header
		desired *acp283.PriceExponent
		want    acp283.PriceExponent
	}{
		{
			name:   "no_parent_no_desired_seeds_initial",
			parent: parentNoExponent,
			want:   acp283.InitialPriceExponent,
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
			desired: utils.PointerTo(acp283.PriceExponent(math.MaxUint64)),
			want:    clampedToward(parentExponent, math.MaxUint64),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHooks(snowtest.Context(t, snowtest.CChainID), nil, txpool.NewPending(), tt.desired)
			header, err := h.BuildHeader(tt.parent)
			require.NoError(t, err)
			got := customtypes.GetHeaderExtra(header).MinPriceExponent
			require.NotNil(t, got)
			assert.Equalf(t, tt.want, *got, "BuildHeader()")
		})
	}
}

func TestBlockRebuilderFromMinPriceExponent(t *testing.T) {
	const parentExponent acp283.PriceExponent = 1000
	parent := headerWithMinPriceExponent(parentExponent)

	tests := []struct {
		name    string
		claimed acp283.PriceExponent
		want    acp283.PriceExponent
	}{
		{
			name:    "honest_claim_within_cap_reproduces",
			claimed: parentExponent + 500,
			want:    parentExponent + 500,
		},
		{
			name:    "cheated_claim_above_step_clamps",
			claimed: math.MaxUint64,
			want:    clampedToward(parentExponent, math.MaxUint64),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := newBlockWithMinPriceExponent(t, 1, parent.Hash(), tt.claimed)
			h := newHooks(snowtest.Context(t, snowtest.CChainID), nil, txpool.NewPending(), nil)
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

// newBlockWithMinPriceExponent returns a parseable block whose header carries
// the given MinPriceExponent.
func newBlockWithMinPriceExponent(tb testing.TB, number uint64, parent common.Hash, exp acp283.PriceExponent) *types.Block {
	tb.Helper()

	extData, err := tx.MarshalSlice(nil)
	require.NoErrorf(tb, err, "tx.MarshalSlice()")

	header := customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash: parent,
			Number:     new(big.Int).SetUint64(number),
		},
		&customtypes.HeaderExtra{MinPriceExponent: &exp},
	)
	return customtypes.NewBlockWithExtData(
		header, nil, nil, nil, saetest.TrieHasher(), extData, true,
	)
}

func TestAncestorInputIDs(t *testing.T) {
	var (
		w       = newWallet(txtest.NewKey(t), snowtest.Context(t, snowtest.CChainID), nil)
		genesis = common.Hash(ids.GenerateTestID())
		tx1     = w.newMinimalTx(t)
		block1  = newBlock(t, 1, genesis, tx1)
		tx2     = w.newMinimalTx(t)
		block2  = newBlock(t, 2, block1.Hash(), tx2)
		tx3     = w.newMinimalTx(t)
		block3  = newBlock(t, 3, block2.Hash(), tx3)
		block4  = newBlock(t, 4, block3.Hash())
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
