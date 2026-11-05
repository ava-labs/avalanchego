// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

type seqAndDigest struct {
	digest common.Digest
	seq    uint64
}

func seqAndDigestFromBlock(block common.Block) seqAndDigest {
	return seqAndDigest{
		digest: block.BlockHeader().Digest,
		seq:    block.BlockHeader().Seq,
	}
}

type finalizedQuorumRound struct {
	block        common.Block
	finalization *common.Finalization
}

type ReplicationState struct {
	enabled  atomic.Bool
	logger   common.Logger
	myNodeID common.NodeID
	rand     *rand.Rand // Random number generator

	// seqs maps sequences to a block and its associated finalization
	seqs map[uint64]*finalizedQuorumRound

	// rounds maps round numbers to QuorumRounds
	rounds map[uint64]*common.QuorumRound

	// digestTimeouts handles timeouts for fetching missing block digests.
	// When a notarization depends on a block we haven't received yet,
	// it means a prior notarization for that block exists but is missing.
	// Since we may not know which round that dependency belongs to,
	// digestTimeouts ensures we re-request the missing digest until it arrives.
	digestTimeouts *common.TimeoutHandler[seqAndDigest]

	// emptyRoundTimeouts handles timeouts for fetching missing empty round notarizations.
	// When replication encounters a notarized block that depends on an empty round we haven't received,
	// emptyRoundTimeouts ensures we re-request those empty rounds until they are received.
	emptyRoundTimeouts *common.TimeoutHandler[uint64]

	roundRequestor        *requestor
	finalizationRequestor *requestor

	sender    Sender
	epochLock *sync.Mutex
}

func NewReplicationState(logger common.Logger, comm Sender, myNodeID common.NodeID, maxRoundWindow uint64, enabled bool, start time.Time, lock *sync.Mutex, rng *rand.Rand) *ReplicationState {
	if !enabled {
		return &ReplicationState{
			logger: logger,
			rand:   rng,
		}
	}

	r := &ReplicationState{
		myNodeID: myNodeID,
		logger:   logger,
		rand:     rng,

		// seq replication
		seqs:                  make(map[uint64]*finalizedQuorumRound),
		finalizationRequestor: newRequestor(logger, start, lock, maxRoundWindow, comm, true),

		// round replication
		rounds:         make(map[uint64]*common.QuorumRound),
		roundRequestor: newRequestor(logger, start, lock, maxRoundWindow, comm, false),

		sender:    comm,
		epochLock: lock,
	}
	r.enabled.Store(true)

	r.digestTimeouts = common.NewTimeoutHandler(logger, "digest", start, DefaultReplicationRequestTimeout, r.requestDigests)
	r.emptyRoundTimeouts = common.NewTimeoutHandler(logger, "empty round replication", start, DefaultReplicationRequestTimeout, r.requestEmptyRounds)

	return r
}

func (r *ReplicationState) AdvanceTime(now time.Time) {
	if !r.enabled.Load() {
		return
	}

	r.finalizationRequestor.advanceTime(now)
	r.roundRequestor.advanceTime(now)
	r.digestTimeouts.Tick(now)
	r.emptyRoundTimeouts.Tick(now)
}

// deleteOldRounds cleans up the replication state for round replication after receiving a finalized round.
func (r *ReplicationState) deleteOldRounds(finalizedRound uint64) {
	for round := range r.rounds {
		if round <= finalizedRound {
			r.logger.Debug("Replication State Deleting Old Round", zap.Uint64("round", round))
			delete(r.rounds, round)
		}
	}

	r.roundRequestor.removeOldTasks(finalizedRound)
	r.emptyRoundTimeouts.RemoveOldTasks(func(r uint64, _ struct{}) bool {
		return r <= finalizedRound
	})
}

// deleteOldSeqs cleans up sequences that have already been committed.
// Committing a sequence only deletes its exact entry, so sequences superseded by
// a different block digest would otherwise never be released.
func (r *ReplicationState) deleteOldSeqs(nextSequenceToCommit uint64) {
	for seq := range r.seqs {
		if seq < nextSequenceToCommit {
			r.logger.Debug("Replication State Deleting Old Sequence", zap.Uint64("seq", seq))
			delete(r.seqs, seq)
		}
	}
}

