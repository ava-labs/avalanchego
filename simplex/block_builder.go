package simplex

import (
	"context"
	"simplex"

	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

var _ simplex.BlockBuilder = (*BlockBuilder)(nil)

type BlockBuilder struct {
	log logging.Logger
}

func NewBlockBuilder(log logging.Logger) *BlockBuilder {
	return &BlockBuilder{
		log: log,
	}
}

func (b *BlockBuilder) BuildBlock(ctx context.Context, metadata simplex.ProtocolMetadata) (simplex.VerifiedBlock, bool) {
	// TODO: Implement the logic to build a block.
	b.log.Info("Building block", zap.Uint64("round", metadata.Round), zap.Uint64("seq", metadata.Seq))
	return nil, false
}

func (b *BlockBuilder) IncomingBlock(ctx context.Context) {
	b.log.Info("Waiting for incoming block")
}
