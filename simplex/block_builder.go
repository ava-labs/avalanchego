package simplex

import (
	"context"
	"simplex"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"go.uber.org/zap"
)

type BlockBuilder struct {
	e      *Engine
	Logger simplex.Logger
	VM     block.ChainVM
}

func (b *BlockBuilder) BuildBlock(ctx context.Context, metadata simplex.ProtocolMetadata) (simplex.VerifiedBlock, bool) {
	b.IncomingBlock(ctx)

	block, err := b.VM.BuildBlock(ctx)
	if err != nil {
		b.Logger.Error("Error building block:", zap.Error(err))
		return nil, false
	}

	if err := block.Verify(context.Background()); err != nil {
		b.Logger.Error("Error verifying block I have built myself: %s", zap.Error(err))
		return nil, false
	}

	var vb VerifiedBlock
	vb.metadata = metadata
	vb.innerBlock = block.Bytes()
	md := vb.BlockHeader()
	b.e.observeDigestToIDMapping(md.Digest, block.ID())
	vb.accept = func(ctx context.Context) error {
		b.e.removeDigestToIDMapping(md.Digest)
		b.e.blockTracker.rejectSiblingsAndUncles(md.Round, md.Digest)
		defer b.VM.SetPreference(context.Background(), block.ID())
		return block.Accept(ctx)
	}

	return &vb, true
}

func (b *BlockBuilder) IncomingBlock(ctx context.Context) {
	b.e.log.Info("Waiting for incoming block")
	// for {
	// 	msg := b.vm.SubscribeToEvents(ctx)
	// 	if msg == common.PendingTxs {
	// 		b.e.log.Info("Received pending transactions")
	// 		return
	// 	}
	// 	select {
	// 	case <-ctx.Done():
	// 		return
	// 	}
	// }
}
