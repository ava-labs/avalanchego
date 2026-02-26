// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/txpool/legacypool"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

// config is the operator-supplied node configuration for the C-Chain, decoded
// from the configBytes passed to [VM.Initialize].
//
// TODO(JonathanOppenheimer) enable and wire all remaining configs
type config struct {
	// Block-building targets
	// PriceTarget is the minimum gas price (in aAVAX) this node enforces when
	// building blocks under ACP-283. nil means follow the parent block.
	PriceTarget *gas.Price `json:"min-price-target,omitempty"`
	// GasTarget is the target gas per second this node votes for when building
	// blocks under ACP-176. nil means follow the parent block.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`
	// MinDelayTarget is the ACP-226 minimum block delay (ms) this node votes
	// for; nil follows the parent.
	MinDelayTarget *uint64 `json:"min-delay-target,omitempty"`

	// State & trie
	Pruning        bool   `json:"pruning-enabled"` // If enabled, trie roots are only persisted every commit-interval blocks.
	CommitInterval uint64 `json:"commit-interval"` // Commit interval at which to persist the state trie.
	TrieCleanCache uint64 `json:"trie-clean-cache"`
	SnapshotCache  uint64 `json:"snapshot-cache"`
	// AllowMissingTries    bool    `json:"allow-missing-tries"`
	// PopulateMissingTries *uint64 `json:"populate-missing-tries,omitempty"`
	// OfflinePruning       bool    `json:"offline-pruning-enabled"`
	StateScheme string `json:"state-scheme"`

	// Transaction pool
	LocalTxsEnabled    bool   `json:"local-txs-enabled"`
	TxPoolAccountSlots uint64 `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64 `json:"tx-pool-global-slots"`

	// APIs
	// MaxBlocksPerRequest int64  `json:"api-max-blocks-per-request"`
	AllowUnprotectedTxs bool `json:"allow-unprotected-txs"` // required for deterministic-address deployments.
	// BatchRequestLimit is the maximum number of requests per JSON-RPC batch;
	// 0 = no limit. An unset config uses the default (1000).
	BatchRequestLimit uint64 `json:"batch-request-limit"`

	// State sync
	// StateSyncEnabled *bool `json:"state-sync-enabled"`

	// Warp
	// WarpOffChainMessages encodes messages that the node is willing to sign.
	// These messages don't need to correspond to any on-chain events.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`

	// internalConfig
}

// // internalConfig holds undocumented, test-only options, kept out of config.md.
// // Don't set these unless you know what you're doing.
// type internalConfig struct {
// 	// State sync
// 	StateSyncIDs []ids.NodeID `json:"state-sync-ids"`
// }

// defaultConfig returns the config used when an operator leaves a field unset.
func defaultConfig() config {
	return config{
		Pruning:            true,
		CommitInterval:     saedb.DefaultCommitInterval,
		TrieCleanCache:     saedb.DefaultTrieCacheSizeMiB,
		SnapshotCache:      saedb.DefaultSnapshotCacheSizeMiB,
		TxPoolAccountSlots: legacypool.DefaultConfig.AccountSlots,
		TxPoolGlobalSlots:  legacypool.DefaultConfig.GlobalSlots,
		BatchRequestLimit:  1000, // matches geth / libevm's node.DefaultConfig
	}
}

var errProductionCommitInterval = fmt.Errorf("production networks must use the commit interval %d", saedb.DefaultCommitInterval)

// parseConfig parses b as a JSON-encoded [config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values.
func parseConfig(b []byte, networkID uint32) (config, error) {
	c := defaultConfig()
	if len(b) == 0 {
		return c, nil
	}

	if err := json.Unmarshal(b, &c); err != nil {
		return config{}, fmt.Errorf("json.Unmarshal(%T): %w", c, err)
	}
	saeCfg := c.saeConfig(nil)
	if err := saeCfg.RPCConfig.Verify(); err != nil {
		return config{}, err
	}
	if err := saeCfg.DBConfig.Verify(); err != nil {
		return config{}, err
	}
	if ci := saeCfg.DBConfig.CommitInterval; ci != saedb.DefaultCommitInterval &&
		constants.ProductionNetworkIDs.Contains(networkID) {
		return config{}, fmt.Errorf("%w: commit interval %d", errProductionCommitInterval, ci)
	}
	return c, nil
}

// saeConfig translates the operator-supplied [config] into the [sae.Config]
// consumed by [sae.NewVM].
func (c config) saeConfig(now func() time.Time) sae.Config {
	mempoolConfig := legacypool.DefaultConfig
	mempoolConfig.NoLocals = !c.LocalTxsEnabled
	mempoolConfig.AccountSlots = c.TxPoolAccountSlots
	mempoolConfig.GlobalSlots = c.TxPoolGlobalSlots
	return sae.Config{
		MempoolConfig: mempoolConfig,
		DBConfig: saedb.Config{
			Archival:         !c.Pruning,
			Scheme:           c.StateScheme,
			TrieCacheMiB:     c.TrieCleanCache,
			CommitInterval:   c.CommitInterval,
			SnapshotCacheMiB: c.SnapshotCache,
		},
		RPCConfig: rpc.Config{
			AllowUnprotectedTxs: c.AllowUnprotectedTxs,
			BatchRequestLimit:   c.BatchRequestLimit,
		},
		Now: now,
	}
}

var errParsingWarpMessage = errors.New("parsing warp message")

// WarpMessages parses and returns the messages encoded in
// [config.WarpOffChainMessages].
func (c config) WarpMessages() ([]*warp.UnsignedMessage, error) {
	msgs := make([]*warp.UnsignedMessage, len(c.WarpOffChainMessages))
	for i, bytes := range c.WarpOffChainMessages {
		msg, err := warp.ParseUnsignedMessage(bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: at index %d: %w", errParsingWarpMessage, i, err)
		}
		msgs[i] = msg
	}
	return msgs, nil
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
