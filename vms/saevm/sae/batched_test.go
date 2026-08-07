// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rlp"
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
			want:    nil, // only accepted blocks are served
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := batched.GetAncestors(ctx, tt.blkID, tt.maxNum, tt.maxSize, time.Minute)
			require.NoErrorf(t, err, "GetAncestors(%s, %d, %d)", tt.blkID, tt.maxNum, tt.maxSize)
			require.Equalf(t, tt.want, got, "GetAncestors(%s, %d, %d)", tt.blkID, tt.maxNum, tt.maxSize)
		})
	}
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

	bytes := make([][]byte, len(chain))
	for i, b := range chain {
		bytes[i] = b.Bytes()
	}

	t.Run("batched_parse", func(t *testing.T) {
		parsed, err := batched.BatchedParseBlock(ctx, bytes)
		require.NoErrorf(t, err, "%T.BatchedParseBlock()", batched)
		require.Lenf(t, parsed, len(chain), "%T.BatchedParseBlock()", batched)
		for i, b := range parsed {
			require.Equalf(t, chain[i].ID(), b.ID(), "%T.BatchedParseBlock()[%d].ID()", batched, i)
			require.Equalf(t, bytes[i], b.Bytes(), "%T.BatchedParseBlock()[%d].Bytes()", batched, i)
		}
	})

	t.Run("invalid_block", func(t *testing.T) {
		bufs := append([][]byte{[]byte("not a block")}, bytes...)
		_, err := batched.BatchedParseBlock(ctx, bufs)
		require.ErrorContainsf(t, err, "rlp.DecodeBytes", "%T.BatchedParseBlock()", batched)
	})

	t.Run("batched_implementer_works", func(t *testing.T) {
		parsed, err := block.BatchedParseBlock(t.Context(), sut, bytes)
		require.NoError(t, err, "block.BatchedParseBlock()")
		require.Len(t, parsed, len(chain), "block.BatchedParseBlock()")
	})
}

// newRawdbTestBlock returns a block with contents unique to the arguments so
// that blocks at equal heights have distinct hashes, headers and bodies.
func newRawdbTestBlock(height uint64, extra string) *types.Block {
	header := &types.Header{
		Number: new(big.Int).SetUint64(height),
		Extra:  []byte(extra),
	}
	uncle := &types.Header{
		Number: new(big.Int).SetUint64(height),
		Extra:  []byte("uncle of " + extra),
	}
	return types.NewBlockWithHeader(header).WithBody(types.Body{
		Uncles: []*types.Header{uncle},
	})
}

// TestReadCanonicalRLPRange exercises [readCanonicalRLPRange] over a database
// populated only via [rawdb], covering height gaps, hash-only heights and
// non-canonical siblings.
func TestReadCanonicalRLPRange(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	const (
		lastHeight     = 12
		gapHeight      = 4 // nothing stored
		siblingHeight  = 6 // canonical and non-canonical blocks stored
		hashOnlyHeight = 8 // canonical hash stored without header or body
	)
	for height := uint64(0); height <= lastHeight; height++ {
		if height == gapHeight {
			continue
		}
		b := newRawdbTestBlock(height, "canonical")
		rawdb.WriteCanonicalHash(db, b.Hash(), height)
		if height == hashOnlyHeight {
			continue
		}
		rawdb.WriteBlock(db, b)
	}
	// Non-canonical siblings, as written by older versions, MUST NOT leak
	// into the results even if one sorts after the canonical block within its
	// height.
	rawdb.WriteBlock(db, newRawdbTestBlock(siblingHeight, "non-canonical"))
	rawdb.WriteBlock(db, newRawdbTestBlock(siblingHeight, "another non-canonical"))

	// Excludes stored heights at both ends to test range bounds.
	const from, to = 2, 10
	want := make([]storedBlockRLP, to-from+1)
	for i := range want {
		num := uint64(from + i)
		hash := rawdb.ReadCanonicalHash(db, num)
		if hash == (common.Hash{}) {
			continue
		}
		want[i] = storedBlockRLP{
			hash:   hash,
			header: rawdb.ReadHeaderRLP(db, hash, num),
			body:   rawdb.ReadBodyRLP(db, hash, num),
		}
	}

	got, err := readCanonicalRLPRange(t.Context(), db, from, to)
	require.NoErrorf(t, err, "readCanonicalRLPRange(..., %d, %d)", from, to)
	require.Equalf(t, want, got, "readCanonicalRLPRange(..., %d, %d)", from, to)
}

