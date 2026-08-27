// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// runSyncRoundTrip applies blocks to a source state, then leaf-syncs the
// resulting atomic trie into a fresh destination state over an in-memory p2p
// network and asserts the destination trie and shared memory match.
func runSyncRoundTrip(t *testing.T, blocks []block) {
	t.Helper()

	srcSUT := newSUT(t)
	srcSUT.apply(t, blocks...)
	src := srcSUT.stateImpl.(*State)

	target := src.currentRoot
	targetHeight := src.CurrentHeight()

	server := newSyncServer(t, src)

	dstSUT := newSUT(t)
	dst := dstSUT.stateImpl.(*State)
	require.NoError(t, server.syncInto(t.Context(), dst, target, targetHeight), "Sync()")

	require.Equal(t, targetHeight, dst.CurrentHeight(), "CurrentHeight()")
	gotRoot, err := dst.GetRoot(targetHeight)
	require.NoErrorf(t, err, "GetRoot(%d)", targetHeight)
	require.Equal(t, target, gotRoot, "GetRoot(%d)", targetHeight)
	require.Equal(t, dbEntries(t, srcSUT.sharedMemoryDB), dbEntries(t, dstSUT.sharedMemoryDB), "shared memory")
}

// syncServer serves a source state's atomic trie to an in-memory p2p peer.
type syncServer struct {
	net     *p2p.Network
	tracker *p2p.PeerTracker
}

func newSyncServer(t *testing.T, src *State) *syncServer {
	t.Helper()

	net, tracker := synctest.NewSelfNetwork(t, t.Context(), src.snowCtx.NodeID)
	require.NoError(t, RegisterSyncHandler(net, src), "RegisterSyncHandler()")
	return &syncServer{net: net, tracker: tracker}
}

// syncInto runs a syncer that pulls the served trie into dst.
func (s *syncServer) syncInto(ctx context.Context, dst *State, target common.Hash, targetHeight uint64) error {
	syncer := NewSyncer(s.net, s.tracker, dst, target, targetHeight)
	return syncer.Sync(ctx)
}

func TestSyncer_RoundTrip(t *testing.T) {
	var build builder
	tests := []struct {
		name   string
		blocks []block
	}{
		{
			name:   "single_import",
			blocks: []block{{height: 1, txs: []*tx.Tx{build.newImport()}}},
		},
		{
			name:   "single_export",
			blocks: []block{{height: 1, txs: []*tx.Tx{build.newExport()}}},
		},
		{
			name: "mixed",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newImport(), build.newExport()}},
				{height: 3, txs: nil},
				{height: 5, txs: []*tx.Tx{build.newExport(), build.newImport()}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSyncRoundTrip(t, tt.blocks)
		})
	}
}

func TestSyncer_BonusBlock(t *testing.T) {
	const (
		nonBonusHeight uint64 = 102971
		bonusHeight    uint64 = 102972
	)
	require.Containsf(t, bonusBlocks, bonusHeight, "bonusHeight=%d must be a known bonus block", bonusHeight)
	require.NotContainsf(t, bonusBlocks, nonBonusHeight, "nonBonusHeight=%d must not be a known bonus block", nonBonusHeight)

	var build builder
	blocks := []block{
		{height: nonBonusHeight, txs: []*tx.Tx{build.newExport()}},
		{height: bonusHeight, txs: []*tx.Tx{build.newExport()}},
	}

	tests := []struct {
		name       string
		networkID  uint32
		wantBlocks []block // expected in shared memory
	}{
		{
			name:       "mainnet_skips_bonus",
			networkID:  constants.MainnetID,
			wantBlocks: blocks[:1],
		},
		{
			name:       "non_mainnet_applies_bonus",
			networkID:  constants.FujiID,
			wantBlocks: blocks,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srcSUT := newSUT(t, withNetworkID(test.networkID))
			srcSUT.apply(t, blocks...)
			src := srcSUT.stateImpl.(*State)

			server := newSyncServer(t, src)

			dstSUT := newSUT(t, withNetworkID(test.networkID))
			dst := dstSUT.stateImpl.(*State)
			require.NoError(t, server.syncInto(t.Context(), dst, src.currentRoot, src.CurrentHeight()), "Sync()")

			// The trie always includes the bonus height's leaves.
			require.Equal(t, src.CurrentHeight(), dst.CurrentHeight(), "CurrentHeight()")
			gotRoot, err := dst.GetRoot(bonusHeight)
			require.NoErrorf(t, err, "GetRoot(%d)", bonusHeight)
			require.Equalf(t, src.currentRoot, gotRoot, "GetRoot(%d)", bonusHeight)

			// The synced shared memory matches a node that applied only the
			// non-skipped blocks.
			want := newSUT(t, withNetworkID(test.networkID))
			want.apply(t, test.wantBlocks...)
			require.Equal(t, dbEntries(t, want.sharedMemoryDB), dbEntries(t, dstSUT.sharedMemoryDB), "shared memory")
		})
	}
}

