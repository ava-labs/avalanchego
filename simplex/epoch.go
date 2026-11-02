// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

var (
	ErrAlreadyStarted            = errors.New("epoch already started")
	errNotarizationBlockMismatch = errors.New("notarization block header mismatches stored round block header")
	errAlreadyTimedOutOnRound    = errors.New("already timed out on this round")
)

const (
	// DefaultMaxBlacklistedRounds is the default maximum number of blocks before a blacklisted node is redeemed.
	// It equals the value the chain starts with, so nodes using the default do not move it.
	DefaultMaxBlacklistedRounds           = common.GenesisMaxBlacklistedRounds
	DefaultMaxRoundWindow                 = 10
	DefaultProcessingBlocks               = 500
	DefaultMaxProposalWaitTime            = 5 * time.Second
	DefaultReplicationRequestTimeout      = 5 * time.Second
	DefaultEmptyVoteRebroadcastTimeout    = 5 * time.Second
	DefaultFinalizeVoteRebroadcastTimeout = 6 * time.Second
	EmptyVoteTimeoutID                    = "rebroadcast_empty_vote"
	maxItemCountPerRequest                = 10 // max number of rounds or sequences that fit in one replication request
	// DefaultMaxReplicationResponseSize is the max size of a replication response. avalanchego rejects messages larger
	// than 2 MiB. we cap at 80% (4/5) same as avalanchego (see utils/constants/networking.go)
	DefaultMaxReplicationResponseSize = avalanchego.MaxContainersLen
)

type EmptyVoteSet struct {
	timedOut          bool
	votes             map[string]*common.EmptyVote
	emptyNotarization *common.EmptyNotarization
	persisted         bool
}

type Round struct {
	num           uint64
	block         common.VerifiedBlock
	votes         map[string]*common.Vote // NodeID --> vote
	notarization  *common.Notarization
	finalizeVotes map[string]*common.FinalizeVote // NodeID --> vote
	finalization  *common.Finalization
}

func NewRound(block common.VerifiedBlock) *Round {
	return &Round{
		num:           block.BlockHeader().Round,
		block:         block,
		votes:         make(map[string]*common.Vote),
		finalizeVotes: make(map[string]*common.FinalizeVote),
	}
}

type EpochConfig struct {
	MaxBlacklistedRounds       uint16
	MaxProposalWait            time.Duration
	MaxRoundWindow             uint64
	MaxReplicationResponseSize int
	MaxRebroadcastWait         time.Duration
	QCDeserializer             common.QCDeserializer
	Logger                     common.Logger
	ID                         common.NodeID
	Signer                     common.Signer
	Verifier                   common.SignatureVerifier
	BlockDeserializer          common.BlockDeserializer
	SignatureAggregatorCreator common.SignatureAggregatorCreator
	Comm                       common.Communication
	Storage                    common.Storage
	WAL                        common.WriteAheadLog
	BlockBuilder               common.BlockBuilder
	StartTime                  time.Time
	ReplicationEnabled         bool
	RandomSource               *rand.Rand
}

type Epoch struct {
	EpochConfig

	Epoch uint64
	// Runtime
	blockBuilder                   common.BlockBuilder
	epochSealed                    atomic.Bool
	signatureAggregator            common.SignatureAggregator
	oneTimeVerifier                *OneTimeVerifier
	buildBlockScheduler            *common.BasicScheduler
	blockVerificationScheduler     *common.BlockDependencyManager
	lock                           sync.Mutex
	lastBlock                      *common.VerifiedFinalizedBlock // latest block & finalization committed
	canReceiveMessages             atomic.Bool
	finishCtx                      context.Context
	finishFn                       context.CancelFunc
	blockBuilderCtx                context.Context
	blockBuilderCancelFunc         context.CancelFunc
	validatorNodeIDs               common.NodeIDs
	validators                     common.Nodes
	validatorsToPKs                map[string][]byte
	rounds                         map[uint64]*Round
	emptyVotes                     map[uint64]*EmptyVoteSet
	blockDigestsVotedFor           map[uint64]common.Digest
	futureMessages                 messagesFromNode
	round                          uint64 // The current round we notarize
	monitor                        *Monitor
	haltedError                    error
	cancelWaitForBlockNotarization context.CancelFunc
	timeoutHandler                 *common.TimeoutHandler[string]
	replicationState               *ReplicationState
	timedOutRounds                 map[uint16]uint64 // NodeIndex -> round
	redeemedRounds                 map[uint16]uint64 // NodeIndex -> round
}

func NewEpoch(conf EpochConfig) (*Epoch, error) {
	e := &Epoch{
		EpochConfig: conf,
	}
	return e, e.init()
}

// AdvanceTime hints the engine that the given amount of time has passed.
func (e *Epoch) AdvanceTime(t time.Time) {
	// Don't advance time until the epoch has finished starting.
	if !e.canReceiveMessages.Load() {
		return
	}
	e.monitor.AdvanceTime(t)
	e.replicationState.AdvanceTime(t)
	e.timeoutHandler.Tick(t)
}

// HandleMessage notifies the engine about a reception of a message.
func (e *Epoch) HandleMessage(msg *common.Message, from common.NodeID) error {
	e.lock.Lock()
	defer e.lock.Unlock()

	select {
	case <-e.finishCtx.Done():
		return nil
	default:
	}

	// Guard against receiving messages before we are ready to handle them.
	if !e.canReceiveMessages.Load() {
		e.Logger.Debug("Cannot receive a message")
		return nil
	}

	if e.haltedError != nil {
		return e.haltedError
	}

	if from.Equals(e.ID) {
		e.Logger.Warn("Received message from self")
		return nil
	}

	_, known := e.validatorsToPKs[string(from)]
	if !known {
		e.Logger.Debug("Received message from a non-validator node", zap.Stringer("nodeID", from))
		switch {
		case msg.ReplicationRequest != nil && e.ReplicationEnabled:
			return e.handleReplicationRequest(msg.ReplicationRequest, from)
		default:
			e.Logger.Debug("Invalid message type", zap.Stringer("from", from))
			return nil
		}
	}

	// Preliminary epoch check: a consensus message must belong to our epoch.
	// Replication responses are exempt, as they are handled internally.
	if epoch, ok := msg.Epoch(); ok && epoch != e.Epoch {
		e.Logger.Debug("Dropping consensus message from a different epoch",
			zap.Uint64("messageEpoch", epoch), zap.Uint64("ourEpoch", e.Epoch))
		return nil
	}

	switch {
	case msg.BlockMessage != nil:
		return e.handleBlockMessage(msg.BlockMessage, from)
	case msg.VoteMessage != nil:
		return e.handleVoteMessage(msg.VoteMessage, from)
	case msg.EmptyVoteMessage != nil:
		return e.handleEmptyVoteMessage(msg.EmptyVoteMessage, from)
	case msg.Notarization != nil:
		return e.handleNotarizationMessage(msg.Notarization, from)
	case msg.EmptyNotarization != nil:
		return e.handleEmptyNotarizationMessage(msg.EmptyNotarization, from)
	case msg.FinalizeVote != nil:
		return e.handleFinalizeVoteMessage(msg.FinalizeVote, from)
	case msg.Finalization != nil:
		return e.handleFinalizationMessage(msg.Finalization, from)
	case msg.ReplicationResponse != nil && e.ReplicationEnabled:
		return e.handleReplicationResponse(msg.ReplicationResponse, from)
	case msg.ReplicationRequest != nil && e.ReplicationEnabled:
		return e.handleReplicationRequest(msg.ReplicationRequest, from)
	case msg.BlockDigestRequest != nil:
		return e.handleBlockDigestRequest(msg.BlockDigestRequest, from)
	default:
		e.Logger.Debug("Invalid message type", zap.Stringer("from", from))
		return nil
	}
}

func (e *Epoch) init() error {
	if err := e.maybeAssignDefaultConfig(); err != nil {
		return err
	}
	e.oneTimeVerifier = NewOneTimeVerifier(e.Logger)
	scheduler := common.NewScheduler(e.Logger, DefaultProcessingBlocks)
	e.blockVerificationScheduler = common.NewBlockVerificationScheduler(e.Logger, DefaultProcessingBlocks, scheduler)
	e.buildBlockScheduler = common.NewScheduler(e.Logger, 1)
	e.monitor = NewMonitor(e.StartTime, e.Logger)
	e.cancelWaitForBlockNotarization = func() {}
	e.finishCtx, e.finishFn = context.WithCancel(context.Background())
	e.blockBuilderCtx = context.Background()
	e.blockBuilderCancelFunc = func() {}
	e.validators = e.Comm.Validators()
	common.SortNodes(e.validators)
	e.validatorNodeIDs = e.validators.NodeIDs()
	e.timedOutRounds = make(map[uint16]uint64, len(e.validatorNodeIDs))
	e.redeemedRounds = make(map[uint16]uint64, len(e.validatorNodeIDs))
	e.rounds = make(map[uint64]*Round)
	e.emptyVotes = make(map[uint64]*EmptyVoteSet)
	e.futureMessages = make(messagesFromNode, len(e.validatorNodeIDs))
	e.replicationState = NewReplicationState(e.Logger, e.Comm, e.ID, e.MaxRoundWindow, e.ReplicationEnabled, e.StartTime, &e.lock, e.RandomSource)
	e.timeoutHandler = common.NewTimeoutHandler(e.Logger, "emptyVoteRebroadcast", e.StartTime, e.MaxRebroadcastWait, e.emptyVoteTimeoutTaskRunner)
	e.signatureAggregator = e.SignatureAggregatorCreator(e.validators)
	e.blockDigestsVotedFor = make(map[uint64]common.Digest)
	e.validatorsToPKs = make(map[string][]byte, len(e.validatorNodeIDs))
	for _, node := range e.validators {
		e.validatorsToPKs[string(node.Id)] = node.PK
	}
	for _, node := range e.validatorNodeIDs {
		e.futureMessages[string(node)] = make(map[uint64]*messagesForRound)
	}
	e.blockBuilder = &EmptyBlockBuilder{
		ShouldBuildEmptyBlock: e.shouldBuildEmptyBlock,
		Timeout:               e.MaxProposalWait,
		BB:                    e.BlockBuilder,
	}
	err := e.loadLastBlock()
	if err != nil {
		return err
	}

	e.Logger.Info("Starting Simplex Epoch", zap.String("ID", e.ID.String()), zap.Stringer("nodes", e.validatorNodeIDs))

	return e.setMetadataFromStorage()
}

func (e *Epoch) maybeAssignDefaultConfig() error {
	if e.MaxBlacklistedRounds == 0 {
		e.MaxBlacklistedRounds = DefaultMaxBlacklistedRounds
	}
	if e.MaxProposalWait == 0 {
		e.MaxProposalWait = DefaultMaxProposalWaitTime
	}
	if e.MaxRebroadcastWait == 0 {
		e.MaxRebroadcastWait = DefaultEmptyVoteRebroadcastTimeout
	}
	if e.MaxReplicationResponseSize == 0 {
		e.MaxReplicationResponseSize = DefaultMaxReplicationResponseSize
	}
	if e.RandomSource == nil {
		source, err := NewRandomSource()
		if err != nil {
			return err
		}
		e.RandomSource = source
	}
	return nil
}

func (e *Epoch) Start() error {
	if e.canReceiveMessages.Load() {
		return ErrAlreadyStarted
	}

	// Restoring may schedule tasks that mutate the epoch from other goroutines.
	e.lock.Lock()
	err := e.restoreFromWal()
	if err != nil {
		e.lock.Unlock()
		return err
	}

	// Only init receiving messages once you have initialized the data structures required for it.
	e.Logger.Debug("Epoch is ready to receive messages", zap.Uint64("epoch", e.Epoch))
	e.canReceiveMessages.Store(true)
	e.lock.Unlock()

	e.broadcastReplicationSync()

	return nil
}

func (e *Epoch) shouldBuildEmptyBlock(ctx context.Context) bool {
	<-ctx.Done()

	if errors.Is(context.Cause(ctx), common.ErrShouldBuildEmptyBlock) {
		e.lock.Lock()
		defer e.lock.Unlock()

		for r, round := range e.rounds {
			didNotTimeOutOnRound := !e.haveWeAlreadyTimedOutOnThisRound(r)
			notarizedButNotFinalized := round.notarization != nil && round.finalization == nil
			if didNotTimeOutOnRound && notarizedButNotFinalized {
				return true
			}
		}
	}

	return false
}

func (e *Epoch) sequenceAlreadyIndexed(seq uint64) bool {
	return seq < e.nextSeqToCommit()
}

func (e *Epoch) restoreBlockRecord(r []byte, highestWalRound *walRound) error {
	block, err := common.BlockFromRecord(e.finishCtx, e.BlockDeserializer, r)
	if err != nil {
		return err
	}

	if block.BlockHeader().Round > highestWalRound.round {
		// Clear all fields when moving to a new higher round
		*highestWalRound = walRound{
			round: block.BlockHeader().Round,
			block: block,
		}
	} else if block.BlockHeader().Round == highestWalRound.round {
		highestWalRound.block = block
	}

	return e.loadBlockRecord(block)
}

func (e *Epoch) loadBlockRecord(block common.Block) error {
	if e.sequenceAlreadyIndexed(block.BlockHeader().Seq) {
		e.Logger.Debug("Block already indexed, skipping restoration", zap.Uint64("Sequence", block.BlockHeader().Seq))
		return nil
	}

	// we have not indexed this block so we need to verify before restoring
	e.Logger.Debug("Verifying block from WAL", zap.Uint64("Round", block.BlockHeader().Round), zap.Uint64("Seq", block.BlockHeader().Seq))
	verifiedBlock, err := block.Verify(e.finishCtx, common.OnlyVMVerifyOpt)
	if err != nil {
		e.Logger.Error("Failed to verify block from WAL", zap.Uint64("Round", block.BlockHeader().Round), zap.Uint64("Seq", block.BlockHeader().Seq), zap.Error(err))
		return fmt.Errorf("failed to verify block: %w. round %d", err, block.BlockHeader().Round)
	}

	e.rounds[block.BlockHeader().Round] = NewRound(verifiedBlock)
	e.Logger.Info("Block Proposal Recovered From WAL", zap.Uint64("Round", block.BlockHeader().Round))

	// Record the block digest for this round if this is the first block we've received for this round.
	r := block.BlockHeader().Round
	if _, exists := e.blockDigestsVotedFor[r]; !exists {
		e.blockDigestsVotedFor[r] = block.BlockHeader().Digest
	}
	return nil
}

func (e *Epoch) restoreNotarizationRecord(r []byte, highestWalRound *walRound) error {
	notarization, err := common.NotarizationFromRecord(r, e.QCDeserializer)
	if err != nil {
		return err
	}

	if notarization.Vote.Round > highestWalRound.round {
		// Clear all fields when moving to a new higher round
		*highestWalRound = walRound{
			round:        notarization.Vote.Round,
			notarization: &notarization,
		}
	} else if notarization.Vote.Round == highestWalRound.round {
		highestWalRound.notarization = &notarization
	}

	return e.loadNotarizationRecord(r)
}

func (e *Epoch) loadNotarizationRecord(r []byte) error {
	notarization, err := common.NotarizationFromRecord(r, e.QCDeserializer)
	if err != nil {
		return err
	}

	if e.sequenceAlreadyIndexed(notarization.Vote.Seq) {
		e.Logger.Debug("Notarization already indexed, skipping restoration", zap.Uint64("Sequence", notarization.Vote.Seq))
		return nil
	}

	if err := e.storeNotarization(&notarization); err != nil && !errors.Is(err, errNotarizationBlockMismatch) {
		e.Logger.Debug("Failed to store notarization from WAL", zap.Uint64("Round", notarization.Vote.Round), zap.Error(err))
		return err
	}

	e.Logger.Info("Notarization Recovered From WAL", zap.Uint64("Round", notarization.Vote.Round))
	return nil
}

func (e *Epoch) storeNotarization(notarization *common.Notarization) error {
	if notarization == nil {
		return errors.New("notarization is nil")
	}

	if notarization.QC == nil {
		return errors.New("notarization has no QC")
	}

	roundNum := notarization.Vote.Round
	round, exists := e.rounds[roundNum]
	if !exists {
		return fmt.Errorf("could not find round %d", roundNum)
	}

	if round.notarization != nil {
		return fmt.Errorf("notarization for round %d already exists", roundNum)
	}

	if round.block == nil {
		return fmt.Errorf("round %d has no block to associate notarization with", roundNum)
	}

	expectedBlockHeader := round.block.BlockHeader()
	if !expectedBlockHeader.Equals(&notarization.Vote.BlockHeader) {
		e.Logger.Debug("Equivocation detected: notarization block header does not match round block header",
			zap.Uint64("round", roundNum),
			zap.Stringer("block header", &expectedBlockHeader),
			zap.Stringer("notarized block header", &notarization.Vote.BlockHeader))
		delete(e.rounds, roundNum)
		// We need to request the correct block from the network, as we have received the wrong block for this round.
		e.replicationState.ReceivedFutureRound(expectedBlockHeader.Round, expectedBlockHeader.Seq, e.round, notarization.QC.Signers())
		return errNotarizationBlockMismatch
	}

	round.notarization = notarization

	return nil
}

func (e *Epoch) restoreEmptyNotarizationRecord(r []byte, highestWalRound *walRound) error {
	emptyNotarization, err := common.EmptyNotarizationFromRecord(r, e.QCDeserializer)
	if err != nil {
		return err
	}

	if emptyNotarization.Vote.Round > highestWalRound.round {
		// Clear all fields when moving to a new higher round
		*highestWalRound = walRound{
			round:             emptyNotarization.Vote.Round,
			emptyNotarization: &emptyNotarization,
		}
	} else if emptyNotarization.Vote.Round == highestWalRound.round {
		highestWalRound.emptyNotarization = &emptyNotarization
	}

	return e.loadEmptyNotarizationRecord(r)
}

