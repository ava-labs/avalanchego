// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"time"

	"github.com/ava-labs/coreth/eth"
	"github.com/spf13/cast"
)

const (
	defaultPruningEnabled              = true
	defaultSnapshotAsync               = true
	defaultRpcGasCap                   = 50_000_000 // Default to 50M Gas Limit
	defaultRpcTxFeeCap                 = 100        // 100 AVAX
	defaultMetricsEnabled              = false
	defaultMetricsExpensiveEnabled     = false
	defaultApiMaxDuration              = 0 // Default to no maximum API call duration
	defaultWsCpuRefillRate             = 0 // Default to no maximum WS CPU usage
	defaultWsCpuMaxStored              = 0 // Default to no maximum WS CPU usage
	defaultMaxBlocksPerRequest         = 0 // Default to no maximum on the number of blocks per getLogs request
	defaultContinuousProfilerFrequency = 15 * time.Minute
	defaultContinuousProfilerMaxFiles  = 5
	defaultTxRegossipFrequency         = 1 * time.Minute
	defaultTxRegossipMaxSize           = 15
	defaultLogLevel                    = "debug"
)

var defaultEnabledAPIs = []string{
	"public-eth",
	"public-eth-filter",
	"net",
	"web3",
	"internal-public-eth",
	"internal-public-blockchain",
	"internal-public-transaction-pool",
}

type Duration struct {
	time.Duration
}

// Config ...
type Config struct {
	// Coreth APIs
	SnowmanAPIEnabled     bool   `json:"snowman-api-enabled"`
	CorethAdminAPIEnabled bool   `json:"coreth-admin-api-enabled"`
	CorethAdminAPIDir     string `json:"coreth-admin-api-dir"`

	// EnabledEthAPIs is a list of Ethereum services that should be enabled
	// If none is specified, then we use the default list [defaultEnabledAPIs]
	EnabledEthAPIs []string `json:"eth-apis"`

	// Continuous Profiler
	ContinuousProfilerDir       string   `json:"continuous-profiler-dir"`       // If set to non-empty string creates a continuous profiler
	ContinuousProfilerFrequency Duration `json:"continuous-profiler-frequency"` // Frequency to run continuous profiler if enabled
	ContinuousProfilerMaxFiles  int      `json:"continuous-profiler-max-files"` // Maximum number of files to maintain

	// Coreth API Gas/Price Caps
	RPCGasCap   uint64  `json:"rpc-gas-cap"`
	RPCTxFeeCap float64 `json:"rpc-tx-fee-cap"`

	// Eth Settings
	Preimages      bool `json:"preimages-enabled"`
	Pruning        bool `json:"pruning-enabled"`
	SnapshotAsync  bool `json:"snapshot-async"`
	SnapshotVerify bool `json:"snapshot-verification-enabled"`

	// Metric Settings
	MetricsEnabled          bool `json:"metrics-enabled"`
	MetricsExpensiveEnabled bool `json:"metrics-expensive-enabled"`

	// API Settings
	LocalTxsEnabled         bool     `json:"local-txs-enabled"`
	APIMaxDuration          Duration `json:"api-max-duration"`
	WSCPURefillRate         Duration `json:"ws-cpu-refill-rate"`
	WSCPUMaxStored          Duration `json:"ws-cpu-max-stored"`
	MaxBlocksPerRequest     int64    `json:"api-max-blocks-per-request"`
	AllowUnfinalizedQueries bool     `json:"allow-unfinalized-queries"`
	AllowUnprotectedTxs     bool     `json:"allow-unprotected-txs"`

	// Keystore Settings
	KeystoreDirectory             string `json:"keystore-directory"` // both absolute and relative supported
	KeystoreExternalSigner        string `json:"keystore-external-signer"`
	KeystoreInsecureUnlockAllowed bool   `json:"keystore-insecure-unlock-allowed"`

	// Gossip Settings
	RemoteTxGossipOnlyEnabled bool     `json:"remote-tx-gossip-only-enabled"`
	TxRegossipFrequency       Duration `json:"tx-regossip-frequency"`
	TxRegossipMaxSize         int      `json:"tx-regossip-max-size"`

	// Log level
	LogLevel string `json:"log-level"`
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
	c.MetricsEnabled = defaultMetricsEnabled
	c.MetricsExpensiveEnabled = defaultMetricsExpensiveEnabled
	c.APIMaxDuration.Duration = defaultApiMaxDuration
	c.WSCPURefillRate.Duration = defaultWsCpuRefillRate
	c.WSCPUMaxStored.Duration = defaultWsCpuMaxStored
	c.MaxBlocksPerRequest = defaultMaxBlocksPerRequest
	c.ContinuousProfilerFrequency.Duration = defaultContinuousProfilerFrequency
	c.ContinuousProfilerMaxFiles = defaultContinuousProfilerMaxFiles
	c.Pruning = defaultPruningEnabled
	c.SnapshotAsync = defaultSnapshotAsync
	c.TxRegossipFrequency.Duration = defaultTxRegossipFrequency
	c.TxRegossipMaxSize = defaultTxRegossipMaxSize
	c.LogLevel = defaultLogLevel
}

func (d *Duration) UnmarshalJSON(data []byte) (err error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	d.Duration, err = cast.ToDurationE(v)
	return err
}
