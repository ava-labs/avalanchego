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

package ethconfig

import (
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/txpool/blobpool"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/txpool/legacypool"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/eth/gasprice"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/miner"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

// DefaultFullGPOConfig contains default gasprice oracle settings for full node.
var DefaultFullGPOConfig = gasprice.Config{
	Blocks:              40,
	Percentile:          40,
	MaxLookbackSeconds:  gasprice.DefaultMaxLookbackSeconds,
	MaxCallBlockHistory: gasprice.DefaultMaxCallBlockHistory,
	MaxBlockHistory:     gasprice.DefaultMaxBlockHistory,
	MinPrice:            gasprice.DefaultMinPrice,
	MaxPrice:            gasprice.DefaultMaxPrice,
	MinGasUsed:          gasprice.DefaultMinGasUsed,
}

// DefaultConfig contains default settings for use on the Avalanche main net.
var DefaultConfig = NewDefaultConfig()

func NewDefaultConfig() Config {
	return Config{
		NetworkId:                 0, // enable auto configuration of networkID == chainID
		StateHistory:              params.FullImmutabilityThreshold,
		TrieCleanCache:            512,
		TrieDirtyCache:            256,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 16,
		SnapshotCache:             256,
		AcceptedCacheSize:         32,
		Miner:                     miner.Config{},
		TxPool:                    legacypool.DefaultConfig,
		BlobPool:                  blobpool.DefaultConfig,
		RPCGasCap:                 25000000,
		RPCEVMTimeout:             5 * time.Second,
		GPO:                       DefaultFullGPOConfig,
		RPCTxFeeCap:               1, // 1 AVAX
	}
}

//go:generate go tool -modfile=../../../../tools/go.mod gencodec -type Config -formats toml -out gen_config.go

// Config contains configuration options for ETH and LES protocols.
type Config struct {
	// The genesis block, which is inserted if the database is empty.
	// If nil, the Ethereum main net block is used.
	Genesis *core.Genesis `toml:",omitempty"`

	// Network ID separates blockchains on the peer-to-peer networking level. When left
	// zero, the chain ID is used as network ID.
	NetworkId uint64

	Pruning                         bool    // Whether to disable pruning and flush everything to disk
	AcceptorQueueLimit              int     // Maximum blocks to queue before blocking during acceptance
	CommitInterval                  uint64  // If pruning is enabled, specified the interval at which to commit an entire trie to disk.
	PopulateMissingTries            *uint64 // Height at which to start re-populating missing tries on startup.
	PopulateMissingTriesParallelism int     // Number of concurrent readers to use when re-populating missing tries on startup.
	AllowMissingTries               bool    // Whether to allow an archival node to run with pruning enabled and corrupt a complete index.
	SnapshotDelayInit               bool    // Whether snapshot tree should be initialized on startup or delayed until explicit call (= StateSyncEnabled)
	SnapshotWait                    bool    // Whether to wait for the initial snapshot generation
	SnapshotVerify                  bool    // Whether to verify generated snapshots
	SkipSnapshotRebuild             bool    // Whether to skip rebuilding the snapshot in favor of returning an error (only set to true for tests)

	// Database options
	SkipBcVersionCheck bool `toml:"-"`

	// TrieDB and snapshot options
	TrieCleanCache            int
	TrieDirtyCache            int
	TrieDirtyCommitTarget     int
	TriePrefetcherParallelism int
	SnapshotCache             int
	Preimages                 bool

	// AcceptedCacheSize is the depth of accepted headers cache and accepted
	// logs cache at the accepted tip.
	AcceptedCacheSize int

	// Mining options
	Miner miner.Config

	// Transaction pool options
	TxPool   legacypool.Config
	BlobPool blobpool.Config

	// Gas Price Oracle options
	GPO gasprice.Config

	// Enables tracking of SHA3 preimages in the VM
	EnablePreimageRecording bool

	// RPCGasCap is the global gas cap for eth-call variants.
	RPCGasCap uint64 `toml:",omitempty"`

	// RPCEVMTimeout is the global timeout for eth-call.
	RPCEVMTimeout time.Duration

	// RPCTxFeeCap is the global transaction fee(price * gaslimit) cap for
	// send-transaction variants. The unit is ether.
	RPCTxFeeCap float64 `toml:",omitempty"`

	// AllowUnfinalizedQueries allow unfinalized queries
	AllowUnfinalizedQueries bool

	// HistoricalProofQueryWindow is the number of blocks before the last accepted block to be accepted for state queries.
	// For archive nodes, it defaults to 43200 and can be set to 0 to indicate to accept any block query.
	// For non-archive nodes, it is forcibly set to the value of StateHistory.
	HistoricalProofQueryWindow uint64

	// AllowUnprotectedTxs allow unprotected transactions to be locally issued.
	// Unprotected transactions are transactions that are signed without EIP-155
	// replay protection.
	AllowUnprotectedTxs bool
	// AllowUnprotectedTxHashes provides a list of transaction hashes, which will be allowed
	// to be issued without replay protection over the API even if AllowUnprotectedTxs is false.
	AllowUnprotectedTxHashes []common.Hash

	// OfflinePruning enables offline pruning on startup of the node. If a node is started
	// with this configuration option, it must finish pruning before resuming normal operation.
	OfflinePruning                bool
	OfflinePruningBloomFilterSize uint64
	OfflinePruningDataDirectory   string

	// SkipUpgradeCheck disables checking that upgrades must take place before the last
	// accepted block. Skipping this check is useful when a node operator does not update
	// their node before the network upgrade and their node accepts blocks that have
	// identical state with the pre-upgrade ruleset.
	SkipUpgradeCheck bool

	// TransactionHistory is the maximum number of blocks from head whose tx indices
	// are reserved:
	//  * 0:   means no limit
	//  * N:   means N block limit [HEAD-N+1, HEAD] and delete extra indexes
	TransactionHistory uint64 `toml:",omitempty"` // The maximum number of blocks from head whose tx indices are reserved.
	StateHistory       uint64 `toml:",omitempty"` // The maximum number of blocks from head whose state histories are reserved.

	// State scheme represents the scheme used to store ethereum states and trie
	// nodes on top. It can be 'hash', 'path', or none which means use the scheme
	// consistent with persistent state.
	StateScheme string `toml:",omitempty"`

	// SkipTxIndexing skips indexing transactions.
	// This is useful for validators that don't need to index transactions.
	// TransactionHistory can be still used to control unindexing old transactions.
	SkipTxIndexing bool
}
