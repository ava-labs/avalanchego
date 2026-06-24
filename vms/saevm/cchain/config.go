// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

var errInvalidConfig = errors.New("invalid config")

// config is the JSON configuration for the cchain VM.
type config struct {
	// PriceTarget is the minimum gas price (in aAVAX) this node enforces when
	// building blocks under ACP-283. nil means follow the parent block.
	PriceTarget *gas.Price `json:"min-price-target,omitempty"`

	// TODO(powerslider): add DelayTarget *uint64 (ACP-226) when wiring lands.
	// TODO(powerslider): add GasTarget *gas.Gas (ACP-176) when wiring lands.
}

// parseConfig decodes b into a [config]. An empty input yields the zero config.
func parseConfig(b []byte) (config, error) {
	var c config
	if len(b) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return config{}, fmt.Errorf("%w: %w", errInvalidConfig, err)
	}
	return c, nil
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
