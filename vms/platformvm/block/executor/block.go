// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block             = (*Block)(nil)
	_ snowman.OracleBlock       = (*Block)(nil)
	_ smblock.WithVerifyContext = (*Block)(nil)
)

// Exported for testing in platformvm package.
type Block struct {
	platform.Block
	manager *manager
}

func (*Block) ShouldVerifyWithContext(context.Context) (bool, error) {
	return true, nil
}

func (b *Block) VerifyWithContext(ctx context.Context, blockContext *smblock.Context) error {
	blkID := b.ID()
	blkState, previouslyExecuted := b.manager.blkIDToState[blkID]
	warpAlreadyVerified := previouslyExecuted && blkState.verifiedHeights.Contains(blockContext.PChainHeight)

	// If the chain is bootstrapped and the warp messages haven't been verified,
	// we must verify them.
	if !warpAlreadyVerified && b.manager.txExecutorBackend.Bootstrapped.Get() {
		err := VerifyWarpMessages(
			ctx,
			b.manager.ctx.NetworkID,
			b.manager.ctx.ValidatorState,
			blockContext.PChainHeight,
			b,
		)
		if err != nil {
			return err
		}
	}

	if err := b.verifyBlockSizePreHelicon(); err != nil {
		return err
	}

	// If the block was previously executed, we don't need to execute it again,
	// we can just mark that the warp messages are valid at this height.
	if previouslyExecuted {
		blkState.verifiedHeights.Add(blockContext.PChainHeight)
		return nil
	}

	// Since this is the first time we are verifying this block, we must execute
	// the state transitions to generate the state diffs.
	return b.Visit(&verifier{
		backend:           b.manager.backend,
		txExecutorBackend: b.manager.txExecutorBackend,
		pChainHeight:      blockContext.PChainHeight,
	})
}

func (b *Block) Verify(ctx context.Context) error {
	return b.VerifyWithContext(
		ctx,
		&smblock.Context{
			PChainHeight: 0,
		},
	)
}

func (b *Block) Accept(context.Context) error {
	return b.Visit(b.manager.acceptor)
}

func (b *Block) Reject(context.Context) error {
	return b.Visit(b.manager.rejector)
}

func (b *Block) Timestamp() time.Time {
	return b.manager.getTimestamp(b.ID())
}

func (b *Block) Options(context.Context) ([2]snowman.Block, error) {
	options := options{
		log:                     b.manager.ctx.Log,
		primaryUptimePercentage: b.manager.txExecutorBackend.Config.UptimePercentage,
		upgradeConfig:           b.manager.txExecutorBackend.Config.UpgradeConfig,
		uptimes:                 b.manager.txExecutorBackend.Uptimes,
		state:                   b.manager.backend.state,
	}
	if err := b.Block.Visit(&options); err != nil {
		return [2]snowman.Block{}, err
	}

	return [2]snowman.Block{
		b.manager.NewBlock(options.preferredBlock),
		b.manager.NewBlock(options.alternateBlock),
	}, nil
}

func (b *Block) verifyBlockSizePreHelicon() error {
	// Gate on the block's own timestamp, not b.Timestamp(): that method
	// falls back to node-local chain time (the last-accepted block's
	// timestamp) until this block is tracked in blkIDToState, which is not
	// yet the case on a block's first verification. Since this is a
	// consensus rule, it must not depend on node-local state: two nodes
	// verifying the same block for the first time could otherwise disagree
	// depending on their own progress through the chain, rather than on the
	// block itself. Pre-Banff blocks have no stored timestamp and
	// necessarily predate Helicon, so falling back to the zero value
	// correctly selects the pre-Helicon limit for them.
	var blkTime time.Time
	if banffBlk, ok := b.Block.(platform.BanffBlock); ok {
		blkTime = banffBlk.Timestamp()
	}
	if !b.manager.txExecutorBackend.Config.UpgradeConfig.IsHeliconActivated(blkTime) {
		blockSize := len(b.Bytes())
		if blockSize > codec.DefaultMaxSize {
			b.manager.ctx.Log.Debug("block verification failed, block too big",
				zap.Int("blockSize", blockSize), zap.Int("maxBlockSize", codec.DefaultMaxSize))
			return ErrBlockTooBigPreHelicon
		}
	}

	return nil
}
