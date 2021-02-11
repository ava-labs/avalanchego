// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "github.com/ava-labs/coreth/eth"

// CommandLineConfig ...
type CommandLineConfig struct {
	// Coreth APIs
	SnowmanAPIEnabled     bool `json:"snowman-api-enabled"`
	CorethAdminAPIEnabled bool `json:"coreth-admin-api-enabled"`
	NetAPIEnabled         bool `json:"net-api-enabled"`

	// Coreth API Gas/Price Caps
	RPCGasCap   uint64  `json:"rpc-gas-cap"`
	RPCTxFeeCap float64 `json:"rpc-tx-fee-cap"`

	// Eth APIs
	EthAPIEnabled      bool `json:"eth-api-enabled"`
	PersonalAPIEnabled bool `json:"personal-api-enabled"`
	TxPoolAPIEnabled   bool `json:"tx-pool-api-enabled"`
	DebugAPIEnabled    bool `json:"debug-api-enabled"`
	Web3APIEnabled     bool `json:"web3-api-enabled"`

	APIMaxDuration      int64 `json:"api-max-duration"`
	MaxBlocksPerRequest int64 `json:"api-max-blocks-per-request"`

	ParsingError error
}

// EthAPIs returns an array of strings representing the Eth APIs that should be enabled
func (c CommandLineConfig) EthAPIs() []string {
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

func (c CommandLineConfig) EthBackendSettings() eth.Settings {
	return eth.Settings{MaxBlocksPerRequest: c.MaxBlocksPerRequest}
}
