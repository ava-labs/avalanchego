// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

// This file converts deprecated Subnet-EVM options to their replacements, so
// that existing configs keep working across the SAE transition.
//
// TODO: delete this file in the next release after the SAE transition.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

var errUnknownLegacyEthAPI = errors.New(`unknown "eth-apis" name`)

// deprecatedConfig holds the deprecated options. Each is converted to its
// replacement, unless the replacement is also set.
type deprecatedConfig struct {
	LegacyFeeRecipient   string   `json:"feeRecipient,omitempty"`
	EthAPIs              []string `json:"eth-apis,omitempty"`
	ValidatorsAPIEnabled *bool    `json:"validators-api-enabled,omitempty"`
	WarpAPIEnabled       bool     `json:"warp-api-enabled,omitempty"`
	AdminAPIEnabled      bool     `json:"admin-api-enabled,omitempty"`
	DatabaseConfigFile   string   `json:"database-config-file,omitempty"`
}

// deprecatedOptions are the JSON keys of [deprecatedConfig].
var deprecatedOptions = []string{
	"feeRecipient",
	"eth-apis",
	"validators-api-enabled",
	"warp-api-enabled",
	"admin-api-enabled",
	"database-config-file",
}

// legacyEthAPIs maps each name that `eth-apis` accepted to the [rpc.API]s that
// serve the same methods. A nil set means the methods no longer exist.
var legacyEthAPIs = map[string]set.Set[rpc.API]{
	"admin":                nil,
	"debug":                nil, // e.g. debug_dumpBlock
	"debug-file-tracer":    set.Of(rpc.APITrace),
	"debug-handler":        set.Of(rpc.APIProfile),
	"debug-tracer":         set.Of(rpc.APITrace),
	"eth":                  nil, // eth_etherbase and eth_coinbase
	"eth-filter":           set.Of(rpc.APISubscription),
	"internal-account":     nil, // eth_accounts
	"internal-blockchain":  set.Of(rpc.APIChain, rpc.APIAvalanche),
	"internal-debug":       set.Of(rpc.APIDB),
	"internal-eth":         set.Of(rpc.APIPrice, rpc.APIAvalanche),
	"internal-personal":    nil,
	"internal-transaction": set.Of(rpc.APITx),
	"internal-tx-pool":     set.Of(rpc.APITxPool),
	"net":                  set.Of(rpc.APINet),
	"web3":                 set.Of(rpc.APIWeb3),

	// Names from before geth v1.10.20.
	"internal-public-eth":              set.Of(rpc.APIPrice, rpc.APIAvalanche),
	"internal-public-blockchain":       set.Of(rpc.APIChain, rpc.APIAvalanche),
	"internal-public-transaction-pool": set.Of(rpc.APITx),
	"internal-public-tx-pool":          set.Of(rpc.APITxPool),
	"internal-public-debug":            set.Of(rpc.APIDB),
	"internal-private-debug":           set.Of(rpc.APIDB),
	"internal-public-account":          nil,
	"internal-private-personal":        nil,
	"public-eth":                       nil,
	"public-eth-filter":                set.Of(rpc.APISubscription),
	"private-admin":                    nil,
	"public-debug":                     nil,
	"private-debug":                    nil,
}

// applyDeprecated converts the deprecated options of c, which was parsed from
// b, to their replacements. A replacement set in b takes precedence.
func (c *config) applyDeprecated(log logging.Logger, b []byte) error {
	d := c.deprecatedConfig
	c.deprecatedConfig = deprecatedConfig{}

	var keys map[string]json.RawMessage
	if len(b) > 0 {
		if err := json.Unmarshal(b, &keys); err != nil {
			return fmt.Errorf("json.Unmarshal(%T): %w", keys, err)
		}
	}
	var found []string
	for _, option := range deprecatedOptions {
		if _, ok := keys[option]; ok {
			found = append(found, option)
		}
	}
	if len(found) > 0 {
		log.Warn("deprecated config options will be removed in the next release",
			zap.Strings("options", found),
		)
	}

	if _, ok := keys["fee-recipient"]; !ok && d.LegacyFeeRecipient != "" {
		// Like Subnet-EVM, an invalid address burns fees.
		if common.IsHexAddress(d.LegacyFeeRecipient) {
			c.FeeRecipient = common.HexToAddress(d.LegacyFeeRecipient)
		} else {
			log.Warn(`burning fees because "feeRecipient" isn't an address`,
				zap.String("feeRecipient", d.LegacyFeeRecipient),
			)
		}
	}
	if err := c.applyLegacyDatabaseConfig(log, d.DatabaseConfigFile); err != nil {
		return err
	}
	if _, ok := keys["apis"]; !ok {
		return c.applyLegacyAPIs(log, d)
	}
	return nil
}

// applyLegacyDatabaseConfig decodes a base64-encoded [config.DatabaseConfig].
// If [config.DatabaseConfig] is then unset, it is read from file.
func (c *config) applyLegacyDatabaseConfig(log logging.Logger, file string) error {
	var encoded string
	if json.Unmarshal(c.DatabaseConfig, &encoded) == nil {
		c.DatabaseConfig = nil
		if encoded != "" {
			log.Warn(`base64-encoded "database-config" will be removed in the next release`)
			b, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return fmt.Errorf(`decoding base64 "database-config": %w`, err)
			}
			c.DatabaseConfig = b
		}
	}

	if file == "" || len(c.DatabaseConfig) > 0 {
		return nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf(`reading "database-config-file": %w`, err)
	}
	c.DatabaseConfig = b
	return nil
}

// applyLegacyAPIs sets [saeconfig.Config.APIs] from the API options of d.
func (c *config) applyLegacyAPIs(log logging.Logger, d deprecatedConfig) error {
	var removed []string
	if d.EthAPIs != nil {
		c.APIs = set.Of(validatorAPI) // Subnet-EVM enabled it by default.
		for _, name := range d.EthAPIs {
			apis, ok := legacyEthAPIs[name]
			if !ok {
				return fmt.Errorf("%w: %q", errUnknownLegacyEthAPI, name)
			}
			if apis.Len() == 0 {
				removed = append(removed, name)
			}
			c.APIs.Union(apis)
		}
	}
	if d.ValidatorsAPIEnabled != nil {
		if *d.ValidatorsAPIEnabled {
			c.APIs.Add(validatorAPI)
		} else {
			c.APIs.Remove(validatorAPI)
		}
	}
	if d.WarpAPIEnabled {
		removed = append(removed, "warp-api-enabled")
	}
	if d.AdminAPIEnabled {
		removed = append(removed, "admin-api-enabled")
	}
	if len(removed) > 0 {
		log.Warn("ignoring deprecated API options whose methods no longer exist",
			zap.Strings("options", removed),
		)
	}
	return nil
}
