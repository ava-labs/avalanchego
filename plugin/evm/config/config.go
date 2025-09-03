// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/spf13/cast"
)

type (
	PBool    bool
	Duration struct {
		time.Duration
	}
)

// Config ...
type Config struct {
	// Airdrop
	AirdropFile string `json:"airdrop"`

	// Subnet EVM APIs
	ValidatorsAPIEnabled bool   `json:"validators-api-enabled"`
	AdminAPIEnabled      bool   `json:"admin-api-enabled"`
	AdminAPIDir          string `json:"admin-api-dir"`
	WarpAPIEnabled       bool   `json:"warp-api-enabled"`

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
	PushGossipPercentStake    float64          `json:"push-gossip-percent-stake"`
	PushGossipNumValidators   int              `json:"push-gossip-num-validators"`
	PushGossipNumPeers        int              `json:"push-gossip-num-peers"`
	PushRegossipNumValidators int              `json:"push-regossip-num-validators"`
	PushRegossipNumPeers      int              `json:"push-regossip-num-peers"`
	PushGossipFrequency       Duration         `json:"push-gossip-frequency"`
	PullGossipFrequency       Duration         `json:"pull-gossip-frequency"`
	RegossipFrequency         Duration         `json:"regossip-frequency"`
	PriorityRegossipAddresses []common.Address `json:"priority-regossip-addresses"`

	// Log
	LogLevel      string `json:"log-level"`
	LogJSONFormat bool   `json:"log-json-format"`

	// Address for Tx Fees (must be empty if not supported by blockchain)
	FeeRecipient string `json:"feeRecipient"`

	// Offline Pruning Settings
	OfflinePruning                bool   `json:"offline-pruning-enabled"`
	OfflinePruningBloomFilterSize uint64 `json:"offline-pruning-bloom-filter-size"`
	OfflinePruningDataDirectory   string `json:"offline-pruning-data-directory"`

	// VM2VM network
	MaxOutboundActiveRequests int64 `json:"max-outbound-active-requests"`

	// Sync settings
	StateSyncEnabled         bool   `json:"state-sync-enabled"`
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
	HttpBodyLimit        uint64 `json:"http-body-limit"`
	BatchRequestLimit    uint64 `json:"batch-request-limit"`
	BatchResponseMaxSize uint64 `json:"batch-response-max-size"`

	// Database settings
	UseStandaloneDatabase *PBool `json:"use-standalone-database"`
	DatabaseConfigContent string `json:"database-config"`
	DatabaseConfigFile    string `json:"database-config-file"`
	DatabaseType          string `json:"database-type"`
	DatabasePath          string `json:"database-path"`
	DatabaseReadOnly      bool   `json:"database-read-only"`

	// Database Scheme
	StateScheme string `json:"state-scheme"`
}

// GetConfig returns a new config object with the default values set and the
// deprecation message.
// If configBytes is not empty, it will be unmarshalled into the config object.
// If the unmarshalling fails, an error is returned.
// If the config is invalid, an error is returned.
func GetConfig(configBytes []byte, networkID uint32) (Config, string, error) {
	config := NewDefaultConfig()
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &config); err != nil {
			return Config{}, "", fmt.Errorf("failed to unmarshal config %s: %w", string(configBytes), err)
		}
	}
	if err := config.validate(networkID); err != nil {
		return Config{}, "", err
	}
	// We should deprecate config flags as the first thing, before we do anything else
	// because this can set old flags to new flags. log the message after we have
	// initialized the logger.
	deprecateMsg := config.deprecate()
	return config, deprecateMsg, nil
}

// EthAPIs returns an array of strings representing the Eth APIs that should be enabled
func (c Config) EthAPIs() []string {
	return c.EnabledEthAPIs
}

func (d *Duration) UnmarshalJSON(data []byte) (err error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	d.Duration, err = cast.ToDurationE(v)
	return err
}

// String implements the stringer interface.
func (d Duration) String() string {
	return d.Duration.String()
}

// String implements the stringer interface.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// validate returns an error if this is an invalid config.
func (c *Config) validate(_ uint32) error {
	if c.PopulateMissingTries != nil && (c.OfflinePruning || c.Pruning) {
		return fmt.Errorf("cannot enable populate missing tries while offline pruning (enabled: %t)/pruning (enabled: %t) are enabled", c.OfflinePruning, c.Pruning)
	}
	if c.PopulateMissingTries != nil && c.PopulateMissingTriesParallelism < 1 {
		return fmt.Errorf("cannot enable populate missing tries without at least one reader (parallelism: %d)", c.PopulateMissingTriesParallelism)
	}

	if !c.Pruning && c.OfflinePruning {
		return errors.New("cannot run offline pruning while pruning is disabled")
	}
	// If pruning is enabled, the commit interval must be non-zero so the node commits state tries every CommitInterval blocks.
	if c.Pruning && c.CommitInterval == 0 {
		return errors.New("cannot use commit interval of 0 with pruning enabled")
	}
	if c.Pruning && c.StateHistory == 0 {
		return errors.New("cannot use state history of 0 with pruning enabled")
	}

	if c.PushGossipPercentStake < 0 || c.PushGossipPercentStake > 1 {
		return fmt.Errorf("push-gossip-percent-stake is %f but must be in the range [0, 1]", c.PushGossipPercentStake)
	}
	return nil
}

// deprecate returns a string of deprecation messages for the config.
// This is used to log a message when the config is loaded and contains deprecated flags.
// This function should be kept as a placeholder even if it is empty.
func (c *Config) deprecate() string {
	msg := ""

	return msg
}

func (p *PBool) String() string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%t", *p)
}

func (p *PBool) Bool() bool {
	if p == nil {
		return false
	}
	return bool(*p)
}
