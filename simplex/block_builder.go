package simplex

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/simplex"

	"go.uber.org/zap"
)

type BlockBuilder struct {
	log          logging.Logger
	vm           block.ChainVM
	blockTracker *blockTracker
}

func (b *BlockBuilder) BuildBlock(ctx context.Context, metadata simplex.ProtocolMetadata) (simplex.VerifiedBlock, bool) {
	b.IncomingBlock(ctx)

	vmBlock, err := b.vm.BuildBlock(ctx)
	if err != nil {
		b.log.Error("Error building block:", zap.Error(err))
		return nil, false
	}

	simplexBlock, err := newBlock(metadata, vmBlock, b.blockTracker)
	if err != nil {
		b.log.Error("Error creating simplex block:", zap.Error(err))
		return nil, false
	}

	verifiedBlock, err := simplexBlock.Verify(context.Background())
	if err != nil {
		b.log.Error("Error verifying block we built ourselves: %s", zap.Error(err))
		return nil, false
	}

	return verifiedBlock, true
}

func (b *BlockBuilder) IncomingBlock(ctx context.Context) {
	for {
		msg, err := b.vm.WaitForEvent(ctx)
		if err != nil {
			b.log.Error("Error waiting for event:", zap.Error(err))
			return
		}
		if msg == common.PendingTxs {
			b.log.Info("Received pending transactions")
			return
		}
		b.log.Warn("Received unexpected message", zap.String("message", msg.String()))
	}
}
