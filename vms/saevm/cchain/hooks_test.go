// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// newBlockGeneric builds a [*types.Block] whose ExtData encodes txs, using the
// supplied header. setExtDataHash controls whether NewBlockWithExtData
// recomputes the committed ExtDataHash from extData (true) or keeps the value
// already on the header (false).
func newBlockGeneric(tb testing.TB, header *types.Header, setExtDataHash bool, txs ...*tx.Tx) *types.Block {
	tb.Helper()

	extData, err := tx.MarshalSlice(txs)
	require.NoErrorf(tb, err, "tx.MarshalSlice(%d txs)", len(txs))

	return customtypes.NewBlockWithExtData(
		header,
		nil, // txs
		nil, // uncles
		nil, // receipts
		saetest.TrieHasher(),
		extData,
		setExtDataHash,
	)
}

// newBlock returns a minimal [*types.Block] whose ExtData encodes txs and
// whose header is configured for ancestor traversal (parent hash + number).
func newBlock(tb testing.TB, number uint64, parent common.Hash, txs ...*tx.Tx) *types.Block {
	tb.Helper()

	return newBlockGeneric(
		tb,
		&types.Header{
			ParentHash: parent,
			Number:     new(big.Int).SetUint64(number),
		},
		true, // setExtDataHash
		txs...,
	)
}

// newTamperedBlock returns a block that encodes txs but whose header commits an
// ExtDataHash that does not match its extData, simulating tampering.
func newTamperedBlock(tb testing.TB, number uint64, parent common.Hash, txs ...*tx.Tx) *types.Block {
	tb.Helper()

	header := customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash: parent,
			Number:     new(big.Int).SetUint64(number),
		},
		&customtypes.HeaderExtra{ExtDataHash: common.Hash(ids.GenerateTestID())},
	)
	return newBlockGeneric(
		tb,
		header,
		false, // keep the mismatching ExtDataHash; do not recompute
		txs...,
	)
}

func TestParseAndVerifyBlockTxs(t *testing.T) {
	w := newWallet(txtest.NewKey(t), snowtest.Context(t, snowtest.CChainID), nil)
	tx1 := w.newMinimalTx(t)

	t.Run("valid", func(t *testing.T) {
		require := require.New(t)

		got, err := parseAndVerifyBlockTxs(newBlock(t, 1, common.Hash{}, tx1))
		require.NoError(err)
		require.Len(got, 1)
		require.Equal(tx1.ID(), got[0].ID())
	})

	t.Run("valid_empty", func(t *testing.T) {
		require := require.New(t)

		// A block with no txs has empty extData, which hashes to
		// EmptyExtDataHash; verification and parsing must both accept it.
		got, err := parseAndVerifyBlockTxs(newBlock(t, 1, common.Hash{}))
		require.NoError(err)
		require.Empty(got)
	})

	t.Run("hash_mismatch", func(t *testing.T) {
		require := require.New(t)

		// newTamperedBlock carries tx1's extData but commits an ExtDataHash that
		// does not match it. Verification must reject it, proving the committed
		// hash is bound to this block's actual extData content.
		_, err := parseAndVerifyBlockTxs(newTamperedBlock(t, 1, common.Hash{}, tx1))
		require.ErrorIs(err, errExtDataHashMismatch)
	})
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

		// tampered is an ancestor whose committed ExtDataHash does not match
		// its extData; tamperedChild points at it so the traversal parses it.
		tampered      = newTamperedBlock(t, 1, genesis, tx1)
		tamperedChild = newBlock(t, 2, tampered.Hash())
	)

	tests := []struct {
		name    string
		header  *types.Header
		settled common.Hash
		source  saetypes.BlockSource
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
		{
			name:    "hash_mismatch",
			header:  tamperedChild.Header(),
			settled: genesis,
			source: func(hash common.Hash, number uint64) (*types.Block, bool) {
				if hash == tampered.Hash() && number == 1 {
					return tampered, true
				}
				return nil, false
			},
			wantErr: errExtDataHashMismatch,
		},
	}
	sharedSource := func(hash common.Hash, number uint64) (*types.Block, bool) {
		for _, b := range []*types.Block{block1, block2, block3} {
			if b.Hash() == hash && b.NumberU64() == number {
				return b, true
			}
		}
		return nil, false
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			source := tt.source
			if source == nil {
				source = sharedSource
			}

			got, err := ancestorInputIDs(tt.header, tt.settled, source)
			require.ErrorIs(err, tt.wantErr, "ancestorInputIDs()")
			require.Equal(tt.want, got, "ancestorInputIDs()")
		})
	}
}
