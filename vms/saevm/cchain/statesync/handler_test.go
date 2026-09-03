// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
	saestatesync "github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

const (
	// commitInterval is the trie commit interval used by every handler under test.
	commitInterval = 4
	// settleLag is the number of blocks behind a block that its settled height is.
	settleLag = 3
)

type (
	// SUT bundles a [SummaryHandler] with the state it was built over. It is driven
	// entirely through hand-populated storage, with no VM.
	SUT struct {
		*SummaryHandler
		state *state.State
	}

	sutConfig struct {
		// lastExecuted is the last block height that has been applied to the state.
		lastExecuted uint64
		// enabled indicates whether state sync is enabled.
		enabled bool
	}
	sutOption = options.Option[sutConfig]
)

// withNumExecutedBlocks returns a [sutOption] that sets the number of blocks
// that have been applied to the state.
func withNumExecutedBlocks(num uint64) sutOption {
	return options.Func[sutConfig](func(cfg *sutConfig) {
		cfg.lastExecuted = num
	})
}

// withEnabled returns a [sutOption] that sets whether state sync is enabled.
func withEnabled(enabled bool) sutOption {
	return options.Func[sutConfig](func(cfg *sutConfig) {
		cfg.enabled = enabled
	})
}

// newSUT builds a handler over an in-memory ethdb and state, where blocks
// 1..lastExecuted are canonical and an atomic root is applied at each of those
// heights. Each block at height h settles height the block [settleLag] behind it.
//
// By default, only the genesis is written.
func newSUT(t *testing.T, opts ...sutOption) *SUT {
	t.Helper()

	cfg := options.As(opts...)

	db := memdb.New()
	smDB := prefixdb.New([]byte("shared memory"), db)
	mem := chainsatomic.NewMemory(smDB)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.SharedMemory = mem.NewSharedMemory(snowtest.CChainID)

	st, err := state.New(snowCtx, prefixdb.New([]byte("chain"), db))
	require.NoError(t, err, "state.New()")

	// Apply a distinct atomic root at every executed height.
	var build exportBuilder
	for h := uint64(1); h <= cfg.lastExecuted; h++ {
		require.NoErrorf(t, st.Apply(h, []*tx.Tx{build.newExport()}), "Apply(%d)", h)
	}

	// Write every executed block to the ethdb and mark the tip as last-accepted.
	// The genesis block (height 0) is shared with the handler so both agree on
	// its hash.
	ethDB := rawdb.NewMemoryDatabase()
	genesis := newBlock(0)
	writeBlock(ethDB, genesis)
	for h := uint64(1); h <= cfg.lastExecuted; h++ {
		writeBlock(ethDB, newBlock(h))
	}

	net, err := network.New(snowCtx, &enginetest.Sender{})
	require.NoError(t, err, "network.New()")

	handler, err := New(
		saestatesync.Config{
			DBConfig: saedb.Config{CommitInterval: commitInterval},
			Enabled:  cfg.enabled,
		},
		ethDB,
		snowCtx,
		net,
		hookStub{},
		st,
	)
	require.NoError(t, err, "New()")
	return &SUT{
		SummaryHandler: handler,
		state:          st,
	}
}

func writeBlock(ethDB ethdb.Database, blk *types.Block) {
	hdr := blk.Header()
	rawdb.WriteHeader(ethDB, hdr)
	rawdb.WriteCanonicalHash(ethDB, hdr.Hash(), hdr.Number.Uint64())
	rawdb.WriteHeadFastBlockHash(ethDB, hdr.Hash())
}

func newBlock(height uint64) *types.Block {
	return types.NewBlockWithHeader(&types.Header{Number: new(big.Int).SetUint64(height)})
}

// wantRoot returns the atomic root that a correct handler must embed for a
// summary served at blockHeight: the root at the height that block settles.
func (s *SUT) wantRoot(t *testing.T, blockHeight uint64) common.Hash {
	t.Helper()

	settled := settledHeightFor(blockHeight, settleLag)
	root, err := s.state.GetRoot(settled)
	require.NoErrorf(t, err, "GetRoot(%d)", settled)
	return root
}

