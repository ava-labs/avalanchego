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
	// builds a versioned snowman.Block
	build() (snowman.Block, error)
}

// Factory method that returns the correct building strategy for the
// current fork.
func getBuildingStrategy(b *blockBuilder) (buildingStrategy, error) {
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
	blkVersion := preferred.ExpectedChildVersion()
	prefBlkID := preferred.ID()
	nextHeight := preferred.Height() + 1
	txes, blkTime, err := b.nextTxs(preferredState, blkVersion)

	switch blkVersion {
	case stateless.ApricotVersion:
		return &apricotStrategy{
			b:           b,
			parentBlkID: prefBlkID,
			height:      nextHeight,
			txes:        txes,
		}, nil
	case stateless.BlueberryVersion:
		return &blueberryStrategy{
			b:           b,
			blkTime:     blkTime,
			parentBlkID: prefBlkID,
			height:      nextHeight,
			txes:        txes,
		}, nil
	default:
		return nil, fmt.Errorf("unsupporrted block version %d", blkVersion)
	}
}
