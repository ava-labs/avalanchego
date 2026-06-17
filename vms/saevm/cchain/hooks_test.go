// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

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

func TestBuildBlockSettledRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		settled hook.Settled
	}{
		{
			name:    "zero",
			settled: hook.Settled{},
		},
		{
			name: "nonzero",
			settled: hook.Settled{
				Height:       7,
				GasUnix:      1_000,
				GasNumerator: gas.Gas(3),
				Excess:       gas.Gas(42),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := customtypes.WithHeaderExtra(
				&types.Header{
					Number:           big.NewInt(1),
					BlobGasUsed:      new(uint64),
					ExcessBlobGas:    new(uint64),
					ParentBeaconRoot: new(common.Hash),
				},
				&customtypes.HeaderExtra{},
			)

			var b builder
			block, err := b.BuildBlock(header, nil, nil, nil, nil, tt.settled)
			require.NoError(t, err, "builder.BuildBlock()")

			// RLP round-trip the header to prove the marker is committed in the
			// hashed header, not just attached in memory.
			enc, err := rlp.EncodeToBytes(block.Header())
			require.NoError(t, err, "rlp.EncodeToBytes(header)")
			decoded := new(types.Header)
			require.NoError(t, rlp.DecodeBytes(enc, decoded), "rlp.DecodeBytes(header)")

			var h hooks
			require.Equal(t, tt.settled, h.SettledBy(decoded), "hooks.SettledBy() after round-trip")
		})
	}
}

func TestSettledByAbsent(t *testing.T) {
	header := customtypes.WithHeaderExtra(
		&types.Header{Number: big.NewInt(1)},
		&customtypes.HeaderExtra{},
	)
	var h hooks
	require.Equal(t, hook.Settled{}, h.SettledBy(header), "hooks.SettledBy() with no marker")
}