// settledHeightFor returns the height a block at blockHeight settles, clamping
// to genesis for the earliest blocks.
func settledHeightFor(blockHeight, settleLag uint64) uint64 {
	if blockHeight <= settleLag {
		return 0
	}
	return blockHeight - settleLag
}

type hookStub struct {
	hook.Points
}

func (hookStub) SettledBy(hdr *types.Header) hook.Settled {
	return hook.Settled{Height: settledHeightFor(hdr.Number.Uint64(), settleLag)}
}

// exportBuilder produces export txs with unique amounts so that each applied
// height yields a distinct root.
type exportBuilder struct {
	amount uint64
}

func (b *exportBuilder) newExport() *tx.Tx {
	b.amount++
	return &tx.Tx{
		Unsigned: &tx.Export{
			DestinationChain: snowtest.XChainID,
			ExportedOutputs: []*avax.TransferableOutput{{
				Out: &secp256k1fx.TransferOutput{
					Amt: b.amount,
				},
			}},
		},
	}
}

func TestGetStateSummary(t *testing.T) {
	const lastExecuted = 2*commitInterval + 1
	sut := newSUT(t, withNumExecutedBlocks(lastExecuted))

	// Only committed heights can be served. Each settles a distinct, earlier
	// height, so an incorrect height selection would embed a different root.
	for _, blockHeight := range []uint64{commitInterval, 2 * commitInterval} {
		t.Run(fmt.Sprintf("height_%d", blockHeight), func(t *testing.T) {
			got, err := sut.GetStateSummary(t.Context(), blockHeight)
			require.NoErrorf(t, err, "GetStateSummary(%d)", blockHeight)
			require.Equalf(t, blockHeight, got.Height(), "GetStateSummary(%d).Height()", blockHeight)
			require.Equalf(t, sut.wantRoot(t, blockHeight), got.settledRoot, "GetStateSummary(%d).settledRoot", blockHeight)
		})
	}

	// The genesis block is synchronous, so its summary must not be served
	// even though height 0 is a committed height.
	t.Run("height_0", func(t *testing.T) {
		_, err := sut.GetStateSummary(t.Context(), 0)
		require.ErrorIs(t, err, database.ErrNotFound, "GetStateSummary(0)")
	})
}

func TestGetLastStateSummary(t *testing.T) {
	const (
		lastCommitted = 2 * commitInterval
		lastExecuted  = lastCommitted + 1
	)

	sut := newSUT(t, withNumExecutedBlocks(lastExecuted))
	got, err := sut.GetLastStateSummary(t.Context())
	require.NoError(t, err, "GetLastStateSummary()")
	require.Equal(t, uint64(lastCommitted), got.Height(), "GetLastStateSummary().Height()")
	require.Equal(t, sut.wantRoot(t, lastCommitted), got.settledRoot, "GetLastStateSummary().settledRoot")
}

func TestWaitForEvent(t *testing.T) {
	handler := newSUT(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := handler.WaitForEvent(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAcceptSummary(t *testing.T) {
	handler := newSUT(t)
	mode, err := handler.AcceptSummary(t.Context(), &summary{})
	require.NoError(t, err)
	require.Equal(t, block.StateSyncSkipped, mode)
}

// TestStateSyncEnabled checks that the configured value is reported back by
// [SummaryHandler.StateSyncEnabled].
func TestStateSyncEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "disabled",
			enabled: false,
		},
		{
			name:    "enabled",
			enabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sut := newSUT(t, withEnabled(tt.enabled))

			gotEnabled, err := sut.StateSyncEnabled(t.Context())
			require.NoErrorf(t, err, "%T.StateSyncEnabled()", sut.SummaryHandler)
			assert.Equalf(t, tt.enabled, gotEnabled, "%T.StateSyncEnabled()", sut.SummaryHandler)
		})
	}
}
