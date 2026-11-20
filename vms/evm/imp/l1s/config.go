// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmconfig"
)

// config is the operator-supplied node configuration for L1s, decoded from the
// configBytes passed to [VM.Initialize].
type config struct {
	vmconfig.Config

	// Block building
	// Needed for allowFeeRecipients. Keeping the camelCase key so existing
	// configs don't break.
	// FeeRecipient string `json:"feeRecipient"`
	// The C-Chain's gas-target and min-price-target are left out until
	// ACP-224 (#4717) decides whether L1s support them.

	// Database
	// UseStandaloneDatabase *PBool `json:"use-standalone-database"`
	// DatabaseConfigContent string `json:"database-config"`
	// DatabaseType          string `json:"database-type"`
	// DatabasePath          string `json:"database-path"`

	deprecatedConfig
}

// defaultConfig returns the config used when an operator leaves a field unset.
func defaultConfig() config {
	return config{
		Config: vmconfig.Default(),
	}
}

// parseConfig parses b as a JSON-encoded [config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values. Options
// that need the operator's attention are logged as warnings.
func parseConfig(snowCtx *snow.Context, b []byte) (config, error) {
	c := defaultConfig()
	keys, err := vmconfig.Parse(snowCtx.Log, b, &c)
	if err != nil {
		return config{}, err
	}

	// TODO(JonathanOppenheimer): delete together with deprecated.go.
	if c.EthAPIs != nil {
		_, apisSet := keys["apis"]
		if err := c.applyDeprecatedAPINames(snowCtx.Log, apisSet); err != nil {
			return config{}, err
		}
	}
	if err := c.Verify(); err != nil {
		return config{}, err
	}
	return c, nil
}