// storeSequence stores a block and finalization into the replication state
func (r *ReplicationState) storeSequence(block common.Block, finalization *common.Finalization) bool {
	if _, exists := r.seqs[finalization.Finalization.Seq]; exists {
		return false
	}

	r.seqs[finalization.Finalization.Seq] = &finalizedQuorumRound{
		block:        block,
		finalization: finalization,
	}

	r.finalizationRequestor.removeTask(finalization.Finalization.Seq)
	r.digestTimeouts.RemoveTask(seqAndDigestFromBlock(block))
	return true
}

// storeRound adds or updates a quorum round in the replication state.
// If the round already exists, it merges any missing notarizations or empty notarizations
// from the provided quorum round. Otherwise, it stores the new round as is.
func (r *ReplicationState) storeRound(qr *common.QuorumRound) {
	if qr.Block != nil {
		r.digestTimeouts.RemoveTask(seqAndDigestFromBlock(qr.Block))
	}

	existing, exists := r.rounds[qr.GetRound()]
	if !exists {
		r.rounds[qr.GetRound()] = qr
		return
	}

	if qr.EmptyNotarization != nil && existing.EmptyNotarization == nil {
		existing.EmptyNotarization = qr.EmptyNotarization
	}

	if (qr.Notarization != nil && qr.Block != nil) && existing.Block == nil {
		existing.Notarization = qr.Notarization
		existing.Block = qr.Block
	}
}

// StoreQuorumRound stores the quorum round into the replication state.
func (r *ReplicationState) StoreQuorumRound(round *common.QuorumRound) {
	if !r.enabled.Load() {
		return
	}

	if round.Finalization != nil {
		r.storeFinalizedQuorumRound(round, round.Finalization.QC.Signers())
		return
	}

	// otherwise we are storing a round without finalization
	// don't bother storing rounds that are older than the highest finalized round we know
	if observed := r.finalizationRequestor.getHighestObserved(); observed != nil && observed.round >= round.GetRound() {
		r.logger.Debug("Replication State received a finalized quorum round for a round we know is finalized.")
		return
	}

	r.logger.Debug("Replication State Storing Quorum Round", zap.Stringer("QR", round))
	r.storeRound(round)
	r.roundRequestor.receivedSignedQuorum(newSignedQuorum(round, r.myNodeID))
	if round.EmptyNotarization != nil {
		r.emptyRoundTimeouts.RemoveTask(round.GetRound())
	}
}

// StoreFinalizedQuorumRoundWithSigners stores a finalized quorum round like StoreQuorumRound, but requests
// the sequences up to it from signers instead of from the signers of its QC.
// It is used when the QC cannot be verified yet, so its signers cannot be trusted.
func (r *ReplicationState) StoreFinalizedQuorumRoundWithSigners(round *common.QuorumRound, signers []common.NodeID) {
	if !r.enabled.Load() {
		return
	}

	r.storeFinalizedQuorumRound(round, signers)
}

func (r *ReplicationState) storeFinalizedQuorumRound(round *common.QuorumRound, signers []common.NodeID) {
	stored := r.storeSequence(round.Block, round.Finalization)
	if !stored {
		return
	}
	finalization := round.Finalization.Finalization
	r.finalizationRequestor.receivedSignedQuorum(newSignedQuorumFromRound(finalization.Round, finalization.Seq, signers, r.myNodeID))
	r.deleteOldRounds(finalization.Round)
	r.logger.Debug("Replication State Storing Quorum Round", zap.Stringer("QR", round))
}

// receivedFutureFinalization notifies the replication state a finalization was created in a future round.
func (r *ReplicationState) ReceivedFutureFinalization(finalization *common.Finalization, nextSeqToCommit uint64) {
	if !r.enabled.Load() {
		return
	}

	signedSequence := newSignedQuorumFromFinalization(finalization, r.myNodeID)

	// maybe this finalization was for a round that we initially thought only had notarizations
	// remove from the round replicator since we now have a finalization for this round
	r.deleteOldRounds(finalization.Finalization.Round)

	// potentially send out requests for blocks/finalizations in between
	r.finalizationRequestor.observedSignedQuorum(signedSequence, nextSeqToCommit)
}

