// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
	saestatesync "github.com/ava-labs/avalanchego/vms/saevm/statesync"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// commitInterval is the trie commit interval used by every handler under test.
const commitInterval = 4

// SUT bundles a [SummaryHandler] with the state it was built over. It is driven
// entirely through hand-populated storage, with no VM.
type SUT struct {
	*SummaryHandler

	state     *state.State
	settleLag uint64
}

// newSUT builds a handler over an in-memory ethdb and state, where blocks
// 1..lastExecuted are canonical and an atomic root is applied at each of those
// heights. Each block at height h settles height [settledHeightFor](h), i.e. the
// block lag heights behind it.
func newSUT(t *testing.T, settleLag, lastExecuted uint64) *SUT {
	t.Helper()
	require.Less(t, settleLag, lastExecuted, "lag cannot exceed executed height")

	// Apply a distinct atomic root at every executed height.
	st := newState(t)
	var build exportBuilder
	for h := uint64(1); h <= lastExecuted; h++ {
		require.NoErrorf(t, st.Apply(h, []*tx.Tx{build.newExport()}), "Apply(%d)", h)
	}

	// Write every executed block to the ethdb and mark the tip as last-accepted.
	// The genesis block (height 0) is shared with the handler so both agree on
	// its hash.
	ethDB := saetypes.NewEthDB(memdb.New())
	genesis := newBlock(0)
	writeBlock(ethDB, genesis)
	for h := uint64(1); h <= lastExecuted; h++ {
		writeBlock(ethDB, newBlock(h))
	}

	handler := newHandler(t, ethDB, st, hookStub{lag: settleLag}, genesis)
	return &SUT{
		SummaryHandler: handler,
		state:          st,
		settleLag:      settleLag,
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

	settled := settledHeightFor(blockHeight, s.settleLag)
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

	lag uint64
}

func (h hookStub) SettledBy(hdr *types.Header) hook.Settled {
	return hook.Settled{Height: settledHeightFor(hdr.Number.Uint64(), h.lag)}
}

var _ hook.Points = hookStub{}

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

// newHandler assembles a [SummaryHandler] over an in-memory ethdb and the given
// state and hooks, without a VM. The caller populates the ethdb directly.
func newHandler(t *testing.T, ethDB ethdb.Database, st *state.State, hooks hook.Points, genesis *types.Block) *SummaryHandler {
	t.Helper()

	snowCtx := snowtest.Context(t, snowtest.CChainID)

	h, err := New(
		snowCtx,
		saestatesync.Config{CommitInterval: commitInterval},
		ethDB,
		nil, // network: TODO unused by these tests
		hooks,
		st,
		genesis,
	)
	require.NoError(t, err, "New()")
	t.Cleanup(func() {
		require.NoError(t, h.Shutdown(t.Context()), "Shutdown()")
	})
	return h
}

func TestGetStateSummary(t *testing.T) {
	const (
		settleLag    = 3
		lastExecuted = 2*commitInterval + 1 // 9
	)
	sut := newSUT(t, settleLag, lastExecuted)

	// Only committed heights can be served. Each settles a distinct, earlier
	// height, so an incorrect height selection would embed a different root.
	for _, blockHeight := range []uint64{0, commitInterval, 2 * commitInterval} {
		t.Run(fmt.Sprintf("height: %d", blockHeight), func(t *testing.T) {
			got, err := sut.GetStateSummary(t.Context(), blockHeight)
			require.NoErrorf(t, err, "GetStateSummary(%d)", blockHeight)
			require.Equalf(t, blockHeight, got.Height(), "GetStateSummary(%d).Height()", blockHeight)
			require.Equalf(t, sut.wantRoot(t, blockHeight), got.settledRoot, "GetStateSummary(%d).settledRoot", blockHeight)
		})
	}
}

func TestGetLastStateSummary(t *testing.T) {
	const (
		settleLag     = 3
		lastCommitted = 2 * commitInterval
		lastExecuted  = lastCommitted + 1
	)

	sut := newSUT(t, settleLag, lastExecuted)
	got, err := sut.GetLastStateSummary(t.Context())
	require.NoError(t, err, "GetLastStateSummary()")
	require.Equal(t, uint64(lastCommitted), got.Height(), "GetLastStateSummary().Height()")
	require.Equal(t, sut.wantRoot(t, lastCommitted), got.settledRoot, "GetLastStateSummary().settledRoot")
}

func TestUncommittedGenesis(t *testing.T) {
	genesis := newBlock(0)

	handler := newHandler(t, saetypes.NewEthDB(memdb.New()), newState(t), hookStub{lag: 5}, genesis)
	got, err := handler.GetLastStateSummary(t.Context())
	require.NoError(t, err, "GetLastStateSummary()")
	require.Equal(t, uint64(0), got.Height(), "GetLastStateSummary().Height()")
	require.Equal(t, types.EmptyRootHash, got.settledRoot, "GetLastStateSummary().settledRoot")
}
