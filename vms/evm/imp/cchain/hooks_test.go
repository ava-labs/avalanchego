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

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
	"github.com/ava-labs/avalanchego/vms/evm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
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
			wantErr: cparams.ErrInsufficientFeeState,
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

// Drives the builder in the order SAE calls it, so [hookstest.Conformance]
// fails if the C-Chain changes a header value it must not.
func TestBuilderConformance(t *testing.T) {
	key := txtest.NewKey(t)
	ctx, sut := newSUT(t, withMaxAllocFor(key.EthAddress()))
	parent, err := sut.EthClient().HeaderByNumber(ctx, big.NewInt(0))
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

	hooks := hookstest.Conformance(t, newHooks(
		sut.ctx,
		sut.VM.state,
		sut.VM.chainConfig,
		sut.VM.pending,
		warp.NewStorage(memdb.New()),
		sut.VM.now,
		desiredParams{},
		sut.VM.metrics,
	))
	built := build(t, hooks)
	rebuilder, err := hooks.BlockRebuilderFrom(built)
	require.NoError(t, err, "BlockRebuilderFrom()")
	assert.Equal(t, built.Hash(), build(t, rebuilder).Hash(), "rebuilt block hash")
}
