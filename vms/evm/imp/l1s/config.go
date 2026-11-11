// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"
	"github.com/ava-labs/avalanchego/vms/evm/sae"

	legacyparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

// config is the operator-supplied per-chain config for the SAE L1 VM. It extends
// the C-Chain config. JSON tags of the added fields match the legacy
// `graft/subnet-evm/plugin/evm/config.Config` schema, so existing operator
// config blobs keep working for those fields.
//
// TODO: Support [saeconfig.Config.StateSyncEnabled] and
// [saeconfig.Config.PriceTarget], which are accepted but ignored.
type config struct {
	saeconfig.Config

	// API resource limits. Mirror the legacy VM where equivalents exist.
	RPCGasCap   uint64  `json:"rpc-gas-cap"`
	RPCTxFeeCap float64 `json:"rpc-tx-fee-cap"`

	// Mempool (txpool) settings. JSON tags match legacy `tx-pool-*`.
	TxPoolPriceLimit   uint64             `json:"tx-pool-price-limit"`
	TxPoolPriceBump    uint64             `json:"tx-pool-price-bump"`
	TxPoolAccountQueue uint64             `json:"tx-pool-account-queue"`
	TxPoolGlobalQueue  uint64             `json:"tx-pool-global-queue"`
	TxPoolLifetime     saeconfig.Duration `json:"tx-pool-lifetime"`

	// FeeRecipient is the local node's preferred fee recipient when the
	// network allows fee recipients (`AllowFeeRecipients=true` or
	// rewardmanager `allowFeeRecipients()`). Must be empty (=> burn) or
	// a valid hex address; [parseConfig] rejects any other value.
	FeeRecipient string `json:"feeRecipient"`
}

// defaultConfig returns the per-chain defaults. Values mirror the legacy
// `graft/subnet-evm/plugin/evm/config.NewDefaultConfig` for the active
// subset.
func defaultConfig() config {
	return config{
		Config:             saeconfig.Default(),
		RPCGasCap:          50_000_000, // 50M gas limit
		RPCTxFeeCap:        100,        // 100 AVAX
		TxPoolPriceLimit:   legacypool.DefaultConfig.PriceLimit,
		TxPoolPriceBump:    legacypool.DefaultConfig.PriceBump,
		TxPoolAccountQueue: legacypool.DefaultConfig.AccountQueue,
		TxPoolGlobalQueue:  legacypool.DefaultConfig.GlobalQueue,
		// The legacy plugin shortens the pool lifetime from legacypool's 3h
		// default; see graft/subnet-evm/plugin/evm/config/default_config.go.
		TxPoolLifetime: saeconfig.Duration{Duration: 10 * time.Minute},
	}
}

var errInvalidFeeRecipient = errors.New("invalid fee recipient")

// parseConfig unmarshals operator-supplied per-chain config bytes on top of
// [defaultConfig], warning about unrecognized options, and validates the
// result for the network of snowCtx.
func parseConfig(snowCtx *snow.Context, b []byte) (config, error) {
	c := defaultConfig()
	if err := saeconfig.Parse(snowCtx.Log, b, &c); err != nil {
		return config{}, err
	}

	if c.FeeRecipient != "" && !common.IsHexAddress(c.FeeRecipient) {
		return config{}, fmt.Errorf("%w: %q is not a valid hex address", errInvalidFeeRecipient, c.FeeRecipient)
	}
	if err := c.Verify(snowCtx.NetworkID); err != nil {
		return config{}, err
	}
	return c, nil
}

// saeConfig translates the operator-supplied [config] into the [sae.Config]
// consumed by [sae.NewVM].
func (c config) saeConfig(now func() time.Time) sae.Config {
	config := c.SAEConfig(now)
	// The legacy plugin never journals local transactions.
	config.MempoolConfig.Journal = ""
	config.MempoolConfig.PriceLimit = c.TxPoolPriceLimit
	config.MempoolConfig.PriceBump = c.TxPoolPriceBump
	config.MempoolConfig.AccountQueue = c.TxPoolAccountQueue
	config.MempoolConfig.GlobalQueue = c.TxPoolGlobalQueue
	config.MempoolConfig.Lifetime = c.TxPoolLifetime.Duration
	config.RPCConfig.GasCap = c.RPCGasCap
	config.RPCConfig.TxFeeCap = c.RPCTxFeeCap
	return config
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

// feeRecipient resolves the local node's preferred fee recipient for
// inclusion in `header.Coinbase` when the chain allows custom fee
// recipients. Empty / unset [config.FeeRecipient] defaults to
// [blackhole.Address] (explicit burn). If the operator left it unset
// on a chain where fee routing CAN go to a custom address (genesis-flag
// `AllowFeeRecipients=true` or rewardmanager precompile configured anywhere
// in the chain config -- not necessarily activated), log a warning so they
// don't silently burn their fees.
func (c config) feeRecipient(chainConfig *legacyparams.ChainConfig, log logging.Logger) common.Address {
	if c.FeeRecipient != "" {
		return common.HexToAddress(c.FeeRecipient)
	}
	if reason, custom := chainAllowsCustomFeeRecipient(chainConfig); custom {
		log.Warn("FeeRecipient is not configured but the chain allows custom fee recipients; this node will burn its block-proposer fees. Set feeRecipient to claim them.",
			zap.String("reason", reason),
		)
	}
	return blackhole.Address
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
