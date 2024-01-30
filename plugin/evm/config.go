// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ava-labs/subnet-evm/core/txpool"
	"github.com/ava-labs/subnet-evm/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/spf13/cast"
)

const (
	defaultAcceptorQueueLimit                         = 64 // Provides 2 minutes of buffer (2s block target) for a commit delay
	defaultPruningEnabled                             = true
	defaultCommitInterval                             = 4096
	defaultTrieCleanCache                             = 512
	defaultTrieDirtyCache                             = 512
	defaultTrieDirtyCommitTarget                      = 20
	defaultSnapshotCache                              = 256
	defaultSyncableCommitInterval                     = defaultCommitInterval * 4
	defaultSnapshotWait                               = false
	defaultRpcGasCap                                  = 50_000_000 // Default to 50M Gas Limit
	defaultRpcTxFeeCap                                = 100        // 100 AVAX
	defaultMetricsExpensiveEnabled                    = true
	defaultApiMaxDuration                             = 0 // Default to no maximum API call duration
	defaultWsCpuRefillRate                            = 0 // Default to no maximum WS CPU usage
	defaultWsCpuMaxStored                             = 0 // Default to no maximum WS CPU usage
	defaultMaxBlocksPerRequest                        = 0 // Default to no maximum on the number of blocks per getLogs request
	defaultContinuousProfilerFrequency                = 15 * time.Minute
	defaultContinuousProfilerMaxFiles                 = 5
	defaultRegossipFrequency                          = 1 * time.Minute
	defaultRegossipMaxTxs                             = 16
	defaultRegossipTxsPerAddress                      = 1
	defaultPriorityRegossipFrequency                  = 1 * time.Second
	defaultPriorityRegossipMaxTxs                     = 32
	defaultPriorityRegossipTxsPerAddress              = 16
	defaultOfflinePruningBloomFilterSize       uint64 = 512 // Default size (MB) for the offline pruner to use
	defaultLogLevel                                   = "info"
	defaultLogJSONFormat                              = false
	defaultMaxOutboundActiveRequests                  = 16
	defaultMaxOutboundActiveCrossChainRequests        = 64
	defaultPopulateMissingTriesParallelism            = 1024
	defaultStateSyncServerTrieCache                   = 64 // MB
	defaultAcceptedCacheSize                          = 32 // blocks

	// defaultStateSyncMinBlocks is the minimum number of blocks the blockchain
	// should be ahead of local last accepted to perform state sync.
	// This constant is chosen so normal bootstrapping is preferred when it would
	// be faster than state sync.
	// time assumptions:
	// - normal bootstrap processing time: ~14 blocks / second
	// - state sync time: ~6 hrs.
	defaultStateSyncMinBlocks   = 300_000
	defaultStateSyncRequestSize = 1024 // the number of key/values to ask peers for per request
)

var (
	defaultEnabledAPIs = []string{
		"eth",
		"eth-filter",
		"net",
		"web3",
		"internal-eth",
		"internal-blockchain",
		"internal-transaction",
	}
	defaultAllowUnprotectedTxHashes = []common.Hash{
		common.HexToHash("0xfefb2da535e927b85fe68eb81cb2e4a5827c905f78381a01ef2322aa9b0aee8e"), // EIP-1820: https://eips.ethereum.org/EIPS/eip-1820
	}
)

type Duration struct {
	time.Duration
}

