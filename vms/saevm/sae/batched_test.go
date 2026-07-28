// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks/blockstest"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

func TestGetAncestors(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	batched, ok := sut.ChainVM.(block.BatchedChainVM)
	require.Truef(t, ok, "%T must implement block.BatchedChainVM", sut.ChainVM)

	const numBlocks = 5
	chain := []*blocks.Block{sut.genesis}
	for range numBlocks {
		chain = append(chain, sut.runConsensusLoop(t))
	}
	tip := chain[len(chain)-1]

	// fromTip[i] is the byte representation of the block i generations before
	// (and including) the tip; i.e. the expected GetAncestors() response when
	// requesting the tip without limits.
	fromTip := make([][]byte, 0, len(chain))
	for _, b := range slices.Backward(chain) {
		fromTip = append(fromTip, b.Bytes())
	}

	// non-canonical blocks
	const numNonCanonical = 3
	lastVerified := tip
	for range numNonCanonical {
		lastSnow := sut.createAndVerifyBlock(t, lastVerified)
		lastVerified = unwrap(t, lastSnow)
	}

	const noSizeLimit = 1e9 // sufficiently large
	tests := []struct {
		name            string
		blkID           ids.ID
		maxNum, maxSize int
		want            [][]byte
	}{
		{
			name:    "whole_chain",
			blkID:   tip.ID(),
			maxNum:  len(chain) + 3, // more than available
			maxSize: noSizeLimit,
			want:    fromTip,
		},
		{
			name:    "exact_num",
			blkID:   tip.ID(),
			maxNum:  len(chain),
			maxSize: noSizeLimit,
			want:    fromTip,
		},
		{
			name:    "max_num_truncates",
			blkID:   tip.ID(),
			maxNum:  3,
			maxSize: noSizeLimit,
			want:    fromTip[:3],
		},
		{
			name:    "max_size_truncates",
			blkID:   tip.ID(),
			maxNum:  len(chain),
			maxSize: len(fromTip[0]) + len(fromTip[1]) + 2*wrappers.IntLen, // inclusive bound; third block exceeds it
			want:    fromTip[:2],
		},
		{
			name:    "intlen_overhead",
			blkID:   tip.ID(),
			maxNum:  len(chain),
			maxSize: len(fromTip[0]) + len(fromTip[1]) + wrappers.IntLen, // inclusive bound; second block exceeds it
			want:    fromTip[:1],
		},
		{
			name:    "max_size_below_first_block",
			blkID:   tip.ID(),
			maxNum:  len(chain),
			maxSize: len(fromTip[0]) - 1,
			want:    fromTip[:1],
		},
		{
			name:    "unknown_block",
			blkID:   ids.GenerateTestID(),
			maxNum:  len(chain),
			maxSize: noSizeLimit,
		},
		{
			name:    "non_canonical_block",
			blkID:   lastVerified.ID(),
			maxNum:  len(chain),
			maxSize: noSizeLimit,
			want:    [][]byte{lastVerified.Bytes()}, // can't resolve parents
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := batched.GetAncestors(ctx, tt.blkID, tt.maxNum, tt.maxSize, time.Minute)
			require.NoErrorf(t, err, "GetAncestors(%s, %d, %d)", tt.blkID, tt.maxNum, tt.maxSize)
			require.Equalf(t, tt.want, got, "GetAncestors(%s, %d, %d)", tt.blkID, tt.maxNum, tt.maxSize)
		})
	}

	t.Run("returned_bytes_parse", func(t *testing.T) {
		got, err := batched.GetAncestors(ctx, tip.ID(), len(chain), noSizeLimit, time.Minute)
		require.NoError(t, err, "GetAncestors()")
		require.Len(t, got, len(chain), "GetAncestors()")

		parsed, err := batched.BatchedParseBlock(ctx, got)
		require.NoError(t, err, "BatchedParseBlock()")
		require.Len(t, parsed, len(chain), "BatchedParseBlock()")

		for i, b := range parsed {
			want := chain[len(chain)-1-i]
			assert.Equalf(t, want.ID(), b.ID(), "BatchedParseBlock()[%d].ID()", i)
			assert.Equalf(t, want.Height(), b.Height(), "BatchedParseBlock()[%d].Height()", i)
		}
	})
}

