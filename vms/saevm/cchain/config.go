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
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
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
	// GasTarget      *gas.Gas   `json:"gas-target,omitempty"`
	// MinDelayTarget *uint64    `json:"min-delay-target,omitempty"`

	// State & trie
	Pruning        bool   `json:"pruning-enabled"` // If enabled, trie roots are only persisted every commit-interval blocks.
	CommitInterval uint64 `json:"commit-interval"` // Commit interval at which to persist the state trie; 0 uses the default (4096).
	// TrieCleanCache       int     `json:"trie-clean-cache"`
	// SnapshotCache        int     `json:"snapshot-cache"`
	// AllowMissingTries    bool    `json:"allow-missing-tries"`
	// PopulateMissingTries *uint64 `json:"populate-missing-tries,omitempty"`
	// OfflinePruning       bool    `json:"offline-pruning-enabled"`
	// StateScheme          string  `json:"state-scheme"`

	// Transaction pool
	LocalTxsEnabled    bool   `json:"local-txs-enabled"`
	TxPoolAccountSlots uint64 `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64 `json:"tx-pool-global-slots"`

	// APIs
	// MaxBlocksPerRequest int64  `json:"api-max-blocks-per-request"`
	// AllowUnprotectedTxs bool   `json:"allow-unprotected-txs"`
	// BatchRequestLimit   uint64 `json:"batch-request-limit"`

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
		TxPoolAccountSlots: legacypool.DefaultConfig.AccountSlots,
		TxPoolGlobalSlots:  legacypool.DefaultConfig.GlobalSlots,
	}
}

// parseConfig parses b as a JSON-encoded [config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values.
func parseConfig(b []byte) (config, error) {
	c := defaultConfig()
	if len(b) == 0 {
		return c, nil
	}

	if err := json.Unmarshal(b, &c); err != nil {
		return config{}, fmt.Errorf("json.Unmarshal(%T): %w", c, err)
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
			TrieDBConfig:       triedb.HashDefaults,
			Archival:           !c.Pruning,
			TrieCommitInterval: c.CommitInterval,
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
	priceExponent *dynamic.PriceExponent
}

// desired returns c's user-facing targets as internal exponent votes.
func (c config) desired() desiredParams {
	var d desiredParams
	if c.PriceTarget != nil {
		e := dynamic.DesiredPriceExponent(*c.PriceTarget)
		d.priceExponent = &e
	}
	return d
}
