// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Tests that abnormal program termination (i.e.crash) and restart doesn't leave
// the database in some strange state with gaps in the chain, nor with block data
// dangling in the future.

package core

import (
	"io/ioutil"
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
)

// Tests a recovery for a short canonical chain where a recent block was already
// committed to disk and then the process crashed. In this case we expect the full
// chain to be rolled back to the committed block, but the chain data itself left
// in the database for replaying.
func TestShortRepair(t *testing.T)              { testShortRepair(t, false) }
func TestShortRepairWithSnapshots(t *testing.T) { testShortRepair(t, true) }

func testShortRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head header    : C8
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    0,
		commitBlock:        4,
		expCanonicalBlocks: 8,
		expSidechainBlocks: 0,
		expHeadHeader:      8,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a short canonical chain and a shorter side chain, where a
// recent block was already committed to disk and then the process crashed. In this
// test scenario the side chain is below the committed block. In this case we expect
// the canonical chain to be rolled back to the committed block, but the chain data
// itself left in the database for replaying.
func TestShortOldForkedRepair(t *testing.T)              { testShortOldForkedRepair(t, false) }
func TestShortOldForkedRepairWithSnapshots(t *testing.T) { testShortOldForkedRepair(t, true) }

func testShortOldForkedRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//   └->S1->S2->S3
	//
	// Expected head header    : C8 (C3 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    3,
		commitBlock:        4,
		expCanonicalBlocks: 8,
		expSidechainBlocks: 3,
		expHeadHeader:      3,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 8
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a short canonical chain and a shorter side chain, where a
// recent block was already committed to disk and then the process crashed. In this
// test scenario the side chain reaches above the committed block. In this case we
// expect the canonical chain to be rolled back to the committed block, but the
// chain data itself left in the database for replaying.
func TestShortNewlyForkedRepair(t *testing.T)              { testShortNewlyForkedRepair(t, false) }
func TestShortNewlyForkedRepairWithSnapshots(t *testing.T) { testShortNewlyForkedRepair(t, true) }

func testShortNewlyForkedRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//   └->S1->S2->S3->S4->S5->S6
	//
	// Expected head header    : C8 (C6 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    6,
		commitBlock:        4,
		expCanonicalBlocks: 8,
		expSidechainBlocks: 6,
		expHeadHeader:      6,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 8
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a short canonical chain and a longer side chain, where a
// recent block was already committed to disk and then the process crashed. In this
// case we expect the canonical chain to be rolled back to the committed block, but
// the chain data itself left in the database for replaying.
func TestShortReorgedRepair(t *testing.T)              { testShortReorgedRepair(t, false) }
func TestShortReorgedRepairWithSnapshots(t *testing.T) { testShortReorgedRepair(t, true) }

func testShortReorgedRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10
	//
	// Expected head header    : C8 (C10 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    10,
		commitBlock:        4,
		expCanonicalBlocks: 8,
		expSidechainBlocks: 10,
		expHeadHeader:      10,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 8
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a long canonical chain where a recent block was already
// committed to disk and then the process crashed. In this case we expect the chain
// to be rolled back to the committed block, but the chain data itself left in the
// database for replaying.
func TestLongShallowRepair(t *testing.T)              { testLongShallowRepair(t, false) }
func TestLongShallowRepairWithSnapshots(t *testing.T) { testLongShallowRepair(t, true) }

func testLongShallowRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18
	//
	// Expected head header    : C18
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    0,
		commitBlock:        4,
		expCanonicalBlocks: 18,
		expSidechainBlocks: 0,
		expHeadHeader:      18,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a long canonical chain where a recent block was already committed
// to disk and then the process crashed. In this case we expect the chain to be rolled
// back to the committed block, but the chain data itself left in the database for replaying.
func TestLongDeepRepair(t *testing.T)              { testLongDeepRepair(t, false) }
func TestLongDeepRepairWithSnapshots(t *testing.T) { testLongDeepRepair(t, true) }

func testLongDeepRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb: none
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24
	//
	// Expected head header    : C24
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    0,
		commitBlock:        4,
		expCanonicalBlocks: 24,
		expSidechainBlocks: 0,
		expHeadHeader:      24,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a long canonical chain with a shorter side chain, where a recent
