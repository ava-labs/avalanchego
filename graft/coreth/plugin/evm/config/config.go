// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/spf13/cast"
)

const (
	defaultAcceptorQueueLimit                     = 64 // Provides 2 minutes of buffer (2s block target) for a commit delay
	defaultPruningEnabled                         = true
	defaultCommitInterval                         = 4096
	defaultTrieCleanCache                         = 512
	defaultTrieDirtyCache                         = 512
	defaultTrieDirtyCommitTarget                  = 20
	defaultTriePrefetcherParallelism              = 16
	defaultSnapshotCache                          = 256
	defaultSyncableCommitInterval                 = defaultCommitInterval * 4
	defaultSnapshotWait                           = false
	defaultRpcGasCap                              = 50_000_000 // Default to 50M Gas Limit
	defaultRpcTxFeeCap                            = 100        // 100 AVAX
	defaultMetricsExpensiveEnabled                = true
	defaultApiMaxDuration                         = 0 // Default to no maximum API call duration
	defaultWsCpuRefillRate                        = 0 // Default to no maximum WS CPU usage
	defaultWsCpuMaxStored                         = 0 // Default to no maximum WS CPU usage
	defaultMaxBlocksPerRequest                    = 0 // Default to no maximum on the number of blocks per getLogs request
	defaultContinuousProfilerFrequency            = 15 * time.Minute
	defaultContinuousProfilerMaxFiles             = 5
	defaultPushGossipPercentStake                 = .9
	defaultPushGossipNumValidators                = 100
	defaultPushGossipNumPeers                     = 0
	defaultPushRegossipNumValidators              = 10
	defaultPushRegossipNumPeers                   = 0
	defaultPushGossipFrequency                    = 100 * time.Millisecond
	defaultPullGossipFrequency                    = 1 * time.Second
	defaultTxRegossipFrequency                    = 30 * time.Second
	defaultOfflinePruningBloomFilterSize   uint64 = 512 // Default size (MB) for the offline pruner to use
	defaultLogLevel                               = "info"
	defaultLogJSONFormat                          = false
	defaultMaxOutboundActiveRequests              = 16
	defaultPopulateMissingTriesParallelism        = 1024
	defaultStateSyncServerTrieCache               = 64 // MB
	defaultAcceptedCacheSize                      = 32 // blocks

	// defaultStateSyncMinBlocks is the minimum number of blocks the blockchain
	// should be ahead of local last accepted to perform state sync.
	// This constant is chosen so normal bootstrapping is preferred when it would
	// be faster than state sync.
	// time assumptions:
	// - normal bootstrap processing time: ~14 blocks / second
	// - state sync time: ~6 hrs.
	defaultStateSyncMinBlocks   = 300_000
	DefaultStateSyncRequestSize = 1024 // the number of key/values to ask peers for per request

	estimatedBlockAcceptPeriod        = 2 * time.Second
	defaultHistoricalProofQueryWindow = uint64(24 * time.Hour / estimatedBlockAcceptPeriod)
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
	// Coreth APIs
	SnowmanAPIEnabled     bool   `json:"snowman-api-enabled"`
	AdminAPIEnabled       bool   `json:"admin-api-enabled"`
	AdminAPIDir           string `json:"admin-api-dir"`
	CorethAdminAPIEnabled bool   `json:"coreth-admin-api-enabled"` // Deprecated: use AdminAPIEnabled instead
	CorethAdminAPIDir     string `json:"coreth-admin-api-dir"`     // Deprecated: use AdminAPIDir instead
	WarpAPIEnabled        bool   `json:"warp-api-enabled"`

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
	TxRegossipFrequency       Duration `json:"tx-regossip-frequency"` // Deprecated: use RegossipFrequency instead

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
	StateSyncEnabled         *bool  `json:"state-sync-enabled"`     // Pointer distinguishes false (no state sync) and not set (state sync only at genesis).
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
	// Deprecated, use 'TransactionHistory' instead.
	TxLookupLimit uint64 `json:"tx-lookup-limit"`

	// SkipTxIndexing skips indexing transactions.
	// This is useful for validators that don't need to index transactions.
	// TxLookupLimit can be still used to control unindexing old transactions.
	SkipTxIndexing bool `json:"skip-tx-indexing"`

	// WarpOffChainMessages encodes off-chain messages (unrelated to any on-chain event ie. block or AddressedCall)
	// that the node should be willing to sign.
	// Note: only supports AddressedCall payloads as defined here:
	// https://github.com/ava-labs/avalanchego/tree/7623ffd4be915a5185c9ed5e11fa9be15a6e1f00/vms/platformvm/warp/payload#addressedcall
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`

	// RPC settings
	HttpBodyLimit uint64 `json:"http-body-limit"`
}

// TxPoolConfig contains the transaction pool config to be passed
// to [Config.SetDefaults].
type TxPoolConfig struct {
	PriceLimit   uint64
	PriceBump    uint64
	AccountSlots uint64
	GlobalSlots  uint64
	AccountQueue uint64
	GlobalQueue  uint64
	Lifetime     time.Duration
}

// EthAPIs returns an array of strings representing the Eth APIs that should be enabled
func (c Config) EthAPIs() []string {
	return c.EnabledEthAPIs
}

func (c *Config) SetDefaults(txPoolConfig TxPoolConfig) {
	c.EnabledEthAPIs = defaultEnabledAPIs
	c.RPCGasCap = defaultRpcGasCap
	c.RPCTxFeeCap = defaultRpcTxFeeCap
	c.MetricsExpensiveEnabled = defaultMetricsExpensiveEnabled

	// TxPool settings
	c.TxPoolPriceLimit = txPoolConfig.PriceLimit
	c.TxPoolPriceBump = txPoolConfig.PriceBump
	c.TxPoolAccountSlots = txPoolConfig.AccountSlots
	c.TxPoolGlobalSlots = txPoolConfig.GlobalSlots
	c.TxPoolAccountQueue = txPoolConfig.AccountQueue
	c.TxPoolGlobalQueue = txPoolConfig.GlobalQueue
	c.TxPoolLifetime.Duration = txPoolConfig.Lifetime

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
	c.TriePrefetcherParallelism = defaultTriePrefetcherParallelism
	c.SnapshotCache = defaultSnapshotCache
	c.AcceptorQueueLimit = defaultAcceptorQueueLimit
	c.CommitInterval = defaultCommitInterval
	c.SnapshotWait = defaultSnapshotWait
	c.PushGossipPercentStake = defaultPushGossipPercentStake
	c.PushGossipNumValidators = defaultPushGossipNumValidators
	c.PushGossipNumPeers = defaultPushGossipNumPeers
	c.PushRegossipNumValidators = defaultPushRegossipNumValidators
	c.PushRegossipNumPeers = defaultPushRegossipNumPeers
	c.PushGossipFrequency.Duration = defaultPushGossipFrequency
	c.PullGossipFrequency.Duration = defaultPullGossipFrequency
	c.RegossipFrequency.Duration = defaultTxRegossipFrequency
	c.OfflinePruningBloomFilterSize = defaultOfflinePruningBloomFilterSize
	c.LogLevel = defaultLogLevel
	c.LogJSONFormat = defaultLogJSONFormat
	c.MaxOutboundActiveRequests = defaultMaxOutboundActiveRequests
	c.PopulateMissingTriesParallelism = defaultPopulateMissingTriesParallelism
	c.StateSyncServerTrieCache = defaultStateSyncServerTrieCache
	c.StateSyncCommitInterval = defaultSyncableCommitInterval
	c.StateSyncMinBlocks = defaultStateSyncMinBlocks
	c.StateSyncRequestSize = DefaultStateSyncRequestSize
	c.AllowUnprotectedTxHashes = defaultAllowUnprotectedTxHashes
	c.AcceptedCacheSize = defaultAcceptedCacheSize
	c.HistoricalProofQueryWindow = defaultHistoricalProofQueryWindow
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
func (c *Config) Validate(networkID uint32) error {
	// Ensure that non-standard commit interval is not allowed for production networks
	if constants.ProductionNetworkIDs.Contains(networkID) {
		if c.CommitInterval != defaultCommitInterval {
			return fmt.Errorf("cannot start non-local network with commit interval %d different than %d", c.CommitInterval, defaultCommitInterval)
		}
		if c.StateSyncCommitInterval != defaultSyncableCommitInterval {
			return fmt.Errorf("cannot start non-local network with syncable interval %d different than %d", c.StateSyncCommitInterval, defaultSyncableCommitInterval)
		}
	}

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

	if c.PushGossipPercentStake < 0 || c.PushGossipPercentStake > 1 {
		return fmt.Errorf("push-gossip-percent-stake is %f but must be in the range [0, 1]", c.PushGossipPercentStake)
	}
	return nil
}

func (c *Config) Deprecate() string {
	msg := ""
	// Deprecate the old config options and set the new ones.
	if c.CorethAdminAPIEnabled {
		msg += "coreth-admin-api-enabled is deprecated, use admin-api-enabled instead. "
		c.AdminAPIEnabled = c.CorethAdminAPIEnabled
	}
	if c.CorethAdminAPIDir != "" {
		msg += "coreth-admin-api-dir is deprecated, use admin-api-dir instead. "
		c.AdminAPIDir = c.CorethAdminAPIDir
	}
	if c.TxRegossipFrequency != (Duration{}) {
		msg += "tx-regossip-frequency is deprecated, use regossip-frequency instead. "
		c.RegossipFrequency = c.TxRegossipFrequency
	}
	if c.TxLookupLimit != 0 {
		msg += "tx-lookup-limit is deprecated, use transaction-history instead. "
		c.TransactionHistory = c.TxLookupLimit
	}

	return msg
}
