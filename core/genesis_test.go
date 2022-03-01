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

	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/core/vm"
	"github.com/ava-labs/subnet-evm/ethdb"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/common"
)

func setupGenesisBlock(db ethdb.Database, genesis *Genesis) (*params.ChainConfig, common.Hash, error) {
	conf, err := SetupGenesisBlock(db, genesis)
	stored := rawdb.ReadCanonicalHash(db, 0)
	return conf, stored, err
}

func TestGenesisBlockForTesting(t *testing.T) {
	genesisBlockForTestingHash := common.HexToHash("0x114ce61b50051f70768f982f7b59e82dd73b7bbd768e310c9d9f508d492e687b")
	block := GenesisBlockForTesting(rawdb.NewMemoryDatabase(), common.Address{1}, big.NewInt(1))
	if block.Hash() != genesisBlockForTestingHash {
		t.Errorf("wrong testing genesis hash, got %v, want %v", block.Hash(), genesisBlockForTestingHash)
	}
}

func TestSetupGenesis(t *testing.T) {
	preSubnetConfig := *params.TestPreSubnetEVMConfig
	preSubnetConfig.SubnetEVMTimestamp = big.NewInt(100)
	var (
		customghash = common.HexToHash("0x89c99d90b79719238d2645c7642f2c9295246e80775b38cfd162b696817fbd50")
		customg     = Genesis{
			Config: &preSubnetConfig,
			Alloc: GenesisAlloc{
				{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			},
		}
		oldcustomg = customg
	)

	rollbackpreSubnetConfig := preSubnetConfig
	rollbackpreSubnetConfig.SubnetEVMTimestamp = big.NewInt(90)
	oldcustomg.Config = &rollbackpreSubnetConfig
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
			name: "compatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				oldcustomg.MustCommit(db)
				return setupGenesisBlock(db, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "incompatible config for avalanche fork in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, error) {
				// Commit the 'old' genesis block with SubnetEVM transition at 90.
				// Advance to block #4, past the SubnetEVM transition block of customg.
				genesis := oldcustomg.MustCommit(db)

				bc, _ := NewBlockChain(db, DefaultCacheConfig, oldcustomg.Config, dummy.NewFullFaker(), vm.Config{}, common.Hash{})
				defer bc.Stop()

				blocks, _, _ := GenerateChain(oldcustomg.Config, genesis, dummy.NewFullFaker(), db, 4, 25, nil)
				bc.InsertChain(blocks)
				bc.CurrentBlock()
				// This should return a compatibility error.
				return setupGenesisBlock(db, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
			wantErr: &params.ConfigCompatError{
				What:         "SubnetEVM fork block timestamp",
				StoredConfig: big.NewInt(90),
				NewConfig:    big.NewInt(100),
				RewindTo:     89,
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

func TestStatefulPrecompilesConfigure(t *testing.T) {
	type test struct {
		getConfig   func() *params.ChainConfig
		assertState func(t *testing.T, sdb *state.StateDB)
	}

	addr := common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")

	// Test suite to ensure that stateful precompiles are configured correctly in the genesis.
	for name, test := range map[string]test{
		"allow list enabled in genesis": {
			getConfig: func() *params.ChainConfig {
				config := *params.TestChainConfig
				config.AllowListConfig = precompile.AllowListConfig{
					BlockTimestamp:  big.NewInt(0),
					AllowListAdmins: []common.Address{addr},
				}
				return &config
			},
			assertState: func(t *testing.T, sdb *state.StateDB) {
				// Check that the stateful precompiles were configured correctly
				if role := precompile.GetAllowListStatus(sdb, addr); !precompile.IsAllowListAdmin(role) {
					t.Fatalf("Expected allow list status to be %s, but found %s", precompile.Admin, role)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			config := test.getConfig()

			genesis := &Genesis{
				Config: config,
				Alloc: GenesisAlloc{
					{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
				},
			}

			db := rawdb.NewMemoryDatabase()
			_, err := SetupGenesisBlock(db, genesis)
			if err != nil {
				t.Fatal(err)
			}

			genesisBlock := genesis.ToBlock(nil)
			genesisRoot := genesisBlock.Root()

			statedb, err := state.New(genesisRoot, state.NewDatabase(db), nil)
			if err != nil {
				t.Fatal(err)
			}
			test.assertState(t, statedb)
		})
	}
}
