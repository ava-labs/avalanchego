// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
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
	lastAccepted := chain[len(chain)-1]

	// fromAccepted[i] is the byte representation of the ith block before (and
	// including) the last accepted block.
	fromAccepted := make([][]byte, 0, len(chain))
	for _, b := range slices.Backward(chain) {
		fromAccepted = append(fromAccepted, b.Bytes())
	}

	verified := unwrap(t, sut.createAndVerifyBlock(t, lastAccepted))

	const noLimit = math.MaxInt
	tests := []struct {
		name    string
		blkID   ids.ID
		maxNum  int
		maxSize int
		want    [][]byte
	}{
		{
			name:    "whole_chain",
			blkID:   lastAccepted.ID(),
			maxNum:  noLimit,
			maxSize: noLimit,
			want:    fromAccepted,
		},
		{
			name:    "exact_num",
			blkID:   lastAccepted.ID(),
			maxNum:  len(chain),
			maxSize: noLimit,
			want:    fromAccepted,
		},
		{
			name:    "max_num_truncates",
			blkID:   lastAccepted.ID(),
			maxNum:  3,
			maxSize: noLimit,
			want:    fromAccepted[:3],
		},
		{
			name:    "max_size_truncates",
			blkID:   lastAccepted.ID(),
			maxNum:  len(chain),
			maxSize: len(fromAccepted[0]) + len(fromAccepted[1]) + 2*wrappers.IntLen, // inclusive; third block exceeds it
			want:    fromAccepted[:2],
		},
		{
			name:    "intlen_overhead",
			blkID:   lastAccepted.ID(),
			maxNum:  len(chain),
			maxSize: len(fromAccepted[0]) + len(fromAccepted[1]) + wrappers.IntLen, // inclusive bound; second block exceeds it
			want:    fromAccepted[:1],
		},
		{
			name:    "max_size_below_first_block",
			blkID:   lastAccepted.ID(),
			maxNum:  len(chain),
			maxSize: len(fromAccepted[0]) - 1,
			want:    fromAccepted[:1],
		},
		{
			name:    "unknown_block",
			blkID:   ids.GenerateTestID(),
			maxNum:  noLimit,
			maxSize: noLimit,
		},
		{
			name:    "non_canonical_block", // only accepted blocks are served
			blkID:   verified.ID(),
			maxNum:  noLimit,
			maxSize: noLimit,
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

	// invalidNumber is correctly encoded but rejected by [VM.ParseBlock] as the
	// height doesn't fit in a uint64.
	invalidNumber := encodeRLP(t, types.NewBlockWithHeader(&types.Header{
		Number: new(big.Int).Lsh(big.NewInt(1), 64),
	}))
	tests := []struct {
		name    string
		bytes   [][]byte
		want    []*blocks.Block
		wantErr error
	}{
		{
			name:  "empty",
			bytes: nil,
			want:  nil,
		},
		{
			name:  "whole_chain",
			bytes: bytes,
			want:  chain,
		},
		{
			name: "first_invalid",
			bytes: slices.Concat(
				[][]byte{invalidNumber},
				bytes,
			),
			wantErr: errBlockHeightNotUint64,
		},
		{
			name: "last_invalid",
			bytes: slices.Concat(
				bytes,
				[][]byte{invalidNumber},
			),
			wantErr: errBlockHeightNotUint64,
		},
	}

	// Accepted blocks carry execution state that freshly parsed blocks lack,
	// so the comparison is limited to the underlying eth blocks.
	opts := cmp.Options{
		cmp.Transformer("EthBlock", (*blocks.Block).EthBlock),
		cmputils.Blocks(),
		cmputils.Headers(),
		cmpopts.EquateEmpty(),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sut.rawVM.BatchedParseBlock(ctx, tt.bytes)
			require.ErrorIsf(t, err, tt.wantErr, "%T.BatchedParseBlock()", sut.rawVM)
			if diff := cmp.Diff(tt.want, got, opts); diff != "" {
				t.Errorf("%T.BatchedParseBlock() diff (-want +got):\n%s", sut.rawVM, diff)
			}
		})
	}
}

func BenchmarkGetAncestors(b *testing.B) {
	log := loggingtest.New(b, logging.Info)

	// The [SUT] uses in-memory databases by default. To make the benchmark a
	// bit more realistic, we provide real implementations here.
	db, err := pebbledb.New(b.TempDir(), nil, log, prometheus.NewRegistry())
	require.NoError(b, err, "pebbledb.New()")
	b.Cleanup(func() {
		require.NoErrorf(b, db.Close(), "%T.Close()", db)
	})

	xdb, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(b.TempDir()),
		log,
	)
	require.NoError(b, err, "blockdb.New()")
	b.Cleanup(func() {
		// This is normally closed by the SUT, but an error occurring before
		// creating the execution results prevents it from being closed there.
		_ = xdb.Close()
	})

	opt, vmTime := withVMTime(b, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(b, 1, opt, withExecResultsDB(xdb), options.Func[sutConfig](func(c *sutConfig) {
		c.db = db
	}))

	const numTxs = 10
	var tip *blocks.Block
	for range defaultAncestorsMaxBlockCount {
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
					defaultAncestorsMaxBlockCount,
					constants.MaxContainersLen,
					time.Minute,
				)
			}
		})
	}
}

// defaultAncestorsMaxBlockCount is the default maximum number of blocks
// requested by GetAncestors.
//
// TODO(StephenButtolph): This really isn't configurable. We should remove this
// as a flag and just make it a global constant.
const defaultAncestorsMaxBlockCount = 2000

func BenchmarkBatchedParseBlock(b *testing.B) {
	opt, vmTime := withVMTime(b, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(b, 1, opt)

	const numTxs = 10
	bufs := make([][]byte, defaultAncestorsMaxBlockCount)
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
