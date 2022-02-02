// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import "github.com/ava-labs/avalanchego/snow/consensus/snowman"

// WrappingBlock represents the functionalities HeightIndexer needs
// out of a PostForkBlock
type WrappingBlock interface {
	snowman.Block

	GetInnerBlk() snowman.Block
}
