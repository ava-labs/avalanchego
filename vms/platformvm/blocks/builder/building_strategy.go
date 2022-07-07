// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

// buildingStrategy defines how to create a versioned block.
// Blocks have different specifications/building instructions as defined by the
// fork that the block exists in.
type buildingStrategy interface {
	// only check whether next block could be built
	hasContent() (bool, error)

	// builds a versioned snowman.Block
	build() (snowman.Block, error)
}

// Factory method that returns the correct building strategy for the
// current fork.
func (b *blockBuilder) getBuildingStrategy() (buildingStrategy, error) {
	preferred, err := b.Preferred()
	if err != nil {
		return nil, err
	}
	preferredDecision, ok := preferred.(stateful.Decision)
	if !ok {
		// The preferred block should always be a decision block
		return nil, fmt.Errorf("expected Decision block but got %T", preferred)
	}
	preferredState := preferredDecision.OnAccept()

	// select transactions to include and finally build the block
	blkVersion := b.blkManager.ExpectedChildVersion(preferred)
	prefBlkID := preferred.ID()
	nextHeight := preferred.Height() + 1

	switch blkVersion {
	case stateless.ApricotVersion:
		return &apricotStrategy{
			blockBuilder: b,
			parentBlkID:  prefBlkID,
			parentState:  preferredState,
			height:       nextHeight,
		}, nil
	case stateless.BlueberryVersion:
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