// receivedFutureRound notifies the replication state of a future round.
func (r *ReplicationState) ReceivedFutureRound(round, seq, currentRound uint64, signers []common.NodeID) {
	if !r.enabled.Load() {
		return
	}

	if observed := r.finalizationRequestor.getHighestObserved(); observed != nil && observed.round >= round {
		r.logger.Debug("Ignoring round replication for a future round since we have a finalization for a higher round", zap.Uint64("round", round))
		return
	}

	sq := newSignedQuorumFromRound(round, seq, signers, r.myNodeID)
	r.roundRequestor.observedSignedQuorum(sq, currentRound)
}

// ResendFinalizationRequest notifies the replication state that `seq` should be re-requested.
func (r *ReplicationState) ResendFinalizationRequest(seq uint64, signers []common.NodeID) {
	if !r.enabled.Load() {
		return
	}

	signers = common.NodeIDs(signers).Remove(r.myNodeID)
	numSigners := int64(len(signers))
	index := r.rand.Int64N(numSigners)

	// because we are resending because the block failed to verify, we should remove the stored quorum round
	// so that we can try to get a new block & finalization
	r.DeleteSeq(seq)
	r.finalizationRequestor.sendRequestToNode([]uint64{seq}, signers[index])
}

// ResendRoundRequest notifies the replication state that `round` should be re-requested.
func (r *ReplicationState) ResendRoundRequest(round uint64, signers []common.NodeID) {
	if !r.enabled.Load() {
		return
	}

	signers = common.NodeIDs(signers).Remove(r.myNodeID)
	numSigners := int64(len(signers))
	index := r.rand.Int64N(numSigners)

	r.DeleteRound(round)
	r.roundRequestor.sendRequestToNode([]uint64{round}, signers[index])
}

// CreateDependencyTasks creates tasks to refetch the given parent digest and empty rounds. If there are no
// dependencies, no tasks are created.
func (r *ReplicationState) CreateDependencyTasks(parent *common.Digest, parentSeq uint64, emptyRounds []uint64) {
	if parent != nil {
		r.digestTimeouts.AddTask(seqAndDigest{digest: *parent, seq: parentSeq})
	}

	if len(emptyRounds) > 0 {
		for _, round := range emptyRounds {
			r.emptyRoundTimeouts.AddTask(round)
		}
	}
}

func (r *ReplicationState) clearBlockDependencyTasks(digest common.Digest, seq uint64, finalizationPersisted bool) {
	if !r.enabled.Load() {
		return
	}

	// if the finalization is persisted, we can clear all tasks for sequences up to and including seq
	if finalizationPersisted {
		r.digestTimeouts.RemoveOldTasks(func(missingBlock seqAndDigest, _ struct{}) bool {
			return missingBlock.seq <= seq
		})

		return
	}

	r.digestTimeouts.RemoveTask(seqAndDigest{
		digest: digest,
		seq:    seq,
	})
}

// MaybeAdvanceState attempts to collect future sequences if
// there are more to be collected and the round has caught up for us to send the request.
func (r *ReplicationState) MaybeAdvanceState(nextSequenceToCommit uint64, currentRound uint64, lastCommittedRound uint64) {
	if !r.enabled.Load() {
		return
	}

	if lastCommittedRound > 0 {
		r.deleteOldRounds(lastCommittedRound)
	}

	if nextSequenceToCommit > 0 {
		r.finalizationRequestor.removeOldTasks(nextSequenceToCommit - 1)
	}

	r.deleteOldSeqs(nextSequenceToCommit)

	// update the requestors in case they need to send more requests
	r.finalizationRequestor.updateState(nextSequenceToCommit)
	r.roundRequestor.updateState(currentRound)
}

func (r *ReplicationState) Purge(shouldDelete func(block common.Block) bool) {
	for seq, blockWithFinalization := range r.seqs {
		if shouldDelete(blockWithFinalization.block) {
			r.logger.Debug("Replication State Purging Sequence", zap.Uint64("seq", seq))
			delete(r.seqs, seq)
		}
	}
}

