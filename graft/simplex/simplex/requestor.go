// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"math"
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

// signedQuorum is a round that has been signed by a quorum certificate.
// if the round was empty notarized, seq is set to 0.
type signedQuorum struct {
	round   uint64
	seq     uint64
	signers common.NodeIDs
}

func newSignedQuorum(qr *common.QuorumRound, myNodeID common.NodeID) *signedQuorum {
	// it's possible our node has signed this quorum.
	// For example this may happen if our node has sent a finalized vote
	// for this round and has not received the
	// finalization from the network.
	switch {
	case qr.EmptyNotarization != nil:
		return &signedQuorum{
			signers: common.NodeIDs(qr.EmptyNotarization.QC.Signers()).Remove(myNodeID),
			round:   qr.EmptyNotarization.Vote.Round,
		}
	case qr.Finalization != nil:
		return &signedQuorum{
			signers: common.NodeIDs(qr.Finalization.QC.Signers()).Remove(myNodeID),
			round:   qr.Finalization.Finalization.Round,
			seq:     qr.Finalization.Finalization.Seq,
		}
	case qr.Notarization != nil:
		return &signedQuorum{
			signers: common.NodeIDs(qr.Notarization.QC.Signers()).Remove(myNodeID),
			round:   qr.Notarization.Vote.Round,
			seq:     qr.Notarization.Vote.Seq,
		}
	default:
		return nil
	}
}

func newSignedQuorumFromFinalization(finalization *common.Finalization, nodeID common.NodeID) *signedQuorum {
	return newSignedQuorum(&common.QuorumRound{
		Finalization: finalization,
	}, nodeID)
}

func newSignedQuorumFromRound(round, seq uint64, signers []common.NodeID, myNodeID common.NodeID) *signedQuorum {
	return &signedQuorum{
		round:   round,
		seq:     seq,
		signers: common.NodeIDs(signers).Remove(myNodeID),
	}
}

type Sender interface {
	// Send sends a message to the given destination node
	Send(msg *common.Message, destination common.NodeID)
}

// requestor fetches quorum rounds up to [highestObserved] from the network,
// allowing up to [maxRoundWindow] concurrent requests to limit memory use.
// Ensures all rounds/sequences are eventually received.
type requestor struct {
	epochLock *sync.Mutex

	// highestSequenceRequested prevents duplicates and limits outstanding requests.
	highestRequested uint64

	// highestCommitted is the highest seq/round for which everything up to and
	// including it has been committed (set via removeOldTasks). We never
	// re-request at or below it, so a stale timeout cannot re-request a
	// sequence/round we have already indexed. nil means nothing committed yet.
	highestCommitted *uint64

	// the requestor stops requesting once all sequences/rounds up to an including `highestObserved` have been received.
	highestObserved *signedQuorum

	// Handles timeouts and retries for missing sequences/rounds.
	timeoutHandler *common.TimeoutHandler[uint64]

	logger common.Logger

	// maxRoundWindow is the maximum number of requests we can request past highestRequested.
	maxRoundWindow uint64

	sender Sender

	// requestIterator is an iterator over NodeIDs in order to request quorum rounds
	requestIterator int

	// replicateSeqs is set true if this requestor is for replicating sequences, and false if for rounds.
	replicateSeqs bool
}

func newRequestor(logger common.Logger, start time.Time, lock *sync.Mutex, maxRoundWindow uint64, sender Sender, replicateSeqs bool) *requestor {
	r := &requestor{
		logger:         logger,
		epochLock:      lock,
		maxRoundWindow: maxRoundWindow,
		sender:         sender,
		replicateSeqs:  replicateSeqs,
	}
	name := "seq-timeout-handler"
	if !replicateSeqs {
		name = "round-timeout-handler"
	}
	r.timeoutHandler = common.NewTimeoutHandler(logger, name, start, DefaultReplicationRequestTimeout, r.resendReplicationRequests)
	return r
}

func (r *requestor) advanceTime(now time.Time) {
	r.timeoutHandler.Tick(now)
}

// resendReplicationRequests re-sends requests for [missingIds], the sequences
// or rounds that were previously requested but not received before their
// timeout expired.
func (r *requestor) resendReplicationRequests(missingIds []uint64) {
	// we call this function in the timeout handler goroutine, so we need to
	// ensure we don't have concurrent access to highestObserved
	r.epochLock.Lock()
	defer r.epochLock.Unlock()

	r.sendRequests(missingIds)
}

// observedSignedQuorum is called when we observe a signed quorum for a future round/sequence.
// we do not mix sequences and rounds because we have separate instances of requestor for each.
func (r *requestor) observedSignedQuorum(observed *signedQuorum, currentSeqOrRound uint64) {
	observedSeqOrRound := r.getSeqOrRound(observed)

	// we've observed something we've already requested
	if r.highestRequested >= observedSeqOrRound && r.highestObserved != nil {
		r.logger.Debug("Already requested observed value, skipping", zap.Uint64("value", observedSeqOrRound), zap.Bool("Seq Replication", r.replicateSeqs))
		return
	}

	// if this is the highest observed sequence, update our state
	if r.highestObserved == nil || observedSeqOrRound > r.getSeqOrRound(r.highestObserved) {
		r.highestObserved = observed
	}

	r.sendMoreReplicationRequests(observedSeqOrRound, currentSeqOrRound)
}