// Config ...
type Config struct {
	// Airdrop
	AirdropFile string `json:"airdrop"`

	// Subnet EVM APIs
	SnowmanAPIEnabled bool   `json:"snowman-api-enabled"`
	AdminAPIEnabled   bool   `json:"admin-api-enabled"`
	AdminAPIDir       string `json:"admin-api-dir"`
	WarpAPIEnabled    bool   `json:"warp-api-enabled"`

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
	TrieCleanCache        int      `json:"trie-clean-cache"`         // Size of the trie clean cache (MB)
	TrieCleanJournal      string   `json:"trie-clean-journal"`       // Directory to use to save the trie clean cache (must be populated to enable journaling the trie clean cache)
	TrieCleanRejournal    Duration `json:"trie-clean-rejournal"`     // Frequency to re-journal the trie clean cache to disk (minimum 1 minute, must be populated to enable journaling the trie clean cache)
	TrieDirtyCache        int      `json:"trie-dirty-cache"`         // Size of the trie dirty cache (MB)
	TrieDirtyCommitTarget int      `json:"trie-dirty-commit-target"` // Memory limit to target in the dirty cache before performing a commit (MB)
	SnapshotCache         int      `json:"snapshot-cache"`           // Size of the snapshot disk layer clean cache (MB)

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

	// Metric Settings
	MetricsExpensiveEnabled bool `json:"metrics-expensive-enabled"` // Debug-level metrics that might impact runtime performance

	// API Settings
	LocalTxsEnabled bool `json:"local-txs-enabled"`

	TxPoolJournal      string   `json:"tx-pool-journal"`
	TxPoolRejournal    Duration `json:"tx-pool-rejournal"`
	TxPoolPriceLimit   uint64   `json:"tx-pool-price-limit"`
	TxPoolPriceBump    uint64   `json:"tx-pool-price-bump"`
	TxPoolAccountSlots uint64   `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64   `json:"tx-pool-global-slots"`
	TxPoolAccountQueue uint64   `json:"tx-pool-account-queue"`
	TxPoolGlobalQueue  uint64   `json:"tx-pool-global-queue"`

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
	RemoteGossipOnlyEnabled       bool             `json:"remote-gossip-only-enabled"`
	RegossipFrequency             Duration         `json:"regossip-frequency"`
	RegossipMaxTxs                int              `json:"regossip-max-txs"`
	RegossipTxsPerAddress         int              `json:"regossip-txs-per-address"`
	PriorityRegossipFrequency     Duration         `json:"priority-regossip-frequency"`
	PriorityRegossipMaxTxs        int              `json:"priority-regossip-max-txs"`
	PriorityRegossipTxsPerAddress int              `json:"priority-regossip-txs-per-address"`
	PriorityRegossipAddresses     []common.Address `json:"priority-regossip-addresses"`

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
	MaxOutboundActiveRequests           int64 `json:"max-outbound-active-requests"`
	MaxOutboundActiveCrossChainRequests int64 `json:"max-outbound-active-cross-chain-requests"`

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

	// TxLookupLimit is the maximum number of blocks from head whose tx indices
	// are reserved:
	//  * 0:   means no limit
	//  * N:   means N block limit [HEAD-N+1, HEAD] and delete extra indexes
	TxLookupLimit uint64 `json:"tx-lookup-limit"`

	// WarpOffChainMessages encodes off-chain messages (unrelated to any on-chain event ie. block or AddressedCall)
	// that the node should be willing to sign.
	// Note: only supports AddressedCall payloads as defined here:
	// https://github.com/ava-labs/avalanchego/tree/7623ffd4be915a5185c9ed5e11fa9be15a6e1f00/vms/platformvm/warp/payload#addressedcall
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`
}

// EthAPIs returns an array of strings representing the Eth APIs that should be enabled
func (c Config) EthAPIs() []string {
	return c.EnabledEthAPIs
}

func (c Config) EthBackendSettings() eth.Settings {
	return eth.Settings{MaxBlocksPerRequest: c.MaxBlocksPerRequest}
}