type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) next() byte {
	if r.pos >= len(r.data) {
		return 0
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

// blocksFromBytes decodes a fuzzer byte stream into blocks. The first byte
// picks the number of blocks; subsequent bytes pick, per block, the height gap,
// the number of txs, and whether each tx is an import (even) or export (odd).
func blocksFromBytes(data []byte, build *builder) []block {
	const (
		maxBlocks    = 8
		maxTxsPerBlk = 4
	)

	r := &byteReader{data: data}
	numBlocks := int(r.next()%maxBlocks) + 1
	blocks := make([]block, numBlocks)

	var height uint64
	for i := range numBlocks {
		height += uint64(r.next()) + 1 // strictly increasing, gaps allowed
		numTxs := int(r.next() % (maxTxsPerBlk + 1))

		txs := make([]*tx.Tx, numTxs)
		for j := range txs {
			if r.next()%2 == 0 {
				txs[j] = build.newImport()
			} else {
				txs[j] = build.newExport()
			}
		}
		blocks[i] = block{height: height, txs: txs}
	}

	return blocks
}

// FuzzSyncer fuzzes the number of blocks and the import/export layout within
// them, verifying each synced trie matches its source.
func FuzzSyncer(f *testing.F) {
	f.Add([]byte{0x00})                         // one block, one import
	f.Add([]byte{0x00, 0x01, 0x01, 0x00})       // one block, one export
	f.Add([]byte{0x02, 0x01, 0x02, 0x00, 0x01}) // two blocks, an import and an export

	f.Fuzz(func(t *testing.T, data []byte) {
		var build builder
		runSyncRoundTrip(t, blocksFromBytes(data, &build))
	})
}

func TestSyncer_Crash(t *testing.T) {
	var build builder
	blocks := []block{
		{height: 1, txs: []*tx.Tx{build.newImport()}},
		{height: 2, txs: []*tx.Tx{build.newExport(), build.newImport()}},
		{height: 4, txs: []*tx.Tx{build.newExport()}},
		{height: 5, txs: []*tx.Tx{build.newImport(), build.newExport()}},
	}

	srcSUT := newSUT(t)
	srcSUT.apply(t, blocks...)
	src := srcSUT.stateImpl.(*State)
	target := src.currentRoot
	targetHeight := src.CurrentHeight()

	server := newSyncServer(t, src)

	wantDB := saetest.NewFlakyDB(memdb.New(), math.MaxInt)
	require.NoError(t, server.syncInto(t.Context(), newSUT(t, withDB(wantDB)).stateImpl.(*State), target, targetHeight))

	for failAfter := range wantDB.Calls() {
		t.Run(fmt.Sprintf("failAfter_%d", failAfter), func(t *testing.T) {
			db := memdb.New()

			preCrash := newSUT(t, withDB(saetest.NewFlakyDB(db, failAfter)))
			err := server.syncInto(t.Context(), preCrash.stateImpl.(*State), target, targetHeight)
			require.ErrorIs(t, err, saetest.ErrInjected, "Sync()")

			// Clean re-run on the same DB must complete and match the source.
			got := newSUT(t, withDB(db))
			require.NoError(t, server.syncInto(t.Context(), got.stateImpl.(*State), target, targetHeight), "re-run Sync()")

			require.Equal(t, targetHeight, got.CurrentHeight(), "CurrentHeight()")
			require.Equal(t, dbEntries(t, srcSUT.sharedMemoryDB), dbEntries(t, got.sharedMemoryDB), "shared memory")
		})
	}
}
