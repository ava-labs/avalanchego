// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

// duration is a [time.Duration] that JSON-unmarshals from a duration string
// parsed by [time.ParseDuration] (e.g. "10s", "2h45m"). Valid units are "ns",
// "us", "ms", "s", "m" and "h".
type duration struct {
	time.Duration
}

var _ json.Marshaler = (*duration)(nil)

func (d *duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func (d duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

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
	Pruning           bool   `json:"pruning-enabled"` // If enabled, trie roots are only persisted every commit-interval blocks.
	CommitInterval    uint64 `json:"commit-interval"` // Commit interval at which to persist the state trie.
	TrieCleanCache    uint64 `json:"trie-clean-cache"`
	SnapshotCache     uint64 `json:"snapshot-cache"`
	AllowMissingTries bool   `json:"allow-missing-tries"` // If enabled, checks preventing an incomplete trie index are skipped.
	// PopulateMissingTries *uint64 `json:"populate-missing-tries,omitempty"`
	// OfflinePruning       bool    `json:"offline-pruning-enabled"`
	StateScheme string `json:"state-scheme"`

	// Transaction pool
	LocalTxsEnabled    bool   `json:"local-txs-enabled"`
	TxPoolAccountSlots uint64 `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64 `json:"tx-pool-global-slots"`

	// APIs
	// MaxBlocksPerRequest int64  `json:"api-max-blocks-per-request"`

	// APIs is the exhaustive set of JSON-RPC APIs this node serves. Methods of
	// any other API return "method not found".
	APIs                set.Set[rpc.API] `json:"apis"`
	AllowUnprotectedTxs bool             `json:"allow-unprotected-txs"` // required for deterministic-address deployments.
	// BatchRequestLimit is the maximum number of requests per JSON-RPC batch;
	// 0 = no limit. An unset config uses the default (1000).
	BatchRequestLimit uint64 `json:"batch-request-limit"`
	// APIMaxDuration limits how long an eth_call (or eth_callDetailed) runs.
	// Non-positive values result in no limit. Defaults to no limit.
	APIMaxDuration               duration `json:"api-max-duration"`
	ResolvePendingToLastExecuted bool     `json:"api-resolve-pending-to-last-executed"`

	// State sync
	StateSyncEnabled bool `json:"state-sync-enabled"`

	// Warp
	// WarpOffChainMessages encodes messages that the node is willing to sign.
	// These messages don't need to correspond to any on-chain events.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`

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
		Pruning:                      true,
		StateSyncEnabled:             true,
		CommitInterval:               saedb.DefaultCommitInterval,
		TrieCleanCache:               saedb.DefaultTrieCacheSizeMiB,
		SnapshotCache:                saedb.DefaultSnapshotCacheSizeMiB,
		TxPoolAccountSlots:           legacypool.DefaultConfig.AccountSlots,
		TxPoolGlobalSlots:            legacypool.DefaultConfig.GlobalSlots,
		APIs:                         rpc.DefaultAPIs(),
		BatchRequestLimit:            1000, // matches geth / libevm's node.DefaultConfig
		ResolvePendingToLastExecuted: true, // support Foundry's cast and geth/libevm's bound contracts
	}
}

var errProductionCommitInterval = fmt.Errorf("production networks must use the commit interval %d", saedb.DefaultCommitInterval)

// parseConfig parses b as a JSON-encoded [config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values. Options
// that need the operator's attention are logged as warnings.
func parseConfig(snowCtx *snow.Context, b []byte) (config, error) {
	c := defaultConfig()
	if len(b) == 0 {
		return c, nil
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return config{}, fmt.Errorf("json.Unmarshal(%T): %w", keys, err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return config{}, fmt.Errorf("json.Unmarshal(%T): %w", c, err)
	}

	// TODO(JonathanOppenheimer): delete together with deprecated.go.
	if rawNames, ok := keys["eth-apis"]; ok {
		delete(keys, "eth-apis")
		_, apisSet := keys["apis"]
		if err := c.applyDeprecatedAPINames(snowCtx.Log, rawNames, apisSet); err != nil {
			return config{}, err
		}
	}

	var unrecognized []string
	for key := range keys {
		if !configKeys.Contains(key) {
			unrecognized = append(unrecognized, key)
		}
	}
	if len(unrecognized) > 0 {
		slices.Sort(unrecognized)
		snowCtx.Log.Warn("ignoring unrecognized config options",
			zap.Strings("options", unrecognized),
		)
	}

	saeCfg := c.saeConfig(nil)
	if err := saeCfg.RPCConfig.Verify(); err != nil {
		return config{}, err
	}
	if err := saeCfg.DBConfig.Verify(); err != nil {
		return config{}, err
	}
	if ci := saeCfg.DBConfig.CommitInterval; ci != saedb.DefaultCommitInterval &&
		constants.ProductionNetworkIDs.Contains(snowCtx.NetworkID) {
		return config{}, fmt.Errorf("%w: commit interval %d", errProductionCommitInterval, ci)
	}
	return c, nil
}

// configKeys are the JSON keys that unmarshal into a [config] field.
// [parseConfig] warns about, and ignores, all other keys.
var configKeys = jsonKeys[config]()

// jsonKeys returns the key under which [encoding/json] decodes each field of
// struct T, including fields promoted from embedded structs.
func jsonKeys[T any]() set.Set[string] {
	t := reflect.TypeFor[T]()
	fields := reflect.VisibleFields(t)
	keys := set.NewSet[string](len(fields))
	for _, f := range fields {
		if f.Anonymous {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" {
			name = f.Name // an untagged field unmarshals from its own name
		}
		keys.Add(name)
	}
	return keys
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
			Archival:          !c.Pruning,
			Scheme:            c.StateScheme,
			TrieCacheMiB:      c.TrieCleanCache,
			CommitInterval:    c.CommitInterval,
			SnapshotCacheMiB:  c.SnapshotCache,
			AllowMissingTries: c.AllowMissingTries,
		},
		RPCConfig: rpc.Config{
			APIs:                c.APIs,
			AllowUnprotectedTxs: c.AllowUnprotectedTxs,
			BatchRequestLimit:   c.BatchRequestLimit,
			EVMTimeout:          c.APIMaxDuration.Duration,
			// GasCap and TxFeeCap are set to reasonable values for mainnet
			// C-Chain. They are left unconfigurable to minimize the size of the
			// user config.
			GasCap:                       50_000_000,
			TxFeeCap:                     100,
			ResolvePendingToLastExecuted: c.ResolvePendingToLastExecuted,
		},
		Now: now,
	}
}

func (c config) stateSyncConfig() statesync.Config {
	saeCfg := c.saeConfig(nil)
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
