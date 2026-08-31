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

func checkStatesMatch(t *testing.T, srcSUT, dstSUT *SUT, blocks ...block) {
	t.Helper()

	var (
		src = srcSUT.stateImpl.(*State)
		dst = dstSUT.stateImpl.(*State)
	)

	require.Equal(t, src.CurrentHeight(), dst.CurrentHeight(), "CurrentHeight()")
	require.Equal(t, dbEntries(t, srcSUT.sharedMemoryDB), dbEntries(t, dstSUT.sharedMemoryDB), "shared memory")
	require.Equal(t, src.currentRoot, dst.currentRoot, "current merkle root")
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

			checkStatesMatch(t, srcSUT, dstSUT)
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

// blocksFromBytes decodes a fuzzer byte stream into blocks.
func blocksFromBytes(data []byte) []block {
	const (
		maxBlocks    = 8
		maxTxsPerBlk = 4
	)

	var (
		build  builder
		height uint64
	)
	r := &byteReader{data: data}
	numBlocks := int(r.next()%maxBlocks) + 1
	blocks := make([]block, numBlocks)

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
	f.Add([]byte{0x00, 0x00, 0x01, 0x00})                   // one block, one import
	f.Add([]byte{0x00, 0x00, 0x01, 0x01})                   // one block, one export
	f.Add([]byte{0x01, 0x00, 0x01, 0x00, 0x00, 0x01, 0x01}) // two blocks, an import then an export

	f.Fuzz(func(t *testing.T, data []byte) {
		srcSUT := newSUT(t)
		srcSUT.apply(t, blocksFromBytes(data)...)
		src := srcSUT.stateImpl.(*State)

		target := src.currentRoot
		targetHeight := src.CurrentHeight()

		server := newSyncServer(t, src)

		dstSUT := newSUT(t)
		dst := dstSUT.stateImpl.(*State)
		require.NoError(t, server.syncInto(t.Context(), dst, target, targetHeight), "Sync()")

		checkStatesMatch(t, srcSUT, dstSUT)
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

			checkStatesMatch(t, srcSUT, got)
		})
	}
}
