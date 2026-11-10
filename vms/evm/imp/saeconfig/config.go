// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saeconfig holds the operator config shared by the SAE-based VMs.
package saeconfig

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
)

// Duration is a [time.Duration] that JSON-unmarshals from a duration string
// parsed by [time.ParseDuration] (e.g. "10s", "2h45m"). Valid units are "ns",
// "us", "ms", "s", "m" and "h".
type Duration struct {
	time.Duration
}

var _ json.Marshaler = (*Duration)(nil)

// UnmarshalJSON parses a JSON duration string into d.
func (d *Duration) UnmarshalJSON(b []byte) error {
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

// MarshalJSON encodes d as a duration string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// Config is the operator config shared by the SAE-based VMs.
type Config struct {
	// Block-building targets
	// GasTarget is the target gas per second this node votes for when building
	// blocks under ACP-176. nil means follow the parent block.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`
	// MinDelayTarget is the ACP-226 minimum block delay (ms) this node votes
	// for; nil follows the parent.
	MinDelayTarget *uint64 `json:"min-delay-target,omitempty"`

	// State & trie
	Pruning        bool   `json:"pruning-enabled"` // If enabled, trie roots are only persisted every commit-interval blocks.
	CommitInterval uint64 `json:"commit-interval"` // HashDB: blocks between trie persistence. Firewood: max unpersisted revisions.

	// Transaction pool
	LocalTxsEnabled    bool   `json:"local-txs-enabled"`
	TxPoolAccountSlots uint64 `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64 `json:"tx-pool-global-slots"`

	// BatchRequestLimit is the maximum number of requests per JSON-RPC batch;
	// 0 = no limit. An unset config uses the default (1000).
	BatchRequestLimit uint64 `json:"batch-request-limit"`
	// APIMaxDuration limits how long each call to the /rpc and /ws endpoints
	// runs. Non-positive values result in no limit. Defaults to no limit.
	APIMaxDuration               Duration `json:"api-max-duration"`
	ResolvePendingToLastExecuted bool     `json:"api-resolve-pending-to-last-executed"`

	// Warp
	// WarpOffChainMessages encodes messages that the node is willing to sign.
	// These messages don't need to correspond to any on-chain events.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`
}

// Default returns the config used when an operator leaves a field unset.
func Default() Config {
	return Config{
		Pruning:                      true,
		CommitInterval:               saedb.DefaultCommitInterval,
		TxPoolAccountSlots:           legacypool.DefaultConfig.AccountSlots,
		TxPoolGlobalSlots:            legacypool.DefaultConfig.GlobalSlots,
		BatchRequestLimit:            1000, // matches geth / libevm's node.DefaultConfig
		ResolvePendingToLastExecuted: true, // support Foundry's cast and geth/libevm's bound contracts
	}
}

// Apply overwrites the fields of sc that c configures.
func (c Config) Apply(sc *sae.Config) {
	sc.MempoolConfig.NoLocals = !c.LocalTxsEnabled
	sc.MempoolConfig.AccountSlots = c.TxPoolAccountSlots
	sc.MempoolConfig.GlobalSlots = c.TxPoolGlobalSlots
	sc.DBConfig.Archival = !c.Pruning
	sc.DBConfig.CommitInterval = c.CommitInterval
	sc.RPCConfig.BatchRequestLimit = c.BatchRequestLimit
	sc.RPCConfig.CallTimeout = c.APIMaxDuration.Duration
	sc.RPCConfig.ResolvePendingToLastExecuted = c.ResolvePendingToLastExecuted
}

// Parse JSON-unmarshals b into c, leaving c unmodified if b is empty. It warns
// about, and ignores, keys that don't unmarshal into a field of T.
func Parse[T any](log logging.Logger, b []byte, c *T) error {
	if len(b) == 0 {
		return nil
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %w", keys, err)
	}
	if err := json.Unmarshal(b, c); err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %w", c, err)
	}

	known := jsonKeys[T]()
	var unrecognized []string
	for key := range keys {
		if !known.Contains(key) {
			unrecognized = append(unrecognized, key)
		}
	}
	if len(unrecognized) > 0 {
		slices.Sort(unrecognized)
		log.Warn("ignoring unrecognized config options",
			zap.Strings("options", unrecognized),
		)
	}
	return nil
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
