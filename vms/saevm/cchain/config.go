// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

type config struct {
	// GasTarget is the target gas per second that this node will attempt to use
	// when creating blocks. If this config is not specified, the node will
	// default to use the parent block's target gas per second.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`

	// DelayTarget is the minimum delay between blocks (in milliseconds) that
	// this node will attempt to use when creating blocks. If this config is not
	// specified, the node will default to use the parent block's delay per
	// second.
	DelayTarget *uint64 `json:"min-delay-target,omitempty"`

	// PriceTarget is the minimum gas price (in aAVAX) that this node will
	// attempt to enforce when creating blocks. If this config is not specified,
	// the node will default to use the parent block's minimum gas price.
	PriceTarget *gas.Price `json:"min-price-target,omitempty"`

	// Pruning encodes whether the node should prune intermediate trie nodes.
	Pruning bool `json:"pruning-enabled"`

	// WarpOffChainMessages encodes off-chain messages (unrelated to any
	// on-chain event ie. block or AddressedCall) that the node is willing to
	// sign.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`
}

// parseConfig parses b as a JSON-encoded [config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values.
func parseConfig(b []byte) (config, error) {
	c := config{
		Pruning: true,
	}
	if len(b) == 0 {
		return c, nil
	}

	if err := json.Unmarshal(b, &c); err != nil {
		return config{}, fmt.Errorf("json.Unmarshal(%T): %w", c, err)
	}
	return c, nil
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
