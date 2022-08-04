// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/version"
)

// buildingStrategy defines how to create a versioned block.
// Blocks have different specifications/building instructions as defined by the
// network upgrades in effect at the time the block is created.
type buildingStrategy interface {
	// hasContent checks whether next block could be built,
	// without affecting the mempool. Returns true if a new block
	// can be built, error if check could not be performed.
	hasContent() (bool, error)

	// build builds a versioned snowman.Block
	build() (snowman.Block, error)
}

// Factory method that returns the correct building strategy for the
// current fork.
func (b *blockBuilder) getBuildingStrategy() (buildingStrategy, error) {
	// Get the block to build on top of and retrieve the new block's context.
	preferred, err := b.Preferred()
	if err != nil {
		return nil, err
	}
	prefBlkID := preferred.ID()
	nextHeight := preferred.Height() + 1
	blkVersion := b.blkManager.ExpectedChildVersion(preferred)

	stateVersions := b.txExecutorBackend.StateVersions
	preferredState, ok := stateVersions.GetState(prefBlkID)
	if !ok {
		return nil, fmt.Errorf("could not retrieve state for block %s. Preferred block must be a decision block", prefBlkID)
	}

	// select transactions to include and finally build the block
	switch blkVersion {
	case version.ApricotBlockVersion:
		return &apricotStrategy{
			blockBuilder: b,
			parentBlkID:  prefBlkID,
			parentState:  preferredState,
			nextHeight:   nextHeight,
		}, nil
	case version.BlueberryBlockVersion:
		return &blueberryStrategy{
			blockBuilder: b,
			parentBlkID:  prefBlkID,
			parentState:  preferredState,
			height:       nextHeight,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported block version %d", blkVersion)
	}
}
