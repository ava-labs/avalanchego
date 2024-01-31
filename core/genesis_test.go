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
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/utils"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/stretchr/testify/require"
)

func setupGenesisBlock(db ethdb.Database, triedb *trie.Database, genesis *Genesis, lastAcceptedHash common.Hash) (*params.ChainConfig, common.Hash, error) {
	return SetupGenesisBlock(db, triedb, genesis, lastAcceptedHash, false)
}

func TestGenesisBlockForTesting(t *testing.T) {
	genesisBlockForTestingHash := common.HexToHash("0xb378f22ccd9ad52c6c42f5d46ef2aad6d6866cfcb778ea97a0b6dfde13387330")
	block := GenesisBlockForTesting(rawdb.NewMemoryDatabase(), common.Address{1}, big.NewInt(1))
	if block.Hash() != genesisBlockForTestingHash {
		t.Errorf("wrong testing genesis hash, got %v, want %v", block.Hash(), genesisBlockForTestingHash)
	}
}

func TestSetupGenesis(t *testing.T) {
	apricotPhase1Config := *params.TestApricotPhase1Config
	apricotPhase1Config.ApricotPhase1BlockTimestamp = utils.NewUint64(100)
	var (
		customghash = common.HexToHash("0x1099a11e9e454bd3ef31d688cf21936671966407bc330f051d754b5ce401e7ed")
		customg     = Genesis{
			Config: &apricotPhase1Config,
			Alloc: GenesisAlloc{
				{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			},
		}
		oldcustomg = customg
	)

	rollbackApricotPhase1Config := apricotPhase1Config
	rollbackApricotPhase1Config.ApricotPhase1BlockTimestamp = utils.NewUint64(90)
	oldcustomg.Config = &rollbackApricotPhase1Config
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
				return setupGenesisBlock(db, trie.NewDatabase(db), new(Genesis), common.Hash{})
			},
			wantErr:    errGenesisNoConfig,
			wantConfig: nil,
		},
		{
			name: "no block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				return setupGenesisBlock(db, trie.NewDatabase(db), nil, common.Hash{})
			},
			wantErr:    ErrNoGenesis,
			wantConfig: nil,
		},
		{
			name: "custom block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				customg.MustCommit(db)
				return setupGenesisBlock(db, trie.NewDatabase(db), nil, common.Hash{})
			},
			wantErr:    ErrNoGenesis,
			wantConfig: nil,
		},
		{
			name: "compatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				oldcustomg.MustCommit(db)
				return setupGenesisBlock(db, trie.NewDatabase(db), &customg, customghash)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "incompatible config for avalanche fork in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				// Commit the 'old' genesis block with ApricotPhase1 transition at 90.
				// Advance to block #4, past the ApricotPhase1 transition block of customg.
				genesis := oldcustomg.MustCommit(db)

				bc, _ := NewBlockChain(db, DefaultCacheConfig, &oldcustomg, dummy.NewFullFaker(), vm.Config{}, genesis.Hash(), false)
				defer bc.Stop()

				blocks, _, _ := GenerateChain(oldcustomg.Config, genesis, dummy.NewFullFaker(), db, 4, 25, nil)
				bc.InsertChain(blocks)

				for _, block := range blocks {
					if err := bc.Accept(block); err != nil {
						t.Fatal(err)
					}
				}

				// This should return a compatibility error.
				return setupGenesisBlock(db, trie.NewDatabase(db), &customg, bc.lastAccepted.Hash())
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
			wantErr: &params.ConfigCompatError{
				What:         "ApricotPhase1 fork block timestamp",
				StoredTime:   u64(90),
				NewTime:      u64(100),
				RewindToTime: 89,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			config, hash, err := test.fn(db)
			// Check the return values.
			if !reflect.DeepEqual(err, test.wantErr) {
				spew := spew.ConfigState{DisablePointerAddresses: true, DisableCapacities: true}
				t.Errorf("returned error %#v, want %#v", spew.NewFormatter(err), spew.NewFormatter(test.wantErr))
			}
			if !reflect.DeepEqual(config, test.wantConfig) {
				t.Errorf("returned %v\nwant     %v", config, test.wantConfig)
			}
			if hash != test.wantHash {
				t.Errorf("returned hash %s, want %s", hash.Hex(), test.wantHash.Hex())
			} else if err == nil {
				// Check database content.
				stored := rawdb.ReadBlock(db, test.wantHash, 0)
				if stored.Hash() != test.wantHash {
					t.Errorf("block in DB has hash %s, want %s", stored.Hash(), test.wantHash)
				}
			}
		})
	}
}

// regression test for precompile activation after header block
func TestNetworkUpgradeBetweenHeadAndAcceptedBlock(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	customg := Genesis{
		Config: params.TestApricotPhase1Config,
		Alloc: GenesisAlloc{
			{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
		},
	}
	bc, _ := NewBlockChain(db, DefaultCacheConfig, &customg, dummy.NewFullFaker(), vm.Config{}, common.Hash{}, false)
	defer bc.Stop()

	// Advance header to block #4, past the ApricotPhase2 timestamp.
	_, blocks, _, _ := GenerateChainWithGenesis(&customg, dummy.NewFullFaker(), 4, 25, nil)

	require := require.New(t)
	_, err := bc.InsertChain(blocks)
	require.NoError(err)

	// accept up to block #2
	for _, block := range blocks[:2] {
		require.NoError(bc.Accept(block))
	}
	block := bc.CurrentBlock()

	require.Equal(blocks[1].Hash(), bc.lastAccepted.Hash())
	// header must be bigger than last accepted
	require.Greater(block.Time, bc.lastAccepted.Time())

	activatedGenesis := customg
	apricotPhase2Timestamp := utils.NewUint64(51)
	updatedApricotPhase2Config := *params.TestApricotPhase1Config
	updatedApricotPhase2Config.ApricotPhase2BlockTimestamp = apricotPhase2Timestamp

	activatedGenesis.Config = &updatedApricotPhase2Config

	// assert block is after the activation block
	require.Greater(block.Time, *apricotPhase2Timestamp)
	// assert last accepted block is before the activation block
	require.Less(bc.lastAccepted.Time(), *apricotPhase2Timestamp)

	// This should not return any error since the last accepted block is before the activation block.
	config, _, err := setupGenesisBlock(db, trie.NewDatabase(db), &activatedGenesis, bc.lastAccepted.Hash())
	require.NoError(err)
	if !reflect.DeepEqual(config, activatedGenesis.Config) {
		t.Errorf("returned %v\nwant     %v", config, activatedGenesis.Config)
	}
}
