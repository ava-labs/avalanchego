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

package core

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/upgrade/legacy"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
)

func verifyUnbrokenCanonchain(bc *BlockChain) error {
	h := bc.hc.CurrentHeader()
	for {
		canonHash := rawdb.ReadCanonicalHash(bc.hc.chainDb, h.Number.Uint64())
		if exp := h.Hash(); canonHash != exp {
			return fmt.Errorf("Canon hash chain broken, block %d got %x, expected %x",
				h.Number, canonHash[:8], exp[:8])
		}
		if h.Number.Uint64() == 0 {
			break
		}
		h = bc.hc.GetHeader(h.ParentHash, h.Number.Uint64()-1)
	}
	return nil
}

func testInsert(t *testing.T, bc *BlockChain, chain []*types.Block, wantErr error) {
	t.Helper()

	_, err := bc.InsertChain(chain)
	// Always verify that the header chain is unbroken
	if err := verifyUnbrokenCanonchain(bc); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("unexpected error from InsertHeaderChain: %v", err)
	}
}

// This test checks status reporting of InsertHeaderChain.
func TestHeaderInsertion(t *testing.T) {
	var (
		db    = rawdb.NewMemoryDatabase()
		gspec = &Genesis{
			BaseFee: big.NewInt(legacy.BaseFee),
			Config:  params.TestChainConfig,
		}
	)
	genesis := gspec.ToBlock()
	chain, err := NewBlockChain(db, DefaultCacheConfig, gspec, dummy.NewCoinbaseFaker(), vm.Config{}, common.Hash{}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Stop()

	// chain A: G->A1->A2...A128
	chainA, _, _ := GenerateChain(params.TestChainConfig, types.NewBlockWithHeader(genesis.Header()), dummy.NewCoinbaseFaker(), db, 128, 10, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{0: byte(10), 19: byte(i)})
	})
	// chain B: G->A1->B2...B128
	chainB, _, _ := GenerateChain(params.TestChainConfig, types.NewBlockWithHeader(chainA[0].Header()), dummy.NewCoinbaseFaker(), db, 128, 10, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{0: byte(10), 19: byte(i)})
	})

	// Inserting 64 headers on an empty chain
	testInsert(t, chain, chainA[:64], nil)

	// Inserting 64 identical headers
	testInsert(t, chain, chainA[:64], nil)

	// Inserting the same some old, some new headers
	testInsert(t, chain, chainA[32:96], nil)

	// Inserting side blocks, but not overtaking the canon chain
	testInsert(t, chain, chainB[0:32], nil)

	// Inserting more side blocks, but we don't have the parent
	testInsert(t, chain, chainB[34:36], consensus.ErrUnknownAncestor)

	// Inserting more sideblocks, overtaking the canon chain
	testInsert(t, chain, chainB[32:97], nil)

	// Inserting more A-headers, taking back the canonicality
	testInsert(t, chain, chainA[90:100], nil)

	// And B becomes canon again
	testInsert(t, chain, chainB[97:107], nil)

	// And B becomes even longer
	testInsert(t, chain, chainB[107:128], nil)
}