func TestAncestorsDescending(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	const (
		numBlocks = 300 // spans multiple pages of ancestorsPageSize heights
		gapHeight = 150 // MUST truncate any response reaching down to it
	)
	var (
		encoded = make([][]byte, numBlocks) // encoded[i] is the block at height i
		hashes  = make([]common.Hash, numBlocks)
	)
	for height := uint64(0); height < numBlocks; height++ {
		if height == gapHeight {
			continue
		}
		b := newRawdbTestBlock(height, "canonical")
		rawdb.WriteBlock(db, b)
		rawdb.WriteCanonicalHash(db, b.Hash(), height)
		hashes[height] = b.Hash()

		enc, err := rlp.EncodeToBytes(b)
		require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", b)
		encoded[height] = enc
	}

	// wantDescending(hi, n) is the expected response of blocks at heights
	// (hi-n, hi], highest first.
	wantDescending := func(hi uint64, n int) [][]byte {
		want := make([][]byte, 0, n)
		for i := range n {
			want = append(want, encoded[hi-uint64(i)])
		}
		return want
	}

	const noSizeLimit = 1e9 // sufficiently large
	tests := []struct {
		name     string
		lo, base uint64
		hash     common.Hash
		maxSize  int
		want     [][]byte
	}{
		{
			name:    "single_page",
			lo:      290,
			base:    numBlocks - 1,
			hash:    hashes[numBlocks-1],
			maxSize: noSizeLimit,
			want:    wantDescending(numBlocks-1, 10),
		},
		{
			name:    "gap_truncates_across_pages",
			lo:      0,
			base:    numBlocks - 1,
			hash:    hashes[numBlocks-1],
			maxSize: noSizeLimit,
			want:    wantDescending(numBlocks-1, numBlocks-gapHeight-1),
		},
		{
			name: "size_limit_truncates",
			lo:   200,
			base: numBlocks - 1,
			hash: hashes[numBlocks-1],
			maxSize: len(encoded[numBlocks-1]) + len(encoded[numBlocks-2]) +
				2*wrappers.IntLen, // inclusive bound; third block exceeds it
			want: wantDescending(numBlocks-1, 2),
		},
		{
			name:    "size_limit_below_first_block",
			lo:      200,
			base:    numBlocks - 1,
			hash:    hashes[numBlocks-1],
			maxSize: len(encoded[numBlocks-1]) + wrappers.IntLen - 1,
			want:    wantDescending(numBlocks-1, 1), // first block always included
		},
		{
			name:    "down_to_genesis_across_pages",
			lo:      0,
			base:    gapHeight - 1,
			hash:    hashes[gapHeight-1],
			maxSize: noSizeLimit,
			want:    wantDescending(gapHeight-1, gapHeight),
		},
		{
			name:    "base_hash_not_canonical",
			lo:      200,
			base:    numBlocks - 1,
			hash:    hashes[250], // a canonical hash, but not of the base height
			maxSize: noSizeLimit,
			want:    nil,
		},
		{
			name:    "base_height_missing",
			lo:      100,
			base:    gapHeight,
			hash:    hashes[gapHeight+1], // nothing is stored at gapHeight
			maxSize: noSizeLimit,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ancestorsDescending(t.Context(), db, tt.hash, tt.lo, tt.base, tt.maxSize)
			require.NoErrorf(t, err, "ancestorsDescending(..., %d, %d, %d)", tt.lo, tt.base, tt.maxSize)
			require.Equalf(t, tt.want, got, "ancestorsDescending(..., %d, %d, %d)", tt.lo, tt.base, tt.maxSize)
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
				b.ReportMetric(float64(len(got)), "blocks")
			}
		})
	}
}

/*
goos: darwin
goarch: arm64
pkg: github.com/ava-labs/avalanchego/vms/saevm/sae
cpu: Apple M2 Max
BenchmarkGetAncestors/batched-12         	    1960	    542917 ns/op	      1054 blocks	 4200376 B/op	    8268 allocs/op
BenchmarkGetAncestors/serial-12          	      20	  54339985 ns/op	      1054 blocks	37979158 B/op	  537176 allocs/op
PASS
ok  	github.com/ava-labs/avalanchego/vms/saevm/sae	44.907s

goos: darwin
goarch: arm64
pkg: github.com/ava-labs/avalanchego/vms/saevm/sae
cpu: Apple M2 Max
BenchmarkGetAncestors/batched-12         	      74	  15920889 ns/op	      1054 blocks	14938205 B/op	  251162 allocs/op
BenchmarkGetAncestors/serial-12          	      19	  59969941 ns/op	      1054 blocks	37900856 B/op	  537168 allocs/op
PASS
ok  	github.com/ava-labs/avalanchego/vms/saevm/sae	45.513s
*/

func BenchmarkBatchedParseBlock(b *testing.B) {
	opt, vmTime := withVMTime(b, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(b, 1, opt)

	// Mirror the largest batch a bootstrapper parses in one call: see
	// config.BootstrapAncestorsMaxContainersReceivedKey.
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
		{"batched", sut.ChainVM},
		{"serial", serialParser{sut.ChainVM}},
	} {
		b.Run(bench.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = block.BatchedParseBlock(ctx, bench.vm, bufs)
			}
		})
	}
}

/*
goos: darwin
goarch: arm64
pkg: github.com/ava-labs/avalanchego/vms/saevm/sae
cpu: Apple M2 Max
BenchmarkBatchedParseBlock/batched-12         	     100	  10505568 ns/op	31995359 B/op	  625716 allocs/op
BenchmarkBatchedParseBlock/serial-12          	      24	  49528109 ns/op	31736952 B/op	  622088 allocs/op
PASS
ok  	github.com/ava-labs/avalanchego/vms/saevm/sae	8.605s
*/
