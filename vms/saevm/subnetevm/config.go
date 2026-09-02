// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/spf13/cast"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

// config is the operator-supplied per-chain config for the SAE subnet-evm
// VM. Field names and JSON tags match the legacy
// `graft/subnet-evm/plugin/evm/config.Config` schema for the subset this VM
// supports, so existing operator config blobs keep working for those fields.
//
// Unknown fields are REJECTED by [parseConfig] so legacy-only knobs (e.g.
// gossip tuning, `state-sync-enabled`, `use-standalone-database`,
// `offline-pruning-*`, `admin-api-enabled`) surface as an obvious error
// rather than silently no-opping. This is the trade-off for a "narrow but
// compatible" surface; if a previously-rejected legacy field becomes
// meaningful in SAE, add it here.
type config struct {
	// MinDelayTarget is the minimum delay between blocks (in milliseconds)
	// that this node will attempt to use when creating blocks. If this
	// config is not specified, the node will default to use the parent
	// block's target delay per second.
	MinDelayTarget *uint64 `json:"min-delay-target,omitempty"`

	// GasTarget is the target gas per second that this node will attempt
	// to use when creating blocks. If this config is not specified, the
	// node will default to use the parent block's target gas per second.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`

	// API resource limits. Mirror legacy Subnet-EVM where equivalents exist.
	RPCGasCap                    uint64   `json:"rpc-gas-cap"`
	RPCTxFeeCap                  float64  `json:"rpc-tx-fee-cap"`
	APIMaxDuration               duration `json:"api-max-duration"`
	BatchRequestLimit            uint64   `json:"batch-request-limit"`
	ResolvePendingToLastExecuted bool     `json:"api-resolve-pending-to-last-executed"`

	// LocalTxsEnabled mirrors the legacy `local-txs-enabled` flag.
	// When false (default), the legacypool runs with `NoLocals=true`.
	// The on-disk transaction journal is disabled either way, matching
	// the legacy plugin.
	LocalTxsEnabled bool `json:"local-txs-enabled"`

	// PruningEnabled mirrors the legacy `pruning-enabled` flag (defaults
	// to true). When false, the SAE state DB runs in archival mode
	// (every state is persisted to disk; `saedb.Config.Archival=true`).
	PruningEnabled bool `json:"pruning-enabled"`

	// CommitInterval mirrors the legacy `commit-interval` flag: the number
	// of blocks between persistent commits of the state trie to disk.
	CommitInterval uint64 `json:"commit-interval"`

	// Mempool (txpool) settings. JSON tags match legacy `tx-pool-*`.
	TxPoolPriceLimit   uint64   `json:"tx-pool-price-limit"`
	TxPoolPriceBump    uint64   `json:"tx-pool-price-bump"`
	TxPoolAccountSlots uint64   `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64   `json:"tx-pool-global-slots"`
	TxPoolAccountQueue uint64   `json:"tx-pool-account-queue"`
	TxPoolGlobalQueue  uint64   `json:"tx-pool-global-queue"`
	TxPoolLifetime     duration `json:"tx-pool-lifetime"`

	// FeeRecipient is the local node's preferred fee recipient when the
	// network allows fee recipients (`AllowFeeRecipients=true` or
	// rewardmanager `allowFeeRecipients()`). Must be empty (=> burn) or
	// a valid hex address; [parseConfig] rejects any other value.
	FeeRecipient string `json:"feeRecipient"`

	// WarpOffChainMessages encodes off-chain messages (unrelated to any
	// on-chain event ie. block or AddressedCall) that the node should
	// be willing to sign.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`

	// LogLevel mirrors the legacy `log-level` flag. Accepted values are
	// the strings parseable by [graft/evm/log.LvlFromString] (trace,
	// debug, info, warn, error, crit). The default is the empty string,
	// which leaves the process-global libevm logger untouched.
	//   1. reinitializes the libevm global logger to write through
	//      `snowCtx.Log` at this level (libevm-internal logs: EVM
	//      execution, txpool, libevm RPC, ...);
	//   2. calls `snowCtx.Log.SetLevel` so SAE/avalanchego-side Go code
	//      under `vms/saevm` and `vms/saevm/subnetevm` (executor, block
	//      builder, gasprice, ...) follows the same threshold,
	//      overriding whatever avalanchego configured for the chain.
	// Values libevm accepts but avalanchego does not (e.g. "crit") are
	// tolerated for (1) and skipped for (2) with a warning log.
	LogLevel string `json:"log-level"`
}

