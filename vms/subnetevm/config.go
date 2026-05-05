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
	"github.com/ava-labs/libevm/triedb"
	"github.com/spf13/cast"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	avawarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// Config is the operator-supplied per-chain config for the SAE
// subnet-evm VM. Field names and JSON tags are kept compatible with
// the legacy `graft/subnet-evm/plugin/evm/config.Config` schema
// (subset that maps cleanly to SAE) so existing operator config blobs
// keep working.
//
// Unknown fields are REJECTED by [ParseConfig] so legacy-only knobs
// (pruning, gossip, state-sync, standalone database, log-level, etc.)
// surface as an obvious error rather than silently no-opping. This is
// the trade-off for a "narrow but compatible" surface; if/when a
// previously-deferred legacy field becomes meaningful in SAE, add it
// here.
type Config struct {
	// MinDelayTarget is the minimum delay between blocks (in milliseconds)
	// that this node will attempt to use when creating blocks. If this
	// config is not specified, the node will default to use the parent
	// block's target delay per second.
	MinDelayTarget *uint64 `json:"min-delay-target,omitempty"`

	// GasTarget is the target gas per second that this node will attempt
	// to use when creating blocks. If this config is not specified, the
	// node will default to use the parent block's target gas per second.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`

	// API gas/price caps. Mirror legacy `rpc-gas-cap` / `rpc-tx-fee-cap`.
	RPCGasCap   uint64  `json:"rpc-gas-cap"`
	RPCTxFeeCap float64 `json:"rpc-tx-fee-cap"`

	// LocalTxsEnabled mirrors the legacy `local-txs-enabled` flag.
	// When false (default), the legacypool runs with `NoLocals=true`
	// and the on-disk transaction journal is disabled.
	LocalTxsEnabled bool `json:"local-txs-enabled"`

	// PruningEnabled mirrors the legacy `pruning-enabled` flag (defaults
	// to true). When false, the SAE state DB runs in archival mode
	// (every state is persisted to disk; `saedb.Config.Archival=true`).
	PruningEnabled bool `json:"pruning-enabled"`

	// CommitInterval mirrors the legacy `commit-interval` flag and
	// drives `saedb.Config.TrieCommitInterval`: the number of blocks
	// between persistent commits of the state trie to disk.
	CommitInterval uint64 `json:"commit-interval"`

	// Mempool (txpool) settings. JSON tags match legacy `tx-pool-*`.
	TxPoolPriceLimit   uint64   `json:"tx-pool-price-limit"`
	TxPoolPriceBump    uint64   `json:"tx-pool-price-bump"`
	TxPoolAccountSlots uint64   `json:"tx-pool-account-slots"`
	TxPoolGlobalSlots  uint64   `json:"tx-pool-global-slots"`
	TxPoolAccountQueue uint64   `json:"tx-pool-account-queue"`
	TxPoolGlobalQueue  uint64   `json:"tx-pool-global-queue"`
	TxPoolLifetime     Duration `json:"tx-pool-lifetime"`

	// FeeRecipient is the local node's preferred fee recipient when the
	// network allows fee recipients (`AllowFeeRecipients=true` or
	// rewardmanager `allowFeeRecipients()`). Must be empty (=> burn) or
	// a valid hex address; [ParseConfig] rejects any other value.
	FeeRecipient string `json:"feeRecipient"`

	// WarpOffChainMessages encodes off-chain messages (unrelated to any
	// on-chain event ie. block or AddressedCall) that the node should
	// be willing to sign.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`
}

// Duration is a JSON-friendly wrapper around [time.Duration] that
// accepts both numeric (nanoseconds) and string forms ("10m", "1h30s"),
// matching the legacy `graft/subnet-evm/plugin/evm/config.Duration`
// shape so existing operator configs round-trip unchanged.
type Duration struct {
	time.Duration
}

