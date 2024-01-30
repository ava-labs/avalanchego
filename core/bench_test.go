// (c) 2019-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
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
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/core/vm"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
)

func BenchmarkInsertChain_empty_memdb(b *testing.B) {
	benchInsertChain(b, false, nil)
}
func BenchmarkInsertChain_empty_diskdb(b *testing.B) {
	benchInsertChain(b, true, nil)
}
func BenchmarkInsertChain_valueTx_memdb(b *testing.B) {
	benchInsertChain(b, false, genValueTx(0))
}
func BenchmarkInsertChain_valueTx_diskdb(b *testing.B) {
	benchInsertChain(b, true, genValueTx(0))
}
func BenchmarkInsertChain_valueTx_100kB_memdb(b *testing.B) {
	benchInsertChain(b, false, genValueTx(100*1024))
}
func BenchmarkInsertChain_valueTx_100kB_diskdb(b *testing.B) {
	benchInsertChain(b, true, genValueTx(100*1024))
}

func BenchmarkInsertChain_ring200_memdb(b *testing.B) {
	benchInsertChain(b, false, genTxRing(200))
}
func BenchmarkInsertChain_ring200_diskdb(b *testing.B) {
	benchInsertChain(b, true, genTxRing(200))
}
func BenchmarkInsertChain_ring1000_memdb(b *testing.B) {
	benchInsertChain(b, false, genTxRing(1000))
}
func BenchmarkInsertChain_ring1000_diskdb(b *testing.B) {
	benchInsertChain(b, true, genTxRing(1000))
}

var (
	// This is the content of the genesis block used by the benchmarks.
	benchRootKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	benchRootAddr   = crypto.PubkeyToAddress(benchRootKey.PublicKey)
	benchRootFunds  = math.BigPow(2, 100)
)

// genValueTx returns a block generator that includes a single
// value-transfer transaction with n bytes of extra data in each
// block.
func genValueTx(nbytes int) func(int, *BlockGen) {
	return func(i int, gen *BlockGen) {
		toaddr := common.Address{}
		data := make([]byte, nbytes)
		gas, _ := IntrinsicGas(data, nil, false, params.Rules{}) // Disable Istanbul and EIP-2028 for this test
		signer := types.MakeSigner(gen.config, big.NewInt(int64(i)), gen.header.Time)
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(benchRootAddr), toaddr, big.NewInt(1), gas, big.NewInt(225000000000), data), signer, benchRootKey)
		gen.AddTx(tx)
	}
}

var (
	ringKeys  = make([]*ecdsa.PrivateKey, 1000)
	ringAddrs = make([]common.Address, len(ringKeys))
)

func init() {
	ringKeys[0] = benchRootKey
	ringAddrs[0] = benchRootAddr
	for i := 1; i < len(ringKeys); i++ {
		ringKeys[i], _ = crypto.GenerateKey()
		ringAddrs[i] = crypto.PubkeyToAddress(ringKeys[i].PublicKey)
	}
}

// genTxRing returns a block generator that sends ether in a ring
// among n accounts. This is creates n entries in the state database
// and fills the blocks with many small transactions.
func genTxRing(naccounts int) func(int, *BlockGen) {
	from := 0
	fee := big.NewInt(0).SetUint64(params.TxGas * 225000000000)
	amount := big.NewInt(0).Set(benchRootFunds)
	return func(i int, gen *BlockGen) {
		block := gen.PrevBlock(i - 1)
		gas := block.GasLimit()
		signer := types.MakeSigner(gen.config, big.NewInt(int64(i)), gen.header.Time)
		for {
			gas -= params.TxGas
			if gas < params.TxGas {
				break
			}
			to := (from + 1) % naccounts
			tx := types.NewTransaction(
				gen.TxNonce(ringAddrs[from]),
				ringAddrs[to],
				amount.Sub(amount, fee),
				params.TxGas,
				big.NewInt(225000000000),
				nil,
			)
			tx, _ = types.SignTx(tx, signer, ringKeys[from])
			gen.AddTx(tx)
			from = to
		}
	}
}

