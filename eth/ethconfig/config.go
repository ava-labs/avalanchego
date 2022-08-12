// (c) 2019-2020, Ava Labs, Inc.
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

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/eth/gasprice"
	"github.com/ava-labs/coreth/miner"
)

// DefaultFullGPOConfig contains default gasprice oracle settings for full node.
var DefaultFullGPOConfig = gasprice.Config{
	Blocks:              40,
	Percentile:          60,
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
		NetworkId:             1,
		LightPeers:            100,
		UltraLightFraction:    75,
		DatabaseCache:         512,
		TrieCleanCache:        256,
		TrieDirtyCache:        256,
		TrieDirtyCommitTarget: 20,
		SnapshotCache:         128,
		Miner:                 miner.Config{},
		TxPool:                core.DefaultTxPoolConfig,
		RPCGasCap:             25000000,
		RPCEVMTimeout:         5 * time.Second,
		GPO:                   DefaultFullGPOConfig,
		RPCTxFeeCap:           1, // 1 AVAX
	}
}

//go:generate go run github.com/fjl/gencodec -type Config -formats toml -out gen_config.go

// Config contains configuration options for of the ETH and LES protocols.
type Config struct {
	// The genesis block, which is inserted if the database is empty.
	// If nil, the Ethereum main net block is used.
	Genesis *core.Genesis `toml:",omitempty"`

	// Protocol options
	NetworkId uint64 // Network ID to use for selecting peers to connect to

	// This can be set to list of enrtree:// URLs which will be queried for
	// for nodes to connect to.
	DiscoveryURLs []string

	Pruning                         bool    // Whether to disable pruning and flush everything to disk
	AcceptorQueueLimit              int     // Maximum blocks to queue before blocking during acceptance
	CommitInterval                  uint64  // If pruning is enabled, specified the interval at which to commit an entire trie to disk.
	PopulateMissingTries            *uint64 // Height at which to start re-populating missing tries on startup.
	PopulateMissingTriesParallelism int     // Number of concurrent readers to use when re-populating missing tries on startup.
	AllowMissingTries               bool    // Whether to allow an archival node to run with pruning enabled and corrupt a complete index.
	SnapshotDelayInit               bool    // Whether snapshot tree should be initialized on startup or delayed until explicit call
	SnapshotAsync                   bool    // Whether to generate the initial snapshot in async mode
	SnapshotVerify                  bool    // Whether to verify generated snapshots
	SkipSnapshotRebuild             bool    // Whether to skip rebuilding the snapshot in favor of returning an error (only set to true for tests)

	// Light client options
	LightServ    int  `toml:",omitempty"` // Maximum percentage of time allowed for serving LES requests
	LightIngress int  `toml:",omitempty"` // Incoming bandwidth limit for light servers
	LightEgress  int  `toml:",omitempty"` // Outgoing bandwidth limit for light servers
	LightPeers   int  `toml:",omitempty"` // Maximum number of LES client peers
	LightNoPrune bool `toml:",omitempty"` // Whether to disable light chain pruning

	// Ultra Light client options
	UltraLightServers      []string `toml:",omitempty"` // List of trusted ultra light servers
	UltraLightFraction     int      `toml:",omitempty"` // Percentage of trusted servers to accept an announcement
	UltraLightOnlyAnnounce bool     `toml:",omitempty"` // Whether to only announce headers, or also serve them

	// Database options
	SkipBcVersionCheck bool `toml:"-"`
	DatabaseHandles    int  `toml:"-"`
	DatabaseCache      int
	// DatabaseFreezer    string

	TrieCleanCache        int
	TrieDirtyCache        int
	TrieDirtyCommitTarget int
	SnapshotCache         int
	Preimages             bool

	// Mining options
	Miner miner.Config

	// Transaction pool options
	TxPool core.TxPoolConfig

	// Gas Price Oracle options
	GPO gasprice.Config

	// Enables tracking of SHA3 preimages in the VM
	EnablePreimageRecording bool

	// Miscellaneous options
	DocRoot string `toml:"-"`

	// RPCGasCap is the global gas cap for eth-call variants.
	RPCGasCap uint64 `toml:",omitempty"`

	// RPCEVMTimeout is the global timeout for eth-call.
	RPCEVMTimeout time.Duration

	// RPCTxFeeCap is the global transaction fee(price * gaslimit) cap for
	// send-transction variants. The unit is ether.
	RPCTxFeeCap float64 `toml:",omitempty"`

	// AllowUnfinalizedQueries allow unfinalized queries
	AllowUnfinalizedQueries bool

	// AllowUnprotectedTxs allow unprotected transactions to be locally issued.
	// Unprotected transactions are transactions that are signed without EIP-155
	// replay protection.
	AllowUnprotectedTxs bool

	// OfflinePruning enables offline pruning on startup of the node. If a node is started
	// with this configuration option, it must finish pruning before resuming normal operation.
	OfflinePruning                bool
	OfflinePruningBloomFilterSize uint64
	OfflinePruningDataDirectory   string
}
