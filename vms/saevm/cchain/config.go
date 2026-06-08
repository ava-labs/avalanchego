// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// Config is the JSON configuration for the cchain VM.
type Config struct {
	// PriceTarget is the minimum gas price (in aAVAX) this node enforces when
	// building blocks under ACP-283. nil means follow the parent block.
	PriceTarget *gas.Price `json:"min-price-target,omitempty"`

	// TODO(powerslider): add DelayTarget *uint64 (ACP-226) when wiring lands.
	// TODO(powerslider): add GasTarget *gas.Gas (ACP-176) when wiring lands.
}

// ParseConfig decodes b into a [Config]. An empty input yields the zero Config.
func ParseConfig(b []byte) (Config, error) {
	var c Config
	if len(b) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("unmarshalling %T: %w", c, err)
	}
	return c, nil
}
