// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"

	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// Config allows the node operator to configure the C-Chain VM.
type Config struct {
	// WarpOffChainMessages encodes off-chain messages (unrelated to any
	// on-chain event ie. block or AddressedCall) that the node is willing to
	// sign.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`
}

// ParseConfig parses b as a JSON-encoded [Config]. This should be preferred
// over [json.Unmarshal] because it correctly populates default values.
func ParseConfig(b []byte) (Config, error) {
	var c Config
	if len(b) == 0 {
		return c, nil
	}

	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("json.Unmarshal(%T): %w", c, err)
	}
	return c, nil
}

var errParsingWarpMessage = errors.New("parsing warp message")

// WarpMessages parses and returns the messages encoded in
// [Config.WarpOffChainMessages].
func (c Config) WarpMessages() ([]*warp.UnsignedMessage, error) {
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
