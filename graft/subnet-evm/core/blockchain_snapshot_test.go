// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
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

// Tests that abnormal program termination (i.e.crash) and restart can recovery
// the snapshot properly if the snapshot is enabled.

package core

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/upgrade/legacy"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/ethdb"
)

// snapshotTestBasic wraps the common testing fields in the snapshot tests.
type snapshotTestBasic struct {
	scheme        string // Disk scheme used for storing trie nodes
	chainBlocks   int    // Number of blocks to generate for the canonical chain
	snapshotBlock uint64 // Block number of the relevant snapshot disk layer

	expCanonicalBlocks int    // Number of canonical blocks expected to remain in the database (excl. genesis)
	expHeadBlock       uint64 // Block number of the expected head full block
	expSnapshotBottom  uint64 // The block height corresponding to the snapshot disk layer

	// share fields, set in runtime
	datadir string
	ancient string
	db      ethdb.Database
	genDb   ethdb.Database
	engine  consensus.Engine
	gspec   *Genesis

	lastAcceptedHash common.Hash
}

func (basic *snapshotTestBasic) prepare(t *testing.T) (*BlockChain, []*types.Block) {
	// Create a temporary persistent database
	datadir := t.TempDir()
	ancient := path.Join(datadir, "ancient")

	db, err := rawdb.Open(rawdb.OpenOptions{
		Directory: datadir,
		Ephemeral: true,
	})
	if err != nil {
		t.Fatalf("Failed to create persistent database: %v", err)
	}
	// Initialize a fresh chain
	var (
		gspec = &Genesis{
			BaseFee: big.NewInt(legacy.BaseFee),
			Config:  params.TestChainConfig,
		}
		engine = dummy.NewFullFaker()
	)
	chain, err := NewBlockChain(db, DefaultCacheConfigWithScheme(basic.scheme), gspec, engine, vm.Config{}, common.Hash{}, false)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}
	genDb, blocks, _, _ := GenerateChainWithGenesis(gspec, engine, basic.chainBlocks, 10, func(i int, b *BlockGen) {})

	// genesis as last accepted
	basic.lastAcceptedHash = chain.GetBlockByNumber(0).Hash()

	// Insert the blocks with configured settings.
	var breakpoints []uint64
	breakpoints = append(breakpoints, basic.snapshotBlock)
	var startPoint uint64
	for _, point := range breakpoints {
		if _, err := chain.InsertChain(blocks[startPoint:point]); err != nil {
			t.Fatalf("Failed to import canonical chain start: %v", err)
		}
		startPoint = point

		if basic.snapshotBlock > 0 && basic.snapshotBlock == point {
			// Flushing from 0 to snapshotBlock into the disk
			for i := uint64(0); i < point; i++ {
				if err := chain.Accept(blocks[i]); err != nil {
					t.Fatalf("Failed to accept block %v: %v", i, err)
				}
				basic.lastAcceptedHash = blocks[i].Hash()
			}
			chain.DrainAcceptorQueue()

			diskRoot, blockRoot := chain.snaps.DiskRoot(), blocks[point-1].Root()
			if !bytes.Equal(diskRoot.Bytes(), blockRoot.Bytes()) {
				t.Fatalf("Failed to flush disk layer change, want %x, got %x", blockRoot, diskRoot)
			}
		}
	}
	if _, err := chain.InsertChain(blocks[startPoint:]); err != nil {
		t.Fatalf("Failed to import canonical chain tail: %v", err)
	}

	// Set runtime fields
	basic.datadir = datadir
	basic.ancient = ancient
	basic.db = db
	basic.genDb = genDb
	basic.engine = engine
	basic.gspec = gspec
	return chain, blocks
}

