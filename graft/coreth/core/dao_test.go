// (c) 2021-2022, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2016 The go-ethereum Authors
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

package core

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
)

// setDAOForkBlock makes a copy of [cfg] and assigns the DAO fork block to [forkBlock].
// This is necessary for testing since coreth restricts the DAO fork to be enabled at
// genesis only.
func setDAOForkBlock(cfg *params.ChainConfig, forkBlock *big.Int) *params.ChainConfig {
	config := *cfg
	config.DAOForkBlock = forkBlock
	return &config
}

// Tests that DAO-fork enabled clients can properly filter out fork-commencing
// blocks based on their extradata fields.
func TestDAOForkRangeExtradata(t *testing.T) {
	forkBlock := big.NewInt(32)
	chainConfig := *params.TestApricotPhase2Config
	chainConfig.DAOForkBlock = nil

	// Generate a common prefix for both pro-forkers and non-forkers
	gspec := &Genesis{
		BaseFee: big.NewInt(params.ApricotPhase3InitialBaseFee),
		Config:  &chainConfig,
	}
	genDb, prefix, _, _ := GenerateChainWithGenesis(gspec, dummy.NewFaker(), int(forkBlock.Int64()-1), 10, func(i int, gen *BlockGen) {})

	// Create the concurrent, conflicting two nodes
	proDb := rawdb.NewMemoryDatabase()
	proConf := *params.TestApricotPhase2Config
	proConf.DAOForkSupport = true

	progspec := &Genesis{
		BaseFee: big.NewInt(params.ApricotPhase3InitialBaseFee),
		Config:  &proConf,
	}
	proBc, _ := NewBlockChain(proDb, DefaultCacheConfig, progspec, dummy.NewFaker(), vm.Config{}, common.Hash{}, false)
	proBc.chainConfig = setDAOForkBlock(proBc.chainConfig, forkBlock)
	defer proBc.Stop()

	conDb := rawdb.NewMemoryDatabase()
	conConf := *params.TestApricotPhase2Config
	conConf.DAOForkSupport = false
	congspec := &Genesis{
		BaseFee: big.NewInt(params.ApricotPhase3InitialBaseFee),
		Config:  &conConf,
	}
	conBc, _ := NewBlockChain(conDb, DefaultCacheConfig, congspec, dummy.NewFaker(), vm.Config{}, common.Hash{}, false)
	conBc.chainConfig = setDAOForkBlock(conBc.chainConfig, forkBlock)
	defer conBc.Stop()

	if _, err := proBc.InsertChain(prefix); err != nil {
		t.Fatalf("pro-fork: failed to import chain prefix: %v", err)
	}
	if _, err := conBc.InsertChain(prefix); err != nil {
		t.Fatalf("con-fork: failed to import chain prefix: %v", err)
	}
	// Try to expand both pro-fork and non-fork chains iteratively with other camp's blocks
	for i := int64(0); i < params.DAOForkExtraRange.Int64(); i++ {
		// Create a pro-fork block, and try to feed into the no-fork chain
		bc, _ := NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, congspec, dummy.NewFaker(), vm.Config{}, common.Hash{}, false)
		bc.chainConfig = setDAOForkBlock(bc.chainConfig, forkBlock)
		defer bc.Stop()

		blocks := conBc.GetBlocksFromHash(conBc.CurrentBlock().Hash(), int(conBc.CurrentBlock().NumberU64()))
		for j := 0; j < len(blocks)/2; j++ {
			blocks[j], blocks[len(blocks)-1-j] = blocks[len(blocks)-1-j], blocks[j]
		}
		if _, err := bc.InsertChain(blocks); err != nil {
			t.Fatalf("failed to import contra-fork chain for expansion: %v", err)
		}
		if err := bc.stateCache.TrieDB().Commit(bc.CurrentHeader().Root, true, nil); err != nil {
			t.Fatalf("failed to commit contra-fork head for expansion: %v", err)
		}
		blocks, _, _ = GenerateChain(&proConf, conBc.CurrentBlock(), dummy.NewFaker(), genDb, 1, 10, func(i int, gen *BlockGen) {})
		if _, err := conBc.InsertChain(blocks); err != nil {
			t.Fatalf("contra-fork chain accepted pro-fork block: %v", blocks[0])
		}
		// Create a proper no-fork block for the contra-forker
		blocks, _, _ = GenerateChain(&conConf, conBc.CurrentBlock(), dummy.NewFaker(), genDb, 1, 10, func(i int, gen *BlockGen) {})
		if _, err := conBc.InsertChain(blocks); err != nil {
			t.Fatalf("contra-fork chain didn't accepted no-fork block: %v", err)
		}
		// Create a no-fork block, and try to feed into the pro-fork chain
		bc, _ = NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, progspec, dummy.NewFaker(), vm.Config{}, common.Hash{}, false)
		defer bc.Stop()

		blocks = proBc.GetBlocksFromHash(proBc.CurrentBlock().Hash(), int(proBc.CurrentBlock().NumberU64()))
		for j := 0; j < len(blocks)/2; j++ {
			blocks[j], blocks[len(blocks)-1-j] = blocks[len(blocks)-1-j], blocks[j]
		}
		if _, err := bc.InsertChain(blocks); err != nil {
			t.Fatalf("failed to import pro-fork chain for expansion: %v", err)
		}
		if err := bc.stateCache.TrieDB().Commit(bc.CurrentHeader().Root, true, nil); err != nil {
			t.Fatalf("failed to commit pro-fork head for expansion: %v", err)
		}
		blocks, _, _ = GenerateChain(&conConf, proBc.CurrentBlock(), dummy.NewFaker(), genDb, 1, 10, func(i int, gen *BlockGen) {})
		if _, err := proBc.InsertChain(blocks); err != nil {
			t.Fatalf("pro-fork chain accepted contra-fork block: %v", blocks[0])
		}
		// Create a proper pro-fork block for the pro-forker
		blocks, _, _ = GenerateChain(&proConf, proBc.CurrentBlock(), dummy.NewFaker(), genDb, 1, 10, func(i int, gen *BlockGen) {})
		if _, err := proBc.InsertChain(blocks); err != nil {
			t.Fatalf("pro-fork chain didn't accepted pro-fork block: %v", err)
		}
	}
	// Verify that contra-forkers accept pro-fork extra-datas after forking finishes
	bc, _ := NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, congspec, dummy.NewFaker(), vm.Config{}, common.Hash{}, false)
	defer bc.Stop()

	blocks := conBc.GetBlocksFromHash(conBc.CurrentBlock().Hash(), int(conBc.CurrentBlock().NumberU64()))
	for j := 0; j < len(blocks)/2; j++ {
		blocks[j], blocks[len(blocks)-1-j] = blocks[len(blocks)-1-j], blocks[j]
	}
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("failed to import contra-fork chain for expansion: %v", err)
	}
	if err := bc.stateCache.TrieDB().Commit(bc.CurrentHeader().Root, true, nil); err != nil {
		t.Fatalf("failed to commit contra-fork head for expansion: %v", err)
	}
	blocks, _, _ = GenerateChain(&proConf, conBc.CurrentBlock(), dummy.NewFaker(), genDb, 1, 10, func(i int, gen *BlockGen) {})
	if _, err := conBc.InsertChain(blocks); err != nil {
		t.Fatalf("contra-fork chain didn't accept pro-fork block post-fork: %v", err)
	}
	// Verify that pro-forkers accept contra-fork extra-datas after forking finishes
	bc, _ = NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, progspec, dummy.NewFaker(), vm.Config{}, common.Hash{}, false)
	defer bc.Stop()

	blocks = proBc.GetBlocksFromHash(proBc.CurrentBlock().Hash(), int(proBc.CurrentBlock().NumberU64()))
	for j := 0; j < len(blocks)/2; j++ {
		blocks[j], blocks[len(blocks)-1-j] = blocks[len(blocks)-1-j], blocks[j]
	}
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("failed to import pro-fork chain for expansion: %v", err)
	}
	if err := bc.stateCache.TrieDB().Commit(bc.CurrentHeader().Root, true, nil); err != nil {
		t.Fatalf("failed to commit pro-fork head for expansion: %v", err)
	}
	blocks, _, _ = GenerateChain(&conConf, proBc.CurrentBlock(), dummy.NewFaker(), genDb, 1, 10, func(i int, gen *BlockGen) {})
	if _, err := proBc.InsertChain(blocks); err != nil {
		t.Fatalf("pro-fork chain didn't accept contra-fork block post-fork: %v", err)
	}
}

func TestDAOForkSupportPostApricotPhase3(t *testing.T) {
	forkBlock := big.NewInt(0)

	conf := *params.TestChainConfig
	conf.DAOForkSupport = true
	conf.DAOForkBlock = forkBlock

	db := rawdb.NewMemoryDatabase()
	gspec := &Genesis{
		BaseFee: big.NewInt(params.ApricotPhase3InitialBaseFee),
		Config:  &conf,
	}
	bc, _ := NewBlockChain(db, DefaultCacheConfig, gspec, dummy.NewFaker(), vm.Config{}, common.Hash{}, false)
	defer bc.Stop()

	_, blocks, _, _ := GenerateChainWithGenesis(gspec, dummy.NewFaker(), 32, 10, func(i int, gen *BlockGen) {})

	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("failed to import blocks: %v", err)
	}
}
