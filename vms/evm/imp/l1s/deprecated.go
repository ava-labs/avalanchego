// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

// This file temporarily accepts deprecated Subnet-EVM options so that existing
// operator configs continue to work across the SAE transition.
//
// TODO: delete this file in the next release after the SAE transition.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

var errUnknownLegacyEthAPI = errors.New(`unknown "eth-apis" name`)

// deprecatedConfig holds the Subnet-EVM options that [config] still accepts. A
// nil field is unset.
type deprecatedConfig struct {
	LegacyFeeRecipient   *string  `json:"feeRecipient,omitempty"`
	EthAPIs              []string `json:"eth-apis,omitempty"`
	ValidatorsAPIEnabled *bool    `json:"validators-api-enabled,omitempty"`
	WarpAPIEnabled       *bool    `json:"warp-api-enabled,omitempty"`
	AdminAPIEnabled      *bool    `json:"admin-api-enabled,omitempty"`
	DatabaseConfigFile   *string  `json:"database-config-file,omitempty"`
}

// legacyEthAPIs maps each name that Subnet-EVM's `eth-apis` option accepted to
// the [rpc.API]s that serve the same methods. A nil set means that the methods
// no longer exist.
var legacyEthAPIs = map[string]set.Set[rpc.API]{
	"admin":                nil, // the admin namespace no longer exists
	"debug":                nil, // eth's debug service (e.g. debug_dumpBlock) no longer exists
	"debug-file-tracer":    set.Of(rpc.APITrace),
	"debug-handler":        set.Of(rpc.APIProfile),
	"debug-tracer":         set.Of(rpc.APITrace),
	"eth":                  nil, // eth_etherbase and eth_coinbase no longer exist
	"eth-filter":           set.Of(rpc.APISubscription),
	"internal-account":     nil, // eth_accounts no longer exists
	"internal-blockchain":  set.Of(rpc.APIChain, rpc.APIAvalanche),
	"internal-debug":       set.Of(rpc.APIDB),
	"internal-eth":         set.Of(rpc.APIPrice, rpc.APIAvalanche),
	"internal-personal":    nil, // the personal namespace no longer exists
	"internal-transaction": set.Of(rpc.APITx),
	"internal-tx-pool":     set.Of(rpc.APITxPool),
	"net":                  set.Of(rpc.APINet),
	"web3":                 set.Of(rpc.APIWeb3),

	// Pre geth v1.10.20 names, which Subnet-EVM still accepted.
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

// applyDeprecated sets the replacements of the deprecated options in c, then
// clears them. b is the JSON config that c was parsed from. A replacement set
// in b takes precedence over the deprecated option.
func (c *config) applyDeprecated(log logging.Logger, b []byte) error {
	if len(b) == 0 {
		return nil
	}
	d := c.deprecatedConfig
	c.deprecatedConfig = deprecatedConfig{}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %w", keys, err)
	}
	_, feeRecipientSet := keys["fee-recipient"]
	c.applyLegacyFeeRecipient(log, d.LegacyFeeRecipient, feeRecipientSet)
	if err := c.applyLegacyDatabaseConfig(log, d.DatabaseConfigFile); err != nil {
		return err
	}
	_, apisSet := keys["apis"]
	return c.applyLegacyAPIs(log, d, apisSet)
}

// applyLegacyFeeRecipient sets [config.FeeRecipient] from legacy, unless legacy
// is nil or empty, or feeRecipientSet. Like Subnet-EVM, an invalid legacy
// address burns fees.
func (c *config) applyLegacyFeeRecipient(log logging.Logger, legacy *string, feeRecipientSet bool) {
	switch {
	case legacy == nil || *legacy == "":
	case feeRecipientSet:
		log.Warn(`ignoring deprecated "feeRecipient" because "fee-recipient" is set; "feeRecipient" will be removed in the next release`)
	case !common.IsHexAddress(*legacy):
		log.Warn(`ignoring deprecated "feeRecipient" because it isn't a hex address; fees will be burned`,
			zap.String("feeRecipient", *legacy),
		)
	default:
		c.FeeRecipient = common.HexToAddress(*legacy)
		log.Warn(`"feeRecipient" is deprecated and will be removed in the next release; set "fee-recipient" instead`,
			zap.Stringer("fee-recipient", c.FeeRecipient),
		)
	}
}