func (e *Epoch) loadEmptyNotarizationRecord(r []byte) error {
	emptyNotarization, err := common.EmptyNotarizationFromRecord(r, e.QCDeserializer)
	if err != nil {
		return err
	}

	emptyVotes := e.getOrCreateEmptyVoteSetForRound(emptyNotarization.Vote.Round)
	emptyVotes.emptyNotarization = &emptyNotarization
	return nil
}

func (e *Epoch) restoreEmptyVoteRecord(r []byte, highestWalRound *walRound) error {
	vote, err := common.ParseEmptyVoteRecord(r)
	if err != nil {
		return err
	}

	if vote.Round > highestWalRound.round {
		// Clear all fields when moving to a new higher round
		*highestWalRound = walRound{
			round:     vote.Round,
			emptyVote: &vote,
		}
	} else if vote.Round == highestWalRound.round {
		highestWalRound.emptyVote = &vote
	}

	return e.loadEmptyVoteRecord(r)
}

func (e *Epoch) loadEmptyVoteRecord(r []byte) error {
	vote, err := common.ParseEmptyVoteRecord(r)
	if err != nil {
		return err
	}

	emptyVotes := e.getOrCreateEmptyVoteSetForRound(vote.Round)
	emptyVotes.timedOut = true

	signature, err := vote.Sign(e.Signer)
	if err != nil {
		return err
	}

	emptyVote := &common.EmptyVote{
		Signature: common.Signature{
			Signer: e.ID,
			Value:  signature,
		},
		Vote: vote,
	}

	emptyVotes.votes[string(e.ID)] = emptyVote

	return nil
}

func (e *Epoch) restoreFinalizationRecord(r []byte, highestWalRound *walRound) error {
	finalization, err := common.FinalizationFromRecord(r, e.QCDeserializer)
	if err != nil {
		return err
	}

	if finalization.Finalization.Round > highestWalRound.round {
		// Clear all fields when moving to a new higher round
		*highestWalRound = walRound{
			round:        finalization.Finalization.Round,
			finalization: &finalization,
		}
	} else if finalization.Finalization.Round == highestWalRound.round {
		highestWalRound.finalization = &finalization
	}

	return e.loadFinalizationRecord(r)
}

func (e *Epoch) loadFinalizationRecord(r []byte) error {
	finalization, err := common.FinalizationFromRecord(r, e.QCDeserializer)
	if err != nil {
		return err
	}
	e.Logger.Info("restoring finalization from wal", zap.Uint64("Round", finalization.Finalization.Round), zap.Uint64("Seq", finalization.Finalization.Seq))
	if e.sequenceAlreadyIndexed(finalization.Finalization.Seq) {
		e.Logger.Debug("Finalization already indexed, skipping restoration", zap.Uint64("Sequence", finalization.Finalization.Seq))
		return nil
	}
	err = e.storeFinalization(&finalization)
	if err != nil {
		return err
	}
	e.pruneCompetingRounds(finalization)
	e.Logger.Info("Finalization Recovered From WAL", zap.Uint64("Round", finalization.Finalization.Round))
	return nil
}

// broadcastLatestReplicationRequest broadcasts a message to the network with our latest round and finalized sequence
// in case we are behind and need to catch up. Potentially, there are no more messages being sent in the network,
// so this method triggers other nodes to send us the messages we missed while we were down.
func (e *Epoch) broadcastReplicationSync() {
	e.lock.Lock()
	latestQR := e.getLatestVerifiedQuorumRound()
	defer e.lock.Unlock()

	var latestRound uint64
	if latestQR != nil {
		latestRound = latestQR.GetRound()
	}

	var latestFinalizedSeq uint64
	if e.lastBlock != nil {
		latestFinalizedSeq = e.lastBlock.Finalization.Finalization.Seq
	}

	replicationRequest := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			LatestRound:        latestRound,
			LatestFinalizedSeq: latestFinalizedSeq,
		},
	}
	e.broadcast(replicationRequest)
}

// resumeFromWal resumes the epoch from the records of the write ahead log.
func (e *Epoch) resumeFromWal(highestRoundRecord *walRound) error {
	e.Logger.Info("Most relevant record recovered from WAL", zap.Uint64("round", highestRoundRecord.round), zap.Stringer("relevant", highestRoundRecord))

	// Handle the most relevant record based on priority: finalization > notarization > emptyNotarization > emptyVote > block
	if highestRoundRecord.finalization != nil {
		finalizationMsg := &common.Message{Finalization: highestRoundRecord.finalization}
		e.broadcast(finalizationMsg)

		e.Logger.Debug("Broadcast finalization",
			zap.Uint64("round", highestRoundRecord.finalization.Finalization.Round),
			zap.Stringer("digest", highestRoundRecord.finalization.Finalization.Digest))

		return e.startRound()
	}

	if highestRoundRecord.notarization != nil {
		notarization := highestRoundRecord.notarization
		lastMessage := common.Message{Notarization: notarization}
		e.broadcast(&lastMessage)

		if e.sequenceAlreadyIndexed(notarization.Vote.Seq) {
			e.Logger.Debug("Notarization already indexed, skipping restoration", zap.Uint64("Sequence", notarization.Vote.Seq))
			return e.startRound()
		}

		return e.doNotarized(notarization.Vote.Round, true)
	}

	if highestRoundRecord.emptyNotarization != nil {
		lastMessage := common.Message{EmptyNotarization: highestRoundRecord.emptyNotarization}
		e.broadcast(&lastMessage)
		return e.startRound()
	}

	if highestRoundRecord.emptyVote != nil {
		ev := highestRoundRecord.emptyVote
		round, exists := e.emptyVotes[ev.Round]
		if !exists {
			return fmt.Errorf("round %d not found for empty vote", ev.Round)
		}
		emptyVote, exists := round.votes[string(e.ID)]
		if !exists {
			return fmt.Errorf("could not find my own vote for round %d", ev.Round)
		}
		lastMessage := common.Message{EmptyVoteMessage: emptyVote}
		e.broadcast(&lastMessage)
		e.Logger.Info("Rebroadcasting empty vote from WAL", zap.Uint64("round", ev.Round))
		e.addEmptyVoteRebroadcastTimeout()
	}

	if highestRoundRecord.block != nil {
		block := highestRoundRecord.block
		if e.sequenceAlreadyIndexed(block.BlockHeader().Seq) {
			e.Logger.Debug("Block already indexed, skipping restoration", zap.Uint64("Sequence", block.BlockHeader().Seq))
			return e.startRound()
		}

		round, exists := e.rounds[block.BlockHeader().Round]
		if !exists {
			// this should not happen, as we restored the block in `restoreBlockRecord`
			return fmt.Errorf("could not find round %d for block", block.BlockHeader().Round)
		}

		if e.ID.Equals(LeaderForRound(e.validatorNodeIDs, block.BlockHeader().Round)) {
			vote, err := e.voteOnBlock(round.block)
			if err != nil {
				return err
			}
			proposal := &common.Message{
				VerifiedBlockMessage: &common.VerifiedBlockMessage{
					VerifiedBlock: round.block,
					Vote:          vote,
				},
			}
			// broadcast only if we are the leader
			e.broadcast(proposal)
			err = e.handleVoteMessage(&vote, e.ID)
			if err != nil {
				return err
			}
		}

		e.monitorProgress(e.round)
		return nil
	}

	return nil
}

func (e *Epoch) setMetadataFromStorage() error {
	// load from storage if no notarization records
	if e.lastBlock == nil {
		return nil
	}

	bh := e.lastBlock.VerifiedBlock.BlockHeader()
	e.round = bh.Round + 1

	// The last block we indexed was a sealing block, therefore the epoch number is that blocks sequence
	if e.lastBlock.VerifiedBlock.SealingBlockInfo() != nil {
		e.Epoch = bh.Seq
		return nil
	}

	// An indexed block without a finalization predates Simplex, so no Simplex block has
	// been indexed and the first Simplex epoch is the sequence the first one will occupy.
	if e.lastBlock.Finalization.QC == nil {
		e.Epoch = e.Storage.NumBlocks()
		return nil
	}

	e.Epoch = bh.Epoch
	return nil
}

func (e *Epoch) setMetadataFromRecords(records [][]byte) error {
	// iterate through all records to find the highest round
	highestRound := e.round
	var highestEpoch uint64
	found := false

	for i := range records {
		recordType := binary.BigEndian.Uint16(records[i])
		switch recordType {
		case common.NotarizationRecordType:
			notarization, err := common.NotarizationFromRecord(records[i], e.QCDeserializer)
			if err != nil {
				return err
			}
			if notarization.Vote.Round >= highestRound {
				highestRound = notarization.Vote.Round
				highestEpoch = notarization.Vote.Epoch
				found = true
			}
		case common.EmptyNotarizationRecordType:
			emptyNotarization, err := common.EmptyNotarizationFromRecord(records[i], e.QCDeserializer)
			if err != nil {
				return err
			}
			if emptyNotarization.Vote.Round >= highestRound {
				highestRound = emptyNotarization.Vote.Round
				highestEpoch = emptyNotarization.Vote.Epoch
				found = true
			}
		case common.FinalizationRecordType:
			finalization, err := common.FinalizationFromRecord(records[i], e.QCDeserializer)
			if err != nil {
				return err
			}
			if finalization.Finalization.Round >= highestRound {
				highestRound = finalization.Finalization.Round
				highestEpoch = finalization.Finalization.Epoch
				found = true
			}
		}
	}

	if found {
		e.round = highestRound + 1
		e.Epoch = highestEpoch
	}

	return nil
}

// restoreFromWal initializes an epoch from the write ahead log.
func (e *Epoch) restoreFromWal() error {
	records, err := e.WAL.ReadAll()
	if err != nil {
		return err
	}

	// Keep records only from the current epoch.
	records, err = common.RecordsFromEpoch(records, e.Epoch)
	if err != nil {
		return err
	}

	if len(records) == 0 {
		return e.startRound()
	}

	highestRoundRecord := &walRound{}

	for _, r := range records {
		if len(r) < 2 {
			return fmt.Errorf("malformed record")
		}
		recordType := binary.BigEndian.Uint16(r)
		switch recordType {
		case common.BlockRecordType:
			err = e.restoreBlockRecord(r, highestRoundRecord)
		case common.NotarizationRecordType:
			err = e.restoreNotarizationRecord(r, highestRoundRecord)
		case common.FinalizationRecordType:
			err = e.restoreFinalizationRecord(r, highestRoundRecord)
		case common.EmptyNotarizationRecordType:
			err = e.restoreEmptyNotarizationRecord(r, highestRoundRecord)
		case common.EmptyVoteRecordType:
			err = e.restoreEmptyVoteRecord(r, highestRoundRecord)
		default:
			e.Logger.Error("undefined record type", zap.Uint16("type", recordType))
			return fmt.Errorf("undefined record type: %d", recordType)
		}
		if err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}

	e.Logger.Info("Recovered from WAL", zap.Int("numRecords", len(records)))
	if err := e.setMetadataFromRecords(records); err != nil {
		return err
	}

	return e.resumeFromWal(highestRoundRecord)
}

// loadLastBlock initializes the epoch with the lastBlock retrieved from storage.
func (e *Epoch) loadLastBlock() error {
	lastIndex, err := RetrieveLastIndexFromStorage(e.Storage)
	if err != nil {
		return err
	}

	e.lastBlock = lastIndex
	return nil
}

func (e *Epoch) Stop() {
	e.Logger.Info("Shutting down node")
	e.finishFn()
	e.monitor.Close()
	e.blockVerificationScheduler.Close()
	e.buildBlockScheduler.Close()
	e.timeoutHandler.Close()
	e.replicationState.Close()
	e.Logger.Info("Node shutdown complete")
}

func (e *Epoch) isEpochSealed() bool {
	return e.epochSealed.Load()
}

func (e *Epoch) handleFinalizationMessage(message *common.Finalization, from common.NodeID) error {
	e.Logger.Verbo("Received finalization message",
		zap.Stringer("from", from), zap.Uint64("round", message.Finalization.Round), zap.Uint64("seq", message.Finalization.Seq))

	nextSeqToCommit := e.nextSeqToCommit()
	// Ignore finalizations for sequences we have already committed
	if nextSeqToCommit > message.Finalization.Seq {
		return nil
	}

	if err := VerifyQC(message.QC, e.signatureAggregator.IsQuorum, e.validatorsToPKs, message, e.validators); err != nil {
		e.Logger.Debug(fmt.Sprintf("Finalization %s", err),
			zap.Int("round", int(message.Finalization.Round)),
			zap.Stringer("NodeID", from))
		return nil
	}

	_, exists := e.rounds[message.Finalization.Round]
	if !exists {
		e.handleFinalizationForPendingOrFutureRound(message, message.Finalization.Round, nextSeqToCommit)
		return nil
	}

	if err := e.storeFinalization(message); err != nil {
		return nil
	}

	return e.persistFinalization(*message)
}

func (e *Epoch) handleFinalizationForPendingOrFutureRound(message *common.Finalization, round uint64, nextSeqToCommit uint64) {
	if round == e.round {
		// delay collecting future finalization if we are verifying the proposal for that round
		// and the finalization is for rounds we have
		for _, msgs := range e.futureMessages {
			msgForRound, exists := msgs[round]
			if exists && msgForRound.proposalBeingProcessed {
				msgForRound.finalization = message
				return
			}
		}
	}

	// TODO: delay requesting future finalizations and blocks, since blocks could be in transit
	e.Logger.Debug("Received finalization for a pending or future round, and we don't have the block", zap.Uint64("round", round), zap.Uint64("our round", e.round))
	if LeaderForRound(e.validatorNodeIDs, e.round).Equals(e.ID) {
		e.Logger.Debug("We are the leader of this round, but a higher round has been finalized. Aborting block building.")
		e.blockBuilderCancelFunc()
	}

	e.replicationState.ReceivedFutureFinalization(message, nextSeqToCommit)
}

func (e *Epoch) handleFinalizeVoteMessage(message *common.FinalizeVote, from common.NodeID) error {
	vote := message.Finalization

	e.Logger.Verbo("Received finalize vote",
		zap.Stringer("from", from), zap.Uint64("round", vote.Round))

	// Only process a point to point finalizations.
	// This is needed to prevent a malicious node from sending us a finalization of a different node for a future round.
	// Since we only verify the finalization when it's due time, this will effectively block us from saving the real finalization
	// from the real node for a future round.
	if !from.Equals(message.Signature.Signer) {
		e.Logger.Debug("Received a finalize vote signed by a different party than sent it", zap.Stringer("signer", message.Signature.Signer), zap.Stringer("sender", from))
		return nil
	}

	// Have we already finalized this round?
	round, exists := e.rounds[vote.Round]

	// If we have not received the proposal yet, we won't have a Round object in e.rounds,
	// yet we may receive the corresponding finalization.
	// This may happen if we're asynchronously verifying the proposal at the moment.
	if !exists && e.round == vote.Round {
		e.Logger.Debug("Received a finalize vote for the current round",
			zap.Uint64("round", vote.Round), zap.Stringer("from", from))
		e.storeFutureFinalizeVote(message, from, vote.Round)
		return nil
	}

	// This finalization may correspond to a proposal from a future round, or to the proposal of the current round
	// which we are still verifying.
	if e.isWithinMaxRoundWindow(vote.Round) {
		e.Logger.Debug("Got finalize vote for a future round", zap.Uint64("round", vote.Round), zap.Uint64("my round", e.round))
		e.storeFutureFinalizeVote(message, from, vote.Round)
		return nil
	}

	if !exists && e.lastBlock != nil && e.lastBlock.Finalization.Finalization.Seq > vote.Seq {
		// This is a finalization for a past round, the node that sent it to us is behind,
		// so we send it the latest finalization to help it catch up and initiate the replication process
		e.sendLatestFinalization(from)
		return nil
	}

	// Finalization for a future round that is too far in the future
	if !exists {
		e.Logger.Debug("Received finalize vote for an unknown round", zap.Uint64("ourRound", e.round), zap.Uint64("round", vote.Round))
		return nil
	}

	if round.finalization != nil {
		e.Logger.Debug("Received finalize vote for an already finalized round", zap.Uint64("round", vote.Round))

		if from.Equals(e.ID) {
			return nil
		}
		// send the finalization to the sender in case they missed it
		e.send(&common.Message{
			Finalization: round.finalization,
		}, from)
		return nil
	}

	if !e.isFinalizationVoteValid(message.Signature, vote, from) {
		return nil
	}

	round.finalizeVotes[string(from)] = message
	e.deleteFutureFinalizeVote(from, vote.Round)

	return e.maybeCollectFinalization(round)
}

func (e *Epoch) storeFutureFinalizeVote(message *common.FinalizeVote, from common.NodeID, round uint64) {
	msgsForRound, exists := e.futureMessages[string(from)][round]
	if !exists {
		msgsForRound = &messagesForRound{}
		e.futureMessages[string(from)][round] = msgsForRound
	}

	// A node only gets to finalize vote once per round.
	if msgsForRound.finalizeVote != nil {
		e.Logger.Debug("Already received a finalize vote from this node for the round",
			zap.Stringer("NodeID", from), zap.Uint64("round", round))
		return
	}

	msgsForRound.finalizeVote = message
}

func (e *Epoch) storeFutureNotarization(message *common.Notarization, from common.NodeID, round uint64) {
	msgsForRound, exists := e.futureMessages[string(from)][round]
	if !exists {
		msgsForRound = &messagesForRound{}
		e.futureMessages[string(from)][round] = msgsForRound
	}
	msgsForRound.notarization = message
}

