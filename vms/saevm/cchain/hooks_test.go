// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
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
	hookstest.TestBlockTime(t, sut.hooks(t), func(h *types.Header, ms uint64) *types.Header {
		return customtypes.WithHeaderExtra(h, &customtypes.HeaderExtra{TimeMilliseconds: &ms})
	})
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
			name: "no_field_fortuna_legacy_state",
			header: &types.Header{
				Time:   fortunaTime,
				Number: big.NewInt(1),
				Extra:  (&acp176.State{TargetExcess: 5_000}).Bytes(),
			},
			want: 5_000,
		},
		{
			name: "no_field_fortuna_invalid_extra",
			header: &types.Header{
				Time:   fortunaTime,
				Number: big.NewInt(1),
				Extra:  []byte{0x01},
			},
			wantErr: acp176.ErrStateInsufficientLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fortuna := &extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					FortunaTimestamp: new(uint64(fortunaTime)),
				},
			}
			got, err := targetExponent(fortuna, tt.header)
			require.ErrorIs(t, err, tt.wantErr, "targetExponent()")
			assert.Equal(t, tt.want, got, "targetExponent()")
		})
	}
}

func TestSettledBy(t *testing.T) {
	key := txtest.NewKey(t)
	_, sut := newSUT(t, withMaxAllocFor(key.EthAddress()))

	stx := newWallet(key, sut.ctx, sut.Client).newMinimalTx(t)
	htx, err := newHookTx(stx, sut.ctx.AVAXAssetID)
	require.NoError(t, err, "newHookTx()")

	hookstest.TestSettledBy(t, sut.hooks(t), nil, []*hookTx{htx})
}