// maybeSendMoreReplicationRequests checks if we need to send more replication requests given an observed quorum.
// it limits the amount of outstanding requests to be at most [maxRoundWindow] ahead of [currentSeqOrRound].
func (r *requestor) sendMoreReplicationRequests(observedSeqOrRound, currentSeqOrRound uint64) {
	start := uint64(math.Max(float64(currentSeqOrRound), float64(r.highestRequested)))
	// we limit the number of outstanding requests to be at most maxRoundWindow ahead of nextSeqToCommit
	end := uint64(math.Min(float64(observedSeqOrRound), float64(r.maxRoundWindow+currentSeqOrRound-1)))

	if start > end {
		return
	}
	seqsOrRounds := make([]uint64, 0, end-start+1)
	for i := start; i <= end; i++ {
		seqsOrRounds = append(seqsOrRounds, i)
	}
	r.logger.Debug("Node is behind, attempting to request missing values", zap.Uint64("value", observedSeqOrRound), zap.Uint64("start", uint64(start)), zap.Uint64("end", uint64(end)), zap.Bool("seq requestor", r.replicateSeqs))
	r.sendRequests(seqsOrRounds)
}

// sendRequests requests [seqsOrRounds] from the nodes that have signed
// [highestObserved], by splitting them into batches of at most
// MaxRoundRequests and sending each batch to one of the signers.
// It is the single send path, used both for initial replication requests
// and for re-sending requests that have timed out.
func (r *requestor) sendRequests(seqsOrRounds []uint64) {
	signers := r.highestObserved.signers
	numNodes := len(signers)
	batches := BatchSequences(seqsOrRounds, uint64(numNodes), maxItemCountPerRequest)

	for i, batch := range batches {
		index := (i + r.requestIterator) % numNodes
		r.sendRequestToNode(batch, signers[index])
	}
	r.requestIterator++
}

// sendRequestToNode requests [seqsOrRounds] from node, skipping
// any sequences we have already committed. In case the node does not respond,
// we create a timeout that will re-send the request. seqsOrRounds is expected to be sorted in ascending order,
// as the last element is used to update highestRequested.
func (r *requestor) sendRequestToNode(seqsOrRounds []uint64, node common.NodeID) {
	toRequest := make([]uint64, 0, len(seqsOrRounds))
	for _, seqOrRound := range seqsOrRounds {
		// Skip sequences we have already committed;
		if r.replicateSeqs && r.highestCommitted != nil && seqOrRound <= *r.highestCommitted {
			continue
		}
		toRequest = append(toRequest, seqOrRound)
		// ensure we set a timeout for this sequence
		r.timeoutHandler.AddTask(seqOrRound)
	}

	if len(toRequest) == 0 {
		return
	}

	if !slices.IsSorted(toRequest) {
		slices.Sort(toRequest)
	}
	if last := toRequest[len(toRequest)-1]; last > r.highestRequested {
		r.highestRequested = last
	}

	request := &common.ReplicationRequest{}
	if r.replicateSeqs {
		request.LatestFinalizedSeq = r.highestObserved.seq
		request.Seqs = toRequest
	} else {
		request.LatestRound = r.highestObserved.round
		request.Rounds = toRequest
	}

	msg := &common.Message{ReplicationRequest: request}

	r.logger.Debug("Requesting missing rounds/sequences ",
		zap.Stringer("from", node),
		zap.Int("sequence count", len(request.Seqs)),
		zap.Int("round count", len(request.Rounds)),
		zap.Uint64("latestSeq", request.LatestFinalizedSeq),
		zap.Uint64("latestRound", request.LatestRound),
	)
	r.sender.Send(msg, node)
}

func (r *requestor) receivedSignedQuorum(signedQuorum *signedQuorum) {
	seqOrRound := r.getSeqOrRound(signedQuorum)

	// check if this is the highest round or seq we have seen
	if r.highestObserved == nil || seqOrRound > r.getSeqOrRound(r.highestObserved) {
		r.highestObserved = signedQuorum
	}

	// we received this sequence, remove the timeout task
	r.timeoutHandler.RemoveTask(seqOrRound)
	r.logger.Debug("Received future quorum round", zap.Uint64("seq or round", seqOrRound), zap.Bool("is finalization", r.replicateSeqs))
}

func (r *requestor) updateState(currentRoundOrNextSeq uint64) {
	// we send out more requests once our seq has caught up to 1/2 of the maxRoundWindow
	if currentRoundOrNextSeq+r.maxRoundWindow/2 > r.highestRequested && r.highestObserved != nil {
		r.observedSignedQuorum(r.highestObserved, currentRoundOrNextSeq)
	}
}

func (r *requestor) getHighestObserved() *signedQuorum {
	return r.highestObserved
}

func (r *requestor) getSeqOrRound(signedQuorum *signedQuorum) uint64 {
	if r.replicateSeqs {
		return signedQuorum.seq
	}

	return signedQuorum.round
}

// removes all tasks less or equal to the targetSeqOrRound
func (r *requestor) removeOldTasks(targetSeqOrRound uint64) {
	// set highest committed if we are replicating sequences
	if r.replicateSeqs && (r.highestCommitted == nil || targetSeqOrRound > *r.highestCommitted) {
		committed := targetSeqOrRound
		r.highestCommitted = &committed
	}

	r.timeoutHandler.RemoveOldTasks(func(seqOrRound uint64, _ struct{}) bool {
		return seqOrRound <= targetSeqOrRound
	})
}

func (r *requestor) removeTask(seqOrRound uint64) {
	r.timeoutHandler.RemoveTask(seqOrRound)
}

func (r *requestor) close() {
	r.timeoutHandler.Close()
}
