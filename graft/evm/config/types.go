// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/spf13/cast"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// Duration wraps time.Duration to support JSON unmarshaling from both
// string formats ("1m", "5s") and integer nanoseconds.
type Duration struct {
	time.Duration
}

// MarshalJSON marshals the duration as a JSON string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

func (d *Duration) UnmarshalJSON(data []byte) (err error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	d.Duration, err = cast.ToDurationE(v)
	return err
}

// String returns the duration as a string.
func (d Duration) String() string {
	return d.Duration.String()
}

// CommonConfig contains configuration fields shared between CChainConfig and L1Config.
type CommonConfig struct {
	// MinDelayTarget is the minimum delay between blocks (in milliseconds) that this node will attempt to use
	// when creating blocks. If this config is not specified, the node will
	// default to use the parent block's target delay per second.
	MinDelayTarget *uint64 `json:"min-delay-target,omitempty"`

	// APIs
	AdminAPIEnabled bool   `json:"admin-api-enabled"`
	AdminAPIDir     string `json:"admin-api-dir"`
	WarpAPIEnabled  bool   `json:"warp-api-enabled"`

	// EnabledEthAPIs is a list of Ethereum services that should be enabled
	// If none is specified, then we use the default list [defaultEnabledAPIs]
	EnabledEthAPIs []string `json:"eth-apis"`

	// Continuous Profiler
	ContinuousProfilerDir       string   `json:"continuous-profiler-dir"`       // If set to non-empty string creates a continuous profiler
	ContinuousProfilerFrequency Duration `json:"continuous-profiler-frequency"` // Frequency to run continuous profiler if enabled
	ContinuousProfilerMaxFiles  int      `json:"continuous-profiler-max-files"` // Maximum number of files to maintain

	// API Gas/Price Caps
	RPCGasCap   uint64  `json:"rpc-gas-cap"`
	RPCTxFeeCap float64 `json:"rpc-tx-fee-cap"`

	// Cache settings
	TrieCleanCache            int `json:"trie-clean-cache"`            // Size of the trie clean cache (MB)
	TrieDirtyCache            int `json:"trie-dirty-cache"`            // Size of the trie dirty cache (MB)
	TrieDirtyCommitTarget     int `json:"trie-dirty-commit-target"`    // Memory limit to target in the dirty cache before performing a commit (MB)
	TriePrefetcherParallelism int `json:"trie-prefetcher-parallelism"` // Max concurrent disk reads trie prefetcher should perform at once
	SnapshotCache             int `json:"snapshot-cache"`              // Size of the snapshot disk layer clean cache (MB)

	// Eth Settings
	Preimages      bool `json:"preimages-enabled"`
	SnapshotWait   bool `json:"snapshot-wait"`
	SnapshotVerify bool `json:"snapshot-verification-enabled"`

	// Pruning Settings
	Pruning                         bool    `json:"pruning-enabled"`                    // If enabled, trie roots are only persisted every 4096 blocks
	AcceptorQueueLimit              int     `json:"accepted-queue-limit"`               // Maximum blocks to queue before blocking during acceptance
	CommitInterval                  uint64  `json:"commit-interval"`                    // Specifies the commit interval at which to persist EVM and atomic tries.
	AllowMissingTries               bool    `json:"allow-missing-tries"`                // If enabled, warnings preventing an incomplete trie index are suppressed
	PopulateMissingTries            *uint64 `json:"populate-missing-tries,omitempty"`   // Sets the starting point for re-populating missing tries. Disables re-generation if nil.
	PopulateMissingTriesParallelism int     `json:"populate-missing-tries-parallelism"` // Number of concurrent readers to use when re-populating missing tries on startup.
	PruneWarpDB                     bool    `json:"prune-warp-db-enabled"`              // Determines if the warpDB should be cleared on startup

	// HistoricalProofQueryWindow is, when running in archive mode only, the number of blocks before the
	// last accepted block to be accepted for proof state queries.
	HistoricalProofQueryWindow uint64 `json:"historical-proof-query-window,omitempty"`

	// Metric Settings
	MetricsExpensiveEnabled bool `json:"metrics-expensive-enabled"` // Debug-level metrics that might impact runtime performance

	// API Settings
	LocalTxsEnabled bool `json:"local-txs-enabled"`

	TxPoolPriceLimit   uint64   `json:"tx-pool-price-limit"`
	TxPoolPriceBump    uint64   `json:"tx-pool-price-bump"`
	TxPoolAccountSlots uint64   `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64   `json:"tx-pool-global-slots"`
	TxPoolAccountQueue uint64   `json:"tx-pool-account-queue"`
	TxPoolGlobalQueue  uint64   `json:"tx-pool-global-queue"`
	TxPoolLifetime     Duration `json:"tx-pool-lifetime"`

	APIMaxDuration           Duration      `json:"api-max-duration"`
	WSCPURefillRate          Duration      `json:"ws-cpu-refill-rate"`
	WSCPUMaxStored           Duration      `json:"ws-cpu-max-stored"`
	MaxBlocksPerRequest      int64         `json:"api-max-blocks-per-request"`
	AllowUnfinalizedQueries  bool          `json:"allow-unfinalized-queries"`
	AllowUnprotectedTxs      bool          `json:"allow-unprotected-txs"`
	AllowUnprotectedTxHashes []common.Hash `json:"allow-unprotected-tx-hashes"`

	// Keystore Settings
	KeystoreDirectory             string `json:"keystore-directory"` // both absolute and relative supported
	KeystoreExternalSigner        string `json:"keystore-external-signer"`
	KeystoreInsecureUnlockAllowed bool   `json:"keystore-insecure-unlock-allowed"`

	// Gossip Settings
	PushGossipPercentStake    float64  `json:"push-gossip-percent-stake"`
	PushGossipNumValidators   int      `json:"push-gossip-num-validators"`
	PushGossipNumPeers        int      `json:"push-gossip-num-peers"`
	PushRegossipNumValidators int      `json:"push-regossip-num-validators"`
	PushRegossipNumPeers      int      `json:"push-regossip-num-peers"`
	PushGossipFrequency       Duration `json:"push-gossip-frequency"`
	PullGossipFrequency       Duration `json:"pull-gossip-frequency"`
	RegossipFrequency         Duration `json:"regossip-frequency"`

	// Log
	LogLevel      string `json:"log-level"`
	LogJSONFormat bool   `json:"log-json-format"`

	// Offline Pruning Settings
	OfflinePruning                bool   `json:"offline-pruning-enabled"`
	OfflinePruningBloomFilterSize uint64 `json:"offline-pruning-bloom-filter-size"`
	OfflinePruningDataDirectory   string `json:"offline-pruning-data-directory"`

	// VM2VM network
	MaxOutboundActiveRequests int64 `json:"max-outbound-active-requests"`

	// Sync settings
	// StateSyncEnabled uses a pointer to distinguish false (no state sync) from not set (state sync only at genesis).
	StateSyncEnabled         *bool  `json:"state-sync-enabled"`
	StateSyncSkipResume      bool   `json:"state-sync-skip-resume"` // Forces state sync to use the highest available summary block
	StateSyncServerTrieCache int    `json:"state-sync-server-trie-cache"`
	StateSyncIDs             string `json:"state-sync-ids"`
	StateSyncCommitInterval  uint64 `json:"state-sync-commit-interval"`
	StateSyncMinBlocks       uint64 `json:"state-sync-min-blocks"`
	StateSyncRequestSize     uint16 `json:"state-sync-request-size"`

	// Database Settings
	InspectDatabase bool `json:"inspect-database"` // Inspects the database on startup if enabled.

	// SkipUpgradeCheck disables checking that upgrades must take place before the last
	// accepted block. Skipping this check is useful when a node operator does not update
	// their node before the network upgrade and their node accepts blocks that have
	// identical state with the pre-upgrade ruleset.
	SkipUpgradeCheck bool `json:"skip-upgrade-check"`

	// AcceptedCacheSize is the depth to keep in the accepted headers cache and the
	// accepted logs cache at the accepted tip.
	//
	// This is particularly useful for improving the performance of eth_getLogs
	// on RPC nodes.
	AcceptedCacheSize int `json:"accepted-cache-size"`

	// TransactionHistory is the maximum number of blocks from head whose tx indices
	// are reserved:
	//  * 0:   means no limit
	//  * N:   means N block limit [HEAD-N+1, HEAD] and delete extra indexes
	TransactionHistory uint64 `json:"transaction-history"`
	// The maximum number of blocks from head whose state histories are reserved for pruning blockchains.
	StateHistory uint64 `json:"state-history"`

	// SkipTxIndexing skips indexing transactions.
	// This is useful for validators that don't need to index transactions.
	// TransactionHistory can be still used to control unindexing old transactions.
	SkipTxIndexing bool `json:"skip-tx-indexing"`

	// WarpOffChainMessages encodes off-chain messages (unrelated to any on-chain event ie. block or AddressedCall)
	// that the node should be willing to sign.
	// Note: only supports AddressedCall payloads as defined here:
	// https://github.com/ava-labs/avalanchego/tree/7623ffd4be915a5185c9ed5e11fa9be15a6e1f00/vms/platformvm/warp/payload#addressedcall
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`

	// RPC settings
	HTTPBodyLimit        uint64 `json:"http-body-limit"`
	BatchRequestLimit    uint64 `json:"batch-request-limit"`
	BatchResponseMaxSize uint64 `json:"batch-response-max-size"`

	// Database Scheme
	StateScheme string `json:"state-scheme"`
}

