// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/version"
)

func Parse(b []byte, c codec.Manager) (Block, error) {
	var blk Block
	if _, err := c.Unmarshal(b, &blk); err != nil {
		return nil, err
	}

	switch blk.(type) {
	case *BlueberryAbortBlock,
		*BlueberryCommitBlock,
		*BlueberryProposalBlock,
		*BlueberryStandardBlock:
		return blk, blk.initialize(version.BlueberryBlockVersion, b)

	case *ApricotAbortBlock,
		*ApricotCommitBlock,
		*ApricotProposalBlock,
		*ApricotStandardBlock,
		*AtomicBlock:
		return blk, blk.initialize(version.ApricotBlockVersion, b)

	default:
		return nil, fmt.Errorf("unhandled block type %T", blk)
	}
}
