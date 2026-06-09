// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"

	avawarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

type Config struct {
	// WarpOffChainMessages encodes off-chain messages (unrelated to any
	// on-chain event ie. block or AddressedCall) that the node should be
	// willing to sign.
	WarpOffChainMessages []hexutil.Bytes `json:"warp-off-chain-messages"`
}

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