func (c *Config) SetDefaults() {
	c.EnabledEthAPIs = defaultEnabledAPIs
	c.RPCGasCap = defaultRpcGasCap
	c.RPCTxFeeCap = defaultRpcTxFeeCap
	c.MetricsExpensiveEnabled = defaultMetricsExpensiveEnabled

	c.TxPoolJournal = txpool.DefaultConfig.Journal
	c.TxPoolRejournal = Duration{txpool.DefaultConfig.Rejournal}
	c.TxPoolPriceLimit = txpool.DefaultConfig.PriceLimit
	c.TxPoolPriceBump = txpool.DefaultConfig.PriceBump
	c.TxPoolAccountSlots = txpool.DefaultConfig.AccountSlots
	c.TxPoolGlobalSlots = txpool.DefaultConfig.GlobalSlots
	c.TxPoolAccountQueue = txpool.DefaultConfig.AccountQueue
	c.TxPoolGlobalQueue = txpool.DefaultConfig.GlobalQueue

	c.APIMaxDuration.Duration = defaultApiMaxDuration
	c.WSCPURefillRate.Duration = defaultWsCpuRefillRate
	c.WSCPUMaxStored.Duration = defaultWsCpuMaxStored
	c.MaxBlocksPerRequest = defaultMaxBlocksPerRequest
	c.ContinuousProfilerFrequency.Duration = defaultContinuousProfilerFrequency
	c.ContinuousProfilerMaxFiles = defaultContinuousProfilerMaxFiles
	c.Pruning = defaultPruningEnabled
	c.TrieCleanCache = defaultTrieCleanCache
	c.TrieDirtyCache = defaultTrieDirtyCache
	c.TrieDirtyCommitTarget = defaultTrieDirtyCommitTarget
	c.SnapshotCache = defaultSnapshotCache
	c.AcceptorQueueLimit = defaultAcceptorQueueLimit
	c.CommitInterval = defaultCommitInterval
	c.SnapshotWait = defaultSnapshotWait
	c.RegossipFrequency.Duration = defaultRegossipFrequency
	c.RegossipMaxTxs = defaultRegossipMaxTxs
	c.RegossipTxsPerAddress = defaultRegossipTxsPerAddress
	c.PriorityRegossipFrequency.Duration = defaultPriorityRegossipFrequency
	c.PriorityRegossipMaxTxs = defaultPriorityRegossipMaxTxs
	c.PriorityRegossipTxsPerAddress = defaultPriorityRegossipTxsPerAddress
	c.OfflinePruningBloomFilterSize = defaultOfflinePruningBloomFilterSize
	c.LogLevel = defaultLogLevel
	c.LogJSONFormat = defaultLogJSONFormat
	c.MaxOutboundActiveRequests = defaultMaxOutboundActiveRequests
	c.MaxOutboundActiveCrossChainRequests = defaultMaxOutboundActiveCrossChainRequests
	c.PopulateMissingTriesParallelism = defaultPopulateMissingTriesParallelism
	c.StateSyncServerTrieCache = defaultStateSyncServerTrieCache
	c.StateSyncCommitInterval = defaultSyncableCommitInterval
	c.StateSyncMinBlocks = defaultStateSyncMinBlocks
	c.StateSyncRequestSize = defaultStateSyncRequestSize
	c.AllowUnprotectedTxHashes = defaultAllowUnprotectedTxHashes
	c.AcceptedCacheSize = defaultAcceptedCacheSize
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

// Validate returns an error if this is an invalid config.
func (c *Config) Validate() error {
	if c.PopulateMissingTries != nil && (c.OfflinePruning || c.Pruning) {
		return fmt.Errorf("cannot enable populate missing tries while offline pruning (enabled: %t)/pruning (enabled: %t) are enabled", c.OfflinePruning, c.Pruning)
	}
	if c.PopulateMissingTries != nil && c.PopulateMissingTriesParallelism < 1 {
		return fmt.Errorf("cannot enable populate missing tries without at least one reader (parallelism: %d)", c.PopulateMissingTriesParallelism)
	}

	if !c.Pruning && c.OfflinePruning {
		return fmt.Errorf("cannot run offline pruning while pruning is disabled")
	}
	// If pruning is enabled, the commit interval must be non-zero so the node commits state tries every CommitInterval blocks.
	if c.Pruning && c.CommitInterval == 0 {
		return fmt.Errorf("cannot use commit interval of 0 with pruning enabled")
	}

	return nil
}
