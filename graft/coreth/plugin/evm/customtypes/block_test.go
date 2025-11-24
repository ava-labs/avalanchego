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

package customtypes_test

import (
	"bytes"
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/graft/coreth/internal/blocktest"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/rlp"

	// This test file has to be in package types_test to avoid a circular
	// dependency when importing `params`. We dot-import the package to mimic
	// regular same-package behaviour.
	. "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

// This test has been modified from https://github.com/ethereum/go-ethereum/blob/v1.9.21/core/types/block_test.go#L35 to fit
// the modified block format of Coreth
func TestBlockEncoding(t *testing.T) {
	blockEnc := common.FromHex("f90291f90217a04504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902a01dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347940100000000000000000000000000000000000000a00202e12a30c13562445052414c24dce5f1c530bb164e2a50897f0a6a1f78f158a0ecdf3b2c973d4156782b95816451fe9ed66b099cdca22f1168591ae2087765f4a0056b23fbba480696b65fe5a59b8f2148a1299103c4f57df839233af2cf4ca2d2b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000103837a12008252088460674e8a80a00000000000000000000000000000000000000000000000000000000000000000880000000000000000a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421f872f870808534630b8a00825208941954b772512974978793a809ecd8dce02dc71ba989014d1120d7b160000080830150f4a0beebf298ec38f9f4204f924686c4e5dd00f525fc1979ad224661ed2839ed55fda0267c480d1236c1684bdbad564a422e0a05007d7e8ca1acefe34e790b1d3a450ec08080")
	var block types.Block
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
	check("Bloom", block.Bloom(), types.BytesToBloom(common.FromHex("00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")))
	check("Difficulty", block.Difficulty(), big.NewInt(1))
	check("BlockNumber", block.NumberU64(), uint64(3))
	check("GasLimit", block.GasLimit(), uint64(8000000))
	check("GasUsed", block.GasUsed(), uint64(21000))
	check("Time", block.Time(), uint64(1617383050))
	check("Extra", block.Extra(), common.FromHex(""))
	check("MixDigest", block.MixDigest(), common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000"))
	check("Nonce", block.Nonce(), uint64(0))
	check("ExtDataHash", GetHeaderExtra(block.Header()).ExtDataHash, common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"))
	check("BaseFee", block.BaseFee(), (*big.Int)(nil))
	check("ExtDataGasUsed", BlockExtDataGasUsed(&block), (*big.Int)(nil))
	check("BlockGasCost", BlockGasCost(&block), (*big.Int)(nil))
	check("TimeMilliseconds", BlockTimeMilliseconds(&block), (*uint64)(nil))
	check("MinDelayExcess", BlockMinDelayExcess(&block), (*acp226.DelayExcess)(nil))

	check("Size", block.Size(), uint64(len(blockEnc)))
	check("BlockHash", block.Hash(), common.HexToHash("0608e5d5e13c337f226b621a0b08b3d50470f1961329826fd59f5a241d1df49e"))

	txHash := common.HexToHash("f5a60149da2ea4e97061a9f47c66036ee843fa76cd1f9ce5a71eb55ff90b2e0e")
	check("len(Transactions)", len(block.Transactions()), 1)
	check("Transactions[0].Hash", block.Transactions()[0].Hash(), txHash)
	ourBlockEnc, err := rlp.EncodeToBytes(&block)
	if err != nil {
		t.Fatal("encode error: ", err)
	}
	if !bytes.Equal(ourBlockEnc, blockEnc) {
		t.Errorf("encoded block mismatch:\ngot:  %x\nwant: %x", ourBlockEnc, blockEnc)
	}
}

func TestEIP1559BlockEncoding(t *testing.T) {
	blockEnc := common.FromHex("f9032ef9021fa04504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902a00000000000000000000000000000000000000000000000000000000000000000948888f1f195afa192cfee860698584c030f4c9db1a0ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000008302000003832fefd8825208845506eb0780a0bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff49888a13a5a8c8f2bb1c4a00000000000000000000000000000000000000000000000000000000000000000843b9aca00f90106f85f800a82c35094095e7baea6a6c7c4c2dfeb977efac326af552d870a801ba09bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094fa08a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b1b8a302f8a0018080843b9aca008301e24194095e7baea6a6c7c4c2dfeb977efac326af552d878080f838f7940000000000000000000000000000000000000001e1a0000000000000000000000000000000000000000000000000000000000000000080a0fe38ca4e44a30002ac54af7cf922a6ac2ba11b7d22f548e8ecb3f51f41cb31b0a06de6a5cbae13c0c856e33acf021b51819636cfc009d39eafb9f606d546e305a8c08080")
	var block types.Block
	if err := rlp.DecodeBytes(blockEnc, &block); err != nil {
		t.Fatal("decode error: ", err)
	}

	check := func(f string, got, want interface{}) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s mismatch: got %v, want %v", f, got, want)
		}
	}

	check("Difficulty", block.Difficulty(), big.NewInt(131072))
	check("GasLimit", block.GasLimit(), uint64(3141592))
	check("GasUsed", block.GasUsed(), uint64(21000))
	check("Coinbase", block.Coinbase(), common.HexToAddress("8888f1f195afa192cfee860698584c030f4c9db1"))
	check("MixDigest", block.MixDigest(), common.HexToHash("bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff498"))
	check("Root", block.Root(), common.HexToHash("ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017"))
	check("Hash", block.Hash(), common.HexToHash("2aefaa81ae43541bf2d608e2bb26a157212394abad4d219c06163be0d5d010f8"))
	check("Nonce", block.Nonce(), uint64(0xa13a5a8c8f2bb1c4))
	check("Time", block.Time(), uint64(1426516743))
	check("Size", block.Size(), uint64(len(blockEnc)))
	check("BaseFee", block.BaseFee(), new(big.Int).SetUint64(1_000_000_000))
	check("ExtDataGasUsed", BlockExtDataGasUsed(&block), (*big.Int)(nil))
	check("BlockGasCost", BlockGasCost(&block), (*big.Int)(nil))
	check("TimeMilliseconds", BlockTimeMilliseconds(&block), (*uint64)(nil))
	check("MinDelayExcess", BlockMinDelayExcess(&block), (*acp226.DelayExcess)(nil))

	tx1 := types.NewTransaction(0, common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"), big.NewInt(10), 50000, big.NewInt(10), nil)
	tx1, _ = tx1.WithSignature(types.HomesteadSigner{}, common.Hex2Bytes("9bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094f8a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b100"))

	addr := common.HexToAddress("0x0000000000000000000000000000000000000001")
	accesses := types.AccessList{types.AccessTuple{
		Address: addr,
		StorageKeys: []common.Hash{
			{0},
		},
	}}
	to := common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87")
	txdata := &types.DynamicFeeTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		To:         &to,
		Gas:        123457,
		GasFeeCap:  new(big.Int).Set(block.BaseFee()),
		GasTipCap:  big.NewInt(0),
		AccessList: accesses,
		Data:       []byte{},
	}
	tx2 := types.NewTx(txdata)
	tx2, err := tx2.WithSignature(types.LatestSignerForChainID(big.NewInt(1)), common.Hex2Bytes("fe38ca4e44a30002ac54af7cf922a6ac2ba11b7d22f548e8ecb3f51f41cb31b06de6a5cbae13c0c856e33acf021b51819636cfc009d39eafb9f606d546e305a800"))
	if err != nil {
		t.Fatal("invalid signature error: ", err)
	}

	check("len(Transactions)", len(block.Transactions()), 2)
	check("Transactions[0].Hash", block.Transactions()[0].Hash(), tx1.Hash())
	check("Transactions[1].Hash", block.Transactions()[1].Hash(), tx2.Hash())
	check("Transactions[1].Type", block.Transactions()[1].Type(), tx2.Type())
	ourBlockEnc, err := rlp.EncodeToBytes(&block)
	if err != nil {
		t.Fatal("encode error: ", err)
	}
	if !bytes.Equal(ourBlockEnc, blockEnc) {
		t.Errorf("encoded block mismatch:\ngot:  %x\nwant: %x", ourBlockEnc, blockEnc)
	}
}

func TestEIP2718BlockEncoding(t *testing.T) {
	blockEnc := common.FromHex("f9033cf90232a00000000000000000000000000000000000000000000000000000000000000000a01dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347948888f1f195afa192cfee860698584c030f4c9db1a0ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017a0e6e49996c7ec59f7a23d22b83239a60151512c65613bf84a0d7da336399ebc4aa0cafe75574d59780665a97fbfd11365c7545aa8f1abf4e5e12e8243334ef7286bb901000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000083020000820200832fefd882a410845506eb0796636f6f6c65737420626c6f636b206f6e20636861696ea0bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff49888a13a5a8c8f2bb1c4a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421f90101f85f800a82c35094095e7baea6a6c7c4c2dfeb977efac326af552d870a801ba09bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094fa08a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b1b89e01f89b01800a8301e24194095e7baea6a6c7c4c2dfeb977efac326af552d878080f838f7940000000000000000000000000000000000000001e1a0000000000000000000000000000000000000000000000000000000000000000001a03dbacc8d0259f2508625e97fdfc57cd85fdd16e5821bc2c10bdd1a52649e8335a0476e10695b183a87b0aa292a7f4b78ef0c3fbe62aa2c42c84e1d9c3da159ef14c08080")
	var block types.Block
	if err := rlp.DecodeBytes(blockEnc, &block); err != nil {
		t.Fatal("decode error: ", err)
	}

	check := func(f string, got, want interface{}) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s mismatch: got %v, want %v", f, got, want)
		}
	}
	check("Difficulty", block.Difficulty(), big.NewInt(131072))
	check("GasLimit", block.GasLimit(), uint64(3141592))
	check("GasUsed", block.GasUsed(), uint64(42000))
	check("Coinbase", block.Coinbase(), common.HexToAddress("8888f1f195afa192cfee860698584c030f4c9db1"))
	check("MixDigest", block.MixDigest(), common.HexToHash("bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff498"))
	check("Root", block.Root(), common.HexToHash("ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017"))
	check("Nonce", block.Nonce(), uint64(0xa13a5a8c8f2bb1c4))
	check("Time", block.Time(), uint64(1426516743))
	check("Size", block.Size(), uint64(len(blockEnc)))
	check("ExtDataHash", GetHeaderExtra(block.Header()).ExtDataHash, common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"))
	check("BaseFee", block.BaseFee(), (*big.Int)(nil))
	check("ExtDataGasUsed", BlockExtDataGasUsed(&block), (*big.Int)(nil))
	check("BlockGasCost", BlockGasCost(&block), (*big.Int)(nil))
	check("TimeMilliseconds", BlockTimeMilliseconds(&block), (*uint64)(nil))
	check("MinDelayExcess", BlockMinDelayExcess(&block), (*acp226.DelayExcess)(nil))

	// Create legacy tx.
	to := common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87")
	tx1 := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(10),
		Gas:      50000,
		GasPrice: big.NewInt(10),
	})
	sig := common.Hex2Bytes("9bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094f8a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b100")
	tx1, _ = tx1.WithSignature(types.HomesteadSigner{}, sig)

	// Create ACL tx.
	addr := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx2 := types.NewTx(&types.AccessListTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		To:         &to,
		Gas:        123457,
		GasPrice:   big.NewInt(10),
		AccessList: types.AccessList{{Address: addr, StorageKeys: []common.Hash{{0}}}},
	})
	sig2 := common.Hex2Bytes("3dbacc8d0259f2508625e97fdfc57cd85fdd16e5821bc2c10bdd1a52649e8335476e10695b183a87b0aa292a7f4b78ef0c3fbe62aa2c42c84e1d9c3da159ef1401")
	tx2, _ = tx2.WithSignature(types.NewEIP2930Signer(big.NewInt(1)), sig2)

	check("len(Transactions)", len(block.Transactions()), 2)
	check("Transactions[0].Hash", block.Transactions()[0].Hash(), tx1.Hash())
	check("Transactions[1].Hash", block.Transactions()[1].Hash(), tx2.Hash())
	check("Transactions[1].Type()", block.Transactions()[1].Type(), uint8(types.AccessListTxType))

	if !bytes.Equal(BlockExtData(&block), []byte{}) {
		t.Errorf("Block ExtraData field mismatch, expected empty byte array, but found 0x%x", BlockExtData(&block))
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
	var block types.Block
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
	check("Bloom", block.Bloom(), types.BytesToBloom(common.FromHex("00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")))
	check("Difficulty", block.Difficulty(), big.NewInt(1))
	check("BlockNumber", block.NumberU64(), uint64(2))
	check("GasLimit", block.GasLimit(), uint64(8000000))
	check("GasUsed", block.GasUsed(), uint64(0))
	check("Time", block.Time(), uint64(1617382963))
	check("Extra", block.Extra(), common.FromHex(""))
	check("MixDigest", block.MixDigest(), common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000"))
	check("Nonce", block.Nonce(), uint64(0))
	check("ExtDataHash", GetHeaderExtra(block.Header()).ExtDataHash, common.HexToHash("296ff3bfdebf7c4b1fb71f589d69ed03b1c59b278d1780d54dc86ea7cb87cf17"))
	check("BaseFee", block.BaseFee(), (*big.Int)(nil))
	check("ExtDataGasUsed", BlockExtDataGasUsed(&block), (*big.Int)(nil))
	check("BlockGasCost", BlockGasCost(&block), (*big.Int)(nil))
	check("TimeMilliseconds", BlockTimeMilliseconds(&block), (*uint64)(nil))
	check("MinDelayExcess", BlockMinDelayExcess(&block), (*acp226.DelayExcess)(nil))

	check("Size", block.Size(), uint64(len(blockEnc)))
	check("BlockHash", block.Hash(), common.HexToHash("4504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902"))

	check("len(Transactions)", len(block.Transactions()), 0)

	expectedBlockExtraData := common.FromHex("00000000000000003039c85fc1980a77c5da78fe5486233fc09a769bb812bcb2cc548cf9495d046b3f1bd891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf000000028a0f7c3e4d840143671a4c4ecacccb4d60fb97dce97a7aa5d60dfd072a7509cf00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500002d79883d20000000000100000000e0d5c4edc78f594b79025a56c44933c28e8ba3e51e6e23318727eeaac10eb27d00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500002d79883d20000000000100000000000000016dc8ea73dd39ab12fa2ecbc3427abaeb87d56fd800005af3107a4000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000200000009000000010d9f115cd63c3ab78b5b82cfbe4339cd6be87f21cda14cf192b269c7a6cb2d03666aa8f8b23ca0a2ceee4050e75c9b05525a17aa1dd0e9ea391a185ce395943f0000000009000000010d9f115cd63c3ab78b5b82cfbe4339cd6be87f21cda14cf192b269c7a6cb2d03666aa8f8b23ca0a2ceee4050e75c9b05525a17aa1dd0e9ea391a185ce395943f00")
	if !bytes.Equal(BlockExtData(&block), expectedBlockExtraData) {
		t.Errorf("Block ExtraData field mismatch, expected 0x%x, but found 0x%x", BlockExtData(&block), expectedBlockExtraData)
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
	uncles := make([]*types.Header, 0)
	h := types.CalcUncleHash(uncles)
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

func makeBenchBlock() *types.Block {
	var (
		key, _   = crypto.GenerateKey()
		txs      = make([]*types.Transaction, 70)
		receipts = make([]*types.Receipt, len(txs))
		signer   = types.LatestSigner(params.TestChainConfig)
		uncles   = make([]*types.Header, 3)
	)
	header := &types.Header{
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
		tx := types.NewTransaction(uint64(i), common.Address{}, amount, 123457, price, data)
		signedTx, err := types.SignTx(tx, signer, key)
		if err != nil {
			panic(err)
		}
		txs[i] = signedTx
		receipts[i] = types.NewReceipt(make([]byte, 32), false, tx.Gas())
	}
	for i := range uncles {
		uncles[i] = &types.Header{
			Difficulty: math.BigPow(11, 11),
			Number:     math.BigPow(2, 9),
			GasLimit:   12345678,
			GasUsed:    1476322,
			Time:       9876543,
			Extra:      []byte("benchmark uncle"),
		}
	}
	return types.NewBlock(header, txs, uncles, receipts, blocktest.NewHasher())
}

func TestAP4BlockEncoding(t *testing.T) {
	blockEnc := common.FromHex("f90335f90226a04504ee98a94d16dbd70a35370501a3cb00c2965b012672085fbd328a72962902a00000000000000000000000000000000000000000000000000000000000000000948888f1f195afa192cfee860698584c030f4c9db1a0ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000008302000003832fefd8825208845506eb0780a0bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff49888a13a5a8c8f2bb1c4a00000000000000000000000000000000000000000000000000000000000000000843b9aca008261a8830f4240f90106f85f800a82c35094095e7baea6a6c7c4c2dfeb977efac326af552d870a801ba09bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094fa08a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b1b8a302f8a0018080843b9aca008301e24194095e7baea6a6c7c4c2dfeb977efac326af552d878080f838f7940000000000000000000000000000000000000001e1a0000000000000000000000000000000000000000000000000000000000000000080a0fe38ca4e44a30002ac54af7cf922a6ac2ba11b7d22f548e8ecb3f51f41cb31b0a06de6a5cbae13c0c856e33acf021b51819636cfc009d39eafb9f606d546e305a8c08080")
	var block types.Block
	if err := rlp.DecodeBytes(blockEnc, &block); err != nil {
		t.Fatal("decode error: ", err)
	}

	check := func(f string, got, want interface{}) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s mismatch: got %v, want %v", f, got, want)
		}
	}

	check("Difficulty", block.Difficulty(), big.NewInt(131072))
	check("GasLimit", block.GasLimit(), uint64(3141592))
	check("GasUsed", block.GasUsed(), uint64(21000))
	check("Coinbase", block.Coinbase(), common.HexToAddress("8888f1f195afa192cfee860698584c030f4c9db1"))
	check("MixDigest", block.MixDigest(), common.HexToHash("bd4472abb6659ebe3ee06ee4d7b72a00a9f4d001caca51342001075469aff498"))
	check("Root", block.Root(), common.HexToHash("ef1552a40b7165c3cd773806b9e0c165b75356e0314bf0706f279c729f51e017"))
	// Hash of EIP-1559 block without ExtDataGasUsed and BlockGasCost is `0x2aefaa81ae43541bf2d608e2bb26a157212394abad4d219c06163be0d5d010f8`
	check("Hash", block.Hash(), common.HexToHash("0xc41340f5d2af79a12373bc8d6f0f05f9f98b240834608f428da171449e8a1468"))
	check("Nonce", block.Nonce(), uint64(0xa13a5a8c8f2bb1c4))
	check("Time", block.Time(), uint64(1426516743))
	check("Size", block.Size(), uint64(len(blockEnc)))
	check("BaseFee", block.BaseFee(), big.NewInt(1_000_000_000))
	check("ExtDataGasUsed", BlockExtDataGasUsed(&block), big.NewInt(25_000))
	check("BlockGasCost", BlockGasCost(&block), big.NewInt(1_000_000))
	check("TimeMilliseconds", BlockTimeMilliseconds(&block), (*uint64)(nil))
	check("MinDelayExcess", BlockMinDelayExcess(&block), (*acp226.DelayExcess)(nil))

	tx1 := types.NewTransaction(0, common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"), big.NewInt(10), 50000, big.NewInt(10), nil)
	tx1, _ = tx1.WithSignature(types.HomesteadSigner{}, common.Hex2Bytes("9bea4c4daac7c7c52e093e6a4c35dbbcf8856f1af7b059ba20253e70848d094f8a8fae537ce25ed8cb5af9adac3f141af69bd515bd2ba031522df09b97dd72b100"))

	addr := common.HexToAddress("0x0000000000000000000000000000000000000001")
	accesses := types.AccessList{types.AccessTuple{
		Address: addr,
		StorageKeys: []common.Hash{
			{0},
		},
	}}
	to := common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87")
	txdata := &types.DynamicFeeTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		To:         &to,
		Gas:        123457,
		GasFeeCap:  new(big.Int).Set(block.BaseFee()),
		GasTipCap:  big.NewInt(0),
		AccessList: accesses,
		Data:       []byte{},
	}
	tx2 := types.NewTx(txdata)
	tx2, err := tx2.WithSignature(types.LatestSignerForChainID(big.NewInt(1)), common.Hex2Bytes("fe38ca4e44a30002ac54af7cf922a6ac2ba11b7d22f548e8ecb3f51f41cb31b06de6a5cbae13c0c856e33acf021b51819636cfc009d39eafb9f606d546e305a800"))
	if err != nil {
		t.Fatal("invalid signature error: ", err)
	}

	check("len(Transactions)", len(block.Transactions()), 2)
	check("Transactions[0].Hash", block.Transactions()[0].Hash(), tx1.Hash())
	check("Transactions[1].Hash", block.Transactions()[1].Hash(), tx2.Hash())
	check("Transactions[1].Type", block.Transactions()[1].Type(), tx2.Type())
	ourBlockEnc, err := rlp.EncodeToBytes(&block)
	if err != nil {
		t.Fatal("encode error: ", err)
	}
	if !bytes.Equal(ourBlockEnc, blockEnc) {
		t.Errorf("encoded block mismatch:\ngot:  %x\nwant: %x", ourBlockEnc, blockEnc)
	}
}
