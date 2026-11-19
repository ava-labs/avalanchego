// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nonvalidator

import (
	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"go.uber.org/zap"
)

// FutureEpochListener detects that `epoch` is stale, meaning a sealing block we have
// not yet indexed finalized a newer epoch. Finalizations and blocks from higher epochs
// only reference the epoch number, so the listener requests the block that sealed it.
// Sealing blocks arriving in replication responses are fed to a threshold collector, and once enough
// validators report the same one, it is handed to onSealingBlock.
type FutureEpochListener struct {
	epoch  uint64
	logger common.Logger
	sender simplex.Sender

	// collector counts sealing blocks per epoch until a threshold of validators report the same one.
	collector *epochDigestCounter

	// onSealingBlock receives a quorum round contain a future sealing block and finalization.
	onSealingBlock func(qr *common.QuorumRound)
}

// NewFutureEpochListener triggers onSealingBlock, when a threshold of responses have collected a sealing block
func NewFutureEpochListener(logger common.Logger, sender simplex.Sender, epoch uint64, latestValidators LatestValidatorSetRetriever, sac common.SignatureAggregatorCreator, onSealingBlock func(qr *common.QuorumRound)) *FutureEpochListener {
	return &FutureEpochListener{
		logger:         logger,
		epoch:          epoch,
		sender:         sender,
		collector:      newEpochReplicator(logger, latestValidators, sac),
		onSealingBlock: onSealingBlock,
	}
}

// HandleMessage inspects msg for evidence of an epoch after f.epoch.
// It will keep track of sealing block responses of future epochs by the most recent validator set, to ensure a node is not behind.
func (f *FutureEpochListener) HandleMessage(msg *common.Message, from common.NodeID) {
	// The highest epoch this message tells us exists but we have no sealing block for.
	var highestEpoch uint64

	switch {
	case msg.ReplicationResponse != nil:
		resp := msg.ReplicationResponse
		qrs := make([]*common.QuorumRound, 0, len(resp.Data)+2)
		for j := range resp.Data {
			qrs = append(qrs, &resp.Data[j])
		}
		qrs = append(qrs, resp.LatestSeq, resp.LatestRound)

		for _, qr := range qrs {
			if qr == nil || qr.Block == nil || qr.Finalization == nil {
				continue
			}

			bh := qr.Block.BlockHeader()
			sealingInfo := qr.Block.SealingBlockInfo()

			// Note the highest epoch in the message
			if bh.Epoch > highestEpoch {
				highestEpoch = bh.Epoch
			}

			// This block is not a sealing block, or its a sealing block for a past epoch
			if sealingInfo == nil || bh.Seq <= f.epoch {
				continue
			}

			if !f.collector.maybeObserveThresholdResponses(sealingInfo, bh, from) {
				continue
			}

			f.logger.Info("Received a threshold of sealing blocks for an epoch after ours",
				zap.Uint64("Our Epoch", f.epoch),
				zap.Uint64("Sealed Epoch", bh.Seq),
				zap.Stringer("From", from),
			)

			// the sealing block's sequence is the epoch it starts
			f.epoch = bh.Seq
			f.onSealingBlock(qr)
			return
		}
	case msg.Finalization != nil:
		highestEpoch = msg.Finalization.Finalization.Epoch
	case msg.BlockMessage != nil && msg.BlockMessage.Block != nil:
		highestEpoch = msg.BlockMessage.Block.BlockHeader().Epoch
	}

	// We received a message that contains a higher epoch than ours, we should try and request that epochs sealing block.
	if highestEpoch > f.epoch {
		f.requestSealingBlock(highestEpoch, from)
	}
}

// requestSealingBlock sends a Block Request for the seq = epoch.
func (f *FutureEpochListener) requestSealingBlock(epoch uint64, to common.NodeID) {
	f.logger.Debug("Requesting the block sealing a future epoch", zap.Uint64("Epoch", epoch), zap.Stringer("To", to))

	f.sender.Send(&common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs: []uint64{epoch},
		},
	}, to)
}
