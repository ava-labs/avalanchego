// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"github.com/ava-labs/avalanchego/codec"
)

func Parse(b []byte, c codec.Manager) (Block, error) {
	var blk Block
	if _, err := c.Unmarshal(b, &blk); err != nil {
		return nil, err
	}

	return blk, blk.initialize(b)
}
