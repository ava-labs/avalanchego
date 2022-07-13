// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import "github.com/ava-labs/avalanchego/codec"

func Parse(b []byte, c codec.Manager) (Block, error) {
	var blk Block
	v, err := c.Unmarshal(b, &blk)
	if err != nil {
		return nil, err
	}

	return blk, blk.Initialize(v, b)
}