func (e *Epoch) handleEmptyVoteMessage(message *common.EmptyVote, from common.NodeID) error {
	vote := message.Vote

	e.Logger.Verbo("Received empty vote message",
		zap.Stringer("from", from), zap.Uint64("round", vote.Round), zap.Uint64("our round", e.round))

	// Only process point to point empty votes.
	// A node will never need to forward to us someone else's vote.
	if !from.Equals(message.Signature.Signer) {
		e.Logger.Debug("Received an empty vote signed by a different party than sent it",
			zap.Stringer("signer", message.Signature.Signer), zap.Stringer("sender", from))
		return nil
	}

	if e.round > vote.Round {
		e.Logger.Debug("Got empty vote from a past round",
			zap.Uint64("round", vote.Round), zap.Uint64("my round", e.round), zap.Stringer("from", from))

		// if this node has sent us an empty vote for a past round, it may be behind
		// send it both the latest finalization and the highest round to help it catch up and initiate the replication process
		e.sendLatestFinalization(from)
		e.sendHighestRound(from)

		// also send the notarization or finalization for this round as well
		e.maybeSendNotarizationOrFinalization(from, vote.Round)
		return nil
	}

	if e.round < vote.Round && !e.isWithinMaxRoundWindow(vote.Round) {
		e.Logger.Debug("Got empty vote from a round too far in the future",
			zap.Uint64("round", vote.Round), zap.Uint64("my round", e.round), zap.Stringer("from", from))
		return nil
	}

	signature := message.Signature

	pk, exists := e.validatorsToPKs[string(signature.Signer)]
	if !exists {
		e.Logger.Debug("Received a finalization from an unknown node", zap.Stringer("NodeID", from))
		return nil
	}

	if err := vote.Verify(signature.Value, e.Verifier, pk); err != nil {
		e.Logger.Debug("ToBeSignedEmptyVote verification failed", zap.Stringer("NodeID", signature.Signer), zap.Error(err))
		return nil
	}

	round := vote.Round

	emptyVotes := e.getOrCreateEmptyVoteSetForRound(round)

	emptyVotes.votes[string(from)] = message

	// Future empty votes are replayed by maybeLoadFutureMessages once we reach their round.
	if e.round < round {
		e.Logger.Debug("Stored empty vote from a future round",
			zap.Uint64("round", round), zap.Uint64("my round", e.round), zap.Stringer("from", from))
		return nil
	}

	e.Logger.Debug("Received an empty vote for the current round",
		zap.Uint64("round", round), zap.Stringer("from", from))

	return e.maybeAssembleEmptyNotarization()
}

func (e *Epoch) sendLatestFinalization(to common.NodeID) {
	if e.lastBlock == nil {
		e.Logger.Debug("No blocks committed yet, cannot send latest block", zap.Stringer("to", to))
		return
	}

	msg := &common.Message{
		Finalization: &e.lastBlock.Finalization,
	}
	e.Logger.Debug("Node appears behind, sending it the latest finalization", zap.Stringer("to", to), zap.Uint64("round", e.lastBlock.Finalization.Finalization.Round), zap.Uint64("sequence", e.lastBlock.Finalization.Finalization.Seq))
	e.send(msg, to)
}

func (e *Epoch) sendHighestRound(to common.NodeID) {
	latestQR := e.getLatestVerifiedQuorumRound()

	if latestQR == nil {
		e.Logger.Debug("Cannot send latest round because there is none", zap.Stringer("to", to))
		return
	}

	if latestQR.Notarization != nil {
		msg := &common.Message{
			Notarization: latestQR.Notarization,
		}
		e.Logger.Debug("Node appears behind, sending it the highest round", zap.Stringer("to", to), zap.Uint64("round", latestQR.Notarization.Vote.Round))
		e.send(msg, to)
		return
	}

	if latestQR.EmptyNotarization != nil {
		msg := &common.Message{
			EmptyNotarization: latestQR.EmptyNotarization,
		}
		e.Logger.Debug("Node appears behind, sending it the highest empty notarized round", zap.Stringer("to", to), zap.Uint64("round", latestQR.EmptyNotarization.Vote.Round))
		e.send(msg, to)
		return
	}
}

func (e *Epoch) maybeSendNotarizationOrFinalization(to common.NodeID, round uint64) {
	r, ok := e.rounds[round]

	if !ok {
		// round could be an empty notarized round
		evs, ok := e.emptyVotes[round]
		if ok && evs.emptyNotarization != nil {
			msg := &common.Message{
				EmptyNotarization: evs.emptyNotarization,
			}
			e.Logger.Debug("Node appears behind, sending it an empty notarization", zap.Stringer("to", to), zap.Uint64("round", round))
			e.send(msg, to)
		}

		return
	}

	if r.finalization != nil {
		msg := &common.Message{
			Finalization: r.finalization,
		}
		e.send(msg, to)
		return
	}

	if r.notarization != nil {
		e.Logger.Debug("Node appears behind, sending it a notarization", zap.Stringer("to", to), zap.Uint64("round", round))
		msg := &common.Message{
			Notarization: r.notarization,
		}
		e.send(msg, to)
		return
	}
}

func (e *Epoch) getOrCreateEmptyVoteSetForRound(round uint64) *EmptyVoteSet {
	emptyVotes, exists := e.emptyVotes[round]
	if !exists {
		emptyVotes = &EmptyVoteSet{votes: make(map[string]*common.EmptyVote)}
		e.emptyVotes[round] = emptyVotes
	}
	return emptyVotes
}

func (e *Epoch) handleVoteMessage(message *common.Vote, from common.NodeID) error {
	vote := message.Vote

	e.Logger.Verbo("Received vote message",
		zap.Stringer("from", from), zap.Uint64("round", vote.Round), zap.Stringer("digest", vote.Digest))

	// Only process point to point votes.
	// This is needed to prevent a malicious node from sending us a vote of a different node for a future round.
	// Since we only verify the vote when it's due time, this will effectively block us from saving the real vote
	// from the real node for a future round.
	if !from.Equals(message.Signature.Signer) {
		e.Logger.Debug("Received a vote signed by a different party than sent it",
			zap.Stringer("signer", message.Signature.Signer), zap.Stringer("sender", from),
			zap.Stringer("digest", vote.Digest))
		return nil
	}

	if !e.isVoteRoundValid(vote.Round) {
		return nil
	}

	// If we have not received the proposal yet, we won't have a Round object in e.rounds,
	// yet we may receive the corresponding vote.
	// This may happen if we're asynchronously verifying the proposal at the moment.
	if _, exists := e.rounds[vote.Round]; !exists && e.round == vote.Round {
		e.Logger.Debug("Received a vote for the current round",
			zap.Uint64("round", vote.Round), zap.Stringer("from", from))
		e.storeFutureVote(message, from, vote.Round)
		return nil
	}

	// This vote may correspond to a proposal from a future round, or to the proposal of the current round
	// which we are still verifying.
	if e.isWithinMaxRoundWindow(vote.Round) {
		e.Logger.Debug("Got vote from a future round",
			zap.Uint64("round", vote.Round), zap.Uint64("my round", e.round), zap.Stringer("from", from))
		e.storeFutureVote(message, from, vote.Round)
		return nil
	}

	round, exists := e.rounds[vote.Round]
	if !exists {
		e.Logger.Debug("Received a vote for a non existent round",
			zap.Uint64("round", vote.Round), zap.Uint64("our round", e.round))
		return nil
	}

	if round.notarization != nil || round.finalization != nil {
		e.Logger.Debug("Round already notarized or finalized", zap.Uint64("round", vote.Round))
		return nil
	}

	signature := message.Signature

	pk, exists := e.validatorsToPKs[string(signature.Signer)]
	if !exists {
		e.Logger.Debug("Received a finalization from an unknown node", zap.Stringer("NodeID", from))
		return nil
	}

	// A node only gets to vote once per round. Keeping the origional vote
	if _, exists := round.votes[string(signature.Signer)]; exists {
		e.Logger.Debug("Already received a vote from this node for the round",
			zap.Stringer("NodeID", signature.Signer), zap.Uint64("round", vote.Round))
		e.deleteFutureVote(from, vote.Round)
		return nil
	}

	if err := vote.Verify(signature.Value, e.Verifier, pk); err != nil {
		e.Logger.Debug("ToBeSignedVote verification failed", zap.Stringer("NodeID", signature.Signer), zap.Error(err))
		return nil
	}

	e.rounds[vote.Round].votes[string(signature.Signer)] = message
	e.deleteFutureVote(from, vote.Round)

	return e.maybeCollectNotarization()
}

func (e *Epoch) haveWeAlreadyTimedOutOnThisRound(round uint64) bool {
	emptyVotes, exists := e.emptyVotes[round]
	return exists && emptyVotes.timedOut
}

func (e *Epoch) storeFutureVote(message *common.Vote, from common.NodeID, round uint64) {
	msgsForRound, exists := e.futureMessages[string(from)][round]
	if !exists {
		msgsForRound = &messagesForRound{}
		e.futureMessages[string(from)][round] = msgsForRound
	}

	if msgsForRound.vote != nil {
		e.Logger.Debug("Already received a vote from this node for the round",
			zap.Stringer("NodeID", from), zap.Uint64("round", round))
		return
	}

	msgsForRound.vote = message
}

func (e *Epoch) deleteFutureVote(from common.NodeID, round uint64) {
	msgsForRound, exists := e.futureMessages[string(from)][round]
	if !exists {
		return
	}
	msgsForRound.vote = nil
}

func (e *Epoch) deleteFutureProposal(from common.NodeID, round uint64) {
	msgsForRound, exists := e.futureMessages[string(from)][round]
	if !exists {
		return
	}
	msgsForRound.proposal = nil
	msgsForRound.proposalBeingProcessed = false
}

func (e *Epoch) deleteFutureFinalizeVote(from common.NodeID, round uint64) {
	msgsForRound, exists := e.futureMessages[string(from)][round]
	if !exists {
		return
	}
	msgsForRound.finalizeVote = nil
}

func (e *Epoch) deleteFutureNotarization(from common.NodeID, round uint64) {
	msgsForRound, exists := e.futureMessages[string(from)][round]
	if !exists {
		return
	}
	msgsForRound.notarization = nil
}

func (e *Epoch) isFinalizationVoteValid(signature common.Signature, finalization common.ToBeSignedFinalization, from common.NodeID) bool {
	pk, exists := e.validatorsToPKs[string(signature.Signer)]
	if !exists {
		e.Logger.Debug("Received a finalization from an unknown node", zap.Stringer("NodeID", from))
		return false
	}
	if err := finalization.Verify(signature.Value, e.Verifier, pk); err != nil {
		e.Logger.Debug("Received a finalization with an invalid signature", zap.Uint64("round", finalization.Round), zap.Error(err))
		return false
	}
	return true
}

func (e *Epoch) isVoteRoundValid(round uint64) bool {
	// Ignore votes for previous rounds
	if round < e.round {
		e.Logger.Debug("Received a vote for a past round", zap.Uint64("round", round), zap.Uint64("my round", e.round))
		return false
	}

	// Ignore votes for rounds too far ahead
	if e.isRoundTooFarAhead(round) {
		e.Logger.Debug("Received a vote for a too advanced round",
			zap.Uint64("round", round), zap.Uint64("my round", e.round))
		return false
	}

	return true
}

func (e *Epoch) maybeCollectFinalization(round *Round) error {
	// Divide finalizations into sets that agree on the same metadata
	finalizationsByMD := make(map[string][]*common.FinalizeVote)

	for _, vote := range round.finalizeVotes {
		key := string(vote.Finalization.Bytes())
		finalizationsByMD[key] = append(finalizationsByMD[key], vote)
	}

	var finalizations []*common.FinalizeVote

	for _, finalizationsWithTheSameDigest := range finalizationsByMD {
		if e.signatureAggregator.IsQuorum(NodeIDsFromVotes(finalizationsWithTheSameDigest)) {
			finalizations = finalizationsWithTheSameDigest
			break
		}
	}

	if len(finalizations) == 0 {
		e.Logger.Debug("Could not find enough finalizations for the same metadata")
		return nil
	}

	return e.assembleFinalization(finalizations)
}

func (e *Epoch) assembleFinalization(finalizationVotes []*common.FinalizeVote) error {
	for _, vote := range finalizationVotes {
		e.Logger.Debug("Collected a finalize vote from node", zap.Stringer("NodeID", vote.Signature.Signer), zap.Uint64("round", vote.Finalization.Round), zap.Uint64("seq", vote.Finalization.Seq))
	}

	finalization, err := common.NewFinalization(e.signatureAggregator, finalizationVotes)
	if err != nil {
		return err
	}

	if err := e.storeFinalization(&finalization); err != nil {
		return nil
	}

	return e.persistFinalization(finalization)
}

func (e *Epoch) progressRoundsDueToCommit(round uint64) {
	e.Logger.Debug("Progressing rounds due to commit", zap.Uint64("round", round), zap.Uint64("current round", e.round))
	for e.round < round {
		e.increaseRound()
	}
}

func (e *Epoch) persistFinalization(finalization common.Finalization) error {
	e.Logger.Debug("Received enough finalize votes to finalize a block", zap.Uint64("round", finalization.Finalization.Round))
	// Check to see if we should commit this finalization to the storage as part of a block commit,
	// or otherwise write it to the WAL in order to commit it later.
	startRound := e.round
	nextSeqToCommit := e.nextSeqToCommit()

	e.blockVerificationScheduler.ExecuteBlockDependents(finalization.Finalization.Digest)
	e.blockVerificationScheduler.RemoveOldTasks(finalization.Finalization.Seq)
	e.replicationState.clearBlockDependencyTasks(finalization.Finalization.Digest, finalization.Finalization.Seq, true)
	e.pruneCompetingRounds(finalization)

	if finalization.Finalization.Seq == nextSeqToCommit {
		if err := e.indexFinalizations(finalization.Finalization.Round); err != nil {
			e.Logger.Error("Failed to index finalizations", zap.Error(err))
			return err
		}
	} else {
		finalizationRecord := common.NewQuorumRecord(finalization.QC.Bytes(), finalization.Finalization.Bytes(), common.FinalizationRecordType)
		if err := e.WAL.Append(finalizationRecord); err != nil {
			e.haltedError = err
			e.Logger.Error("Failed to append finalization record to WAL", zap.Error(err))
			return err
		}

		e.Logger.Debug("Persisted finalization to WAL",
			zap.Uint64("round", finalization.Finalization.Round),
			zap.Uint64("seq", finalization.Finalization.Seq),
			zap.Uint64("height", nextSeqToCommit),
			zap.Int("size", len(finalizationRecord)),
			zap.Stringer("digest", finalization.Finalization.Digest))

		// we receive a finalization for a future round
		e.Logger.Debug("Received a finalization for a future sequence", zap.Uint64("seq", finalization.Finalization.Seq), zap.Uint64("nextSeqToCommit", nextSeqToCommit))
		e.replicationState.ReceivedFutureFinalization(&finalization, nextSeqToCommit)

		if err := e.rebroadcastPastFinalizeVotes(); err != nil {
			return err
		}

		if e.round == finalization.Finalization.Round {
			round, ok := e.rounds[e.round]
			// This code path can be hit after incrementing the round from a notarization.
			// this check ensure we do not double increment.
			if ok && round.notarization == nil {
				e.increaseRound()
			}
		}
	}

	finalizationMsg := &common.Message{Finalization: &finalization}
	e.broadcast(finalizationMsg)

	e.Logger.Debug("Broadcast finalization",
		zap.Uint64("round", finalization.Finalization.Round),
		zap.Stringer("digest", finalization.Finalization.Digest))

	// If we have progressed to a new round while we committed blocks,
	// start the new round.
	if startRound < e.round {
		return e.startRound()
	}

	return nil
}

func (e *Epoch) rebroadcastPastFinalizeVotes() error {
	startRound := e.minRoundInRoundsMap()

	for r := startRound; r <= e.round; r++ {
		round, exists := e.rounds[r]
		if !exists {
			e.Logger.Debug("Round not found when rebroadcasting finalize votes", zap.Uint64("round", r))
			continue
		}

		// Already collected a finalization
		if round.finalization != nil {
			e.Logger.Debug("Round already finalized when rebroadcasting finalize votes", zap.Uint64("round", r))
			continue
		}

		// Has notarized this round?
		if round.notarization == nil {
			e.Logger.Debug("Round not notarized when rebroadcasting finalize votes", zap.Uint64("round", r))
			continue
		}

		if e.haveWeAlreadyTimedOutOnThisRound(r) {
			e.Logger.Debug("Round already timed out when rebroadcasting finalize votes", zap.Uint64("round", r))
			continue
		}

		var finalizeVoteMessage *common.Message
		// Try to re-use finalization we created if possible, else create it.
		if vote, exists := round.finalizeVotes[string(e.ID)]; exists {
			finalizeVoteMessage = &common.Message{FinalizeVote: vote}
		} else {
			_, msg, err := e.maybeConstructFinalizeVoteMessage(round.notarization.Vote.BlockHeader)
			if err != nil {
				return err
			}
			finalizeVoteMessage = msg
		}
		e.Logger.Debug("Rebroadcasting finalize vote", zap.Uint64("round", r), zap.Uint64("seq", finalizeVoteMessage.FinalizeVote.Finalization.Seq))
		e.broadcast(finalizeVoteMessage)
	}

	return nil
}

func (e *Epoch) broadcast(msg *common.Message) {
	// If the epoch is sealed, we only allow messages related to replication to be broadcasted.
	// This is to prevent advancing the entire protocol state, but allowing replicating past protocol state.
	if e.isEpochSealed() && !msg.IsReplicationMessage() {
		e.Logger.Debug("Epoch is sealed, aborting message broadcast")
		return
	}
	e.Comm.Broadcast(msg)
}

