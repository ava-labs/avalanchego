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
// Copyright 2017 The go-ethereum Authors
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
	_ "embed"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/params"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
)

//go:embed "genesis_test_data.json"
var genesisTestAllocJsonData []byte

type genesisTestAllocData struct {
	FakeMainnetAllocData []byte `json:"fakeMainnetAllocData"`
	FakeRopstenAllocData []byte `json:"fakeRopstenAllocData"`
	FakeRinkebyAllocData []byte `json:"fakeRinkebyAllocData"`
	FakeGoerliAllocData  []byte `json:"fakeGoerliAllocData"`
}

func setupGenesisBlock(db ethdb.Database, genesis *Genesis) (*params.ChainConfig, common.Hash, error) {
	conf, err := SetupGenesisBlock(db, genesis)
	stored := rawdb.ReadCanonicalHash(db, 0)
	return conf, stored, err
}

func TestGenesisBlockForTesting(t *testing.T) {
	genesisBlockForTestingHash := common.HexToHash("0xb378f22ccd9ad52c6c42f5d46ef2aad6d6866cfcb778ea97a0b6dfde13387330")
	block := GenesisBlockForTesting(rawdb.NewMemoryDatabase(), common.Address{1}, big.NewInt(1))
	if block.Hash() != genesisBlockForTestingHash {
		t.Errorf("wrong testing genesis hash, got %v, want %v", block.Hash(), genesisBlockForTestingHash)
	}
}

func TestToBlock(t *testing.T) {
	block := fakeMainnetGenesisBlock().ToBlock(nil)
	if block.Hash() != fakeMainnetGenesisHash {
		t.Errorf("wrong mainnet genesis hash, got %v, want %v", block.Hash(), fakeMainnetGenesisHash)
	}
	block = fakeRopstenGenesisBlock().ToBlock(nil)
	if block.Hash() != fakeRopstenGenesisHash {
		t.Errorf("wrong ropsten genesis hash, got %v, want %v", block.Hash(), fakeRopstenGenesisHash)
	}
	block = fakeRinkebyGenesisBlock().ToBlock(nil)
	if block.Hash() != fakeRinkebyGenesisHash {
		t.Errorf("wrong ropsten genesis hash, got %v, want %v", block.Hash(), fakeRinkebyGenesisHash)
	}
	block = fakeGoerliGenesisBlock().ToBlock(nil)
	if block.Hash() != fakeGoerliGenesisHash {
		t.Errorf("wrong ropsten genesis hash, got %v, want %v", block.Hash(), fakeGoerliGenesisHash)
	}
}

func TestSetupGenesis(t *testing.T) {
	var (
		customghash = common.HexToHash("0x1099a11e9e454bd3ef31d688cf21936671966407bc330f051d754b5ce401e7ed")
		customg     = Genesis{
			Config: &params.ChainConfig{
				HomesteadBlock:              big.NewInt(0),
				ApricotPhase1BlockTimestamp: big.NewInt(time.Date(2021, time.July, 31, 14, 0, 0, 0, time.UTC).Unix()),
			},
			Alloc: GenesisAlloc{
				{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			},
		}
		oldcustomg = customg
	)
	oldcustomg.Config = &params.ChainConfig{
		HomesteadBlock:              big.NewInt(0),
		ApricotPhase1BlockTimestamp: big.NewInt(time.Date(2021, time.March, 31, 14, 0, 0, 0, time.UTC).Unix()),
	}
	tests := []struct {
		name       string
		fn         func(ethdb.Database) (*params.ChainConfig, common.Hash, error)
		wantConfig *params.ChainConfig
		wantHash   common.Hash
		wantErr    error
	}{
		{
			name: "genesis without ChainConfig",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				return setupGenesisBlock(db, new(Genesis))
			},
			wantErr:    errGenesisNoConfig,
			wantConfig: nil,
		},
		{
			name: "no block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				return setupGenesisBlock(db, nil)
			},
			wantErr:    ErrNoGenesis,
			wantConfig: nil,
		},
		{
			name: "mainnet block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				fakeMainnetGenesisBlock().MustCommit(db)
				return setupGenesisBlock(db, nil)
			},
			wantErr:    ErrNoGenesis,
			wantHash:   fakeMainnetGenesisHash,
			wantConfig: nil,
		},
		{
			name: "custom block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				customg.MustCommit(db)
				return setupGenesisBlock(db, nil)
			},
			wantErr:    ErrNoGenesis,
			wantHash:   customghash,
			wantConfig: nil,
		},
		{
			name: "custom block in DB, genesis == ropsten",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				customg.MustCommit(db)
				return setupGenesisBlock(db, fakeRopstenGenesisBlock())
			},
			wantErr:    &GenesisMismatchError{Stored: customghash, New: fakeRopstenGenesisHash},
			wantHash:   customghash,
			wantConfig: params.TestChainConfig,
		},
		{
			name: "compatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				oldcustomg.MustCommit(db)
				return setupGenesisBlock(db, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "incompatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				genesis := oldcustomg.MustCommit(db)
				bc, _ := NewBlockChain(db, DefaultCacheConfig, oldcustomg.Config, dummy.NewFullFaker(), vm.Config{}, common.Hash{})
				defer bc.Stop()
				blocks, _, _ := GenerateChain(oldcustomg.Config, genesis, dummy.NewFaker(), db, 4, 10, nil)
				bc.InsertChain(blocks)
				bc.CurrentBlock()
				return setupGenesisBlock(db, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
			wantErr: &params.ConfigCompatError{
				What:         "ApricotPhase1 fork block",
				StoredConfig: big.NewInt(1617199200),
				NewConfig:    big.NewInt(1627740000),
				RewindTo:     1617199199,
			},
		},
	}

	for _, test := range tests {
		db := rawdb.NewMemoryDatabase()
		config, hash, err := test.fn(db)
		// Check the return values.
		if !reflect.DeepEqual(err, test.wantErr) {
			spew := spew.ConfigState{DisablePointerAddresses: true, DisableCapacities: true}
			t.Errorf("%s: returned error %#v, want %#v", test.name, spew.NewFormatter(err), spew.NewFormatter(test.wantErr))
		}
		if !reflect.DeepEqual(config, test.wantConfig) {
			t.Errorf("%s:\nreturned %v\nwant     %v", test.name, config, test.wantConfig)
		}
		if hash != test.wantHash {
			t.Errorf("%s: returned hash %s, want %s", test.name, hash.Hex(), test.wantHash.Hex())
		} else if err == nil {
			// Check database content.
			stored := rawdb.ReadBlock(db, test.wantHash, 0)
			if stored.Hash() != test.wantHash {
				t.Errorf("%s: block in DB has hash %s, want %s", test.name, stored.Hash(), test.wantHash)
			}
		}
	}
}