// CChainConfig contains C-Chain specific configuration.
type CChainConfig struct {
	CommonConfig

	// GasTarget is the target gas per second that this node will attempt to use
	// when creating blocks. If this config is not specified, the node will
	// default to use the parent block's target gas per second.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`

	// Price Option Settings (C-Chain specific)
	PriceOptionSlowFeePercentage uint64 `json:"price-options-slow-fee-percentage"`
	PriceOptionFastFeePercentage uint64 `json:"price-options-fast-fee-percentage"`
	PriceOptionMaxTip            uint64 `json:"price-options-max-tip"`
}

// L1Config contains L1 (subnet-evm) specific configuration.
type L1Config struct {
	CommonConfig

	// Airdrop
	AirdropFile string `json:"airdrop"`

	// Subnet EVM APIs
	ValidatorsAPIEnabled bool `json:"validators-api-enabled"`

	// Gossip Settings (L1 specific)
	PriorityRegossipAddresses []common.Address `json:"priority-regossip-addresses"`

	// Address for Tx Fees (must be empty if not supported by blockchain)
	FeeRecipient string `json:"feeRecipient"`

	// Database settings (L1 specific)
	UseStandaloneDatabase *bool  `json:"use-standalone-database"`
	DatabaseConfigContent string `json:"database-config"`
	DatabaseConfigFile    string `json:"database-config-file"`
	DatabaseType          string `json:"database-type"`
	DatabasePath          string `json:"database-path"`
	DatabaseReadOnly      bool   `json:"database-read-only"`
}
