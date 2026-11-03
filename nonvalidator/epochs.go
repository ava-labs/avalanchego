// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nonvalidator

import (
	"bytes"
	"errors"
	"slices"

	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

var (
	errNoGenesis          = errors.New("no genesis found")
	errMissingSealingInfo = errors.New("no sealing block info for sealing block")
)

type epochMetadata struct {
	nodes                common.Nodes
	eligibleSigners      map[string][]byte
	epoch                uint64
	signatureAggregator  common.SignatureAggregator
	prevSealingBlockHash common.Digest
}

type epochs map[uint64]*epochMetadata

func newEpochMetadata(epoch uint64, sealingMetadata *common.SealingBlockInfo, sigCreator common.SignatureAggregatorCreator) *epochMetadata {
	if sealingMetadata == nil {
		return nil
	}

	// We sort the validators so that LeaderForRound derives the same leader the validators do.
	// Cloned because the validator set belongs to the caller.
	nodes := slices.Clone(sealingMetadata.ValidatorSet)
	common.SortNodes(nodes)

	lookup := make(map[string][]byte, len(nodes))
	for _, node := range nodes {
		lookup[string(node.Id)] = []byte{}
	}

	return &epochMetadata{
		nodes:                nodes,
		eligibleSigners:      lookup,
		epoch:                epoch,
		signatureAggregator:  sigCreator(nodes),
		prevSealingBlockHash: sealingMetadata.PrevSealingBlockHash,
	}
}

// newEpochs creates a mapping of epoch numbers -> epoch metadata. The epoch metadata is used for verifying
// blocks and finalizations, and should only contain epochMetadata that we have validated.
func newEpochs(storage common.Storage, sigAggCreator common.SignatureAggregatorCreator) (epochs, error) {
	lastBlockHeight := storage.NumBlocks()
	if lastBlockHeight == 0 {
		return nil, errNoGenesis
	}

	lastBlock, _, err := storage.Retrieve(lastBlockHeight - 1)
	if err != nil {
		return nil, err
	}

	epochs := make(map[uint64]*epochMetadata)

	// A zero Epoch means this is before the first ever Simplex block(ex. Genesis or Last Snowman Block)
	if lastBlock.BlockHeader().Epoch == 0 {
		return epochs, nil
	}

	var sealingBlock common.VerifiedBlock
	if lastBlock.SealingBlockInfo() == nil {
		sealingBlock, _, err = storage.Retrieve(lastBlock.BlockHeader().Epoch)
		if err != nil {
			return nil, err
		}
		if sealingBlock.SealingBlockInfo() == nil {
			return nil, errMissingSealingInfo
		}
	} else {
		sealingBlock = lastBlock
	}

	lastAcceptedEpoch := newEpochMetadata(sealingBlock.BlockHeader().Seq, sealingBlock.SealingBlockInfo(), sigAggCreator)
	epochs[lastAcceptedEpoch.epoch] = lastAcceptedEpoch
	return epochs, nil
}

func (e epochs) highestEpoch() (uint64, common.Nodes) {
	highest := uint64(0)
	nodes := []common.Node{}
	for epoch, info := range e {
		if epoch > highest {
			highest = epoch
			nodes = info.nodes
		}
	}

	return highest, nodes
}

// removeOldEpochs deletes all epochs strictly less than startEpoch.
func (e epochs) removeOldEpochs(minEpochToKeep uint64) {
	for epoch := range e {
		if epoch < minEpochToKeep {
			delete(e, epoch)
		}
	}
}

// canValidate returns true if `block` is valid sealing block in the chain.
// block is valid if it's a sealing block that isn't in `e`
// and for which there exists a sealing block in e that has a backwards hash pointer to block.
func (e epochs) canValidate(block common.Block) bool {
	if block.SealingBlockInfo() == nil {
		return false
	}

	// the sequence number is the next proposed epoch
	_, ok := e[block.BlockHeader().Seq]
	if ok {
		// cannot validate twice
		return false
	}

	digest := block.BlockHeader().Digest
	for _, md := range e {
		if bytes.Equal(md.prevSealingBlockHash[:], digest[:]) {
			// We have validated the next epoch, and the next epoch has a backward pointer to this one
			return true
		}
	}
	return false
}

// LatestValidatorSetRetriever is an allows the epoch replicator to get the latest validator set.
// This is used to calculate the threshold of votes needed to validate an epoch.
type LatestValidatorSetRetriever func() common.Nodes

// epochDigestCounter counts sealing block responses from validators for each epoch.
// It uses latestValidatorSetRetriever to determine when the required response threshold
// has been reached.
type epochDigestCounter struct {
	logger common.Logger

	// sealingBlockResponses stores the highest sealing block each validator has sent us keyed by NodeID.
	// Once we have collected a threshold of messages for a finalization, the sealing block for that epoch is validated.
	sealingBlockResponses map[string]sealingBlockResponse

	// latestValidatorSetRetriever is used to calculate the threshold of votes needed to validate an epoch
	latestValidatorSetRetriever LatestValidatorSetRetriever

	// signatureAggregatorCreator is used to create a signature aggregator to determine if we have a threshold of votes for an epoch
	signatureAggregatorCreator common.SignatureAggregatorCreator
}

type sealingBlockResponse struct {
	epoch  uint64
	digest common.Digest
}

func newEpochReplicator(logger common.Logger, validatorSetRetriever LatestValidatorSetRetriever, sac common.SignatureAggregatorCreator) *epochDigestCounter {
	return &epochDigestCounter{
		sealingBlockResponses:       make(map[string]sealingBlockResponse),
		logger:                      logger,
		latestValidatorSetRetriever: validatorSetRetriever,
		signatureAggregatorCreator:  sac,
	}
}

// maybeObserveThresholdResponses records a sealing block response for an unknown epoch
// and returns true once a threshold of matching responses has been collected for
// that epoch. Nil sealingBlockInfo values are ignored and return false.
func (e *epochDigestCounter) maybeObserveThresholdResponses(sealingBlockInfo *common.SealingBlockInfo, bh common.BlockHeader, from common.NodeID) bool {
	if sealingBlockInfo == nil {
		return false
	}

	validators := e.latestValidatorSetRetriever()

	if !validators.Contains(from) {
		e.logger.Debug("Received a quorum round from a node that is not a validator", zap.Stringer("from", from))
		return false
	}

	e.logger.Debug("Collected a sealing block", zap.Stringer("QR", sealingBlockInfo), zap.Stringer("From", from))

	// the sequence number is the epoch the sealing block creates
	response := sealingBlockResponse{epoch: bh.Seq, digest: bh.Digest}
	if stored, ok := e.sealingBlockResponses[string(from)]; ok && stored.epoch > response.epoch {
		e.logger.Debug("Ignoring sealing block for a lower epoch than already collected from this node", zap.Stringer("From", from), zap.Uint64("Stored Epoch", stored.epoch), zap.Uint64("Epoch", response.epoch))
		return false
	}
	e.sealingBlockResponses[string(from)] = response

	// check if we have a threshold of responses
	var nodes common.NodeIDs
	nodesByID := make(map[string]common.Node)
	for _, node := range validators {
		nodesByID[string(node.Id)] = node
	}

	signatureAggregator := e.signatureAggregatorCreator(validators)

	for from, other := range e.sealingBlockResponses {
		if other != response {
			continue
		}

		node, exists := nodesByID[from]
		if !exists {
			delete(e.sealingBlockResponses, from)
			continue
		}

		nodes = append(nodes, node.Id)
	}

	if signatureAggregator.AboveByzantineThreshold(nodes) {
		e.logger.Info("We received enough messages to validate a higher epoch",
			zap.Stringer("EpochInfo", sealingBlockInfo),
			zap.Stringer("nodes", nodes),
		)
		return true
	}

	return false
}

// removeOldEpochs deletes all responses for epochs strictly less than minEpochToKeep.
func (e *epochDigestCounter) removeOldEpochs(minEpochToKeep uint64) {
	for from, response := range e.sealingBlockResponses {
		if response.epoch < minEpochToKeep {
			delete(e.sealingBlockResponses, from)
		}
	}
}
