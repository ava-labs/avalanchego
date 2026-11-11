// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"
)

// parseConfig parses b as a JSON-encoded [saeconfig.Config]. This should be
// preferred over [json.Unmarshal] because it correctly populates default
// values. Options that need the operator's attention are logged as warnings.
func parseConfig(snowCtx *snow.Context, b []byte) (saeconfig.Config, error) {
	c := saeconfig.Default()
	if err := saeconfig.Parse(snowCtx.Log, b, &c); err != nil {
		return saeconfig.Config{}, err
	}
	if err := c.Verify(snowCtx.NetworkID); err != nil {
		return saeconfig.Config{}, err
	}
	return c, nil
}

// desiredParams bundles this node's votes for the dynamic consensus
// parameters. A nil field means no vote.
type desiredParams struct {
	targetExponent *dynamic.TargetExponent
	priceExponent  *dynamic.PriceExponent
	delayExponent  *dynamic.DelayExponent
}

// desired returns c's user-facing targets as internal exponent votes.
func desired(c saeconfig.Config) desiredParams {
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
