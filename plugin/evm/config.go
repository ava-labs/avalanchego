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
	defaultEthApiEnabled               = true
	defaultNetApiEnabled               = true
	defaultWeb3ApiEnabled              = true
	defaultRpcGasCap                   = 2500000000 // 25000000 X 100
	defaultRpcTxFeeCap                 = 100        // 100 AVAX
	defaultApiMaxDuration              = 0          // Default to no maximum API Call duration
	defaultMaxBlocksPerRequest         = 0          // Default to no maximum on the number of blocks per getLogs request
	defaultContinuousProfilerFrequency = 15 * time.Minute
	defaultContinuousProfilerMaxFiles  = 5
)

type Duration struct {
	time.Duration
}

// Config ...
type Config struct {
	// Coreth APIs
	SnowmanAPIEnabled     bool `json:"snowman-api-enabled"`
	CorethAdminAPIEnabled bool `json:"coreth-admin-api-enabled"`
	NetAPIEnabled         bool `json:"net-api-enabled"`

	// Continuous Profiler
	ContinuousProfilerDir       string   `json:"continuous-profiler-dir"`       // If set to non-empty string creates a continuous profiler
	ContinuousProfilerFrequency Duration `json:"continuous-profiler-frequency"` // Frequency to run continuous profiler if enabled
	ContinuousProfilerMaxFiles  int      `json:"continuous-profiler-max-files"` // Maximum number of files to maintain

	// Coreth API Gas/Price Caps
	RPCGasCap   uint64  `json:"rpc-gas-cap"`
	RPCTxFeeCap float64 `json:"rpc-tx-fee-cap"`

	// Eth APIs
	EthAPIEnabled      bool `json:"eth-api-enabled"`
	PersonalAPIEnabled bool `json:"personal-api-enabled"`
	TxPoolAPIEnabled   bool `json:"tx-pool-api-enabled"`
	DebugAPIEnabled    bool `json:"debug-api-enabled"`
	Web3APIEnabled     bool `json:"web3-api-enabled"`

	// Eth Settings
	Pruning                 bool     `json:"pruning-enabled"`
	LocalTxsEnabled         bool     `json:"local-txs-enabled"`
	APIMaxDuration          Duration `json:"api-max-duration"`
	MaxBlocksPerRequest     int64    `json:"api-max-blocks-per-request"`
	AllowUnfinalizedQueries bool     `json:"allow-unfinalized-queries"`

	// Keystore Settings
	KeystoreDirectory             string `json:"keystore-directory"` // both absolute and relative supported
	KeystoreExternalSigner        string `json:"keystore-external-signer"`
	KeystoreInsecureUnlockAllowed bool   `json:"keystore-insecure-unlock-allowed"`
}

// EthAPIs returns an array of strings representing the Eth APIs that should be enabled
func (c Config) EthAPIs() []string {
	ethAPIs := make([]string, 0)

	if c.EthAPIEnabled {
		ethAPIs = append(ethAPIs, "eth")
	}
	if c.PersonalAPIEnabled {
		ethAPIs = append(ethAPIs, "personal")
	}
	if c.TxPoolAPIEnabled {
		ethAPIs = append(ethAPIs, "txpool")
	}
	if c.DebugAPIEnabled {
		ethAPIs = append(ethAPIs, "debug")
	}

	return ethAPIs
}

func (c Config) EthBackendSettings() eth.Settings {
	return eth.Settings{MaxBlocksPerRequest: c.MaxBlocksPerRequest}
}

func (c *Config) SetDefaults() {
	c.EthAPIEnabled = defaultEthApiEnabled
	c.NetAPIEnabled = defaultNetApiEnabled
	c.Web3APIEnabled = defaultWeb3ApiEnabled
	c.RPCGasCap = defaultRpcGasCap
	c.RPCTxFeeCap = defaultRpcTxFeeCap
	c.APIMaxDuration.Duration = defaultApiMaxDuration
	c.MaxBlocksPerRequest = defaultMaxBlocksPerRequest
	c.ContinuousProfilerFrequency.Duration = defaultContinuousProfilerFrequency
	c.ContinuousProfilerMaxFiles = defaultContinuousProfilerMaxFiles
}

func (d *Duration) UnmarshalJSON(data []byte) (err error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	d.Duration, err = cast.ToDurationE(v)
	return err
}
