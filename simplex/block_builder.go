// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"

	"github.com/ava-labs/simplex"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type BlockBuilder struct {
	log          logging.Logger
	vm           block.ChainVM
	blockTracker *blockTracker
}

// BuildBlock continuously tries to build a block until the context is cancelled. If there are no blocks to be built, it will wait for an event from the VM.
// It returns false if the context was cancelled, otherwise it returns the built block and true.
func (b *BlockBuilder) BuildBlock(ctx context.Context, metadata simplex.ProtocolMetadata) (simplex.VerifiedBlock, bool) {
	for {
		select {
		case <-ctx.Done():
			b.log.Info("Context cancelled, stopping block building")
			return nil, false
		default:
			err := b.incomingBlock(ctx)
			if err != nil {
				b.log.Error("Error waiting for incoming block:", zap.Error(err))
				continue
			}
			vmBlock, err := b.vm.BuildBlock(ctx)
			if err != nil {
				b.log.Error("Error building block:", zap.Error(err))
				continue
			}
			simplexBlock, err := newBlock(metadata, vmBlock, b.blockTracker)
			if err != nil {
				b.log.Error("Error creating simplex block:", zap.Error(err))
				continue
			}

			verifiedBlock, err := simplexBlock.Verify(context.Background())
			if err != nil {
				b.log.Error("Error verifying block we built ourselves: %s", zap.Error(err))
				continue
			}

			return verifiedBlock, true
		}
	}
}

// IncomingBlock blocks until a new block is ready to be built from the VM, or until the
// context is cancelled.
func (b *BlockBuilder) IncomingBlock(ctx context.Context) {
	err := b.incomingBlock(ctx)
	if err != nil {
		b.log.Error("Error waiting for incoming block:", zap.Error(err))
	}
}

func (b *BlockBuilder) incomingBlock(ctx context.Context) error {
	for {
		msg, err := b.vm.WaitForEvent(ctx)
		if err != nil {
			return err
		}
		if msg == common.PendingTxs {
			b.log.Info("Received pending transactions")
			return nil
		}
		b.log.Warn("Received unexpected message", zap.String("message", msg.String()))
	}
}