// TestGenesisHashes checks the congruity of default genesis data to corresponding hardcoded genesis hash values.
func TestGenesisHashes(t *testing.T) {
	cases := []struct {
		genesis *Genesis
		hash    common.Hash
	}{
		{
			genesis: fakeMainnetGenesisBlock(),
			hash:    fakeMainnetGenesisHash,
		},
		{
			genesis: fakeGoerliGenesisBlock(),
			hash:    fakeGoerliGenesisHash,
		},
		{
			genesis: fakeRopstenGenesisBlock(),
			hash:    fakeRopstenGenesisHash,
		},
		{
			genesis: fakeRinkebyGenesisBlock(),
			hash:    fakeRinkebyGenesisHash,
		},
	}
	for i, c := range cases {
		b := c.genesis.MustCommit(rawdb.NewMemoryDatabase())
		if got := b.Hash(); got != c.hash {
			t.Errorf("case: %d, want: %s, got: %s", i, c.hash.Hex(), got.Hex())
		}
	}
}

func decodePrealloc(data string) GenesisAlloc {
	var p []struct{ Addr, Balance *big.Int }
	if err := rlp.NewStream(strings.NewReader(data), 0).Decode(&p); err != nil {
		panic(err)
	}
	ga := make(GenesisAlloc, len(p))
	for _, account := range p {
		ga[common.BigToAddress(account.Addr)] = GenesisAccount{Balance: account.Balance}
	}
	return ga
}

var fakeMainnetGenesisHash = common.HexToHash("0x41442cdabaf3cdcf0109ab0acd96ab2b8f06c4888952ee53a8e166d4c7a04bd5")
var fakeRopstenGenesisHash = common.HexToHash("0xea76bad279d6e0be97a6c2b7adb75fbfc5566e59cb23869e0c91a7c1fff96ae0")
var fakeGoerliGenesisHash = common.HexToHash("0xff59f9571fa557f03d46cbfcfb508fe16f665fb70b80fdfcc8311cffc706934c")
var fakeRinkebyGenesisHash = common.HexToHash("0xa6f4addcecb90a03354cdf993f8c3c99d208c796735000aaa8954fb0540b4fe3")

func loadGenesisTestAllocData() *genesisTestAllocData {
	m := genesisTestAllocData{}
	err := json.Unmarshal(genesisTestAllocJsonData, &m)
	if err != nil {
		panic(err)
	}
	return &m
}

func fakeMainnetGenesisBlock() *Genesis {
	data := loadGenesisTestAllocData()
	return &Genesis{
		Config:     params.TestChainConfig,
		Nonce:      66,
		ExtraData:  hexutil.MustDecode("0x11bbe8db4e347b4e8c937c1c8370e4b5ed33adb3db69cbdb7a38e1e50b1b82fa"),
		GasLimit:   5000,
		Difficulty: big.NewInt(17179869184),
		Alloc:      decodePrealloc(string(data.FakeMainnetAllocData)),
	}
}

func fakeRopstenGenesisBlock() *Genesis {
	data := loadGenesisTestAllocData()
	return &Genesis{
		Config:     params.TestChainConfig,
		Nonce:      66,
		ExtraData:  hexutil.MustDecode("0x3535353535353535353535353535353535353535353535353535353535353535"),
		GasLimit:   16777216,
		Difficulty: big.NewInt(1048576),
		Alloc:      decodePrealloc(string(data.FakeRopstenAllocData)),
	}
}

func fakeRinkebyGenesisBlock() *Genesis {
	data := loadGenesisTestAllocData()
	return &Genesis{
		Config:     params.TestChainConfig,
		Timestamp:  1492009146,
		ExtraData:  hexutil.MustDecode("0x52657370656374206d7920617574686f7269746168207e452e436172746d616e42eb768f2244c8811c63729a21a3569731535f067ffc57839b00206d1ad20c69a1981b489f772031b279182d99e65703f0076e4812653aab85fca0f00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   4700000,
		Difficulty: big.NewInt(1),
		Alloc:      decodePrealloc(string(data.FakeRinkebyAllocData)),
	}
}

func fakeGoerliGenesisBlock() *Genesis {
	data := loadGenesisTestAllocData()
	return &Genesis{
		Config:     params.TestChainConfig,
		Timestamp:  1548854791,
		ExtraData:  hexutil.MustDecode("0x22466c6578692069732061207468696e6722202d204166726900000000000000e0a2bd4258d2768837baa26a28fe71dc079f84c70000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   10485760,
		Difficulty: big.NewInt(1),
		Alloc:      decodePrealloc(string(data.FakeGoerliAllocData)),
	}
}
