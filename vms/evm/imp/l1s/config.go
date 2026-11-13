// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/zap"

	_ "github.com/ava-labs/avalanchego/database/leveldb" // For [leveldb.Name] comment resolution
	_ "github.com/ava-labs/avalanchego/database/memdb"   // For [memdb.Name] comment resolution

	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"

	legacyparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

// config is the operator-supplied per-chain config for the L1 VM.
//
// TODO: Support [saeconfig.Config.StateSyncEnabled] and
// [saeconfig.Config.PriceTarget], which are accepted but ignored.
type config struct {
	saeconfig.Config

	// FeeRecipient is paid the fees of blocks built by this node, if the chain
	// allows custom fee recipients.
	FeeRecipient common.Address `json:"fee-recipient"`

	// UseStandaloneDatabase, if true, stores chain data in a database opened by
	// the VM instead of the node's database. It is ignored if the node's
	// database already holds the chain.
	UseStandaloneDatabase bool `json:"use-standalone-database"`
	// DatabaseConfig is the JSON config of the standalone database.
	DatabaseConfig json.RawMessage `json:"database-config"`
	// DatabasePath is the parent directory of the standalone database, which is
	// stored in DatabasePath/DatabaseType.
	DatabasePath string `json:"database-path"`
	// DatabaseType is [leveldb.Name], [memdb.Name] or [pebbledb.Name].
	DatabaseType string `json:"database-type"`

	deprecatedConfig
}

// apiValidator is the [rpc.API] of the `validator` namespace, which is served
// by the L1 VM.
const apiValidator rpc.API = "validator"

// defaultConfig returns the defaults for the chain with data directory
// chainDataDir. Values mirror the legacy
// `graft/subnet-evm/plugin/evm/config.NewDefaultConfig` for the active
// subset.
func defaultConfig(chainDataDir string) config {
	c := config{
		Config:                saeconfig.Default(),
		FeeRecipient:          blackhole.Address,
		UseStandaloneDatabase: true,
		DatabasePath:          filepath.Join(chainDataDir, "db"),
		DatabaseType:          pebbledb.Name,
	}
	c.APIs.Add(apiValidator)
	return c
}

// parseConfig unmarshals operator-supplied per-chain config bytes on top of
// [defaultConfig], warning about unrecognized options, and validates the
// result for the network of snowCtx.
func parseConfig(snowCtx *snow.Context, b []byte) (config, error) {
	c := defaultConfig(snowCtx.ChainDataDir)
	if err := saeconfig.Parse(snowCtx.Log, b, &c); err != nil {
		return config{}, err
	}
	if err := c.applyDeprecated(snowCtx.Log, b); err != nil {
		return config{}, err
	}
	if err := c.sae().Verify(snowCtx.NetworkID); err != nil {
		return config{}, err
	}
	return c, nil
}

// sae returns the [saeconfig.Config] embedded in c, without the [rpc.API]s
// served by the L1 VM.
func (c config) sae() saeconfig.Config {
	s := c.Config
	s.APIs = set.NewSet[rpc.API](c.APIs.Len())
	for api := range c.APIs {
		if api != apiValidator {
			s.APIs.Add(api)
		}
	}
	return s
}

// saeConfig translates the operator-supplied [config] into the [sae.Config]
// consumed by [sae.NewVM].
func (c config) saeConfig(now func() time.Time) sae.Config {
	return c.sae().SAEConfig(now)
}

// desired returns c's user-facing targets as internal excess votes.
func (c config) desired() desiredParams {
	var d desiredParams
	if c.MinDelayTarget != nil {
		e := dynamic.DesiredDelayExponent(*c.MinDelayTarget)
		d.delayExcess = &e
	}
	if c.GasTarget != nil {
		e := gas.Gas(dynamic.DesiredTargetExponent(*c.GasTarget))
		d.targetExcess = &e
	}
	return d
}

// feeRecipient returns [config.FeeRecipient], warning if it is
// [blackhole.Address] but chainConfig allows custom fee recipients.
func (c config) feeRecipient(chainConfig *legacyparams.ChainConfig, log logging.Logger) common.Address {
	if c.FeeRecipient != blackhole.Address {
		return c.FeeRecipient
	}
	if reason, custom := chainAllowsCustomFeeRecipient(chainConfig); custom {
		log.Warn(`burning fees of blocks built by this node, although the chain allows claiming them; set "fee-recipient" to claim them`,
			zap.String("reason", reason),
		)
	}
	return c.FeeRecipient
}

// chainAllowsCustomFeeRecipient reports whether the chain config (genesis
// + upgrades) permits a node to stamp a custom fee recipient
// into `header.Coinbase`. Returns a short human-readable reason when it
// does (for log fields).
func chainAllowsCustomFeeRecipient(chainConfig *legacyparams.ChainConfig) (reason string, custom bool) {
	configExtra := legacyparams.GetExtra(chainConfig)
	if configExtra.AllowFeeRecipients {
		return "AllowFeeRecipients=true", true
	}
	if _, ok := configExtra.GenesisPrecompiles[rewardmanager.ConfigKey]; ok {
		return "rewardmanager precompile configured at genesis", true
	}
	for _, upgrade := range configExtra.PrecompileUpgrades {
		if upgrade.Key() == rewardmanager.ConfigKey {
			return "rewardmanager precompile scheduled in PrecompileUpgrades", true
		}
	}
	return "", false
}
