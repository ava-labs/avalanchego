package evm

// CommandLineConfig ...
type CommandLineConfig struct {
	// Coreth APIs
	SnowmanAPIEnabled     bool `json:"snowmanAPIEnabled"`
	Web3APIEnabled        bool `json:"web3APIEnabled"`
	CorethAdminAPIEnabled bool `json:"corethAdminAPIEnabled"`

	// Coreth API Gas/Price Caps
	RPCGasCap   uint64  `json:"rpcGasCap"`
	RPCTxFeeCap float64 `json:"rpcTxFeeCap"`

	// Eth APIs
	EthAPIEnabled      bool `json:"ethAPIEnabled"`
	PersonalAPIEnabled bool `json:"personalAPIEnabled"`
	TxPoolAPIEnabled   bool `json:"txPoolAPIEnabled"`
	DebugAPIEnabled    bool `json:"debugAPIEnabled"`
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