func (basic *snapshotTestBasic) verify(t *testing.T, chain *BlockChain, blocks []*types.Block) {
	// Iterate over all the remaining blocks and ensure there are no gaps
	verifyNoGaps(t, chain, true, blocks)
	verifyCutoff(t, chain, true, blocks, basic.expCanonicalBlocks)

	if head := chain.CurrentHeader(); head.Number.Uint64() != basic.expHeadBlock {
		t.Errorf("Head header mismatch: have %d, want %d", head.Number, basic.expHeadBlock)
	}
	if head := chain.CurrentBlock(); head.Number.Uint64() != basic.expHeadBlock {
		t.Errorf("Head block mismatch: have %d, want %d", head.Number, basic.expHeadBlock)
	}

	// Check the disk layer, ensure they are matched
	block := chain.GetBlockByNumber(basic.expSnapshotBottom)
	if block == nil {
		t.Errorf("The corresponding block[%d] of snapshot disk layer is missing", basic.expSnapshotBottom)
	} else if !bytes.Equal(chain.snaps.DiskRoot().Bytes(), block.Root().Bytes()) {
		t.Errorf("The snapshot disk layer root is incorrect, want %x, get %x", block.Root(), chain.snaps.DiskRoot())
	} else if len(chain.snaps.Snapshots(block.Hash(), -1, false)) != 1 {
		t.Errorf("The corresponding block[%d] of snapshot disk layer is missing", basic.expSnapshotBottom)
	}

	// Check the snapshot, ensure it's integrated
	if err := chain.snaps.Verify(block.Root()); err != nil {
		t.Errorf("The disk layer is not integrated %v", err)
	}
}

//nolint:unused
func (basic *snapshotTestBasic) dump() string {
	buffer := new(strings.Builder)

	fmt.Fprint(buffer, "Chain:\n  G")
	for i := 0; i < basic.chainBlocks; i++ {
		fmt.Fprintf(buffer, "->C%d", i+1)
	}
	fmt.Fprint(buffer, " (HEAD)\n\n")

	fmt.Fprintf(buffer, "Snapshot: G")
	if basic.snapshotBlock > 0 {
		fmt.Fprintf(buffer, ", C%d", basic.snapshotBlock)
	}
	fmt.Fprint(buffer, "\n")

	//if crash {
	//	fmt.Fprintf(buffer, "\nCRASH\n\n")
	//} else {
	//	fmt.Fprintf(buffer, "\nSetHead(%d)\n\n", basic.setHead)
	//}
	fmt.Fprintf(buffer, "------------------------------\n\n")

	fmt.Fprint(buffer, "Expected in leveldb:\n  G")
	for i := 0; i < basic.expCanonicalBlocks; i++ {
		fmt.Fprintf(buffer, "->C%d", i+1)
	}
	fmt.Fprintf(buffer, "\n\n")
	fmt.Fprintf(buffer, "Expected head header    : C%d\n", basic.expHeadBlock)
	if basic.expHeadBlock == 0 {
		fmt.Fprintf(buffer, "Expected head block     : G\n")
	} else {
		fmt.Fprintf(buffer, "Expected head block     : C%d\n", basic.expHeadBlock)
	}
	if basic.expSnapshotBottom == 0 {
		fmt.Fprintf(buffer, "Expected snapshot disk  : G\n")
	} else {
		fmt.Fprintf(buffer, "Expected snapshot disk  : C%d\n", basic.expSnapshotBottom)
	}
	return buffer.String()
}

func (basic *snapshotTestBasic) teardown() {
	basic.db.Close()
	basic.genDb.Close()
	os.RemoveAll(basic.datadir)
	os.RemoveAll(basic.ancient)
}

// snapshotTest is a test case type for normal snapshot recovery.
// It can be used for testing that restart Geth normally.
type snapshotTest struct {
	snapshotTestBasic
}

