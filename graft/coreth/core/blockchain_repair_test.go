// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

// Tests that abnormal program termination (i.e.crash) and restart doesn't leave
// the database in some strange state with gaps in the chain, nor with block data
// dangling in the future.

package core

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
)

// rewindTest is a test case for chain rollback upon user request.
type rewindTest struct {
	canonicalBlocks int    // Number of blocks to generate for the canonical chain (heavier)
	sidechainBlocks int    // Number of blocks to generate for the side chain (lighter)
	commitBlock     uint64 // Block number for which to commit the state to disk

	expCanonicalBlocks int    // Number of canonical blocks expected to remain in the database (excl. genesis)
	expSidechainBlocks int    // Number of sidechain blocks expected to remain in the database (excl. genesis)
	expHeadBlock       uint64 // Block number of the expected head full block
}

// Tests a recovery for a short canonical chain where a recent block was already
// committed to disk and then the process crashed. In this case we expect the full
// chain to be rolled back to the committed block, but the chain data itself left
// in the database for replaying.
func TestShortRepair(t *testing.T)              { testShortRepair(t, false) }
func TestShortRepairWithSnapshots(t *testing.T) { testShortRepair(t, true) }

func testShortRepair(t *testing.T, snapshots bool) {
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    0,
		commitBlock:        4,
		expCanonicalBlocks: 8,
		expSidechainBlocks: 0,
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
func TestShortOldForkedRepair(t *testing.T) { testShortOldForkedRepair(t, false) }
func TestShortOldForkedRepairWithSnapshots(t *testing.T) {
	testShortOldForkedRepair(t, true)
}

func testShortOldForkedRepair(t *testing.T, snapshots bool) {
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    3,
		commitBlock:        4,
		expCanonicalBlocks: 8,
		expSidechainBlocks: 3,
		expHeadBlock:       0,
	}
	if snapshots {
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    6,
		commitBlock:        4,
		expCanonicalBlocks: 8,
		expSidechainBlocks: 6,
		expHeadBlock:       0,
	}
	if snapshots {
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    8,
		sidechainBlocks:    10,
		commitBlock:        4,
		expCanonicalBlocks: 8,
		expSidechainBlocks: 10,
		expHeadBlock:       0,
	}
	if snapshots {
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    0,
		commitBlock:        4,
		expCanonicalBlocks: 18,
		expSidechainBlocks: 0,
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    0,
		commitBlock:        4,
		expCanonicalBlocks: 24,
		expSidechainBlocks: 0,
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    3,
		commitBlock:        4,
		expCanonicalBlocks: 18,
		expSidechainBlocks: 3,
		expHeadBlock:       0,
	}
	if snapshots {
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    3,
		commitBlock:        4,
		expCanonicalBlocks: 24,
		expSidechainBlocks: 3,
		expHeadBlock:       0,
	}
	if snapshots {
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    12,
		commitBlock:        4,
		expCanonicalBlocks: 18,
		expSidechainBlocks: 12,
		expHeadBlock:       0,
	}
	if snapshots {
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    12,
		commitBlock:        4,
		expCanonicalBlocks: 24,
		expSidechainBlocks: 12,
		expHeadBlock:       0,
	}
	if snapshots {
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    18,
		sidechainBlocks:    26,
		commitBlock:        4,
		expCanonicalBlocks: 18,
		expSidechainBlocks: 26,
		expHeadBlock:       0,
	}
	if snapshots {
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
	t.Parallel()
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
	// Expected head block     : C4 (C0 with no snapshots)
	rt := &rewindTest{
		canonicalBlocks:    24,
		sidechainBlocks:    26,
		commitBlock:        4,
		expCanonicalBlocks: 24,
		expSidechainBlocks: 26,
		expHeadBlock:       0,
	}
	if snapshots {
		rt.expHeadBlock = 4
	}
	testRepair(t, rt, snapshots)
}

func testRepair(t *testing.T, tt *rewindTest, snapshots bool) {
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme, customrawdb.FirewoodScheme} {
		t.Run(scheme, func(t *testing.T) {
			testRepairWithScheme(t, tt, snapshots, scheme)
		})
	}
}