func (r *ReplicationState) GetFinalizedBlockForSequence(seq uint64) (common.Block, *common.Finalization, bool) {
	blockWithFinalization, exists := r.seqs[seq]
	if !exists {
		return nil, nil, false
	}

	return blockWithFinalization.block, blockWithFinalization.finalization, true
}

func (r *ReplicationState) GetLowestRound() *common.QuorumRound {
	var lowestRound *common.QuorumRound

	for round, qr := range r.rounds {
		if lowestRound == nil {
			lowestRound = qr
			continue
		}

		if lowestRound.GetRound() > round {
			lowestRound = qr
		}
	}

	return lowestRound
}

// GetHighestRound returns the highest round known to the replicator.
func (r *ReplicationState) GetHighestRound() uint64 {
	var highestRound uint64

	for round, qr := range r.rounds {
		if highestRound < round {
			highestRound = qr.GetRound()
		}
	}

	for _, qr := range r.seqs {
		if qr.block.BlockHeader().Round > highestRound {
			highestRound = qr.block.BlockHeader().Round
		}
	}

	return highestRound
}

func (r *ReplicationState) GetBlockWithSeq(seq uint64) common.Block {
	block, _, _ := r.GetFinalizedBlockForSequence(seq)
	if block != nil {
		return block
	}

	// check rounds replicator.
	// note: this is not deterministic since we can have multiple blocks notarized with the same seq
	// its fine to return since the caller can still optimistically advance the round
	for _, round := range r.rounds {
		if round.GetSequence() == seq && round.Block != nil {
			return round.Block
		}
	}

	return nil
}

func (r *ReplicationState) requestDigests(missingBlocks []seqAndDigest) {
	// grab the lock since this is called in the timeout handler goroutine
	r.epochLock.Lock()
	defer r.epochLock.Unlock()

	signedQuorum := r.roundRequestor.getHighestObserved()
	if signedQuorum == nil {
		signedQuorum = r.finalizationRequestor.getHighestObserved()
	}
	if signedQuorum == nil {
		r.logger.Warn("Replication State cannot request missing block digests, no known nodes to request from")
		return
	}

	if len(signedQuorum.signers) == 0 {
		r.logger.Error("Replication State cannot request missing block digests, no known nodes to request from")
		return
	}

	startingIndex := int(r.rand.Int64N(int64(len(signedQuorum.signers))))

	for i, mb := range missingBlocks {
		// grab the node to send it to
		index := (i + startingIndex) % len(signedQuorum.signers)
		node := signedQuorum.signers[index]

		r.logger.Debug("Replication State requesting missing block digest", zap.Uint64("seq", mb.seq), zap.Stringer("digest", &mb.digest))
		blockRequest := &common.BlockDigestRequest{
			Seq:    mb.seq,
			Digest: mb.digest,
		}
		r.sender.Send(&common.Message{
			BlockDigestRequest: blockRequest,
		}, node)
	}
}

func (r *ReplicationState) requestEmptyRounds(emptyRounds []uint64) {
	r.logger.Debug("Replication State requesting empty rounds", zap.Uint64s("empty rounds", emptyRounds))
	r.roundRequestor.resendReplicationRequests(emptyRounds)
}

func (r *ReplicationState) DeleteRound(round uint64) {
	if !r.enabled.Load() {
		return
	}

	r.logger.Debug("Replication State Removing Round", zap.Uint64("round", round))
	r.emptyRoundTimeouts.RemoveTask(round)
	r.roundRequestor.removeTask(round)
	delete(r.rounds, round)
}

func (r *ReplicationState) DeleteSeq(seq uint64) {
	if !r.enabled.Load() {
		return
	}

	delete(r.seqs, seq)
}

func (r *ReplicationState) Close() {
	if !r.enabled.Load() {
		return
	}
	r.enabled.Store(false)

	r.digestTimeouts.Close()
	r.emptyRoundTimeouts.Close()
	r.roundRequestor.close()
	r.finalizationRequestor.close()
}
