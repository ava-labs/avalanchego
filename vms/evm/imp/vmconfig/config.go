// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package vmconfig holds the operator-supplied node configuration shared by the
// C-Chain and L1 VMs.
package vmconfig

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
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

// Config is the operator-supplied node configuration shared by the C-Chain and
// L1 VMs, which embed it in their own config.
type Config struct {
	// State & trie
	Pruning           bool   `json:"pruning-enabled"` // If enabled, trie roots are only persisted every commit-interval blocks.
	CommitInterval    uint64 `json:"commit-interval"` // HashDB: blocks between trie persistence. Firewood: max unpersisted revisions.
	TrieCleanCache    uint64 `json:"trie-clean-cache"`
	SnapshotCache     uint64 `json:"snapshot-cache"`
	AllowMissingTries bool   `json:"allow-missing-tries"` // If enabled, checks preventing an incomplete trie index are skipped.
	StateScheme       string `json:"state-scheme"`
	StateHistory      uint64 `json:"state-history"` // Number of recent states to guarantee. Defaults to core.TriesInMemory.

	// Transaction pool
	LocalTxsEnabled    bool   `json:"local-txs-enabled"`
	TxPoolAccountSlots uint64 `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64 `json:"tx-pool-global-slots"`

	// APIs is the exhaustive set of JSON-RPC APIs this node serves. Methods of
	// any other API return "method not found".
	APIs                set.Set[rpc.API] `json:"apis"`
	AllowUnprotectedTxs bool             `json:"allow-unprotected-txs"` // required for deterministic-address deployments.
	// BatchRequestLimit is the maximum number of requests per JSON-RPC batch;
	// 0 = no limit. An unset config uses the default (1000).
	BatchRequestLimit uint64 `json:"batch-request-limit"`
	// APIMaxDuration limits how long each call to the /rpc and /ws endpoints
	// runs. Non-positive values result in no limit. Defaults to no limit.
	APIMaxDuration duration `json:"api-max-duration"`
	// MaxBlocksPerRequest is the maximum block range (inclusive) that can be
	// used in an eth_getLogs request.
	MaxBlocksPerRequest          uint64 `json:"api-max-blocks-per-request"`
	ResolvePendingToLastExecuted bool   `json:"api-resolve-pending-to-last-executed"`

	internalConfig
}

// internalConfig holds undocumented, test-only options, kept out of config.md.
// Don't set these unless you know what you're doing.
type internalConfig struct {
	// State sync
	StateSyncIDs set.Set[ids.NodeID] `json:"state-sync-ids"`
}

// Default returns the config used when an operator leaves a field unset.
func Default() Config {
	return Config{
		Pruning:            true,
		CommitInterval:     saedb.DefaultCommitInterval,
		TrieCleanCache:     saedb.DefaultTrieCacheSizeMiB,
		SnapshotCache:      saedb.DefaultSnapshotCacheSizeMiB,
		TxPoolAccountSlots: legacypool.DefaultConfig.AccountSlots,
		TxPoolGlobalSlots:  legacypool.DefaultConfig.GlobalSlots,
		APIs:               rpc.DefaultAPIs(),
		BatchRequestLimit:  1000, // matches geth / libevm's node.DefaultConfig
	}
}

// Parse parses b as a JSON-encoded T into c, which SHOULD hold T's defaults,
// and returns the top-level keys of b. This should be preferred over
// [json.Unmarshal] because it warns about, and ignores, keys that don't
// unmarshal into a field of T.
func Parse[T any](log logging.Logger, b []byte, c *T) (map[string]json.RawMessage, error) {
	if len(b) == 0 {
		return nil, nil
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return nil, fmt.Errorf("json.Unmarshal(%T): %w", keys, err)
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("json.Unmarshal(%T): %w", c, err)
	}

	configKeys := jsonKeys[T]()
	var unrecognized []string
	for key := range keys {
		if !configKeys.Contains(key) {
			unrecognized = append(unrecognized, key)
		}
	}
	if len(unrecognized) > 0 {
		slices.Sort(unrecognized)
		log.Warn("ignoring unrecognized config options",
			zap.Strings("options", unrecognized),
		)
	}
	return keys, nil
}

// Verify returns an error if c is invalid.
func (c Config) Verify() error {
	saeCfg := c.SAEConfig(nil)
	if err := saeCfg.RPCConfig.Verify(); err != nil {
		return err
	}
	return saeCfg.DBConfig.Verify()
}

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

// SAEConfig translates the operator-supplied [Config] into the [sae.Config]
// consumed by [sae.NewVM].
func (c Config) SAEConfig(now func() time.Time) sae.Config {
	mempoolConfig := legacypool.DefaultConfig
	mempoolConfig.NoLocals = !c.LocalTxsEnabled
	mempoolConfig.Journal = "" // Disable journaling
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
			StateHistory:      c.StateHistory,
		},
		RPCConfig: rpc.Config{
			APIs:                c.APIs,
			AllowUnprotectedTxs: c.AllowUnprotectedTxs,
			BatchRequestLimit:   c.BatchRequestLimit,
			CallTimeout:         c.APIMaxDuration.Duration,
			MaxBlocksPerRequest: c.MaxBlocksPerRequest,
			// GasCap and TxFeeCap are set to reasonable values. They are left
			// unconfigurable to minimize the size of the user config.
			GasCap:                       50_000_000,
			TxFeeCap:                     100,
			ResolvePendingToLastExecuted: c.ResolvePendingToLastExecuted,
		},
		Now: now,
	}
}

func (c Config) NetworkOptions() []network.Option {
	return []network.Option{
		network.WithAllowedTrackedPeers(c.StateSyncIDs),
	}
}
