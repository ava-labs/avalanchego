// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

func Parse(b []byte) (CommonBlockIntf, error) {
	var blk CommonBlockIntf
	v, err := Codec.Unmarshal(b, &blk)
	if err != nil {
		return nil, err
	}

	return blk, blk.Initialize(v, b)
}