func (e *Epoch) send(msg *common.Message, to common.NodeID) {
	// If the epoch is sealed, we only allow messages related to replication to be sent.
	// This is to prevent advancing the entire protocol state, but allowing replicating past protocol state.
	if e.isEpochSealed() && !msg.IsReplicationMessage() {
		e.Logger.Debug("Epoch is sealed, aborting message send", zap.Stringer("to", to))
		return
	}
	e.Comm.Send(msg, to)
}

func (e *Epoch) maxRoundInRoundsMap() uint64 {
	maxRound := uint64(0)
	for r := range e.rounds {
		if r > maxRound {
			maxRound = r
		}
	}
	return maxRound
}

func (e *Epoch) minRoundInRoundsMap() uint64 {
	minRound := uint64(math.MaxUint64)
	for r := range e.rounds {
		if r < minRound {
			minRound = r
		}
	}
	return minRound
}

func (e *Epoch) indexFinalizations(startRound uint64) error {
	maxRound := e.maxRoundInRoundsMap()

	e.Logger.Debug("Indexing finalizations",
		zap.Uint64("startRound", startRound),
		zap.Uint64("maxRound", maxRound),
		zap.Uint64("nextSeqToCommit", e.nextSeqToCommit()))

	for currentRound := startRound; currentRound <= maxRound; currentRound++ {
		round, exists := e.rounds[currentRound]
		if !exists {
			e.Logger.Debug("Round not found", zap.Uint64("round", currentRound))
			continue
		}
		if round.finalization == nil {
			break
		}
		if round.finalization.Finalization.Seq != e.nextSeqToCommit() {
			e.Logger.Debug("Finalization does not correspond to the next sequence to commit",
				zap.Uint64("seq", round.finalization.Finalization.Seq), zap.Uint64("height", e.nextSeqToCommit()))
			return nil
		}

		finalization := *round.finalization
		block := round.block
		if err := e.indexFinalization(block, finalization); err != nil {
			return err
		}

		e.deleteRounds(round.num)
		// Clean up the future messages - Remove all messages we may have stored for all rounds until this round
		for _, messagesFromNode := range e.futureMessages {
			for round := range messagesFromNode {
				if round > finalization.Finalization.Round {
					continue
				}
				delete(messagesFromNode, round)
			}
		}
	}
	return nil
}

func (e *Epoch) indexFinalization(block common.VerifiedBlock, finalization common.Finalization) error {
	if err := e.Storage.Index(e.finishCtx, block, finalization); err != nil {
		return err
	}
	e.Logger.Info("Committed block",
		zap.Uint64("round", finalization.Finalization.Round),
		zap.Uint64("sequence", finalization.Finalization.Seq),
		zap.Stringer("digest", finalization.Finalization.Digest))
	e.lastBlock = &common.VerifiedFinalizedBlock{
		VerifiedBlock: block,
		Finalization:  finalization,
	}

	// If this is a sealing block, and is not the zero block, then we have sealed the epoch and should stop processing messages.
	// In case it's the zero block, we do not not seal the epoch.
	if block.SealingBlockInfo() != nil && block.SealingBlockInfo().PrevSealingBlockHash != [32]byte{} {
		e.Logger.Info("Committed a sealing block, epoch is sealed",
			zap.Uint64("round", finalization.Finalization.Round),
			zap.Uint64("sequence", finalization.Finalization.Seq),
			zap.Stringer("digest", finalization.Finalization.Digest))

		finalizationMsg := &common.Message{Finalization: &finalization}
		e.broadcast(finalizationMsg)

		e.epochSealed.Store(true)
	}

	// We have committed because we have collected a finalization.
	// However, we may have not witnessed a notarization.
	// Regardless of that, we can safely progress to the round succeeding the finalization.
	e.progressRoundsDueToCommit(finalization.Finalization.Round + 1)
	return nil
}

func (e *Epoch) maybeAssembleEmptyNotarization() error {
	emptyVotes, exists := e.emptyVotes[e.round]

	// This should never happen, but done for sanity
	if !exists {
		return fmt.Errorf("could not find empty vote set for round %d", e.round)
	}

	// Check if we found a quorum of votes for the same metadata
	popularEmptyVote, signatures, found := findEmptyVoteThatIsQuorum(emptyVotes.votes, e.signatureAggregator.IsQuorum)
	if !found {
		e.Logger.Debug("Could not find empty vote with a quorum or more votes", zap.Uint64("round", e.round))
		return nil
	}

	qc, err := e.signatureAggregator.Aggregate(signatures)
	if err != nil {
		e.Logger.Error("Could not aggregate empty votes signatures", zap.Error(err), zap.Uint64("round", e.round))
		return nil
	}

	emptyNotarization := &common.EmptyNotarization{QC: qc, Vote: popularEmptyVote}
	// write to the empty vote set
	emptyVotes.emptyNotarization = emptyNotarization

	// Persist the empty notarization and also broadcast it to everyone
	return e.persistEmptyNotarization(emptyVotes, true)
}

func findEmptyVoteThatIsQuorum(votes map[string]*common.EmptyVote, isQuorum func([]common.NodeID) bool) (common.ToBeSignedEmptyVote, []common.Signature, bool) {
	votesByBytes := make(map[string][]*common.EmptyVote)
	for _, vote := range votes {
		key := string(vote.Vote.Bytes())
		votesByBytes[key] = append(votesByBytes[key], vote)
	}

	for _, votes := range votesByBytes {
		if len(votes) > 0 && isQuorum(NodeIDsFromVotes(votes)) {
			return votes[0].Vote, emptyVotesToSignatures(votes), true
		}
	}

	return common.ToBeSignedEmptyVote{}, nil, false
}

func (e *Epoch) persistEmptyNotarization(emptyVotes *EmptyVoteSet, shouldBroadcast bool) error {
	if emptyVotes.persisted {
		e.Logger.Debug("Received an empty notarization for a persisted round",
			zap.Uint64("round", emptyVotes.emptyNotarization.Vote.Round))
		return nil
	}

	emptyNotarization := emptyVotes.emptyNotarization
	emptyNotarizationRecord := common.NewEmptyNotarizationRecord(emptyNotarization)
	if err := e.WAL.Append(emptyNotarizationRecord); err != nil {
		e.haltedError = err
		e.Logger.Error("Failed to append empty notarization record to WAL", zap.Error(err))
		return err
	}

	e.Logger.Debug("Persisted empty notarization to WAL",
		zap.Int("size", len(emptyNotarizationRecord)),
		zap.Uint64("round", emptyNotarization.Vote.Round))

	emptyVotes.persisted = true
	if shouldBroadcast {
		notarizationMessage := &common.Message{EmptyNotarization: emptyNotarization}
		e.broadcast(notarizationMessage)
		e.Logger.Debug("Broadcast empty notarization",
			zap.Uint64("round", emptyNotarization.Vote.Round))
	}

	e.blockVerificationScheduler.ExecuteEmptyRoundDependents(emptyNotarization.Vote.Round)
	e.replicationState.DeleteRound(emptyNotarization.Vote.Round)
	// don't increase the round if this is a empty notarization for a past round
	if e.round != emptyNotarization.Vote.Round {
		return nil
	}

	err := e.maybeMarkLeaderAsTimedOutForFutureBlacklisting(emptyNotarization)
	if err != nil {
		return err
	}

	e.increaseRound()

	return e.startRound()
}

func (e *Epoch) maybeMarkLeaderAsTimedOutForFutureBlacklisting(emptyNotarization *common.EmptyNotarization) error {
	e.Logger.Debug("Marking the leader as timed out", zap.Uint64("round", emptyNotarization.Vote.Round), zap.Stringer("leader", LeaderForRound(e.validatorNodeIDs, emptyNotarization.Vote.Round)))
	var blacklist common.Blacklist
	if e.lastBlock != nil {
		if e.lastBlock.VerifiedBlock == nil {
			e.Logger.Error("No verified block found in last block")
			return fmt.Errorf("last block is nil")
		}
		blacklist = e.lastBlock.VerifiedBlock.Blacklist()
		if e.lastBlock.Finalization.Finalization.Epoch < e.Epoch {
			blacklist = blacklist.Stateless(uint16(len(e.validators)))
		}
	}
	round := emptyNotarization.Vote.Round
	leaderIndex := round % uint64(len(e.validatorNodeIDs))
	if !blacklist.IsNodeSuspected(uint16(leaderIndex)) {
		e.timedOutRounds[uint16(leaderIndex)] = round
	}
	return nil
}

func (e *Epoch) maybeCollectNotarization() error {
	votesForCurrentRound := e.rounds[e.round].votes
	voteCount := len(votesForCurrentRound)

	block := e.rounds[e.round].block
	md := block.BlockHeader()

	votesForOurBlock := make([]*common.Vote, 0, voteCount)

	// Ensure we have enough votes for the same block header.
	for _, vote := range votesForCurrentRound {
		if md.Equals(&vote.Vote.BlockHeader) {
			votesForOurBlock = append(votesForOurBlock, vote)
		}
	}

	if !e.signatureAggregator.IsQuorum(NodeIDsFromVotes(votesForOurBlock)) {
		e.Logger.Debug("Not enough votes to form a notarization for our block",
			zap.Uint64("round", e.round),
			zap.Int("voteForOurBlock", len(votesForOurBlock)),
			zap.Int("total votes", voteCount))
		return nil
	}

	notarization, err := common.NewNotarization(e.Logger, e.signatureAggregator, votesForCurrentRound, block.BlockHeader())
	if err != nil {
		return err
	}

	if err := e.storeNotarization(&notarization); err != nil {
		e.Logger.Debug("Not persisting notarization", zap.Error(err))
		return nil
	}

	return e.persistAndBroadcastNotarization(notarization)
}

func (e *Epoch) writeNotarizationToWal(notarization common.Notarization) error {
	notarizationRecord := common.NewQuorumRecord(notarization.QC.Bytes(), notarization.Vote.Bytes(), common.NotarizationRecordType)

	if err := e.WAL.Append(notarizationRecord); err != nil {
		e.haltedError = err
		e.Logger.Error("Failed to append notarization record to WAL", zap.Error(err))
		return err
	}

	e.Logger.Debug("Persisted notarization to WAL",
		zap.Int("size", len(notarizationRecord)),
		zap.Uint64("round", notarization.Vote.Round),
		zap.Stringer("digest", notarization.Vote.Digest))

	return nil
}

func (e *Epoch) persistNotarization(notarization common.Notarization) error {
	if err := e.writeNotarizationToWal(notarization); err != nil {
		return err
	}

	e.blockVerificationScheduler.ExecuteBlockDependents(notarization.Vote.Digest)
	e.replicationState.clearBlockDependencyTasks(notarization.Vote.Digest, notarization.Vote.Seq, false)

	round := notarization.Vote.Round
	for _, signer := range notarization.QC.Signers() {
		if signerIndex := e.validatorNodeIDs.IndexOf(signer); signerIndex != -1 {
			e.Logger.Debug("Potentially redeeming node", zap.Stringer("signer", signer), zap.Uint64("round", round))
			// Replication persists notarizations for past rounds, so only keep the latest round
			// we have seen the node sign in.
			if prev, seen := e.redeemedRounds[uint16(signerIndex)]; !seen || round > prev {
				e.redeemedRounds[uint16(signerIndex)] = round
			}
		} else {
			e.Logger.Error("Signer of notarization not found in eligible nodes", zap.Stringer("signer", signer))
		}
	}

	if notarization.Vote.Round == e.round {
		e.increaseRound()
	}

	return nil
}

func (e *Epoch) persistAndBroadcastNotarization(notarization common.Notarization) error {
	advancedRound := notarization.Vote.Round == e.round
	err := e.persistNotarization(notarization)
	if err != nil {
		return err
	}

	notarizationMessage := &common.Message{Notarization: &notarization}
	e.broadcast(notarizationMessage)

	e.Logger.Debug("Broadcast notarization",
		zap.Uint64("round", notarization.Vote.Round),
		zap.Uint64("seq", notarization.Vote.Seq),
		zap.Stringer("digest", notarization.Vote.Digest))

	return e.doNotarized(notarization.Vote.Round, advancedRound)
}

func (e *Epoch) handleEmptyNotarizationMessage(emptyNotarization *common.EmptyNotarization, from common.NodeID) error {
	vote := emptyNotarization.Vote

	e.Logger.Verbo("Received empty notarization message", zap.Uint64("round", vote.Round))

	if e.isBelowHighestFinalizedRound(vote.Round) {
		e.Logger.Debug("Received an empty notarization for a too low round",
			zap.Uint64("round", vote.Round), zap.Uint64("our round", e.round))

		return nil
	}

	// Otherwise, this round is not notarized or finalized yet, so verify the empty notarization and store it.
	if err := VerifyQC(emptyNotarization.QC, e.signatureAggregator.IsQuorum, e.validatorsToPKs, emptyNotarization, e.validators); err != nil {
		e.Logger.Debug(fmt.Sprintf("Empty notarization %s", err),
			zap.Stringer("NodeID", from))
		return nil
	}

	if vote.Round > e.round {
		e.Logger.Debug("Received an empty notarization for a higher round",
			zap.Uint64("round", vote.Round), zap.Uint64("our round", e.round))

		e.replicationState.ReceivedFutureRound(vote.Round, 0, e.round, emptyNotarization.QC.Signers())

		// store in future state if within max round window
		if e.isWithinMaxRoundWindow(vote.Round) {
			emptyVotes := e.getOrCreateEmptyVoteSetForRound(vote.Round)
			emptyVotes.emptyNotarization = emptyNotarization
		}
		return nil
	}

	emptyVotes := e.getOrCreateEmptyVoteSetForRound(vote.Round)
	emptyVotes.emptyNotarization = emptyNotarization

	// The empty notarization is for this round, so store it but don't broadcast it, as we've received it via a broadcast.
	return e.persistEmptyNotarization(emptyVotes, false)
}

// we do not care to process votes for rounds where we have finalized
func (e *Epoch) isBelowHighestFinalizedRound(round uint64) bool {
	max := uint64(0)

	for _, round := range e.rounds {
		if round.num >= max {
			if round.finalization == nil {
				continue
			}
			max = round.num
		}
	}

	// check if highest is in storage
	if e.lastBlock != nil && e.lastBlock.Finalization.Finalization.Round >= max {
		max = e.lastBlock.Finalization.Finalization.Round
	}

	return round < max
}

func (e *Epoch) handleNotarizationMessage(message *common.Notarization, from common.NodeID) error {
	vote := message.Vote

	e.Logger.Verbo("Received notarization message",
		zap.Stringer("from", from), zap.Uint64("round", vote.Round))

	if e.isBelowHighestFinalizedRound(vote.Round) {
		return nil
	}

	if err := VerifyQC(message.QC, e.signatureAggregator.IsQuorum, e.validatorsToPKs, message, e.validators); err != nil {
		e.Logger.Debug(fmt.Sprintf("Notarization %s", err),
			zap.Stringer("NodeID", from))
		return nil
	}

	if vote.Round > e.round {
		e.Logger.Debug("Received a notarization for a future round",
			zap.Uint64("round", vote.Round), zap.Uint64("our round", e.round))
		e.replicationState.ReceivedFutureRound(vote.Round, vote.Seq, e.round, message.QC.Signers())
		if e.isWithinMaxRoundWindow(vote.Round) {
			e.storeFutureNotarization(message, from, vote.Round)
		}

		return nil
	}

	// Can we handle this notarization right away or should we handle it later?
	round, exists := e.rounds[vote.Round]
	// If we have already notarized the round, no need to continue
	if exists && (round.notarization != nil || round.finalization != nil) {
		e.Logger.Debug("Received a notarization for an already notarized or finalized round")
		return nil
	}

	// Either this notarization is for a round we are currently processing, or for a past round that
	// was empty notarized. In both cases, request the notarization from the sender of the notarization.
	if !exists {
		e.Logger.Info("Received a notarization for this round, but we don't have a block for it yet", zap.Uint64("round", vote.Round), zap.Uint64("epoch round", e.round))
		e.storeFutureNotarization(message, from, vote.Round)

		// we need to request the block
		blockDigestRequest := &common.Message{
			BlockDigestRequest: &common.BlockDigestRequest{
				Digest: vote.Digest,
				Seq:    vote.Seq,
			},
		}
		e.send(blockDigestRequest, from)
		return nil
	}

	// We are about to persist the notarization, so delete it in case it came from the future messages.
	e.deleteFutureNotarization(from, vote.Round)

	// Else, this is a notarization for the current round, and we have stored the proposal for this round.
	// Note that we don't need to check if we have timed out on this round,
	// because if we had collected an empty notarization for this round, we would have progressed to the next round.
	if err := e.storeNotarization(message); err != nil {
		e.Logger.Debug("Not persisting notarization", zap.Error(err))
		return nil
	}

	return e.persistAndBroadcastNotarization(*message)
}

