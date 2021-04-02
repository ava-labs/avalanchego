// (c) 2020-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
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

package types

import (
	"bytes"
	"hash"
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

// This test has been modified from https://github.com/ethereum/go-ethereum/blob/v1.9.21/core/types/block_test.go#L35 to fit
// the modified block format of Coreth
func TestBlockEncoding(t *testing.T) {
	blockEnc := common.FromHex("f90291f90217a04504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902a01dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347940100000000000000000000000000000000000000a00202e12a30c13562445052414c24dce5f1c530bb164e2a50897f0a6a1f78f158a0ecdf3b2c973d4156782b95816451fe9ed66b099cdca22f1168591ae2087765f4a0056b23fbba480696b65fe5a59b8f2148a1299103c4f57df839233af2cf4ca2d2b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000103837a12008252088460674e8a80a00000000000000000000000000000000000000000000000000000000000000000880000000000000000a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421f872f870808534630b8a00825208941954b772512974978793a809ecd8dce02dc71ba989014d1120d7b160000080830150f4a0beebf298ec38f9f4204f924686c4e5dd00f525fc1979ad224661ed2839ed55fda0267c480d1236c1684bdbad564a422e0a05007d7e8ca1acefe34e790b1d3a450ec08080")
	var block Block
	if err := rlp.DecodeBytes(blockEnc, &block); err != nil {
		t.Fatal("decode error: ", err)
	}

	check := func(f string, got, want interface{}) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s mismatch: got %v, want %v", f, got, want)
		}
	}
	check("ParentHash", block.ParentHash(), common.HexToHash("4504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902"))
	check("UncleHash", block.UncleHash(), common.HexToHash("1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"))
	check("Coinbase", block.Coinbase(), common.HexToAddress("0100000000000000000000000000000000000000"))
	check("Root", block.Root(), common.HexToHash("0202e12a30c13562445052414c24dce5f1c530bb164e2a50897f0a6a1f78f158"))
	check("TxHash", block.TxHash(), common.HexToHash("ecdf3b2c973d4156782b95816451fe9ed66b099cdca22f1168591ae2087765f4"))
	check("ReceiptHash", block.ReceiptHash(), common.HexToHash("056b23fbba480696b65fe5a59b8f2148a1299103c4f57df839233af2cf4ca2d2"))
	check("Bloom", block.Bloom(), BytesToBloom(common.FromHex("00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")))
	check("Difficulty", block.Difficulty(), big.NewInt(1))
	check("BlockNumber", block.NumberU64(), uint64(3))
	check("GasLimit", block.GasLimit(), uint64(8000000))
	check("GasUsed", block.GasUsed(), uint64(21000))
	check("Time", block.Time(), uint64(1617383050))
	check("Extra", block.Extra(), common.FromHex(""))
	check("MixDigest", block.MixDigest(), common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000"))
	check("Nonce", block.Nonce(), uint64(0))
	check("ExtDataHash", block.header.ExtDataHash, common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"))

	check("Size", block.Size(), common.StorageSize(len(blockEnc)))
	check("BlockHash", block.Hash(), common.HexToHash("0608e5d5e13c337f226b621a0b08b3d50470f1961329826fd59f5a241d1df49e"))

	txHash := common.HexToHash("f5a60149da2ea4e97061a9f47c66036ee843fa76cd1f9ce5a71eb55ff90b2e0e")
	check("len(Transactions)", len(block.Transactions()), 1)
	check("Transactions[0].Hash", block.Transactions()[0].Hash(), txHash)

	if !bytes.Equal(block.ExtData(), []byte{}) {
		t.Errorf("Block ExtraData field mismatch, expected empty byte array, but found 0x%x", block.ExtData())
	}

	ourBlockEnc, err := rlp.EncodeToBytes(&block)
	if err != nil {
		t.Fatal("encode error: ", err)
	}
	if !bytes.Equal(ourBlockEnc, blockEnc) {
		t.Errorf("encoded block mismatch:\ngot:  %x\nwant: %x", ourBlockEnc, blockEnc)
	}
}

func TestBlockEncodingWithExtraData(t *testing.T) {
	blockEnc := common.FromHex("f903f2f90215a02a0d1d68d26eb213cf1c6c1e6abbaf374f0ee9a5428558df334c36d380c6a080a01dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347940100000000000000000000000000000000000000a0c0caa90fe3722cb2e288f7998d54a855a6d40f67e0e77a695d0d65dad22c6290a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000102837a1200808460674e3380a00000000000000000000000000000000000000000000000000000000000000000880000000000000000a0296ff3bfdebf7c4b1fb71f589d69ed03b1c59b278d1780d54dc86ea7cb87cf17c0c080b901d400000000000000003039c85fc1980a77c5da78fe5486233fc09a769bb812bcb2cc548cf9495d046b3f1bd891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf000000028a0f7c3e4d840143671a4c4ecacccb4d60fb97dce97a7aa5d60dfd072a7509cf00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500002d79883d20000000000100000000e0d5c4edc78f594b79025a56c44933c28e8ba3e51e6e23318727eeaac10eb27d00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500002d79883d20000000000100000000000000016dc8ea73dd39ab12fa2ecbc3427abaeb87d56fd800005af3107a4000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000200000009000000010d9f115cd63c3ab78b5b82cfbe4339cd6be87f21cda14cf192b269c7a6cb2d03666aa8f8b23ca0a2ceee4050e75c9b05525a17aa1dd0e9ea391a185ce395943f0000000009000000010d9f115cd63c3ab78b5b82cfbe4339cd6be87f21cda14cf192b269c7a6cb2d03666aa8f8b23ca0a2ceee4050e75c9b05525a17aa1dd0e9ea391a185ce395943f00")
	var block Block
	if err := rlp.DecodeBytes(blockEnc, &block); err != nil {
		t.Fatal("decode error: ", err)
	}

	check := func(f string, got, want interface{}) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s mismatch: got %v, want %v", f, got, want)
		}
	}
	check("ParentHash", block.ParentHash(), common.HexToHash("2a0d1d68d26eb213cf1c6c1e6abbaf374f0ee9a5428558df334c36d380c6a080"))
	check("UncleHash", block.UncleHash(), common.HexToHash("1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"))
	check("Coinbase", block.Coinbase(), common.HexToAddress("0100000000000000000000000000000000000000"))
	check("Root", block.Root(), common.HexToHash("c0caa90fe3722cb2e288f7998d54a855a6d40f67e0e77a695d0d65dad22c6290"))
	check("TxHash", block.TxHash(), common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"))
	check("ReceiptHash", block.ReceiptHash(), common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"))
	check("Bloom", block.Bloom(), BytesToBloom(common.FromHex("00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")))
	check("Difficulty", block.Difficulty(), big.NewInt(1))
	check("BlockNumber", block.NumberU64(), uint64(2))
	check("GasLimit", block.GasLimit(), uint64(8000000))
	check("GasUsed", block.GasUsed(), uint64(0))
	check("Time", block.Time(), uint64(1617382963))
	check("Extra", block.Extra(), common.FromHex(""))
	check("MixDigest", block.MixDigest(), common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000"))
	check("Nonce", block.Nonce(), uint64(0))
	check("ExtDataHash", block.header.ExtDataHash, common.HexToHash("296ff3bfdebf7c4b1fb71f589d69ed03b1c59b278d1780d54dc86ea7cb87cf17"))

	check("Size", block.Size(), common.StorageSize(len(blockEnc)))
	check("BlockHash", block.Hash(), common.HexToHash("4504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902"))

	check("len(Transactions)", len(block.Transactions()), 0)

	expectedBlockExtraData := common.FromHex("00000000000000003039c85fc1980a77c5da78fe5486233fc09a769bb812bcb2cc548cf9495d046b3f1bd891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf000000028a0f7c3e4d840143671a4c4ecacccb4d60fb97dce97a7aa5d60dfd072a7509cf00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500002d79883d20000000000100000000e0d5c4edc78f594b79025a56c44933c28e8ba3e51e6e23318727eeaac10eb27d00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500002d79883d20000000000100000000000000016dc8ea73dd39ab12fa2ecbc3427abaeb87d56fd800005af3107a4000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000200000009000000010d9f115cd63c3ab78b5b82cfbe4339cd6be87f21cda14cf192b269c7a6cb2d03666aa8f8b23ca0a2ceee4050e75c9b05525a17aa1dd0e9ea391a185ce395943f0000000009000000010d9f115cd63c3ab78b5b82cfbe4339cd6be87f21cda14cf192b269c7a6cb2d03666aa8f8b23ca0a2ceee4050e75c9b05525a17aa1dd0e9ea391a185ce395943f00")
	if !bytes.Equal(block.ExtData(), expectedBlockExtraData) {
		t.Errorf("Block ExtraData field mismatch, expected 0x%x, but found 0x%x", block.ExtData(), expectedBlockExtraData)
	}

	ourBlockEnc, err := rlp.EncodeToBytes(&block)
	if err != nil {
		t.Fatal("encode error: ", err)
	}
	if !bytes.Equal(ourBlockEnc, blockEnc) {
		t.Errorf("encoded block mismatch:\ngot:  %x\nwant: %x", ourBlockEnc, blockEnc)
	}
}

func TestUncleHash(t *testing.T) {
	uncles := make([]*Header, 0)
	h := CalcUncleHash(uncles)
	exp := common.HexToHash("1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347")
	if h != exp {
		t.Fatalf("empty uncle hash is wrong, got %x != %x", h, exp)
	}
}

var benchBuffer = bytes.NewBuffer(make([]byte, 0, 32000))

func BenchmarkEncodeBlock(b *testing.B) {
	block := makeBenchBlock()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchBuffer.Reset()
		if err := rlp.Encode(benchBuffer, block); err != nil {
			b.Fatal(err)
		}
	}
}

// testHasher is the helper tool for transaction/receipt list hashing.
// The original hasher is trie, in order to get rid of import cycle,
// use the testing hasher instead.
type testHasher struct {
	hasher hash.Hash
}

func newHasher() *testHasher {
	return &testHasher{hasher: sha3.NewLegacyKeccak256()}
}

func (h *testHasher) Reset() {
	h.hasher.Reset()
}

func (h *testHasher) Update(key, val []byte) {
	h.hasher.Write(key)
	h.hasher.Write(val)
}

func (h *testHasher) Hash() common.Hash {
	return common.BytesToHash(h.hasher.Sum(nil))
}

func makeBenchBlock() *Block {
	var (
		key, _   = crypto.GenerateKey()
		txs      = make([]*Transaction, 70)
		receipts = make([]*Receipt, len(txs))
		signer   = NewEIP155Signer(params.TestChainConfig.ChainID)
		uncles   = make([]*Header, 3)
	)
	header := &Header{
		Difficulty: math.BigPow(11, 11),
		Number:     math.BigPow(2, 9),
		GasLimit:   12345678,
		GasUsed:    1476322,
		Time:       9876543,
		Extra:      []byte("coolest block on chain"),
	}
	for i := range txs {
		amount := math.BigPow(2, int64(i))
		price := big.NewInt(300000)
		data := make([]byte, 100)
		tx := NewTransaction(uint64(i), common.Address{}, amount, 123457, price, data)
		signedTx, err := SignTx(tx, signer, key)
		if err != nil {
			panic(err)
		}
		txs[i] = signedTx
		receipts[i] = NewReceipt(make([]byte, 32), false, tx.Gas())
	}
	for i := range uncles {
		uncles[i] = &Header{
			Difficulty: math.BigPow(11, 11),
			Number:     math.BigPow(2, 9),
			GasLimit:   12345678,
			GasUsed:    1476322,
			Time:       9876543,
			Extra:      []byte("benchmark uncle"),
		}
	}
	return NewBlock(header, txs, uncles, receipts, newHasher(), nil, true)
}
