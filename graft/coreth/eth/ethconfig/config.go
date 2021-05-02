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
	"errors"
	"math/big"
	"time"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/eth/gasprice"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/downloader"
)

// DefaultFullGPOConfig contains default gasprice oracle settings for full node.
var DefaultFullGPOConfig = gasprice.Config{
	Blocks:     20,
	Percentile: 60,
	MaxPrice:   gasprice.DefaultMaxPrice,
}

// DefaultLightGPOConfig contains default gasprice oracle settings for light client.
var DefaultLightGPOConfig = gasprice.Config{
	Blocks:     2,
	Percentile: 60,
	MaxPrice:   gasprice.DefaultMaxPrice,
}

// DefaultConfig contains default settings for use on the Avalanche main net.
var DefaultConfig = NewDefaultConfig()

func NewDefaultConfig() Config {
	return Config{
		SyncMode:                downloader.FastSync,
		NetworkId:               1,
		LightPeers:              100,
		UltraLightFraction:      75,
		DatabaseCache:           512,
		TrieCleanCache:          154,
		TrieCleanCacheJournal:   "triecache",
		TrieCleanCacheRejournal: 60 * time.Minute,
		TrieDirtyCache:          256,
		TrieTimeout:             60 * time.Minute,
		SnapshotCache:           102,
		Miner: miner.Config{
			GasFloor:              8000000,
			GasCeil:               8000000,
			ApricotPhase1GasLimit: params.ApricotPhase1GasLimit,
			GasPrice:              big.NewInt(params.GWei),
			Recommit:              3 * time.Second,
		},
		TxPool:      core.DefaultTxPoolConfig,
		RPCGasCap:   25000000,
		GPO:         DefaultFullGPOConfig,
		RPCTxFeeCap: 1, // 1 ether
	}
}

// Original Code:
// func init() {
// 	home := os.Getenv("HOME")
// 	if home == "" {
// 		if user, err := user.Current(); err == nil {
// 			home = user.HomeDir
// 		}
// 	}
// 	if runtime.GOOS == "darwin" {
// 		DefaultConfig.Ethash.DatasetDir = filepath.Join(home, "Library", "Ethash")
// 	} else if runtime.GOOS == "windows" {
// 		localappdata := os.Getenv("LOCALAPPDATA")
// 		if localappdata != "" {
// 			DefaultConfig.Ethash.DatasetDir = filepath.Join(localappdata, "Ethash")
// 		} else {
// 			DefaultConfig.Ethash.DatasetDir = filepath.Join(home, "AppData", "Local", "Ethash")
// 		}
// 	} else {
// 		DefaultConfig.Ethash.DatasetDir = filepath.Join(home, ".ethash")
// 	}
// }

//go:generate gencodec -type Config -formats toml -out gen_config.go

type Config struct {
	// The genesis block, which is inserted if the database is empty.
	// If nil, the Ethereum main net block is used.
	Genesis *core.Genesis `toml:",omitempty"`

	// Protocol options
	NetworkId uint64 // Network ID to use for selecting peers to connect to
	SyncMode  downloader.SyncMode

	// This can be set to list of enrtree:// URLs which will be queried for
	// for nodes to connect to.
	DiscoveryURLs []string

	NoPruning  bool // Whether to disable pruning and flush everything to disk
	NoPrefetch bool // Whether to disable prefetching and only load state on demand

	TxLookupLimit uint64 `toml:",omitempty"` // The maximum number of blocks from head whose tx indices are reserved.

	// Whitelist of required block number -> hash values to accept
	Whitelist map[uint64]common.Hash `toml:"-"`

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

	TrieCleanCache          int
	TrieCleanCacheJournal   string        `toml:",omitempty"` // Disk journal directory for trie cache to survive node restarts
	TrieCleanCacheRejournal time.Duration `toml:",omitempty"` // Time interval to regenerate the journal for clean cache
	TrieDirtyCache          int
	TrieTimeout             time.Duration
	SnapshotCache           int
	Preimages               bool

	// Mining options
	Miner miner.Config

	// Original Code:
	// // Ethash options
	// Ethash ethash.Config

	// Transaction pool options
	TxPool core.TxPoolConfig

	// Gas Price Oracle options
	GPO gasprice.Config

	// Enables tracking of SHA3 preimages in the VM
	EnablePreimageRecording bool

	// Miscellaneous options
	DocRoot string `toml:"-"`

	// Type of the EWASM interpreter ("" for default)
	EWASMInterpreter string

	// Type of the EVM interpreter ("" for default)
	EVMInterpreter string

	// RPCGasCap is the global gas cap for eth-call variants.
	RPCGasCap uint64 `toml:",omitempty"`

	// RPCTxFeeCap is the global transaction fee(price * gaslimit) cap for
	// send-transction variants. The unit is ether.
	RPCTxFeeCap float64 `toml:",omitempty"`

	// Original Code:
	// // Checkpoint is a hardcoded checkpoint which can be nil.
	// Checkpoint *params.TrustedCheckpoint `toml:",omitempty"`

	// // CheckpointOracle is the configuration for checkpoint oracle.
	// CheckpointOracle *params.CheckpointOracleConfig `toml:",omitempty"`

	// AllowUnfinalizedQueries allow unfinalized queries
	AllowUnfinalizedQueries bool
}

func (cfg *Config) SetGCMode(gcmode string) error {
	switch gcmode {
	case "full":
		cfg.NoPruning = false
	case "archive":
		cfg.NoPruning = true
	default:
		return errors.New("invalid gcmode value")
	}
	return nil
}
