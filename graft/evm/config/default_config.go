// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/libevm/common"
)

const (
	defaultCommitInterval                       = 4096
	DefaultTxGossipBloomMinTargetElements       = 8 * 1024
	DefaultTxGossipBloomTargetFalsePositiveRate = 0.01
	DefaultTxGossipBloomResetFalsePositiveRate  = 0.05
	DefaultTxGossipBloomChurnMultiplier         = 3
)

// newDefaultCommonConfig returns a CommonConfig with sensible defaults shared by both C-Chain and L1.
func newDefaultCommonConfig() CommonConfig {
	return CommonConfig{
		AllowUnprotectedTxHashes: []common.Hash{
			common.HexToHash("0xfefb2da535e927b85fe68eb81cb2e4a5827c905f78381a01ef2322aa9b0aee8e"), // EIP-1820: https://eips.ethereum.org/EIPS/eip-1820
		},
		EnabledEthAPIs: []string{
			"eth",
			"eth-filter",
			"net",
			"web3",
			"internal-eth",
			"internal-blockchain",
			"internal-transaction",
		},
		// Provides 2 minutes of buffer (2s block target) for a commit delay
		AcceptorQueueLimit:        64,
		Pruning:                   true,
		CommitInterval:            defaultCommitInterval,
		TrieCleanCache:            512,
		TrieDirtyCache:            512,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 16,
		SnapshotCache:             256,
		StateSyncCommitInterval:   defaultCommitInterval * 4,
		SnapshotWait:              false,
		RPCGasCap:                 50_000_000, // 50M Gas Limit
		RPCTxFeeCap:               100,        // 100 AVAX
		MetricsExpensiveEnabled:   true,
		// Default to no maximum API call duration
		APIMaxDuration: timeToDuration(0),
		// Default to no maximum WS CPU usage
		WSCPURefillRate: timeToDuration(0),
		// Default to no maximum WS CPU usage
		WSCPUMaxStored: timeToDuration(0),
		// Default to no maximum on the number of blocks per getLogs request
		MaxBlocksPerRequest:         0,
		ContinuousProfilerFrequency: timeToDuration(15 * time.Minute),
		ContinuousProfilerMaxFiles:  5,
		PushGossipPercentStake:      .9,
		PushGossipNumValidators:     100,
		PushGossipNumPeers:          0,
		PushRegossipNumValidators:   10,
		PushRegossipNumPeers:        0,
		PushGossipFrequency:         timeToDuration(100 * time.Millisecond),
		PullGossipFrequency:         timeToDuration(1 * time.Second),
		RegossipFrequency:           timeToDuration(30 * time.Second),
		// Default size (MB) for the offline pruner to use
		OfflinePruningBloomFilterSize:   uint64(512),
		LogLevel:                        "info",
		LogJSONFormat:                   false,
		MaxOutboundActiveRequests:       16,
		PopulateMissingTriesParallelism: 1024,
		StateSyncServerTrieCache:        64, // MB
		AcceptedCacheSize:               32, // blocks
		// StateSyncMinBlocks is the minimum number of blocks the blockchain
		// should be ahead of local last accepted to perform state sync.
		// This constant is chosen so normal bootstrapping is preferred when it would
		// be faster than state sync.
		// time assumptions:
		// - normal bootstrap processing time: ~14 blocks / second
		// - state sync time: ~6 hrs.
		StateSyncMinBlocks: 300_000,
		// the number of key/values to ask peers for per request
		StateSyncRequestSize: 1024,
		StateHistory:         uint64(32),
		// Estimated block count in 24 hours with 2s block accept period
		HistoricalProofQueryWindow: uint64(24 * time.Hour / (2 * time.Second)),
		// Mempool settings
		TxPoolPriceLimit:   1,
		TxPoolPriceBump:    10,
		TxPoolAccountSlots: 16,
		TxPoolGlobalSlots:  4096 + 1024, // urgent + floating queue capacity with 4:1 ratio,
		TxPoolAccountQueue: 64,
		TxPoolGlobalQueue:  1024,
		TxPoolLifetime:     timeToDuration(10 * time.Minute),
		// RPC settings
		BatchRequestLimit:    1000,
		BatchResponseMaxSize: 25 * 1000 * 1000, // 25MB
	}
}

func NewDefaultCChainConfig() CChainConfig {
	return CChainConfig{
		CommonConfig: newDefaultCommonConfig(),
		// Price Option Defaults (C-Chain specific)
		PriceOptionSlowFeePercentage: uint64(95),
		PriceOptionFastFeePercentage: uint64(105),
		PriceOptionMaxTip:            uint64(20 * utils.GWei),
	}
}

func NewDefaultL1Config() L1Config {
	return L1Config{
		CommonConfig: newDefaultCommonConfig(),
		// Subnet EVM API settings
		ValidatorsAPIEnabled: true,
		// Database settings
		DatabaseType: pebbledb.Name,
	}
}

func timeToDuration(t time.Duration) Duration {
	return Duration{t}
}