func TestBatchedParseBlock(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	batched, ok := sut.ChainVM.(block.BatchedChainVM)
	require.Truef(t, ok, "%T must implement block.BatchedChainVM", sut.ChainVM)

	const numBlocks = 5
	chain := []*blocks.Block{sut.genesis}
	for range numBlocks {
		chain = append(chain, sut.runConsensusLoop(t,
			sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
				To:        &zeroAddr,
				Gas:       params.TxGas,
				GasFeeCap: big.NewInt(1),
				Value:     big.NewInt(1),
			}),
		))
	}

	parsedChain := make([][]byte, len(chain))
	for i, b := range chain {
		parsedChain[i] = b.Bytes()
	}

	// Syntactically invalid block: valid RLP but the header's TxHash doesn't
	// commit to the (empty) body.
	invalidBlock := blockstest.NewBlock(t, types.NewBlockWithHeader(&types.Header{
		Number:    big.NewInt(1),
		UncleHash: types.EmptyUncleHash,
		TxHash:    common.Hash{}, // MUST be [types.EmptyTxsHash]
	}), nil, nil).Bytes()

	t.Run("whole_chain", func(t *testing.T) {
		got, err := batched.BatchedParseBlock(ctx, parsedChain)
		require.NoError(t, err, "BatchedParseBlock()")
		require.Len(t, got, len(chain), "BatchedParseBlock()")

		for i, b := range got {
			want := chain[i]
			assert.Equalf(t, want.ID(), b.ID(), "BatchedParseBlock()[%d].ID()", i)
			assert.Equalf(t, want.Height(), b.Height(), "BatchedParseBlock()[%d].Height()", i)
		}
	})

	t.Run("empty_input", func(t *testing.T) {
		got, err := batched.BatchedParseBlock(ctx, nil)
		require.NoError(t, err, "BatchedParseBlock(nil)")
		assert.Empty(t, got, "BatchedParseBlock(nil)")
	})

	t.Run("invalid_block_among_valid", func(t *testing.T) {
		_, err := batched.BatchedParseBlock(ctx, [][]byte{parsedChain[0], invalidBlock, parsedChain[1]})
		require.ErrorIs(t, err, errTxHashMismatch, "BatchedParseBlock([valid, invalid, valid])")
	})
}

func BenchmarkGetAncestors(b *testing.B) {
	opt, vmTime := withVMTime(b, time.Unix(saeparams.TauSeconds, 0))
	db, err := pebbledb.New(b.TempDir(), nil, loggingtest.New(b, logging.Info), prometheus.NewRegistry())
	require.NoError(b, err, "pebbledb.New()")
	b.Cleanup(func() { require.NoErrorf(b, db.Close(), "%T.Close()", db) })
	ctx, sut := newSUT(b, 1, opt, options.Func[sutConfig](func(c *sutConfig) {
		c.db = db
	}))

	// Mirror the limits used by snow/engine/snowman/getter when serving a
	// bootstrapper: see config.BootstrapAncestorsMaxContainersSentKey.
	const (
		numTxs        = 10
		maxBlocksNum  = 2000
		maxBlocksSize = constants.MaxContainersLen
	)

	buildBlock := func() *blocks.Block {
		txs := make([]*types.Transaction, numTxs)
		for i := range txs {
			txs[i] = sut.wallet.SetNonceAndSign(b, 0, &types.DynamicFeeTx{
				To:        &zeroAddr,
				Gas:       params.TxGas,
				GasFeeCap: big.NewInt(1),
				Value:     big.NewInt(1),
			})
		}
		return sut.runConsensusLoop(b, txs...)
	}

	var tip *blocks.Block
	for range maxBlocksNum {
		tip = buildBlock()
		vmTime.AdvanceToSettle(ctx, b, tip)
	}

	batched, ok := sut.ChainVM.(block.BatchedChainVM)
	require.Truef(b, ok, "%T must implement block.BatchedChainVM", sut.ChainVM)
	want, err := batched.GetAncestors(ctx, tip.ID(), maxBlocksNum, maxBlocksSize, time.Minute)
	require.NoError(b, err, "GetAncestors()")
	require.Greaterf(b, tip.Height()+1, uint64(len(want)), "size limit MUST truncate the response; increase the number of blocks built")

	type serialGetter struct{ block.Getter }
	for _, bench := range []struct {
		name string
		vm   block.Getter
	}{
		{"batched", sut.ChainVM},
		{"serial", serialGetter{sut.ChainVM}},
	} {
		b.Run(bench.name, func(b *testing.B) {
			for b.Loop() {
				got, err := block.GetAncestors(ctx, logging.NoLog{}, bench.vm, tip.ID(), maxBlocksNum, maxBlocksSize, time.Minute)
				require.NoError(b, err, "block.GetAncestors()")
				require.Len(b, got, len(want), "block.GetAncestors()")
			}
		})
	}
}
