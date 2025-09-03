// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/simplex"
	"github.com/ava-labs/simplex/wal"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

var _ common.Engine = (*Engine)(nil)

var (
	maxProposalWaitTime = time.Second * 2
	maxRebroadcastWait  = time.Second * 2
	tickInterval        = time.Millisecond * 500

	errUnknownMessageType = errors.New("unknown message type")
	errConvertingMessage  = errors.New("error converting message to simplex message")
)

type Engine struct {
	// list of NoOpsHandler for messages dropped by engine
	common.AllGetsServer
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler

	// Handler that passes application messages to the VM
	common.AppHandler

	epoch              *simplex.Epoch
	blockDeserializer  *blockDeserializer
	quorumDeserializer *QCDeserializer
	logger             logging.Logger
	vm                 block.ChainVM
	consensusCtx       *snow.ConsensusContext
}

// THe VM must be initialized before creating the engine
func NewEngine(consensusCtx *snow.ConsensusContext, ctx context.Context, config *Config) (*Engine, error) {
	signer, verifier := NewBLSAuth(config)
	qcDeserializer := &QCDeserializer{
		verifier: &verifier,
	}
	blockDeserializer := &blockDeserializer{
		parser: config.VM,
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
	wal, err := wal.New(config.WALLocation)
	if err != nil {
		return nil, err
	}

	lastBlock, _, err := storage.Retrieve(storage.NumBlocks() - 1)
	if err != nil {
		return nil, fmt.Errorf("couldn't find last block at height %d: %w", storage.NumBlocks()-1, err)
	}

	blockTracker := newBlockTracker(lastBlock.(*Block))

	blockBuilder := &BlockBuilder{
		vm:           config.VM,
		blockTracker: blockTracker,
		log:          config.Log,
	}

	epochConfig := simplex.EpochConfig{
		MaxProposalWait:     maxProposalWaitTime,
		MaxRebroadcastWait:  maxRebroadcastWait,
		QCDeserializer:      qcDeserializer,
		Logger:              config.Log,
		ID:                  config.Ctx.NodeID[:],
		Signer:              &signer,
		Verifier:            &verifier,
		BlockDeserializer:   blockDeserializer,
		SignatureAggregator: signatureAggregator,
		Comm:                comm,
		Storage:             storage,
		WAL:                 wal,
		BlockBuilder:        blockBuilder,
		Epoch:               0, // 0 for now, but we would get the epoch from the metadata associated with the latest block
		StartTime:           time.Now(),
		ReplicationEnabled:  true,
	}

	epoch, err := simplex.NewEpoch(epochConfig)
	if err != nil {
		return nil, err
	}

	return &Engine{
		epoch:                       epoch,
		blockDeserializer:           blockDeserializer,
		quorumDeserializer:          qcDeserializer,
		logger:                      config.Log,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(config.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(config.Log),
		AncestorsHandler:            common.NewNoOpAncestorsHandler(config.Log),
		PutHandler:                  common.NewNoOpPutHandler(config.Log),
		QueryHandler:                common.NewNoOpQueryHandler(config.Log),
		ChitsHandler:                common.NewNoOpChitsHandler(config.Log),

		AppHandler:   config.VM,
		vm:           config.VM,
		consensusCtx: consensusCtx,
	}, nil
}

func (e *Engine) Start(ctx context.Context, _ uint32) error {
	e.logger.Info("Starting simplex engine")
	go e.tick()
	err := e.epoch.Start()
	if err != nil {
		return fmt.Errorf("failed to start simplex epoch: %w", err)
	}

	e.consensusCtx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.NormalOp,
	})
	return e.vm.SetState(ctx, snow.NormalOp)
}

// tick periodically advances the engine's time.
func (e *Engine) tick() {
	ticker := time.NewTicker(tickInterval)
	for {
		tick := <-ticker.C
		e.epoch.AdvanceTime(tick)
	}
}

func (e *Engine) SimplexMessage(ctx context.Context, nodeID ids.NodeID, msg *p2p.Simplex) error {
	simplexMsg, err := e.p2pToSimplexMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("%w: %w", errConvertingMessage, err)
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
	return nil
}

func (e *Engine) Gossip(_ context.Context) error {
	e.logger.Debug("Gossip is not implemented for simplex")
	return nil
}

func (e *Engine) Notify(_ context.Context, _ common.Message) error {
	e.logger.Debug("Notify is not implemented for simplex")
	return nil
}

func (e *Engine) Connected(_ context.Context, _ ids.NodeID, _ *version.Application) error {
	e.logger.Debug("Connected is not implemented for simplex")
	return nil
}

func (e *Engine) Disconnected(_ context.Context, _ ids.NodeID) error {
	e.logger.Debug("Disconnected is not implemented for simplex")
	return nil
}

func (*Engine) HealthCheck(_ context.Context) (interface{}, error) {
	return nil, nil
}

var _ common.BootstrapableEngine = (*TODOBootstrapper)(nil)

type TODOBootstrapper struct {
	*Engine
	Log logging.Logger
}

func (t *TODOBootstrapper) Start(ctx context.Context, _ uint32) error {
	t.Log.Info("Starting TODO bootstrapper - does nothing")

	t.Engine.consensusCtx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.NormalOp,
	})
	if err := t.Engine.vm.SetState(ctx, snow.NormalOp); err != nil {
		return fmt.Errorf("failed to notify VM that consensus is starting: %w",
			err)
	}

	return t.Engine.Start(ctx, 0)
}

func (*TODOBootstrapper) Clear(_ context.Context) error {
	return nil
}
