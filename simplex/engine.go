// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/simplex"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"

	pSimplex "github.com/ava-labs/avalanchego/snow/consensus/simplex"
)

var errUnknownMessageType = errors.New("unknown message type")

type Engine struct {
	epoch              *simplex.Epoch
	blockDeserializer  *blockDeserializer
	quorumDeserializer *QCDeserializer
	logger             logging.Logger

	tickInterval time.Duration
	shutdown     chan struct{}
}

// The VM must be initialized before creating the engine
func NewEngine(ctx context.Context, config *Config) (*Engine, error) {
	signer, verifier, err := NewBLSAuth(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create BLS auth: %w", err)
	}
	qcDeserializer := &QCDeserializer{
		verifier: &verifier,
	}

	signatureAggregator := &SignatureAggregator{
		verifier: &verifier,
	}
	comm, err := NewComm(config)
	if err != nil {
		return nil, err
	}

	storage, err := newStorage(ctx, config, qcDeserializer, &blockTracker{})
	if err != nil {
		return nil, err
	}

	if storage.NumBlocks() == 0 {
		return nil, errors.New("storage has no blocks")
	}

	lastBlock, _, err := storage.Retrieve(storage.NumBlocks() - 1)
	if err != nil {
		return nil, fmt.Errorf("couldn't find last block at height %d: %w", storage.NumBlocks()-1, err)
	}

	simplexBlock, ok := lastBlock.(*Block)
	if !ok {
		return nil, fmt.Errorf("expected last block to be of type *Block but got %T", lastBlock)
	}

	blockTracker := newBlockTracker(simplexBlock)
	blockBuilder := &BlockBuilder{
		vm:           config.VM,
		blockTracker: blockTracker,
		log:          config.Log,
	}
	storage.blockTracker = blockTracker

	blockDeserializer := &blockDeserializer{
		parser:       config.VM,
		blockTracker: blockTracker,
	}

	epochConfig := simplex.EpochConfig{
		MaxProposalWait:     config.Params.MaxProposalWait,
		MaxRebroadcastWait:  config.Params.MaxRebroadcastWait,
		QCDeserializer:      qcDeserializer,
		Logger:              config.Log,
		ID:                  config.Ctx.NodeID[:],
		Signer:              &signer,
		Verifier:            &verifier,
		BlockDeserializer:   blockDeserializer,
		SignatureAggregator: signatureAggregator,
		Comm:                comm,
		Storage:             storage,
		WAL:                 config.WAL,
		BlockBuilder:        blockBuilder,
		Epoch:               simplexBlock.metadata.Epoch,
		StartTime:           time.Now(),
		ReplicationEnabled:  true,
	}

	epoch, err := simplex.NewEpoch(epochConfig)
	if err != nil {
		return nil, err
	}

	return &Engine{
		epoch:              epoch,
		blockDeserializer:  blockDeserializer,
		quorumDeserializer: qcDeserializer,
		logger:             config.Log,

		tickInterval: getTickInterval(config.Params),
	}, nil
}

func (e *Engine) Start(_ context.Context, _ uint32) error {
	e.logger.Info("Starting simplex engine")
	err := e.epoch.Start()
	if err != nil {
		return fmt.Errorf("failed to start simplex epoch: %w", err)
	}

	go e.tick()
	return nil
}

func getTickInterval(params *pSimplex.Parameters) time.Duration {
	tick := min(int64(params.MaxProposalWait), int64(params.MaxRebroadcastWait)) / 10
	return time.Duration(tick)
}

// tick periodically advances the engine's time.
func (e *Engine) tick() {
	ticker := time.NewTicker(e.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case tick := <-ticker.C:
			e.epoch.AdvanceTime(tick)
		case <-e.shutdown:
			ticker.Stop()
			return
		}
	}
}

func (e *Engine) Simplex(ctx context.Context, nodeID ids.NodeID, msg *p2p.Simplex) error {
	simplexMsg, err := e.p2pToSimplexMessage(ctx, msg)
	if err != nil {
		e.logger.Debug("failed to convert p2p message to simplex message", zap.Error(err))
		return nil
	}

	return e.epoch.HandleMessage(simplexMsg, nodeID[:])
}

func (e *Engine) p2pToSimplexMessage(ctx context.Context, msg *p2p.Simplex) (*simplex.Message, error) {
	if msg == nil {
		return nil, errNilField
	}

	switch {
	case msg.GetBlockProposal() != nil:
		return blockProposalFromP2P(ctx, msg.GetBlockProposal(), e.blockDeserializer)
	case msg.GetEmptyNotarization() != nil:
		return emptyNotarizationMessageFromP2P(msg.GetEmptyNotarization(), e.quorumDeserializer)
	case msg.GetVote() != nil:
		return voteFromP2P(msg.GetVote())
	case msg.GetEmptyVote() != nil:
		return emptyVoteFromP2P(msg.GetEmptyVote())
	case msg.GetNotarization() != nil:
		return notarizationMessageFromP2P(msg.GetNotarization(), e.quorumDeserializer)
	case msg.GetFinalizeVote() != nil:
		return finalizeVoteFromP2P(msg.GetFinalizeVote())
	case msg.GetFinalization() != nil:
		return finalizationMessageFromP2P(msg.GetFinalization(), e.quorumDeserializer)
	case msg.GetReplicationRequest() != nil:
		return replicationRequestFromP2P(msg.GetReplicationRequest()), nil
	case msg.GetReplicationResponse() != nil:
		return replicationResponseFromP2P(ctx, msg.GetReplicationResponse(), e.blockDeserializer, e.quorumDeserializer)
	default:
		return nil, errUnknownMessageType
	}
}

func (e *Engine) Shutdown(_ context.Context) error {
	e.epoch.Stop()
	e.logger.Info("Stopped simplex engine")
	close(e.shutdown)
	return nil
}
