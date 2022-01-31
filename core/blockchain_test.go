// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"

	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/vm"
	"github.com/ava-labs/subnet-evm/ethdb"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
)

func TestArchiveBlockChain(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := NewBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit: 256,
				TrieDirtyLimit: 256,
				Pruning:        false, // Archive mode
				SnapshotLimit:  256,
			},
			chainConfig,
			dummy.NewFaker(),
			vm.Config{},
			lastAcceptedHash,
		)
		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestArchiveBlockChainSnapsDisabled(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := NewBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit: 256,
				TrieDirtyLimit: 256,
				Pruning:        false, // Archive mode
				SnapshotLimit:  0,     // Disable snapshots
			},
			chainConfig,
			dummy.NewFaker(),
			vm.Config{},
			lastAcceptedHash,
		)
		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestPruningBlockChain(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := NewBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit: 256,
				TrieDirtyLimit: 256,
				Pruning:        true, // Enable pruning
				SnapshotLimit:  256,
			},
			chainConfig,
			dummy.NewFaker(),
			vm.Config{},
			lastAcceptedHash,
		)
		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestPruningBlockChainSnapsDisabled(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := NewBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit: 256,
				TrieDirtyLimit: 256,
				Pruning:        true, // Enable pruning
				SnapshotLimit:  0,    // Disable snapshots
			},
			chainConfig,
			dummy.NewFaker(),
			vm.Config{},
			lastAcceptedHash,
		)
		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

type wrappedStateManager struct {
	TrieWriter
}

func (w *wrappedStateManager) Shutdown() error { return nil }

func TestPruningBlockChainUngracefulShutdown(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := NewBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit: 256,
				TrieDirtyLimit: 256,
				Pruning:        true, // Enable pruning
				SnapshotLimit:  256,
			},
			chainConfig,
			dummy.NewFaker(),
			vm.Config{},
			lastAcceptedHash,
		)
		if err != nil {
			return nil, err
		}

		// Overwrite state manager, so that Shutdown is not called.
		// This tests to ensure that the state manager handles an ungraceful shutdown correctly.
		blockchain.stateManager = &wrappedStateManager{TrieWriter: blockchain.stateManager}
		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestPruningBlockChainUngracefulShutdownSnapsDisabled(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := NewBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit: 256,
				TrieDirtyLimit: 256,
				Pruning:        true, // Enable pruning
				SnapshotLimit:  0,    // Disable snapshots
			},
			chainConfig,
			dummy.NewFaker(),
			vm.Config{},
			lastAcceptedHash,
		)
		if err != nil {
			return nil, err
		}

		// Overwrite state manager, so that Shutdown is not called.
		// This tests to ensure that the state manager handles an ungraceful shutdown correctly.
		blockchain.stateManager = &wrappedStateManager{TrieWriter: blockchain.stateManager}
		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestEnableSnapshots(t *testing.T) {
	// Set snapshots to be disabled the first time, and then enable them on the restart
	snapLimit := 0
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := NewBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit: 256,
				TrieDirtyLimit: 256,
				Pruning:        true,      // Enable pruning
				SnapshotLimit:  snapLimit, // Disable snapshots
			},
			chainConfig,
			dummy.NewFaker(),
			vm.Config{},
			lastAcceptedHash,
		)
		if err != nil {
			return nil, err
		}
		snapLimit = 256

		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestCorruptSnapshots(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Delete the snapshot block hash and state root to ensure that if we die in between writing a snapshot
		// diff layer to disk at any point, we can still recover on restart.
		rawdb.DeleteSnapshotBlockHash(db)
		rawdb.DeleteSnapshotRoot(db)
		// Import the chain. This runs all block validation rules.
		blockchain, err := NewBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit: 256,
				TrieDirtyLimit: 256,
				Pruning:        true, // Enable pruning
				SnapshotLimit:  256,  // Disable snapshots
			},
			chainConfig,
			dummy.NewFaker(),
			vm.Config{},
			lastAcceptedHash,
		)
		if err != nil {
			return nil, err
		}

		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}
