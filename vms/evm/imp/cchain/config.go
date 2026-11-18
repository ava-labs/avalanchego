// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmconfig"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
	"github.com/ava-labs/avalanchego/vms/evm/statesync"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
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

	vmconfig.Config

	// State sync
	StateSyncEnabled bool `json:"state-sync-enabled"`

	// Warp
	// WarpOffChainMessages encodes messages that the node is willing to sign.
	// These messages don't need to correspond to any on-chain events.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`
}

// defaultConfig returns the config used when an operator leaves a field unset.
func defaultConfig() config {
	return config{
		Config:           vmconfig.Default(),
		StateSyncEnabled: true,
	}
}

// parseConfig parses b as a JSON-encoded [config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values. Options
// that need the operator's attention are logged as warnings.
func parseConfig(snowCtx *snow.Context, b []byte) (config, error) {
	c := defaultConfig()
	if err := vmconfig.Parse(snowCtx.Log, b, &c); err != nil {
		return config{}, err
	}
	if err := c.Verify(snowCtx.NetworkID); err != nil {
		return config{}, err
	}
	return c, nil
}

func (c config) stateSyncConfig(networkID uint32) statesync.Config {
	saeCfg := c.SAEConfig(nil)
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
