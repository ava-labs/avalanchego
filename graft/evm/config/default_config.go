// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	"github.com/ava-labs/libevm/common"
)

const (
	defaultCommitInterval = 4096

	// Protocol constants for transaction gossip bloom filters.
	// These values are fixed across all nodes and cannot be changed without
	// a coordinated network upgrade.
	TxGossipBloomMinTargetElements       = 8 * 1024
	TxGossipBloomTargetFalsePositiveRate = 0.01
	TxGossipBloomResetFalsePositiveRate  = 0.05
	TxGossipBloomChurnMultiplier         = 3
)

// SetDefaults applies default values to fields that are still zero-valued.
func (c *BaseConfig) SetDefaults() {
	// Special slice fields with non-empty defaults
	if len(c.AllowUnprotectedTxHashes) == 0 {
		c.AllowUnprotectedTxHashes = []common.Hash{
			common.HexToHash("0xfefb2da535e927b85fe68eb81cb2e4a5827c905f78381a01ef2322aa9b0aee8e"), // EIP-1820
		}
	}
	if len(c.EnabledEthAPIs) == 0 {
		c.EnabledEthAPIs = []string{
			"eth",
			"eth-filter",
			"net",
			"web3",
			"internal-eth",
			"internal-blockchain",
			"internal-transaction",
		}
	}

	// Numeric fields with non-zero defaults
	if c.AcceptorQueueLimit == 0 {
		c.AcceptorQueueLimit = 64 // Provides 2 minutes of buffer (2s block target) for a commit delay
	}
	if c.CommitInterval == 0 {
		c.CommitInterval = defaultCommitInterval
	}
	if c.TrieCleanCache == 0 {
		c.TrieCleanCache = 512
	}
	if c.TrieDirtyCache == 0 {
		c.TrieDirtyCache = 512
	}
	if c.TrieDirtyCommitTarget == 0 {
		c.TrieDirtyCommitTarget = 20
	}
	if c.TriePrefetcherParallelism == 0 {
		c.TriePrefetcherParallelism = 16
	}
	if c.SnapshotCache == 0 {
		c.SnapshotCache = 256
	}
	if c.StateSyncCommitInterval == 0 {
		c.StateSyncCommitInterval = defaultCommitInterval * 4
	}
	if c.RPCGasCap == 0 {
		c.RPCGasCap = 50_000_000 // 50M Gas Limit
	}
	if c.RPCTxFeeCap == 0 {
		c.RPCTxFeeCap = 100 // 100 AVAX
	}
	if c.ContinuousProfilerFrequency.Duration == 0 {
		c.ContinuousProfilerFrequency = wrapDuration(15 * time.Minute)
	}
	if c.ContinuousProfilerMaxFiles == 0 {
		c.ContinuousProfilerMaxFiles = 5
	}
	if c.PushGossipPercentStake == 0 {
		c.PushGossipPercentStake = .9
	}
	if c.PushGossipNumValidators == 0 {
		c.PushGossipNumValidators = 100
	}
	if c.PushRegossipNumValidators == 0 {
		c.PushRegossipNumValidators = 10
	}
	if c.PushGossipFrequency.Duration == 0 {
		c.PushGossipFrequency = wrapDuration(100 * time.Millisecond)
	}
	if c.PullGossipFrequency.Duration == 0 {
		c.PullGossipFrequency = wrapDuration(1 * time.Second)
	}
	if c.RegossipFrequency.Duration == 0 {
		c.RegossipFrequency = wrapDuration(30 * time.Second)
	}
	if c.OfflinePruningBloomFilterSize == 0 {
		c.OfflinePruningBloomFilterSize = 512
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.MaxOutboundActiveRequests == 0 {
		c.MaxOutboundActiveRequests = 16
	}
	if c.PopulateMissingTriesParallelism == 0 {
		c.PopulateMissingTriesParallelism = 1024
	}
	if c.StateSyncServerTrieCache == 0 {
		c.StateSyncServerTrieCache = 64
	}
	if c.AcceptedCacheSize == 0 {
		c.AcceptedCacheSize = 32
	}
	if c.StateSyncMinBlocks == 0 {
		c.StateSyncMinBlocks = 300_000
	}
	if c.StateSyncRequestSize == 0 {
		c.StateSyncRequestSize = 1024
	}
	if c.StateHistory == 0 {
		c.StateHistory = 32
	}
	if c.HistoricalProofQueryWindow == 0 {
		c.HistoricalProofQueryWindow = uint64(24 * time.Hour / (2 * time.Second))
	}
	if c.TxPoolPriceLimit == 0 {
		c.TxPoolPriceLimit = 1
	}
	if c.TxPoolPriceBump == 0 {
		c.TxPoolPriceBump = 10
	}
	if c.TxPoolAccountSlots == 0 {
		c.TxPoolAccountSlots = 16
	}
	if c.TxPoolGlobalSlots == 0 {
		c.TxPoolGlobalSlots = 4096 + 1024 // urgent + floating queue capacity with 4:1 ratio
	}
	if c.TxPoolAccountQueue == 0 {
		c.TxPoolAccountQueue = 64
	}
	if c.TxPoolGlobalQueue == 0 {
		c.TxPoolGlobalQueue = 1024
	}
	if c.TxPoolLifetime.Duration == 0 {
		c.TxPoolLifetime = wrapDuration(10 * time.Minute)
	}
	if c.BatchRequestLimit == 0 {
		c.BatchRequestLimit = 1000
	}
	if c.BatchResponseMaxSize == 0 {
		c.BatchResponseMaxSize = 25 * 1000 * 1000 // 25MB
	}
	if !c.Pruning {
		c.Pruning = true
	}
	if !c.MetricsExpensiveEnabled {
		c.MetricsExpensiveEnabled = true
	}
}

func wrapDuration(t time.Duration) Duration {
	return Duration{t}
}