func (snaptest *snapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Restart the chain normally
	chain.Stop()
	newchain, err := NewBlockChain(snaptest.db, DefaultCacheConfigWithScheme(snaptest.scheme), snaptest.gspec, snaptest.engine, vm.Config{}, snaptest.lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newchain.Stop()

	snaptest.verify(t, newchain, blocks)
}

// crashSnapshotTest is a test case type for irregular snapshot recovery.
// It can be used for testing that restart Geth after the crash.
type crashSnapshotTest struct {
	snapshotTestBasic
}

func (snaptest *crashSnapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Pull the plug on the database, simulating a hard crash
	db := chain.db
	db.Close()
	chain.stopWithoutSaving()
	chain.triedb.Close()

	// Start a new blockchain back up and see where the repair leads us
	newdb, err := rawdb.Open(rawdb.OpenOptions{
		Directory: snaptest.datadir,
		Ephemeral: true,
	})
	if err != nil {
		t.Fatalf("Failed to reopen persistent database: %v", err)
	}
	defer newdb.Close()

	// The interesting thing is: instead of starting the blockchain after
	// the crash, we do restart twice here: one after the crash and one
	// after the normal stop. It's used to ensure the broken snapshot
	// can be detected all the time.
	newchain, err := NewBlockChain(newdb, DefaultCacheConfigWithScheme(snaptest.scheme), snaptest.gspec, snaptest.engine, vm.Config{}, snaptest.lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	newchain.Stop()

	newchain, err = NewBlockChain(newdb, DefaultCacheConfigWithScheme(snaptest.scheme), snaptest.gspec, snaptest.engine, vm.Config{}, snaptest.lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newchain.Stop()

	snaptest.verify(t, newchain, blocks)
}

// gappedSnapshotTest is a test type used to test this scenario:
// - have a complete snapshot
// - restart without enabling the snapshot
// - insert a few blocks
// - restart with enabling the snapshot again
type gappedSnapshotTest struct {
	snapshotTestBasic
	gapped int // Number of blocks to insert without enabling snapshot
}

func (snaptest *gappedSnapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Insert blocks without enabling snapshot if gapping is required.
	chain.Stop()
	gappedBlocks, _, _ := GenerateChain(snaptest.gspec.Config, blocks[len(blocks)-1], snaptest.engine, snaptest.genDb, snaptest.gapped, 10, func(i int, b *BlockGen) {})

	// Insert a few more blocks without enabling snapshot
	var cacheConfig = &CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		SnapshotLimit:  0,
		Pruning:        true,
		CommitInterval: 4096,
		StateHistory:   32,
		StateScheme:    snaptest.scheme,
	}
	newchain, err := NewBlockChain(snaptest.db, cacheConfig, snaptest.gspec, snaptest.engine, vm.Config{}, snaptest.lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	newchain.InsertChain(gappedBlocks)
	newchain.Stop()

	// Restart the chain with enabling the snapshot
	newchain, err = NewBlockChain(snaptest.db, DefaultCacheConfigWithScheme(snaptest.scheme), snaptest.gspec, snaptest.engine, vm.Config{}, snaptest.lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newchain.Stop()

	snaptest.verify(t, newchain, blocks)
}

// wipeCrashSnapshotTest is the test type used to test this scenario:
// - have a complete snapshot
// - restart, insert more blocks without enabling the snapshot
// - restart again with enabling the snapshot
// - crash
type wipeCrashSnapshotTest struct {
	snapshotTestBasic
	newBlocks int
}

func (snaptest *wipeCrashSnapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Firstly, stop the chain properly, with all snapshot journal
	// and state committed.
	chain.Stop()

	config := &CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		SnapshotLimit:  0,
		Pruning:        true,
		CommitInterval: 4096,
		StateHistory:   32,
		StateScheme:    snaptest.scheme,
	}
	newchain, err := NewBlockChain(snaptest.db, config, snaptest.gspec, snaptest.engine, vm.Config{}, snaptest.lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	newBlocks, _, _ := GenerateChain(snaptest.gspec.Config, blocks[len(blocks)-1], snaptest.engine, snaptest.genDb, snaptest.newBlocks, 10, func(i int, b *BlockGen) {})
	newchain.InsertChain(newBlocks)
	newchain.Stop()

	// Restart the chain, the wiper should start working
	config = &CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		SnapshotLimit:  256,
		Pruning:        true,
		CommitInterval: 4096,
		StateHistory:   32,
		SnapshotWait:   false, // Don't wait rebuild
		StateScheme:    snaptest.scheme,
	}
	tmp, err := NewBlockChain(snaptest.db, config, snaptest.gspec, snaptest.engine, vm.Config{}, snaptest.lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}

	// Simulate the blockchain crash.
	tmp.triedb.Close()
	tmp.stopWithoutSaving()

	newchain, err = NewBlockChain(snaptest.db, DefaultCacheConfigWithScheme(snaptest.scheme), snaptest.gspec, snaptest.engine, vm.Config{}, snaptest.lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	snaptest.verify(t, newchain, blocks)
	newchain.Stop()
}

// Tests a Geth restart with valid snapshot. Before the shutdown, all snapshot
// journal will be persisted correctly. In this case no snapshot recovery is
// required.
func TestRestartWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Snapshot: G
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head header    : C8
	// Expected head block     : C4
	// Expected snapshot disk  : C4
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		test := &snapshotTest{
			snapshotTestBasic{
				scheme:             scheme,
				chainBlocks:        8,
				snapshotBlock:      4,
				expCanonicalBlocks: 8,
				expHeadBlock:       4,
				expSnapshotBottom:  4, // Initial disk layer built from genesis
			},
		}
		test.test(t)
		test.teardown()
	}
}

// Tests a Geth was crashed and restarts with a broken snapshot. In this case the
// chain head should be rewound to the point with available state. And also the
// new head should must be lower than disk layer. But there is no committed point
// so the chain should be rewound to genesis and the disk layer should be left
// for recovery.
func TestNoCommitCrashWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Snapshot: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head block     : C4
	// Expected snapshot disk  : C4
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		test := &crashSnapshotTest{
			snapshotTestBasic{
				scheme:             scheme,
				chainBlocks:        8,
				snapshotBlock:      4,
				expCanonicalBlocks: 8,
				expHeadBlock:       4,
				expSnapshotBottom:  4, // Last committed disk layer, wait recovery
			},
		}
		test.test(t)
		test.teardown()
	}
}

