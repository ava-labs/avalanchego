// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"time"

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
	const (
		maxBackoff  = 5 * time.Second
		initBackoff = 10 * time.Millisecond
	)

	for curWait := initBackoff; ; curWait = backoff(ctx, curWait, maxBackoff) {
		if ctx.Err() != nil {
			b.log.Debug("Context cancelled, stopping block building", zap.Error(ctx.Err()))
			return nil, false
		}

		err := b.incomingBlock(ctx)
		if err != nil {
			b.log.Debug("Error waiting for incoming block", zap.Error(err))
			curWait = backoff(ctx, curWait, maxBackoff)
			continue
		}
		vmBlock, err := b.vm.BuildBlock(ctx)
		if err != nil {
			b.log.Info("Error building block", zap.Error(err))
			curWait = backoff(ctx, curWait, maxBackoff)
			continue
		}
		simplexBlock, err := newBlock(metadata, vmBlock, b.blockTracker)
		if err != nil {
			b.log.Error("Error creating simplex block from built block", zap.Error(err))
			return nil, false
		}
		curWait = initBackoff // Reset backoff after a successful block build
		verifiedBlock, err := simplexBlock.Verify(ctx)
		if err != nil {
			b.log.Warn("Error verifying block we built ourselves", zap.Error(err))
			curWait = backoff(ctx, curWait, maxBackoff)
			continue
		}

		return verifiedBlock, true
	}
}

// IncomingBlock blocks until a new block is ready to be built from the VM, or until the
// context is cancelled.
func (b *BlockBuilder) IncomingBlock(ctx context.Context) {
	err := b.incomingBlock(ctx)
	if err != nil {
		b.log.Debug("Error waiting for incoming block", zap.Error(err))
	}
}

func (b *BlockBuilder) incomingBlock(ctx context.Context) error {
	for {
		msg, err := b.vm.WaitForEvent(ctx)
		if err != nil {
			return err
		}
		if msg == common.PendingTxs {
			return nil
		}
		b.log.Warn("Received unexpected message", zap.Stringer("message", msg))
	}
}

// backoff waits for `backoff` duration before returning the next backoff duration.
// It doubles the backoff duration each time it is called, up to a maximum of `maxBackoff`.
func backoff(ctx context.Context, backoff, maxBackoff time.Duration) time.Duration {
	select {
	case <-ctx.Done():
		return 0
	case <-time.After(backoff):
	}

	return min(maxBackoff, 2*backoff) // exponential backoff
}
