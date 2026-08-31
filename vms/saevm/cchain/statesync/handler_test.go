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
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
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

// newSUT builds a handler over an in-memory ethdb and state, where blocks
// 1..lastExecuted are canonical and an atomic root is applied at each of those
// heights. Each block at height h settles height the block [settleLag] behind it.
//
// By default, only the genesis is written.
func newSUT(t *testing.T, opts ...sutOption) *SUT {
	t.Helper()

	cfg := options.As(opts...)

	// Apply a distinct atomic root at every executed height.
	st := newState(t)
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

	handler, err := New(
		saestatesync.Config{DBConfig: saedb.Config{CommitInterval: commitInterval}},
		ethDB,
		hookStub{},
		st,
		loggingtest.New(t, logging.Debug),
	)
	require.NoError(t, err, "New()")
	t.Cleanup(func() {
		require.NoErrorf(t, handler.Shutdown(t.Context()), "%T.Shutdown()", handler)
	})
	return &SUT{
		SummaryHandler: handler,
		state:          st,
	}
}

// newState returns a [state.State] backed by a fresh db with working shared
// memory, ready for [state.State.Apply].
func newState(t *testing.T) *state.State {
	t.Helper()

	db := memdb.New()
	smDB := prefixdb.New([]byte("shared memory"), db)
	mem := chainsatomic.NewMemory(smDB)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.SharedMemory = mem.NewSharedMemory(snowtest.CChainID)

	st, err := state.New(snowCtx, prefixdb.New([]byte("chain"), db))
	require.NoError(t, err, "state.New()")
	return st
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
	for _, blockHeight := range []uint64{0, commitInterval, 2 * commitInterval} {
		t.Run(fmt.Sprintf("height_%d", blockHeight), func(t *testing.T) {
			got, err := sut.GetStateSummary(t.Context(), blockHeight)
			require.NoErrorf(t, err, "GetStateSummary(%d)", blockHeight)
			require.Equalf(t, blockHeight, got.Height(), "GetStateSummary(%d).Height()", blockHeight)
			require.Equalf(t, sut.wantRoot(t, blockHeight), got.settledRoot, "GetStateSummary(%d).settledRoot", blockHeight)
		})
	}
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

func TestOnlyGenesis(t *testing.T) {
	handler := newSUT(t)

	got, err := handler.GetLastStateSummary(t.Context())
	require.NoError(t, err, "GetLastStateSummary()")
	require.Equal(t, uint64(0), got.Height(), "GetLastStateSummary().Height()")
	require.Equal(t, types.EmptyRootHash, got.settledRoot, "GetLastStateSummary().settledRoot")

	got, err = handler.GetStateSummary(t.Context(), 0)
	require.NoError(t, err, "GetStateSummary(0)")
	require.Equal(t, uint64(0), got.Height(), "GetStateSummary(0).Height()")
	require.Equal(t, types.EmptyRootHash, got.settledRoot, "GetStateSummary(0).settledRoot")
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

// newHandler builds a [SummaryHandler] over an existing ethDB and atomic state,
// leaving how that state was acquired (executed vs. synced) to the caller.
func newHandler(t *testing.T, ethDB ethdb.Database, st *state.State) *SummaryHandler {
	t.Helper()

	handler, err := New(
		saestatesync.Config{DBConfig: saedb.Config{CommitInterval: commitInterval}},
		ethDB,
		hookStub{},
		st,
		loggingtest.New(t, logging.Debug),
	)
	require.NoError(t, err, "New()")
	t.Cleanup(func() {
		require.NoErrorf(t, handler.Shutdown(t.Context()), "%T.Shutdown()", handler)
	})
	return handler
}

// leafSyncInto leaf-syncs src's atomic trie into a fresh state over an in-memory
// p2p network and returns the synced state.
func leafSyncInto(t *testing.T, src *state.State) *state.State {
	t.Helper()

	targetHeight := src.CurrentHeight()
	target, err := src.GetRoot(targetHeight)
	require.NoErrorf(t, err, "src.GetRoot(%d)", targetHeight)

	net, tracker := synctest.NewSelfNetwork(t, t.Context(), ids.GenerateTestNodeID())
	require.NoError(t, state.RegisterSyncHandler(net, src), "RegisterSyncHandler()")

	dst := newState(t)
	require.NoError(t, state.NewSyncer(net, tracker, dst, target, targetHeight).Sync(t.Context()), "Sync()")
	return dst
}

// newCanonicalEthDB returns an in-memory ethdb with canonical blocks 0..upTo and
// its head at upTo.
func newCanonicalEthDB(t *testing.T, upTo uint64) ethdb.Database {
	t.Helper()

	ethDB := rawdb.NewMemoryDatabase()
	for h := uint64(0); h <= upTo; h++ {
		writeBlock(ethDB, newBlock(h))
	}
	return ethDB
}

// TestGetLastStateSummary_FreshlySyncedNodeServingWindow characterizes the real,
// bounded impact of the sync path's sparse root index: a freshly leaf-synced node
// temporarily declines to serve its last state summary, and recovers on its own
// once it executes past its sync point.
//
// This is a serving-side degradation, not a state-correctness bug. The synced
// node's atomic state is complete and it validates blocks normally; the only
// consumer of the sparse index is SummaryHandler.wrap, which needs the atomic
// root at a summary's settled height (settleLag behind the committed block). Right
// after syncing to height H, the last committed summary settles a height that was
// acquired by sync and, if op-free, has no marker — so serving fails. As the node
// executes forward, its last committed summary settles a height it applied itself
// (always marked), so serving succeeds again.
func TestGetLastStateSummary_FreshlySyncedNodeServingWindow(t *testing.T) {
	const syncHeight = 2 * commitInterval // 8, a committed height

	// The height that a summary at syncHeight settles carries no atomic txs, so
	// its root marker exists only on a node that executed it.
	opFreeSettled := settledHeightFor(syncHeight, settleLag) // 5
	require.Positivef(t, opFreeSettled, "settled height for %d must be non-genesis", syncHeight)

	// Source: an export at every height in [1, syncHeight] except the op-free
	// settled height.
	src := newState(t)
	var build exportBuilder
	for h := uint64(1); h <= syncHeight; h++ {
		var txs []*tx.Tx
		if h != opFreeSettled {
			txs = []*tx.Tx{build.newExport()}
		}
		require.NoErrorf(t, src.Apply(h, txs), "src.Apply(%d)", h)
	}

	// An executed-from-genesis node serves its last summary: its index is dense.
	t.Run("executed_node_serves", func(t *testing.T) {
		sut := newHandler(t, newCanonicalEthDB(t, syncHeight), src)
		got, err := sut.GetLastStateSummary(t.Context())
		require.NoError(t, err, "GetLastStateSummary()")
		require.Equal(t, uint64(syncHeight), got.Height(), "summary height")
	})

	// A node that leaf-synced the identical trie, with its head at the sync
	// point, shares one ethdb across the window so it advances in place.
	dst := leafSyncInto(t, src)
	require.Equal(t, uint64(syncHeight), dst.CurrentHeight(), "synced CurrentHeight()")
	ethDB := newCanonicalEthDB(t, syncHeight)

	// wrap() logs the expected serving-window miss at Error, so record logs
	// rather than using loggingtest.New (which fails the test on Error logs).
	// That Error-level logging of an expected, transient miss is itself worth
	// noting; a recorder lets us assert it happens without failing.
	rec := loggingtest.NewRecorder(logging.Debug)
	sut, err := New(
		saestatesync.Config{DBConfig: saedb.Config{CommitInterval: commitInterval}},
		ethDB,
		hookStub{},
		dst,
		rec,
	)
	require.NoError(t, err, "New()")
	t.Cleanup(func() {
		require.NoErrorf(t, sut.Shutdown(t.Context()), "%T.Shutdown()", sut)
	})

	t.Run("freshly_synced_declines", func(t *testing.T) {
		// Last committed height is syncHeight, which settles the op-free height
		// acquired by sync: no marker, so serving fails.
		_, err := sut.GetLastStateSummary(t.Context())
		require.ErrorIs(t, err, database.ErrNotFound, "GetLastStateSummary() right after sync")
		require.NotEmpty(t, rec.At(logging.Error), "the transient miss is logged at Error")
	})

	t.Run("self_heals_after_advancing", func(t *testing.T) {
		// Execute forward past the sync point + settleLag. These heights are
		// applied by this node, so their markers are written even though they are
		// op-free.
		const advancedHeight = syncHeight + commitInterval // 12
		for h := uint64(syncHeight) + 1; h <= advancedHeight; h++ {
			require.NoErrorf(t, dst.Apply(h, nil), "dst.Apply(%d)", h)
			writeBlock(ethDB, newBlock(h))
		}

		// Last committed height is now advancedHeight, which settles a height
		// this node applied itself, so serving succeeds again.
		got, err := sut.GetLastStateSummary(t.Context())
		require.NoError(t, err, "GetLastStateSummary() after advancing")
		require.Equal(t, uint64(advancedHeight), got.Height(), "summary height")
	})
}