// Tests a Geth was crashed and restarts with a broken snapshot. In this case the
// chain head should be rewound to the point with available state. And also the
// new head should must be lower than disk layer. But there is only a low committed
// point so the chain should be rewound to committed point and the disk layer
// should be left for recovery.
func TestLowCommitCrashWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Snapshot: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head block     : C4
	// Expected snapshot disk  : C4
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		test := &crashSnapshotTest{
			snapshotTestBasic{
				scheme:             scheme,
				chainBlocks:        8,
				snapshotBlock:      4,
				expCanonicalBlocks: 8,
				expHeadBlock:       4,
				expSnapshotBottom:  4, // Last committed disk layer, wait recovery
			},
		}
		test.test(t)
		test.teardown()
	}
}

// Tests a Geth was crashed and restarts with a broken snapshot. In this case
// the chain head should be rewound to the point with available state. And also
// the new head should must be lower than disk layer. But there is only a high
// committed point so the chain should be rewound to genesis and the disk layer
// should be left for recovery.
func TestHighCommitCrashWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Snapshot: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head block     : C4
	// Expected snapshot disk  : C4
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		test := &crashSnapshotTest{
			snapshotTestBasic{
				scheme:             scheme,
				chainBlocks:        8,
				snapshotBlock:      4,
				expCanonicalBlocks: 8,
				expHeadBlock:       4,
				expSnapshotBottom:  4, // Last committed disk layer, wait recovery
			},
		}
		test.test(t)
		test.teardown()
	}
}

// Tests a Geth was running with snapshot enabled. Then restarts without
// enabling snapshot and after that re-enable the snapshot again. In this
// case the snapshot should be rebuilt with latest chain head.
func TestGappedNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Snapshot: G
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Expected head block     : G
	// Expected snapshot disk  : G
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		test := &gappedSnapshotTest{
			snapshotTestBasic: snapshotTestBasic{
				scheme:             scheme,
				chainBlocks:        8,
				snapshotBlock:      0,
				expCanonicalBlocks: 10,
				expHeadBlock:       0,
				expSnapshotBottom:  0, // Rebuilt snapshot from the latest HEAD
			},
			gapped: 2,
		}
		test.test(t)
		test.teardown()
	}
}

// Tests the Geth was running with a complete snapshot and then imports a few
// more new blocks on top without enabling the snapshot. After the restart,
// crash happens. Check everything is ok after the restart.
func TestRecoverSnapshotFromWipingCrash(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Snapshot: G
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Expected head block     : C4
	// Expected snapshot disk  : C4
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme} {
		test := &wipeCrashSnapshotTest{
			snapshotTestBasic: snapshotTestBasic{
				scheme:             scheme,
				chainBlocks:        8,
				snapshotBlock:      4,
				expCanonicalBlocks: 10,
				expHeadBlock:       4,
				expSnapshotBottom:  4,
			},
			newBlocks: 2,
		}
		test.test(t)
		test.teardown()
	}
}
