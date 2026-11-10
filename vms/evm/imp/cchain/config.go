// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"fmt"
	"time"

	"github.com/ava-labs/libevm/core/txpool/legacypool"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
	"github.com/ava-labs/avalanchego/vms/evm/statesync"
)

// config is the operator-supplied node configuration for the C-Chain, decoded
// from the configBytes passed to [VM.Initialize].
//
// TODO(JonathanOppenheimer) enable and wire all remaining configs
type config struct {
	saeconfig.Config

	// Block-building targets
	// PriceTarget is the minimum gas price (in aAVAX) this node enforces when
	// building blocks under ACP-283. nil means follow the parent block.
	PriceTarget *gas.Price `json:"min-price-target,omitempty"`

	// State & trie
	TrieCleanCache    uint64 `json:"trie-clean-cache"`
	SnapshotCache     uint64 `json:"snapshot-cache"`
	AllowMissingTries bool   `json:"allow-missing-tries"` // If enabled, checks preventing an incomplete trie index are skipped.
	// PopulateMissingTries *uint64 `json:"populate-missing-tries,omitempty"`
	// OfflinePruning       bool    `json:"offline-pruning-enabled"`
	StateScheme  string `json:"state-scheme"`
	StateHistory uint64 `json:"state-history"` // Number of recent states to guarantee. Defaults to core.TriesInMemory.

	// APIs is the exhaustive set of JSON-RPC APIs this node serves. Methods of
	// any other API return "method not found".
	APIs                set.Set[rpc.API] `json:"apis"`
	AllowUnprotectedTxs bool             `json:"allow-unprotected-txs"` // required for deterministic-address deployments.
	// MaxBlocksPerRequest is the maximum block range (inclusive) that can be
	// used in an eth_getLogs request.
	MaxBlocksPerRequest uint64 `json:"api-max-blocks-per-request"`

	// State sync
	StateSyncEnabled bool `json:"state-sync-enabled"`

	internalConfig
}

// internalConfig holds undocumented, test-only options, kept out of config.md.
// Don't set these unless you know what you're doing.
type internalConfig struct {
	// State sync
	StateSyncIDs set.Set[ids.NodeID] `json:"state-sync-ids"`
}

// defaultConfig returns the config used when an operator leaves a field unset.
func defaultConfig() config {
	return config{
		Config:           saeconfig.Default(),
		StateSyncEnabled: true,
		TrieCleanCache:   saedb.DefaultTrieCacheSizeMiB,
		SnapshotCache:    saedb.DefaultSnapshotCacheSizeMiB,
		APIs:             rpc.DefaultAPIs(),
	}
}

var errProductionCommitInterval = fmt.Errorf("production networks must use the commit interval %d", saedb.DefaultCommitInterval)

// parseConfig parses b as a JSON-encoded [config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values. Options
// that need the operator's attention are logged as warnings.
func parseConfig(snowCtx *snow.Context, b []byte) (config, error) {
	c := defaultConfig()
	if err := saeconfig.Parse(snowCtx.Log, b, &c); err != nil {
		return config{}, err
	}

	saeCfg := c.saeConfig(nil)
	if err := saeCfg.RPCConfig.Verify(); err != nil {
		return config{}, err
	}
	if err := saeCfg.DBConfig.Verify(); err != nil {
		return config{}, err
	}
	ci := saeCfg.DBConfig.CommitInterval
	if constants.ProductionNetworkIDs.Contains(snowCtx.NetworkID) &&
		c.StateScheme != firewood.Scheme &&
		ci != saedb.DefaultCommitInterval {
		return config{}, fmt.Errorf("%w: commit interval %d", errProductionCommitInterval, ci)
	}
	return c, nil
}

// saeConfig translates the operator-supplied [config] into the [sae.Config]
// consumed by [sae.NewVM].
func (c config) saeConfig(now func() time.Time) sae.Config {
	cfg := sae.Config{
		MempoolConfig: legacypool.DefaultConfig,
		DBConfig: saedb.Config{
			Scheme:            c.StateScheme,
			TrieCacheMiB:      c.TrieCleanCache,
			SnapshotCacheMiB:  c.SnapshotCache,
			AllowMissingTries: c.AllowMissingTries,
			StateHistory:      c.StateHistory,
		},
		RPCConfig: rpc.Config{
			APIs:                c.APIs,
			AllowUnprotectedTxs: c.AllowUnprotectedTxs,
			MaxBlocksPerRequest: c.MaxBlocksPerRequest,
			// GasCap and TxFeeCap are set to reasonable values for mainnet
			// C-Chain. They are left unconfigurable to minimize the size of the
			// user config.
			GasCap:   50_000_000,
			TxFeeCap: 100,
		},
		Now: now,
	}
	c.Config.Apply(&cfg)
	return cfg
}

func (c config) stateSyncConfig(networkID uint32) statesync.Config {
	saeCfg := c.saeConfig(nil)
	// All nodes in production networks MUST agree which state summaries to
	// serve to ensure the state sync engine can hit the required quorum to
	// accept the summaries.
	if constants.ProductionNetworkIDs.Contains(networkID) {
		saeCfg.DBConfig.CommitInterval = saedb.DefaultCommitInterval
	}
	return statesync.Config{
		DBConfig: saeCfg.DBConfig,
		Enabled:  c.StateSyncEnabled,
	}
}

func (c config) networkOptions() []network.Option {
	return []network.Option{
		network.WithAllowedTrackedPeers(c.StateSyncIDs),
	}
}

// desiredParams bundles this node's votes for the dynamic consensus
// parameters. A nil field means no vote.
type desiredParams struct {
	targetExponent *dynamic.TargetExponent
	priceExponent  *dynamic.PriceExponent
	delayExponent  *dynamic.DelayExponent
}

// desired returns c's user-facing targets as internal exponent votes.
func (c config) desired() desiredParams {
	var d desiredParams
	if c.GasTarget != nil {
		e := dynamic.DesiredTargetExponent(*c.GasTarget)
		d.targetExponent = &e
	}
	if c.PriceTarget != nil {
		e := dynamic.DesiredPriceExponent(*c.PriceTarget)
		d.priceExponent = &e
	}
	if c.MinDelayTarget != nil {
		e := dynamic.DesiredDelayExponent(*c.MinDelayTarget)
		d.delayExponent = &e
	}
	return d
}