func (e *Epoch) handleBlockMessage(message *common.BlockMessage, from common.NodeID) error {
	block := message.Block
	if block == nil {
		e.Logger.Debug("Got empty block in a BlockMessage")
		return nil
	}

	md := block.BlockHeader()

	e.Logger.Verbo("Received block message",
		zap.Stringer("from", from),
		zap.Uint64("round", md.Round),
		zap.Stringer("digest", md.Digest))

	vote := message.Vote
	from = vote.Signature.Signer

	// Don't bother processing blocks from the past
	if e.round > md.Round {
		return nil
	}

	// The block is for a too high round, we shouldn't handle it as
	// we have only so much memory.
	if e.isRoundTooFarAhead(md.Round) {
		e.Logger.Debug("Received a block message for a too high round",
			zap.Uint64("round", md.Round), zap.Uint64("our round", e.round))
		return nil
	}

	// Check that the node is a leader for the round corresponding to the block.
	if !LeaderForRound(e.validatorNodeIDs, md.Round).Equals(from) {
		// The block is associated with a round in which the sender is not the leader,
		// it should not be sending us any block at all.
		e.Logger.Debug("Got block from a block proposer that is not the leader of the round", zap.Stringer("NodeID", from), zap.Uint64("round", md.Round))
		return nil
	}

	// Check if we have verified this message in the past:
	if err := e.verifyBlockMessageVote(from, md, vote); err != nil {
		return nil
	}

	// If this is a message from a more advanced round,
	// only store it if it is up to `maxRoundWindow` ahead.
	if e.isWithinMaxRoundWindow(md.Round) {
		e.Logger.Debug("Got block of a future round", zap.Uint64("round", md.Round), zap.Uint64("my round", e.round))
		msgsForRound, exists := e.futureMessages[string(from)][md.Round]
		if !exists {
			msgsForRound = &messagesForRound{}
			e.futureMessages[string(from)][md.Round] = msgsForRound
		}

		// Has this node already sent us a proposal?
		// If so, it cannot send it again.
		if msgsForRound.proposal != nil {
			e.Logger.Debug("Already received a proposal from this node for the round",
				zap.Stringer("NodeID", from), zap.Uint64("round", md.Round))
			return nil
		}

		msgsForRound.proposal = message
		return nil
	}

	if !e.verifyProposalMetadataAndBlacklist(block) {
		e.Logger.Debug("Got invalid block in a BlockMessage")
		return nil
	}

	// If we are already processing a block for this round, reject the block while it is being processed.
	if msgForRound, exists := e.futureMessages[string(from)][md.Round]; exists && msgForRound.proposalBeingProcessed {
		e.Logger.Debug("Got block for a round that is being processed", zap.Uint64("round", md.Round))
		return nil
	}

	// Create a task that will verify the block in the future, after its predecessors have also been verified.
	task := e.createBlockVerificationTask(e.oneTimeVerifier.Wrap(block), from, vote)

	prevBlockDependency, missingRounds := e.blockDependencies(md)

	if len(missingRounds) > 0 {
		e.sendMissingRoundsRequest(from, missingRounds)
	}

	if err := e.blockVerificationScheduler.ScheduleTaskWithDependencies(task, md.Seq, md.Epoch, prevBlockDependency, missingRounds); err != nil {
		return nil
	}

	// Schedule the block to be verified once its direct predecessor have been verified,
	// or if it can be verified immediately.
	e.Logger.Debug("Scheduling block verification",
		zap.Uint64("round", md.Round),
		zap.Uint64("seq", md.Seq),
		zap.Stringer("digest", md.Digest),
		zap.Stringer("prev dependency", prevBlockDependency),
		zap.Uint64s("missing empty notarization rounds", missingRounds),
	)

	// mark in future messages while we are verifying the block
	msgForRound, exists := e.futureMessages[string(from)][md.Round]
	if !exists {
		msgsForRound := &messagesForRound{}
		msgsForRound.proposalBeingProcessed = true
		e.futureMessages[string(from)][md.Round] = msgsForRound
	} else {
		msgForRound.proposalBeingProcessed = true
	}

	return nil
}

func (e *Epoch) sendMissingRoundsRequest(to common.NodeID, missingRounds []uint64) {
	e.Logger.Debug("Requesting missing empty notarizations for rounds",
		zap.Stringer("to", to),
		zap.Uint64s("missing rounds", missingRounds))

	request := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Rounds: missingRounds,
		},
	}

	e.send(request, to)
}

// blockDependencies returns the dependencies bh has before it can be verified.
// It returns the digest of the previous block it depends on (or emptyDigest if none),
// as well as a list of rounds for which it needs to verify empty notarizations.
func (e *Epoch) blockDependencies(bh common.BlockHeader) (*common.Digest, []uint64) {
	if bh.Seq == 0 {
		// genesis block has no dependencies
		return nil, nil
	}

	prevBlockDependency := &bh.Prev

	prevBlock, notarizationOrFinalization, found := e.locateBlock(bh.Seq-1, bh.Prev[:])
	if !found {
		e.Logger.Info("Could not find predecessor block with notarization or finalization for proposal scheduling",
			zap.Uint64("seq", bh.Seq-1),
			zap.Stringer("prev", bh.Prev))

		return &bh.Prev, nil
	}

	// no block dependency if we already have a notarization or finalization for the previous block
	if notarizationOrFinalization != nil {
		prevBlockDependency = nil
	}

	// missing empty rounds
	var missingRounds []uint64
	for round := prevBlock.BlockHeader().Round + 1; round < bh.Round; round++ {
		emptyVotes, exists := e.emptyVotes[round]
		if !exists || emptyVotes.emptyNotarization == nil {
			missingRounds = append(missingRounds, round)
		}
	}

	return prevBlockDependency, missingRounds
}

// processFinalizedBlocks processes a block that has a finalization.
// if the block has already been verified, it will index the finalization,
// otherwise it will verify the block first.
func (e *Epoch) processFinalizedBlock(block common.Block, finalization *common.Finalization) error {
	e.Logger.Debug("Processing finalized block during replication", zap.Uint64("round", finalization.Finalization.Round), zap.Uint64("sequence", finalization.Finalization.Seq))

	round, exists := e.rounds[finalization.Finalization.Round]
	// dont create a block verification task if the block is already in the rounds map
	if exists {
		roundDigest := round.block.BlockHeader().Digest
		seqDigest := finalization.Finalization.Digest
		if !bytes.Equal(roundDigest[:], seqDigest[:]) {
			e.Logger.Debug("Received finalized block that is different from the one we have in the rounds map",
				zap.Stringer("roundDigest", roundDigest), zap.Stringer("seqDigest", seqDigest))
			delete(e.rounds, round.num)
			return e.processFinalizedBlock(block, finalization)
		}
		// The round can already hold this finalization, restored from the WAL or received in a
		// finalization message. Indexing it is all that remains.
		if round.finalization == nil {
			if err := e.storeFinalization(finalization); err != nil {
				e.Logger.Error("Failed storing finalization", zap.Error(err))
				return err
			}
		}
		prevEpochRound := e.round
		if err := e.indexFinalizations(round.num); err != nil {
			e.Logger.Error("Failed to index finalization", zap.Error(err))
			e.haltedError = err
			return err
		}

		if err := e.processReplicationState(); err != nil {
			e.haltedError = err
			return err
		}

		e.blockVerificationScheduler.ExecuteBlockDependents(finalization.Finalization.Digest)

		// Start the round if the epoch has advanced a round & is beyond our replication state
		if e.round > prevEpochRound && e.round > e.replicationState.GetHighestRound() {
			if err := e.startRound(); err != nil {
				e.haltedError = err
				return err
			}
		}
		return nil
	}

	blockDependency, missingRounds := e.blockDependencies(block.BlockHeader())
	// because its finalized we don't care about empty rounds
	if blockDependency != nil {
		e.Logger.Error(
			"Received a finalization for nextSeqToCommit that breaks our chain",
			zap.Stringer("block digest", block.BlockHeader().Digest),
			zap.Stringer("expected digest", blockDependency),
			zap.Uint64s("missing rounds", missingRounds),
		)
		return errors.New("received a finalization for nextSeqToCommit that breaks our chain")
	}

	// Create a task that will verify the block in the future, after its predecessors have also been verified.
	task := e.createFinalizedBlockVerificationTask(e.oneTimeVerifier.Wrap(block), finalization)
	err := e.blockVerificationScheduler.ScheduleTaskWithDependencies(task, block.BlockHeader().Seq, block.BlockHeader().Epoch, blockDependency, []uint64{})
	if errors.Is(err, common.ErrTooManyPendingVerifications) {
		// A full scheduler shouldn't fatal
		e.Logger.Debug("Dropping finalized block, too many pending verifications",
			zap.Uint64("seq", block.BlockHeader().Seq), zap.Error(err))
		e.replicationState.ResendFinalizationRequest(block.BlockHeader().Seq, finalization.QC.Signers())
		return nil
	}
	return err
}

// processNotarizedBlock processes a block that has a notarization.
// if the block has already been verified, it will persist the notarization,
// otherwise it will verify the block first.
func (e *Epoch) processNotarizedBlock(block common.Block, notarization *common.Notarization) error {
	e.Logger.Debug("Processing notarized block during replication", zap.Uint64("round", notarization.Vote.Round), zap.Uint64("sequence", notarization.Vote.Seq))
	md := block.BlockHeader()
	round, exists := e.rounds[md.Round]

	// don't create a block verification task if the block is already in the rounds map
	if exists {
		if round.notarization != nil || round.finalization != nil {
			e.Logger.Debug("Round already notarized", zap.Uint64("round", md.Round))
			return nil
		}

		roundDigest := round.block.BlockHeader().Digest
		notarizedDigest := notarization.Vote.Digest
		if !bytes.Equal(roundDigest[:], notarizedDigest[:]) {
			e.Logger.Debug("Received notarized block that is different from the one we have in the rounds map",
				zap.Stringer("roundDigest", roundDigest), zap.Stringer("notarizedDigest", notarizedDigest))
			// by deleting the round, and recursively calling processNotarizedBlock
			// we will verify this new block and store the notarization.
			delete(e.rounds, md.Round)
			return e.processNotarizedBlock(block, notarization)
		}

		if err := e.storeNotarization(notarization); err != nil {
			return nil
		}

		if err := e.persistNotarization(*notarization); err != nil {
			e.Logger.Warn("Failed to persist notarization", zap.Error(err))
			e.haltedError = err
			return nil
		}

		return e.processReplicationState()
	}

	// Create a task that will verify the block in the future, after its predecessors have also been verified.
	task := e.createNotarizedBlockVerificationTask(e.oneTimeVerifier.Wrap(block), *notarization)

	// Only fetch potential block dependencies and not empty notarizations for notarized quorum rounds
	blockDependency, _ := e.blockDependencies(md)

	e.replicationState.CreateDependencyTasks(blockDependency, md.Seq-1, []uint64{})
	err := e.blockVerificationScheduler.ScheduleTaskWithDependencies(task, md.Seq, md.Epoch, blockDependency, []uint64{})
	if errors.Is(err, common.ErrTooManyPendingVerifications) {
		e.Logger.Debug("Verification queue is full, re-requesting notarized block", zap.Uint64("round", md.Round))
		e.replicationState.ResendRoundRequest(md.Round, notarization.QC.Signers())
		return nil
	}

	return err
}

// createBlockVerificationTask returns a task that verifies a proposed block and stores it in the rounds map.
// The task returns the block's digest only if the block is stored, so that blocks depending on it are verified.
// Otherwise it returns the zero digest, which executes no dependents.
func (e *Epoch) createBlockVerificationTask(block common.Block, from common.NodeID, vote common.Vote) func() common.Digest {
	return func() common.Digest {
		md := block.BlockHeader()

		e.Logger.Debug("Block verification started", zap.Uint64("round", md.Round))
		start := time.Now()
		defer func() {
			elapsed := time.Since(start)
			e.Logger.Debug("Block verification ended", zap.Uint64("round", md.Round), zap.Duration("elapsed", elapsed))
		}()

		if e.isEpochSealed() {
			e.Logger.Debug("Aborting block verification because the epoch is sealed", zap.Uint64("seq", md.Seq))
			return common.Digest{}
		}
		verifiedBlock, err := block.Verify(context.Background())

		e.lock.Lock()
		defer e.lock.Unlock()

		e.deleteFutureProposal(from, md.Round)

		if err != nil {
			leader := LeaderForRound(e.validatorNodeIDs, md.Round)
			e.Logger.Info("Triggering empty block agreement",
				zap.String("reason", "Failed verifying block"),
				zap.Uint64("round", md.Round),
				zap.Stringer("leader", leader),
				zap.Error(err))
			e.triggerEmptyBlockNotarization(md.Round)
			return common.Digest{}
		}

		// A higher round was finalized while we were verifying, so this block can no longer be finalized.
		if e.isBelowHighestFinalizedRound(md.Round) {
			e.Logger.Debug("Dropping proposal for a round below a finalized round", zap.Uint64("round", md.Round))
			return common.Digest{}
		}

		if !e.storeProposal(verifiedBlock) {
			e.Logger.Debug("Unable to store proposed block for the round", zap.Stringer("NodeID", from), zap.Uint64("round", md.Round))
			return common.Digest{}
		}

		// We might have received votes and finalizations from future rounds before we received this block.
		// So load the messages into our round data structure now that we have created it.
		err = e.maybeLoadFutureMessages()
		if err != nil {
			e.haltedError = err
			return md.Digest
		}

		// Check if we have timed out on this round.
		// Although we store the proposal for this round,
		// we refuse to vote for it because we have timed out.
		// We store the proposal only in order to be able to finalize it
		// in case we cannot assemble an empty notarization but eventually
		// this proposal is either notarized or finalized.
		if e.haveWeAlreadyTimedOutOnThisRound(md.Round) {
			e.Logger.Debug("Refusing to vote on block because already timed out in this round", zap.Uint64("round", md.Round), zap.Stringer("NodeID", from))
			return md.Digest
		}

		// Once we have stored the proposal, we have a Round object for the round.
		// We store the vote to prevent verifying its signature again.
		round, exists := e.rounds[md.Round]
		if !exists {
			// This shouldn't happen, but in case it does, return an error
			e.Logger.Error("programming error: round not found", zap.Uint64("round", md.Round))
			return common.Digest{}
		}
		round.votes[string(vote.Signature.Signer)] = &vote

		if err := e.doProposed(verifiedBlock); err != nil {
			e.Logger.Error("Failed voting on block", zap.Error(err))
		}

		return md.Digest
	}
}

// createFinalizedBlockVerificationTask returns a task that verifies a replicated finalized block and indexes it.
// The task returns the block's digest only if the block is stored, so that blocks depending on it are verified.
// Otherwise it returns the zero digest, which executes no dependents.
func (e *Epoch) createFinalizedBlockVerificationTask(block common.Block, finalization *common.Finalization) func() common.Digest {
	return func() common.Digest {
		md := block.BlockHeader()
		e.Logger.Debug("Block verification started", zap.Uint64("round", md.Round))
		start := time.Now()
		defer func() {
			elapsed := time.Since(start)
			e.Logger.Debug("Block verification ended", zap.Uint64("round", md.Round), zap.Duration("elapsed", elapsed))
		}()

		if e.isEpochSealed() {
			e.Logger.Debug("Aborting block verification because the epoch is sealed", zap.Uint64("seq", md.Seq))
			return common.Digest{}
		}

		verifiedBlock, err := block.Verify(context.Background(), common.OnlyVMVerifyOpt)
		if err != nil {
			e.Logger.Debug("Failed verifying block", zap.Error(err))
			// if we fail to verify the block, we re-add to request timeout
			e.lock.Lock()
			defer e.lock.Unlock()
			e.replicationState.ResendFinalizationRequest(md.Seq, finalization.QC.Signers())
			return common.Digest{}
		}

		e.lock.Lock()
		defer e.lock.Unlock()

		round := e.round

		// we started verifying the block when it was the next sequence to commit, however its
		// possible we received a finalization for this block in the meantime. This check ensures we commit
		// the block only if it is still the next sequence to commit.
		if e.Storage.NumBlocks() != md.Seq {
			e.Logger.Debug("Received finalized block that is not the next sequence to commit",
				zap.Uint64("seq", md.Seq), zap.Uint64("height", e.nextSeqToCommit()))
			return common.Digest{}
		}

		// Store the verified block in rounds map so subsequent blocks can find it as a dependency
		roundEntry := NewRound(verifiedBlock)
		e.rounds[md.Round] = roundEntry
		if err := e.storeFinalization(finalization); err != nil {
			// storeFinalization removes the round on a block header mismatch, so the block is not available.
			return common.Digest{}
		}
		e.Logger.Debug("Stored finalized replicated block in rounds map",
			zap.Uint64("round", md.Round),
			zap.Uint64("seq", md.Seq),
			zap.Stringer("digest", md.Digest))

		if err := e.indexFinalization(verifiedBlock, *finalization); err != nil {
			e.haltedError = err
			e.Logger.Error("Failed to index finalization", zap.Error(err))
			return md.Digest
		}
		err = e.processReplicationState()

		if err != nil {
			e.haltedError = err
			e.Logger.Error("Failed to process replication state", zap.Error(err))
			return md.Digest
		}

		// Start the round if the epoch has advanced a round & is beyond our replication state
		if e.round > round && e.round > e.replicationState.GetHighestRound() {
			if err := e.startRound(); err != nil {
				e.haltedError = err
				e.Logger.Error("Failed to process replication state", zap.Error(err))
				return md.Digest
			}
		}

		return md.Digest
	}
}

