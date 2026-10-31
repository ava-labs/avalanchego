// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
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
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
)

func TestDelayExponent(t *testing.T) {
	tests := []struct {
		name   string
		header *types.Header
		want   dynamic.DelayExponent
	}{
		{
			name: "header_carries_excess",
			header: customtypes.WithHeaderExtra(
				&types.Header{},
				&customtypes.HeaderExtra{MinDelayExcess: new(acp226.DelayExcess(42))},
			),
			want: dynamic.DelayExponent(42),
		},
		{
			name:   "no_field_defaults_to_initial",
			header: &types.Header{},
			want:   dynamic.InitialDelayExponent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, delayExponent(tt.header), "delayExponent()")
		})
	}
}

func TestBlockTime(t *testing.T) {
	_, sut := newSUT(t)
	hooks := sut.hooks(t)

	tests := []struct {
		name string
		// When TimeMilliseconds is unset, BlockTime falls back to Time's
		// seconds. The VM always sets it, so this decode path is only reachable
		// by exercising the hook directly.
		header    *types.Header
		wantMilli int64
	}{
		{
			name:      "unset_falls_back_to_seconds",
			header:    &types.Header{Time: 1_700_000_000},
			wantMilli: 1_700_000_000_000,
		},
		{
			// TimeMilliseconds encodes 105.500s while Time says 100s (e.g. from
			// a malicious peer). Only the 500ms sub-second remainder is honored.
			name:      "milliseconds_disagreeing_on_second_ignored",
			header:    customtypes.WithHeaderExtra(&types.Header{Time: 100}, &customtypes.HeaderExtra{TimeMilliseconds: new(uint64(105_500))}),
			wantMilli: 100_500,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hooks.BlockTime(tt.header)
			require.Equal(t, tt.wantMilli, got.UnixMilli(), "hooks.BlockTime().UnixMilli()")
			// Documented invariant: BlockTime(h).Unix() == h.Time, i.e. the
			// second is just the millisecond value floored.
			require.Equal(t, tt.wantMilli/1000, got.Unix(), "hooks.BlockTime().Unix()")
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

func TestTargetExponent(t *testing.T) {
	const fortunaTime = 100

	tests := []struct {
		name    string
		header  *types.Header
		want    dynamic.TargetExponent
		wantErr error
	}{
		{
			name: "header_carries_exponent",
			header: customtypes.WithHeaderExtra(
				&types.Header{Number: big.NewInt(1)},
				&customtypes.HeaderExtra{TargetExponent: new(dynamic.TargetExponent(42))},
			),
			want: 42,
		},
		{
			name:   "no_field_pre_fortuna",
			header: &types.Header{Time: fortunaTime - 1, Number: big.NewInt(1)},
			want:   dynamic.InitialTargetExponent,
		},
		{
			name:   "no_field_genesis",
			header: &types.Header{Time: fortunaTime, Number: big.NewInt(0)},
			want:   dynamic.InitialTargetExponent,
		},
		{
			name:    "no_field_fortuna_parses_extra",
			header:  &types.Header{Time: fortunaTime, Number: big.NewInt(1)},
			wantErr: customtypes.ErrInsufficientFeeState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fortuna := &cparams.ChainConfig{
				FortunaTimestamp: new(uint64(fortunaTime)),
			}
			got, err := targetExponent(fortuna, tt.header)
			require.ErrorIs(t, err, tt.wantErr, "targetExponent()")
			assert.Equal(t, tt.want, got, "targetExponent()")
		})
	}
}

// Verifies that [hooks.SettledBy] decodes the marker that [builder.BuildBlock]
// writes into the header, and returns the zero marker when the header carries none.
func TestSettledBy(t *testing.T) {
	key := txtest.NewKey(t)
	_, sut := newSUT(t, withMaxAllocFor(key.EthAddress()))
	hooks := sut.hooks(t)

	stx := newWallet(key, sut.ctx, sut.Client).newMinimalTx(t)
	htx, err := newHookTx(stx, sut.ctx.AVAXAssetID)
	require.NoError(t, err, "newHookTx()")

	// built returns the header of a block built carrying the given settled marker.
	built := func(t *testing.T, settled hook.Settled) *types.Header {
		t.Helper()

		block, err := hooks.BuildBlock(
			&types.Header{},
			nil, // blockContext
			nil, // ethTxs
			nil, // receipts
			[]*hookTx{htx},
			settled,
		)
		require.NoError(t, err, "builder.BuildBlock()")
		return block.Header()
	}

	nonzero := hook.Settled{
		Height:       7,
		GasUnix:      1_000,
		GasNumerator: gas.Gas(3),
		Excess:       gas.Gas(42),
	}
	tests := []struct {
		name   string
		header *types.Header
		want   hook.Settled
	}{
		{
			name:   "absent_marker",
			header: &types.Header{},
			want:   hook.Settled{},
		},
		{
			name:   "zero",
			header: built(t, hook.Settled{}),
			want:   hook.Settled{},
		},
		{
			name:   "nonzero",
			header: built(t, nonzero),
			want:   nonzero,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, hooks.SettledBy(tt.header), "hooks.SettledBy()")
		})
	}
}

// Drives the builder in the order SAE calls it, so [hookstest.Conformance]
// fails if the C-Chain changes a header value it must not.
func TestBuilderConformance(t *testing.T) {
	key := txtest.NewKey(t)
	ctx, sut := newSUT(t, withMaxAllocFor(key.EthAddress()))
	parent, err := sut.ethclient.HeaderByNumber(ctx, big.NewInt(0))
	require.NoError(t, err, "HeaderByNumber(0)")
	// A sub-second block time, so that dropping its milliseconds is visible.
	sut.clock.Advance(1500 * time.Millisecond)

	stx := newWallet(key, sut.ctx, sut.Client).newMinimalTx(t)
	htx, err := newHookTx(stx, sut.ctx.AVAXAssetID)
	require.NoError(t, err, "newHookTx()")

	build := func(t *testing.T, builder hook.BlockBuilder[*hookTx]) *types.Block {
		t.Helper()

		hdr, err := builder.BuildHeader(parent)
		require.NoError(t, err, "BuildHeader()")
		hdr.Root = parent.Root
		hdr.GasLimit = parent.GasLimit
		hdr.BaseFee = parent.BaseFee
		noBlocks := func(common.Hash, uint64) (*types.Block, bool) {
			return nil, false
		}
		for range builder.PotentialEndOfBlockOps(ctx, hdr, parent.Hash(), noBlocks) {
		}
		block, err := builder.BuildBlock(
			hdr,
			nil, // blockContext
			nil, // ethTxs
			nil, // receipts
			[]*hookTx{htx},
			hook.Settled{GasUnix: parent.Time},
		)
		require.NoError(t, err, "BuildBlock()")
		return block
	}

	hooks := hookstest.Conformance(t, sut.hooks(t))
	built := build(t, hooks)
	rebuilder, err := hooks.BlockRebuilderFrom(built)
	require.NoError(t, err, "BlockRebuilderFrom()")
	assert.Equal(t, built.Hash(), build(t, rebuilder).Hash(), "rebuilt block hash")
}
