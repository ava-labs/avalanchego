// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexes

import "github.com/ava-labs/avalanchego/snow/consensus/snowman"

var _ WrappingBlock = &TestWrappingBlock{}

// TestBlock is a useful test block
type TestWrappingBlock struct {
	*snowman.TestBlock
	innerBlk *snowman.TestBlock
}

func (twb *TestWrappingBlock) GetInnerBlk() snowman.Block {
	return twb.innerBlk
}