func benchInsertChain(b *testing.B, disk bool, gen func(int, *BlockGen)) {
	// Create the database in memory or in a temporary directory.
	var db ethdb.Database
	var err error
	if !disk {
		db = rawdb.NewMemoryDatabase()
	} else {
		dir := b.TempDir()
		db, err = rawdb.NewLevelDBDatabase(dir, 128, 128, "", false)
		if err != nil {
			b.Fatalf("cannot create temporary database: %v", err)
		}
		defer db.Close()
	}

	// Generate a chain of b.N blocks using the supplied block
	// generator function.
	gspec := &Genesis{
		Config: params.TestChainConfig,
		Alloc:  GenesisAlloc{benchRootAddr: {Balance: benchRootFunds}},
	}
	_, chain, _, _ := GenerateChainWithGenesis(gspec, dummy.NewCoinbaseFaker(), b.N, 10, gen)

	// Time the insertion of the new chain.
	// State and blocks are stored in the same DB.
	chainman, _ := NewBlockChain(db, DefaultCacheConfig, gspec, dummy.NewCoinbaseFaker(), vm.Config{}, common.Hash{}, false)
	defer chainman.Stop()
	b.ReportAllocs()
	b.ResetTimer()
	if i, err := chainman.InsertChain(chain); err != nil {
		b.Fatalf("insert error (block %d): %v\n", i, err)
	}
}

func BenchmarkChainRead_header_10k(b *testing.B) {
	benchReadChain(b, false, 10000)
}
func BenchmarkChainRead_full_10k(b *testing.B) {
	benchReadChain(b, true, 10000)
}
func BenchmarkChainRead_header_100k(b *testing.B) {
	benchReadChain(b, false, 100000)
}
func BenchmarkChainRead_full_100k(b *testing.B) {
	benchReadChain(b, true, 100000)
}
func BenchmarkChainRead_header_500k(b *testing.B) {
	benchReadChain(b, false, 500000)
}
func BenchmarkChainRead_full_500k(b *testing.B) {
	benchReadChain(b, true, 500000)
}
func BenchmarkChainWrite_header_10k(b *testing.B) {
	benchWriteChain(b, false, 10000)
}
func BenchmarkChainWrite_full_10k(b *testing.B) {
	benchWriteChain(b, true, 10000)
}
func BenchmarkChainWrite_header_100k(b *testing.B) {
	benchWriteChain(b, false, 100000)
}
func BenchmarkChainWrite_full_100k(b *testing.B) {
	benchWriteChain(b, true, 100000)
}
func BenchmarkChainWrite_header_500k(b *testing.B) {
	benchWriteChain(b, false, 500000)
}
func BenchmarkChainWrite_full_500k(b *testing.B) {
	benchWriteChain(b, true, 500000)
}

// makeChainForBench writes a given number of headers or empty blocks/receipts
// into a database.
func makeChainForBench(db ethdb.Database, full bool, count uint64) {
	var hash common.Hash
	for n := uint64(0); n < count; n++ {
		header := &types.Header{
			Coinbase:    common.Address{},
			Number:      big.NewInt(int64(n)),
			ParentHash:  hash,
			Difficulty:  big.NewInt(1),
			UncleHash:   types.EmptyUncleHash,
			TxHash:      types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash,
		}
		hash = header.Hash()

		rawdb.WriteHeader(db, header)
		rawdb.WriteCanonicalHash(db, hash, n)

		if n == 0 {
			rawdb.WriteChainConfig(db, hash, params.TestChainConfig)
		}
		rawdb.WriteHeadHeaderHash(db, hash)

		if full || n == 0 {
			block := types.NewBlockWithHeader(header)
			rawdb.WriteBody(db, hash, n, block.Body())
			rawdb.WriteReceipts(db, hash, n, nil)
		}
	}
}

func benchWriteChain(b *testing.B, full bool, count uint64) {
	for i := 0; i < b.N; i++ {
		dir := b.TempDir()
		db, err := rawdb.NewLevelDBDatabase(dir, 128, 1024, "", false)
		if err != nil {
			b.Fatalf("error opening database at %v: %v", dir, err)
		}
		makeChainForBench(db, full, count)
		db.Close()
	}
}

func benchReadChain(b *testing.B, full bool, count uint64) {
	dir := b.TempDir()

	db, err := rawdb.NewLevelDBDatabase(dir, 128, 1024, "", false)
	if err != nil {
		b.Fatalf("error opening database at %v: %v", dir, err)
	}
	makeChainForBench(db, full, count)
	db.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		db, err := rawdb.NewLevelDBDatabase(dir, 128, 1024, "", false)
		if err != nil {
			b.Fatalf("error opening database at %v: %v", dir, err)
		}
		chain, err := NewBlockChain(db, DefaultCacheConfig, nil, dummy.NewFaker(), vm.Config{}, common.Hash{}, false)
		if err != nil {
			b.Fatalf("error creating chain: %v", err)
		}

		for n := uint64(0); n < count; n++ {
			header := chain.GetHeaderByNumber(n)
			if full {
				hash := header.Hash()
				rawdb.ReadBody(db, hash, n)
				rawdb.ReadReceipts(db, hash, n, header.Time, chain.Config())
			}
		}
		chain.Stop()
		db.Close()
	}
}
