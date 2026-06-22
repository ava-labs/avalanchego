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

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
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

// Verifies that [hooks.SettledBy] decodes the marker that [builder.BuildBlock]
// writes into the header, and returns the zero marker when the header carries
// none. The all-zero marker is encoded as non-nil zero pointers, distinct from
// an absent marker. The RLP codec for these header fields is covered by coreth's
// customtypes tests.
func TestSettledBy(t *testing.T) {
	newHeader := func() *types.Header {
		return customtypes.WithHeaderExtra(&types.Header{Number: big.NewInt(1)}, &customtypes.HeaderExtra{})
	}
	// built returns the header of a block built carrying the given settled marker.
	built := func(t *testing.T, settled hook.Settled) *types.Header {
		t.Helper()
		var b builder
		block, err := b.BuildBlock(newHeader(), nil, nil, nil, nil, settled)
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
			header: newHeader(),
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
			var h hooks
			require.Equal(t, tt.want, h.SettledBy(tt.header), "hooks.SettledBy()")
		})
	}
}
