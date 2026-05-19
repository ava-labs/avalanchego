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
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
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

func TestAncestorInputIDs(t *testing.T) {
	w := newWallet(txtest.NewKey(t), snowtest.Context(t, snowtest.CChainID), nil)
	export := func() *tx.Tx {
		const (
			exportedAmount = 50
			txFee          = 0
		)
		signedExport, _ := w.newExportTx(
			t,
			snowtest.XChainID,
			[]*secp256k1fx.TransferOutput{
				txtest.NewTransferOutput(exportedAmount, w.sk.Address()),
			},
			txFee,
		)
		return signedExport
	}

	var (
		settled = common.Hash(ids.GenerateTestID())

		tx1    = export()
		block1 = newBlock(t, 1, settled, tx1)
		tx2    = export()
		block2 = newBlock(t, 2, block1.Hash(), tx2)
		tx3    = export()
		block3 = newBlock(t, 3, block2.Hash(), tx3)
		block4 = newBlock(t, 4, block3.Hash())
	)

	tests := []struct {
		name    string
		header  *types.Header
		settled common.Hash
		want    set.Set[ids.ID]
		wantErr error
	}{
		{
			name:    "empty range",
			header:  block1.Header(),
			settled: settled,
			want:    nil,
		},
		{
			name:    "single ancestor",
			header:  block2.Header(),
			settled: settled,
			want:    tx1.InputIDs(),
		},
		{
			name:    "multiple ancestors",
			header:  block4.Header(),
			settled: settled,
			want:    union(tx1.InputIDs(), tx2.InputIDs(), tx3.InputIDs()),
		},
		{
			name:    "missing block",
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

func union[T comparable](sets ...set.Set[T]) set.Set[T] {
	var out set.Set[T]
	for _, s := range sets {
		out.Union(s)
	}
	return out
}