// createNotarizedBlockVerificationTask returns a task that verifies a replicated notarized block and stores it
// along with its notarization. The task returns the block's digest only if the block is stored, so that blocks
// depending on it are verified. Otherwise it returns the zero digest, which executes no dependents.
func (e *Epoch) createNotarizedBlockVerificationTask(block common.Block, notarization common.Notarization) func() common.Digest {
	return func() common.Digest {
		md := block.BlockHeader()

		e.Logger.Debug("Block verification started", zap.Uint64("round", md.Round))
		start := time.Now()
		defer func() {
			elapsed := time.Since(start)
			e.Logger.Debug("Block verification ended", zap.Uint64("round", md.Round), zap.Duration("elapsed", elapsed))
		}()

		if e.isEpochSealed() {
			e.Logger.Debug("Aborting block verification because the epoch is sealed", zap.Uint64("seq", md.Seq))
			return common.Digest{}
		}

		verifiedBlock, err := block.Verify(context.Background(), common.OnlyVMVerifyOpt)
		if err != nil {
			e.Logger.Debug("Failed verifying block", zap.Error(err))
			e.lock.Lock()
			defer e.lock.Unlock()
			e.replicationState.ResendRoundRequest(md.Round, notarization.QC.Signers())
			return common.Digest{}
		}

		e.lock.Lock()
		defer e.lock.Unlock()

		// A higher round was finalized while we were verifying, so this block can no longer be finalized.
		if e.isBelowHighestFinalizedRound(md.Round) {
			e.Logger.Debug("Dropping notarized block for a round below a finalized round", zap.Uint64("round", md.Round))
			return common.Digest{}
		}

		// we started verifying the block when we didn't have a notarization, however its
		// possible we received a notarization or empty notarization for this block in the meantime.
		round, ok := e.rounds[md.Round]
		if ok && round.notarization != nil {
			e.Logger.Debug("Verifying notarized block that already has a notarization for the round",
				zap.Uint64("round", md.Round))
			return common.Digest{}
		}

		if ok && round.finalization != nil {
			e.Logger.Debug("Verifying notarized block that already has a finalization for the round",
				zap.Uint64("round", md.Round))
			return common.Digest{}
		}

		if ok && round.notarization == nil && round.finalization == nil && round.block != nil {
			// The round already has a block in the rounds map, but it's not notarized or finalized.
			// We have the block and its notarization, so just delete the round before we proceed with
			// storing the new block and notarization.
			delete(e.rounds, md.Round)
		}

		// store the block in rounds
		if !e.storeProposal(verifiedBlock) {
			e.Logger.Debug("Unable to store proposed block for the round", zap.Uint64("round", md.Round))
			return common.Digest{}
		}

		if err := e.storeNotarization(&notarization); err != nil {
			// storeNotarization removes the round on a block header mismatch, so the block is not available.
			return common.Digest{}
		}

		if err := e.persistNotarization(notarization); err != nil {
			e.haltedError = err
			e.Logger.Error("Failed to persist notarization", zap.Error(err))
			return md.Digest
		}

		if err := e.finalizeVoteForReplicatedNotarization(md.Round); err != nil {
			e.Logger.Error("Failed to finalize vote for replicated notarization", zap.Uint64("round", md.Round), zap.Error(err))
		}

		err = e.processReplicationState()
		if err != nil {
			e.haltedError = err
			e.Logger.Error("Failed to process replication state", zap.Error(err))
			return md.Digest
		}

		// If we have advanced a round, and the new round is beyond our replication state, start the new round.
		if e.round > md.Round && e.round > e.replicationState.GetHighestRound() {
			if err := e.startRound(); err != nil {
				e.haltedError = err
				return md.Digest
			}
		}

		return md.Digest
	}
}

// verifyBlockMessageVote verifies that the vote corresponds to the block proposed, and that the vote is properly signed.
func (e *Epoch) verifyBlockMessageVote(from common.NodeID, md common.BlockHeader, vote common.Vote) error {
	pk, exists := e.validatorsToPKs[string(vote.Signature.Signer)]
	if !exists {
		e.Logger.Debug("Received a finalization from an unknown node", zap.Stringer("NodeID", from))
		return fmt.Errorf("received a finalization from an unknown node %s", from)
	}

	// Ensure the block was voted on by its block producer:
	// 1) Verify block header corresponds to the block header voted on
	if !md.Equals(&vote.Vote.BlockHeader) {
		return errors.New("vote block header does not match block header")
	}

	// 2) Verify the vote is properly signed
	if err := vote.Vote.Verify(vote.Signature.Value, e.Verifier, pk); err != nil {
		e.Logger.Debug("ToBeSignedVote verification failed", zap.Stringer("NodeID", vote.Signature.Signer), zap.Error(err))
		return errors.New("vote signature verification failed")
	}

	return nil
}

func (e *Epoch) verifyProposalMetadataAndBlacklist(block common.Block) bool {
	bh := block.BlockHeader()

	var expectedSeq uint64
	var expectedPrevDigest common.Digest

	// Else, either it's not the first block, or we haven't committed the first block, and it is the first block.
	// If it's the latter we have nothing else to do.
	// If it's the former, we need to find the parent of the block and ensure it is correct.
	prevBlacklist := common.NewBlacklist(uint16(len(e.validatorNodeIDs)), common.GenesisMaxBlacklistedRounds)
	if bh.Seq < e.nextSeqToCommit() {
		return false
	}
	if bh.Seq > 0 {
		prevBlock, _, found := e.locateBlock(bh.Seq-1, bh.Prev[:])
		if !found {
			e.Logger.Debug("Could not find parent block with given digest",
				zap.Uint64("blockSeq", bh.Seq-1),
				zap.Stringer("digest", bh.Prev))
			// We could not find the parent block, so no way to verify this proposal.
			return false
		}

		expectedSeq = prevBlock.BlockHeader().Seq + 1
		expectedPrevDigest = prevBlock.BlockHeader().Digest

		prevBlacklist = prevBlock.Blacklist()

		// If the previous block belongs to an earlier epoch, the blacklist should be empty.
		if prevBlock.BlockHeader().Epoch < e.Epoch || prevBlacklist.IsEmpty() {
			prevBlacklist = prevBlacklist.Stateless(uint16(len(e.validators)))
		}
	}

	if err := prevBlacklist.VerifyProposedBlacklist(block.Blacklist(), e.round); err != nil {
		e.Logger.Debug("Block contains an invalid blacklist", zap.Error(err))
		return false
	}

	digest := block.BlockHeader().Digest

	expectedBH := common.BlockHeader{
		Digest: digest,
		ProtocolMetadata: common.ProtocolMetadata{
			Round:   e.round,
			Seq:     expectedSeq,
			Epoch:   e.Epoch,
			Prev:    expectedPrevDigest,
			Version: 0,
		},
	}

	equals := expectedBH.Equals(&bh)

	if !equals {
		e.Logger.Debug("Received block with an incorrect header",
			zap.Stringer("expected", &expectedBH),
			zap.Stringer("received", &bh))
	}

	return equals
}

// locateBlock locates a block:
// 1) In memory
// 2) Else, on storage.
// Compares to the given digest, and if it's the same, returns it.
// Otherwise, returns false.
func (e *Epoch) locateBlock(seq uint64, digest []byte) (common.VerifiedBlock, *notarizationOrFinalization, bool) {
	for _, round := range e.rounds {
		dig := round.block.BlockHeader().Digest
		if bytes.Equal(dig[:], digest) {
			nof := &notarizationOrFinalization{
				Notarization: round.notarization,
				Finalization: round.finalization,
			}
			if nof.Notarization == nil && nof.Finalization == nil {
				return nil, nil, false
			}
			return round.block, nof, true
		}
	}

	height := e.nextSeqToCommit()
	// Not in memory, and no block resides in storage.
	if height == 0 {
		return nil, nil, false
	}

	// If the given block has a sequence that is higher than the last block we committed to storage,
	// we don't have the block in our storage.
	maxSeq := height - 1
	if maxSeq < seq {
		return nil, nil, false
	}

	if seq >= e.nextSeqToCommit() {
		e.Logger.Debug("Requested block sequence we have not yet committed to storage",
			zap.Uint64("requestedSeq", seq), zap.Uint64("numBlocks", e.nextSeqToCommit()))
		return nil, nil, false
	}

	block, finalization, ok := e.retrieveBlockOrHalt(seq)
	if !ok {
		return nil, nil, false
	}

	nof := &notarizationOrFinalization{
		Finalization: &finalization,
	}

	dig := block.BlockHeader().Digest
	if bytes.Equal(dig[:], digest) {
		return block, nof, true
	}

	return nil, nil, false
}

func (e *Epoch) buildBlock() {
	metadata := e.metadata()

	prevBlacklist, ok := e.retrieveBlacklistForBlockBuilding(metadata)
	if !ok {
		return
	}

	// If I'm blacklisted, I cannot propose a block.
	if prevBlacklist.IsNodeSuspected(uint16(e.validatorNodeIDs.IndexOf(e.ID))) {
		e.Logger.Debug("I'm blacklisted, cannot propose a block", zap.Uint64("round", metadata.Round), zap.Stringer("blacklist", &prevBlacklist))
		e.triggerEmptyBlockNotarization(metadata.Round)
		return
	}

	// Create the blacklist for the next round:
	nextBlacklist := prevBlacklist.Next(metadata.Round, e.MaxBlacklistedRounds, e.timedOutRounds, e.redeemedRounds)

	e.Logger.Debug("Blacklist updated",
		zap.Uint64("round", metadata.Round),
		zap.Stringer("blacklist", &nextBlacklist),
		zap.Stringer("prev", &prevBlacklist), zap.Stringer("next", &nextBlacklist))

	buildTheBlock := e.createBlockBuildingTask(metadata, nextBlacklist)

	round := e.round

	task := func() common.Digest {
		digest := buildTheBlock()
		e.lock.Lock()
		defer e.lock.Unlock()
		e.monitorProgress(round)
		return digest
	}

	e.Logger.Debug("Scheduling block building", zap.Uint64("round", metadata.Round))
	e.buildBlockScheduler.ScheduleOrReplace(task)
}

func (e *Epoch) retrieveBlacklistForBlockBuilding(metadata common.ProtocolMetadata) (common.Blacklist, bool) {
	var blacklist common.Blacklist
	if metadata.Seq > 0 {
		prevBlock, _, ok := e.locateBlock(metadata.Seq-1, metadata.Prev[:])
		if !ok {
			e.Logger.Error("Failed locating previous block",
				zap.Uint64("round", metadata.Round),
				zap.Uint64("seq", metadata.Seq),
				zap.Stringer("digest", metadata.Prev))
			e.haltedError = fmt.Errorf("failed locating previous block (%d)", metadata.Seq)
			return common.Blacklist{}, false
		}

		blacklist = prevBlock.Blacklist()

		// If the previous block belongs to a previous epoch, we need to reset the blacklist to be empty.
		if metadata.Epoch > prevBlock.BlockHeader().Epoch {
			blacklist = blacklist.Stateless(uint16(len(e.validators)))
		}
	}

	if blacklist.IsEmpty() {
		blacklist = blacklist.Stateless(uint16(len(e.validators)))
	}

	return blacklist, true
}

func (e *Epoch) createBlockBuildingTask(metadata common.ProtocolMetadata, blacklist common.Blacklist) func() common.Digest {
	e.blockBuilderCancelFunc()
	e.blockBuilderCtx, e.blockBuilderCancelFunc = context.WithCancel(e.finishCtx)
	context := e.blockBuilderCtx
	cancel := e.blockBuilderCancelFunc

	return func() common.Digest {
		e.lock.Lock()
		if e.isEpochSealed() {
			e.lock.Unlock()
			e.Logger.Debug("Aborting block building because the epoch is sealed")
			return common.Digest{}
		}
		e.lock.Unlock()

		block, ok := e.blockBuilder.BuildBlock(context, metadata, blacklist)

		e.lock.Lock()
		defer e.lock.Unlock()

		canceled := context.Err() != nil
		cancel()

		if canceled {
			return common.Digest{}
		}

		if !ok {
			e.Logger.Debug("Failed building block")
			return common.Digest{}
		}

		if err := e.proposeBlock(block); err != nil {
			e.Logger.Error("Failed proposing block", zap.Error(err))
			e.haltedError = err
			return common.Digest{}
		}

		return block.BlockHeader().Digest
	}
}

func (e *Epoch) proposeBlock(block common.VerifiedBlock) error {
	md := block.BlockHeader()

	// Write record to WAL before broadcasting it, so that
	// if we crash during broadcasting, we know what we sent.
	rawBlock := block.Bytes()

	vote, err := e.voteOnBlock(block)
	if err != nil {
		return err
	}

	if !e.storeProposal(block) {
		return errors.New("failed to store block proposed by me")
	}

	proposal := &common.Message{
		VerifiedBlockMessage: &common.VerifiedBlockMessage{
			VerifiedBlock: block,
			Vote:          vote,
		},
	}

	e.broadcast(proposal)
	e.Logger.Debug("Proposal broadcast",
		zap.Uint64("round", md.Round),
		zap.Int("size", len(rawBlock)),
		zap.Stringer("digest", md.Digest))

	// We might have received votes and finalizations from future rounds before we received this block.
	// So load the messages into our round data structure now that we have created it.
	return errors.Join(e.handleVoteMessage(&vote, e.ID), e.maybeLoadFutureMessages())
}

// Metadata returns the metadata of the next expected block of the epoch.
func (e *Epoch) Metadata() common.ProtocolMetadata {
	e.lock.Lock()
	defer e.lock.Unlock()

	return e.metadata()
}

func (e *Epoch) metadata() common.ProtocolMetadata {
	var prev common.Digest
	seq := e.nextSeqToCommit()

	highestRound := e.getHighestRound()
	if highestRound != nil {
		// Build on top of the latest block
		currMed := highestRound.block.BlockHeader()
		prev = currMed.Digest
		seq = currMed.Seq + 1
	}

	if e.lastBlock != nil {
		currMed := e.lastBlock.VerifiedBlock.BlockHeader()
		if currMed.Seq+1 >= seq {
			prev = currMed.Digest
			seq = currMed.Seq + 1
		}
	}

	md := common.ProtocolMetadata{
		Round:   e.round,
		Seq:     seq,
		Epoch:   e.Epoch,
		Prev:    prev,
		Version: 0,
	}
	return md
}

func (e *Epoch) triggerEmptyBlockNotarization(round uint64) {
	if e.haltedError != nil {
		e.Logger.Debug("Not triggering empty block notarization because the epoch is halted", zap.Error(e.haltedError))
		return
	}
	if e.round > round {
		e.Logger.Debug("Not triggering empty block notarization because we advanced to a higher round",
			zap.Uint64("round", round), zap.Uint64("currentRound", e.round))
		return
	}

	// Several paths trigger the empty block agreement for the same round, so everything below,
	// including the WAL append, must not run twice.
	if e.haveWeAlreadyTimedOutOnThisRound(round) {
		e.Logger.Debug("Not triggering empty block notarization because we already timed out on this round",
			zap.Uint64("round", round))
		return
	}

	emptyVote := common.ToBeSignedEmptyVote{EmptyVoteMetadata: common.EmptyVoteMetadata{
		Round: round,
		Epoch: e.Epoch,
	}}
	rawSig, err := emptyVote.Sign(e.Signer)
	if err != nil {
		e.Logger.Error("Failed signing message", zap.Error(err))
		return
	}

	emptyVoteRecord := common.NewEmptyVoteRecord(emptyVote)
	if err := e.WAL.Append(emptyVoteRecord); err != nil {
		e.haltedError = err
		e.Logger.Error("Failed appending empty vote", zap.Error(err))
		return
	}
	e.Logger.Debug("Persisted empty vote to WAL",
		zap.Uint64("round", round),
		zap.Int("size", len(emptyVoteRecord)))

	emptyVotes := e.getOrCreateEmptyVoteSetForRound(round)
	emptyVotes.timedOut = true

	signedEV := common.EmptyVote{Vote: emptyVote, Signature: common.Signature{Signer: e.ID, Value: rawSig}}

	// Add our own empty vote to the set
	emptyVotes.votes[string(e.ID)] = &signedEV

	e.broadcast(&common.Message{EmptyVoteMessage: &signedEV})

	e.addEmptyVoteRebroadcastTimeout()

	if err := e.maybeAssembleEmptyNotarization(); err != nil {
		e.Logger.Error("Failed assembling empty notarization", zap.Error(err))
		e.haltedError = err
	}
}

func (e *Epoch) emptyVoteTimeoutTaskRunner(_ []string) {
	e.lock.Lock()
	defer e.lock.Unlock()

	roundVotes, ok := e.emptyVotes[e.round]

	if !ok {
		e.Logger.Debug("No empty vote set found to rebroadcast, yet expected to rebroadcast", zap.Uint64("round", e.round))
		return
	}

	ourVote, voted := roundVotes.votes[string(e.ID)]
	if !voted {
		e.Logger.Debug("Our empty vote not found in the set to rebroadcast, yet expected to rebroadcast", zap.Uint64("round", e.round))
		return
	}

	e.Logger.Debug("Rebroadcasting empty vote because round has not advanced", zap.Uint64("round", ourVote.Vote.Round))
	e.broadcast(&common.Message{EmptyVoteMessage: ourVote})
}

func (e *Epoch) addEmptyVoteRebroadcastTimeout() {
	e.timeoutHandler.AddTask(EmptyVoteTimeoutID)
}