func testRepairWithScheme(t *testing.T, tt *rewindTest, snapshots bool, scheme string) {
	// It's hard to follow the test case, visualize the input
	//log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump(true))

	if scheme == customrawdb.FirewoodScheme && snapshots {
		t.Skip("Firewood scheme does not support snapshots")
	}

	// Create a temporary persistent database
	datadir := t.TempDir()

	db, err := rawdb.Open(rawdb.OpenOptions{
		Directory: datadir,
		Ephemeral: true,
	})
	if err != nil {
		t.Fatalf("Failed to create persistent database: %v", err)
	}
	defer db.Close() // Might double close, should be fine

	// Initialize a fresh chain
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		gspec   = &Genesis{
			BaseFee: big.NewInt(ap3.InitialBaseFee),
			Config:  params.TestChainConfig,
			Alloc:   types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
		}
		signer = types.LatestSigner(gspec.Config)
		engine = dummy.NewFullFaker()
		config = &CacheConfig{
			TrieCleanLimit:            256,
			TrieDirtyLimit:            256,
			TriePrefetcherParallelism: 4,
			SnapshotLimit:             0, // Disable snapshot by default
			StateHistory:              32,
			StateScheme:               scheme,
			ChainDataDir:              datadir,
		}
	)
	defer engine.Close()
	if snapshots {
		config.SnapshotLimit = 256
		config.SnapshotWait = true
	}
	chain, err := NewBlockChain(db, config, gspec, engine, vm.Config{}, common.Hash{}, false)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}
	defer chain.Stop()
	lastAcceptedHash := chain.GetBlockByNumber(0).Hash()

	// If sidechain blocks are needed, make a light chain and import it
	var sideblocks types.Blocks
	if tt.sidechainBlocks > 0 {
		genDb := rawdb.NewMemoryDatabase()
		gspec.MustCommit(genDb, triedb.NewDatabase(genDb, nil))
		sideblocks, _, err = GenerateChain(gspec.Config, gspec.ToBlock(), engine, genDb, tt.sidechainBlocks, 10, func(i int, b *BlockGen) {
			b.SetCoinbase(common.Address{0x01})
			tx, err := types.SignTx(types.NewTransaction(b.TxNonce(addr1), common.Address{0x01}, big.NewInt(10000), ethparams.TxGas, big.NewInt(ap3.InitialBaseFee), nil), signer, key1)
			require.NoError(err)
			b.AddTx(tx)
		})
		require.NoError(err)
		if _, err := chain.InsertChain(sideblocks); err != nil {
			t.Fatalf("Failed to import side chain: %v", err)
		}
	}
	genDb := rawdb.NewMemoryDatabase()
	gspec.MustCommit(genDb, triedb.NewDatabase(genDb, nil))
	canonblocks, _, err := GenerateChain(gspec.Config, gspec.ToBlock(), engine, genDb, tt.canonicalBlocks, 10, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{0x02})
		b.SetDifficulty(big.NewInt(1000000))
		tx, err := types.SignTx(types.NewTransaction(b.TxNonce(addr1), common.Address{0x02}, big.NewInt(10000), ethparams.TxGas, big.NewInt(ap3.InitialBaseFee), nil), signer, key1)
		require.NoError(err)
		b.AddTx(tx)
	})
	require.NoError(err)
	if _, err := chain.InsertChain(canonblocks[:tt.commitBlock]); err != nil {
		t.Fatalf("Failed to import canonical chain start: %v", err)
	}
	if tt.commitBlock > 0 {
		if snapshots {
			for i := uint64(0); i < tt.commitBlock; i++ {
				if err := chain.Accept(canonblocks[i]); err != nil {
					t.Fatalf("Failed to accept block %v: %v", i, err)
				}
				lastAcceptedHash = canonblocks[i].Hash()
			}
			chain.DrainAcceptorQueue()
		}
	}
	if _, err := chain.InsertChain(canonblocks[tt.commitBlock:]); err != nil {
		t.Fatalf("Failed to import canonical chain tail: %v", err)
	}

	// Pull the plug on the database, simulating a hard crash
	chain.triedb.Close()
	db.Close()
	chain.stopWithoutSaving()

	// Start a new blockchain back up and see where the repair leads us
	db, err = rawdb.Open(rawdb.OpenOptions{
		Directory: datadir,
		Ephemeral: true,
	})
	if err != nil {
		t.Fatalf("Failed to reopen persistent database: %v", err)
	}
	defer db.Close()

	newChain, err := NewBlockChain(db, config, gspec, engine, vm.Config{}, lastAcceptedHash, false)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newChain.Stop()

	// Iterate over all the remaining blocks and ensure there are no gaps
	verifyNoGaps(t, newChain, true, canonblocks)
	verifyNoGaps(t, newChain, false, sideblocks)
	verifyCutoff(t, newChain, true, canonblocks, tt.expCanonicalBlocks)
	verifyCutoff(t, newChain, false, sideblocks, tt.expSidechainBlocks)

	if head := newChain.CurrentHeader(); head.Number.Uint64() != tt.expHeadBlock {
		t.Errorf("Head header mismatch: have %d, want %d", head.Number, tt.expHeadBlock)
	}
	if head := newChain.CurrentBlock(); head.Number.Uint64() != tt.expHeadBlock {
		t.Errorf("Head block mismatch: have %d, want %d", head.Number, tt.expHeadBlock)
	}
}