// duration is a JSON-friendly wrapper around [time.Duration] that accepts
// both numeric (nanoseconds) and string forms ("10m", "1h30s"), matching the
// legacy `graft/subnet-evm/plugin/evm/config.Duration` shape so existing
// operator configs round-trip unchanged. The legacy type is deliberately not
// imported: that package is a deletion target once the legacy plugin
// retires, and coreth keeps an equivalent local copy for the same reason.
type duration struct {
	time.Duration
}

// UnmarshalJSON accepts either a numeric value (nanoseconds) or a
// string parseable by [cast.ToDurationE].
func (d *duration) UnmarshalJSON(data []byte) error {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	parsed, err := cast.ToDurationE(v)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

// MarshalJSON encodes the duration as a string (e.g. "10m0s").
func (d duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// defaultConfig returns the per-chain defaults. Values mirror the legacy
// `graft/subnet-evm/plugin/evm/config.NewDefaultConfig` for the active
// subset.
func defaultConfig() config {
	return config{
		RPCGasCap:                    50_000_000, // 50M gas limit
		RPCTxFeeCap:                  100,        // 100 AVAX
		BatchRequestLimit:            1000,       // matches legacy Subnet-EVM and libevm
		ResolvePendingToLastExecuted: true,
		LocalTxsEnabled:              false, // => NoLocals=true in legacypool
		PruningEnabled:               true,  // => saedb.Config.Archival=false
		CommitInterval:               saedb.DefaultCommitInterval,
		TxPoolPriceLimit:             legacypool.DefaultConfig.PriceLimit,
		TxPoolPriceBump:              legacypool.DefaultConfig.PriceBump,
		TxPoolAccountSlots:           legacypool.DefaultConfig.AccountSlots,
		TxPoolGlobalSlots:            legacypool.DefaultConfig.GlobalSlots,
		TxPoolAccountQueue:           legacypool.DefaultConfig.AccountQueue,
		TxPoolGlobalQueue:            legacypool.DefaultConfig.GlobalQueue,
		// The legacy plugin shortens the pool lifetime from legacypool's 3h
		// default; see graft/subnet-evm/plugin/evm/config/default_config.go.
		TxPoolLifetime: duration{10 * time.Minute},
	}
}

var errInvalidFeeRecipient = errors.New("invalid fee recipient")

// parseConfig unmarshals operator-supplied per-chain config bytes on top of
// [defaultConfig], rejecting unknown fields so legacy-only knobs surface as
// an obvious error, and validates the resulting SAE configuration.
func parseConfig(b []byte) (config, error) {
	c := defaultConfig()
	if len(b) > 0 {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return config{}, fmt.Errorf("json.Unmarshal(%T): %w", c, err)
		}
	}

	if c.FeeRecipient != "" && !common.IsHexAddress(c.FeeRecipient) {
		return config{}, fmt.Errorf("%w: %q is not a valid hex address", errInvalidFeeRecipient, c.FeeRecipient)
	}
	saeCfg := c.saeConfig(nil)
	if err := saeCfg.RPCConfig.Verify(); err != nil {
		return config{}, err
	}
	if err := saeCfg.DBConfig.Verify(); err != nil {
		return config{}, err
	}
	return c, nil
}

// saeConfig translates the operator-supplied [config] into the [sae.Config]
// consumed by [sae.NewVM]. Legacy `state-scheme` / trie-cache-size knobs
// have no SAE equivalent in this VM's config today, so the corresponding
// [saedb.Config] fields take saedb defaults.
func (c config) saeConfig(now func() time.Time) sae.Config {
	mempoolConfig := legacypool.DefaultConfig
	// Disable the on-disk transaction journal, matching the legacy plugin.
	// legacypool's default is the RELATIVE path "transactions.rlp", which
	// would land in the node process's working directory.
	mempoolConfig.Journal = ""
	mempoolConfig.NoLocals = !c.LocalTxsEnabled
	mempoolConfig.PriceLimit = c.TxPoolPriceLimit
	mempoolConfig.PriceBump = c.TxPoolPriceBump
	mempoolConfig.AccountSlots = c.TxPoolAccountSlots
	mempoolConfig.GlobalSlots = c.TxPoolGlobalSlots
	mempoolConfig.AccountQueue = c.TxPoolAccountQueue
	mempoolConfig.GlobalQueue = c.TxPoolGlobalQueue
	mempoolConfig.Lifetime = c.TxPoolLifetime.Duration
	return sae.Config{
		MempoolConfig: mempoolConfig,
		DBConfig: saedb.Config{
			Archival:         !c.PruningEnabled,
			TrieCacheMiB:     saedb.DefaultTrieCacheSizeMiB,
			CommitInterval:   c.CommitInterval,
			SnapshotCacheMiB: saedb.DefaultSnapshotCacheSizeMiB,
		},
		RPCConfig: rpc.Config{
			EVMTimeout:                   c.APIMaxDuration.Duration,
			GasCap:                       c.RPCGasCap,
			BatchRequestLimit:            c.BatchRequestLimit,
			TxFeeCap:                     c.RPCTxFeeCap,
			ResolvePendingToLastExecuted: c.ResolvePendingToLastExecuted,
		},
		Now: now,
	}
}

// desired returns c's user-facing targets as internal excess votes.
func (c config) desired() desiredParams {
	var d desiredParams
	if c.MinDelayTarget != nil {
		e := dynamic.DesiredDelayExponent(*c.MinDelayTarget)
		d.delayExcess = &e
	}
	if c.GasTarget != nil {
		e := acp176.DesiredTargetExcess(*c.GasTarget)
		d.targetExcess = &e
	}
	return d
}

// feeRecipient resolves the local node's preferred fee recipient for
// inclusion in `header.Coinbase` when the chain allows custom fee
// recipients. Empty / unset [config.FeeRecipient] defaults to
// [constants.BlackholeAddr] (explicit burn). If the operator left it unset
// on a chain where fee routing CAN go to a custom address (genesis-flag
// `AllowFeeRecipients=true` or rewardmanager precompile configured anywhere
// in the chain config -- not necessarily activated), log a warning so they
// don't silently burn their fees.
func (c config) feeRecipient(chainConfig *subnetevmparams.ChainConfig, log logging.Logger) common.Address {
	if c.FeeRecipient != "" {
		return common.HexToAddress(c.FeeRecipient)
	}
	if reason, custom := chainAllowsCustomFeeRecipient(chainConfig); custom {
		log.Warn("FeeRecipient is not configured but the chain allows custom fee recipients; this node will burn its block-proposer fees. Set feeRecipient to claim them.",
			zap.String("reason", reason),
		)
	}
	return constants.BlackholeAddr
}

// chainAllowsCustomFeeRecipient reports whether the chain config (genesis
// + upgrades) permits a node to stamp a custom fee recipient
// into `header.Coinbase`. Returns a short human-readable reason when it
// does (for log fields).
func chainAllowsCustomFeeRecipient(chainConfig *subnetevmparams.ChainConfig) (reason string, custom bool) {
	configExtra := subnetevmparams.GetExtra(chainConfig)
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