func (e *Epoch) monitorProgress(round uint64) {
	if round < e.round {
		e.Logger.Debug("Aborting monitoring progress because we advanced to a higher round", zap.Uint64("round", round), zap.Uint64("currentRound", e.round))
		return
	}

	e.Logger.Debug("Monitoring progress", zap.Uint64("round", round), zap.Uint64("currentRound", e.round))
	ctx, cancelContext := context.WithCancel(e.finishCtx)

	noop := func() {}

	leader := LeaderForRound(e.validatorNodeIDs, round)

	// If we have a task pending to be executed, remove it from execution because we're about to schedule
	// a task for a higher round.
	e.monitor.CancelTask()
	e.monitor.CancelFutureTask()

	// Since we're about to execute a task, abort the previous execution to make room for the new execution.
	e.cancelWaitForBlockNotarization()

	proposalWaitTimeExpired := func() {
		e.lock.Lock()
		defer e.lock.Unlock()

		// Check if we have advanced to a higher round in the meantime while this task was dispatched.
		if round < e.round {
			e.Logger.Debug("Not triggering empty block agreement because we advanced to a higher round")
			return
		}

		leader := LeaderForRound(e.validatorNodeIDs, round)
		e.Logger.Debug("Triggering empty block agreement",
			zap.String("reason", "Timed out on block agreement"),
			zap.Uint64("round", round),
			zap.Stringer("leader", leader))
		e.triggerEmptyBlockNotarization(round)
	}

	var cancelled atomic.Bool

	blockShouldBeBuiltNotification := func() {
		blacklist, ok := e.retrieveLastPersistedBlacklist()
		if !ok {
			return
		}

		// Before waiting for the block to be built, check if the leader is blacklisted.

		// If the current leader is blacklisted, we should not wait for it to propose a block.
		// Instead, we should immediately trigger the empty block agreement.
		leaderIndex := e.validatorNodeIDs.IndexOf(leader)
		if leaderIndex >= 0 && blacklist.IsNodeSuspected(uint16(leaderIndex)) {
			e.Logger.Debug("Leader is blacklisted, will not wait for it to propose a block",
				zap.Uint64("round", round), zap.Stringer("leader", leader))
			proposalWaitTimeExpired()
			return
		}

		// Check if we have advanced to a higher round in the meantime while this task was dispatched.
		e.lock.Lock()
		epochRound := e.round
		shouldAbort := round < epochRound
		e.lock.Unlock()

		if shouldAbort {
			e.Logger.Debug("Aborting monitoring progress for round because we advanced to a higher round",
				zap.Uint64("monitored round", round), zap.Uint64("new round", epochRound))
			return
		}

		// This invocation blocks until the block builder tells us it's time to build a new block.
		e.blockBuilder.WaitForPendingBlock(ctx)
		// While we waited, a block might have been notarized.
		// If so, then don't start monitoring for it being notarized.
		if cancelled.Load() {
			e.Logger.Debug("Not starting monitoring for block notarization because we were cancelled while waiting for block to be built", zap.Uint64("epoch round", epochRound), zap.Uint64("monitored round", round))
			return
		}

		e.Logger.Info("It is time to build a block", zap.Uint64("round", round))

		// Once it's time to build a new block, wait a grace period of 'e.maxProposalWait' time,
		// and if the monitor isn't cancelled by then, invoke proposalWaitTimeExpired() above.
		e.monitor.FutureTask(e.MaxProposalWait, proposalWaitTimeExpired)
	}

	// Registers a wait operation that:
	// (1) Waits for the block builder to tell us it thinks it's time to build a new block.
	// (2) Registers a monitor which, if not cancelled earlier, notifies the Epoch about a timeout for this round.
	scheduled := e.monitor.RunTask(blockShouldBeBuiltNotification)
	if !scheduled {
		// If we fail monitoring for a block to be proposed by the leader, this is not an irrecoverable error,
		// because it might be that other nodes have succeeded.
		e.Logger.Warn("Failed monitoring leader progress")
		cancelContext()
		return
	}

	// If we notarize a block for this round we should cancel the monitor,
	// so first stop it and then cancel the context.
	e.cancelWaitForBlockNotarization = func() {
		e.monitor.CancelFutureTask()
		cancelled.Store(true)
		cancelContext()
		e.cancelWaitForBlockNotarization = noop
	}
}

func (e *Epoch) retrieveLastPersistedBlacklist() (common.Blacklist, bool) {
	e.lock.Lock()
	defer e.lock.Unlock()

	var blacklist common.Blacklist
	// This can be the first block, in which case we don't have a blacklist.
	if e.lastBlock != nil {
		// If we have a last block that is non-nil, it should have a verified block inside.
		if e.lastBlock.VerifiedBlock == nil {
			e.Logger.Error("Last block is nil")
			e.haltedError = fmt.Errorf("last block is nil")
			return common.Blacklist{}, false
		}

		blacklist = e.lastBlock.VerifiedBlock.Blacklist()
		if e.lastBlock.Finalization.Finalization.Epoch < e.Epoch {
			blacklist = blacklist.Stateless(uint16(len(e.validators)))
		}
	}
	return blacklist, true
}

func (e *Epoch) storeFinalization(finalization *common.Finalization) error {
	if finalization == nil {
		return errors.New("finalization is nil")
	}
	roundNum := finalization.Finalization.Round
	round, exists := e.rounds[roundNum]
	if !exists {
		return fmt.Errorf("round %d not found", roundNum)
	}
	if round.finalization != nil {
		e.Logger.Debug("Received finalization for an already finalized round", zap.Uint64("round", finalization.Finalization.Round))
		return fmt.Errorf("round %d already has a finalization", roundNum)
	}
	if round.block == nil {
		return fmt.Errorf("round %d has no block to associate finalization with", roundNum)
	}

	expectedBlockHeader := round.block.BlockHeader()
	if !expectedBlockHeader.Equals(&finalization.Finalization.BlockHeader) {
		e.Logger.Debug("Equivocation detected: finalization block header does not match round block header",
			zap.Uint64("round", roundNum),
			zap.Stringer("block header", &expectedBlockHeader),
			zap.Stringer("finalized block header", &finalization.Finalization.BlockHeader))
		delete(e.rounds, roundNum)
		// We have received a block that has been equivocated and doesn't match the finalization that was assembled.
		// We therefore need to request a different block that corresponds to this finalization.
		e.replicationState.ReceivedFutureFinalization(finalization, e.nextSeqToCommit())
		return fmt.Errorf("finalization block header does not match round %d block header", roundNum)
	}

	round.finalization = finalization

	return nil
}

func (e *Epoch) startRound() error {
	// before starting the round, load any future messages we might have received
	if err := e.maybeLoadFutureMessages(); err != nil {
		return err
	}

	leaderForCurrentRound := LeaderForRound(e.validatorNodeIDs, e.round)

	if e.ID.Equals(leaderForCurrentRound) {
		e.buildBlock()
		return nil
	}

	// We're not the leader, make sure if a block is not notarized within a timely manner,
	// we will agree on an empty block.
	e.monitorProgress(e.round)
	return nil
}

func (e *Epoch) doProposed(block common.VerifiedBlock) error {
	r := block.BlockHeader().Round
	d := block.BlockHeader().Digest

	if digestAlreadyVotedOn, exists := e.blockDigestsVotedFor[r]; exists && digestAlreadyVotedOn != d {
		e.Logger.Debug("Not voting on block because we already voted on a different block for this round",
			zap.Uint64("round", r),
			zap.Stringer("already voted on digest", digestAlreadyVotedOn),
			zap.Stringer("new block digest", d))
		return nil
	}

	vote, err := e.voteOnBlock(block)
	if err != nil {
		return err
	}

	e.blockDigestsVotedFor[r] = d

	md := block.BlockHeader()

	// We do not write the vote to the WAL as we have written the block itself to the WAL
	// and we can always restore the block and sign it again if needed.
	voteMsg := &common.Message{
		VoteMessage: &vote,
	}

	e.Logger.Debug("Broadcasting vote",
		zap.Uint64("round", md.Round),
		zap.Stringer("digest", md.Digest))

	e.broadcast(voteMsg)
	// Send yourself a vote message
	return e.handleVoteMessage(&vote, e.ID)
}

func (e *Epoch) voteOnBlock(block common.VerifiedBlock) (common.Vote, error) {
	if e.haltedError != nil {
		return common.Vote{}, e.haltedError
	}
	vote := common.ToBeSignedVote{BlockHeader: block.BlockHeader()}
	sig, err := vote.Sign(e.Signer)
	if err != nil {
		return common.Vote{}, fmt.Errorf("failed signing vote %w", err)
	}

	sv := common.Vote{
		Signature: common.Signature{
			Signer: e.ID,
			Value:  sig,
		},
		Vote: vote,
	}
	return sv, nil
}

// deletesRounds deletes all the rounds before [round] in the rounds map.
func (e *Epoch) deleteRounds(round uint64) {
	for r := range e.blockDigestsVotedFor {
		if r < round {
			delete(e.blockDigestsVotedFor, r)
		}
	}
	for i, r := range e.rounds {
		if r.num+e.MaxRoundWindow < round {
			delete(e.rounds, i)
		}
	}
}

// pruneCompetingRounds deletes the blocks and empty votes below the finalized round that are not
// part of the finalized block's chain. Ex. if we have blockchains b1 <- b3 <- b5, and b1 <- b2 <- b4,
// a call to pruneCompetingRounds(finalization b5), will prune blocks b2 & b4 from the rounds map.
func (e *Epoch) pruneCompetingRounds(finalization common.Finalization) {
	prev := finalization.Finalization.Prev
	minRound := e.minRoundInRoundsMap()
	for r := finalization.Finalization.Round; r > minRound; r-- {
		round, exists := e.rounds[r-1]
		if !exists {
			continue
		}

		// don't delete this round, since the block is an ancestor of finalization in our chain
		if header := round.block.BlockHeader(); header.Digest == prev {
			prev = header.Prev
			continue
		}

		e.Logger.Debug("Pruning block not on the finalized chain", zap.Uint64("round", r-1), zap.Uint64("finalized round", finalization.Finalization.Round))
		delete(e.rounds, r-1)
	}

	for r := range e.emptyVotes {
		if r < finalization.Finalization.Round {
			delete(e.emptyVotes, r)
		}
	}
}

func (e *Epoch) increaseRound() {
	// In case we're waiting for a block to be notarized, cancel the wait because
	// we advanced to the next round.
	e.cancelWaitForBlockNotarization()

	// Cancel the block building context since we have advanced a round and might still be building a block
	e.blockBuilderCancelFunc()

	// remove the rebroadcast empty vote task
	e.timeoutHandler.RemoveTask(EmptyVoteTimeoutID)
	prevLeader := LeaderForRound(e.validatorNodeIDs, e.round)
	nextLeader := LeaderForRound(e.validatorNodeIDs, e.round+1)

	e.Logger.Info("Moving to a new round",
		zap.Uint64("prev round", e.round),
		zap.Uint64("next round", e.round+1),
		zap.Stringer("prev leader", prevLeader),
		zap.Stringer("next leader", nextLeader))
	e.round++
}

// finalizeVoteForReplicatedNotarization casts our finalize vote for a round whose notarization we learned
// of through replication instead of by collecting votes.
func (e *Epoch) finalizeVoteForReplicatedNotarization(r uint64) error {
	round, exists := e.rounds[r]
	if !exists || round.notarization == nil || round.finalization != nil {
		return nil
	}

	if e.haveWeAlreadyTimedOutOnThisRound(r) {
		e.Logger.Debug("Not finalize voting for a replicated notarization of a round we timed out on", zap.Uint64("round", r))
		return nil
	}

	md := round.notarization.Vote.BlockHeader
	finalizeVote, finalizeVoteMsg, err := e.maybeConstructFinalizeVoteMessage(md)
	if err != nil {
		return err
	}
	e.broadcast(finalizeVoteMsg)

	e.Logger.Debug("Broadcasting finalize vote for a replicated notarization",
		zap.Uint64("round", md.Round),
		zap.Uint64("seq", md.Seq),
		zap.Stringer("digest", md.Digest))

	return e.handleFinalizeVoteMessage(&finalizeVote, e.ID)
}

func (e *Epoch) doNotarized(r uint64, advancedRound bool) error {
	if e.haveWeAlreadyTimedOutOnThisRound(r) {
		e.Logger.Info("We have already timed out on this round, will not finalize it", zap.Uint64("round", r))
		if advancedRound {
			return e.startRound()
		}
		return nil
	}

	round := e.rounds[r]
	block := round.block

	md := block.BlockHeader()

	finalizeVote, finalizeVoteMsg, err := e.maybeConstructFinalizeVoteMessage(md)
	if err != nil {
		return err
	}
	e.broadcast(finalizeVoteMsg)

	e.Logger.Debug("Broadcasting finalize vote",
		zap.Uint64("round", md.Round),
		zap.Uint64("seq", md.Seq),
		zap.Stringer("digest", md.Digest))

	var advancedRoundErr error
	if advancedRound {
		advancedRoundErr = e.startRound()
	}
	return errors.Join(advancedRoundErr, e.handleFinalizeVoteMessage(&finalizeVote, e.ID))
}

func (e *Epoch) maybeConstructFinalizeVoteMessage(md common.BlockHeader) (common.FinalizeVote, *common.Message, error) {
	if e.haltedError != nil {
		return common.FinalizeVote{}, nil, e.haltedError
	}
	if e.haveWeAlreadyTimedOutOnThisRound(md.Round) {
		e.Logger.Error("Will not cast a finalize vote for a round we timed out on)", zap.Uint64("round", md.Round))
		return common.FinalizeVote{}, nil, errAlreadyTimedOutOnRound
	}

	f := common.ToBeSignedFinalization{BlockHeader: md}
	signature, err := f.Sign(e.Signer)
	if err != nil {
		return common.FinalizeVote{}, nil, fmt.Errorf("failed signing vote %w", err)
	}

	vote := common.FinalizeVote{
		Signature: common.Signature{
			Signer: e.ID,
			Value:  signature,
		},
		Finalization: common.ToBeSignedFinalization{
			BlockHeader: md,
		},
	}

	finalizationMsg := &common.Message{
		FinalizeVote: &vote,
	}
	return vote, finalizationMsg, nil
}

func (e *Epoch) maybeLoadFutureMessages() error {
	for {
		round := e.round
		nextSeqToCommit := e.nextSeqToCommit()

		for from, messagesFromNode := range e.futureMessages {
			if msgs, exists := messagesFromNode[round]; exists {
				if msgs.proposal != nil && !msgs.proposalBeingProcessed {
					if err := e.handleBlockMessage(msgs.proposal, common.NodeID(from)); err != nil {
						return err
					}
				}
				if msgs.finalization != nil {
					if err := e.handleFinalizationMessage(msgs.finalization, common.NodeID(from)); err != nil {
						return err
					}
				}
				if msgs.vote != nil {
					if err := e.handleVoteMessage(msgs.vote, common.NodeID(from)); err != nil {
						return err
					}
				}
				if msgs.notarization != nil {
					if err := e.handleNotarizationMessage(msgs.notarization, common.NodeID(from)); err != nil {
						return err
					}
				}
				if msgs.finalizeVote != nil {
					if err := e.handleFinalizeVoteMessage(msgs.finalizeVote, common.NodeID(from)); err != nil {
						return err
					}
				}
			} else {
				e.Logger.Debug("No future messages received for this round",
					zap.Stringer("from", common.NodeID(from)), zap.Uint64("round", round))
			}
		}

		emptyVotes, exists := e.emptyVotes[round]
		if exists {
			if emptyVotes.emptyNotarization != nil {
				if err := e.handleEmptyNotarizationMessage(emptyVotes.emptyNotarization, nil); err != nil {
					return err
				}
			} else {
				for from, vote := range emptyVotes.votes {
					if err := e.handleEmptyVoteMessage(vote, common.NodeID(from)); err != nil {
						return err
					}
				}
			}
		}

		if e.round == round && nextSeqToCommit == e.nextSeqToCommit() {
			return nil
		}
		e.Logger.Debug("Round or height was increased while processing future messages", zap.Uint64("epoch round", e.round), zap.Uint64("Round", round), zap.Uint64("previous nextSeqToCommit", nextSeqToCommit), zap.Uint64("nextSeqToCommit", e.nextSeqToCommit()))
	}
}

// storeProposal stores a block in the epochs memory(NOT ledger) and appends it to the WAL.
// it creates a new round with the block and stores it in the rounds map.
func (e *Epoch) storeProposal(block common.VerifiedBlock) bool {
	md := block.BlockHeader()

	// Have we already received a block from that node?
	// If so, it cannot change its mind and send us a different block.
	if _, exists := e.rounds[md.Round]; exists {
		// We have already received a block for this round in the past, refuse receiving an alternative block.
		// We do this because we may have already voted for a different block.
		// Refuse processing the block to not be coerced into voting for a different block.
		e.Logger.Debug("Already received block for round", zap.Uint64("round", md.Round))
		return false
	}

	round := NewRound(block)
	e.rounds[md.Round] = round

	e.Logger.Debug("Stored proposal in memory",
		zap.Uint64("round", md.Round),
		zap.Uint64("seq", md.Seq),
		zap.Stringer("digest", md.Digest))

	blockBytes := block.Bytes()

	blockRecord, err := common.BlockRecord(md, blockBytes)
	if err != nil {
		e.haltedError = err
		e.Logger.Error("Failed to create block record for WAL", zap.Error(err))
		return false
	}
	if err := e.WAL.Append(blockRecord); err != nil {
		e.haltedError = err
		e.Logger.Error("Failed to append block record to WAL", zap.Error(err))
		return false
	}

	e.Logger.Debug("Appended block record to WAL", zap.Uint64("round", md.Round),
		zap.Uint64("seq", md.Seq),
		zap.Int("size", len(blockBytes)),
		zap.Stringer("digest", md.Digest))

	return true
}