// applyLegacyDatabaseConfig replaces a base64-encoded JSON string in
// [config.DatabaseConfig] with the bytes it encodes. Then, unless file is nil
// or empty, or [config.DatabaseConfig] is set, it sets [config.DatabaseConfig]
// to the contents of file.
func (c *config) applyLegacyDatabaseConfig(log logging.Logger, file *string) error {
	var encoded string
	if json.Unmarshal(c.DatabaseConfig, &encoded) == nil {
		c.DatabaseConfig = nil
		if encoded != "" {
			b, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return fmt.Errorf(`decoding base64 "database-config": %w`, err)
			}
			c.DatabaseConfig = b
			log.Warn(`base64-encoded "database-config" is deprecated and will be removed in the next release; set it to the decoded JSON instead`)
		}
	}

	switch {
	case file == nil || *file == "":
	case len(c.DatabaseConfig) > 0:
		log.Warn(`ignoring deprecated "database-config-file" because "database-config" is set; "database-config-file" will be removed in the next release`)
	default:
		b, err := os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf(`reading "database-config-file": %w`, err)
		}
		c.DatabaseConfig = b
		log.Warn(`"database-config-file" is deprecated and will be removed in the next release; set "database-config" to the file's JSON instead`,
			zap.String("database-config-file", *file),
		)
	}
	return nil
}

// applyLegacyAPIs sets [saeconfig.Config.APIs] from the deprecated API options
// in d, unless apisSet.
func (c *config) applyLegacyAPIs(log logging.Logger, d deprecatedConfig, apisSet bool) error {
	if d.EthAPIs == nil && d.ValidatorsAPIEnabled == nil && d.WarpAPIEnabled == nil && d.AdminAPIEnabled == nil {
		return nil
	}
	// A config that sets both is the expected migration path: Subnet-EVM reads
	// the deprecated options before the SAE transition and this VM reads
	// "apis" after it.
	if apisSet {
		log.Warn(`ignoring deprecated API options because "apis" is set; they will be removed in the next release`)
		return nil
	}

	var removed []string
	if d.EthAPIs != nil {
		apis := set.Of(apiValidator) // Subnet-EVM's validators-api-enabled default
		for _, name := range d.EthAPIs {
			mapped, ok := legacyEthAPIs[name]
			if !ok {
				return fmt.Errorf("%w: %q", errUnknownLegacyEthAPI, name)
			}
			if mapped.Len() == 0 {
				removed = append(removed, name)
				continue
			}
			apis.Union(mapped)
		}
		c.APIs = apis
	}
	if d.ValidatorsAPIEnabled != nil {
		if *d.ValidatorsAPIEnabled {
			c.APIs.Add(apiValidator)
		} else {
			c.APIs.Remove(apiValidator)
		}
	}
	if d.WarpAPIEnabled != nil && *d.WarpAPIEnabled {
		removed = append(removed, "warp-api-enabled")
	}
	if d.AdminAPIEnabled != nil && *d.AdminAPIEnabled {
		removed = append(removed, "admin-api-enabled")
	}

	names := make([]string, 0, c.APIs.Len())
	for api := range c.APIs {
		names = append(names, string(api))
	}
	slices.Sort(names)
	log.Warn(`deprecated API options will be removed in the next release; set "apis" instead`,
		zap.Strings("apis", names),
	)
	if len(removed) > 0 {
		log.Warn(`ignoring deprecated API options whose methods no longer exist`,
			zap.Strings("options", removed),
		)
	}
	return nil
}