// UnmarshalJSON accepts either a numeric value (nanoseconds) or a
// string parseable by [cast.ToDurationE].
func (d *Duration) UnmarshalJSON(data []byte) error {
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
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// DefaultConfig returns the per-chain defaults. Values mirror the
// legacy `graft/subnet-evm/plugin/evm/config.NewDefaultConfig` for
// the active subset.
func DefaultConfig() Config {
	return Config{
		RPCGasCap:          50_000_000, // 50M gas limit
		RPCTxFeeCap:        100,        // 100 AVAX
		LocalTxsEnabled:    false,      // => NoLocals=true in legacypool
		PruningEnabled:     true,       // => saedb.Config.Archival=false
		CommitInterval:     4096,       // matches saedb's defaultCommitInterval
		TxPoolPriceLimit:   1,
		TxPoolPriceBump:    10,
		TxPoolAccountSlots: 16,
		TxPoolGlobalSlots:  4096 + 1024, // urgent + floating queue, 4:1 ratio
		TxPoolAccountQueue: 64,
		TxPoolGlobalQueue:  1024,
		TxPoolLifetime:     Duration{10 * time.Minute},
	}
}

var errInvalidFeeRecipient = errors.New("invalid fee recipient")

// ParseConfig unmarshals operator-supplied per-chain config bytes on
// top of [DefaultConfig], rejecting unknown fields so legacy-only
// knobs (pruning, gossip, state-sync, standalone database, log-level,
// etc.) surface as an obvious error.
func ParseConfig(b []byte) (Config, error) {
	c := DefaultConfig()
	if len(b) == 0 {
		return c, nil
	}

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("json.Unmarshal(%T): %w", c, err)
	}

	if c.FeeRecipient != "" && !common.IsHexAddress(c.FeeRecipient) {
		return Config{}, fmt.Errorf("%w: %q is not a valid hex address", errInvalidFeeRecipient, c.FeeRecipient)
	}
	return c, nil
}

// toMempoolConfig translates the operator-facing tx-pool fields into
// the libevm [legacypool.Config] consumed by the SAE VM. Defaults
// outside the operator-visible subset (Journal, Rejournal) come from
// [legacypool.DefaultConfig]; `NoLocals` is the inverse of
// `LocalTxsEnabled`.
func (c Config) toMempoolConfig() legacypool.Config {
	mp := legacypool.DefaultConfig
	mp.NoLocals = !c.LocalTxsEnabled
	mp.PriceLimit = c.TxPoolPriceLimit
	mp.PriceBump = c.TxPoolPriceBump
	mp.AccountSlots = c.TxPoolAccountSlots
	mp.GlobalSlots = c.TxPoolGlobalSlots
	mp.AccountQueue = c.TxPoolAccountQueue
	mp.GlobalQueue = c.TxPoolGlobalQueue
	mp.Lifetime = c.TxPoolLifetime.Duration
	return mp
}

// toRPCConfig translates the operator-facing RPC fields into the SAE
// [rpc.Config] consumed by the SAE VM.
func (c Config) toRPCConfig() rpc.Config {
	return rpc.Config{
		GasCap:   c.RPCGasCap,
		TxFeeCap: c.RPCTxFeeCap,
	}
}

// toDBConfig returns the SAE [saedb.Config] used by the SAE VM. The
// `*triedb.Config` is always the daemon-default `triedb.HashDefaults`
// (legacy `state-scheme` / trie-cache-size knobs have no SAE
// equivalent today). `Archival` is the inverse of [Config.PruningEnabled];
// `TrieCommitInterval` is taken verbatim from [Config.CommitInterval].
func (c Config) toDBConfig() saedb.Config {
	return saedb.Config{
		TrieDBConfig:       triedb.HashDefaults,
		Archival:           !c.PruningEnabled,
		TrieCommitInterval: c.CommitInterval,
	}
}

var errParsingWarpMessage = errors.New("parsing warp message")

func (c Config) WarpMessages() ([]*avawarp.UnsignedMessage, error) {
	msgs := make([]*avawarp.UnsignedMessage, len(c.WarpOffChainMessages))
	for i, bytes := range c.WarpOffChainMessages {
		msg, err := avawarp.ParseUnsignedMessage(bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: at index %d: %w", errParsingWarpMessage, i, err)
		}
		msgs[i] = msg
	}
	return msgs, nil
}
