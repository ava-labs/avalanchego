// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmconfig"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
	"github.com/ava-labs/avalanchego/vms/evm/statesync"
)

// config is the operator-supplied node configuration for the C-Chain, decoded
// from the configBytes passed to [VM.Initialize].
type config struct {
	vmconfig.Config

	// Block building

	// PriceTarget is the minimum gas price (in aAVAX) this node enforces when
	// building blocks under ACP-283. nil means follow the parent block.
	PriceTarget *gas.Price `json:"min-price-target,omitempty"`
	// GasTarget is the target gas per second this node votes for when building
	// blocks under ACP-176. nil means follow the parent block.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`
}

// defaultConfig returns the config used when an operator leaves a field unset.
func defaultConfig() config {
	c := config{
		Config: vmconfig.Default(),
	}
	c.StateSyncEnabled = true
	return c
}

var errProductionCommitInterval = fmt.Errorf("production networks must use the commit interval %d", saedb.DefaultCommitInterval)

// parseConfig parses b as a JSON-encoded [config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values. Options
// that need the operator's attention are logged as warnings.
func parseConfig(snowCtx *snow.Context, b []byte) (config, error) {
	c := defaultConfig()
	if _, err := vmconfig.Parse(snowCtx.Log, b, &c); err != nil {
		return config{}, err
	}
	ci := c.CommitInterval
	if constants.ProductionNetworkIDs.Contains(snowCtx.NetworkID) &&
		c.StateScheme != firewood.Scheme &&
		ci != saedb.DefaultCommitInterval {
		return config{}, fmt.Errorf("%w: commit interval %d", errProductionCommitInterval, ci)
	}
	return c, nil
}

func (c config) stateSyncConfig(networkID uint32) statesync.Config {
	dbCfg := c.SAEConfig(nil).DBConfig
	summaryInterval := dbCfg.CommitInterval
	// All nodes in production networks MUST agree which state summaries to
	// serve to ensure the state sync engine can hit the required quorum to
	// accept the summaries.
	if constants.ProductionNetworkIDs.Contains(networkID) {
		summaryInterval = saedb.DefaultCommitInterval
	}
	return statesync.Config{
		DBConfig:        dbCfg,
		SummaryInterval: summaryInterval,
		Enabled:         c.StateSyncEnabled,
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