// HandleRequest processes a request and returns a response. It also sends a response to the sender.
func (e *Epoch) handleReplicationRequest(req *common.ReplicationRequest, from common.NodeID) error {
	e.Logger.Debug("Received replication request", zap.Stringer("from", from), zap.Uint64s("seqs", req.Seqs), zap.Int("num rounds", len(req.Rounds)), zap.Uint64("latest round", req.LatestRound), zap.Uint64("latest seq", req.LatestFinalizedSeq))
	if !e.ReplicationEnabled {
		return nil
	}
	response := &common.VerifiedReplicationResponse{}

	seqs := req.Seqs
	slices.Sort(seqs)
	if len(seqs) > int(e.MaxRoundWindow) {
		e.Logger.Debug("Truncating replication request seqs",
			zap.Stringer("from", from),
			zap.Int("num seqs", len(req.Seqs)),
			zap.Uint64("max round window", e.MaxRoundWindow))
		seqs = seqs[:e.MaxRoundWindow]
	}
	rounds := req.Rounds
	slices.Sort(rounds)
	if len(rounds) > int(e.MaxRoundWindow) {
		e.Logger.Debug("Truncating replication request rounds",
			zap.Stringer("from", from),
			zap.Int("num rounds", len(req.Rounds)),
			zap.Uint64("max round window", e.MaxRoundWindow))
		rounds = rounds[:e.MaxRoundWindow]
	}
	remainingBytes := e.MaxReplicationResponseSize

	if req.LatestFinalizedSeq > 0 {
		if e.lastBlock != nil && e.lastBlock.Finalization.Finalization.Seq >= req.LatestFinalizedSeq {
			latestFinalizedSeq := &common.VerifiedQuorumRound{
				VerifiedBlock: e.lastBlock.VerifiedBlock,
				Finalization:  &e.lastBlock.Finalization,
			}
			size := latestFinalizedSeq.Size()
			if size > remainingBytes {
				e.Logger.Error("Latest finalized block exceeds the maximum replication response size",
					zap.Stringer("from", from),
					zap.Uint64("seq", e.lastBlock.Finalization.Finalization.Seq),
					zap.Int("size", size),
					zap.Int("max size", e.MaxReplicationResponseSize))
				return nil
			}
			response.LatestFinalizedSeq = latestFinalizedSeq
			remainingBytes -= size
		}
	}
	if req.LatestRound > 0 {
		latestRound := e.getLatestVerifiedQuorumRound()
		if latestRound != nil && latestRound.GetRound() > req.LatestRound {
			size := latestRound.Size()
			if size <= remainingBytes || response.LatestFinalizedSeq == nil {
				response.LatestRound = latestRound
				remainingBytes -= size
			}
		}
	}

	seqData := make([]common.VerifiedQuorumRound, len(seqs))
	for i, seq := range seqs {
		quorumRound := e.locateQuorumRecord(seq)
		if quorumRound == nil {
			// since we are sorted, we can break early
			seqData = seqData[:i]
			break
		}
		size := quorumRound.Size()
		if size > remainingBytes {
			e.Logger.Debug("Replication response reached size limit",
				zap.Stringer("from", from),
				zap.Uint64("Stopped at Seq", seq),
				zap.Int("max size", e.MaxReplicationResponseSize))
			seqData = seqData[:i]
			break
		}
		remainingBytes -= size

		seqData[i] = *quorumRound
	}

	roundData := make([]common.VerifiedQuorumRound, 0, len(rounds))
	for _, roundNum := range rounds {
		quorumRound := e.locateQuorumRecordByRound(roundNum)
		if quorumRound == nil {
			// we cannot break early since empty votes may
			continue
		}
		size := quorumRound.Size()
		if size > remainingBytes {
			e.Logger.Debug("Replication response reached size limit",
				zap.Stringer("from", from),
				zap.Uint64("Stopped at Round", roundNum),
				zap.Int("max size", e.MaxReplicationResponseSize))
			break
		}
		remainingBytes -= size

		roundData = append(roundData, *quorumRound)
	}

	data := make([]common.VerifiedQuorumRound, 0, len(seqData)+len(roundData))
	data = append(data, seqData...)
	data = append(data, roundData...)
	response.Data = data

	if len(data) == 0 && response.LatestRound == nil && response.LatestFinalizedSeq == nil {
		e.Logger.Debug("No data found for replication request", zap.Stringer("from", from))
		return nil
	}

	e.Logger.Debug("Sending response back to node", zap.Stringer("to", from), zap.Int("num rounds", len(data)))
	msg := &common.Message{VerifiedReplicationResponse: response}
	e.send(msg, from)
	return nil
}

// locateQuorumRecord locates a block with a notarization or finalization in the epochs memory or storage.
func (e *Epoch) locateQuorumRecord(seq uint64) *common.VerifiedQuorumRound {
	var notarizedRound *Round
	for _, round := range e.rounds {
		blockSeq := round.block.BlockHeader().Seq
		if blockSeq == seq {
			if round.finalization != nil {
				return &common.VerifiedQuorumRound{
					VerifiedBlock: round.block,
					Finalization:  round.finalization,
				}
			} else if round.notarization != nil {
				if notarizedRound == nil {
					notarizedRound = round
				} else if round.notarization.Vote.Round > notarizedRound.num {
					// set the notarized round if it is the highest round we have seen so far
					notarizedRound = round
				}
			}
		}
	}

	if notarizedRound != nil {
		// we have a notarization, but no finalization, so we return the notarization
		return &common.VerifiedQuorumRound{
			VerifiedBlock: notarizedRound.block,
			Notarization:  notarizedRound.notarization,
		}
	}

	if seq >= e.nextSeqToCommit() {
		e.Logger.Debug("Requested quorum record sequence we have not yet committed to storage",
			zap.Uint64("seq", seq), zap.Uint64("height", e.nextSeqToCommit()))
		return nil
	}

	block, finalization, ok := e.retrieveBlockOrHalt(seq)
	if !ok {
		return nil
	}

	return &common.VerifiedQuorumRound{
		VerifiedBlock: block,
		Finalization:  &finalization,
	}
}

func (e *Epoch) locateQuorumRecordByRound(targetRound uint64) *common.VerifiedQuorumRound {
	var qr *common.VerifiedQuorumRound

	for _, round := range e.rounds {
		blockRound := round.block.BlockHeader().Round
		if blockRound == targetRound {
			if round.finalization != nil || round.notarization != nil {
				qr = &common.VerifiedQuorumRound{
					VerifiedBlock: round.block,
					Finalization:  round.finalization,
					Notarization:  round.notarization,
				}
				break
			}
		}
	}

	// check if the round is empty notarized
	emptyVoteForRound, exists := e.emptyVotes[targetRound]
	if exists && emptyVoteForRound.emptyNotarization != nil {
		if qr != nil {
			qr.EmptyNotarization = emptyVoteForRound.emptyNotarization
			return qr
		}
		qr = &common.VerifiedQuorumRound{
			EmptyNotarization: emptyVoteForRound.emptyNotarization,
		}
	}

	return qr
}

func (e *Epoch) handleBlockDigestRequest(req *common.BlockDigestRequest, from common.NodeID) error {
	e.Logger.Debug("Received block digest request", zap.Stringer("from", from), zap.Uint64("seq", req.Seq))
	block, notarizationOrFinalization, ok := e.locateBlock(req.Seq, req.Digest[:])

	if !ok {
		e.Logger.Debug("Block not found for digest request", zap.Uint64("seq", req.Seq), zap.Stringer("digest", req.Digest))
		return nil
	}

	if notarizationOrFinalization == nil {
		e.Logger.Debug("No notarization or finalization found for block digest request", zap.Uint64("seq", req.Seq), zap.Stringer("digest", req.Digest))
		return nil
	}

	qr := common.VerifiedQuorumRound{
		VerifiedBlock: block,
		Notarization:  notarizationOrFinalization.Notarization,
		Finalization:  notarizationOrFinalization.Finalization,
	}

	response := &common.VerifiedReplicationResponse{
		Data: []common.VerifiedQuorumRound{qr},
	}

	msg := &common.Message{VerifiedReplicationResponse: response}
	e.send(msg, from)
	return nil
}

func (e *Epoch) handleReplicationResponse(resp *common.ReplicationResponse, from common.NodeID) error {
	if !e.ReplicationEnabled {
		return nil
	}

	e.Logger.Debug("Received replication response", zap.Stringer("from", from), zap.Int("num seqs", len(resp.Data)), zap.Stringer("latest round", resp.LatestRound), zap.Stringer("latest seq", resp.LatestSeq))
	for _, data := range resp.Data {
		if err := e.processQuorumRound(&data, from); err != nil {
			e.Logger.Debug("Failed processing quorum round", zap.Error(err))
		}
	}

	if err := e.processQuorumRound(resp.LatestRound, from); err != nil {
		e.Logger.Debug("Failed processing latest round", zap.Error(err))
	}

	if err := e.processQuorumRound(resp.LatestSeq, from); err != nil {
		e.Logger.Debug("Failed processing latest seq", zap.Error(err))
	}

	return e.processReplicationState()
}

func (e *Epoch) verifyQuorumRound(q common.QuorumRound) error {
	if err := q.VerifyQCConsistentWithBlock(); err != nil {
		return err
	}

	if q.Finalization != nil {
		// extra check needed if we have a finalized block
		if err := VerifyQC(q.Finalization.QC, e.signatureAggregator.IsQuorum, e.validatorsToPKs, q.Finalization, e.validators); err != nil {
			return fmt.Errorf("invalid finalization: %w", err)
		}
	}

	if q.Notarization != nil {
		if err := VerifyQC(q.Notarization.QC, e.signatureAggregator.IsQuorum, e.validatorsToPKs, q.Notarization, e.validators); err != nil {
			return fmt.Errorf("invalid notarization: %w", err)
		}
	}

	if q.EmptyNotarization != nil {
		if err := VerifyQC(q.EmptyNotarization.QC, e.signatureAggregator.IsQuorum, e.validatorsToPKs, q.EmptyNotarization, e.validators); err != nil {
			return fmt.Errorf("invalid empty notarization QC: %w", err)
		}
	}

	return nil
}

func (e *Epoch) processEmptyNotarization(emptyNotarization *common.EmptyNotarization) error {
	e.Logger.Debug("Processing empty notarization due to replication", zap.Uint64("round", emptyNotarization.Vote.Round), zap.Uint64("our round", e.round))
	emptyVotes := e.getOrCreateEmptyVoteSetForRound(emptyNotarization.Vote.Round)
	emptyVotes.emptyNotarization = emptyNotarization

	err := e.persistEmptyNotarization(emptyVotes, false)
	if err != nil {
		return err
	}

	return e.processReplicationState()
}

// processQuorumRound processes a quorum round received from another node.
// It verifies the quorum round and stores it in the replication state if valid.
func (e *Epoch) processQuorumRound(round *common.QuorumRound, from common.NodeID) error {
	if round == nil {
		return nil
	}

	// make sure the round is well formed
	if err := round.IsWellFormed(); err != nil {
		return fmt.Errorf("received malformed latest round: %w", err)
	}

	epochOfQR := round.GetEpoch()
	if e.Epoch != epochOfQR {
		// Skip processing the quorum round if it belongs to a different epoch.
		e.Logger.Debug("Received quorum round for a different epoch, ignoring", zap.Uint64("our epoch", e.Epoch), zap.Uint64("received epoch", epochOfQR))
		return nil
	}

	if round.Finalization == nil && e.isBelowHighestFinalizedRound(round.GetRound()) {
		return fmt.Errorf("received a quorum round for a round that has been finalized. round: %d; seq: %d", round.GetRound(), round.GetSequence())
	}

	if round.Finalization != nil && e.lastBlock != nil && e.lastBlock.VerifiedBlock.BlockHeader().Seq > round.Finalization.Finalization.Seq {
		return fmt.Errorf("received a finalized round for a committed sequence. round: %d; seq: %d", round.GetRound(), round.GetSequence())
	}

	if err := e.verifyQuorumRound(*round); err != nil {
		return fmt.Errorf("failed verifying latest round: %w", err)
	}

	e.maybeStoreQuorumRound(round)
	return nil
}

// maybeStoreQuorumRound stores a verified quorum round in the replication state if it is within the round window.
// Quorum rounds beyond the window notify replication, but are not stored to keep memory bounded.
func (e *Epoch) maybeStoreQuorumRound(round *common.QuorumRound) {
	nextSeqToCommit := e.nextSeqToCommit()
	if round.Finalization != nil && round.GetSequence() > nextSeqToCommit+e.MaxRoundWindow {
		e.replicationState.ReceivedFutureFinalization(round.Finalization, nextSeqToCommit)
		return
	}

	// Store the next sequence to commit, even if it is passed MaxRoundWindow, since we may have a lot of empty notarizations
	isNextSeqToCommit := round.GetSequence() == nextSeqToCommit
	if round.Finalization == nil && round.GetRound() > e.round+e.MaxRoundWindow && !isNextSeqToCommit {
		var signers []common.NodeID
		if round.EmptyNotarization != nil {
			signers = round.EmptyNotarization.QC.Signers()
		} else if round.Notarization != nil {
			signers = round.Notarization.QC.Signers()
		}
		e.replicationState.ReceivedFutureRound(round.GetRound(), round.GetSequence(), e.round, signers)
		return
	}

	e.replicationState.StoreQuorumRound(round)
}

func (e *Epoch) processReplicationState() error {
	nextSeqToCommit := e.nextSeqToCommit()

	// We might have advanced the rounds from non-replicating paths such as future messages. Advance replication state accordingly.
	var lastCommittedRound uint64
	if e.lastBlock != nil {
		lastCommittedRound = e.lastBlock.VerifiedBlock.BlockHeader().Round
	}
	e.replicationState.MaybeAdvanceState(nextSeqToCommit, e.round, lastCommittedRound)

	// first we check if we can commit the next sequence, it is ok to try and commit the next sequence
	// directly, since if there are any empty notarizations, `indexFinalization` will
	// increment the round properly.
	block, finalization, exists := e.replicationState.GetFinalizedBlockForSequence(nextSeqToCommit)
	if exists {
		e.replicationState.DeleteSeq(nextSeqToCommit)
		return e.processFinalizedBlock(block, finalization)
	}

	// process the lowest round in our replication state(up to and including the current epoch round)
	lowestRound := e.replicationState.GetLowestRound()
	if lowestRound != nil && lowestRound.GetRound() <= e.round {
		e.Logger.Debug("Process replication state", zap.Stringer("lowest round", lowestRound), zap.Uint64("Our round", e.round))

		// Remove before processing to avoid infinite recursion
		e.replicationState.DeleteRound(lowestRound.GetRound())
		if lowestRound.Notarization != nil {
			if err := e.processNotarizedBlock(lowestRound.Block, lowestRound.Notarization); err != nil {
				return err
			}
		}
		// we can also have an empty notarization
		if lowestRound.EmptyNotarization != nil {
			if err := e.processEmptyNotarization(lowestRound.EmptyNotarization); err != nil {
				return err
			}
		}

	}

	// maybe there are no replication rounds < our round but we can still advance from empty notarizations
	err := e.maybeAdvanceRoundFromEmptyNotarizations()
	if err != nil {
		return err
	}

	e.Logger.Debug("Nothing to process in replication state", zap.Uint64("Our round", e.round), zap.Uint64("NextSeqToCommit", nextSeqToCommit), zap.Stringer("lowest replication round", lowestRound))
	return nil
}

// maybeAdvanceRoundFromEmptyNotarizations advances the round if
// there is an empty notarization for the current sequence.
//
// For example, say we have the following QuorumRounds
//
//	QRound1 { round 1, seq 1 }
//	QRound2 { round 8, seq 1 }
//
// in this case we can infer there was 8-1 empty notarizations during rounds [2, 8].
func (e *Epoch) maybeAdvanceRoundFromEmptyNotarizations() error {
	round := e.round
	expectedSeq := e.metadata().Seq

	block := e.replicationState.GetBlockWithSeq(expectedSeq)
	if block != nil {
		bh := block.BlockHeader()
		// num empty notarizations
		if round < bh.Round {
			e.Logger.Debug("Advancing round from a gap in empty notarizations", zap.Uint64("epoch round", round), zap.Uint64("block round", bh.Round))
			for range bh.Round - round {
				e.increaseRound()
			}
			return e.processReplicationState()
		}
	}

	return nil
}

// getHighestRound returns the highest round that has either a notarization or finalization
func (e *Epoch) getHighestRound() *Round {
	var max uint64
	var found bool

	for _, round := range e.rounds {
		if round.num >= max {
			if round.notarization == nil && round.finalization == nil {
				continue
			}
			max = round.num
			found = true
		}
	}

	if found {
		return e.rounds[max]
	}

	return nil
}

func (e *Epoch) getHighestEmptyNotarization() *common.EmptyNotarization {
	var emptyNotarization *common.EmptyNotarization
	var max uint64
	for round, emptyVote := range e.emptyVotes {
		if round > max && emptyVote.emptyNotarization != nil {
			max = round
			emptyNotarization = emptyVote.emptyNotarization
		}
	}

	return emptyNotarization
}

func (e *Epoch) getLatestVerifiedQuorumRound() *common.VerifiedQuorumRound {
	return GetLatestVerifiedQuorumRound(
		e.getHighestRound(),
		e.getHighestEmptyNotarization(),
	)
}

// isRoundTooFarAhead returns true if [round] is more than `maxRoundWindow` rounds ahead of the current round.
func (e *Epoch) isRoundTooFarAhead(round uint64) bool {
	return round > e.round+e.MaxRoundWindow
}

// isWithinMaxRoundWindow checks if [round] is within `maxRoundWindow` rounds ahead of the current round.
func (e *Epoch) isWithinMaxRoundWindow(round uint64) bool {
	return e.round < round && round-e.round < e.MaxRoundWindow
}

func (e *Epoch) retrieveBlockOrHalt(seq uint64) (common.VerifiedBlock, common.Finalization, bool) {
	block, finalization, err := e.Storage.Retrieve(seq)
	if err == common.ErrBlockNotFound {
		return nil, common.Finalization{}, false
	}

	if err != nil {
		e.Logger.Error("Failed retrieving block from storage", zap.Uint64("seq", seq), zap.Error(err))
		e.haltedError = err
		return nil, common.Finalization{}, false
	}

	return block, finalization, true
}

func (e *Epoch) nextSeqToCommit() uint64 {
	// The next sequence to commit is always the number of blocks in the storage.
	return e.Storage.NumBlocks()
}

func LeaderForRound(nodes []common.NodeID, r uint64) common.NodeID {
	n := len(nodes)
	return nodes[r%uint64(n)]
}

// messagesFromNode maps nodeIds to the messages it sent in a given round.
type messagesFromNode map[string]map[uint64]*messagesForRound

type messagesForRound struct {
	proposalBeingProcessed bool
	proposal               *common.BlockMessage
	vote                   *common.Vote
	finalizeVote           *common.FinalizeVote
	finalization           *common.Finalization
	notarization           *common.Notarization
}

type notarizationOrFinalization struct {
	*common.Notarization
	*common.Finalization
}
