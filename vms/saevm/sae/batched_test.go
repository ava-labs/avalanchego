// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/x/blockdb"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

func TestGetAncestors(t *testing.T) {
	ctx, sut := newSUT(t, 1)

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
			want:    nil, // only accepted blocks are served
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sut.GetAncestors(ctx, tt.blkID, tt.maxNum, tt.maxSize, time.Minute)
			require.NoErrorf(t, err, "GetAncestors(%s, %d, %d)", tt.blkID, tt.maxNum, tt.maxSize)
			require.Equalf(t, tt.want, got, "GetAncestors(%s, %d, %d)", tt.blkID, tt.maxNum, tt.maxSize)
		})
	}
}

func TestBatchedParseBlock(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	const numBlocks = 5
	chain := []*blocks.Block{sut.genesis}
	for range numBlocks {
		chain = append(chain, sut.runConsensusLoop(t))
	}

	bytes := make([][]byte, len(chain))
	for i, b := range chain {
		bytes[i] = b.Bytes()
	}

	tests := []struct {
		name    string
		bufs    [][]byte
		want    []*blocks.Block
		wantErr string // required substring of the error; empty means no error
	}{
		{
			name: "whole_chain",
			bufs: bytes,
			want: chain,
		},
		{
			name:    "invalid_block",
			bufs:    append([][]byte{[]byte("not a block")}, bytes...),
			wantErr: "rlp.DecodeBytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := sut.BatchedParseBlock(ctx, tt.bufs)
			if tt.wantErr != "" {
				require.ErrorContainsf(t, err, tt.wantErr, "%T.BatchedParseBlock()", sut.ChainVMWithContext)
				return
			}
			require.NoErrorf(t, err, "%T.BatchedParseBlock()", sut.ChainVMWithContext)
			require.Lenf(t, parsed, len(tt.want), "%T.BatchedParseBlock()", sut.ChainVMWithContext)
			for i, b := range parsed {
				require.Equalf(t, tt.want[i].ID(), b.ID(), "%T.BatchedParseBlock()[%d].ID()", sut.ChainVMWithContext, i)
				require.Equalf(t, tt.bufs[i], b.Bytes(), "%T.BatchedParseBlock()[%d].Bytes()", sut.ChainVMWithContext, i)
			}
		})
	}
}

func BenchmarkGetAncestors(b *testing.B) {
	log := loggingtest.New(b, logging.Info)

	db, err := pebbledb.New(b.TempDir(), nil, log, prometheus.NewRegistry())
	require.NoError(b, err, "pebbledb.New()")
	b.Cleanup(func() { require.NoErrorf(b, db.Close(), "%T.Close()", db) })

	// Closed by SUT
	xdb, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(b.TempDir()),
		log,
	)
	require.NoError(b, err, "blockdb.New()")

	opt, vmTime := withVMTime(b, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(b, 1, opt, withExecResultsDB(xdb), options.Func[sutConfig](func(c *sutConfig) {
		c.db = db
	}))

	const (
		numTxs        = 10
		maxBlocksNum  = 2000
		maxBlocksSize = constants.MaxContainersLen
	)
	var tip *blocks.Block
	for range maxBlocksNum {
		txs := make([]*types.Transaction, numTxs)
		for i := range txs {
			txs[i] = sut.wallet.SetNonceAndSign(b, 0, &types.DynamicFeeTx{
				To:        &zeroAddr,
				Gas:       params.TxGas,
				GasFeeCap: big.NewInt(1),
				Value:     big.NewInt(1),
			})
		}
		tip = sut.runConsensusLoop(b, txs...)
		vmTime.AdvanceToSettle(ctx, b, tip)
	}
	tipID := tip.ID()

	type serialGetter struct{ block.Getter }
	for _, bench := range []struct {
		name string
		vm   block.Getter
	}{
		{"batched", sut},
		{"serial", serialGetter{sut}},
	} {
		b.Run(bench.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = block.GetAncestors(
					ctx,
					sut.logger,
					bench.vm,
					tipID,
					maxBlocksNum,
					maxBlocksSize,
					time.Minute,
				)
			}
		})
	}
}

func BenchmarkBatchedParseBlock(b *testing.B) {
	opt, vmTime := withVMTime(b, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(b, 1, opt)

	const (
		numTxs    = 10
		numBlocks = 2000
	)
	bufs := make([][]byte, numBlocks)
	for i := range bufs {
		txs := make([]*types.Transaction, numTxs)
		for j := range txs {
			txs[j] = sut.wallet.SetNonceAndSign(b, 0, &types.DynamicFeeTx{
				To:        &zeroAddr,
				Gas:       params.TxGas,
				GasFeeCap: big.NewInt(1),
				Value:     big.NewInt(1),
			})
		}
		tip := sut.runConsensusLoop(b, txs...)
		vmTime.AdvanceToSettle(ctx, b, tip)
		bufs[i] = tip.Bytes()
	}

	type serialParser struct{ block.Parser }
	for _, bench := range []struct {
		name string
		vm   block.Parser
	}{
		{"batched", sut},
		{"serial", serialParser{sut}},
	} {
		b.Run(bench.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = block.BatchedParseBlock(ctx, bench.vm, bufs)
			}
		})
	}
}