// block was already committed to disk and then the process crashed. In this test scenario
// the side chain is below the committed block. In this case we expect the chain to be
// rolled back to the committed block, but the chain data itself left in the database
// for replaying.
func TestLongOldForkedShallowRepair(t *testing.T) {
	testLongOldForkedShallowRepair(t, false)
}
func TestLongOldForkedShallowRepairWithSnapshots(t *testing.T) {
	testLongOldForkedShallowRepair(t, true)
}

func testLongOldForkedShallowRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18
	//   └->S1->S2->S3
	//
	// Expected head header    : C18 (C3 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    3,
		commitBlock:        4,
		expCanonicalBlocks: 18,
		expSidechainBlocks: 3,
		expHeadHeader:      3,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 18
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a long canonical chain a shorter side chain, where a recent block
// was already committed to disk and then the process crashed. In this test scenario the side
// chain is below the committed block. In this case we expect the canonical chain to be
// rolled back to the committed block, but the chain data itself left in the database for replaying.
func TestLongOldForkedDeepRepair(t *testing.T)              { testLongOldForkedDeepRepair(t, false) }
func TestLongOldForkedDeepRepairWithSnapshots(t *testing.T) { testLongOldForkedDeepRepair(t, true) }

func testLongOldForkedDeepRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24
	//   └->S1->S2->S3
	//
	// Expected head header    : C24 (C3 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    3,
		commitBlock:        4,
		expCanonicalBlocks: 24,
		expSidechainBlocks: 3,
		expHeadHeader:      3,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 24
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a long canonical chain with a shorter side chain, where a recent
// block was already committed to disk and then the process crashed. In this test scenario
// the side chain is above the committed block. In this case we expect the chain to be
// rolled back to the committed block, but the chain data itself left in the database for replaying.
func TestLongNewerForkedShallowRepair(t *testing.T) {
	testLongNewerForkedShallowRepair(t, false)
}
func TestLongNewerForkedShallowRepairWithSnapshots(t *testing.T) {
	testLongNewerForkedShallowRepair(t, true)
}

func testLongNewerForkedShallowRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Expected head header    : C18 (C12 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    12,
		commitBlock:        4,
		expCanonicalBlocks: 18,
		expSidechainBlocks: 12,
		expHeadHeader:      12,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 18
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a long canonical chain with a shorter side chain, where a recent block
// was already committed to disk and then the process crashed. In this test scenario the side
// chain is above the committed block. In this case we expect the canonical chain to be rolled
// back to the committed block, but the chain data itself left in the database for replaying.
func TestLongNewerForkedDeepRepair(t *testing.T)              { testLongNewerForkedDeepRepair(t, false) }
func TestLongNewerForkedDeepRepairWithSnapshots(t *testing.T) { testLongNewerForkedDeepRepair(t, true) }

func testLongNewerForkedDeepRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12
	//
	// Expected head header    : C24 (C12 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    12,
		commitBlock:        4,
		expCanonicalBlocks: 24,
		expSidechainBlocks: 12,
		expHeadHeader:      12,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 24
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a long canonical chain with a longer side chain, where a recent block
// was already committed to disk and then the process crashed. In this case we expect the chain to be
// rolled back to the committed block, but the chain data itself left in the database for replaying.
func TestLongReorgedShallowRepair(t *testing.T)              { testLongReorgedShallowRepair(t, false) }
func TestLongReorgedShallowRepairWithSnapshots(t *testing.T) { testLongReorgedShallowRepair(t, true) }

func testLongReorgedShallowRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Expected head header    : C18 (C26 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    26,
		commitBlock:        4,
		expCanonicalBlocks: 18,
		expSidechainBlocks: 26,
		expHeadHeader:      26,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 18
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

// Tests a recovery for a long canonical chain with a longer side chain, where a recent block
// was already committed to disk and then the process crashed. In this case we expect the canonical
// chains to be rolled back to the committed block, but the chain data itself left in the database
// for replaying.
func TestLongReorgedDeepRepair(t *testing.T)              { testLongReorgedDeepRepair(t, false) }
func TestLongReorgedDeepRepairWithSnapshots(t *testing.T) { testLongReorgedDeepRepair(t, true) }

func testLongReorgedDeepRepair(t *testing.T, snapshots bool) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24 (HEAD)
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Commit: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10->C11->C12->C13->C14->C15->C16->C17->C18->C19->C20->C21->C22->C23->C24
	//   └->S1->S2->S3->S4->S5->S6->S7->S8->S9->S10->S11->S12->S13->S14->S15->S16->S17->S18->S19->S20->S21->S22->S23->S24->S25->S26
	//
	// Expected head header    : C24 (C26 with no snapshots)
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    26,
		commitBlock:        4,
		expCanonicalBlocks: 24,
		expSidechainBlocks: 26,
		expHeadHeader:      26,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadHeader = 24
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

func testRepair(t *testing.T, tt *rewindTest, snapshots bool) {
	// It's hard to follow the test case, visualize the input
	//log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump(true))

	// Create a temporary persistent database
	datadir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("Failed to create temporary datadir: %v", err)
	}
	os.RemoveAll(datadir)

	db, err := NewLevelDBDatabase(datadir, 0, 0, "", false)
	if err != nil {
		t.Fatalf("Failed to create persistent database: %v", err)
	}
	defer db.Close() // Might double close, should be fine

	// Initialize a fresh chain
	var (
		genesis = (&Genesis{Config: params.TestChainConfig, BaseFee: big.NewInt(params.ApricotPhase3InitialBaseFee)}).MustCommit(db)
		engine  = dummy.NewFullFaker()
		config  = &CacheConfig{
			TrieCleanLimit: 256,
			TrieDirtyLimit: 256,
			SnapshotLimit:  0, // Disable snapshot by default
		}
	)
	if snapshots {
		config.SnapshotLimit = 256
	}
	chain, err := NewBlockChain(db, config, params.TestChainConfig, engine, vm.Config{}, common.Hash{})
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}
	lastAcceptedHash := chain.GetBlockByNumber(0).Hash()

	// If sidechain blocks are needed, make a light chain and import it
	var sideblocks types.Blocks
	if tt.sidechainBlocks > 0 {
		sideblocks, _, _ = GenerateChain(params.TestChainConfig, genesis, engine, rawdb.NewMemoryDatabase(), tt.sidechainBlocks, 10, func(i int, b *BlockGen) {
			b.SetCoinbase(common.Address{0x01})
		})
		if _, err := chain.InsertChain(sideblocks); err != nil {
			t.Fatalf("Failed to import side chain: %v", err)
		}
	}
	canonblocks, _, _ := GenerateChain(params.TestChainConfig, genesis, engine, rawdb.NewMemoryDatabase(), tt.canonicalBlocks, 10, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{0x02})
		b.SetDifficulty(big.NewInt(1000000))
	})
	if _, err := chain.InsertChain(canonblocks[:tt.commitBlock]); err != nil {
		t.Fatalf("Failed to import canonical chain start: %v", err)
	}
	if tt.commitBlock > 0 {
		chain.stateCache.TrieDB().Commit(canonblocks[tt.commitBlock-1].Root(), true, nil)
		if snapshots {
			for i := uint64(0); i < tt.commitBlock; i++ {
				if err := chain.Accept(canonblocks[i]); err != nil {
					t.Fatalf("Failed to accept block %v: %v", i, err)
				}
				lastAcceptedHash = canonblocks[i].Hash()
			}
		}
	}
	if _, err := chain.InsertChain(canonblocks[tt.commitBlock:]); err != nil {
		t.Fatalf("Failed to import canonical chain tail: %v", err)
	}

	// Pull the plug on the database, simulating a hard crash
	db.Close()

	// Start a new blockchain back up and see where the repait leads us
	db, err = NewLevelDBDatabase(datadir, 0, 0, "", false)
	if err != nil {
		t.Fatalf("Failed to reopen persistent database: %v", err)
	}
	defer db.Close()

	chain, err = NewBlockChain(db, DefaultCacheConfig, params.TestChainConfig, engine, vm.Config{}, lastAcceptedHash)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer chain.Stop()

	// Iterate over all the remaining blocks and ensure there are no gaps
	verifyNoGaps(t, chain, true, canonblocks)
	verifyNoGaps(t, chain, false, sideblocks)
	verifyCutoff(t, chain, true, canonblocks, tt.expCanonicalBlocks)
	verifyCutoff(t, chain, false, sideblocks, tt.expSidechainBlocks)

	if head := chain.CurrentHeader(); head.Number.Uint64() != tt.expHeadHeader {
		t.Errorf("Head header mismatch: have %d, want %d", head.Number, tt.expHeadHeader)
	}
	if head := chain.CurrentBlock(); head.NumberU64() != tt.expHeadBlock {
		t.Errorf("Head block mismatch: have %d, want %d", head.NumberU64(), tt.expHeadBlock)
	}
}
